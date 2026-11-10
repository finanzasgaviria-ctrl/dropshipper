// Package shipping chooses the carrier for an order.
//
// For every ACTIVE carrier it prices the order with rules.Profit (freight of
// that carrier, return rate of that carrier) and keeps the one with the best
// expected profit. Freight comes from Dropi's quote API when it is connected
// and from the product's estimated freight otherwise; every result says which.
package shipping

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/michiTrader/dropshipper/motor/internal/config"
	"github.com/michiTrader/dropshipper/motor/internal/dropi"
	"github.com/michiTrader/dropshipper/motor/internal/geo"
	"github.com/michiTrader/dropshipper/motor/internal/rules"
)

// Sources of the freight number.
const (
	SourceDropi    = "dropi"
	SourceEstimate = "estimado"
)

// Quoter is the part of the Dropi client the chooser needs.
type Quoter interface {
	Quote(ctx context.Context, in dropi.QuoteInput) ([]dropi.Quote, error)
}

// Scenario is one order to price.
type Scenario struct {
	Department, City string
	Price            int64 // order total
	UnitCost         int64
	Units            int
	EstimatedFreight int64 // product's flete_estimado, used when Dropi cannot quote
	Risk             string
	Policy           string
	AdvancePaid      int64
	// ZoneCarriers are the carriers the zone rules suggest (fallback order).
	ZoneCarriers []string
}

// Option is one carrier priced for a scenario.
type Option struct {
	ID                string  `json:"id"`
	Carrier           string  `json:"transportadora"`
	Freight           int64   `json:"flete"`
	ProfitIfDelivered int64   `json:"ganancia_si_entrega"`
	LossIfReturned    int64   `json:"perdida_si_devuelve"`
	ExpectedProfit    int64   `json:"ganancia_esperada"`
	ReturnRate        float64 `json:"tasa_devolucion"`
	Viable            bool    `json:"viable"`
	Best              bool    `json:"mejor"`
	// Days is the delivery time in days; DaysSource says if it is Dropi's
	// number ("dropi") or the carrier's typical time you configured ("estimado").
	Days       int    `json:"dias"`
	DaysSource string `json:"fuente_dias"`
	// WithinLimit is false when the carrier is slower than envio.max_dias.
	WithinLimit bool `json:"dentro_del_limite"`
	// Fastest / Cheapest mark the quickest and the lowest-freight carrier.
	Fastest  bool `json:"mas_rapida"`
	Cheapest bool `json:"mas_barata"`
}

// Comparison is the full answer.
type Comparison struct {
	Options []Option `json:"opciones"`
	Best    *Option  `json:"mejor,omitempty"`
	// Source says where the freight numbers come from: "dropi" or "estimado".
	Source  string `json:"fuente_flete"`
	Warning string `json:"advertencia,omitempty"`
	// Criterion is how Best was chosen (rentable | rapido | barato) and
	// MaxDays the delivery limit applied (0 = none).
	Criterion string `json:"criterio"`
	MaxDays   int    `json:"max_dias"`
	// NoneInTime is true when no carrier meets MaxDays (Best is then the fastest).
	NoneInTime bool `json:"ninguna_a_tiempo,omitempty"`
}

// Service compares carriers.
type Service struct {
	Negocio *config.Live
	// Client returns the live Dropi client (nil when not connected).
	Client func() Quoter
	// Origin returns where parcels ship from (from the Dropi settings).
	Origin func() (dept, city string)
	Now    func() time.Time

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	at     time.Time
	quotes []dropi.Quote
}

const cacheTTL = 10 * time.Minute

func (s *Service) neg() config.Negocio {
	if s.Negocio == nil {
		return config.DefaultNegocio()
	}
	return s.Negocio.Get()
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Compare prices every active carrier for the scenario, best first.
//
// Best is chosen so that a slow carrier is never recommended when a faster one
// exists: first the carriers that arrive within envio.max_dias, then (among
// those) the owner's criterion: most profitable, fastest or cheapest. If none
// arrives in time, the fastest wins and the answer says so.
func (s *Service) Compare(ctx context.Context, sc Scenario) Comparison {
	n := s.neg()
	var out Comparison
	freights, warn := s.freights(ctx, n, sc)
	out.Warning = warn
	out.Source = SourceEstimate
	if len(freights) > 0 {
		out.Source = SourceDropi
	} else {
		freights = s.fallback(n, sc)
	}
	out.Criterion, out.MaxDays = n.Envio.Criterio, n.Envio.MaxDias
	for _, f := range freights {
		t, _ := n.Carrier(f.name)
		o := price(n, sc, t, f.freight)
		o.Days, o.DaysSource = f.days, SourceDropi
		if o.Days <= 0 {
			o.Days, o.DaysSource = t.EstimatedDays(sc.Risk), SourceEstimate
		}
		o.WithinLimit = n.Envio.MaxDias <= 0 || o.Days <= n.Envio.MaxDias
		out.Options = append(out.Options, o)
	}
	if len(out.Options) == 0 {
		return out
	}
	markFastestCheapest(out.Options)

	anyInTime := false
	for _, o := range out.Options {
		anyInTime = anyInTime || o.WithinLimit
	}
	out.NoneInTime = !anyInTime
	if out.NoneInTime {
		extra := fmt.Sprintf("Ninguna transportadora llega en %d días o menos a ese destino; se recomienda la más rápida.", n.Envio.MaxDias)
		out.Warning = strings.TrimSpace(out.Warning + " " + extra)
	}
	crit := n.Envio.Criterio
	sort.SliceStable(out.Options, func(i, j int) bool {
		return better(n, crit, out.NoneInTime, out.Options[i], out.Options[j])
	})
	out.Options[0].Best = true
	best := out.Options[0]
	out.Best = &best
	return out
}

func markFastestCheapest(opts []Option) {
	fast, cheap := 0, 0
	for i, o := range opts {
		if o.Days < opts[fast].Days || (o.Days == opts[fast].Days && o.Freight < opts[fast].Freight) {
			fast = i
		}
		if o.Freight < opts[cheap].Freight || (o.Freight == opts[cheap].Freight && o.Days < opts[cheap].Days) {
			cheap = i
		}
	}
	opts[fast].Fastest = true
	opts[cheap].Cheapest = true
}

// better reports whether a should be listed before b.
func better(n config.Negocio, crit string, noneInTime bool, a, b Option) bool {
	if noneInTime {
		// Nobody meets the limit: speed first, then profit.
		if a.Days != b.Days {
			return a.Days < b.Days
		}
		return profitFirst(n, a, b)
	}
	if a.WithinLimit != b.WithinLimit {
		return a.WithinLimit
	}
	switch crit {
	case config.CriterioRapido, config.CriterioBarato:
		// Never recommend a carrier that loses money if another one does not.
		if a.Viable != b.Viable {
			return a.Viable
		}
		if crit == config.CriterioRapido && a.Days != b.Days {
			return a.Days < b.Days
		}
		if crit == config.CriterioBarato && a.Freight != b.Freight {
			return a.Freight < b.Freight
		}
		if crit == config.CriterioRapido && a.Freight != b.Freight {
			return a.Freight < b.Freight
		}
		if a.Days != b.Days {
			return a.Days < b.Days
		}
	}
	return profitFirst(n, a, b)
}

func profitFirst(n config.Negocio, a, b Option) bool {
	if a.ExpectedProfit != b.ExpectedProfit {
		return a.ExpectedProfit > b.ExpectedProfit
	}
	if a.Days != b.Days {
		return a.Days < b.Days
	}
	if a.Freight != b.Freight {
		return a.Freight < b.Freight
	}
	return prio(n, a.ID) < prio(n, b.ID)
}

func prio(n config.Negocio, id string) int {
	for _, t := range n.Transportadoras {
		if t.ID == id {
			return t.Prioridad
		}
	}
	return 1000
}

type carrierFreight struct {
	name    string
	freight int64
	days    int // 0 = Dropi did not say
}

func price(n config.Negocio, sc Scenario, t config.Transportadora, freight int64) Option {
	base := n.Finanzas.TasaDevolucion
	if t.TasaDevolucion > 0 {
		base = t.TasaDevolucion
	}
	ret := rules.ReturnRateFor(base, sc.Risk, sc.Policy)
	pr := rules.Profit(rules.ProfitInput{
		Price: sc.Price, UnitCost: sc.UnitCost, Units: sc.Units, Freight: freight,
		ReturnRate: ret, AdvancePaid: sc.AdvancePaid,
	})
	return Option{
		ID: t.ID, Carrier: t.Nombre, Freight: freight,
		ProfitIfDelivered: pr.ProfitIfDelivered, LossIfReturned: pr.LossIfReturned,
		ExpectedProfit: pr.ExpectedProfit, ReturnRate: ret, Viable: pr.Viable,
	}
}

// freights asks Dropi (cached). Only ACTIVE configured carriers are kept.
func (s *Service) freights(ctx context.Context, n config.Negocio, sc Scenario) ([]carrierFreight, string) {
	if s.Client == nil {
		return nil, ""
	}
	q := s.Client()
	if q == nil {
		return nil, ""
	}
	var od, oc string
	if s.Origin != nil {
		od, oc = s.Origin()
	}
	origin, ok := geo.Find(od, oc)
	if !ok {
		return nil, "Falta la ciudad de origen de tus envíos (Conexiones > Dropi): se usa el flete estimado."
	}
	dest, ok := geo.Find(sc.Department, sc.City)
	if !ok {
		return nil, fmt.Sprintf("No se reconoce el municipio %q para cotizar en Dropi: se usa el flete estimado.", sc.City)
	}
	cod := sc.Policy != rules.PayFullAdvance
	amount := sc.Price - sc.AdvancePaid
	if amount < 0 {
		amount = 0
	}
	key := fmt.Sprintf("%s>%s|%v|%d", origin.DANE7(), dest.DANE7(), cod, amount)
	quotes, hit := s.cached(key)
	if !hit {
		var err error
		quotes, err = q.Quote(ctx, dropi.QuoteInput{COD: cod, Amount: amount, OriginDANE: origin.DANE7(), DestDANE: dest.DANE7()})
		if err != nil {
			return nil, "Dropi no pudo cotizar el flete (" + err.Error() + "): se usa el flete estimado."
		}
		s.store(key, quotes)
	}
	best := map[string]carrierFreight{}
	for _, qu := range quotes {
		t, ok := n.Carrier(qu.Carrier)
		if !ok || !t.Activa {
			continue
		}
		if cur, ok := best[t.ID]; !ok || qu.Freight < cur.freight {
			best[t.ID] = carrierFreight{name: t.Nombre, freight: qu.Freight, days: qu.Days}
		}
	}
	if len(best) == 0 {
		return nil, "Dropi cotizó, pero ninguna de las transportadoras activas cubre esa ruta: se usa el flete estimado."
	}
	out := make([]carrierFreight, 0, len(best))
	for _, v := range best {
		out = append(out, v)
	}
	return out, ""
}

func (s *Service) cached(key string) ([]dropi.Quote, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.cache[key]
	if !ok || s.now().Sub(c.at) > cacheTTL {
		return nil, false
	}
	return c.quotes, true
}

func (s *Service) store(key string, q []dropi.Quote) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cache == nil || len(s.cache) > 500 {
		s.cache = map[string]cached{}
	}
	s.cache[key] = cached{at: s.now(), quotes: q}
}

// fallback prices the zone-suggested carriers (or every active one) with the
// product's estimated freight.
func (s *Service) fallback(n config.Negocio, sc Scenario) []carrierFreight {
	seen := map[string]bool{}
	var out []carrierFreight
	add := func(t config.Transportadora) {
		if t.Activa && !seen[t.ID] {
			seen[t.ID] = true
			out = append(out, carrierFreight{name: t.Nombre, freight: sc.EstimatedFreight})
		}
	}
	for _, name := range sc.ZoneCarriers {
		if t, ok := n.Carrier(name); ok {
			add(t)
		}
	}
	if len(out) == 0 {
		for _, t := range n.Transportadoras {
			add(t)
		}
	}
	return out
}

// Clear drops cached quotes (after changing the origin or the token).
func (s *Service) Clear() {
	s.mu.Lock()
	s.cache = nil
	s.mu.Unlock()
}

// PromptSection is the carrier knowledge injected into the AI system prompt:
// what each ACTIVE carrier is good for, as the owner wrote it.
func PromptSection(n config.Negocio) string {
	var b strings.Builder
	b.WriteString("# Transportadoras\n")
	b.WriteString("El sistema elige la transportadora más rentable para cada pedido según su destino (tú no la eliges ni prometes una en concreto). Esto es lo que sabe la tienda de cada una; úsalo solo para orientar al cliente sobre tiempos o cobertura, sin inventar nada más:\n")
	list := append([]config.Transportadora{}, n.Transportadoras...)
	sort.SliceStable(list, func(i, j int) bool { return list[i].Prioridad < list[j].Prioridad })
	any := false
	for _, t := range list {
		if !t.Activa {
			continue
		}
		any = true
		fmt.Fprintf(&b, "- %s: %s", t.Nombre, firstNonEmpty(t.Notas, "sin notas"))
		if len(t.Zonas) > 0 {
			fmt.Fprintf(&b, " Zonas fuertes: %s.", strings.Join(t.Zonas, ", "))
		}
		b.WriteString("\n")
	}
	if !any {
		return ""
	}
	return b.String()
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
