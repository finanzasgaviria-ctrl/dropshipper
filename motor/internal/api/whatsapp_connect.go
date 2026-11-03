package api

// connectWhatsApp does the two Meta-side steps the panel cannot ask the owner
// for: registering the webhook on the app and subscribing the app to the
// WhatsApp Business account. Both are done with the credentials the owner
// pastes, so a new shop only needs five values and one button.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/michiTrader/dropshipper/motor/internal/config"
	"github.com/michiTrader/dropshipper/motor/internal/hub"
	"github.com/michiTrader/dropshipper/motor/internal/settings"
)

// connectStep is one line of the connection report shown in the panel.
type connectStep struct {
	Paso    string `json:"paso"`
	Ok      bool   `json:"ok"`
	Detalle string `json:"detalle,omitempty"`
	Arreglo string `json:"arreglo,omitempty"`
}

// webhookBase is the address Meta must be able to reach right now: the live
// tunnel first, then the saved public URL.
func (s *Server) webhookBase(r *http.Request, st settings.Settings) (base, fuente string) {
	if u := strings.TrimRight(s.Hub.Tunnel.URL(), "/"); u != "" {
		return u, "tunel"
	}
	if u := strings.TrimRight(st.General.PublicURL, "/"); u != "" {
		return u, "publica"
	}
	return strings.TrimRight(origin(r), "/"), "local"
}

// handshake asks our own public address the same question Meta asks when it
// registers a webhook. It proves the address works before blaming Meta.
func handshake(ctx context.Context, hookURL, token, challenge string) error {
	u := hookURL + "?hub.mode=subscribe&hub.verify_token=" + url.QueryEscape(token) + "&hub.challenge=" + url.QueryEscape(challenge)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Webhooks/1.0 (+https://fb.me/webhooks)")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	buf := make([]byte, 256)
	n, _ := resp.Body.Read(buf)
	body := strings.TrimSpace(string(buf[:n]))
	if resp.StatusCode != 200 {
		return fmt.Errorf("la dirección respondió HTTP %d", resp.StatusCode)
	}
	if body != challenge {
		return errors.New("la dirección no devolvió el reto de Meta: el webhook no llega al Motor")
	}
	return nil
}

func (s *Server) connectWhatsApp(w http.ResponseWriter, r *http.Request) {
	var in waIn
	if err := readJSON(r, &in); err != nil {
		testFail(w, "los datos enviados no son válidos")
		return
	}
	st := s.Settings.Get()
	cfg := in.apply(st.WhatsApp)
	steps := make([]connectStep, 0, 7)

	add := func(paso string, ok bool, detalle, arreglo string) {
		steps = append(steps, connectStep{Paso: paso, Ok: ok, Detalle: detalle, Arreglo: arreglo})
	}
	fail := func(paso, detalle, arreglo string) {
		add(paso, false, detalle, arreglo)
		writeJSON(w, 200, map[string]any{"ok": false, "pasos": steps})
	}

	if cfg.Token == "" || cfg.PhoneNumberID == "" {
		fail("Credenciales", "faltan el token de acceso y el ID del número de teléfono", "")
		return
	}
	if cfg.VerifyToken == "" {
		cfg.VerifyToken = settings.NewToken()[:24]
	}
	if cfg.GraphVersion == "" {
		cfg.GraphVersion = "v21.0"
	}
	cl := newWA(cfg)
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	// 1. Do the credentials point at a real number?
	info, err := cl.Check(ctx)
	if err != nil {
		fail("Número de WhatsApp", whatsappAdvice(err), "Revisa el token permanente y el ID del número de teléfono en Meta > Configuración de la API.")
		return
	}
	num := strings.TrimSpace(info.DisplayPhone + " " + info.VerifiedName)
	add("Número de WhatsApp", true, num, "")

	// 2. Is the address Meta will call reachable right now?
	base, fuente := s.webhookBase(r, st)
	hookURL := base + "/webhook/whatsapp"
	if !strings.HasPrefix(hookURL, "https://") {
		fail("Dirección pública", "solo existe en este computador ("+hookURL+")",
			"Activa el túnel de ngrok en el bloque de abajo (pegas tu token) y vuelve a intentar.")
		return
	}
	if err := handshake(ctx, hookURL, cfg.VerifyToken, "motor-conectar"); err != nil {
		fail("Dirección pública", hookURL+": "+err.Error(),
			"El túnel tiene que estar encendido y apuntar al Motor (puerto 8080).")
		return
	}
	add("Dirección pública", true, hookURL+" ("+fuente+")", "")

	// 3. Which Meta app is behind the token? (asked to Meta, not to the owner)
	if cfg.AppID == "" {
		id, err := cl.AppID(ctx)
		if err != nil {
			fail("App de Meta", "no se pudo saber a qué app pertenece el token: "+err.Error(),
				"Pega el ID de la app (Meta > Información básica) en el campo correspondiente.")
			return
		}
		cfg.AppID = id
	}
	if cfg.AppSecret == "" {
		fail("Secreto de la app", "falta el secreto de la app: sin él no se puede configurar el webhook ni verificar que los mensajes vienen de Meta",
			"Meta > Información básica > Clave secreta de la app > Mostrar. Son 32 caracteres.")
		return
	}
	add("App de Meta", true, cfg.AppID, "")

	// 4. Register (or repair) the webhook on the app.
	if err := cl.SetWebhook(ctx, cfg.AppID, cfg.AppSecret, hookURL, cfg.VerifyToken, []string{"messages"}); err != nil {
		fail("Webhook en Meta", err.Error(), "Comprueba que la clave secreta de la app sea la de esta misma app.")
		return
	}
	spec, err := cl.Webhook(ctx, cfg.AppID, cfg.AppSecret)
	if err != nil {
		fail("Webhook en Meta", "Meta no confirmó el webhook: "+err.Error(), "")
		return
	}
	switch {
	case !spec.Active:
		fail("Webhook en Meta", "Meta tiene el webhook inactivo", "Revisa que la app esté publicada o vuelve a intentar.")
		return
	case spec.CallbackURL != hookURL:
		fail("Webhook en Meta", "Meta apunta a "+spec.CallbackURL, "Puede tardar unos segundos en reflejarse: vuelve a intentar.")
		return
	}
	fields := strings.Join(spec.Fields, ", ")
	if !strings.Contains(fields, "messages") {
		fail("Campos del webhook", "Meta no tiene suscrito el campo 'messages' (tiene: "+fields+")", "Vuelve a intentar.")
		return
	}
	add("Webhook en Meta", true, hookURL+" · campo messages suscrito", "")

	// 5. The WhatsApp Business account: the step nobody finds in the panel.
	if cfg.WABAID == "" {
		fail("Cuenta de WhatsApp Business", "falta el ID de la cuenta de WhatsApp Business",
			"En Meta > Configuración de la API aparece como «Identificador de la cuenta de WhatsApp Business» ("+
				"un número largo, distinto del ID del número). Pégalo en el campo «Cuenta (WABA)» y vuelve a intentar. "+
				"También se rellena solo en cuanto llega cualquier mensaje.")
		return
	}
	subs, err := cl.SubscribedApps(ctx, cfg.WABAID)
	if err != nil {
		fail("Cuenta de WhatsApp Business", "Meta no deja leer la cuenta "+cfg.WABAID+": "+err.Error(),
			"Revisa que ese ID sea el de tu cuenta y que el token tenga permisos de administración.")
		return
	}
	if !contains(subs, cfg.AppID) {
		if err := cl.SubscribeApp(ctx, cfg.WABAID); err != nil {
			fail("Suscripción de la app a la cuenta", err.Error(), "Vuelve a intentar en unos segundos.")
			return
		}
		subs, _ = cl.SubscribedApps(ctx, cfg.WABAID)
		if !contains(subs, cfg.AppID) {
			fail("Suscripción de la app a la cuenta", "Meta no confirmó la suscripción de la app "+cfg.AppID,
				"En Meta > Configuración de la API activa el interruptor «Suscribir webhooks» de la cuenta.")
			return
		}
	}
	add("Cuenta de WhatsApp Business", true, cfg.WABAID+" · app suscrita", "")

	// 6. Save everything, including the values Meta told us.
	if err := s.Settings.Update(r.Context(), func(st *settings.Settings) error {
		cfg.Canal = nonEmpty(cfg.Canal, config.ChannelCloud)
		st.WhatsApp = cfg
		return nil
	}); err != nil {
		fail("Guardar", "no se pudo guardar la configuración: "+err.Error(), "")
		return
	}
	add("Guardar", true, "credenciales guardadas", "")

	writeJSON(w, 200, map[string]any{"ok": true, "mensaje": "WhatsApp conectado de punta a punta.", "pasos": steps})
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func contains(all []string, want string) bool {
	for _, v := range all {
		if v == want {
			return true
		}
	}
	return false
}

// whatsappStatus answers the panel without changing anything: what is
// configured, what Meta has registered and what is still missing.
func (s *Server) whatsappStatus(w http.ResponseWriter, r *http.Request) {
	st := s.Settings.Get()
	cfg := st.WhatsApp
	base, fuente := s.webhookBase(r, st)
	out := map[string]any{
		"configurado": cfg.Enabled(), "app_id": cfg.AppID, "waba_id": cfg.WABAID,
		"webhook_url": base + "/webhook/whatsapp", "webhook_fuente": fuente,
		"app_secret": cfg.AppSecret != "", "verify_token": cfg.VerifyToken != "",
	}
	if cfg.Token == "" || cfg.PhoneNumberID == "" {
		writeJSON(w, 200, out)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), hub.TestTimeout)
	defer cancel()
	cl := newWA(cfg)
	if info, err := cl.Check(ctx); err == nil {
		out["numero"] = info.DisplayPhone
		out["nombre"] = info.VerifiedName
	} else {
		//nolint:errcheck // status is best-effort
		out["error_numero"] = whatsappAdvice(err)
	}
	appID := cfg.AppID
	if appID == "" {
		if id, err := cl.AppID(ctx); err == nil {
			appID = id
			out["app_id"] = id
		}
	}
	if appID != "" && cfg.AppSecret != "" {
		if spec, err := cl.Webhook(ctx, appID, cfg.AppSecret); err == nil {
			out["webhook_meta_url"] = spec.CallbackURL
			out["webhook_activo"] = spec.Active
			out["webhook_campos"] = spec.Fields
			out["webhook_ok"] = spec.Active && spec.CallbackURL == base+"/webhook/whatsapp"
		}
	}
	if cfg.WABAID != "" {
		if subs, err := cl.SubscribedApps(ctx, cfg.WABAID); err == nil {
			out["app_suscrita"] = contains(subs, appID)
			out["apps"] = subs
		}
	}
	writeJSON(w, 200, out)
}
