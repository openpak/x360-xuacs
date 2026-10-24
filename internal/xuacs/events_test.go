package xuacs

import (
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func cev(v uint64, typ, acct string) CoreEvent {
	return CoreEvent{Version: v, ID: "ev-" + typ, Type: typ, AccountID: acct}
}

func TestPlanEvent(t *testing.T) {
	for typ, want := range map[string]Action{
		"account_banned": ActBan, "account_unbanned": ActUnban, "account_delete_started": ActSuspend,
		"account_restored": ActRestore, "account_deleted": ActPurge, "friend_accepted": ActNone, "link_unlinked": ActNone,
	} {
		if got := PlanEvent(cev(1, typ, "a")); got != want {
			t.Errorf("%s: %v, want %v", typ, got, want)
		}
	}
	if PlanEvent(cev(1, "account_deleted", "")) != ActNone {
		t.Fatal("no account, no action")
	}
}

func TestSuspendRestorePurgePersistAndSkipRedelivery(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	tok, _ := s.NewToken("acct-a", "a@example.org")
	other, _ := s.NewToken("acct-b", "b@example.org")
	online, _ := s.OnlineXUID("acct-a", 0)
	if _, _, err := s.Profile("acct-a", "123456789012", "Alpha Tag"); err != nil {
		t.Fatal(err)
	}

	if ok, err := s.ApplyEvent(1, "acct-a", ActSuspend); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, _, ok := s.Token(tok); ok {
		t.Fatal("a closing account's session token still resolves")
	}
	if _, err := s.NewToken("acct-a", "a@example.org"); err != ErrDenied {
		t.Fatalf("new sign-in while closing: %v", err)
	}
	if _, _, ok := s.Token(other); !ok {
		t.Fatal("another account's session was cut")
	}
	if len(s.ConsoleProfiles("123456789012")) != 1 {
		t.Fatal("suspension must keep the profile and gamertag held")
	}
	if ok, _ := s.ApplyEvent(1, "acct-a", ActRestore); ok {
		t.Fatal("a redelivered version was applied")
	}

	// The cursor and the standing survive a restart.
	s, _ = OpenStore(dir, time.Now)
	if s.Cursor() != 1 || !s.Denied("acct-a") {
		t.Fatalf("after reopen: cursor %d denied %v", s.Cursor(), s.Denied("acct-a"))
	}
	s.ApplyEvent(2, "acct-a", ActBan)
	s.ApplyEvent(3, "acct-a", ActRestore)
	if !s.Denied("acct-a") {
		t.Fatal("restore lifted a ban")
	}
	s.ApplyEvent(4, "acct-a", ActUnban)
	if s.Denied("acct-a") {
		t.Fatal("unbanned and restored, still denied")
	}
	if x, _ := s.OnlineXUID("acct-a", 0); x != online {
		t.Fatal("restore changed the online XUID")
	}

	s.ApplyEvent(5, "acct-a", ActSuspend)
	if ok, err := s.ApplyEvent(6, "acct-a", ActPurge); !ok || err != nil {
		t.Fatal(ok, err)
	}
	s, _ = OpenStore(dir, time.Now)
	if len(s.ConsoleProfiles("123456789012")) != 0 || s.Denied("acct-a") {
		t.Fatal("purge left the profile or the standing")
	}
	if _, ok := s.AccountByOnlineXUID(online); ok {
		t.Fatal("purge did not release the online XUID")
	}
	if _, _, ok := s.Token(other); !ok || s.Cursor() != 6 {
		t.Fatal("purge touched another account, or the cursor did not move")
	}
}

type fakeEvents struct{ evs []CoreEvent }

func (f *fakeEvents) PollEvents(_ context.Context, since uint64, _ int) ([]CoreEvent, error) {
	var out []CoreEvent
	for _, e := range f.evs {
		if e.Version > since {
			out = append(out, e)
		}
	}
	return out, nil
}

func TestProcessEventsAppliesInOrderOnce(t *testing.T) {
	s, _ := OpenStore("", nil)
	h := &Handler{Store: s}
	tok, _ := s.NewToken("acct-b", "b@example.org")
	src := &fakeEvents{evs: []CoreEvent{cev(7, "friend_requested", "acct-b"), cev(8, "account_banned", "acct-b")}}
	if n, err := h.ProcessEvents(context.Background(), src); n != 2 || err != nil {
		t.Fatal(n, err)
	}
	if _, _, ok := s.Token(tok); ok || s.Cursor() != 8 {
		t.Fatal("ban did not cut the session")
	}
	if n, _ := h.ProcessEvents(context.Background(), src); n != 0 {
		t.Fatalf("redelivered %d", n)
	}
}

func TestWLIDTicketRefusedForDeniedAccount(t *testing.T) {
	s, _ := OpenStore("", nil)
	h := &Handler{Store: s, WLIDKey: make([]byte, 32)}
	raw := h.sealTicket(magicTicket, WLIDTicket{Account: "acct-c", XUID: 1, Expires: time.Now().Add(time.Hour)})
	if _, err := h.VerifyWLIDTicket(raw); err != nil {
		t.Fatal(err)
	}
	s.ApplyEvent(1, "acct-c", ActSuspend)
	if _, err := h.VerifyWLIDTicket(raw); err == nil {
		t.Fatal("a suspended account's WLID ticket still verifies")
	}
}

// The poll over the wire: a cleartext HTTP/2 gRPC exchange as the core answers it.
func TestCoreEventsPoll(t *testing.T) {
	ev := func(v uint64, id, typ, acct string) []byte {
		var m []byte
		m = binary.AppendUvarint(m, 1<<3)
		m = binary.AppendUvarint(m, v)
		for i, s := range []string{id, typ, acct} {
			m = binary.AppendUvarint(m, uint64(i+2)<<3|2)
			m = binary.AppendUvarint(m, uint64(len(s)))
			m = append(m, s...)
		}
		m = binary.AppendUvarint(m, 7<<3)
		return binary.AppendUvarint(m, 1700000000)
	}
	var since uint64
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openpak.account.v1.Events/PollEvents" || r.Header.Get("X-Internal-Key") != "k" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		b, _ := io.ReadAll(r.Body)
		protoFields(b[5:], func(f int, v uint64, _ []byte) {
			if f == 1 {
				since = v
			}
		})
		var resp []byte
		for _, e := range [][]byte{ev(4, "e4", "account_banned", "acct-a"), ev(5, "e5", "account_deleted", "acct-b")} {
			resp = binary.AppendUvarint(resp, 1<<3|2)
			resp = binary.AppendUvarint(resp, uint64(len(e)))
			resp = append(resp, e...)
		}
		resp = binary.AppendUvarint(resp, 2<<3)
		resp = binary.AppendUvarint(resp, 5)
		w.Header().Set("Content-Type", "application/grpc")
		w.Header().Set("Trailer", "Grpc-Status")
		w.Write(binary.BigEndian.AppendUint32([]byte{0}, uint32(len(resp))))
		w.Write(resp)
		w.Header().Set("Grpc-Status", "0")
	}))
	srv.Config.Protocols = new(http.Protocols)
	srv.Config.Protocols.SetUnencryptedHTTP2(true)
	srv.Start()
	defer srv.Close()
	got, err := NewCoreEvents(srv.URL, "k").PollEvents(context.Background(), 3, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if since != 3 || len(got) != 2 || got[0] != (CoreEvent{Version: 4, ID: "e4", Type: "account_banned", AccountID: "acct-a"}) || got[1].Type != "account_deleted" {
		t.Fatalf("since %d, got %+v", since, got)
	}
	if _, err := NewCoreEvents(srv.URL, "wrong").PollEvents(context.Background(), 0, 10); err == nil {
		t.Fatal("a refused poll must be an error")
	}
}
