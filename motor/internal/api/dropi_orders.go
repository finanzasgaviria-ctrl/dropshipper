package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/michiTrader/dropshipper/motor/internal/catalog"
	"github.com/michiTrader/dropshipper/motor/internal/config"
	"github.com/michiTrader/dropshipper/motor/internal/convo"
	"github.com/michiTrader/dropshipper/motor/internal/rules"
	"github.com/michiTrader/dropshipper/motor/internal/store"
)

// dropiCreateMu serialises order creation in Dropi: two clicks (or a click and
// the automatic creation on approval) must never produce two orders.
var dropiCreateMu sync.Mutex

func (s *Server) dropiOrderRoutes(api *http.ServeMux) {
	api.HandleFunc("POST /api/pedidos/{id}/crear-en-dropi", s.createInDropiHandler)
	api.HandleFunc("POST /api/pedidos/{id}/transportadora", s.setCarrier)
}

// DropiOutcome tells the panel what happened with the automatic Dropi order.
type DropiOutcome struct {
	// Status: "creado" | "no_automatico" | "error"
	Status  string `json:"estado"`
	Message string `json:"mensaje"`
}

// orderWithDropi is the order plus the outcome of the Dropi step.
type orderWithDropi struct {
	store.Order
	Dropi *DropiOutcome `json:"dropi,omitempty"`
}

func (s *Server) setCarrier(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Carrier string `json:"transportadora"`
	}
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	s.orderAction(w, r, func(ctx context.Context, id int64) (store.Order, error) {
		return s.Sales.SetCarrier(ctx, id, in.Carrier, "dueno")
	})
}

// approve is the owner's "Aprobar" button. When Dropi is connected and the
// owner chose "crear los pedidos aprobados directamente en Dropi", the order is
// also created there. A Dropi failure NEVER undoes the approval (the customer
// was already told): the order stays approved and the panel explains what to do.
func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	o, err := s.Sales.Approve(r.Context(), id, "dueno")
	if err != nil {
		writeErr(w, err)
		return
	}
	out := orderWithDropi{Order: o}
	if s.Settings.Get().Negocio.Dropi.Modo == "api" {
		o2, oc := s.autoCreateInDropi(r.Context(), o)
		out.Order, out.Dropi = o2, &oc
	} else {
		out.Dropi = &DropiOutcome{Status: "no_automatico", Message: "Dropi está en modo manual: copia los datos y crea la orden en Dropi."}
	}
	s.Engine.Events.Publish(convo.Event{Type: "pedido", Phone: out.Phone, ID: out.ID})
	writeJSON(w, 200, out)
}

// createInDropiHandler is the manual retry button ("Crear en Dropi").
func (s *Server) createInDropiHandler(w http.ResponseWriter, r *http.Request) {
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
	o2, err := s.CreateInDropi(r.Context(), o)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.Engine.Events.Publish(convo.Event{Type: "pedido", Phone: o2.Phone, ID: o2.ID})
	writeJSON(w, 200, orderWithDropi{Order: o2, Dropi: &DropiOutcome{Status: "creado", Message: createdMsg(o2)}})
}

func createdMsg(o store.Order) string {
	m := fmt.Sprintf("Orden %s creada en Dropi con %s.", o.DropiOrderID, o.Carrier)
	if o.TrackingNumber == "" {
		m += " Dropi aún no entregó la guía: cuando salga, regístrala en \"Registrar despacho\" y el cliente la recibe por WhatsApp."
	}
	return m
}

// autoCreateInDropi is CreateInDropi turned into a report the panel can show.
func (s *Server) autoCreateInDropi(ctx context.Context, o store.Order) (store.Order, DropiOutcome) {
	o2, err := s.CreateInDropi(ctx, o)
	if err != nil {
		_, _ = s.Store.AddAlert(ctx, store.Alert{Phone: o.Phone, OrderID: o.ID, Kind: store.AlertError,
			Message: fmt.Sprintf("El pedido #%d quedó aprobado pero NO se creó en Dropi: %s", o.ID, err.Error())})
		return o, DropiOutcome{Status: "error", Message: err.Error()}
	}
	return o2, DropiOutcome{Status: "creado", Message: createdMsg(o2)}
}

// CreateInDropi creates the order in Dropi and records its id (and the guide
// when Dropi already has it). It refuses to run twice for the same order.
func (s *Server) CreateInDropi(ctx context.Context, o store.Order) (store.Order, error) {
	dropiCreateMu.Lock()
	defer dropiCreateMu.Unlock()

	// Re-read inside the lock: a concurrent request may have created it.
	cur, err := s.Store.GetOrder(ctx, o.ID)
	if err != nil {
		return o, err
	}
	o = cur
	if strings.TrimSpace(o.DropiOrderID) != "" {
		return o, fmt.Errorf("el pedido #%d ya tiene la orden %s en Dropi; no se crea otra para no duplicarlo", o.ID, o.DropiOrderID)
	}
	if o.Status != store.OrderApproved {
		return o, fmt.Errorf("el pedido #%d está en estado %q: solo se crean en Dropi los pedidos aprobados", o.ID, o.Status)
	}
	client := s.Hub.Dropi()
	if client == nil {
		return o, errors.New("Dropi no está conectado (Conexiones > Dropi)")
	}
	st := s.Settings.Get()
	if st.Dropi.Sandbox {
		return o, errors.New("Dropi está en entorno de pruebas: desmárcalo en Conexiones > Dropi para crear pedidos reales")
	}
	p, ok := s.Catalog.Get(o.ProductSKU)
	if !ok {
		return o, fmt.Errorf("el producto %q ya no existe en el catálogo", o.ProductSKU)
	}
	if err := checkDropiReady(o, p); err != nil {
		return o, err
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := client.CreateOrder(cctx, buildRealOrderPayload(o, p, st.Dropi))
	if err != nil {
		return o, dropiCreateError(o, err)
	}
	carrier := o.Carrier
	if res.Carrier != "" {
		if t, ok := s.Settings.Get().Negocio.Carrier(res.Carrier); ok {
			carrier = t.Nombre
		} else {
			carrier = res.Carrier
		}
	}
	// Dispatch records the id, moves the order to "en_dropi" and, only when a
	// guide already exists, tells the customer.
	return s.Sales.Dispatch(ctx, o.ID, res.ID, res.Tracking, carrier, "dropi_api")
}

// dropiCreateError explains a failed creation. A timeout is special: Dropi may
// have created the order anyway, so the owner must look before retrying.
func dropiCreateError(o store.Order, err error) error {
	low := strings.ToLower(err.Error())
	if strings.Contains(low, "timeout") || strings.Contains(low, "deadline") || strings.Contains(low, "eof") {
		return fmt.Errorf("Dropi no respondió a tiempo. Puede que SÍ haya creado la orden: revisa en Dropi si existe el pedido de %s antes de volver a intentarlo (para no duplicarlo)", o.CustomerName)
	}
	return errors.New(dropiAdvice(err))
}

// checkDropiReady lists, before calling Dropi, what the order lacks so the
// owner fixes it instead of reading a cryptic API error.
func checkDropiReady(o store.Order, p catalog.Product) error {
	var miss []string
	add := func(c bool, what string) {
		if c {
			miss = append(miss, what)
		}
	}
	add(strings.TrimSpace(p.DropiProductID) == "", "el ID de Dropi del producto (Productos)")
	add(strings.TrimSpace(o.CustomerName) == "", "nombre del cliente")
	add(strings.TrimSpace(o.CustomerPhone) == "", "celular")
	add(strings.TrimSpace(o.Department) == "" || strings.TrimSpace(o.City) == "", "departamento y ciudad")
	add(strings.TrimSpace(o.Address) == "", "dirección")
	add(strings.TrimSpace(o.Carrier) == "", "transportadora")
	if len(miss) > 0 {
		return fmt.Errorf("falta para crear la orden en Dropi: %s", strings.Join(miss, ", "))
	}
	return nil
}

// buildRealOrderPayload is the body for /orders/myorders built from a real
// order. Field names follow Dropi's integration docs and have NOT been verified
// against a live account: check the first real order in Dropi.
func buildRealOrderPayload(o store.Order, p catalog.Product, dc config.DropiConfig) map[string]any {
	name, surname := splitName(o.CustomerName)
	rate, total := "CON RECAUDO", o.CODAmount
	if o.PaymentPolicy == rules.PayFullAdvance || o.CODAmount <= 0 {
		rate, total = "SIN RECAUDO", 0
	}
	addr := strings.TrimSpace(o.Address)
	if o.Neighborhood != "" {
		addr += ", barrio " + o.Neighborhood
	}
	notes := []string{fmt.Sprintf("Pedido #%d del Motor", o.ID)}
	if o.AddressRefs != "" {
		notes = append(notes, "Referencias: "+o.AddressRefs)
	}
	if o.AdvanceAmount > 0 {
		notes = append(notes, fmt.Sprintf("Anticipo ya pagado: $%s", catalog.FormatCOP(o.AdvanceAmount)))
	}
	if o.Notes != "" {
		notes = append(notes, o.Notes)
	}
	email := strings.TrimSpace(o.Email)
	return map[string]any{
		"type": "FINAL_ORDER", "rate_type": rate, "total_order": total,
		"name": name, "surname": surname, "phone": o.CustomerPhone, "client_email": email,
		"country": "COLOMBIA", "state": o.Department, "city": o.City, "dir": addr,
		"distributionCompany": o.Carrier,
		"notes":               strings.Join(notes, " | "),
		"products":            []map[string]any{{"id": p.DropiProductID, "price": o.Total / int64(max1(o.Units)), "quantity": o.Units}},
		"origen":              strings.TrimSpace(dc.OriginCity + " " + dc.OriginDept),
	}
}

// splitName gives Dropi a first name and a surname ("Ana María Pérez Gómez" ->
// "Ana María", "Pérez Gómez").
func splitName(full string) (name, surname string) {
	f := strings.Fields(full)
	switch len(f) {
	case 0:
		return "", ""
	case 1:
		return f[0], "."
	case 2:
		return f[0], f[1]
	case 3:
		return f[0], strings.Join(f[1:], " ")
	}
	return strings.Join(f[:2], " "), strings.Join(f[2:], " ")
}
