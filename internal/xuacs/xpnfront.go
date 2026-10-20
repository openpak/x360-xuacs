package xuacs

import (
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
	out, err := h.presenceFriends(r.Context(), body, r.Header.Get(wire.UsersHeader))
	if err != nil {
		status := http.StatusServiceUnavailable
		var re *reqErr
		if errors.As(err, &re) {
			status = re.status
		}
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
