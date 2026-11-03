package api

import (
	"context"
	"io"
	"log"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/michiTrader/dropshipper/motor/internal/tunnel"
)

// ngrokEnv wires a fake ngrok (a local listener) into the test environment.
func ngrokEnv(t *testing.T) (*env, *int32) {
	t.Helper()
	e := newEnv(t, "")
	var opens int32
	e.srv.Hub.Tunnel = tunnel.New(log.New(io.Discard, "", 0), func(ctx context.Context, token, domain string) (*tunnel.Session, error) {
		atomic.AddInt32(&opens, 1)
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		u := "https://abc123.ngrok-free.app"
		if domain != "" {
			u = "https://" + domain
		}
		return &tunnel.Session{Listener: ln, URL: u, Close: func() { _ = ln.Close() }}, nil
	})
	e.srv.Hub.Tunnel.SetHandler(e.h)
	t.Cleanup(e.srv.Hub.Tunnel.Close)
	e.srv.Hub.Apply(e.set.Get())
	return e, &opens
}

func waitTunnel(t *testing.T, e *env, state string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if e.srv.Hub.Tunnel.Info().Estado == state {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("tunnel never reached %q: %+v", state, e.srv.Hub.Tunnel.Info())
}

func TestNgrokOffByDefaultWebhookIsLocal(t *testing.T) {
	e := newEnv(t, "")
	st := e.do("GET", "/api/estado", nil).json(t)
	if st["tunel"].(map[string]any)["estado"] != "apagado" {
		t.Fatalf("tunel: %v", st["tunel"])
	}
	if st["whatsapp"].(map[string]any)["webhook_fuente"] != "local" {
		t.Fatalf("whatsapp: %v", st["whatsapp"])
	}
}

func TestNgrokTokenActivatesTunnelAndWebhookUsesIt(t *testing.T) {
	e, opens := ngrokEnv(t)
	r := e.do("PUT", "/api/ajustes/ngrok", map[string]any{"token": "ngrok config add-authtoken 2tokenPrueba123"})
	if r.Code != 200 {
		t.Fatalf("put: %d %s", r.Code, r.Body)
	}
	waitTunnel(t, e, "conectado")
	if atomic.LoadInt32(opens) != 1 {
		t.Fatalf("opens = %d", *opens)
	}
	cfg := e.do("GET", "/api/ajustes", nil)
	if strings.Contains(cfg.Body, "2tokenPrueba123") {
		t.Fatal("the ngrok token must never be sent back to the browser")
	}
	j := cfg.json(t)
	if j["ngrok"].(map[string]any)["activo"] != true || j["ngrok"].(map[string]any)["token"].(map[string]any)["configurada"] != true {
		t.Fatalf("ngrok view: %v", j["ngrok"])
	}
	if j["whatsapp"].(map[string]any)["webhook_url"] != "https://abc123.ngrok-free.app/webhook/whatsapp" {
		t.Fatalf("webhook_url: %v", j["whatsapp"])
	}
	if j["wompi"].(map[string]any)["events_url"] != "https://abc123.ngrok-free.app/webhook/wompi" {
		t.Fatalf("events_url: %v", j["wompi"])
	}
	if j["general"].(map[string]any)["webhook_fuente"] != "tunel" {
		t.Fatalf("fuente: %v", j["general"])
	}
	est := e.do("GET", "/api/estado", nil).json(t)
	if est["whatsapp"].(map[string]any)["webhook_fuente"] != "tunel" || est["tunel"].(map[string]any)["url"] != "https://abc123.ngrok-free.app" {
		t.Fatalf("estado: %v", est)
	}
}

func TestNgrokDomainReopensOnThatDomain(t *testing.T) {
	e, _ := ngrokEnv(t)
	e.do("PUT", "/api/ajustes/ngrok", map[string]any{"token": "2tokenPrueba123"})
	waitTunnel(t, e, "conectado")
	e.do("PUT", "/api/ajustes/ngrok", map[string]any{"dominio": "https://Mi-Tienda.ngrok-free.app/"})
	deadline := time.Now().Add(5 * time.Second)
	for e.srv.Hub.Tunnel.URL() != "https://mi-tienda.ngrok-free.app" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if e.srv.Hub.Tunnel.URL() != "https://mi-tienda.ngrok-free.app" {
		t.Fatalf("info: %+v", e.srv.Hub.Tunnel.Info())
	}
}

func TestNgrokOffAndRemove(t *testing.T) {
	e, _ := ngrokEnv(t)
	e.do("PUT", "/api/ajustes/ngrok", map[string]any{"token": "2tokenPrueba123"})
	waitTunnel(t, e, "conectado")

	// Turning it off keeps the token so it can be turned on again.
	if r := e.do("PUT", "/api/ajustes/ngrok", map[string]any{"activo": false}); r.Code != 200 {
		t.Fatalf("off: %d %s", r.Code, r.Body)
	}
	waitTunnel(t, e, "apagado")
	if e.set.Get().Ngrok.Token != "2tokenPrueba123" || e.set.Get().Ngrok.Enabled {
		t.Fatalf("saved: %+v", e.set.Get().Ngrok)
	}
	if got := e.do("GET", "/api/ajustes", nil).json(t)["whatsapp"].(map[string]any)["webhook_url"]; strings.Contains(got.(string), "ngrok") {
		t.Fatalf("webhook must stop using the tunnel: %v", got)
	}

	// Removing the token forgets everything.
	if r := e.do("DELETE", "/api/ajustes/ngrok", nil); r.Code != 200 {
		t.Fatalf("delete: %d", r.Code)
	}
	if n := e.set.Get().Ngrok; n.Token != "" || n.Enabled || n.Domain != "" {
		t.Fatalf("not removed: %+v", n)
	}
}

func TestNgrokValidation(t *testing.T) {
	e, opens := ngrokEnv(t)
	if r := e.do("PUT", "/api/ajustes/ngrok", map[string]any{"activo": true}); r.Code != 400 {
		t.Fatalf("activate without token: %d %s", r.Code, r.Body)
	}
	if r := e.do("PUT", "/api/ajustes/ngrok", map[string]any{"token": "t", "dominio": "sin punto"}); r.Code != 400 {
		t.Fatalf("bad domain: %d", r.Code)
	}
	if atomic.LoadInt32(opens) != 0 {
		t.Fatal("nothing valid was saved, nothing may open")
	}
}

func TestNgrokRetryAndStatusEndpoints(t *testing.T) {
	e, opens := ngrokEnv(t)
	if r := e.do("POST", "/api/ngrok/reintentar", map[string]any{}); r.Code != 200 {
		t.Fatalf("retry while off: %d", r.Code)
	}
	e.do("PUT", "/api/ajustes/ngrok", map[string]any{"token": "2tokenPrueba123"})
	waitTunnel(t, e, "conectado")
	if r := e.do("POST", "/api/ngrok/reintentar", map[string]any{}); r.Code != 200 {
		t.Fatalf("retry: %d", r.Code)
	}
	waitTunnel(t, e, "conectado")
	if atomic.LoadInt32(opens) != 2 {
		t.Fatalf("retry must reopen once: opens=%d", *opens)
	}
	if r := e.do("GET", "/api/ngrok/estado", nil); r.Code != 200 || r.json(t)["estado"] != "conectado" {
		t.Fatalf("estado: %d %s", r.Code, r.Body)
	}
}
