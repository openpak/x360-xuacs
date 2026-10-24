package xuacs

// The account core's event stream (openpak.account.v1.Events/PollEvents), so a ban
// or a deletion ends Xbox 360 sessions at once:
//
//	account_banned          deny: session tokens dropped, no new sign-in, no user key
//	                        for the KDC, WLID tickets refused
//	account_unbanned        lift the ban
//	account_delete_started  suspend (the core's grace period): the same denial, the
//	                        profiles and gamertag stay held for the account
//	account_restored        lift the suspension
//	account_deleted         final erasure: purge the account's profiles (online XUID,
//	                        offline XUIDs, XenonKeys, gamertags) and tokens
//
// The effect and the cursor are written together (Store.ApplyEvent), so a
// redelivered event is a no-op and a restart resumes where it stopped. A fresh
// state starts at version 0: every effect is a state, so replaying the stream
// converges on the core's current standing.
//
// x360-common's accountcore keeps its gRPC framing unexported and has no Events
// call; the poll is spoken here with the same wire format (five-byte frame,
// application/grpc, grpc-status) rather than widening the shared library.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Action is what one core event does here.
type Action int

const (
	ActNone Action = iota
	ActBan
	ActUnban
	ActSuspend
	ActRestore
	ActPurge
)

// CoreEvent is one entry of the core's invalidation stream.
type CoreEvent struct {
	Version   uint64
	ID        string
	Type      string
	AccountID string
	Namespace string
	SubjectID string
}

// PlanEvent maps a core event to its action; anything not about the account's
// standing (friends, messages, other networks' links) only moves the cursor.
func PlanEvent(e CoreEvent) Action {
	if e.AccountID == "" {
		return ActNone
	}
	switch e.Type {
	case "account_banned":
		return ActBan
	case "account_unbanned":
		return ActUnban
	case "account_delete_started":
		return ActSuspend
	case "account_restored":
		return ActRestore
	case "account_deleted":
		return ActPurge
	}
	return ActNone
}

// EventSource is the core's event poll.
type EventSource interface {
	PollEvents(ctx context.Context, since uint64, limit int) ([]CoreEvent, error)
}

const eventBatch = 1000

// ProcessEvents applies one page after the store's cursor and returns how many
// events it held. A failed write stops the page; the rest is polled again.
func (h *Handler) ProcessEvents(ctx context.Context, src EventSource) (int, error) {
	evs, err := src.PollEvents(ctx, h.Store.Cursor(), eventBatch)
	if err != nil {
		return 0, err
	}
	for _, e := range evs {
		if e.ID == "" {
			return 0, errors.New("core event without id")
		}
		a := PlanEvent(e)
		applied, err := h.Store.ApplyEvent(e.Version, e.AccountID, a)
		if err != nil {
			return 0, err
		}
		if applied && a != ActNone {
			h.log().Info("account event", "type", e.Type, "version", e.Version)
		}
	}
	return len(evs), nil
}

// RunEvents polls until ctx ends: full pages back to back, otherwise every interval.
func (h *Handler) RunEvents(ctx context.Context, src EventSource, every time.Duration) {
	for ctx.Err() == nil {
		n, err := h.ProcessEvents(ctx, src)
		if err != nil {
			h.log().Warn("account events", "err", err)
		}
		if err == nil && n == eventBatch {
			continue
		}
		select {
		case <-ctx.Done():
		case <-time.After(every):
		}
	}
}

// CoreEvents polls the core's Events service at base (the same URL and internal
// key as the accountcore client).
type CoreEvents struct {
	base, token string
	http        *http.Client
}

func NewCoreEvents(base, token string) *CoreEvents {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Protocols = new(http.Protocols)
	tr.Protocols.SetHTTP2(true)
	tr.Protocols.SetUnencryptedHTTP2(true)
	return &CoreEvents{base: strings.TrimRight(base, "/"), token: token, http: &http.Client{Transport: tr}}
}

func (c *CoreEvents) PollEvents(ctx context.Context, since uint64, limit int) ([]CoreEvent, error) {
	var req []byte
	if since > 0 {
		req = binary.AppendUvarint(req, 1<<3)
		req = binary.AppendUvarint(req, since)
	}
	req = binary.AppendUvarint(req, 2<<3)
	req = binary.AppendUvarint(req, uint64(limit))
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	frame := binary.BigEndian.AppendUint32([]byte{0}, uint32(len(req)))
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/openpak.account.v1.Events/PollEvents", bytes.NewReader(append(frame, req...)))
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Content-Type", "application/grpc")
	hr.Header.Set("TE", "trailers")
	hr.Header.Set("X-Internal-Key", c.token)
	resp, err := c.http.Do(hr)
	if err != nil {
		return nil, fmt.Errorf("account core events: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("account core events: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("account core events: http status %d", resp.StatusCode)
	}
	st := resp.Trailer.Get("Grpc-Status")
	if st == "" {
		st = resp.Header.Get("Grpc-Status")
	}
	if code, err := strconv.Atoi(st); err != nil || code != 0 {
		return nil, fmt.Errorf("account core events: grpc status %q", st)
	}
	if len(body) < 5 || body[0] != 0 || int(binary.BigEndian.Uint32(body[1:5])) != len(body)-5 {
		return nil, errors.New("account core events: bad message frame")
	}
	return parsePollEvents(body[5:])
}

// parsePollEvents reads PollEventsResponse: field 1 the repeated events.
func parsePollEvents(b []byte) ([]CoreEvent, error) {
	var out []CoreEvent
	err := protoFields(b, func(field int, v uint64, s []byte) {
		if field != 1 || s == nil {
			return
		}
		var e CoreEvent
		protoFields(s, func(f int, v uint64, s []byte) {
			switch f {
			case 1:
				e.Version = v
			case 2:
				e.ID = string(s)
			case 3:
				e.Type = string(s)
			case 4:
				e.AccountID = string(s)
			case 5:
				e.Namespace = string(s)
			case 6:
				e.SubjectID = string(s)
			}
		})
		out = append(out, e)
	})
	return out, err
}

// protoFields walks a message's varint and length-delimited fields (s is nil
// for a varint) and skips fixed-width ones.
func protoFields(b []byte, fn func(field int, v uint64, s []byte)) error {
	bad := errors.New("account core events: malformed message")
	for len(b) > 0 {
		tag, n := binary.Uvarint(b)
		if n <= 0 {
			return bad
		}
		b = b[n:]
		field := int(tag >> 3)
		switch tag & 7 {
		case 0:
			v, n := binary.Uvarint(b)
			if n <= 0 {
				return bad
			}
			fn(field, v, nil)
			b = b[n:]
		case 2:
			l, n := binary.Uvarint(b)
			if n <= 0 || uint64(len(b)-n) < l {
				return bad
			}
			fn(field, 0, b[n:n+int(l)])
			b = b[n+int(l):]
		case 1:
			if len(b) < 8 {
				return bad
			}
			b = b[8:]
		case 5:
			if len(b) < 4 {
				return bad
			}
			b = b[4:]
		default:
			return bad
		}
	}
	return nil
}
