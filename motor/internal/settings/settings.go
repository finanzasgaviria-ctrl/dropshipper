// Package settings holds everything the owner configures: AI providers,
// WhatsApp, Wompi, security and business rules. The database is the single
// source of truth; the panel and the terminal CLI both edit it, and a running
// Motor notices changes made by the other side (Manager.Watch).
package settings

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/michiTrader/dropshipper/motor/internal/config"
	"github.com/michiTrader/dropshipper/motor/internal/llm"
	"github.com/michiTrader/dropshipper/motor/internal/store"
)

// MinTokenLen is the shortest admin token accepted.
const MinTokenLen = 8

// Provider is one OpenAI-compatible AI endpoint.
type Provider struct {
	ID          string   `json:"id"`
	Name        string   `json:"nombre"`
	Preset      string   `json:"preset,omitempty"`
	BaseURL     string   `json:"base_url"`
	APIKey      string   `json:"api_key"`
	Model       string   `json:"modelo"`
	Temperature *float64 `json:"temperatura,omitempty"`
	NoKey       bool     `json:"sin_clave,omitempty"`
}

// Ready reports whether the provider has everything needed to be called.
func (p Provider) Ready() bool {
	return p.BaseURL != "" && p.Model != "" && (p.APIKey != "" || p.NoKey)
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// Slug makes a short id out of a name.
func Slug(s string) string {
	s = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-"), "-")
	if s == "" {
		s = "proveedor"
	}
	if len(s) > 32 {
		s = s[:32]
	}
	return s
}

// Validate normalizes and checks a provider, with owner-friendly errors.
func (p *Provider) Validate() error {
	if pr, ok := PresetByID(p.Preset); ok {
		if p.BaseURL == "" {
			p.BaseURL = pr.BaseURL
		}
		if p.Model == "" {
			p.Model = pr.Model
		}
		if pr.NoKey {
			p.NoKey = true
		}
		if p.Name == "" {
			p.Name = pr.Name
		}
	}
	p.BaseURL = llm.NormalizeBaseURL(p.BaseURL)
	if err := llm.ValidateBaseURL(p.BaseURL); err != nil {
		return err
	}
	p.APIKey = strings.TrimSpace(p.APIKey)
	p.Model = strings.TrimSpace(p.Model)
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		if u, err := url.Parse(p.BaseURL); err == nil {
			p.Name = u.Hostname()
		}
	}
	if p.ID == "" {
		p.ID = Slug(p.Name)
	}
	if p.Model == "" {
		return errors.New("falta el modelo (por ejemplo deepseek-chat); usa 'Cargar modelos' para ver los disponibles")
	}
	if p.APIKey == "" && !p.NoKey {
		return errors.New("falta la clave (API key) del proveedor")
	}
	if p.Temperature != nil && (*p.Temperature < 0 || *p.Temperature > 2) {
		return errors.New("la temperatura debe estar entre 0 y 2")
	}
	return nil
}

// IA is the list of AI providers and which one is active.
type IA struct {
	Providers []Provider `json:"proveedores"`
	ActiveID  string     `json:"activo"`
}

// Provider finds a provider by id.
func (ia IA) Provider(id string) (Provider, bool) {
	for _, p := range ia.Providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// NewID returns an unused id derived from name.
func (ia IA) NewID(name string) string {
	base := Slug(name)
	id := base
	for i := 2; ; i++ {
		if _, ok := ia.Provider(id); !ok {
			return id
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
}

// General holds public URL and the panel access token.
type General struct {
	PublicURL  string `json:"url_publica"`
	AdminToken string `json:"token_admin"`
}

// Ngrok is the optional public tunnel the Motor opens itself, so Meta and Wompi
// can reach the webhooks without the owner running ngrok by hand.
type Ngrok struct {
	Token   string `json:"token"`   // authtoken from dashboard.ngrok.com
	Domain  string `json:"dominio"` // fixed domain (recommended); empty = random one
	Enabled bool   `json:"activo"`
}

// CleanNgrokToken accepts the token alone or the whole command copied from the
// ngrok dashboard ("ngrok config add-authtoken XXXX") and returns just the token.
func CleanNgrokToken(v string) string {
	f := strings.Fields(v)
	if len(f) == 0 {
		return ""
	}
	return strings.Trim(f[len(f)-1], `"'`)
}

// CleanNgrokDomain turns "https://mi-app.ngrok-free.app/" into "mi-app.ngrok-free.app".
func CleanNgrokDomain(v string) string {
	v = strings.TrimSpace(v)
	for _, p := range []string{"https://", "http://"} {
		v = strings.TrimPrefix(v, p)
	}
	if i := strings.IndexAny(v, "/?#"); i >= 0 {
		v = v[:i]
	}
	return strings.ToLower(strings.TrimSpace(v))
}

// Settings is the complete configuration.
type Settings struct {
	IA       IA                    `json:"ia"`
	WhatsApp config.WhatsAppConfig `json:"whatsapp"`
	Wompi    config.WompiConfig    `json:"wompi"`
	Dropi    config.DropiConfig    `json:"dropi"`
	Ngrok    Ngrok                 `json:"ngrok"`
	General  General               `json:"general"`
	Negocio  config.Negocio        `json:"negocio"`
}

// Active returns the active provider (ok=false when none).
func (s Settings) Active() (Provider, bool) {
	return s.IA.Provider(s.IA.ActiveID)
}

// Validate checks cross-field rules; nil means safe to persist.
func (s *Settings) Validate() error {
	g := &s.General
	g.PublicURL = strings.TrimRight(strings.TrimSpace(g.PublicURL), "/")
	if g.PublicURL != "" {
		u, err := url.Parse(g.PublicURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("la dirección pública debe empezar por https:// (por ejemplo https://tienda.midominio.com)")
		}
	}
	g.AdminToken = strings.TrimSpace(g.AdminToken)
	if g.AdminToken != "" && len(g.AdminToken) < MinTokenLen {
		return fmt.Errorf("el token de acceso debe tener al menos %d caracteres", MinTokenLen)
	}
	seen := map[string]bool{}
	for i := range s.IA.Providers {
		p := &s.IA.Providers[i]
		if p.ID == "" {
			return errors.New("proveedor de IA sin id")
		}
		if seen[p.ID] {
			return fmt.Errorf("el proveedor %q está repetido", p.ID)
		}
		seen[p.ID] = true
	}
	if s.IA.ActiveID != "" {
		if _, ok := s.IA.Provider(s.IA.ActiveID); !ok {
			return fmt.Errorf("el proveedor activo %q no existe", s.IA.ActiveID)
		}
	}
	n := &s.Negocio
	n.ApplyDefaults()
	if n.Pagos.Metodo != "manual" && n.Pagos.Metodo != "wompi" {
		return errors.New("el método de pago debe ser 'manual' o 'wompi'")
	}
	if n.Dropi.Modo != "manual" && n.Dropi.Modo != "api" {
		return errors.New("el modo de Dropi debe ser 'manual' o 'api'")
	}
	if t := s.General.AdminToken; t != "" && (s.WhatsApp.Token == t || s.WhatsApp.VerifyToken == t || s.WhatsApp.AppSecret == t) {
		return errors.New("el dato de WhatsApp es igual al token de acceso del panel: seguramente el navegador lo rellenó solo. Pega aquí el valor que te dio Meta")
	}
	switch s.WhatsApp.Canal {
	case "", config.ChannelCloud, config.ChannelWeb:
	default:
		return errors.New("el canal de WhatsApp debe ser 'cloud' (API oficial) o 'web' (WhatsApp Web)")
	}
	ng := &s.Ngrok
	ng.Token = CleanNgrokToken(ng.Token)
	ng.Domain = CleanNgrokDomain(ng.Domain)
	if ng.Domain != "" && (!strings.Contains(ng.Domain, ".") || strings.ContainsAny(ng.Domain, " :@")) {
		return errors.New("el dominio de ngrok debe verse como mi-app.ngrok-free.app (sin https:// ni espacios)")
	}
	if t := s.General.AdminToken; t != "" && ng.Token == t {
		return errors.New("el token de ngrok es igual al token de acceso del panel: seguramente el navegador lo rellenó solo. Pega aquí el Authtoken de ngrok")
	}
	if ng.Enabled && ng.Token == "" {
		return errors.New("pega primero tu Authtoken de ngrok para activar el túnel")
	}
	s.Dropi.Token = strings.TrimSpace(s.Dropi.Token)
	s.Dropi.OriginDept = strings.TrimSpace(s.Dropi.OriginDept)
	s.Dropi.OriginCity = strings.TrimSpace(s.Dropi.OriginCity)
	ids := map[string]bool{}
	for i := range n.Transportadoras {
		t := &n.Transportadoras[i]
		if t.Nombre == "" {
			return errors.New("hay una transportadora sin nombre")
		}
		if ids[t.ID] {
			return fmt.Errorf("la transportadora %q está repetida", t.Nombre)
		}
		ids[t.ID] = true
		if t.TasaDevolucion < 0 || t.TasaDevolucion >= 1 {
			return fmt.Errorf("la tasa de devolución de %s debe estar entre 0%% y 99%%", t.Nombre)
		}
	}
	if n.Envio.MaxDias > 60 {
		return errors.New("el máximo de días de entrega no puede pasar de 60 (usa 0 para no limitar)")
	}
	if n.Cazador.MinVentas7d > 100000 || n.Cazador.CPAMinimo > 5000000 || n.Cazador.MaxAnunciantes > 100000 {
		return errors.New("las reglas del cazador tienen un valor demasiado alto; revísalas")
	}
	for _, t := range n.Transportadoras {
		if t.DiasEntrega > 60 || t.DiasLejano > 60 {
			return fmt.Errorf("los días de entrega de %s no pueden pasar de 60", t.Nombre)
		}
	}
	f := n.Finanzas
	if f.TasaDevolucion < 0 || f.TasaDevolucion >= 1 {
		return errors.New("la tasa de devolución debe estar entre 0% y 99%")
	}
	if f.TasaConfirmacion <= 0 || f.TasaConfirmacion > 1 {
		return errors.New("la tasa de confirmación debe estar entre 1% y 100%")
	}
	if f.MargenSeguridad < 0 || f.MargenSeguridad >= 1 {
		return errors.New("el margen de seguridad debe estar entre 0% y 99%")
	}
	if f.TRM <= 0 {
		return errors.New("la TRM (pesos por dólar) debe ser mayor que cero")
	}
	if n.Autonomia.AutoMaxValor < 0 {
		return errors.New("el valor máximo de auto-aprobación no puede ser negativo")
	}
	if n.Conversacion.EsperaAgruparSeg < 0 || n.Conversacion.HistorialMaximo < 4 {
		return errors.New("revisa la espera de agrupación (≥0) y el historial (≥4 mensajes)")
	}
	return nil
}

// Seed builds first-run settings from .env/negocio.yaml values.
func Seed(cfg *config.Config) Settings {
	s := Settings{
		WhatsApp: cfg.WhatsApp,
		Wompi:    cfg.Wompi,
		General:  General{PublicURL: cfg.PublicURL, AdminToken: cfg.AdminToken},
		Negocio:  cfg.Negocio,
	}
	if len(s.General.AdminToken) < MinTokenLen {
		s.General.AdminToken = ""
	}
	if cfg.LLM.APIKey != "" {
		p := Provider{Name: "DeepSeek", Preset: "deepseek", BaseURL: cfg.LLM.BaseURL, APIKey: cfg.LLM.APIKey,
			Model: cfg.LLM.Model, Temperature: cfg.LLM.Temperature}
		if !strings.Contains(cfg.LLM.BaseURL, "deepseek") {
			p.Name, p.Preset = "Proveedor de IA", "custom"
		}
		if p.Validate() == nil {
			s.IA.Providers = []Provider{p}
			s.IA.ActiveID = p.ID
		}
	}
	return s
}

// NewToken makes a random access token.
func NewToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------- manager ----------

const (
	keyIA       = "ia"
	keyWhatsApp = "whatsapp"
	keyWompi    = "wompi"
	keyDropi    = "dropi"
	keyNgrok    = "ngrok"
	keyGeneral  = "general"
	keyNegocio  = "negocio"
	keyReady    = "_seeded"
	keyMigWait  = "_mig_espera_60"
)

// Manager persists Settings and notifies listeners on every change.
type Manager struct {
	st *store.Store

	mu       sync.RWMutex
	cur      Settings
	rev      int64
	onChange []func(Settings)
	writeMu  sync.Mutex
}

// Open loads settings, seeding them from seedFn the first time.
func Open(ctx context.Context, st *store.Store, seedFn func() Settings) (*Manager, error) {
	m := &Manager{st: st}
	if _, err := st.GetSetting(ctx, keyReady); errors.Is(err, store.ErrNotFound) {
		seed := seedFn()
		if err := seed.Validate(); err != nil {
			return nil, fmt.Errorf("configuración inicial inválida: %w", err)
		}
		if err := m.persist(ctx, seed); err != nil {
			return nil, err
		}
		if err := st.SetSetting(ctx, keyReady, "1"); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if _, err := m.Reload(ctx); err != nil {
		return nil, err
	}
	// One-time: the wait to group a customer's messages is now 1 minute by
	// default (people write in separate lines). Owners can change it later.
	if _, err := st.GetSetting(ctx, keyMigWait); errors.Is(err, store.ErrNotFound) {
		if cur := m.Get(); cur.Negocio.Conversacion.EsperaAgruparSeg < config.DefaultEsperaAgruparSeg {
			_ = m.Update(ctx, func(s *Settings) error {
				s.Negocio.Conversacion.EsperaAgruparSeg = config.DefaultEsperaAgruparSeg
				return nil
			})
		}
		_ = st.SetSetting(ctx, keyMigWait, "1")
		// The marker bumped the DB revision: record it so the next Reload
		// does not report a change nobody made.
		if rev, err := st.SettingsRevision(ctx); err == nil {
			m.mu.Lock()
			m.rev = rev
			m.mu.Unlock()
		}
	}
	return m, nil
}

func (m *Manager) persist(ctx context.Context, s Settings) error {
	parts := map[string]any{keyIA: s.IA, keyWhatsApp: s.WhatsApp, keyWompi: s.Wompi, keyDropi: s.Dropi, keyNgrok: s.Ngrok, keyGeneral: s.General, keyNegocio: s.Negocio}
	for k, v := range parts {
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if err := m.st.SetSetting(ctx, k, string(raw)); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) load(ctx context.Context) (Settings, error) {
	var s Settings
	get := func(k string, dst any) error {
		raw, err := m.st.GetSetting(ctx, k)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		return json.Unmarshal([]byte(raw), dst)
	}
	s.Negocio = config.DefaultNegocio()
	for k, dst := range map[string]any{keyIA: &s.IA, keyWhatsApp: &s.WhatsApp, keyWompi: &s.Wompi, keyDropi: &s.Dropi, keyNgrok: &s.Ngrok, keyGeneral: &s.General, keyNegocio: &s.Negocio} {
		if err := get(k, dst); err != nil {
			return s, fmt.Errorf("leer configuración %q: %w", k, err)
		}
	}
	s.Negocio.ApplyDefaults()
	s.healAutofill()
	return s, nil
}

// healAutofill clears WhatsApp secrets that are identical to the panel token.
// That can only happen when a browser password manager filled the login token
// into a WhatsApp field and the form was saved: it is never a real credential.
func (s *Settings) healAutofill() {
	t := s.General.AdminToken
	if t == "" {
		return
	}
	w := &s.WhatsApp
	if w.Token == t {
		w.Token = ""
	}
	if w.VerifyToken == t {
		w.VerifyToken = ""
	}
	if w.AppSecret == t {
		w.AppSecret = ""
	}
}

// Get returns a copy of the current settings.
func (m *Manager) Get() Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cur
}

// AdminToken is the current panel token ("" = local-only access).
func (m *Manager) AdminToken() string { return m.Get().General.AdminToken }

// OnChange registers a listener called after every change (and immediately
// once with the current settings).
func (m *Manager) OnChange(fn func(Settings)) {
	m.mu.Lock()
	m.onChange = append(m.onChange, fn)
	cur := m.cur
	m.mu.Unlock()
	fn(cur)
}

func (m *Manager) notify(s Settings) {
	m.mu.RLock()
	fns := append([]func(Settings){}, m.onChange...)
	m.mu.RUnlock()
	for _, fn := range fns {
		fn(s)
	}
}

// Update applies fn to a copy, validates, persists and notifies.
func (m *Manager) Update(ctx context.Context, fn func(*Settings) error) error {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	// Start from what is in the DB so a terminal edit is never overwritten
	// with a stale in-memory copy.
	s, err := m.load(ctx)
	if err != nil {
		return err
	}
	if err := fn(&s); err != nil {
		return err
	}
	if err := s.Validate(); err != nil {
		return err
	}
	if err := m.persist(ctx, s); err != nil {
		return err
	}
	rev, _ := m.st.SettingsRevision(ctx)
	m.mu.Lock()
	m.cur, m.rev = s, rev
	m.mu.Unlock()
	m.notify(s)
	return nil
}

// Reload re-reads the database; returns true if something changed.
func (m *Manager) Reload(ctx context.Context) (bool, error) {
	rev, err := m.st.SettingsRevision(ctx)
	if err != nil {
		return false, err
	}
	m.mu.RLock()
	same := rev == m.rev && m.rev != 0
	m.mu.RUnlock()
	if same {
		return false, nil
	}
	s, err := m.load(ctx)
	if err != nil {
		return false, err
	}
	m.mu.Lock()
	m.cur, m.rev = s, rev
	m.mu.Unlock()
	m.notify(s)
	return true, nil
}

// Watch polls for changes made by another process (the CLI) until ctx ends.
func (m *Manager) Watch(ctx context.Context, every time.Duration, onReload func()) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if changed, err := m.Reload(ctx); err == nil && changed && onReload != nil {
				onReload()
			}
		}
	}
}
