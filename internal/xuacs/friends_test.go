package xuacs

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openpak/x360-common/accountcore"
	"github.com/openpak/x360-common/wire"
)

const aliveFixture = "01040000790000000300000025e4cd2d00000000000000000000000000000000dbda39515f00090001000a00020f0000000000000050f22cca1c25e4cd2d0000000000000000000000000000000087724b7c6958a75911cd8eb7732e816c96609395fb733814000000000000000000000000000001000000000000000000000000000000000000000000000000000000000000000000000000"
const fixtureUser = "0009005f5139dadb:owner"

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
	s, err := OpenStore(t.TempDir(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return &Handler{Core: c, Store: s}
}

func TestFriendsReplyMatchesNativeSchema(t *testing.T) {
	h := graphHandler(t, &graphCore{})
	req, _ := hex.DecodeString(aliveFixture)
	got, err := h.presenceFriends(context.Background(), req, fixtureUser)
	if err != nil {
		t.Fatal(err)
	}
	// Independently encoded with the reverse-engineered XAM 17559 XRL codec.
	want, _ := hex.DecodeString("010400002a0000000300000025e4cd2d00000000000000000000000000000000000000008a3179250100c2d5a2ac01000200000000000900070000467269656e64000300000000000900")
	if !bytes.Equal(got, want) {
		t.Fatalf("native schema mismatch\n got %x\nwant %x", got, want)
	}
	// A previous nonempty list must be cleared when the core graph becomes empty.
	h.Core = &graphCore{empty: true}
	empty, err := h.presenceFriends(context.Background(), req, fixtureUser)
	if err != nil || len(empty) != 48 || binary.LittleEndian.Uint32(empty[36:]) != 0 || binary.LittleEndian.Uint32(empty[42:]) != 0 {
		t.Fatalf("empty graph does not clear versions: %x %v", empty, err)
	}
}

func TestFriendsBindEachRequestToTicketUser(t *testing.T) {
	core := &graphCore{empty: true}
	h := graphHandler(t, core)
	req, _ := hex.DecodeString(aliveFixture)
	second := append([]byte{}, req...)
	binary.LittleEndian.PutUint64(second[32:], 0x0009000000000042)
	binary.LittleEndian.PutUint32(second[8:], 9)
	got, err := h.presenceFriends(context.Background(), append(req, second...), fixtureUser+",0009000000000042:other")
	if err != nil || len(got) != 96 || strings.Join(core.owners, ",") != "owner,other" || binary.LittleEndian.Uint32(got[56:]) != 9 {
		t.Fatalf("account switching projection: owners=%v data=%x err=%v", core.owners, got, err)
	}
	for _, header := range []string{"", "0009000000000042:other", fixtureUser + "," + fixtureUser} {
		before := len(core.owners)
		if _, err := h.presenceFriends(context.Background(), req, header); err == nil || len(core.owners) != before {
			t.Errorf("foreign/invalid user queried graph: %q", header)
		}
	}
}

func TestPresenceMalformedAndCoreFailure(t *testing.T) {
	h := graphHandler(t, &graphCore{})
	req, _ := hex.DecodeString(aliveFixture)
	for _, body := range [][]byte{nil, req[:100], append(append([]byte{}, req...), 1)} {
		if _, err := h.presenceFriends(context.Background(), body, fixtureUser); err == nil {
			t.Errorf("malformed body accepted: %x", body)
		}
	}
	h.Core = &graphCore{failure: true}
	r := httptest.NewRequest(http.MethodPost, PathXPresence, bytes.NewReader(req))
	r.Header.Set(wire.UsersHeader, fixtureUser)
	w := httptest.NewRecorder()
	h.xpresence(w, r, req)
	if w.Code != 503 {
		t.Fatalf("core failure reported as successful graph: %d", w.Code)
	}
}
