// Package api exposes the Motor over HTTP: the owner's panel (static files +
// JSON API + live events), the WhatsApp and Wompi webhooks, and the simulator.
package api

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/michiTrader/dropshipper/motor/internal/catalog"
	"github.com/michiTrader/dropshipper/motor/internal/convo"
	"github.com/michiTrader/dropshipper/motor/internal/hub"
	"github.com/michiTrader/dropshipper/motor/internal/rules"
	"github.com/michiTrader/dropshipper/motor/internal/sales"
	"github.com/michiTrader/dropshipper/motor/internal/settings"
	"github.com/michiTrader/dropshipper/motor/internal/shipping"
	"github.com/michiTrader/dropshipper/motor/internal/store"
	"github.com/michiTrader/dropshipper/motor/internal/whatsapp"
)

// Version is the Motor version shown in the panel.
const Version = "0.2.0"

//go:embed web
var webFS embed.FS

// Server holds everything the handlers need.
type Server struct {
	Settings *settings.Manager
	Hub      *hub.Hub
	Store    *store.Store
	Engine   *convo.Engine
	Sales    *sales.Service
	Catalog  *catalog.Catalog
	Zones    *rules.ZoneRules
	Shipping *shipping.Service
	MediaDir string
	Log      *log.Logger
	Started  time.Time
}

// Handler builds the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public: health and webhooks (verified by their own signatures).
	mux.HandleFunc("GET /salud", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]any{"ok": true}) })
	mux.HandleFunc("GET /webhook/whatsapp", s.waVerify)
	mux.HandleFunc("POST /webhook/whatsapp", s.waWebhook)
	mux.HandleFunc("POST /webhook/wompi", s.wompiWebhook)

	// Panel API (admin token).
	api := http.NewServeMux()
	api.HandleFunc("GET /api/estado", s.status)
	api.HandleFunc("GET /api/stats", s.stats)
	api.HandleFunc("GET /api/eventos", s.events)
	s.settingsRoutes(api)
	s.whatsWebRoutes(api)
	s.productRoutes(api)
	api.HandleFunc("GET /api/contactos", s.contacts)
	api.HandleFunc("GET /api/contactos/{phone}", s.contact)
	api.HandleFunc("GET /api/contactos/{phone}/mensajes", s.messages)
	api.HandleFunc("GET /api/contactos/{phone}/auditoria", s.audit)
	api.HandleFunc("POST /api/contactos/{phone}/enviar", s.ownerSend)
	api.HandleFunc("POST /api/contactos/{phone}/pausar", s.pause)
	api.HandleFunc("POST /api/contactos/{phone}/responder", s.forceReply)
	api.HandleFunc("POST /api/contactos/{phone}/leido", s.markRead)
	api.HandleFunc("POST /api/contactos/{phone}/archivar", s.archive)
	api.HandleFunc("GET /api/pedidos", s.orders)
	api.HandleFunc("GET /api/pedidos/{id}", s.order)
	api.HandleFunc("DELETE /api/pedidos/{id}", s.deleteOrder)
	api.HandleFunc("POST /api/pedidos/borrar-simulador", s.deleteSimOrders)
	api.HandleFunc("POST /api/pedidos/{id}/aprobar", s.approve)
	api.HandleFunc("POST /api/pedidos/{id}/rechazar", s.reject)
	api.HandleFunc("POST /api/pedidos/{id}/editar", s.edit)
	api.HandleFunc("POST /api/pedidos/{id}/pago-confirmado", s.paymentOK)
	api.HandleFunc("POST /api/pedidos/{id}/pago-rechazado", s.paymentBad)
	api.HandleFunc("POST /api/pedidos/{id}/despachar", s.dispatch)
	api.HandleFunc("POST /api/pedidos/{id}/estado", s.setStatus)
	api.HandleFunc("GET /api/alertas", s.alerts)
	api.HandleFunc("POST /api/alertas/{id}/resolver", s.resolveAlert)
	api.HandleFunc("POST /api/simulador/mensaje", s.simMessage)
	api.HandleFunc("DELETE /api/simulador/{phone}", s.simReset)
	api.HandleFunc("POST /api/zonas/consultar", s.zoneQuery)
	api.HandleFunc("GET /media/{file}", s.media)
	mux.Handle("/api/", s.auth(api))
	mux.Handle("/media/", s.auth(api))

	// Static panel.
	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("/", noCache(http.FileServerFS(sub)))
	return logRequests(s.Log, mux)
}

// ---------- auth & helpers ----------

// proxyHeaders reveal that a request went through a reverse proxy or tunnel,
// so a "local" connection may in fact come from the internet.
var proxyHeaders = []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Real-Ip", "Forwarded", "Cf-Connecting-Ip", "True-Client-Ip", "X-Forwarded-Proto"}

// isLocal is true only for direct loopback connections without proxy headers.
func isLocal(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return false
	}
	for _, h := range proxyHeaders {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	return true
}

// auth protects the panel API. With a token: Bearer header or ?token=.
// Without a token the panel only works from this very computer, so a Motor
// exposed to the internet by mistake never serves its data to strangers.
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := s.Settings.AdminToken()
		if want == "" {
			if isLocal(r) {
				next.ServeHTTP(w, r)
				return
			}
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error":          "el panel solo está disponible desde este computador hasta que definas un token de acceso (en Conexiones, Seguridad y acceso)",
				"requiere_token": true, "sin_token": true,
			})
			return
		}
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if got == "" {
			got = r.URL.Query().Get("token") // EventSource and <img> cannot set headers
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "token inválido", "requiere_token": true})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	if errors.Is(err, store.ErrNotFound) {
		code = http.StatusNotFound
	}
	writeJSON(w, code, map[string]any{"error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(body))) == 0 {
		return nil
	}
	return json.Unmarshal(body, v)
}

func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("id inválido")
	}
	return id, nil
}

func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		h.ServeHTTP(w, r)
	})
}

func logRequests(lg *log.Logger, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		h.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/webhook/") {
			lg.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}

// ---------- status ----------

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	st, err := s.Store.Stats(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, st)
}

// events streams change notifications to the panel (Server-Sent Events).
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming no soportado", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	ch, stop := s.Engine.Events.Subscribe()
	defer stop()
	fmt.Fprint(w, ": conectado\n\n")
	flusher.Flush()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
	}
}

// ---------- contacts & messages ----------

func (s *Server) contacts(w http.ResponseWriter, r *http.Request) {
	// ?archivados=1 lists the archive; by default the inbox.
	archived := r.URL.Query().Get("archivados") == "1"
	cs, err := s.Store.ListContacts(r.Context(), archived)
	if err != nil {
		writeErr(w, err)
		return
	}
	if cs == nil {
		cs = []store.Contact{}
	}
	writeJSON(w, 200, cs)
}

func (s *Server) contact(w http.ResponseWriter, r *http.Request) {
	phone := r.PathValue("phone")
	c, err := s.Store.GetContact(r.Context(), phone)
	if err != nil {
		writeErr(w, err)
		return
	}
	orders, _ := s.Store.OrdersFor(r.Context(), phone)
	if orders == nil {
		orders = []store.Order{}
	}
	writeJSON(w, 200, map[string]any{"contacto": c, "pedidos": orders})
}

func (s *Server) messages(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	ms, err := s.Store.Messages(r.Context(), r.PathValue("phone"), after, 1000)
	if err != nil {
		writeErr(w, err)
		return
	}
	if ms == nil {
		ms = []store.Message{}
	}
	writeJSON(w, 200, ms)
}

func (s *Server) audit(w http.ResponseWriter, r *http.Request) {
	a, err := s.Store.AuditFor(r.Context(), r.PathValue("phone"), 200)
	if err != nil {
		writeErr(w, err)
		return
	}
	if a == nil {
		a = []store.AuditEntry{}
	}
	writeJSON(w, 200, a)
}

func (s *Server) ownerSend(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Engine.OwnerSend(r.Context(), r.PathValue("phone"), in.Text); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) pause(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Paused bool `json:"paused"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Engine.SetPaused(r.Context(), r.PathValue("phone"), in.Paused); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "paused": in.Paused})
}

func (s *Server) forceReply(w http.ResponseWriter, r *http.Request) {
	phone := r.PathValue("phone")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		s.Engine.Reply(ctx, phone)
	}()
	writeJSON(w, 202, map[string]any{"ok": true})
}

func (s *Server) markRead(w http.ResponseWriter, r *http.Request) {
	_ = s.Store.MarkRead(r.Context(), r.PathValue("phone"))
	writeJSON(w, 200, map[string]any{"ok": true})
}

// archive moves a chat to the archive (or back to the inbox). Unlike
// DeleteContact it never loses anything: the history is still there.
func (s *Server) archive(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Archivado bool `json:"archivado"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	phone := r.PathValue("phone")
	if err := s.Store.SetArchived(r.Context(), phone, in.Archivado); err != nil {
		writeErr(w, err)
		return
	}
	action := "chat_desarchivado"
	if in.Archivado {
		action = "chat_archivado"
	}
	s.Store.Audit(r.Context(), phone, "dueno", action, "")
	s.Engine.Events.Publish(convo.Event{Type: "contacto", Phone: phone})
	writeJSON(w, 200, map[string]any{"ok": true, "archivado": in.Archivado})
}

// ---------- orders ----------

func (s *Server) orders(w http.ResponseWriter, r *http.Request) {
	var statuses []string
	if v := r.URL.Query().Get("estado"); v != "" {
		statuses = strings.Split(v, ",")
	}
	os, err := s.Store.ListOrders(r.Context(), statuses...)
	if err != nil {
		writeErr(w, err)
		return
	}
	if os == nil {
		os = []store.Order{}
	}
	writeJSON(w, 200, os)
}

// deleteOrder erases one order (test orders). The customer and the chat stay.
func (s *Server) deleteOrder(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	o, err := s.Store.DeleteOrder(r.Context(), id, "dueno")
	if err != nil {
		writeErr(w, err)
		return
	}
	s.Engine.Events.Publish(convo.Event{Type: "pedido", Phone: o.Phone, ID: o.ID})
	writeJSON(w, 200, map[string]any{"ok": true, "en_dropi": o.DropiOrderID != ""})
}

// deleteSimOrders cleans every order born in the simulator.
func (s *Server) deleteSimOrders(w http.ResponseWriter, r *http.Request) {
	n, err := s.Store.DeleteSimOrders(r.Context(), "dueno")
	if err != nil {
		writeErr(w, err)
		return
	}
	s.Engine.Events.Publish(convo.Event{Type: "pedido"})
	writeJSON(w, 200, map[string]any{"ok": true, "borrados": n})
}

func (s *Server) order(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	o, err := s.Store.GetOrder(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"pedido": o, "faltan": sales.Missing(o), "resumen": s.Sales.Summary(o)})
}

func (s *Server) orderAction(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context, id int64) (store.Order, error)) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	o, err := fn(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.Engine.Events.Publish(convo.Event{Type: "pedido", Phone: o.Phone, ID: o.ID})
	writeJSON(w, 200, o)
}

func (s *Server) reject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Motivo string `json:"motivo"`
	}
	_ = readJSON(r, &in)
	s.orderAction(w, r, func(ctx context.Context, id int64) (store.Order, error) {
		return s.Sales.Reject(ctx, id, in.Motivo, "dueno")
	})
}

func (s *Server) edit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		sales.DraftInput
		Riesgo string `json:"riesgo"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	s.orderAction(w, r, func(ctx context.Context, id int64) (store.Order, error) {
		return s.Sales.OwnerEdit(ctx, id, in.DraftInput, in.Riesgo)
	})
}

func (s *Server) paymentOK(w http.ResponseWriter, r *http.Request) {
	s.orderAction(w, r, func(ctx context.Context, id int64) (store.Order, error) {
		return s.Sales.ConfirmPayment(ctx, id, "dueno")
	})
}

func (s *Server) paymentBad(w http.ResponseWriter, r *http.Request) {
	s.orderAction(w, r, func(ctx context.Context, id int64) (store.Order, error) {
		return s.Sales.RejectPayment(ctx, id, "dueno")
	})
}

func (s *Server) dispatch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DropiID        string `json:"dropi_id"`
		Guia           string `json:"guia"`
		Transportadora string `json:"transportadora"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	s.orderAction(w, r, func(ctx context.Context, id int64) (store.Order, error) {
		return s.Sales.Dispatch(ctx, id, in.DropiID, in.Guia, in.Transportadora, "dueno")
	})
}

func (s *Server) setStatus(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Estado string `json:"estado"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	s.orderAction(w, r, func(ctx context.Context, id int64) (store.Order, error) {
		return s.Sales.SetStatus(ctx, id, in.Estado, "dueno")
	})
}

// ---------- alerts ----------

func (s *Server) alerts(w http.ResponseWriter, r *http.Request) {
	as, err := s.Store.OpenAlerts(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	if as == nil {
		as = []store.Alert{}
	}
	writeJSON(w, 200, as)
}

func (s *Server) resolveAlert(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Store.ResolveAlert(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	s.Engine.Events.Publish(convo.Event{Type: "alerta"})
	writeJSON(w, 200, map[string]any{"ok": true})
}

// ---------- simulator ----------

func (s *Server) simMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Phone string `json:"phone"`
		Name  string `json:"name"`
		Text  string `json:"text"`
		Kind  string `json:"kind"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	in.Phone = strings.TrimSpace(in.Phone)
	if !strings.HasPrefix(in.Phone, "sim-") {
		in.Phone = "sim-" + whatsapp.SafeName(in.Phone)
	}
	if in.Kind == "" {
		in.Kind = "text"
	}
	if in.Kind == "text" && strings.TrimSpace(in.Text) == "" {
		writeErr(w, errors.New("mensaje vacío"))
		return
	}
	err := s.Engine.HandleInbound(r.Context(), convo.Inbound{
		Phone: in.Phone, Name: in.Name, Kind: in.Kind, Text: in.Text, Simulated: true, At: time.Now().UTC(),
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "phone": in.Phone})
}

func (s *Server) simReset(w http.ResponseWriter, r *http.Request) {
	phone := r.PathValue("phone")
	if !strings.HasPrefix(phone, "sim-") {
		writeErr(w, errors.New("solo se pueden borrar chats simulados"))
		return
	}
	if err := s.Store.DeleteContact(r.Context(), phone); err != nil {
		writeErr(w, err)
		return
	}
	s.Engine.Events.Publish(convo.Event{Type: "contacto", Phone: phone})
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) zoneQuery(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Departamento string `json:"departamento"`
		Ciudad       string `json:"ciudad"`
		Barrio       string `json:"barrio"`
		Total        int64  `json:"total"`
		SKU          string `json:"producto_sku"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if in.Total <= 0 {
		p := s.Catalog.Default()
		if sku := strings.TrimSpace(in.SKU); sku != "" {
			if q, ok := s.Catalog.Get(sku); ok {
				p = q
			}
		}
		if len(p.Offers) == 0 {
			writeErr(w, errors.New("no hay productos con ofertas para calcular el valor"))
			return
		}
		in.Total = p.Offers[0].Price
	}
	writeJSON(w, 200, s.Zones.Decide(rules.Destination{Department: in.Departamento, City: in.Ciudad, Neighborhood: in.Barrio}, in.Total))
}

func (s *Server) media(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.PathValue("file"))
	if name == "." || name == "/" || strings.Contains(name, "..") {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(s.MediaDir, name)
	if _, err := os.Stat(path); err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, path)
}
