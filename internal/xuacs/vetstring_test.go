package xuacs

import (
	"encoding/hex"
	"net/http/httptest"
	"testing"
)

func TestVetStringAcceptsAll(t *testing.T) {
	h := &Handler{}
	// title FFFE07D1, flags 0, locale "en-US", two strings "hi" and "n".
	body, _ := hex.DecodeString("d107feff0000000005000200656e2d5553020068690100" + "6e")
	rec := httptest.NewRecorder()
	if !h.vetString(rec, httptest.NewRequest("POST", PathVetString, nil), body) {
		t.Fatal("not served")
	}
	if got := hex.EncodeToString(rec.Body.Bytes()); got != "02000000000000000000" {
		t.Fatalf("reply %s", got)
	}
	if h.vetString(httptest.NewRecorder(), httptest.NewRequest("POST", "/msgserver/other.ashx", nil), body) {
		t.Fatal("other path served")
	}
}
