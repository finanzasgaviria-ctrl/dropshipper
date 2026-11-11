package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/michiTrader/dropshipper/motor/internal/config"
	"github.com/michiTrader/dropshipper/motor/internal/hub"
	"github.com/michiTrader/dropshipper/motor/internal/llm"
	"github.com/michiTrader/dropshipper/motor/internal/payments"
	"github.com/michiTrader/dropshipper/motor/internal/settings"
	"github.com/michiTrader/dropshipper/motor/internal/store"
	"github.com/michiTrader/dropshipper/motor/internal/whatsapp"
)

func newWA(c config.WhatsAppConfig) *whatsapp.Client {
	return whatsapp.New(c.Token, c.PhoneNumberID, c.GraphVersion)
}

func newWompi(c config.WompiConfig) *payments.Wompi {
	return payments.NewWompi(c.PrivateKey, c.EventsSecret, c.Sandbox)
}

func (s *Server) settingsRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/ajustes", s.getSettings)
	api.HandleFunc("PUT /api/ajustes/negocio", s.putNegocio)
	api.HandleFunc("PUT /api/ajustes/general", s.putGeneral)
	api.HandleFunc("PUT /api/ajustes/whatsapp", s.putWhatsApp)
	api.HandleFunc("DELETE /api/ajustes/whatsapp", s.delWhatsApp)
	api.HandleFunc("PUT /api/ajustes/whatsapp/canal", s.putWhatsAppChannel)
	api.HandleFunc("PUT /api/ajustes/wompi", s.putWompi)
	api.HandleFunc("DELETE /api/ajustes/wompi", s.delWompi)
	api.HandleFunc("POST /api/whatsapp/probar", s.testWhatsApp)
	api.HandleFunc("POST /api/wompi/probar", s.testWompi)
	api.HandleFunc("POST /api/ia/proveedores", s.addProvider)
	api.HandleFunc("PUT /api/ia/proveedores/{pid}", s.updateProvider)
	api.HandleFunc("DELETE /api/ia/proveedores/{pid}", s.deleteProvider)
	api.HandleFunc("POST /api/ia/proveedores/{pid}/activar", s.activateProvider)
	api.HandleFunc("POST /api/ia/probar", s.testProvider)
	api.HandleFunc("POST /api/ia/modelos", s.listModels)
	s.dropiRoutes(api)
	s.hunterRoutes(api)
	s.ngrokRoutes(api)
}

// origin is the scheme://host the browser used, to show webhook URLs when no
// public URL has been set yet.
func origin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.Settings.Get().PublicWith(s.Hub.Tunnel.URL(), origin(r)))
}

// keep returns old when the browser sent an empty secret (= "do not change").
func keep(in, old string) string {
	if strings.TrimSpace(in) == "" {
		return old
	}
	return strings.TrimSpace(in)
}

func (s *Server) update(w http.ResponseWriter, r *http.Request, fn func(*settings.Settings) error) bool {
	if err := s.Settings.Update(r.Context(), fn); err != nil {
		writeErr(w, err)
		return false
	}
	return true
}

func (s *Server) putNegocio(w http.ResponseWriter, r *http.Request) {
	ok := s.update(w, r, func(st *settings.Settings) error {
		n := st.Negocio // start from the saved values: missing fields keep their value
		if err := readJSON(r, &n); err != nil {
			return errors.New("los datos enviados no son válidos")
		}
		st.Negocio = n
		return nil
	})
	if ok {
		writeJSON(w, 200, map[string]any{"ok": true})
	}
}

func (s *Server) putGeneral(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PublicURL   *string `json:"url_publica"`
		Token       string  `json:"token_admin"`
		RemoveToken bool    `json:"quitar_token"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	var newToken string
	if s.update(w, r, func(st *settings.Settings) error {
		if in.PublicURL != nil {
			st.General.PublicURL = *in.PublicURL
		}
		switch {
		case in.RemoveToken:
			st.General.AdminToken = ""
		case strings.TrimSpace(in.Token) != "":
			st.General.AdminToken = strings.TrimSpace(in.Token)
		}
		newToken = st.General.AdminToken
		return nil
	}) {
		writeJSON(w, 200, map[string]any{"ok": true, "token_cambiado": in.Token != "" || in.RemoveToken})
	}
	_ = newToken
}

type waIn struct {
	Canal         string `json:"canal"`
	Token         string `json:"token"`
	PhoneNumberID string `json:"phone_number_id"`
	VerifyToken   string `json:"verify_token"`
	AppSecret     string `json:"app_secret"`
	GraphVersion  string `json:"graph_version"`
}

func (in waIn) apply(old config.WhatsAppConfig) config.WhatsAppConfig {
	out := old
	if c := strings.TrimSpace(in.Canal); c != "" {
		out.Canal = c
	}
	out.Token = keep(in.Token, old.Token)
	out.VerifyToken = keep(in.VerifyToken, old.VerifyToken)
	out.AppSecret = keep(in.AppSecret, old.AppSecret)
	out.PhoneNumberID = strings.TrimSpace(in.PhoneNumberID)
	if v := strings.TrimSpace(in.GraphVersion); v != "" {
		out.GraphVersion = v
	}
	if out.GraphVersion == "" {
		out.GraphVersion = "v21.0"
	}
	return out
}

func (s *Server) putWhatsApp(w http.ResponseWriter, r *http.Request) {
	var in waIn
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if s.update(w, r, func(st *settings.Settings) error {
		st.WhatsApp = in.apply(st.WhatsApp)
		if !st.WhatsApp.UsesWeb() && (st.WhatsApp.Token == "") != (st.WhatsApp.PhoneNumberID == "") {
			return errors.New("para conectar WhatsApp se necesitan las dos cosas: el token de acceso y el ID del número de teléfono")
		}
		return nil
	}) {
		writeJSON(w, 200, map[string]any{"ok": true})
	}
}

// putWhatsAppChannel switches between the official API and WhatsApp Web without
// touching the saved Meta credentials.
func (s *Server) putWhatsAppChannel(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Canal string `json:"canal"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if in.Canal != config.ChannelCloud && in.Canal != config.ChannelWeb {
		writeErr(w, errors.New("el canal debe ser 'cloud' o 'web'"))
		return
	}
	if s.update(w, r, func(st *settings.Settings) error {
		st.WhatsApp.Canal = in.Canal
		return nil
	}) {
		writeJSON(w, 200, map[string]any{"ok": true, "canal": in.Canal})
	}
}

func (s *Server) delWhatsApp(w http.ResponseWriter, r *http.Request) {
	if s.update(w, r, func(st *settings.Settings) error {
		st.WhatsApp = config.WhatsAppConfig{Canal: st.WhatsApp.Canal, GraphVersion: "v21.0"}
		return nil
	}) {
		writeJSON(w, 200, map[string]any{"ok": true})
	}
}

type wompiIn struct {
	PublicKey    string `json:"public_key"`
	PrivateKey   string `json:"private_key"`
	EventsSecret string `json:"events_secret"`
	Sandbox      bool   `json:"sandbox"`
}

func (in wompiIn) apply(old config.WompiConfig) config.WompiConfig {
	return config.WompiConfig{
		PublicKey:    strings.TrimSpace(in.PublicKey),
		PrivateKey:   keep(in.PrivateKey, old.PrivateKey),
		EventsSecret: keep(in.EventsSecret, old.EventsSecret),
		Sandbox:      in.Sandbox,
	}
}

func (s *Server) putWompi(w http.ResponseWriter, r *http.Request) {
	var in wompiIn
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if s.update(w, r, func(st *settings.Settings) error {
		st.Wompi = in.apply(st.Wompi)
		if st.Wompi.PrivateKey != "" && st.Wompi.EventsSecret == "" {
			return errors.New("falta el secreto de eventos de Wompi: sin él no se pueden confirmar los pagos automáticamente")
		}
		return nil
	}) {
		writeJSON(w, 200, map[string]any{"ok": true})
	}
}

func (s *Server) delWompi(w http.ResponseWriter, r *http.Request) {
	if s.update(w, r, func(st *settings.Settings) error {
		st.Wompi = config.WompiConfig{Sandbox: true}
		if st.Negocio.Pagos.Metodo == "wompi" {
			st.Negocio.Pagos.Metodo = "manual"
		}
		return nil
	}) {
		writeJSON(w, 200, map[string]any{"ok": true})
	}
}

// ---------- connection tests (always HTTP 200: {ok, error|mensaje}) ----------

func testFail(w http.ResponseWriter, msg string) {
	writeJSON(w, 200, map[string]any{"ok": false, "error": msg})
}

func (s *Server) testWhatsApp(w http.ResponseWriter, r *http.Request) {
	var in waIn
	if err := readJSON(r, &in); err != nil {
		testFail(w, "los datos enviados no son válidos")
		return
	}
	cfg := in.apply(s.Settings.Get().WhatsApp)
	if cfg.Token == "" || cfg.PhoneNumberID == "" {
		testFail(w, "Escribe el token de acceso y el ID del número de teléfono, y vuelve a probar.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), hub.TestTimeout)
	defer cancel()
	info, err := newWA(cfg).Check(ctx)
	if err != nil {
		testFail(w, whatsappAdvice(err))
		return
	}
	msg := "Conectado"
	if info.DisplayPhone != "" {
		msg += ": " + info.DisplayPhone
	}
	if info.VerifiedName != "" {
		msg += " (" + info.VerifiedName + ")"
	}
	writeJSON(w, 200, map[string]any{"ok": true, "mensaje": msg, "info": info})
}

func (s *Server) testWompi(w http.ResponseWriter, r *http.Request) {
	var in wompiIn
	if err := readJSON(r, &in); err != nil {
		testFail(w, "los datos enviados no son válidos")
		return
	}
	cfg := in.apply(s.Settings.Get().Wompi)
	if cfg.PublicKey == "" {
		testFail(w, "Escribe la llave pública de Wompi (empieza por pub_test_ o pub_prod_) y vuelve a probar.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), hub.TestTimeout)
	defer cancel()
	info, err := newWompi(cfg).CheckMerchant(ctx, cfg.PublicKey)
	if err != nil {
		testFail(w, wompiAdvice(err))
		return
	}
	msg := "Conectado"
	if info.Name != "" {
		msg += ": " + info.Name
	}
	if strings.HasPrefix(cfg.PublicKey, "pub_test_") != cfg.Sandbox {
		msg += ". Atención: la llave y el modo (pruebas/producción) no coinciden."
	}
	writeJSON(w, 200, map[string]any{"ok": true, "mensaje": msg, "info": info})
}

func whatsappAdvice(err error) string {
	low := strings.ToLower(err.Error())
	switch {
	case strings.Contains(low, "http 401") || strings.Contains(low, "oauth") || strings.Contains(low, "access token"):
		return "Meta rechazó el token. Genera un token permanente (usuario del sistema) en Meta Business y pégalo completo."
	case strings.Contains(low, "http 400") || strings.Contains(low, "http 404") || strings.Contains(low, "does not exist"):
		return "Meta no encuentra ese ID de número. Cópialo de WhatsApp > Configuración de la API (es un número largo, no tu teléfono)."
	case strings.Contains(low, "no such host") || strings.Contains(low, "connection refused") || strings.Contains(low, "timeout") || strings.Contains(low, "deadline"):
		return "No se pudo conectar con Meta. Revisa tu conexión a internet e inténtalo de nuevo."
	}
	return err.Error()
}

func wompiAdvice(err error) string {
	low := strings.ToLower(err.Error())
	switch {
	case strings.Contains(low, "http 404") || strings.Contains(low, "http 401"):
		return "Wompi no reconoce esa llave pública. Revisa que esté completa y que coincida con el modo (pruebas o producción)."
	case strings.Contains(low, "no such host") || strings.Contains(low, "connection refused") || strings.Contains(low, "timeout") || strings.Contains(low, "deadline"):
		return "No se pudo conectar con Wompi. Revisa tu conexión a internet e inténtalo de nuevo."
	}
	return err.Error()
}

// ---------- AI providers ----------

type providerIn struct {
	ID          string   `json:"id"`
	Name        string   `json:"nombre"`
	Preset      string   `json:"preset"`
	BaseURL     string   `json:"base_url"`
	APIKey      string   `json:"api_key"`
	Model       string   `json:"modelo"`
	Temperature *float64 `json:"temperatura"`
	Activate    bool     `json:"activar"`
}

func hostOf(u string) string {
	pu, err := url.Parse(llm.NormalizeBaseURL(u))
	if err != nil {
		return ""
	}
	return strings.ToLower(pu.Host)
}

// resolveProvider merges the form with the saved provider (same id). An empty
// key keeps the saved one, but only while the address stays on the same host:
// a saved key is never sent to a different server.
func resolveProvider(cur settings.Settings, in providerIn) (settings.Provider, bool, error) {
	var p settings.Provider
	old, exists := cur.IA.Provider(in.ID)
	if exists {
		p = old
	}
	p.ID = in.ID
	if in.Name != "" {
		p.Name = in.Name
	}
	if in.Preset != "" {
		p.Preset = in.Preset
	}
	if in.BaseURL != "" {
		p.BaseURL = in.BaseURL
	}
	if in.Model != "" {
		p.Model = in.Model
	}
	p.Temperature = in.Temperature
	if strings.TrimSpace(in.APIKey) != "" {
		p.APIKey = strings.TrimSpace(in.APIKey)
	} else if exists && hostOf(old.BaseURL) != hostOf(p.BaseURL) {
		p.APIKey = ""
	}
	if pr, ok := settings.PresetByID(p.Preset); ok && pr.NoKey {
		p.NoKey = true
	}
	return p, exists, nil
}

func (s *Server) saveProvider(w http.ResponseWriter, r *http.Request, create bool) {
	var in providerIn
	if err := readJSON(r, &in); err != nil {
		writeErr(w, errors.New("los datos enviados no son válidos"))
		return
	}
	if !create {
		in.ID = r.PathValue("pid")
	}
	var saved settings.Provider
	if s.update(w, r, func(st *settings.Settings) error {
		if create {
			in.ID = ""
		} else if _, ok := st.IA.Provider(in.ID); !ok {
			return store.ErrNotFound
		}
		p, exists, _ := resolveProvider(*st, in)
		if create {
			p.ID = ""
		}
		if err := p.Validate(); err != nil {
			return err
		}
		if create {
			p.ID = st.IA.NewID(p.Name)
		}
		if exists {
			for i := range st.IA.Providers {
				if st.IA.Providers[i].ID == p.ID {
					st.IA.Providers[i] = p
				}
			}
		} else {
			st.IA.Providers = append(st.IA.Providers, p)
		}
		if in.Activate || st.IA.ActiveID == "" {
			st.IA.ActiveID = p.ID
		}
		saved = p
		return nil
	}) {
		writeJSON(w, 200, map[string]any{"ok": true, "id": saved.ID, "activo": s.Settings.Get().IA.ActiveID == saved.ID})
	}
}

func (s *Server) addProvider(w http.ResponseWriter, r *http.Request)    { s.saveProvider(w, r, true) }
func (s *Server) updateProvider(w http.ResponseWriter, r *http.Request) { s.saveProvider(w, r, false) }

func (s *Server) deleteProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("pid")
	if s.update(w, r, func(st *settings.Settings) error {
		if _, ok := st.IA.Provider(id); !ok {
			return store.ErrNotFound
		}
		out := st.IA.Providers[:0:0]
		for _, p := range st.IA.Providers {
			if p.ID != id {
				out = append(out, p)
			}
		}
		st.IA.Providers = out
		if st.IA.ActiveID == id {
			st.IA.ActiveID = ""
			for _, p := range out {
				if p.Ready() {
					st.IA.ActiveID = p.ID
					break
				}
			}
		}
		return nil
	}) {
		writeJSON(w, 200, map[string]any{"ok": true, "activo": s.Settings.Get().IA.ActiveID})
	}
}

func (s *Server) activateProvider(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("pid")
	if s.update(w, r, func(st *settings.Settings) error {
		p, ok := st.IA.Provider(id)
		if !ok {
			return store.ErrNotFound
		}
		if !p.Ready() {
			return errors.New("ese proveedor está incompleto: falta la clave o el modelo")
		}
		st.IA.ActiveID = id
		return nil
	}) {
		writeJSON(w, 200, map[string]any{"ok": true})
	}
}

func clientFor(p settings.Provider) *llm.Client {
	c := llm.NewClient(p.BaseURL, p.APIKey)
	c.AllowNoKey = p.NoKey
	return c
}

// probeProvider builds the provider to test from the form (+ saved key).
func (s *Server) probeProvider(r *http.Request) (settings.Provider, error) {
	var in providerIn
	if err := readJSON(r, &in); err != nil {
		return settings.Provider{}, errors.New("los datos enviados no son válidos")
	}
	p, _, _ := resolveProvider(s.Settings.Get(), in)
	return p, nil
}

func (s *Server) testProvider(w http.ResponseWriter, r *http.Request) {
	p, err := s.probeProvider(r)
	if err != nil {
		testFail(w, err.Error())
		return
	}
	if err := p.Validate(); err != nil {
		testFail(w, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), hub.TestTimeout)
	defer cancel()
	res, err := clientFor(p).Check(ctx, p.Model)
	if err != nil {
		testFail(w, llm.Friendly(err))
		return
	}
	out := map[string]any{"ok": true, "mensaje": "Conexión correcta: el modelo respondió en " + msFmt(res.LatencyMs), "resultado": res}
	if sug := suggestedURL(p.BaseURL, res.BaseURL); sug != "" {
		out["url_sugerida"] = sug
		out["mensaje"] = "Conexión correcta usando " + sug + " (se añadió /v1 a la dirección). Guarda así para que quede fija."
	}
	writeJSON(w, 200, out)
}

// suggestedURL returns the base URL that really worked when it differs from
// the one typed (typically the same address plus "/v1"), or "".
func suggestedURL(typed, worked string) string {
	typed = llm.NormalizeBaseURL(typed)
	if worked == "" || worked == typed {
		return ""
	}
	return worked
}

func msFmt(ms int64) string {
	if ms < 1000 {
		return itoa(ms) + " ms"
	}
	return itoa(ms/1000) + "." + itoa((ms%1000)/100) + " s"
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func (s *Server) listModels(w http.ResponseWriter, r *http.Request) {
	p, err := s.probeProvider(r)
	if err != nil {
		testFail(w, err.Error())
		return
	}
	p.BaseURL = llm.NormalizeBaseURL(p.BaseURL)
	if err := llm.ValidateBaseURL(p.BaseURL); err != nil {
		testFail(w, err.Error())
		return
	}
	if p.APIKey == "" && !p.NoKey {
		testFail(w, "Escribe la clave (API key) para poder ver los modelos disponibles.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), hub.TestTimeout)
	defer cancel()
	cl := clientFor(p)
	models, err := cl.ListModels(ctx)
	if err != nil {
		testFail(w, llm.Friendly(err))
		return
	}
	if models == nil {
		models = []string{}
	}
	out := map[string]any{"ok": true, "modelos": models}
	if sug := suggestedURL(p.BaseURL, cl.EffectiveBaseURL()); sug != "" {
		out["url_sugerida"] = sug
	}
	writeJSON(w, 200, out)
}

// ---------- status ----------

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	st := s.Settings.Get()
	n := st.Negocio
	prods := s.Catalog.List()
	active, confirmed := 0, 0
	for _, p := range prods {
		if p.Active {
			active++
			if p.CostConfirmed {
				confirmed++
			}
		}
	}
	prov, hasProv := st.Active()
	provs := []map[string]any{}
	for _, p := range st.IA.Providers {
		provs = append(provs, map[string]any{"id": p.ID, "nombre": p.Name, "modelo": p.Model, "lista": p.Ready(), "activo": p.ID == st.IA.ActiveID})
	}
	wompi := s.Hub.Wompi() != nil
	pagoListo := n.Pagos.Metodo == "wompi" && wompi ||
		n.Pagos.Metodo == "manual" && (n.Pagos.Manual.LlaveBreB != "" || n.Pagos.Manual.NumeroCuenta != "")
	base, source := s.Hub.PublicBase()
	if base == "" {
		base = origin(r)
	}
	writeJSON(w, 200, map[string]any{
		"tienda":  n.NombreTienda,
		"persona": n.Persona,
		"ia": map[string]any{
			"configurada": s.Hub.AI.Ready(), "proveedor": prov.Name, "modelo": prov.Model, "endpoint": prov.BaseURL,
			"hay_proveedor": hasProv, "proveedores": provs,
		},
		"whatsapp": map[string]any{
			"canal": st.WhatsApp.Channel(), "configurado": s.Hub.Available(), "nube_configurada": st.WhatsApp.Enabled(),
			"web": s.Hub.Web.Info(), "verify_token": st.WhatsApp.VerifyToken != "",
			"firma_verificada": st.WhatsApp.AppSecret != "", "webhook_url": base + "/webhook/whatsapp", "webhook_fuente": source,
		},
		"pagos": map[string]any{
			"metodo": n.Pagos.Metodo, "listo": pagoListo, "wompi": wompi,
			"llave_bre_b": n.Pagos.Manual.LlaveBreB != "", "cuenta": n.Pagos.Manual.NumeroCuenta != "",
		},
		"dropi": map[string]any{"modo": n.Dropi.Modo, "conectado": s.Hub.Dropi() != nil, "sandbox": st.Dropi.Sandbox,
			"origen": strings.TrimSpace(st.Dropi.OriginCity + " " + st.Dropi.OriginDept)},
		"productos":      map[string]any{"total": len(prods), "activos": active, "costo_confirmado": confirmed},
		"autonomia":      n.Autonomia,
		"aviso_dueno":    n.Notificar.WhatsAppDueno != "",
		"requiere_token": st.General.AdminToken != "",
		"acceso_local":   isLocal(r),
		"url_publica":    st.General.PublicURL,
		"tunel":          s.Hub.Tunnel.Info(),
		"iniciado":       s.Started,
		"version":        Version,
	})
}
