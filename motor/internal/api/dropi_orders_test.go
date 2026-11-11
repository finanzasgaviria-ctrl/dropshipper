package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/michiTrader/dropshipper/motor/internal/dropi"
	"github.com/michiTrader/dropshipper/motor/internal/store"
)

// dropiOrdersFake is a Dropi that also accepts orders and counts them.
type dropiOrdersFake struct {
	mu      sync.Mutex
	created int
	last    map[string]any
	failing bool
}

func (f *dropiOrdersFake) count() int { f.mu.Lock(); defer f.mu.Unlock(); return f.created }

func newDropiOrdersFake(t *testing.T, token string) *dropiOrdersFake {
	t.Helper()
	f := &dropiOrdersFake{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("dropi-integration-key") != token {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"isSuccess":false,"message":"Access denied"}`))
			return
		}
		switch r.URL.Path {
		case "/department":
			_, _ = w.Write([]byte(`{"isSuccess":true,"objects":[{"id":1,"name":"Antioquia"}]}`))
		case "/orders/cotizaEnvioTransportadoraV2":
			_, _ = w.Write([]byte(`{"isSuccess":true,"objects":[
				{"distribution_company":{"name":"COORDINADORA"},"shipping_amount":19000,"delivery_days":2},
				{"distribution_company":{"name":"ENVIA"},"shipping_amount":14000,"delivery_days":5}]}`))
		case "/products/v2/77":
			_, _ = w.Write([]byte(`{"isSuccess":true,"objects":{"id":77,"name":"Masajeador","sale_price":26000,"stock":42}}`))
		case "/orders/myorders":
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.failing {
				w.WriteHeader(500)
				_, _ = w.Write([]byte(`{"isSuccess":false,"message":"boom"}`))
				return
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &f.last)
			f.created++
			_, _ = w.Write([]byte(`{"isSuccess":true,"objects":{"id":9912,"distribution_company":{"name":"ENVIA"}}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	old := dropi.BaseURLOverride
	dropi.BaseURLOverride = srv.URL
	t.Cleanup(func() { dropi.BaseURLOverride = old })
	return f
}

// pendingOrder saves a complete order waiting for approval and returns its id.
func pendingOrder(t *testing.T, e *env, status string, dropiProductID string) int64 {
	t.Helper()
	p := e.cat.Default()
	p.DropiProductID = dropiProductID
	if _, err := e.cat.Save(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	of := p.Offers[0]
	o, err := e.srv.Store.SaveOrder(t.Context(), store.Order{
		Phone: "573001112233", Status: status, ProductSKU: p.SKU, ProductName: p.Name,
		OfferID: of.ID, OfferLabel: of.Label, Units: of.Units, Total: of.Price,
		CustomerName: "Ana María Pérez Gómez", Email: "ana@example.com", CustomerPhone: "3001112233",
		Department: "Antioquia", City: "Medellín", Neighborhood: "Laureles", Address: "Calle 10 # 20-30",
		Carrier: "Envía", CODAmount: of.Price, PaymentPolicy: "contraentrega",
	})
	if err != nil {
		t.Fatal(err)
	}
	return o.ID
}

func orderURL(id int64, action string) string {
	return "/api/pedidos/" + strconv.FormatInt(id, 10) + "/" + action
}

func TestApproveCreatesTheOrderInDropiOnlyOnce(t *testing.T) {
	f := newDropiOrdersFake(t, "tok-dropi-123456")
	e := newEnv(t, "")
	connectDropi(t, e, "tok-dropi-123456") // also switches Dropi to "api" mode
	id := pendingOrder(t, e, store.OrderPendingApprove, "555")

	r := e.do("POST", orderURL(id, "aprobar"), nil)
	if r.Code != 200 {
		t.Fatalf("aprobar: %d %s", r.Code, r.Body)
	}
	out := r.json(t)
	d := out["dropi"].(map[string]any)
	if d["estado"] != "creado" || out["status"] != store.OrderInDropi || out["dropi_order_id"] != "9912" {
		t.Fatalf("debía quedar creado en Dropi: %+v", out)
	}
	if f.count() != 1 {
		t.Fatalf("se esperaba 1 orden en Dropi, hay %d", f.count())
	}
	// What was sent follows the order: name split, COD, product id, carrier.
	if f.last["name"] != "Ana María" || f.last["surname"] != "Pérez Gómez" || f.last["rate_type"] != "CON RECAUDO" || f.last["distributionCompany"] != "Envía" {
		t.Fatalf("payload: %+v", f.last)
	}
	if prods := f.last["products"].([]any); prods[0].(map[string]any)["id"] != "555" {
		t.Fatalf("producto: %+v", f.last["products"])
	}

	// A second click (or a retry) must NEVER create a second order.
	r = e.do("POST", orderURL(id, "crear-en-dropi"), nil)
	if r.Code != 400 || !strings.Contains(r.Body, "no se crea otra") {
		t.Fatalf("segundo intento: %d %s", r.Code, r.Body)
	}
	if f.count() != 1 {
		t.Fatalf("no debe duplicarse: %d órdenes", f.count())
	}
}

func TestDropiFailureDoesNotUndoTheApproval(t *testing.T) {
	f := newDropiOrdersFake(t, "tok-dropi-123456")
	f.failing = true
	e := newEnv(t, "")
	connectDropi(t, e, "tok-dropi-123456")
	id := pendingOrder(t, e, store.OrderPendingApprove, "555")

	r := e.do("POST", orderURL(id, "aprobar"), nil)
	if r.Code != 200 {
		t.Fatalf("aprobar debe responder 200 aunque Dropi falle: %d %s", r.Code, r.Body)
	}
	out := r.json(t)
	if out["status"] != store.OrderApproved || out["dropi"].(map[string]any)["estado"] != "error" {
		t.Fatalf("debía quedar aprobado con error de Dropi: %+v", out)
	}
	alerts, _ := e.srv.Store.OpenAlerts(t.Context())
	found := false
	for _, a := range alerts {
		found = found || (a.OrderID == id && strings.Contains(a.Message, "NO se creó en Dropi"))
	}
	if !found {
		t.Fatalf("debe dejar una alerta para que el dueño lo vea: %+v", alerts)
	}

	// Once Dropi works again the retry button creates it.
	f.mu.Lock()
	f.failing = false
	f.mu.Unlock()
	r = e.do("POST", orderURL(id, "crear-en-dropi"), nil)
	if r.Code != 200 || f.count() != 1 {
		t.Fatalf("reintento: %d %s (órdenes=%d)", r.Code, r.Body, f.count())
	}
}

func TestManualModeNeverCallsDropiOrders(t *testing.T) {
	f := newDropiOrdersFake(t, "tok-dropi-123456")
	e := newEnv(t, "")
	connectDropi(t, e, "tok-dropi-123456")
	if r := e.do("PUT", "/api/ajustes/negocio", map[string]any{"dropi": map[string]any{"modo": "manual"}}); r.Code != 200 {
		t.Fatalf("modo manual: %d %s", r.Code, r.Body)
	}
	id := pendingOrder(t, e, store.OrderPendingApprove, "555")
	out := e.do("POST", orderURL(id, "aprobar"), nil).json(t)
	if out["status"] != store.OrderApproved || out["dropi"].(map[string]any)["estado"] != "no_automatico" || f.count() != 0 {
		t.Fatalf("modo manual no debe crear nada en Dropi: %+v (órdenes=%d)", out, f.count())
	}
}

func TestCreateInDropiExplainsWhatIsMissing(t *testing.T) {
	f := newDropiOrdersFake(t, "tok-dropi-123456")
	e := newEnv(t, "")
	connectDropi(t, e, "tok-dropi-123456")
	id := pendingOrder(t, e, store.OrderApproved, "") // product without Dropi id
	r := e.do("POST", orderURL(id, "crear-en-dropi"), nil)
	if r.Code != 400 || !strings.Contains(r.Body, "ID de Dropi del producto") || f.count() != 0 {
		t.Fatalf("debía explicar qué falta sin llamar a Dropi: %d %s", r.Code, r.Body)
	}
	// Not approved yet: refuse.
	id2 := pendingOrder(t, e, store.OrderPendingApprove, "555")
	if r := e.do("POST", orderURL(id2, "crear-en-dropi"), nil); r.Code != 400 || !strings.Contains(r.Body, "aprobados") || f.count() != 0 {
		t.Fatalf("sin aprobar: %d %s", r.Code, r.Body)
	}
}

func TestSandboxNeverCreatesRealOrders(t *testing.T) {
	f := newDropiOrdersFake(t, "tok-dropi-123456")
	e := newEnv(t, "")
	if r := e.do("PUT", "/api/ajustes/dropi", map[string]any{"token": "tok-dropi-123456", "sandbox": true, "origen_departamento": "Cundinamarca", "origen_ciudad": "Bogotá"}); r.Code != 200 {
		t.Fatalf("guardar: %d %s", r.Code, r.Body)
	}
	id := pendingOrder(t, e, store.OrderApproved, "555")
	r := e.do("POST", orderURL(id, "crear-en-dropi"), nil)
	if r.Code != 400 || !strings.Contains(r.Body, "entorno de pruebas") || f.count() != 0 {
		t.Fatalf("sandbox: %d %s", r.Code, r.Body)
	}
}

func TestOwnerCanPickTheCarrierAndItSticks(t *testing.T) {
	newDropiOrdersFake(t, "tok-dropi-123456")
	e := newEnv(t, "")
	connectDropi(t, e, "tok-dropi-123456")
	id := pendingOrder(t, e, store.OrderPendingApprove, "555")

	// An edit prices the order with Dropi's quotes (Envía is cheaper: 14.000).
	if r := e.do("POST", orderURL(id, "editar"), map[string]any{"ciudad": "Medellín"}); r.Code != 200 {
		t.Fatalf("editar: %d %s", r.Code, r.Body)
	}
	o, _ := e.srv.Store.GetOrder(t.Context(), id)
	if o.Carrier != "Envía" {
		t.Fatalf("recomendada: %s", o.Carrier)
	}

	// The owner prefers Coordinadora (2 days).
	r := e.do("POST", orderURL(id, "transportadora"), map[string]any{"transportadora": "coordinadora"})
	if r.Code != 200 {
		t.Fatalf("elegir: %d %s", r.Code, r.Body)
	}
	o, _ = e.srv.Store.GetOrder(t.Context(), id)
	var pr map[string]any
	_ = json.Unmarshal([]byte(o.ProfitJSON), &pr)
	if o.Carrier != "Coordinadora" || pr["flete"].(float64) != 19000 || pr["transportadora_manual"] != "coordinadora" || pr["dias_entrega"].(float64) != 2 {
		t.Fatalf("elegida a mano: carrier=%s %+v", o.Carrier, pr)
	}

	// Editing the address later must NOT silently undo the owner's choice.
	e.do("POST", orderURL(id, "editar"), map[string]any{"direccion": "Carrera 5 # 6-7"})
	o, _ = e.srv.Store.GetOrder(t.Context(), id)
	if o.Carrier != "Coordinadora" {
		t.Fatalf("la elección manual debe conservarse: %s", o.Carrier)
	}

	// Back to automatic.
	e.do("POST", orderURL(id, "transportadora"), map[string]any{"transportadora": ""})
	o, _ = e.srv.Store.GetOrder(t.Context(), id)
	if o.Carrier != "Envía" {
		t.Fatalf("automática: %s", o.Carrier)
	}

	// A carrier that does not exist, or one Dropi did not quote, is refused.
	if r := e.do("POST", orderURL(id, "transportadora"), map[string]any{"transportadora": "noexiste"}); r.Code != 400 {
		t.Fatalf("inexistente: %d %s", r.Code, r.Body)
	}
	if r := e.do("POST", orderURL(id, "transportadora"), map[string]any{"transportadora": "tcc"}); r.Code != 400 || !strings.Contains(r.Body, "no cubre") {
		t.Fatalf("sin cobertura: %d %s", r.Code, r.Body)
	}
	o, _ = e.srv.Store.GetOrder(t.Context(), id)
	if o.Carrier != "Envía" {
		t.Fatalf("un intento fallido no debe cambiar el pedido: %s", o.Carrier)
	}
}

func TestSplitName(t *testing.T) {
	for in, want := range map[string][2]string{
		"Ana":                    {"Ana", "."},
		"Ana Pérez":              {"Ana", "Pérez"},
		"Ana Pérez Gómez":        {"Ana", "Pérez Gómez"},
		"Ana María Pérez Gómez":  {"Ana María", "Pérez Gómez"},
		"  Luis   Carlos  Diaz ": {"Luis", "Carlos Diaz"},
	} {
		if n, s := splitName(in); n != want[0] || s != want[1] {
			t.Fatalf("%q -> %q / %q", in, n, s)
		}
	}
}

func TestDeleteTestOrders(t *testing.T) {
	e := newEnv(t, "")
	ctx := t.Context()
	real := pendingOrder(t, e, store.OrderPendingApprove, "555")
	sim1, _ := e.srv.Store.SaveOrder(ctx, store.Order{Phone: "sim-111", Status: store.OrderPendingApprove, ProductSKU: "x", ProductName: "x", Units: 1, Total: 1000})
	sim2, _ := e.srv.Store.SaveOrder(ctx, store.Order{Phone: "sim-222", Status: store.OrderApproved, ProductSKU: "x", ProductName: "x", Units: 1, Total: 1000})
	simDropi, _ := e.srv.Store.SaveOrder(ctx, store.Order{Phone: "sim-333", Status: store.OrderInDropi, ProductSKU: "x", ProductName: "x", Units: 1, Total: 1000, DropiOrderID: "77"})
	_, _ = e.srv.Store.AddAlert(ctx, store.Alert{Phone: "sim-111", OrderID: sim1.ID, Kind: "x", Message: "m"})

	// Bulk clean: removes simulator orders, keeps real ones and those already in Dropi.
	r := e.do("POST", "/api/pedidos/borrar-simulador", nil)
	if r.Code != 200 || r.json(t)["borrados"] != float64(2) {
		t.Fatalf("limpiar: %d %s", r.Code, r.Body)
	}
	if _, err := e.srv.Store.GetOrder(ctx, sim1.ID); err == nil {
		t.Fatal("el pedido del simulador debía borrarse")
	}
	if _, err := e.srv.Store.GetOrder(ctx, sim2.ID); err == nil {
		t.Fatal("el segundo pedido del simulador debía borrarse")
	}
	if _, err := e.srv.Store.GetOrder(ctx, real); err != nil {
		t.Fatal("el pedido real no se toca")
	}
	if _, err := e.srv.Store.GetOrder(ctx, simDropi.ID); err != nil {
		t.Fatal("un pedido que ya está en Dropi no se borra en bloque")
	}
	if a, _ := e.srv.Store.OpenAlerts(ctx); len(a) != 0 {
		t.Fatalf("las alertas del pedido borrado deben irse: %+v", a)
	}

	// Single delete works, and a missing order is a clean error.
	r = e.do("DELETE", "/api/pedidos/"+strconv.FormatInt(real, 10), nil)
	if r.Code != 200 {
		t.Fatalf("borrar uno: %d %s", r.Code, r.Body)
	}
	if r = e.do("DELETE", "/api/pedidos/"+strconv.FormatInt(real, 10), nil); r.Code == 200 {
		t.Fatal("borrar uno inexistente debe fallar")
	}
}
