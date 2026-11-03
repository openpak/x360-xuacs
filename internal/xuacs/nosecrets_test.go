package xuacs

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogCarriesNoSecrets(t *testing.T) {
	var logs bytes.Buffer
	r := newRig(t, t.TempDir())
	r.h.Log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	for _, h := range []interface {
		ServeHTTP(http.ResponseWriter, *http.Request)
	}{r.h, r.h.WLID()} {
		req := httptest.NewRequest("POST", PathMemberExists+"?token=QUERYSECRET", strings.NewReader("BODYSECRET"))
		req.Header.Set("Authorization", "HEADERSECRET")
		req.Header.Set("Cookie", "COOKIESECRET")
		req.Header.Set("User-Agent", "UASECRET")
		req.Header.Set("Content-Type", "application/octet-stream")
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	out := logs.String()
	if !strings.Contains(out, "request") {
		t.Fatalf("request line missing: %q", out)
	}
	for _, v := range []string{"QUERYSECRET", "BODYSECRET", "HEADERSECRET", "COOKIESECRET", "UASECRET", "424f445953454352", "token="} {
		if strings.Contains(out, v) {
			t.Fatalf("%q leaked: %s", v, out)
		}
	}
}
