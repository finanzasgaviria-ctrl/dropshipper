package api

import (
	"strconv"
	"strings"
	"testing"
)

func TestHunterCandidateLifecycle(t *testing.T) {
	e := newEnv(t, "")

	// Unknown numbers stay unknown (-1) and the verdict asks for them.
	r := e.do("POST", "/api/candidatos", map[string]any{"nombre": "Masajeador de cuello", "costo": 25000, "precio_venta": 89900, "flete": 16000})
	if r.Code != 200 {
		t.Fatalf("crear: %d %s", r.Code, r.Body)
	}
	c := r.json(t)
	id := strconv.FormatInt(int64(c["id"].(float64)), 10)
	if c["ventas_7d"] != float64(-1) || c["stock"] != float64(-1) || c["veredicto"].(map[string]any)["nivel"] != "dudoso" {
		t.Fatalf("lo desconocido debe quedar -1 y dudoso: %s", r.Body)
	}

	// Live calculation without saving.
	r = e.do("POST", "/api/candidatos/calcular", map[string]any{"nombre": "x", "costo": 60000, "precio_venta": 70000, "flete": 16000, "stock": 5, "ventas_7d": 30})
	if r.json(t)["veredicto"].(map[string]any)["nivel"] != "no" {
		t.Fatalf("debía rechazarlo: %s", r.Body)
	}

	// Update with real data: now it is good.
	r = e.do("PUT", "/api/candidatos/"+id, map[string]any{"nombre": "Masajeador de cuello", "costo": 25000, "precio_venta": 89900, "flete": 16000, "stock": 90, "ventas_7d": 35})
	if r.Code != 200 || r.json(t)["veredicto"].(map[string]any)["nivel"] != "bueno" {
		t.Fatalf("actualizar: %d %s", r.Code, r.Body)
	}

	// The owner's rules change the verdict: a stricter minimum of ad budget.
	if r = e.do("PUT", "/api/ajustes/negocio", map[string]any{"cazador": map[string]any{"min_ventas_7d": 50, "cpa_minimo": 15000}}); r.Code != 200 {
		t.Fatalf("reglas: %d %s", r.Code, r.Body)
	}
	r = e.do("GET", "/api/candidatos", nil)
	if !strings.Contains(r.Body, `"nivel":"dudoso"`) {
		t.Fatalf("con 50 ventas mínimas debía quedar dudoso: %s", r.Body)
	}

	// Turn it into a product sheet: created OFF, once only.
	r = e.do("POST", "/api/candidatos/"+id+"/crear-producto", nil)
	if r.Code != 200 {
		t.Fatalf("crear producto: %d %s", r.Code, r.Body)
	}
	sku := r.json(t)["sku"].(string)
	if sku != "MASAJEADOR-DE-CUELLO" {
		t.Fatalf("sku: %q", sku)
	}
	p, ok := e.srv.Catalog.Get(sku)
	if !ok || p.Active || p.UnitCost != 25000 || p.Offers[0].Price != 89900 {
		t.Fatalf("la ficha debe quedar apagada y con los números: %+v", p)
	}
	if r = e.do("POST", "/api/candidatos/"+id+"/crear-producto", nil); r.Code != 400 || !strings.Contains(r.Body, "ya se creó") {
		t.Fatalf("no debe duplicarse: %d %s", r.Code, r.Body)
	}

	// Delete; deleting a missing one is a clean 404.
	if r = e.do("DELETE", "/api/candidatos/"+id, nil); r.Code != 200 {
		t.Fatalf("borrar: %d %s", r.Code, r.Body)
	}
	if r = e.do("DELETE", "/api/candidatos/"+id, nil); r.Code != 404 {
		t.Fatalf("borrar inexistente: %d", r.Code)
	}
	if _, ok := e.srv.Catalog.Get(sku); !ok {
		t.Fatal("borrar el candidato no debe borrar la ficha ya creada")
	}
}

func TestHunterNeedsNameAndDropi(t *testing.T) {
	e := newEnv(t, "")
	if r := e.do("POST", "/api/candidatos", map[string]any{"costo": 1}); r.Code != 400 {
		t.Fatalf("sin nombre: %d %s", r.Code, r.Body)
	}
	r := e.do("POST", "/api/candidatos", map[string]any{"nombre": "A", "costo": 1000, "precio_venta": 5000})
	id := strconv.FormatInt(int64(r.json(t)["id"].(float64)), 10)
	if r = e.do("POST", "/api/candidatos/"+id+"/traer-dropi", nil); r.Code != 400 || !strings.Contains(r.Body, "conecta Dropi") {
		t.Fatalf("sin Dropi: %d %s", r.Code, r.Body)
	}
}

func TestHunterPullsFromDropi(t *testing.T) {
	newDropiOrdersFake(t, "tok-dropi-123456")
	e := newEnv(t, "")
	connectDropi(t, e, "tok-dropi-123456")
	r := e.do("POST", "/api/candidatos", map[string]any{"nombre": "A", "dropi_product_id": "77", "precio_venta": 89900, "flete": 15000})
	id := strconv.FormatInt(int64(r.json(t)["id"].(float64)), 10)
	r = e.do("POST", "/api/candidatos/"+id+"/traer-dropi", nil)
	if r.Code != 200 {
		t.Fatalf("traer: %d %s", r.Code, r.Body)
	}
	out := r.json(t)
	if out["costo"] == float64(0) || out["stock"] == float64(-1) {
		t.Fatalf("debía traer costo y stock: %s", r.Body)
	}
}

func TestSkuFrom(t *testing.T) {
	for in, want := range map[string]string{"LULLA 60!": "LULLA-60", "  Café con leche ": "CAF-CON-LECHE", "???": "PRODUCTO"} {
		if got := skuFrom(in); got != want {
			t.Fatalf("%q -> %q (esperaba %q)", in, got, want)
		}
	}
}
