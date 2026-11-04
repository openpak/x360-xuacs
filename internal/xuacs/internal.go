package xuacs

// Internal API for x360-kdc (the user AS exchange on the secondary host):
//
//	POST /internal/v1/user-key     {"console":"<12 digits>","gamertag":"<name, '_' for spaces>"}
//	  -> {"account","online_xuid","xenon_key"}   the profile 0x63F issued on that console
//	POST /internal/v1/wlid-ticket  {"ticket":"<std base64 of the padata 0xDA ticket>"}
//	  -> {"account","online_xuid","expires"}     a WLID access ticket this service signed
//
// and for the other Xbox 360 services on openpak.network (x360-common/xuacsapi):
//
//	POST /internal/v1/identity          {"account"}      -> {"online_xuid","gamertag"}
//	POST /internal/v1/online-xuid       {"account"}      -> {"online_xuid"}
//	POST /internal/v1/account-xuids     {"account"}      -> {"xuids":[...]}         (held ones only, never allocates)
//	POST /internal/v1/xuid-account      {"online_xuid"}  -> {"account"}            (404: none)
//	POST /internal/v1/console-profiles  {"console"}      -> {"profiles":[{"account","online_xuid"}]}
//	POST /internal/v1/wlid-ca           {}               -> {"ca_pem"}
//
// Authenticated by X-Internal-Key; served on its own listener that is only
// published on the private (ZeroTier) address.

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/openpak/x360-common/accountcore"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// InternalKeyHeader carries the shared key.
const InternalKeyHeader = "X-Internal-Key"

// Internal returns the internal API handler; key must be non-empty.
func (h *Handler) Internal(key string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if key == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get(InternalKeyHeader)), []byte(key)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		var in struct {
			Console  string `json:"console"`
			Gamertag string `json:"gamertag"`
			Ticket   string `json:"ticket"`
			Account  string `json:"account"`
			Online   string `json:"online_xuid"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&in); err != nil {
			http.Error(w, "json", http.StatusBadRequest)
			return
		}
		var out any
		var status int
		switch r.URL.Path {
		case "/internal/v1/user-key":
			out, status = h.userKey(r, in.Console, in.Gamertag)
		case "/internal/v1/wlid-ticket":
			out, status = h.wlidTicket(in.Ticket)
		case "/internal/v1/identity":
			out, status = h.identityAPI(r, in.Account)
		case "/internal/v1/online-xuid":
			out, status = h.onlineXUIDAPI(r, in.Account)
		case "/internal/v1/account-xuids":
			if in.Account == "" {
				out, status = errBody{"account required"}, http.StatusBadRequest
				break
			}
			out, status = struct {
				XUIDs []string `json:"xuids"`
			}{append([]string{}, h.Store.XUIDs(in.Account)...)}, http.StatusOK
		case "/internal/v1/xuid-account":
			out, status = h.xuidAccountAPI(in.Online)
		case "/internal/v1/console-profiles":
			out, status = h.consoleProfilesAPI(in.Console)
		case "/internal/v1/wlid-ca":
			out, status = struct {
				PEM string `json:"ca_pem"`
			}{string(h.WLIDCA)}, http.StatusOK
			if len(h.WLIDCA) == 0 {
				out, status = errBody{"no WLID CA"}, http.StatusNotFound
			}
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(out)
	})
}

type errBody struct {
	Error string `json:"error"`
}

// normTag compares gamertags the way the user AS-REQ cname renders them.
func normTag(s string) string { return strings.ToLower(strings.ReplaceAll(s, " ", "_")) }

type profileOut struct {
	Account string `json:"account"`
	Online  string `json:"online_xuid"`
	Key     string `json:"xenon_key"`
}

// userKey answers the profile issued as gamertag on console. An empty gamertag
// (the 17559 user AS-REQ can carry "@xbox.com") lists every profile issued on
// that console under "profiles"; the KDC picks the one whose key opens the request.
func (h *Handler) userKey(r *http.Request, console, gamertag string) (any, int) {
	if consoleDigits(console+"@x") == "" {
		return errBody{"console required"}, http.StatusBadRequest
	}
	// A released handle must never let a deleted account's stored key answer
	// for a new owner. Verify account liveness and canonical name before exposing
	// any console profile to the KDC.
	var profiles []ConsoleProfile
	for _, p := range h.Store.ConsoleProfiles(console) {
		if h.Store.Denied(p.Account) { // banned or closed: no key for the KDC
			continue
		}
		handle, err := h.Core.Handle(r.Context(), p.Account)
		if errors.Is(err, accountcore.ErrInactive) {
			continue
		}
		if err != nil {
			return errBody{"account service unavailable"}, http.StatusServiceUnavailable
		}
		if handle != "" && p.Gamertag != handle {
			continue
		}
		profiles = append(profiles, p)
	}
	if gamertag == "" {
		var list []profileOut
		for _, p := range profiles {
			list = append(list, profileOut{p.Account, fmt.Sprintf("%016x", p.Online), hex.EncodeToString(p.Key)})
		}
		h.log().Info("internal user-key: all profiles on console", "console", console, "profiles", len(list))
		if len(list) == 0 {
			return errBody{"no profile on that console"}, http.StatusNotFound
		}
		return struct {
			Profiles []profileOut `json:"profiles"`
		}{list}, http.StatusOK
	}
	var hit *ConsoleProfile
	for i := range profiles {
		p := &profiles[i]
		tag := p.Gamertag
		if tag == "" { // record from before 0.8: the profile link's public code
			if l, err := h.Core.ActiveXboxLink(r.Context(), p.Account); err == nil && l != nil {
				tag = l.PublicCode
			}
		}
		if tag != "" && normTag(tag) == normTag(gamertag) {
			hit = p
			break
		}
	}
	log := h.log()
	if hit == nil {
		log.Info("internal user-key: no profile", "console", console, "gamertag", gamertag, "profiles", len(profiles))
		return errBody{"no profile with that gamertag on that console"}, http.StatusNotFound
	}
	log.Info("internal user-key", "console", console, "gamertag", gamertag, "account", hit.Account,
		"online", fmt.Sprintf("%016x", hit.Online))
	return profileOut{hit.Account, fmt.Sprintf("%016x", hit.Online), hex.EncodeToString(hit.Key)}, http.StatusOK
}

func (h *Handler) wlidTicket(b64 string) (any, int) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return errBody{"base64"}, http.StatusBadRequest
	}
	t, err := h.VerifyWLIDTicket(raw)
	if err != nil {
		h.log().Info("internal wlid-ticket refused", "reason", err.Error(), "len", len(raw))
		return errBody{err.Error()}, http.StatusForbidden
	}
	return struct {
		Account string `json:"account"`
		Online  string `json:"online_xuid"`
		Expires string `json:"expires"`
	}{t.Account, fmt.Sprintf("%016x", t.XUID), t.Expires.UTC().Format(time.RFC3339)}, http.StatusOK
}

func apiErr(err error) (any, int) {
	var re *reqErr
	if errors.As(err, &re) {
		return errBody{re.msg}, re.status
	}
	return errBody{"account service unavailable"}, http.StatusServiceUnavailable
}

func (h *Handler) identityAPI(r *http.Request, account string) (any, int) {
	if account == "" {
		return errBody{"account required"}, http.StatusBadRequest
	}
	_, xuid, tag, err := h.identityOf(r.Context(), account, "")
	if err != nil {
		return apiErr(err)
	}
	return struct {
		Online   string `json:"online_xuid"`
		Gamertag string `json:"gamertag"`
	}{fmt.Sprintf("%016x", xuid), tag}, http.StatusOK
}

func (h *Handler) onlineXUIDAPI(r *http.Request, account string) (any, int) {
	if account == "" {
		return errBody{"account required"}, http.StatusBadRequest
	}
	xuid, err := h.onlineXUID(r.Context(), account)
	if err != nil {
		return apiErr(err)
	}
	return struct {
		Online string `json:"online_xuid"`
	}{fmt.Sprintf("%016x", xuid)}, http.StatusOK
}

// onlineXUID is the account's online XUID, taking its active Xbox link's
// subject when it has one (allocated on first use).
func (h *Handler) onlineXUID(ctx context.Context, account string) (uint64, error) {
	link, err := h.Core.ActiveXboxLink(ctx, account)
	if err != nil {
		return 0, err
	}
	var preferred uint64
	if link != nil {
		preferred, err = strconv.ParseUint(link.SubjectID, 16, 64)
		if err != nil || preferred>>48 != 9 {
			return 0, errors.New("invalid account XUID")
		}
	}
	return h.Store.OnlineXUID(account, preferred)
}

func (h *Handler) xuidAccountAPI(online string) (any, int) {
	x, err := strconv.ParseUint(online, 16, 64)
	if err != nil {
		return errBody{"online_xuid required"}, http.StatusBadRequest
	}
	acct, ok := h.Store.AccountByOnlineXUID(x)
	if !ok {
		return errBody{"no account"}, http.StatusNotFound
	}
	return struct {
		Account string `json:"account"`
	}{acct}, http.StatusOK
}

func (h *Handler) consoleProfilesAPI(console string) (any, int) {
	if consoleDigits(console+"@x") == "" {
		return errBody{"console required"}, http.StatusBadRequest
	}
	type item struct {
		Account string `json:"account"`
		Online  string `json:"online_xuid"`
	}
	list := []item{}
	for _, p := range h.Store.ConsoleProfiles(console) {
		list = append(list, item{p.Account, fmt.Sprintf("%016x", p.Online)})
	}
	return struct {
		Profiles []item `json:"profiles"`
	}{list}, http.StatusOK
}
