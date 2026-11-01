package xuacs

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/openpak/x360-common/xuacsapi"
)

// The identity endpoints the split-out services use, through their client.
func TestInternalIdentityAPI(t *testing.T) {
	h := graphHandler(t, &graphCore{})
	h.WLIDCA = []byte("-----BEGIN CERTIFICATE-----\n")
	srv := httptest.NewServer(h.Internal("k"))
	defer srv.Close()
	c := &xuacsapi.Client{URL: srv.URL, Key: "k"}
	ctx := context.Background()
	if x, tag, err := c.Identity(ctx, "friend"); err != nil || x != 0x0009000000000002 || tag != "Friend" {
		t.Fatalf("identity: %x %q %v", x, tag, err)
	}
	if x, err := c.OnlineXUID(ctx, "blocked"); err != nil || x != 0x0009000000000003 {
		t.Fatalf("online xuid: %x %v", x, err)
	}
	if a, ok, err := c.AccountByXUID(ctx, 0x0009000000000002); err != nil || !ok || a != "friend" {
		t.Fatalf("xuid account: %q %v %v", a, ok, err)
	}
	if _, ok, err := c.AccountByXUID(ctx, 0x0009000000000009); err != nil || ok {
		t.Fatalf("unknown xuid: %v %v", ok, err)
	}
	if ps, err := c.ConsoleProfiles(ctx, "478700668470"); err != nil || len(ps) != 0 {
		t.Fatalf("console profiles: %v %v", ps, err)
	}
	if pem, err := c.WLIDCA(ctx); err != nil || string(pem) != string(h.WLIDCA) {
		t.Fatalf("wlid ca: %q %v", pem, err)
	}
	if _, err := (&xuacsapi.Client{URL: srv.URL, Key: "wrong"}).OnlineXUID(ctx, "friend"); err == nil {
		t.Fatal("wrong key accepted")
	}
}
