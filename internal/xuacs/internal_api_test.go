package xuacs

import (
	"context"
	"errors"
	"time"

	"github.com/openpak/x360-common/accountcore"
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

type graphCore struct {
	Core
	failure bool
	empty   bool
	owners  []string
}

func (c *graphCore) ListFriends(_ context.Context, a string) ([]accountcore.Friend, error) {
	c.owners = append(c.owners, a)
	if c.failure {
		return nil, errors.New("unavailable")
	}
	if c.empty || a != "owner" {
		return nil, nil
	}
	return []accountcore.Friend{{AccountID: "friend"}, {AccountID: "blocked"}, {AccountID: "hidden"}}, nil
}
func (c *graphCore) ListBlocks(_ context.Context, a string) ([]accountcore.Friend, error) {
	if c.empty || a != "owner" {
		return nil, nil
	}
	return []accountcore.Friend{{AccountID: "blocked"}}, nil
}
func (c *graphCore) CanDiscover(_ context.Context, a, b string) (bool, error) {
	return b != "hidden", nil
}
func (c *graphCore) Handle(_ context.Context, a string) (string, error) { return "Friend", nil }
func (c *graphCore) ActiveXboxLink(_ context.Context, a string) (*accountcore.Link, error) {
	x := "0009000000000002"
	if a == "blocked" {
		x = "0009000000000003"
	}
	return &accountcore.Link{AccountID: a, SubjectID: x, PublicCode: "Friend", State: accountcore.StateActive}, nil
}
func graphHandler(t *testing.T, c *graphCore) *Handler {
	t.Helper()
	s := NewMemoryStore(time.Now)
	var err error
	if err != nil {
		t.Fatal(err)
	}
	return &Handler{Core: c, Store: s}
}

const fixtureUser = "0009005f5139dadb:owner"
