package xuacs

import (
	"encoding/binary"
	"net/http"
	"strconv"
	"strings"
)

// PathVetString is XStringVerify (XRL 0x801, Live service 2 "msgserver").
// The dashboard's Edit Profile vets Motto and Bio text with it before the
// profile-setting write; a failed request shows "Can't change your motto".
const PathVetString = "/msgserver/vetstring2.ashx"

// vetString: request LE u32 title, u32 flags, u16 localeLen (<=16),
// u16 count (<=10), locale bytes, count x {u16 len, UTF-8 bytes}.
// Reply: u16 count, count x u32 HRESULT. Every string is accepted (S_OK);
// ponytail: no word filter yet, add one here if the network needs it.
func (h *Handler) vetString(w http.ResponseWriter, r *http.Request, body []byte) bool {
	if strings.ToLower(r.URL.Path) != PathVetString {
		return false
	}
	if len(body) < 12 {
		http.Error(w, "short request", http.StatusBadRequest)
		return true
	}
	n := binary.LittleEndian.Uint16(body[10:])
	if n > 10 {
		http.Error(w, "too many strings", http.StatusBadRequest)
		return true
	}
	out := binary.LittleEndian.AppendUint16(nil, n)
	out = append(out, make([]byte, 4*int(n))...)
	h.log().Info("vetstring", "account", r.Header.Get("X-X360-Account"), "strings", n, "body_len", len(body))
	w.Header().Set("Content-Type", ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(out)))
	w.WriteHeader(http.StatusOK)
	w.Write(out)
	return true
}
