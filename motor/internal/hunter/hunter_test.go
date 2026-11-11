package hunter

import (
	"strings"
	"testing"

	"github.com/michiTrader/dropshipper/motor/internal/config"
	"github.com/michiTrader/dropshipper/motor/internal/store"
)

var fin = config.FinanzasConf{TasaDevolucion: 0.25, TRM: 4000, MargenSeguridad: 0.30, TasaConfirmacion: 0.85}
var rl = config.CazadorConf{MinVentas7d: 10, CPAMinimo: 15000}

func cand(cost, price, freight, stock int64, sales, ads int) store.Candidate {
	return store.Candidate{Name: "x", Cost: cost, SalePrice: price, Freight: freight, Stock: stock, Sales7d: sales, Advertisers: ads}
}

func find(v Verdict, name string) *Check {
	for i := range v.Checks {
		if v.Checks[i].Name == name {
			return &v.Checks[i]
		}
	}
	return nil
}

func TestMissingData(t *testing.T) {
	if v := Evaluate(cand(0, 0, 0, -1, -1, -1), fin, rl); v.Level != Missing {
		t.Fatalf("sin costo ni precio debe pedir datos: %+v", v)
	}
}

func TestGoodProduct(t *testing.T) {
	v := Evaluate(cand(25000, 89900, 16000, 120, 40, -1), fin, rl)
	if v.Level != Good {
		t.Fatalf("producto sano debía ser bueno: %+v", v)
	}
	if v.TargetCPA < rl.CPAMinimo || v.ProfitIfDelivered != 89900-25000-16000 {
		t.Fatalf("cuentas: %+v", v)
	}
}

func TestLosesMoneyEvenIfDelivered(t *testing.T) {
	v := Evaluate(cand(60000, 70000, 16000, 100, 50, -1), fin, rl)
	if v.Level != Bad || find(v, "Ganancia").State != StBad || !strings.Contains(find(v, "Ganancia").Detail, "pierdes") {
		t.Fatalf("debía rechazarse por perder dinero: %+v", v)
	}
}

func TestThinMarginLeavesNoRoomForAds(t *testing.T) {
	// Wins ~8k per delivery: after returns and safety there is nothing for ads.
	v := Evaluate(cand(45000, 69900, 16000, 100, 50, -1), fin, rl)
	if v.Level != Bad || find(v, "Ganancia").State != StBad {
		t.Fatalf("margen mínimo para anuncios: %+v", v)
	}
}

func TestUnknownsMakeItDoubtfulNotGood(t *testing.T) {
	v := Evaluate(cand(25000, 89900, 16000, -1, -1, -1), fin, rl)
	if v.Level != Doubtful {
		t.Fatalf("con datos desconocidos nunca es 'bueno': %+v", v)
	}
	if find(v, "Demanda").State != StUnknown || find(v, "Stock").State != StUnknown {
		t.Fatalf("debe marcar lo que no sabe: %+v", v.Checks)
	}
}

func TestNoStockIsBad(t *testing.T) {
	if v := Evaluate(cand(25000, 89900, 16000, 0, 40, -1), fin, rl); v.Level != Bad {
		t.Fatalf("sin stock: %+v", v)
	}
}

func TestLowDemandAndCrowding(t *testing.T) {
	r := rl
	r.MaxAnunciantes = 20
	v := Evaluate(cand(25000, 89900, 16000, 100, 3, 80), fin, r)
	if v.Level != Doubtful || find(v, "Demanda").State != StWarn || find(v, "Competencia").State != StWarn {
		t.Fatalf("poca demanda y saturado: %+v", v)
	}
	// Competition is not checked when the owner sets no limit.
	if find(Evaluate(cand(25000, 89900, 16000, 100, 30, 80), fin, rl), "Competencia") != nil {
		t.Fatal("sin límite no se revisa la competencia")
	}
}

func TestMissingFreightWarns(t *testing.T) {
	v := Evaluate(cand(25000, 89900, 0, 100, 40, -1), fin, rl)
	if v.Level != Doubtful || find(v, "Flete") == nil {
		t.Fatalf("sin flete debe avisar: %+v", v)
	}
}
