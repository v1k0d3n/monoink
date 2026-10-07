package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/v1k0d3n/monoink/backend/internal/conn"
	"github.com/v1k0d3n/monoink/backend/internal/engine"
	"github.com/v1k0d3n/monoink/backend/internal/settings"
	"github.com/v1k0d3n/monoink/backend/internal/sysinfo"
)

func newServer(t *testing.T) (*Server, *engine.Engine) {
	st, err := settings.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	noBT := func() (conn.Bus, error) { return nil, errors.New("no bluetooth in tests") }
	e := engine.New(&engine.Engine{Store: st, Conn: conn.New(noBT, nil), Sys: sysinfo.New()})
	return &Server{Engine: e}, e
}

func do(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func TestSettingsPatch(t *testing.T) {
	s, e := newServer(t)
	h := s.Control()
	token := e.Store.Get().WebUI.Token

	rec := do(h, "GET", "/api/status", "")
	if rec.Code != 200 || strings.Contains(rec.Body.String(), token) {
		t.Fatalf("status leaked token or failed: %d", rec.Code)
	}

	rec = do(h, "PATCH", "/api/settings", `{"clock_24h": true, "web_ui": {"token": "attacker"}, "weather": {"place": "X"}}`)
	if rec.Code != 200 {
		t.Fatalf("patch: %d %s", rec.Code, rec.Body)
	}
	got := e.Store.Get()
	if !got.Clock24h || got.Weather.Place != "X" || got.WebUI.Token != token {
		t.Fatalf("patch result wrong: %+v", got)
	}

	rec = do(h, "PATCH", "/api/settings", `{"clock_24h": false, "rotate_minutes": "ten"}`)
	if rec.Code != 400 || !e.Store.Get().Clock24h {
		t.Fatalf("bad patch must change nothing: %d %+v", rec.Code, e.Store.Get())
	}
}

func TestShowAndPreview(t *testing.T) {
	s, _ := newServer(t)
	h := s.Control()
	if rec := do(h, "POST", "/api/show", `{"screen":"nope"}`); rec.Code != 400 {
		t.Errorf("unknown screen accepted: %d", rec.Code)
	}
	rec := do(h, "GET", "/api/preview/clock", "")
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("preview: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestProviderSocket(t *testing.T) {
	s, _ := newServer(t)
	p, c := s.Providers(), s.Control()
	card := `{"id":"demo","title":"Hello","lines":["a"]}`
	if rec := do(p, "POST", "/v1/card", card); rec.Code != 202 {
		t.Fatalf("unapproved provider: %d %s", rec.Code, rec.Body)
	}
	if rec := do(p, "GET", "/api/status", ""); rec.Code != 404 {
		t.Fatal("provider socket must not expose control API")
	}
	if rec := do(c, "POST", "/api/providers/demo/approve", ""); rec.Code != 200 {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body)
	}
	if rec := do(p, "POST", "/v1/card", card); rec.Code != 200 {
		t.Fatalf("approved provider: %d %s", rec.Code, rec.Body)
	}
	if rec := do(p, "POST", "/v1/card", `{"id":"BAD ID"}`); rec.Code != 400 {
		t.Fatalf("bad id: %d", rec.Code)
	}
}
