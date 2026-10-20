package xuacs

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/openpak/x360-common/accountcore"
)

// socialCore projects the core graph; xuacs never owns another friend graph.
type socialCore interface {
	ListFriends(context.Context, string) ([]accountcore.Friend, error)
	ListBlocks(context.Context, string) ([]accountcore.Friend, error)
	CanDiscover(context.Context, string, string) (bool, error)
}

func (c CoreClient) ListFriends(ctx context.Context, account string) ([]accountcore.Friend, error) {
	return c.Client.ListFriends(ctx, account)
}
func (c CoreClient) ListBlocks(ctx context.Context, account string) ([]accountcore.Friend, error) {
	return c.Client.ListBlocks(ctx, account)
}
func (c CoreClient) CanDiscover(ctx context.Context, account, other string) (bool, error) {
	return c.Client.CanDiscover(ctx, account, other)
}

type friendRecord struct {
	xuid uint64
	name string
}
type friendSnapshot struct {
	friends []friendRecord
	blocks  []uint64
}

func (h *Handler) friendSnapshot(ctx context.Context, account string) (*friendSnapshot, error) {
	core, ok := h.Core.(socialCore)
	if !ok || h.Store == nil {
		return nil, errors.New("social account service unavailable")
	}
	friends, err := core.ListFriends(ctx, account)
	if err != nil {
		return nil, err
	}
	blocks, err := core.ListBlocks(ctx, account)
	if err != nil {
		return nil, err
	}
	s := &friendSnapshot{}
	blocked := make(map[string]bool)
	for _, b := range blocks {
		blocked[b.AccountID] = true
		link, err := h.Core.ActiveXboxLink(ctx, b.AccountID)
		if err != nil {
			return nil, err
		}
		var preferred uint64
		if link != nil {
			preferred, err = strconv.ParseUint(link.SubjectID, 16, 64)
			if err != nil || preferred>>48 != 9 {
				return nil, errors.New("invalid blocked account XUID")
			}
		}
		xuid, err := h.Store.OnlineXUID(b.AccountID, preferred)
		if err != nil {
			return nil, err
		}
		s.blocks = append(s.blocks, xuid)
	}
	seen := make(map[string]bool)
	for _, f := range friends {
		if f.AccountID == account || seen[f.AccountID] || blocked[f.AccountID] {
			continue
		}
		seen[f.AccountID] = true
		allowed, err := core.CanDiscover(ctx, account, f.AccountID)
		if err != nil {
			return nil, err
		}
		if !allowed {
			continue
		}
		_, xuid, name, err := h.identityOf(ctx, f.AccountID, "")
		if err != nil {
			return nil, err
		}
		// Native XAM permits at most 16 name bytes, including the terminator.
		if len(name) == 0 || len(name) > 15 || strings.IndexByte(name, 0) >= 0 {
			return nil, errors.New("invalid native friend gamertag")
		}
		for _, c := range []byte(name) {
			if c < 0x20 || c > 0x7e {
				return nil, errors.New("non-ASCII native friend gamertag")
			}
		}
		s.friends = append(s.friends, friendRecord{xuid, name})
	}
	if len(s.friends) > 100 || len(s.blocks) > 250 {
		return nil, errors.New("social graph exceeds native Xbox 360 limits")
	}
	sort.Slice(s.friends, func(i, j int) bool { return s.friends[i].xuid < s.friends[j].xuid })
	sort.Slice(s.blocks, func(i, j int) bool { return s.blocks[i] < s.blocks[j] })
	return s, nil
}

func listVersion(b []byte) uint32 {
	if len(b) == 0 {
		return 0
	}
	sum := sha256.Sum256(b)
	v := binary.LittleEndian.Uint32(sum[:4])
	if v == 0 {
		return 1
	}
	return v
}

func (s *friendSnapshot) reply(e []byte) []byte {
	var friends, blocks []byte
	for _, f := range s.friends {
		friends = binary.LittleEndian.AppendUint64(friends, f.xuid)
		friends = binary.LittleEndian.AppendUint16(friends, uint16(len(f.name)+1))
		friends = append(friends, 0) // accepted friendship; pending directions need separate schemas
		friends = append(friends, f.name...)
		friends = append(friends, 0)
	}
	for _, xuid := range s.blocks {
		blocks = binary.LittleEndian.AppendUint64(blocks, xuid)
	}
	out := append([]byte{}, e[:presenceHeader]...)
	binary.LittleEndian.PutUint32(out[4:], uint32(presenceBody+len(friends)+len(blocks)))
	out = binary.LittleEndian.AppendUint32(out, 0)
	out = binary.LittleEndian.AppendUint32(out, listVersion(friends))
	out = binary.LittleEndian.AppendUint16(out, uint16(len(s.friends)))
	out = binary.LittleEndian.AppendUint32(out, listVersion(blocks))
	out = binary.LittleEndian.AppendUint16(out, uint16(len(s.blocks)))
	out = append(out, friends...)
	return append(out, blocks...)
}

// Gateway user projections come from the authenticated tunnel's ticket. The
// gateway strips client-provided versions of these internal headers.
func ticketUsers(header string) (map[uint64]string, error) {
	users := make(map[uint64]string)
	for _, entry := range strings.Split(header, ",") {
		x, account, ok := strings.Cut(entry, ":")
		if !ok || len(x) != 16 || account == "" {
			return nil, deny(403, "missing tunnel user")
		}
		xuid, err := strconv.ParseUint(x, 16, 64)
		if err != nil || xuid>>48 != 9 || users[xuid] != "" {
			return nil, deny(403, "invalid tunnel user")
		}
		users[xuid] = account
	}
	if len(users) > 4 {
		return nil, deny(403, "too many tunnel users")
	}
	return users, nil
}

func (h *Handler) presenceFriends(ctx context.Context, body []byte, usersHeader string) ([]byte, error) {
	users, err := ticketUsers(usersHeader)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var out []byte
	count := 0
	for len(body) > 0 {
		if len(body) < presenceHeader {
			return nil, deny(400, "short presence header")
		}
		size := uint64(binary.LittleEndian.Uint32(body[4:])) + presenceHeader
		if size > uint64(len(body)) || size < presenceHeader {
			return nil, deny(400, "invalid presence length")
		}
		e := body[:int(size)]
		body = body[int(size):]
		// XRL opcode 0xF logoff: a ticket-bound XUID, no response schema.
		if binary.LittleEndian.Uint32(e) == 0x400 {
			if len(e) != 40 {
				return nil, deny(400, "invalid logoff length")
			}
			if users[binary.LittleEndian.Uint64(e[32:])] == "" {
				return nil, deny(403, "logoff user is not on tunnel ticket")
			}
			count++
			if count > 4 {
				return nil, deny(400, "too many presence entries")
			}
			continue
		}
		if len(e) < 0x99 {
			return nil, deny(400, "short presence alive")
		}
		if binary.LittleEndian.Uint32(e) != 0x401 || int(binary.LittleEndian.Uint16(e[0x28:]))+0x98 != len(e) {
			return nil, deny(400, "unsupported presence request")
		}
		account := users[binary.LittleEndian.Uint64(e[0x20:])]
		if account == "" {
			return nil, deny(403, "presence user is not on tunnel ticket")
		}
		count++
		if count > 4 {
			return nil, deny(400, "too many presence entries")
		}
		snapshot, err := h.friendSnapshot(ctx, account)
		if err != nil {
			h.log().Error("social snapshot failed", "err", err)
			return nil, deny(503, "social service unavailable")
		}
		out = append(out, snapshot.reply(e)...)
	}
	if count == 0 {
		return nil, deny(400, "empty presence request")
	}
	return out, nil
}
