package xuacs

import (
	"encoding/binary"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/openpak/x360-common/wire"
)

// PathXPresence is Live service 1 (xpnfront), the presence/notification
// endpoint the console's XLive worker calls right after the service-26 stream
// is up (xrl-schema-opcodes.md in the Wharf notes).
const PathXPresence = "/xpnfront/xpresence.srf"

// The native presence-alive request has a 32-byte header and one element per
// user. Replies echo its sequence/network block and carry authoritative friend
// and block list versions, counts and records from account core (friends.go).
const (
	presenceHeader = 32
	presenceBody   = 16
)

func (h *Handler) xpresence(w http.ResponseWriter, r *http.Request, body []byte) bool {
	if !strings.EqualFold(r.URL.Path, PathXPresence) {
		return false
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return true
	}
	var out []byte
	var err error
	if len(body) >= 4 && binary.LittleEndian.Uint32(body) == 0x400 {
		err = h.consoleLogoff(body, r.Header.Get(wire.UsersHeader), r.Header.Get(wire.AccountHeader))
	} else {
		out, err = h.presenceFriends(r.Context(), body, r.Header.Get(wire.UsersHeader))
	}
	if err != nil {
		status := http.StatusServiceUnavailable
		var re *reqErr
		if errors.As(err, &re) {
			status = re.status
		}
		h.log().Warn("xpresence refused", "status", status, "body_len", len(body))
		http.Error(w, http.StatusText(status), status)
		return true
	}
	h.log().Info("xpresence reply", "body_len", len(body), "reply_len", len(out))
	w.Header().Set("Content-Type", ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(out)))
	w.WriteHeader(http.StatusOK)
	w.Write(out)
	return true
}

// A replacement console-only ticket can precede P_LOGOFF. In that case validate
// the departing XUID against profiles issued to that authenticated console,
// rather than requiring it to remain on the new ticket's active-user list.
func (h *Handler) consoleLogoff(body []byte, usersHeader, consoleHeader string) error {
	users, err := ticketUsers(usersHeader)
	if err != nil {
		users = map[uint64]string{}
	}
	digits := consoleDigits(consoleHeader)
	if digits != "" && h.Store != nil {
		for _, p := range h.Store.ConsoleProfiles(digits) {
			users[p.Online] = p.Account
		}
	}
	count := 0
	for len(body) > 0 {
		if len(body) < 40 || binary.LittleEndian.Uint32(body) != 0x400 || binary.LittleEndian.Uint32(body[4:]) != 8 {
			return deny(400, "invalid native logoff")
		}
		if users[binary.LittleEndian.Uint64(body[32:])] == "" {
			return deny(403, "logoff profile is not bound to this console")
		}
		body = body[40:]
		count++
		if count > 4 {
			return deny(400, "too many logoff entries")
		}
	}
	return nil
}
