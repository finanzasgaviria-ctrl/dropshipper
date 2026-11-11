// Package config loads the Motor configuration.
//
// Secrets (API keys, tokens) come ONLY from environment variables (or a .env
// file next to the binary). Business settings (persona, payment methods, auto
// approval rules) live in negocio.yaml so they can be edited without touching
// code and without leaking secrets into git.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the full runtime configuration.
type Config struct {
	Port         int
	DataDir      string
	KnowledgeDir string
	AdminToken   string
	PublicURL    string

	LLM      LLMConfig
	WhatsApp WhatsAppConfig
	Wompi    WompiConfig
	Negocio  Negocio
}

// LLMConfig points to any OpenAI-compatible chat completions endpoint.
// DeepSeek is the default: https://api.deepseek.com with model deepseek-chat.
type LLMConfig struct {
	BaseURL string
	APIKey  string
	Model   string
	// Temperature nil = do not send it (some models only accept the default).
	// Set LLM_TEMPERATURE=omit to leave it out.
	Temperature *float64
}

// Channels the Motor can use to talk to customers on WhatsApp.
const (
	ChannelCloud = "cloud" // official WhatsApp Business Cloud API (Meta)
	ChannelWeb   = "web"   // WhatsApp Web: a phone linked with a QR code (unofficial)
)

// WhatsAppConfig holds the WhatsApp settings: which channel is used and the
// Cloud API credentials (kept even while the Web channel is selected).
type WhatsAppConfig struct {
	Canal         string `json:"canal"`
	Token         string `json:"token"`
	PhoneNumberID string `json:"phone_number_id"`
	VerifyToken   string `json:"verify_token"`
	AppSecret     string `json:"app_secret"`
	GraphVersion  string `json:"graph_version"`
}

// Channel is the selected channel; anything but "web" means the Cloud API.
func (w WhatsAppConfig) Channel() string {
	if w.Canal == ChannelWeb {
		return ChannelWeb
	}
	return ChannelCloud
}

// UsesWeb reports whether the WhatsApp Web channel is selected.
func (w WhatsAppConfig) UsesWeb() bool { return w.Channel() == ChannelWeb }

// Enabled reports whether the Cloud API credentials are complete.
func (w WhatsAppConfig) Enabled() bool { return w.Token != "" && w.PhoneNumberID != "" }

// UsesCloud reports whether the Cloud API is the selected channel and ready.
func (w WhatsAppConfig) UsesCloud() bool { return !w.UsesWeb() && w.Enabled() }

// WompiConfig holds Wompi (Bancolombia) credentials for automatic payment links.
type WompiConfig struct {
	PublicKey    string `json:"public_key"`
	PrivateKey   string `json:"private_key"`
	EventsSecret string `json:"events_secret"`
	Sandbox      bool   `json:"sandbox"`
}

// Enabled reports whether automatic Wompi payment links can be created.
func (w WompiConfig) Enabled() bool { return w.PrivateKey != "" }

// Negocio is the editable business configuration (negocio.yaml).
type Negocio struct {
	NombreTienda string       `yaml:"nombre_tienda" json:"nombre_tienda"`
	Ciudad       string       `yaml:"ciudad" json:"ciudad"`
	Persona      Persona      `yaml:"persona" json:"persona"`
	Pagos        Pagos        `yaml:"pagos" json:"pagos"`
	Autonomia    Autonomia    `yaml:"autonomia" json:"autonomia"`
	Conversacion Conversacion `yaml:"conversacion" json:"conversacion"`
	Finanzas     FinanzasConf `yaml:"finanzas" json:"finanzas"`
	Dropi        DropiConf    `yaml:"dropi" json:"dropi"`
	// Envio: how the carrier is recommended (speed vs. profit).
	Envio EnvioConf `yaml:"envio" json:"envio"`
	// Cazador: the rules used to judge candidate products.
	Cazador CazadorConf `yaml:"cazador" json:"cazador"`
	// Transportadoras: the carriers the owner works with. Notes feed the AI
	// system prompt; the return rate and priority drive the carrier choice.
	Transportadoras []Transportadora `yaml:"transportadoras" json:"transportadoras"`
	Notificar       NotificarConf    `yaml:"notificar" json:"notificar"`
}

// Persona describes how the seller agent presents itself.
type Persona struct {
	Nombre string `yaml:"nombre" json:"nombre"`
	Rol    string `yaml:"rol" json:"rol"`
	Tono   string `yaml:"tono" json:"tono"`
}

// Pagos configures how advance payments are collected.
type Pagos struct {
	// Metodo: "manual" (llave Bre-B / cuenta Bancolombia + comprobante) or
	// "wompi" (link de pago con confirmación automática).
	Metodo string     `yaml:"metodo" json:"metodo"`
	Manual PagoManual `yaml:"manual" json:"manual"`
	// HorasParaPagar: after this, an unpaid order is flagged for follow-up.
	HorasParaPagar int `yaml:"horas_para_pagar" json:"horas_para_pagar"`
}

// PagoManual are the payment instructions shown to the customer.
type PagoManual struct {
	LlaveBreB     string `yaml:"llave_bre_b" json:"llave_bre_b"`
	Banco         string `yaml:"banco" json:"banco"`
	TipoCuenta    string `yaml:"tipo_cuenta" json:"tipo_cuenta"`
	NumeroCuenta  string `yaml:"numero_cuenta" json:"numero_cuenta"`
	Titular       string `yaml:"titular" json:"titular"`
	Instrucciones string `yaml:"instrucciones" json:"instrucciones"`
}

// Autonomia controls what the AI may do without the owner pressing "Aceptar".
type Autonomia struct {
	// AutoAprobarPedidos: level 2. Off by default: every order waits for the owner.
	AutoAprobarPedidos bool  `yaml:"auto_aprobar_pedidos" json:"auto_aprobar_pedidos"`
	AutoMaxValor       int64 `yaml:"auto_max_valor" json:"auto_max_valor"`
	AutoSoloCOD        bool  `yaml:"auto_solo_contraentrega" json:"auto_solo_contraentrega"`
	AutoSoloRiesgoBajo bool  `yaml:"auto_solo_riesgo_normal" json:"auto_solo_riesgo_normal"`
}

// DefaultEsperaAgruparSeg: customers write in separate lines, so the seller
// waits one minute after the last message before answering.
const DefaultEsperaAgruparSeg = 60

// Conversacion tunes the chat behaviour.
type Conversacion struct {
	// EsperaAgruparSeg: customers often send several short messages in a row;
	// the agent waits this long after the last one before answering.
	EsperaAgruparSeg int `yaml:"espera_agrupar_seg" json:"espera_agrupar_seg"`
	// PausaEntreMensajesMs: delay between the bubbles of one answer (feels human).
	PausaEntreMensajesMs int `yaml:"pausa_entre_mensajes_ms" json:"pausa_entre_mensajes_ms"`
	HistorialMaximo      int `yaml:"historial_maximo" json:"historial_maximo"`
}

// FinanzasConf holds the default economic assumptions.
type FinanzasConf struct {
	TasaDevolucion   float64 `yaml:"tasa_devolucion" json:"tasa_devolucion"`
	TRM              float64 `yaml:"trm_cop_usd" json:"trm_cop_usd"`
	MargenSeguridad  float64 `yaml:"margen_seguridad" json:"margen_seguridad"`
	TasaConfirmacion float64 `yaml:"tasa_confirmacion" json:"tasa_confirmacion"`
}

// Carrier selection criteria.
const (
	CriterioRentable = "rentable" // best expected profit (default)
	CriterioRapido   = "rapido"   // fastest delivery
	CriterioBarato   = "barato"   // lowest freight
)

// EnvioConf tunes the carrier recommendation.
type EnvioConf struct {
	// MaxDias: carriers slower than this are never recommended (0 = no limit).
	MaxDias int `yaml:"max_dias" json:"max_dias"`
	// Criterio: rentable | rapido | barato, applied among the carriers that
	// arrive within MaxDias.
	Criterio string `yaml:"criterio" json:"criterio"`
}

// CazadorConf holds the thresholds used to judge a candidate product. They
// are rules of thumb the owner can change from the panel.
type CazadorConf struct {
	// MinVentas7d: fewer sales than this in 7 days = weak demand (default 10).
	MinVentas7d int `yaml:"min_ventas_7d" json:"min_ventas_7d"`
	// CPAMinimo: after costs, returns and the safety margin, the product must
	// leave at least this much (COP) to spend on ads per order (default 15000).
	CPAMinimo int64 `yaml:"cpa_minimo" json:"cpa_minimo"`
	// MaxAnunciantes: more advertisers than this = crowded (0 = not checked).
	MaxAnunciantes int `yaml:"max_anunciantes" json:"max_anunciantes"`
}

// DropiConf: until API access is granted, orders are uploaded by hand.
type DropiConf struct {
	Modo string `yaml:"modo" json:"modo"` // "manual" | "api"
}

// DropiConfig holds the Dropi Integrations API credentials (stored in the DB,
// never in negocio.yaml).
type DropiConfig struct {
	// Token is the integration key from Dropi > Configuración > Integraciones.
	Token string `json:"token"`
	// Sandbox uses test-api.dropi.co (no carriers: it cannot quote freight).
	Sandbox bool `json:"sandbox"`
	// OriginDept / OriginCity is where the owner ships from (quote origin).
	OriginDept string `json:"origen_departamento"`
	OriginCity string `json:"origen_ciudad"`
}

// Enabled reports whether the Dropi API can be called.
func (d DropiConfig) Enabled() bool { return strings.TrimSpace(d.Token) != "" }

// Transportadora is one carrier the owner can use through Dropi.
type Transportadora struct {
	ID     string `yaml:"id" json:"id"`
	Nombre string `yaml:"nombre" json:"nombre"`
	Activa bool   `yaml:"activa" json:"activa"`
	// Aliases are other spellings Dropi may use for the same carrier.
	Alias []string `yaml:"alias" json:"alias"`
	// Notas: what the AI must know (coverage, strengths, cash-on-delivery,
	// delivery times...). Injected into the system prompt.
	Notas string `yaml:"notas" json:"notas"`
	// TasaDevolucion: this carrier's own return rate (0..1); 0 = use the
	// business default adjusted by zone.
	TasaDevolucion float64 `yaml:"tasa_devolucion" json:"tasa_devolucion"`
	// Prioridad breaks ties (lower wins).
	Prioridad int `yaml:"prioridad" json:"prioridad"`
	// Zonas the carrier is best for (department names), a hint for the AI and
	// the fallback when Dropi cannot quote.
	Zonas []string `yaml:"zonas" json:"zonas"`
	// DiasEntrega: typical business days to a normal destination (main cities).
	// DiasLejano: same for far / hard-to-reach destinations. Dropi's own quote
	// wins when it includes a delivery time; these are the fallback. 0 = default.
	DiasEntrega int `yaml:"dias_entrega" json:"dias_entrega"`
	DiasLejano  int `yaml:"dias_lejano" json:"dias_lejano"`
}

// defaultDays are rough typical delivery times (business days) per carrier,
// used until the owner edits them with what he actually observes.
var defaultDays = map[string][2]int{
	"coordinadora": {3, 5}, "interrapidisimo": {3, 5}, "envia": {3, 5}, "tcc": {3, 5},
	"servientrega": {3, 5}, "veloces": {2, 4}, "domina": {2, 4}, "99minutos": {1, 2},
	"wiilog": {3, 5}, "jamv-drive": {2, 4}, "derocha-express": {2, 4},
}

// DefaultDays returns the typical delivery days (normal, far) of a carrier id.
func DefaultDays(id string) (normal, far int) {
	if d, ok := defaultDays[id]; ok {
		return d[0], d[1]
	}
	return 3, 5
}

// EstimatedDays is the carrier's typical delivery time for a destination risk
// level ("normal", "lejano", "dificil_acceso", "zona_roja").
func (t Transportadora) EstimatedDays(risk string) int {
	d, far := t.DiasEntrega, t.DiasLejano
	if d <= 0 || far <= 0 {
		def, ok := defaultDays[t.ID]
		if !ok {
			def = [2]int{3, 5}
		}
		if d <= 0 {
			d = def[0]
		}
		if far <= 0 {
			far = def[1]
		}
	}
	if far < d {
		far = d
	}
	if risk == "" || risk == "normal" {
		return d
	}
	return far
}

// DefaultTransportadoras are the ten carriers available through Dropi
// Colombia, all active, with neutral notes the owner can edit.
func DefaultTransportadoras() []Transportadora {
	mk := func(id, name string, prio int, notes string, alias ...string) Transportadora {
		return Transportadora{ID: id, Nombre: name, Activa: true, Prioridad: prio, Notas: notes, Alias: alias}
	}
	return []Transportadora{
		mk("coordinadora", "Coordinadora", 1, "Cobertura nacional amplia, buena en ciudades principales y municipios intermedios. Contraentrega disponible."),
		mk("interrapidisimo", "Interrapidísimo", 2, "Muy buena cobertura en municipios pequeños y zonas apartadas. Contraentrega disponible.", "inter rapidisimo", "interrapidísimo"),
		mk("envia", "Envía", 3, "Cobertura nacional, competitiva en precio en ciudades principales.", "envia", "envía"),
		mk("tcc", "TCC", 4, "Fuerte en ciudades principales y corredores industriales."),
		mk("servientrega", "Servientrega", 99, "Respaldo. Desactivada por defecto si no la usas.", "servi entrega"),
		mk("veloces", "Veloces", 5, "Transportadora regional/urbana; confirma cobertura antes de ofrecerla.", "veloces"),
		mk("domina", "Domina", 6, "Entrega urbana; confirma cobertura antes de ofrecerla.", "domina entrega total", "dominaentregatotal"),
		mk("99minutos", "99minutos", 7, "Entrega rápida urbana en ciudades principales.", "99 minutos"),
		mk("wiilog", "Wiilog", 8, "Operador logístico; confirma cobertura antes de ofrecerla.", "wilog", "willog"),
		mk("jamv-drive", "Jamv-drive", 9, "Entrega local; confirma cobertura antes de ofrecerla.", "jamv drive", "jamvdrive"),
		mk("derocha-express", "Derocha-express", 10, "Entrega local/regional; confirma cobertura antes de ofrecerla.", "derocha express", "derochaexpress"),
	}
}

// NotificarConf: the owner's own WhatsApp number to receive alerts.
type NotificarConf struct {
	WhatsAppDueno string `yaml:"whatsapp_dueno" json:"whatsapp_dueno"`
}

// Load reads .env (optional), environment variables and negocio.yaml.
func Load(baseDir string) (*Config, error) {
	loadDotEnv(filepath.Join(baseDir, ".env"))

	cfg := &Config{
		Port:         envInt("PORT", 8080),
		DataDir:      env("DATA_DIR", filepath.Join(baseDir, "data")),
		KnowledgeDir: env("KNOWLEDGE_DIR", filepath.Join(baseDir, "knowledge")),
		AdminToken:   env("ADMIN_TOKEN", ""),
		PublicURL:    strings.TrimRight(env("PUBLIC_URL", ""), "/"),
		LLM: LLMConfig{
			BaseURL:     strings.TrimRight(env("LLM_BASE_URL", "https://api.deepseek.com"), "/"),
			APIKey:      firstNonEmpty(os.Getenv("LLM_API_KEY"), os.Getenv("DEEPSEEK_API_KEY")),
			Model:       env("LLM_MODEL", "deepseek-chat"),
			Temperature: envTemperature("LLM_TEMPERATURE", 0.7),
		},
		WhatsApp: WhatsAppConfig{
			Token:         env("WHATSAPP_TOKEN", ""),
			PhoneNumberID: env("WHATSAPP_PHONE_NUMBER_ID", ""),
			VerifyToken:   env("WHATSAPP_VERIFY_TOKEN", ""),
			AppSecret:     env("WHATSAPP_APP_SECRET", ""),
			GraphVersion:  env("WHATSAPP_GRAPH_VERSION", "v21.0"),
		},
		Wompi: WompiConfig{
			PublicKey:    env("WOMPI_PUBLIC_KEY", ""),
			PrivateKey:   env("WOMPI_PRIVATE_KEY", ""),
			EventsSecret: env("WOMPI_EVENTS_SECRET", ""),
			Sandbox:      env("WOMPI_ENV", "sandbox") != "production",
		},
	}

	negPath := env("NEGOCIO_FILE", filepath.Join(baseDir, "negocio.yaml"))
	neg, err := LoadNegocio(negPath)
	if err != nil {
		return nil, err
	}
	cfg.Negocio = neg
	return cfg, nil
}

// LoadNegocio reads negocio.yaml, applying defaults for anything missing.
func LoadNegocio(path string) (Negocio, error) {
	n := DefaultNegocio()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return n, nil
		}
		return n, fmt.Errorf("leer %s: %w", path, err)
	}
	if err := yaml.Unmarshal(raw, &n); err != nil {
		return n, fmt.Errorf("negocio.yaml inválido: %w", err)
	}
	n.ApplyDefaults()
	return n, nil
}

// DefaultNegocio returns safe defaults (human approval for everything).
func DefaultNegocio() Negocio {
	n := Negocio{}
	n.ApplyDefaults()
	return n
}

// ApplyDefaults fills missing values with safe defaults.
func (n *Negocio) ApplyDefaults() {
	if n.NombreTienda == "" {
		n.NombreTienda = "Mi tienda"
	}
	if n.Ciudad == "" {
		n.Ciudad = "Medellín"
	}
	if n.Persona.Nombre == "" {
		n.Persona.Nombre = "Sofi"
	}
	if n.Persona.Rol == "" {
		n.Persona.Rol = "asesora de ventas"
	}
	if n.Pagos.Metodo == "" {
		n.Pagos.Metodo = "manual"
	}
	if n.Pagos.HorasParaPagar == 0 {
		n.Pagos.HorasParaPagar = 24
	}
	if n.Conversacion.EsperaAgruparSeg == 0 {
		n.Conversacion.EsperaAgruparSeg = DefaultEsperaAgruparSeg
	}
	if n.Conversacion.PausaEntreMensajesMs == 0 {
		n.Conversacion.PausaEntreMensajesMs = 1200
	}
	if n.Conversacion.HistorialMaximo == 0 {
		n.Conversacion.HistorialMaximo = 40
	}
	if n.Finanzas.TasaDevolucion == 0 {
		n.Finanzas.TasaDevolucion = 0.25
	}
	if n.Finanzas.TRM == 0 {
		n.Finanzas.TRM = 4000
	}
	if n.Finanzas.MargenSeguridad == 0 {
		n.Finanzas.MargenSeguridad = 0.30
	}
	if n.Finanzas.TasaConfirmacion == 0 {
		n.Finanzas.TasaConfirmacion = 0.85
	}
	if n.Dropi.Modo == "" {
		n.Dropi.Modo = "manual"
	}
	switch n.Envio.Criterio {
	case CriterioRentable, CriterioRapido, CriterioBarato:
	default:
		n.Envio.Criterio = CriterioRentable
	}
	if n.Envio.MaxDias < 0 {
		n.Envio.MaxDias = 0
	}
	if n.Cazador.MinVentas7d <= 0 {
		n.Cazador.MinVentas7d = 10
	}
	if n.Cazador.CPAMinimo <= 0 {
		n.Cazador.CPAMinimo = 15000
	}
	if n.Cazador.MaxAnunciantes < 0 {
		n.Cazador.MaxAnunciantes = 0
	}
	if len(n.Transportadoras) == 0 {
		n.Transportadoras = DefaultTransportadoras()
		for i := range n.Transportadoras {
			if n.Transportadoras[i].ID == "servientrega" {
				n.Transportadoras[i].Activa = false
			}
		}
	}
	for i := range n.Transportadoras {
		t := &n.Transportadoras[i]
		t.Nombre = strings.TrimSpace(t.Nombre)
		if t.ID == "" {
			t.ID = CarrierID(t.Nombre)
		}
		if t.DiasEntrega < 0 {
			t.DiasEntrega = 0
		}
		if t.DiasLejano < 0 {
			t.DiasLejano = 0
		}
	}
}

// CarrierID makes a stable id from a carrier name ("Envía" -> "envia").
func CarrierID(name string) string {
	r := strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ñ", "n", "Á", "a", "É", "e", "Í", "i", "Ó", "o", "Ú", "u")
	name = strings.ToLower(r.Replace(strings.TrimSpace(name)))
	var b strings.Builder
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-':
			b.WriteRune(c)
		case c == ' ' || c == '_':
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// Carrier finds a carrier by id, name or alias.
func (n Negocio) Carrier(name string) (Transportadora, bool) {
	key := CarrierID(name)
	if key == "" {
		return Transportadora{}, false
	}
	for _, t := range n.Transportadoras {
		if t.ID == key || CarrierID(t.Nombre) == key {
			return t, true
		}
		for _, a := range t.Alias {
			if CarrierID(a) == key {
				return t, true
			}
		}
	}
	return Transportadora{}, false
}

// AgruparDuration is the debounce window as a duration.
func (c Conversacion) AgruparDuration() time.Duration {
	return time.Duration(c.EsperaAgruparSeg) * time.Second
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return v
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v, err := strconv.ParseFloat(os.Getenv(key), 64); err == nil {
		return v
	}
	return def
}

func envTemperature(key string, def float64) *float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if strings.EqualFold(raw, "omit") || strings.EqualFold(raw, "none") {
		return nil
	}
	v := envFloat(key, def)
	return &v
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// loadDotEnv sets variables from a KEY=VALUE file without overriding the real
// environment. Missing file is fine.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, val)
		}
	}
}
