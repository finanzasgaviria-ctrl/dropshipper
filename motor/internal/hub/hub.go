// Package hub owns the live integrations (AI, WhatsApp, Wompi) and rebuilds
// them whenever the settings change, from the panel or from the terminal.
package hub

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/michiTrader/dropshipper/motor/internal/config"
	"github.com/michiTrader/dropshipper/motor/internal/dropi"
	"github.com/michiTrader/dropshipper/motor/internal/llm"
	"github.com/michiTrader/dropshipper/motor/internal/payments"
	"github.com/michiTrader/dropshipper/motor/internal/settings"
	"github.com/michiTrader/dropshipper/motor/internal/tunnel"
	"github.com/michiTrader/dropshipper/motor/internal/whatsapp"
	"github.com/michiTrader/dropshipper/motor/internal/whatsweb"
)

// TestTimeout bounds every "probar conexión" call.
const TestTimeout = 25 * time.Second

// Hub holds the integrations built from the current settings.
type Hub struct {
	AI *llm.Switch
	// Web is the WhatsApp Web session (nil in the command-line tool). The hub
	// switches it on or off according to the selected channel.
	Web *whatsweb.Manager
	// Tunnel is the ngrok public address for the webhooks (nil in the
	// command-line tool). The hub opens or closes it with the settings.
	Tunnel *tunnel.Manager

	mu    sync.RWMutex
	cur   settings.Settings
	wa    *whatsapp.Client
	wompi *payments.Wompi
	dropi *dropi.Client
}

// New creates an empty hub.
func New() *Hub { return &Hub{AI: &llm.Switch{}} }

// Apply rebuilds the integrations from settings.
func (h *Hub) Apply(s settings.Settings) {
	h.mu.Lock()
	h.cur = s
	h.wa, h.wompi, h.dropi = nil, nil, nil
	if s.WhatsApp.UsesCloud() {
		h.wa = whatsapp.New(s.WhatsApp.Token, s.WhatsApp.PhoneNumberID, s.WhatsApp.GraphVersion)
	}
	if s.Wompi.Enabled() {
		h.wompi = payments.NewWompi(s.Wompi.PrivateKey, s.Wompi.EventsSecret, s.Wompi.Sandbox)
	}
	if s.Dropi.Enabled() {
		h.dropi = dropi.New(s.Dropi.Token, s.Dropi.Sandbox)
	}
	h.mu.Unlock()
	h.Web.Configure(s.WhatsApp.UsesWeb(), nil)
	h.Tunnel.Configure(s.Ngrok.Enabled, s.Ngrok.Token, s.Ngrok.Domain)

	if p, ok := s.Active(); ok && p.Ready() {
		c := llm.NewClient(p.BaseURL, p.APIKey)
		c.AllowNoKey = p.NoKey
		h.AI.Set(c, p.Model, p.Temperature)
	} else {
		h.AI.Set(nil, "", nil)
	}
}

// PublicBase is the address the outside world reaches the webhooks at: the live
// ngrok tunnel when it is up, otherwise the saved public address (may be "").
func (h *Hub) PublicBase() (base, source string) {
	if u := h.Tunnel.URL(); u != "" {
		return u, "tunel"
	}
	if u := h.Settings().General.PublicURL; u != "" {
		return u, "publica"
	}
	return "", "local"
}

// Settings returns the settings last applied.
func (h *Hub) Settings() settings.Settings {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.cur
}

// WhatsApp returns the live Cloud API client (nil when that channel is not the
// selected one or is not configured).
func (h *Hub) WhatsApp() *whatsapp.Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.wa
}

// Wompi returns the live Wompi client (nil when not configured).
func (h *Hub) Wompi() *payments.Wompi {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.wompi
}

// Dropi returns the live Dropi client (nil when no token is set).
func (h *Hub) Dropi() *dropi.Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.dropi
}

// Channel is the selected WhatsApp channel ("cloud" or "web").
func (h *Hub) Channel() string { return h.Settings().WhatsApp.Channel() }

// Available reports whether WhatsApp sending works right now through the
// selected channel. It lets the conversation engine decide at call time.
func (h *Hub) Available() bool {
	if h.Channel() == config.ChannelWeb {
		return h.Web.Available()
	}
	return h.WhatsApp() != nil
}

// SendText sends through the selected channel.
func (h *Hub) SendText(ctx context.Context, to, text string) (string, error) {
	if h.Channel() == config.ChannelWeb {
		return h.Web.SendText(ctx, to, text)
	}
	c := h.WhatsApp()
	if c == nil {
		return "", whatsapp.ErrNotConfigured
	}
	return c.SendText(ctx, to, text)
}

// MarkRead marks a customer message as read (and shows "escribiendo...").
func (h *Hub) MarkRead(ctx context.Context, waID string, typing bool) error {
	if h.Channel() == config.ChannelWeb {
		return h.Web.MarkRead(ctx, waID, typing)
	}
	c := h.WhatsApp()
	if c == nil {
		return whatsapp.ErrNotConfigured
	}
	return c.MarkRead(ctx, waID, typing)
}

// LinksAvailable reports whether automatic payment links can be created.
func (h *Hub) LinksAvailable() bool { return h.Wompi() != nil }

// CreateLink creates a Wompi payment link with the current client.
func (h *Hub) CreateLink(ctx context.Context, reference, description string, amount int64) (string, string, error) {
	w := h.Wompi()
	if w == nil {
		return "", "", errors.New("Wompi no está configurado")
	}
	return w.CreateLink(ctx, reference, description, amount)
}
