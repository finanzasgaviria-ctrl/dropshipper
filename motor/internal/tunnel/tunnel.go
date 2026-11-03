// Package tunnel opens a public address (ngrok) for the Motor's webhooks, so
// Meta and Wompi can reach a Motor that runs on the owner's own computer.
//
// Only the webhooks and the health check go through the tunnel. The panel and
// its API never do: they stay reachable from the local machine only (or with
// the access token), even while the tunnel is up.
package tunnel

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"golang.ngrok.com/ngrok/v2"
)

// States of the tunnel.
const (
	StateOff        = "apagado"
	StateConnecting = "conectando"
	StateOnline     = "conectado"
	StateError      = "error"
)

// Session is one open public endpoint.
type Session struct {
	Listener net.Listener
	URL      string // https://...
	Close    func() // releases the endpoint and the connection to ngrok
}

// Opener opens a public endpoint. The default one talks to ngrok; tests plug in
// their own.
type Opener func(ctx context.Context, token, domain string) (*Session, error)

// Info is the status shown in the panel.
type Info struct {
	Activo  bool   `json:"activo"`
	Estado  string `json:"estado"`
	URL     string `json:"url"`
	Dominio string `json:"dominio"`
	Error   string `json:"error"`
}

type config struct {
	on            bool
	token, domain string
}

// Manager keeps the tunnel up while it is enabled, reconnecting on failures.
// All methods are safe to call on a nil Manager (command-line tool).
type Manager struct {
	log   *log.Logger
	open  Opener
	retry time.Duration // first wait before retrying; doubles up to a minute

	mu       sync.Mutex
	handler  http.Handler
	cfg      config
	cancel   context.CancelFunc
	gen      int
	state    string
	url      string
	lastErr  string
	onChange func()
}

// New creates an idle manager. A nil opener means ngrok.
func New(lg *log.Logger, open Opener) *Manager {
	if lg == nil {
		lg = log.New(os.Stderr, "", log.LstdFlags)
	}
	if open == nil {
		open = ngrokOpen
	}
	return &Manager{log: lg, open: open, retry: 3 * time.Second, state: StateOff}
}

// SetHandler sets what answers the requests that come through the tunnel (the
// Motor's whole handler: only the webhook paths are let through to it).
func (m *Manager) SetHandler(h http.Handler) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.handler = h
	m.mu.Unlock()
}

// OnChange registers a callback run whenever the status changes.
func (m *Manager) OnChange(fn func()) {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.onChange = fn
	m.mu.Unlock()
}

// Configure turns the tunnel on or off. It is idempotent: the hub calls it on
// every settings change and nothing restarts unless the token or domain moved.
func (m *Manager) Configure(on bool, token, domain string) {
	if m == nil {
		return
	}
	next := config{on: on && token != "", token: token, domain: domain}
	m.mu.Lock()
	if next == m.cfg {
		m.mu.Unlock()
		return
	}
	m.cfg = next
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.gen++
	gen := m.gen
	m.url, m.lastErr = "", ""
	if !next.on {
		m.state = StateOff
		m.mu.Unlock()
		m.changed()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.state = StateConnecting
	m.mu.Unlock()
	m.changed()
	go m.run(ctx, gen, next)
}

// Restart reconnects with the same settings (the "Reintentar" button), for
// instance after the owner closed the ngrok that was holding the address.
func (m *Manager) Restart() {
	if m == nil {
		return
	}
	m.mu.Lock()
	c := m.cfg
	m.cfg = config{}
	m.mu.Unlock()
	if c.on {
		m.Configure(true, c.token, c.domain)
	}
}

// Close shuts the tunnel down.
func (m *Manager) Close() {
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.cfg = config{}
	m.gen++
	m.state, m.url = StateOff, ""
	m.mu.Unlock()
}

// URL is the public address while the tunnel is online, "" otherwise.
func (m *Manager) URL() string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != StateOnline {
		return ""
	}
	return m.url
}

// Info returns the current status.
func (m *Manager) Info() Info {
	if m == nil {
		return Info{Estado: StateOff}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return Info{Activo: m.cfg.on, Estado: m.state, URL: m.url, Dominio: m.cfg.domain, Error: m.lastErr}
}

func (m *Manager) changed() {
	m.mu.Lock()
	fn := m.onChange
	m.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// set records a new status unless a newer configuration replaced this run.
func (m *Manager) set(gen int, state, url, errMsg string) bool {
	m.mu.Lock()
	if gen != m.gen {
		m.mu.Unlock()
		return false
	}
	m.state, m.url, m.lastErr = state, url, errMsg
	m.mu.Unlock()
	m.changed()
	return true
}

func sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func (m *Manager) run(ctx context.Context, gen int, cfg config) {
	wait := m.retry
	for ctx.Err() == nil {
		octx, cancel := context.WithTimeout(ctx, 30*time.Second)
		sess, err := m.open(octx, cfg.token, cfg.domain)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			msg := Advice(err)
			if !m.set(gen, StateError, "", msg) {
				return
			}
			m.log.Printf("túnel ngrok: %s", msg)
			if fatal(err) {
				return // a wrong token will not fix itself: wait for new settings
			}
			sleep(ctx, wait)
			if wait *= 2; wait > time.Minute {
				wait = time.Minute
			}
			continue
		}
		wait = m.retry
		if !m.set(gen, StateOnline, sess.URL, "") {
			sess.Close()
			return
		}
		m.log.Printf("túnel ngrok activo: %s (webhook de WhatsApp: %s/webhook/whatsapp)", sess.URL, sess.URL)

		srv := &http.Server{Handler: webhooksOnly(m.handlerFunc()), ReadHeaderTimeout: 10 * time.Second}
		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				_ = srv.Close()
			case <-done:
			}
		}()
		serr := srv.Serve(sess.Listener)
		close(done)
		sess.Close()
		if ctx.Err() != nil {
			return
		}
		msg := "Se perdió la conexión con ngrok; reconectando..."
		if serr != nil && !errors.Is(serr, http.ErrServerClosed) {
			msg += " (" + serr.Error() + ")"
		}
		if !m.set(gen, StateConnecting, "", msg) {
			return
		}
		sleep(ctx, wait)
	}
}

// handlerFunc reads the handler at request time: it may be set after the
// tunnel starts (settings are applied before the HTTP server is built).
func (m *Manager) handlerFunc() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		h := m.handler
		m.mu.Unlock()
		if h == nil {
			http.Error(w, "el Motor aún está arrancando", http.StatusServiceUnavailable)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// webhooksOnly lets through the health check and the webhooks, nothing else.
// The panel, its API and the media files are never reachable from the tunnel.
func webhooksOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if path.Clean(p) != p { // no "..", "//" or other tricks
			http.NotFound(w, r)
			return
		}
		if p == "/salud" || strings.HasPrefix(p, "/webhook/") {
			next.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})
}

// ---------- ngrok ----------

func ngrokOpen(ctx context.Context, token, domain string) (*Session, error) {
	ag, err := ngrok.NewAgent(
		ngrok.WithAuthtoken(token),
		ngrok.WithClientInfo("dropshipper-motor", "0.3"),
	)
	if err != nil {
		return nil, err
	}
	if err := ag.Connect(ctx); err != nil {
		return nil, err
	}
	opts := []ngrok.EndpointOption{ngrok.WithDescription("Dropshipper Motor (webhooks)")}
	if domain != "" {
		opts = append(opts, ngrok.WithURL("https://"+domain))
	}
	ln, err := ag.Listen(ctx, opts...)
	if err != nil {
		_ = ag.Disconnect()
		return nil, err
	}
	return &Session{
		Listener: ln,
		URL:      strings.TrimRight(ln.URL().String(), "/"),
		Close:    func() { _ = ln.Close(); _ = ag.Disconnect() },
	}, nil
}

// coded matches ngrok's own errors, which carry a code like ERR_NGROK_108.
type coded interface{ Code() string }

func code(err error) string {
	var c coded
	if errors.As(err, &c) {
		return strings.TrimPrefix(c.Code(), "ERR_NGROK_")
	}
	return ""
}

// fatal errors do not get retried: the owner has to fix the token first.
func fatal(err error) bool {
	switch code(err) {
	case "105", "106", "107":
		return true
	}
	return false
}

// Advice turns an ngrok error into something the owner can act on.
func Advice(err error) string {
	c := code(err)
	switch c {
	case "105", "106", "107":
		return "ngrok no aceptó el Authtoken. Cópialo otra vez desde dashboard.ngrok.com (Your Authtoken) y pégalo completo, sin espacios."
	case "108":
		return "Tu cuenta de ngrok ya tiene otra sesión abierta (por ejemplo el ngrok que iniciaste en la terminal). Ciérralo y el Motor se conectará solo. (ERR_NGROK_108)"
	case "334", "336":
		return "Esa dirección ya está en uso por otro ngrok que sigue abierto. Ciérralo y el Motor se conectará solo. (ERR_NGROK_" + c + ")"
	}
	msg := err.Error()
	if i := strings.Index(msg, "\n"); i > 0 {
		msg = msg[:i]
	}
	if c != "" {
		return fmt.Sprintf("ngrok respondió con un error: %s (ERR_NGROK_%s). Revisa el token y el dominio.", msg, c)
	}
	return "No se pudo conectar con ngrok: " + msg
}
