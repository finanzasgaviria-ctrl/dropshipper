package settings

import (
	"strings"

	"github.com/michiTrader/dropshipper/motor/internal/config"
)

// Mask hides a secret, keeping the last 4 characters of long ones.
func Mask(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= 8 {
		return "••••"
	}
	return "••••" + string(r[len(r)-4:])
}

// Secret is how a credential is shown to the browser: never the value.
type Secret struct {
	Set  bool   `json:"configurada"`
	Mask string `json:"mascara"`
}

// NewSecret builds the public view of a secret.
func NewSecret(v string) Secret { return Secret{Set: v != "", Mask: Mask(v)} }

// ProviderView is a provider as the panel sees it.
type ProviderView struct {
	ID          string   `json:"id"`
	Name        string   `json:"nombre"`
	Preset      string   `json:"preset"`
	BaseURL     string   `json:"base_url"`
	Key         Secret   `json:"clave"`
	Model       string   `json:"modelo"`
	Temperature *float64 `json:"temperatura,omitempty"`
	NoKey       bool     `json:"sin_clave"`
	Ready       bool     `json:"lista"`
	Active      bool     `json:"activo"`
}

// IAView is the AI section of the settings view.
type IAView struct {
	Providers []ProviderView `json:"proveedores"`
	ActiveID  string         `json:"activo"`
	Presets   []Preset       `json:"presets"`
}

// WhatsAppView is the WhatsApp section (secrets masked).
type WhatsAppView struct {
	Canal         string `json:"canal"`
	Token         Secret `json:"token"`
	PhoneNumberID string `json:"phone_number_id"`
	VerifyToken   Secret `json:"verify_token"`
	AppSecret     Secret `json:"app_secret"`
	GraphVersion  string `json:"graph_version"`
	AppID         string `json:"app_id"`
	WABAID        string `json:"waba_id"`
	Configured    bool   `json:"configurado"`
	WebhookURL    string `json:"webhook_url"`
}

// WompiView is the Wompi section (secrets masked).
type WompiView struct {
	PublicKey    string `json:"public_key"`
	PrivateKey   Secret `json:"private_key"`
	EventsSecret Secret `json:"events_secret"`
	Sandbox      bool   `json:"sandbox"`
	Configured   bool   `json:"configurado"`
	EventsURL    string `json:"events_url"`
}

// DropiView is the Dropi section (token masked).
type DropiView struct {
	Token      Secret `json:"token"`
	Sandbox    bool   `json:"sandbox"`
	OriginDept string `json:"origen_departamento"`
	OriginCity string `json:"origen_ciudad"`
	Configured bool   `json:"configurado"`
	BaseURL    string `json:"base_url"`
}

// DropiBase is the Dropi Integrations API base URL for the environment.
func DropiBase(sandbox bool) string {
	if sandbox {
		return "https://test-api.dropi.co/integrations"
	}
	return "https://api.dropi.co/integrations"
}

// NgrokView is the tunnel section (token masked).
type NgrokView struct {
	Token  Secret `json:"token"`
	Domain string `json:"dominio"`
	Active bool   `json:"activo"`
}

// GeneralView is the security/general section.
type GeneralView struct {
	PublicURL  string `json:"url_publica"`
	Token      Secret `json:"token"`
	WebhookURL string `json:"webhook_url"`
	// WebhookSource says where WebhookURL comes from: "tunel" (live ngrok),
	// "publica" (saved public address) or "local" (this computer only: Meta
	// cannot reach it).
	WebhookSource string `json:"webhook_fuente"`
}

// View is the full, secret-free settings document sent to the browser.
type View struct {
	IA       IAView       `json:"ia"`
	WhatsApp WhatsAppView `json:"whatsapp"`
	Wompi    WompiView    `json:"wompi"`
	Dropi    DropiView    `json:"dropi"`
	Ngrok    NgrokView    `json:"ngrok"`
	General  GeneralView  `json:"general"`
	Negocio  any          `json:"negocio"`
	// DiasTipicos: typical delivery days per carrier id ([normal, lejano]),
	// shown as the placeholder of the days fields.
	DiasTipicos map[string][2]int `json:"dias_tipicos"`
}

// Public builds the masked view. webhookBase is the URL to show for hooks
// when neither a tunnel nor a public URL exists (the request origin).
func (s Settings) Public(webhookBase string) View { return s.PublicWith("", webhookBase) }

// PublicWith is Public with a live tunnel address, which wins over the saved
// public URL: it is the address Meta can really reach right now.
func (s Settings) PublicWith(tunnelURL, webhookBase string) View {
	source := "tunel"
	base := strings.TrimRight(tunnelURL, "/")
	if base == "" {
		source = "publica"
		base = strings.TrimRight(s.General.PublicURL, "/")
	}
	if base == "" {
		source = "local"
		base = strings.TrimRight(webhookBase, "/")
	}
	v := View{Negocio: s.Negocio, DiasTipicos: map[string][2]int{}}
	for _, t := range s.Negocio.Transportadoras {
		a, b := config.DefaultDays(t.ID)
		v.DiasTipicos[t.ID] = [2]int{a, b}
	}
	v.IA.ActiveID = s.IA.ActiveID
	v.IA.Presets = Presets
	v.IA.Providers = []ProviderView{}
	for _, p := range s.IA.Providers {
		v.IA.Providers = append(v.IA.Providers, ProviderView{
			ID: p.ID, Name: p.Name, Preset: p.Preset, BaseURL: p.BaseURL, Key: NewSecret(p.APIKey),
			Model: p.Model, Temperature: p.Temperature, NoKey: p.NoKey, Ready: p.Ready(), Active: p.ID == s.IA.ActiveID,
		})
	}
	w := s.WhatsApp
	v.WhatsApp = WhatsAppView{Canal: w.Channel(), Token: NewSecret(w.Token), PhoneNumberID: w.PhoneNumberID, VerifyToken: NewSecret(w.VerifyToken),
		AppSecret: NewSecret(w.AppSecret), GraphVersion: w.GraphVersion, AppID: w.AppID, WABAID: w.WABAID,
		Configured: w.Enabled(), WebhookURL: base + "/webhook/whatsapp"}
	o := s.Wompi
	v.Wompi = WompiView{PublicKey: o.PublicKey, PrivateKey: NewSecret(o.PrivateKey), EventsSecret: NewSecret(o.EventsSecret),
		Sandbox: o.Sandbox, Configured: o.Enabled(), EventsURL: base + "/webhook/wompi"}
	d := s.Dropi
	v.Dropi = DropiView{Token: NewSecret(d.Token), Sandbox: d.Sandbox, OriginDept: d.OriginDept, OriginCity: d.OriginCity,
		Configured: d.Enabled(), BaseURL: DropiBase(d.Sandbox)}
	v.Ngrok = NgrokView{Token: NewSecret(s.Ngrok.Token), Domain: s.Ngrok.Domain, Active: s.Ngrok.Enabled}
	v.General = GeneralView{PublicURL: s.General.PublicURL, Token: NewSecret(s.General.AdminToken), WebhookURL: base + "/webhook/whatsapp", WebhookSource: source}
	return v
}
