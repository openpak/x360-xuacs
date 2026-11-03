package xuacs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openpak/x360-common/pgstate"
)

// testOpen opens the Postgres store in a schema private to dir (the same dir
// reopens the same state, a restart). An empty dir is an in-memory store.
// Tests that need Postgres skip without X360_TEST_DATABASE_URL.
func testOpen(t *testing.T, dir string, now func() time.Time) (*Store, error) {
	t.Helper()
	if dir == "" {
		return NewMemoryStore(now), nil
	}
	dsn := os.Getenv("X360_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("X360_TEST_DATABASE_URL not set")
	}
	h := sha256.Sum256([]byte(dir))
	schema := "t_xuacs_" + hex.EncodeToString(h[:6])
	t.Cleanup(func() {
		if p, err := pgstate.Open(context.Background(), dsn, schema); err == nil {
			p.Pool().Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
			p.Close()
		}
	})
	return OpenStore(context.Background(), dsn, schema, "", now)
}

func TestOpenStoreRefusesWithoutDSN(t *testing.T) {
	if _, err := OpenStore(context.Background(), "", Schema, "", nil); err == nil {
		t.Fatal("started without a database URL")
	}
}

func TestLegacyFileImportedOnce(t *testing.T) {
	dsn := os.Getenv("X360_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("X360_TEST_DATABASE_URL not set")
	}
	file := filepath.Join(t.TempDir(), "xuacs.json")
	js := `{"accounts":{"A":{"online":"0009000000000001","consoles":{"c1":{"offline":"e000000000000001","key":"00112233445566778899aabbccddeeff","gamertag":"Ann"}}}},
"tokens":{},"standing":{"B":{"banned":true}},"event_cursor":9}`
	if err := os.WriteFile(file, []byte(js), 0o600); err != nil {
		t.Fatal(err)
	}
	schema := "t_xuacs_import"
	ctx := context.Background()
	t.Cleanup(func() {
		if p, err := pgstate.Open(ctx, dsn, schema); err == nil {
			p.Pool().Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
			p.Close()
		}
	})
	s, err := OpenStore(ctx, dsn, schema, file, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	os.Remove(file) // a second start must read the database, not the file
	s, err = OpenStore(ctx, dsn, schema, file, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Cursor() != 9 || !s.Denied("B") || len(s.ConsoleProfiles("c1")) != 1 {
		t.Fatal("legacy state was not imported")
	}
}
