package xuacs

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/openpak/x360-common/pgstate"
)

// TokenTTL is how long a 28-byte session token stays valid.
const TokenTTL = 24 * time.Hour

const (
	onlineHigh  = uint64(0x0009) << 48 // online XUID high word
	offlineHigh = uint64(0xE000) << 48 // offline XUID high word
	onlineMask  = uint64(1)<<40 - 1    // [guess] 40 account-derived low bits
	offlineMask = uint64(1)<<48 - 1
)

type consoleRec struct {
	Offline string `json:"offline"` // 16 hex digits
	Key     string `json:"key"`     // 32 hex digits, the XenonKey
	// Gamertag issued with the profile; the user AS-REQ names the profile
	// "<gamertag>@xbox.com" (spaces as '_'). Empty in records from before 0.8.
	Gamertag string `json:"gamertag,omitempty"`
}

type accountRec struct {
	Online   string                 `json:"online"` // 16 hex digits
	Consoles map[string]*consoleRec `json:"consoles,omitempty"`
}

type tokenRec struct {
	Account string    `json:"account"`
	Email   string    `json:"email"`
	Expires time.Time `json:"expires"`
}

type stateFile struct {
	Accounts map[string]*accountRec `json:"accounts"`
	Tokens   map[string]*tokenRec   `json:"tokens"`
	// Standing is every account the core's event stream has denied (events.go),
	// and EventCursor the version of the last event applied.
	Standing    map[string]*standingRec `json:"standing,omitempty"`
	EventCursor uint64                  `json:"event_cursor,omitempty"`
}

// standingRec is why an account is denied. A ban and a closure are kept apart
// so restoring a banned account keeps it banned.
type standingRec struct {
	Banned  bool `json:"banned,omitempty"`
	Closing bool `json:"closing,omitempty"`
}

// Store keeps the session tokens and the profiles xuacs issued (online XUID per
// account; offline XUID and XenonKey per account and console). It lives in
// Postgres (schema x360_xuacs, document "state"); a nil pg keeps everything in
// memory, which only tests use.
type Store struct {
	pg  *pgstate.Store
	now func() time.Time
	mu  sync.Mutex
	d   stateFile
}

const (
	// Schema is the Postgres schema xuacs owns inside the x360 database.
	Schema   = "x360_xuacs"
	stateKey = "state"
)

func emptyState() stateFile {
	return stateFile{Accounts: map[string]*accountRec{}, Tokens: map[string]*tokenRec{}}
}

// NewMemoryStore is a store that forgets on exit (tests).
func NewMemoryStore(now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{now: now, d: emptyState()}
}

// OpenStore opens the Postgres store in schema and loads the state. legacyFile,
// when the database holds no state yet and the file exists, is imported once
// (the file is left in place). An empty dsn is an error.
func OpenStore(ctx context.Context, dsn, schema, legacyFile string, now func() time.Time) (*Store, error) {
	if now == nil {
		now = time.Now
	}
	pg, err := pgstate.Open(ctx, dsn, schema)
	if err != nil {
		return nil, err
	}
	s := &Store{pg: pg, now: now, d: emptyState()}
	found, err := pg.Load(ctx, stateKey, &s.d)
	if err != nil {
		pg.Close()
		return nil, err
	}
	if !found && legacyFile != "" {
		b, err := os.ReadFile(legacyFile)
		switch {
		case err == nil:
			if err := json.Unmarshal(b, &s.d); err != nil {
				pg.Close()
				return nil, fmt.Errorf("%s: %w", legacyFile, err)
			}
			if err := s.save(); err != nil {
				pg.Close()
				return nil, err
			}
			slog.Info("imported xuacs.json into Postgres", "accounts", len(s.d.Accounts), "tokens", len(s.d.Tokens),
				"standing", len(s.d.Standing), "cursor", s.d.EventCursor)
		case !errors.Is(err, os.ErrNotExist):
			pg.Close()
			return nil, err
		}
	}
	if s.d.Accounts == nil {
		s.d.Accounts = map[string]*accountRec{}
	}
	if s.d.Tokens == nil {
		s.d.Tokens = map[string]*tokenRec{}
	}
	return s, nil
}

// Close releases the database pool.
func (s *Store) Close() {
	if s.pg != nil {
		s.pg.Close()
	}
}

// ErrDenied is an account the event stream has banned or closed.
var ErrDenied = errors.New("account suspended")

func (s *Store) save() error {
	if s.pg == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.pg.Save(ctx, stateKey, &s.d)
}

// NewToken issues a random 28-byte token bound to the account.
func (s *Store) NewToken(account, email string) ([]byte, error) {
	if s.Denied(account) {
		return nil, ErrDenied
	}
	tok := make([]byte, 28)
	if _, err := rand.Read(tok); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, t := range s.d.Tokens { // drop expired ones
		if now.After(t.Expires) {
			delete(s.d.Tokens, k)
		}
	}
	s.d.Tokens[hex.EncodeToString(tok)] = &tokenRec{Account: account, Email: email, Expires: now.Add(TokenTTL)}
	return tok, s.save()
}

// Token resolves a token to its account and sign-in email; ok is false when
// unknown or expired.
func (s *Store) Token(tok []byte) (account, email string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.d.Tokens[hex.EncodeToString(tok)]
	if t == nil || s.now().After(t.Expires) || s.d.Standing[t.Account] != nil {
		return "", "", false
	}
	return t.Account, t.Email, true
}

// OnlineXUID returns the account's stored online XUID. When there is none it
// stores preferred (if non-zero, e.g. the subject of an existing x360 link)
// or derives one: 0x0009 high word and 40 low bits from SHA-256 of the account
// id, re-hashed with a counter on a collision with another account.
func (s *Store) OnlineXUID(account string, preferred uint64) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a := s.d.Accounts[account]; a != nil && a.Online != "" {
		return parseXUID(a.Online), nil
	}
	x := preferred
	if x == 0 {
		for n := uint32(0); ; n++ {
			h := sha256.Sum256(binary.BigEndian.AppendUint32([]byte("xuacs-online|"+account), n))
			x = onlineHigh | binary.BigEndian.Uint64(h[:8])&onlineMask
			if x&onlineMask != 0 && !s.usedLocked(x) {
				break
			}
		}
	}
	a := s.d.Accounts[account]
	if a == nil {
		a = &accountRec{}
		s.d.Accounts[account] = a
	}
	a.Online = fmt.Sprintf("%016x", x)
	return x, s.save()
}

func (s *Store) usedLocked(x uint64) bool {
	want := fmt.Sprintf("%016x", x)
	for _, a := range s.d.Accounts {
		if a.Online == want {
			return true
		}
	}
	return false
}

// Profile returns the offline XUID and the 16-byte XenonKey for an account on
// a console, creating them on first use (offline: 0xE000 + 48 random bits).
func (s *Store) Profile(account, console, gamertag string) (offline uint64, key []byte, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.d.Accounts[account]
	if a == nil {
		a = &accountRec{}
		s.d.Accounts[account] = a
	}
	if a.Consoles == nil {
		a.Consoles = map[string]*consoleRec{}
	}
	c := a.Consoles[console]
	if c == nil {
		var b [8]byte
		var k [16]byte
		if _, err := rand.Read(b[:]); err != nil {
			return 0, nil, err
		}
		if _, err := rand.Read(k[:]); err != nil {
			return 0, nil, err
		}
		c = &consoleRec{
			Offline: fmt.Sprintf("%016x", offlineHigh|binary.BigEndian.Uint64(b[:])&offlineMask),
			Key:     hex.EncodeToString(k[:]),
		}
		a.Consoles[console] = c
		if err := s.save(); err != nil {
			return 0, nil, err
		}
	}
	if gamertag != "" && c.Gamertag != gamertag {
		c.Gamertag = gamertag
		if err := s.save(); err != nil {
			return 0, nil, err
		}
	}
	key, _ = hex.DecodeString(c.Key)
	return parseXUID(c.Offline), key, nil
}

func parseXUID(h string) uint64 {
	var x uint64
	fmt.Sscanf(h, "%x", &x)
	return x
}

// ConsoleProfile is a profile issued on one console.
type ConsoleProfile struct {
	Account  string
	Online   uint64
	Offline  uint64
	Key      []byte
	Gamertag string // empty for records from before 0.8
}

// ConsoleProfiles lists every profile issued on a console.
func (s *Store) ConsoleProfiles(console string) []ConsoleProfile {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ConsoleProfile
	for acct, a := range s.d.Accounts {
		c := a.Consoles[console]
		if c == nil || a.Online == "" {
			continue
		}
		k, _ := hex.DecodeString(c.Key)
		out = append(out, ConsoleProfile{Account: acct, Online: parseXUID(a.Online), Offline: parseXUID(c.Offline), Key: k, Gamertag: c.Gamertag})
	}
	return out
}

// AccountByOnlineXUID is the reverse of OnlineXUID for an already issued XUID.
func (s *Store) AccountByOnlineXUID(x uint64) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := fmt.Sprintf("%016x", x)
	for id, a := range s.d.Accounts {
		if a.Online == want {
			return id, true
		}
	}
	return "", false
}

// Denied reports whether the event stream has banned or closed the account.
func (s *Store) Denied(account string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.Standing[account] != nil
}

// Cursor is the version of the last applied core event.
func (s *Store) Cursor() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.d.EventCursor
}

// ApplyEvent applies one core event's action and moves the cursor to version,
// in one write. A version at or below the cursor is a redelivery and changes
// nothing (applied false).
//
//	Ban, Suspend  deny the account and drop its session tokens
//	Unban, Restore lift that reason (the other one stays)
//	Purge         final erasure: drop the account's profiles (online XUID,
//	              per-console offline XUIDs, XenonKeys, gamertags), its tokens and
//	              its standing, which releases the XUID and gamertag for reuse
func (s *Store) ApplyEvent(version uint64, account string, a Action) (applied bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if version <= s.d.EventCursor {
		return false, nil
	}
	if s.d.Standing == nil {
		s.d.Standing = map[string]*standingRec{}
	}
	st := s.d.Standing[account]
	if st == nil {
		st = &standingRec{}
	}
	switch a {
	case ActBan:
		st.Banned = true
	case ActUnban:
		st.Banned = false
	case ActSuspend:
		st.Closing = true
	case ActRestore:
		st.Closing = false
	case ActPurge:
		s.eraseLocked(account)
	}
	switch a {
	case ActBan, ActUnban, ActSuspend, ActRestore:
		if st.Banned || st.Closing {
			s.d.Standing[account] = st
			s.dropTokensLocked(account)
		} else {
			delete(s.d.Standing, account)
		}
	}
	s.d.EventCursor = version
	return true, s.save()
}

// eraseLocked removes everything held for the account (profile and per-console
// records, tokens, standing) and returns how many records that was.
func (s *Store) eraseLocked(account string) int {
	n := 0
	if a := s.d.Accounts[account]; a != nil {
		n += 1 + len(a.Consoles)
		delete(s.d.Accounts, account)
	}
	if s.d.Standing[account] != nil {
		n++
		delete(s.d.Standing, account)
	}
	for _, t := range s.d.Tokens {
		if t.Account == account {
			n++
		}
	}
	s.dropTokensLocked(account)
	return n
}

// Erase deletes everything held for the account and persists the change. It
// returns the number of records removed (0 on a repeat). The account's
// XUIDs are released with it.
func (s *Store) Erase(account string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.eraseLocked(account)
	if n == 0 {
		return 0, nil
	}
	return n, s.save()
}

// recordsOf counts what Erase would remove.
func (s *Store) recordsOf(account string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	if a := s.d.Accounts[account]; a != nil {
		n += 1 + len(a.Consoles)
	}
	if s.d.Standing[account] != nil {
		n++
	}
	for _, t := range s.d.Tokens {
		if t.Account == account {
			n++
		}
	}
	return n
}

// XUIDs lists the account's online and offline XUIDs without allocating any.
func (s *Store) XUIDs(account string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.d.Accounts[account]
	if a == nil {
		return nil
	}
	var out []string
	if a.Online != "" {
		out = append(out, a.Online)
	}
	for _, c := range a.Consoles {
		if c.Offline != "" {
			out = append(out, c.Offline)
		}
	}
	return out
}

func (s *Store) dropTokensLocked(account string) {
	for k, t := range s.d.Tokens {
		if t.Account == account {
			delete(s.d.Tokens, k)
		}
	}
}
