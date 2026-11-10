package shipping

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/michiTrader/dropshipper/motor/internal/config"
	"github.com/michiTrader/dropshipper/motor/internal/dropi"
	"github.com/michiTrader/dropshipper/motor/internal/rules"
)

type fakeQ struct {
	quotes []dropi.Quote
	err    error
	calls  int
}

func (f *fakeQ) Quote(_ context.Context, _ dropi.QuoteInput) ([]dropi.Quote, error) {
	f.calls++
	return f.quotes, f.err
}

func svc(q *fakeQ, mut func(*config.Negocio)) *Service {
	n := config.DefaultNegocio()
	if mut != nil {
		mut(&n)
	}
	s := &Service{
		Negocio: config.NewLive(n),
		Origin:  func() (string, string) { return "Cundinamarca", "Bogotá" },
	}
	if q != nil {
		s.Client = func() Quoter { return q }
	}
	return s
}

var scn = Scenario{Department: "Antioquia", City: "Medellín", Price: 149900, UnitCost: 40000, Units: 2,
	EstimatedFreight: 18000, Risk: rules.RiskNormal, Policy: rules.PayCOD, ZoneCarriers: []string{"Coordinadora", "Envía"}}

func TestChoosesMostProfitableCarrier(t *testing.T) {
	q := &fakeQ{quotes: []dropi.Quote{{Carrier: "COORDINADORA", Freight: 16000}, {Carrier: "ENVIA", Freight: 12000}, {Carrier: "TCC", Freight: 13000}}}
	c := svc(q, nil).Compare(context.Background(), scn)
	if c.Source != SourceDropi || c.Best == nil || c.Best.Carrier != "Envía" || c.Best.Freight != 12000 {
		t.Fatalf("%+v", c)
	}
	if len(c.Options) != 3 || !c.Options[0].Best || c.Options[1].Best {
		t.Fatalf("opciones: %+v", c.Options)
	}
	if c.Options[0].ProfitIfDelivered != 149900-80000-12000 || c.Options[0].LossIfReturned >= 0 {
		t.Fatalf("economía: %+v", c.Options[0])
	}
}

func TestCarrierOwnReturnRateChangesTheWinner(t *testing.T) {
	q := &fakeQ{quotes: []dropi.Quote{{Carrier: "ENVIA", Freight: 12000}, {Carrier: "COORDINADORA", Freight: 13000}}}
	c := svc(q, func(n *config.Negocio) {
		for i := range n.Transportadoras {
			if n.Transportadoras[i].ID == "envia" {
				n.Transportadoras[i].TasaDevolucion = 0.6 // very bad delivery rate
			}
			if n.Transportadoras[i].ID == "coordinadora" {
				n.Transportadoras[i].TasaDevolucion = 0.05
			}
		}
	}).Compare(context.Background(), scn)
	if c.Best.Carrier != "Coordinadora" {
		t.Fatalf("debía ganar Coordinadora por devolver menos: %+v", c.Options)
	}
}

func TestInactiveCarrierIsNeverChosen(t *testing.T) {
	q := &fakeQ{quotes: []dropi.Quote{{Carrier: "TCC", Freight: 5000}, {Carrier: "COORDINADORA", Freight: 15000}}}
	c := svc(q, func(n *config.Negocio) {
		for i := range n.Transportadoras {
			if n.Transportadoras[i].ID == "tcc" {
				n.Transportadoras[i].Activa = false
			}
		}
	}).Compare(context.Background(), scn)
	if c.Best.Carrier != "Coordinadora" || len(c.Options) != 1 {
		t.Fatalf("%+v", c)
	}
}

func TestFallsBackToEstimateWhenDropiFails(t *testing.T) {
	q := &fakeQ{err: errors.New("timeout")}
	c := svc(q, nil).Compare(context.Background(), scn)
	if c.Source != SourceEstimate || c.Warning == "" || c.Best == nil || c.Best.Freight != 18000 || c.Best.Carrier != "Coordinadora" {
		t.Fatalf("%+v", c)
	}
	// No Dropi at all: same fallback, without a warning.
	c = svc(nil, nil).Compare(context.Background(), scn)
	if c.Source != SourceEstimate || c.Warning != "" || c.Best == nil {
		t.Fatalf("%+v", c)
	}
}

func TestUnknownCityOrOriginFallsBack(t *testing.T) {
	q := &fakeQ{quotes: []dropi.Quote{{Carrier: "TCC", Freight: 9000}}}
	s := svc(q, nil)
	bad := scn
	bad.City = "Narnia"
	if c := s.Compare(context.Background(), bad); c.Source != SourceEstimate || !strings.Contains(c.Warning, "Narnia") || q.calls != 0 {
		t.Fatalf("%+v calls=%d", c, q.calls)
	}
	s.Origin = func() (string, string) { return "", "" }
	if c := s.Compare(context.Background(), scn); c.Source != SourceEstimate || !strings.Contains(c.Warning, "origen") {
		t.Fatalf("%+v", c)
	}
}

func TestQuoteIsCached(t *testing.T) {
	q := &fakeQ{quotes: []dropi.Quote{{Carrier: "TCC", Freight: 9000}}}
	s := svc(q, nil)
	for i := 0; i < 3; i++ {
		s.Compare(context.Background(), scn)
	}
	if q.calls != 1 {
		t.Fatalf("la cotización debía cachearse: %d llamadas", q.calls)
	}
	s.Clear()
	s.Compare(context.Background(), scn)
	if q.calls != 2 {
		t.Fatalf("tras Clear debía volver a cotizar: %d", q.calls)
	}
}

func TestPromptSection(t *testing.T) {
	n := config.DefaultNegocio()
	for i := range n.Transportadoras {
		if n.Transportadoras[i].ID == "tcc" {
			n.Transportadoras[i].Notas = "Llega en 2 días a ciudades principales"
		}
	}
	p := PromptSection(n)
	if !strings.Contains(p, "# Transportadoras") || !strings.Contains(p, "TCC: Llega en 2 días") || strings.Contains(p, "Servientrega") {
		t.Fatalf("%s", p)
	}
	for _, id := range []string{"Veloces", "Envía", "Interrapidísimo", "Domina", "Coordinadora", "99minutos", "TCC", "Wiilog", "Jamv-drive", "Derocha-express"} {
		if !strings.Contains(p, id+":") {
			t.Errorf("falta %s en el prompt", id)
		}
	}
}

// ---------- tiempos de entrega y criterio ----------

func withEnvio(max int, crit string) func(*config.Negocio) {
	return func(n *config.Negocio) { n.Envio.MaxDias, n.Envio.Criterio = max, crit }
}

func TestDaysComeFromDropiWhenReported(t *testing.T) {
	q := &fakeQ{quotes: []dropi.Quote{{Carrier: "COORDINADORA", Freight: 16000, Days: 2}, {Carrier: "ENVIA", Freight: 12000}}}
	c := svc(q, nil).Compare(context.Background(), scn)
	got := map[string]Option{}
	for _, o := range c.Options {
		got[o.ID] = o
	}
	if got["coordinadora"].Days != 2 || got["coordinadora"].DaysSource != SourceDropi {
		t.Fatalf("Dropi informó 2 días: %+v", got["coordinadora"])
	}
	if got["envia"].Days != 3 || got["envia"].DaysSource != SourceEstimate {
		t.Fatalf("sin dato de Dropi se usa el típico configurado (3): %+v", got["envia"])
	}
}

func TestSlowCarrierIsNeverRecommendedWhenAFasterOneArrivesInTime(t *testing.T) {
	// Envía is the most profitable but takes 6 days; the limit is 4.
	q := &fakeQ{quotes: []dropi.Quote{{Carrier: "ENVIA", Freight: 9000, Days: 6}, {Carrier: "COORDINADORA", Freight: 15000, Days: 3}}}
	c := svc(q, withEnvio(4, config.CriterioRentable)).Compare(context.Background(), scn)
	if c.Best == nil || c.Best.ID != "coordinadora" || c.NoneInTime {
		t.Fatalf("debía ganar Coordinadora (a tiempo): %+v", c)
	}
	for _, o := range c.Options {
		if o.ID == "envia" && o.WithinLimit {
			t.Fatalf("Envía (6 días) no cumple el plazo de 4: %+v", o)
		}
	}
	// Same quotes without a limit: the most profitable wins.
	c = svc(&fakeQ{quotes: q.quotes}, withEnvio(0, config.CriterioRentable)).Compare(context.Background(), scn)
	if c.Best.ID != "envia" {
		t.Fatalf("sin plazo gana la más rentable: %+v", c.Best)
	}
}

func TestNoneInTimeFallsBackToFastestAndWarns(t *testing.T) {
	q := &fakeQ{quotes: []dropi.Quote{{Carrier: "ENVIA", Freight: 9000, Days: 8}, {Carrier: "COORDINADORA", Freight: 15000, Days: 6}}}
	c := svc(q, withEnvio(3, config.CriterioRentable)).Compare(context.Background(), scn)
	if !c.NoneInTime || c.Best.ID != "coordinadora" || !strings.Contains(c.Warning, "3 días") {
		t.Fatalf("ninguna llega en 3 días: debía recomendar la más rápida y avisar: %+v", c)
	}
}

func TestCriterionFastestAndCheapest(t *testing.T) {
	quotes := []dropi.Quote{{Carrier: "ENVIA", Freight: 9000, Days: 5}, {Carrier: "COORDINADORA", Freight: 15000, Days: 2}, {Carrier: "TCC", Freight: 12000, Days: 3}}
	fast := svc(&fakeQ{quotes: quotes}, withEnvio(0, config.CriterioRapido)).Compare(context.Background(), scn)
	if fast.Best.ID != "coordinadora" {
		t.Fatalf("criterio rápido: %+v", fast.Best)
	}
	cheap := svc(&fakeQ{quotes: quotes}, withEnvio(0, config.CriterioBarato)).Compare(context.Background(), scn)
	if cheap.Best.ID != "envia" {
		t.Fatalf("criterio barato: %+v", cheap.Best)
	}
	for _, o := range fast.Options {
		if o.ID == "coordinadora" && !o.Fastest {
			t.Fatal("Coordinadora debe marcarse como más rápida")
		}
		if o.ID == "envia" && !o.Cheapest {
			t.Fatal("Envía debe marcarse como más barata")
		}
	}
}

func TestFastOrCheapNeverPicksACarrierThatLosesMoney(t *testing.T) {
	// A $60.000 order with $40.000 of product: the 2-day carrier charges so much
	// freight that it loses money; the 5-day one still earns.
	sc := scn
	sc.Price, sc.UnitCost, sc.Units = 60000, 40000, 1
	q := &fakeQ{quotes: []dropi.Quote{{Carrier: "COORDINADORA", Freight: 30000, Days: 2}, {Carrier: "ENVIA", Freight: 8000, Days: 5}}}
	c := svc(q, withEnvio(0, config.CriterioRapido)).Compare(context.Background(), sc)
	if c.Best.ID != "envia" || !c.Best.Viable {
		t.Fatalf("no debe recomendar la rápida si pierde dinero habiendo otra que no: %+v", c.Options)
	}
}

func TestEstimatedDaysByRisk(t *testing.T) {
	tr, _ := config.DefaultNegocio().Carrier("Coordinadora")
	if n, f := tr.EstimatedDays("normal"), tr.EstimatedDays("lejano"); n != 3 || f != 5 {
		t.Fatalf("típicos por defecto: normal=%d lejano=%d", n, f)
	}
	tr.DiasEntrega, tr.DiasLejano = 2, 7
	if tr.EstimatedDays("normal") != 2 || tr.EstimatedDays("zona_roja") != 7 {
		t.Fatalf("lo configurado manda: %d %d", tr.EstimatedDays("normal"), tr.EstimatedDays("zona_roja"))
	}
	tr.DiasEntrega, tr.DiasLejano = 6, 2 // far can never be faster than near
	if tr.EstimatedDays("lejano") != 6 {
		t.Fatalf("lejano no puede ser más rápido que normal: %d", tr.EstimatedDays("lejano"))
	}
}

func TestFallbackEstimateAlsoHasDays(t *testing.T) {
	c := svc(nil, nil).Compare(context.Background(), scn)
	if c.Best == nil || c.Best.Days <= 0 || c.Best.DaysSource != SourceEstimate {
		t.Fatalf("incluso sin Dropi cada opción lleva su tiempo típico: %+v", c.Best)
	}
}
