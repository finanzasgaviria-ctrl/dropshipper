package settings

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/michiTrader/dropshipper/motor/internal/config"
	"github.com/michiTrader/dropshipper/motor/internal/store"
)

func newCfg() *config.Config {
	n := config.Negocio{}
	n.ApplyDefaults()
	return &config.Config{
		LLM:      config.LLMConfig{BaseURL: "https://api.deepseek.com", APIKey: "sk-seed-123456", Model: "deepseek-chat"},
		Negocio:  n,
		WhatsApp: config.WhatsAppConfig{GraphVersion: "v21.0"},
	}
}

func open(t *testing.T) (*Manager, *store.Store) {
	t.Helper()
	st, err := store.OpenPath(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m, err := Open(context.Background(), st, func() Settings { return Seed(newCfg()) })
	if err != nil {
		t.Fatal(err)
	}
	return m, st
}

func TestSeedCreatesDeepSeekProvider(t *testing.T) {
	m, _ := open(t)
	p, ok := m.Get().Active()
	if !ok || p.ID != "deepseek" || p.Model != "deepseek-chat" || !p.Ready() {
		t.Fatalf("%+v ok=%v", p, ok)
	}
}

func TestSeedOnlyOnce(t *testing.T) {
	m, st := open(t)
	_ = m.Update(context.Background(), func(s *Settings) error { s.IA = IA{}; return nil })
	m2, err := Open(context.Background(), st, func() Settings { return Seed(newCfg()) })
	if err != nil {
		t.Fatal(err)
	}
	if len(m2.Get().IA.Providers) != 0 {
		t.Fatal("the seed must not run again once settings exist")
	}
}

func TestProviderValidate(t *testing.T) {
	ok := Provider{Name: "Arxi", Preset: "custom", BaseURL: "https://api.arxi.example/v1/chat/completions", Model: "arxi-1", APIKey: " k "}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	if ok.BaseURL != "https://api.arxi.example/v1" || ok.APIKey != "k" || ok.ID != "arxi" {
		t.Fatalf("%+v", ok)
	}
	cases := map[string]Provider{
		"modelo": {Preset: "custom", BaseURL: "https://x.example/v1", APIKey: "k"},
		"clave":  {Preset: "custom", BaseURL: "https://x.example/v1", Model: "m"},
		"URL":    {Preset: "custom", Model: "m", APIKey: "k"},
	}
	for want, p := range cases {
		err := p.Validate()
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want error mentioning %q, got %v", want, err)
		}
	}
	temp := 3.0
	bad := Provider{Preset: "deepseek", APIKey: "k", Temperature: &temp}
	if err := bad.Validate(); err == nil {
		t.Error("temperature 3 must be rejected")
	}
	ol := Provider{Preset: "ollama"}
	if err := ol.Validate(); err != nil || !ol.Ready() {
		t.Errorf("ollama needs no key: %v", err)
	}
}

func TestPresetFillsDefaults(t *testing.T) {
	p := Provider{Preset: "openrouter", APIKey: "k"}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.BaseURL, "openrouter.ai") || p.Model == "" || p.Name != "OpenRouter" {
		t.Fatalf("%+v", p)
	}
}

func TestNewIDIsUnique(t *testing.T) {
	ia := IA{Providers: []Provider{{ID: "arxi"}, {ID: "arxi-2"}}}
	if id := ia.NewID("Arxi"); id != "arxi-3" {
		t.Fatalf("got %s", id)
	}
}

func TestValidateRules(t *testing.T) {
	m, _ := open(t)
	ctx := context.Background()
	bad := map[string]func(*Settings){
		"token":        func(s *Settings) { s.General.AdminToken = "abc" },
		"dirección":    func(s *Settings) { s.General.PublicURL = "midominio.com" },
		"activo":       func(s *Settings) { s.IA.ActiveID = "nope" },
		"devolución":   func(s *Settings) { s.Negocio.Finanzas.TasaDevolucion = 1.5 },
		"confirmación": func(s *Settings) { s.Negocio.Finanzas.TasaConfirmacion = 1.5 },
		"TRM":          func(s *Settings) { s.Negocio.Finanzas.TRM = -1 },
		"pago":         func(s *Settings) { s.Negocio.Pagos.Metodo = "efectivo" },
	}
	for want, fn := range bad {
		err := m.Update(ctx, func(s *Settings) error { fn(s); return nil })
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: got %v", want, err)
		}
	}
	// a rejected update must leave the saved settings untouched
	if m.Get().General.AdminToken != "" || m.Get().IA.ActiveID != "deepseek" {
		t.Fatal("rejected update leaked into settings")
	}
}

func TestPublicViewNeverLeaksSecrets(t *testing.T) {
	m, _ := open(t)
	_ = m.Update(context.Background(), func(s *Settings) error {
		s.WhatsApp.Token = "EAAB-super-secret-token"
		s.WhatsApp.PhoneNumberID = "123"
		s.WhatsApp.AppSecret = "app-secret-value"
		s.Wompi.PrivateKey = "prv_test_secretkey"
		s.General.AdminToken = "admin-token-12345"
		return nil
	})
	v := m.Get().Public("http://x")
	raw := mustJSON(t, v)
	for _, secret := range []string{"EAAB-super-secret-token", "app-secret-value", "prv_test_secretkey", "admin-token-12345", "sk-seed-123456"} {
		if strings.Contains(raw, secret) {
			t.Errorf("secret %q leaked in the public view", secret)
		}
	}
	if !v.WhatsApp.Token.Set || !strings.HasSuffix(v.WhatsApp.Token.Mask, "oken") {
		t.Errorf("mask: %+v", v.WhatsApp.Token)
	}
	if v.WhatsApp.WebhookURL != "http://x/webhook/whatsapp" || v.Wompi.EventsURL != "http://x/webhook/wompi" {
		t.Errorf("webhooks: %s %s", v.WhatsApp.WebhookURL, v.Wompi.EventsURL)
	}
}

func TestMask(t *testing.T) {
	if Mask("") != "" || Mask("short") != "••••" || Mask("abcdefghijkl") != "••••ijkl" {
		t.Fatalf("%q %q %q", Mask(""), Mask("short"), Mask("abcdefghijkl"))
	}
}

func TestOnChangeAndTwoWriters(t *testing.T) {
	m, st := open(t)
	var n atomic.Int32
	m.OnChange(func(Settings) { n.Add(1) })
	ctx := context.Background()
	if err := m.Update(ctx, func(s *Settings) error { s.Negocio.Persona.Nombre = "Ana"; return nil }); err != nil {
		t.Fatal(err)
	}
	// once on registration (current settings) and once for the update
	if n.Load() != 2 {
		t.Fatalf("notified %d times", n.Load())
	}
	// a second process (the CLI) edits the same database
	cli, err := Open(ctx, st, func() Settings { return Seed(newCfg()) })
	if err != nil {
		t.Fatal(err)
	}
	if err := cli.Update(ctx, func(s *Settings) error { s.General.PublicURL = "https://tienda.example"; return nil }); err != nil {
		t.Fatal(err)
	}
	// the "server" manager then updates something else: must not erase the CLI edit
	if err := m.Update(ctx, func(s *Settings) error { s.Negocio.Ciudad = "Cali"; return nil }); err != nil {
		t.Fatal(err)
	}
	got := m.Get()
	if got.General.PublicURL != "https://tienda.example" || got.Negocio.Ciudad != "Cali" || got.Negocio.Persona.Nombre != "Ana" {
		t.Fatalf("lost update: %+v", got.General)
	}
}

func TestReloadAndWatchPickUpExternalChanges(t *testing.T) {
	m, st := open(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if changed, _ := m.Reload(ctx); changed {
		t.Fatal("nothing changed yet")
	}
	var reloaded atomic.Int32
	go m.Watch(ctx, 20*time.Millisecond, func() { reloaded.Add(1) })

	other, _ := Open(ctx, st, func() Settings { return Seed(newCfg()) })
	if err := other.Update(ctx, func(s *Settings) error { s.Negocio.Persona.Nombre = "Lucía"; return nil }); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for reloaded.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if reloaded.Load() == 0 || m.Get().Negocio.Persona.Nombre != "Lucía" {
		t.Fatalf("watch did not reload: %d %q", reloaded.Load(), m.Get().Negocio.Persona.Nombre)
	}
}

func TestNewTokenLooksRight(t *testing.T) {
	a, b := NewToken(), NewToken()
	if len(a) < MinTokenLen || a == b {
		t.Fatalf("%q %q", a, b)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestHealAutofillClearsPanelTokenInWhatsApp(t *testing.T) {
	m, _ := open(t)
	ctx := context.Background()
	tok := NewToken()
	_ = m.Update(ctx, func(s *Settings) error { s.General.AdminToken = tok; return nil })
	// Simulate a stale value written by the browser's password manager.
	raw := mustJSON(t, config.WhatsAppConfig{Token: tok, VerifyToken: tok, AppSecret: tok, PhoneNumberID: "123"})
	if err := m.st.SetSetting(ctx, keyWhatsApp, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	w := m.Get().WhatsApp
	if w.Token != "" || w.VerifyToken != "" || w.AppSecret != "" || w.PhoneNumberID != "123" {
		t.Fatalf("autofill not healed: %+v", w)
	}
}

func TestValidateRejectsWhatsAppTokenEqualToPanelToken(t *testing.T) {
	s := Seed(newCfg())
	s.General.AdminToken = NewToken()
	s.WhatsApp.Token = s.General.AdminToken
	if err := s.Validate(); err == nil {
		t.Fatal("a WhatsApp token equal to the panel token must be rejected")
	}
}

func TestWaitDefaultsToOneMinuteAndMigratesOnce(t *testing.T) {
	st, err := store.OpenPath(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	seed := func() Settings {
		s := Seed(newCfg())
		s.Negocio.Conversacion.EsperaAgruparSeg = 6
		return s
	}
	m, err := Open(ctx, st, seed)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Get().Negocio.Conversacion.EsperaAgruparSeg; got != 60 {
		t.Fatalf("old 6 s must migrate to 60, got %d", got)
	}
	// The owner lowers it: it must stay (migration runs only once).
	_ = m.Update(ctx, func(s *Settings) error { s.Negocio.Conversacion.EsperaAgruparSeg = 20; return nil })
	m2, err := Open(ctx, st, seed)
	if err != nil {
		t.Fatal(err)
	}
	if got := m2.Get().Negocio.Conversacion.EsperaAgruparSeg; got != 20 {
		t.Fatalf("owner value overwritten: %d", got)
	}
}

// A browser password manager once pasted the panel token into the WhatsApp
// token field. It is never a real credential: refuse it and heal old data.
func TestWhatsAppSecretEqualToPanelTokenIsRejectedAndHealed(t *testing.T) {
	m, st := open(t)
	ctx := context.Background()
	if err := m.Update(ctx, func(s *Settings) error { s.General.AdminToken = "token-del-panel-1234"; return nil }); err != nil {
		t.Fatal(err)
	}
	err := m.Update(ctx, func(s *Settings) error { s.WhatsApp.Token = "token-del-panel-1234"; return nil })
	if err == nil || !strings.Contains(err.Error(), "token de acceso del panel") {
		t.Fatalf("debía rechazar el token repetido, err=%v", err)
	}
	if m.Get().WhatsApp.Token != "" {
		t.Fatal("el token repetido se guardó")
	}
	// Data saved by an older version is cleaned on load.
	raw, _ := json.Marshal(config.WhatsAppConfig{Token: "token-del-panel-1234", GraphVersion: "v21.0"})
	if err := st.SetSetting(ctx, keyWhatsApp, string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if got := m.Get().WhatsApp.Token; got != "" {
		t.Fatalf("no se limpió el token heredado: %q", got)
	}
	if m.Get().Public("").WhatsApp.Token.Set {
		t.Fatal("el panel no debe mostrar un token de WhatsApp que nadie puso")
	}
}

func TestWaitBeforeAnsweringDefaultsToOneMinute(t *testing.T) {
	m, _ := open(t)
	if got := m.Get().Negocio.Conversacion.EsperaAgruparSeg; got != 60 {
		t.Fatalf("la espera por defecto es %d s, debía ser 60", got)
	}
	var n config.Negocio
	n.ApplyDefaults()
	if n.Conversacion.AgruparDuration() != time.Minute {
		t.Fatalf("AgruparDuration=%v", n.Conversacion.AgruparDuration())
	}
	// The owner can change it, and 0 (no wait) is allowed.
	if err := m.Update(context.Background(), func(s *Settings) error { s.Negocio.Conversacion.EsperaAgruparSeg = 90; return nil }); err != nil {
		t.Fatal(err)
	}
	if m.Get().Negocio.Conversacion.AgruparDuration() != 90*time.Second {
		t.Fatal("no respetó el valor configurado")
	}
}

func TestCleanNgrokToken(t *testing.T) {
	for in, want := range map[string]string{
		"2abcDEF_ghi":                          "2abcDEF_ghi",
		"  2abcDEF_ghi \n":                     "2abcDEF_ghi",
		"ngrok config add-authtoken 2abcDEF_x": "2abcDEF_x",
		`"2abcDEF_x"`:                          "2abcDEF_x",
		"":                                     "",
		"   ":                                  "",
	} {
		if got := CleanNgrokToken(in); got != want {
			t.Errorf("CleanNgrokToken(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanNgrokDomain(t *testing.T) {
	for in, want := range map[string]string{
		"mi-app.ngrok-free.app":           "mi-app.ngrok-free.app",
		"https://Mi-App.ngrok-free.app/":  "mi-app.ngrok-free.app",
		" http://x.ngrok.dev/webhook?a=1": "x.ngrok.dev",
		"":                                "",
	} {
		if got := CleanNgrokDomain(in); got != want {
			t.Errorf("CleanNgrokDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNgrokSettingsValidateAndNormalise(t *testing.T) {
	m, _ := open(t)
	ctx := context.Background()
	if err := m.Update(ctx, func(s *Settings) error {
		s.Ngrok = Ngrok{Token: "ngrok config add-authtoken 2tokenNgrok", Domain: "https://Fijo.ngrok-free.app/", Enabled: true}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	n := m.Get().Ngrok
	if n.Token != "2tokenNgrok" || n.Domain != "fijo.ngrok-free.app" || !n.Enabled {
		t.Fatalf("not normalised: %+v", n)
	}
	for name, fn := range map[string]func(*Settings){
		"activo sin token":     func(s *Settings) { s.Ngrok = Ngrok{Enabled: true} },
		"dominio invalido":     func(s *Settings) { s.Ngrok = Ngrok{Token: "t", Domain: "sin punto"} },
		"token igual al panel": func(s *Settings) { s.General.AdminToken = "panel-token-123"; s.Ngrok = Ngrok{Token: "panel-token-123"} },
	} {
		if err := m.Update(ctx, func(s *Settings) error { fn(s); return nil }); err == nil {
			t.Errorf("%s: must be rejected", name)
		}
	}
	if m.Get().Ngrok.Token != "2tokenNgrok" {
		t.Fatal("a rejected update changed the saved token")
	}
}

func TestNgrokTokenNeverInPublicView(t *testing.T) {
	m, _ := open(t)
	_ = m.Update(context.Background(), func(s *Settings) error {
		s.Ngrok = Ngrok{Token: "2SECRETngrokTOKEN99", Enabled: true}
		return nil
	})
	v := m.Get().Public("http://x")
	if strings.Contains(mustJSON(t, v), "2SECRETngrokTOKEN99") {
		t.Fatal("ngrok token leaked in the public view")
	}
	if !v.Ngrok.Token.Set || !v.Ngrok.Active {
		t.Fatalf("view: %+v", v.Ngrok)
	}
}

func TestWebhookBasePrefersTunnelThenPublicThenOrigin(t *testing.T) {
	m, _ := open(t)
	_ = m.Update(context.Background(), func(s *Settings) error { return nil })
	s := m.Get()
	if v := s.PublicWith("", "http://localhost:8080"); v.General.WebhookURL != "http://localhost:8080/webhook/whatsapp" || v.General.WebhookSource != "local" {
		t.Fatalf("local: %+v", v.General)
	}
	s.General.PublicURL = "https://mi.dominio.com"
	if v := s.PublicWith("", "http://localhost:8080"); v.General.WebhookSource != "publica" || v.Wompi.EventsURL != "https://mi.dominio.com/webhook/wompi" {
		t.Fatalf("publica: %+v", v.General)
	}
	v := s.PublicWith("https://t.ngrok-free.app/", "http://localhost:8080")
	if v.General.WebhookSource != "tunel" || v.WhatsApp.WebhookURL != "https://t.ngrok-free.app/webhook/whatsapp" || v.Wompi.EventsURL != "https://t.ngrok-free.app/webhook/wompi" {
		t.Fatalf("tunel: %+v %+v", v.General, v.WhatsApp)
	}
}
