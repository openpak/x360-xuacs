package xuacs

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

type ageCore struct {
	Core
	birthday string
	failure  bool
	asked    string
}

func (c *ageCore) Birthday(_ context.Context, a string) (string, error) {
	c.asked = a
	if c.failure {
		return "", errors.New("unavailable")
	}
	return c.birthday, nil
}
func TestAgePolicyBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		birthday string
		group    uint32
		fail     bool
	}{{"", 0, false}, {"2013-10-21", 0, false}, {"2013-10-20", 1, false}, {"2008-10-21", 1, false}, {"2008-10-20", 2, false}, {"2027-01-01", 0, true}, {"invalid", 0, true}} {
		got, err := ageGroupForBirthday(tc.birthday, now)
		if got != tc.group || (err != nil) != tc.fail {
			t.Errorf("%q: %d %v", tc.birthday, got, err)
		}
	}
}
func TestAgeGroupNativeReplyAndTicketBinding(t *testing.T) {
	core := &ageCore{birthday: "2000-10-20"}
	h := &Handler{Core: core, Now: func() time.Time { return time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC) }}
	req, _ := hex.DecodeString("dbda39515f0009003686c8746f000900d107feff")
	out, err := h.ageGroup(context.Background(), req, fixtureUser)
	if err != nil || hex.EncodeToString(out) != "02000000" || core.asked != "owner" {
		t.Fatalf("%x %v %s", out, err, core.asked)
	}
	core.asked = ""
	if _, err = h.ageGroup(context.Background(), req, "0009000000000001:other"); err == nil || core.asked != "" {
		t.Fatal("queried foreign account")
	}
	if _, err = h.ageGroup(context.Background(), req[:19], fixtureUser); err == nil {
		t.Fatal("accepted truncated request")
	}
	core.failure = true
	if _, err = h.ageGroup(context.Background(), req, fixtureUser); err == nil {
		t.Fatal("core error became a policy result")
	}
}
func TestNativeLogoffAcknowledgement(t *testing.T) {
	h := &Handler{}
	req, _ := hex.DecodeString("00040000080000000300000025e4cd2d00000000000000000000000000000000dbda39515f000900")
	out, err := h.presenceFriends(context.Background(), req, fixtureUser)
	if err != nil || len(out) != 0 {
		t.Fatalf("logoff reply %x %v", out, err)
	}
	if _, err = h.presenceFriends(context.Background(), req, "0009000000000001:other"); err == nil {
		t.Fatal("accepted foreign logoff")
	}
	if _, err = h.presenceFriends(context.Background(), req[:39], fixtureUser); err == nil {
		t.Fatal("accepted truncated logoff")
	}
}

func TestLogoffAfterTicketUserRemoval(t *testing.T) {
	s, err := OpenStore("", time.Now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.OnlineXUID("owner", 0x0009005f5139dadb)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Profile("owner", "478700668470", "Xbox360Test")
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{Store: s}
	req, _ := hex.DecodeString("00040000080000000300000025e4cd2d00000000000000000000000000000000dbda39515f000900")
	if err = h.consoleLogoff(req, "", "478700668470@xbox.com"); err != nil {
		t.Fatal(err)
	}
	for _, console := range []string{"", "111111111111@xbox.com", "malformed"} {
		if err = h.consoleLogoff(req, "", console); err == nil {
			t.Errorf("accepted unbound console %q", console)
		}
	}
}
