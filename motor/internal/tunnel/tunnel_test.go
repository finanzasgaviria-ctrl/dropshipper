package tunnel

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func quiet() *log.Logger { return log.New(io.Discard, "", 0) }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timeout esperando: %s", what)
}

// fakeOpener hands out a local listener as if it were the ngrok endpoint.
func fakeOpener(opens *int32, url string) Opener {
	return func(ctx context.Context, token, domain string) (*Session, error) {
		atomic.AddInt32(opens, 1)
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		u := url
		if domain != "" {
			u = "https://" + domain
		}
		return &Session{Listener: ln, URL: u, Close: func() { _ = ln.Close() }}, nil
	}
}

func TestNilManagerIsSafe(t *testing.T) {
	var m *Manager
	m.Configure(true, "tok", "")
	m.SetHandler(nil)
	m.OnChange(nil)
	m.Restart()
	m.Close()
	if m.URL() != "" || m.Info().Estado != StateOff {
		t.Fatal("nil manager must be off")
	}
}

func TestOffWithoutToken(t *testing.T) {
	var opens int32
	m := New(quiet(), fakeOpener(&opens, "https://x.ngrok.app"))
	defer m.Close()
	m.Configure(true, "", "")
	time.Sleep(50 * time.Millisecond)
	if m.Info().Estado != StateOff || atomic.LoadInt32(&opens) != 0 {
		t.Fatalf("no token must not open a tunnel: %+v", m.Info())
	}
}

func TestOnlineServesOnlyWebhooks(t *testing.T) {
	var opens int32
	var addr atomic.Value
	open := func(ctx context.Context, token, domain string) (*Session, error) {
		s, err := fakeOpener(&opens, "https://demo.ngrok.app")(ctx, token, domain)
		if err == nil {
			addr.Store(s.Listener.Addr().String())
		}
		return s, err
	}
	m := New(quiet(), open)
	defer m.Close()
	m.SetHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "motor:"+r.URL.Path) }))
	changes := int32(0)
	m.OnChange(func() { atomic.AddInt32(&changes, 1) })

	m.Configure(true, "token-de-prueba", "")
	waitFor(t, "estado conectado", func() bool { return m.Info().Estado == StateOnline })
	if m.URL() != "https://demo.ngrok.app" {
		t.Fatalf("url = %q", m.URL())
	}
	if atomic.LoadInt32(&changes) == 0 {
		t.Fatal("onChange must fire")
	}

	get := func(p string) (int, string) {
		resp, err := http.Get("http://" + addr.Load().(string) + p)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	for _, p := range []string{"/webhook/whatsapp", "/webhook/wompi", "/salud"} {
		if code, body := get(p); code != 200 || body != "motor:"+p {
			t.Errorf("%s: %d %q", p, code, body)
		}
	}
	// The panel, its API, the media files and path tricks must never get through.
	for _, p := range []string{"/", "/index.html", "/api/estado", "/api/ajustes", "/media/x.jpg", "/webhook/../api/estado", "//api/estado", "/webhook"} {
		if code, body := get(p); code != 404 {
			t.Errorf("%s must be 404 through the tunnel, got %d %q", p, code, body)
		}
	}
}

func TestSameConfigDoesNotReopen(t *testing.T) {
	var opens int32
	m := New(quiet(), fakeOpener(&opens, "https://a.ngrok.app"))
	defer m.Close()
	m.Configure(true, "tok", "")
	waitFor(t, "online", func() bool { return m.Info().Estado == StateOnline })
	m.Configure(true, "tok", "")
	m.Configure(true, "tok", "")
	time.Sleep(50 * time.Millisecond)
	if n := atomic.LoadInt32(&opens); n != 1 {
		t.Fatalf("opened %d times, want 1", n)
	}
	// A new domain reopens it on that domain.
	m.Configure(true, "tok", "fijo.ngrok-free.app")
	waitFor(t, "new domain", func() bool { return m.URL() == "https://fijo.ngrok-free.app" })
	if atomic.LoadInt32(&opens) != 2 {
		t.Fatalf("opens = %d", opens)
	}
	if m.Info().Dominio != "fijo.ngrok-free.app" {
		t.Fatalf("info: %+v", m.Info())
	}
}

func TestTurnOff(t *testing.T) {
	var opens int32
	m := New(quiet(), fakeOpener(&opens, "https://a.ngrok.app"))
	defer m.Close()
	m.Configure(true, "tok", "")
	waitFor(t, "online", func() bool { return m.Info().Estado == StateOnline })
	m.Configure(false, "tok", "")
	if in := m.Info(); in.Estado != StateOff || in.URL != "" || in.Activo || m.URL() != "" {
		t.Fatalf("must be off: %+v", in)
	}
}

func TestRetriesOnTransientErrorAndShowsIt(t *testing.T) {
	var tries int32
	open := func(ctx context.Context, token, domain string) (*Session, error) {
		if atomic.AddInt32(&tries, 1) < 3 {
			return nil, errors.New("dial tcp: sin internet")
		}
		return fakeOpener(new(int32), "https://ok.ngrok.app")(ctx, token, domain)
	}
	m := New(quiet(), open)
	m.retry = 10 * time.Millisecond
	defer m.Close()
	m.Configure(true, "tok", "")
	waitFor(t, "error visible", func() bool { return m.Info().Estado == StateError })
	if m.Info().Error == "" {
		t.Fatal("the error must be explained")
	}
	waitFor(t, "recovers", func() bool { return m.Info().Estado == StateOnline })
	if m.Info().Error != "" {
		t.Fatalf("error must clear when online: %+v", m.Info())
	}
}

type codedErr struct{ code string }

func (e codedErr) Error() string { return "ngrok: " + e.code + "\nmás detalle" }
func (e codedErr) Code() string  { return e.code }

func TestBadTokenIsNotRetried(t *testing.T) {
	var tries int32
	open := func(ctx context.Context, token, domain string) (*Session, error) {
		atomic.AddInt32(&tries, 1)
		return nil, codedErr{"ERR_NGROK_105"}
	}
	m := New(quiet(), open)
	m.retry = 5 * time.Millisecond
	defer m.Close()
	m.Configure(true, "malo", "")
	waitFor(t, "error", func() bool { return m.Info().Estado == StateError })
	time.Sleep(80 * time.Millisecond)
	if n := atomic.LoadInt32(&tries); n != 1 {
		t.Fatalf("a rejected token must not be retried; tries=%d", n)
	}
	if got := m.Info().Error; got == "" || !contains(got, "Authtoken") {
		t.Fatalf("advice: %q", got)
	}
	// Restart (the "Reintentar" button) tries again.
	m.Restart()
	waitFor(t, "second try", func() bool { return atomic.LoadInt32(&tries) == 2 })
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestAdvice(t *testing.T) {
	for code, want := range map[string]string{"ERR_NGROK_105": "Authtoken", "ERR_NGROK_108": "otra sesión", "ERR_NGROK_334": "ya está en uso"} {
		if got := Advice(codedErr{code}); !contains(got, want) {
			t.Errorf("%s: %q does not mention %q", code, got, want)
		}
	}
	if got := Advice(errors.New("boom")); !contains(got, "boom") {
		t.Errorf("generic: %q", got)
	}
}

func TestWebhooksOnlyHandler(t *testing.T) {
	h := webhooksOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for p, want := range map[string]int{"/salud": 204, "/webhook/whatsapp": 204, "/": 404, "/api/x": 404, "/webhook/../api/x": 404} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "http://x"+p, nil))
		if rec.Code != want {
			t.Errorf("%s = %d, want %d", p, rec.Code, want)
		}
	}
}
