// Package hunter judges a candidate product ("Cazador"): does it leave money
// after costs, returns and a safety margin, is there demand, is there stock,
// is it too crowded. It never decides for the owner: it explains.
package hunter

import (
	"fmt"

	"github.com/michiTrader/dropshipper/motor/internal/config"
	"github.com/michiTrader/dropshipper/motor/internal/rules"
	"github.com/michiTrader/dropshipper/motor/internal/store"
)

// Levels of the verdict.
const (
	Good     = "bueno"
	Doubtful = "dudoso"
	Bad      = "no"
	Missing  = "faltan_datos"
)

// Check states.
const (
	StOK      = "ok"
	StWarn    = "warn"
	StBad     = "bad"
	StUnknown = "unknown"
)

// Check is one line of the verdict.
type Check struct {
	Name   string `json:"nombre"`
	State  string `json:"estado"`
	Detail string `json:"detalle"`
}

// Verdict is the full judgment of a candidate.
type Verdict struct {
	Level   string  `json:"nivel"`
	Title   string  `json:"titulo"`
	Summary string  `json:"resumen"`
	Checks  []Check `json:"revision"`

	ProfitIfDelivered int64   `json:"ganancia_si_entrega"`
	LossIfReturned    int64   `json:"perdida_si_devuelve"`
	BreakEvenCPA      int64   `json:"cpa_equilibrio"`
	TargetCPA         int64   `json:"cpa_objetivo"`
	TargetLead        int64   `json:"costo_por_chat_objetivo"`
	MarginPct         float64 `json:"margen_pct"`
}

func cop(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := fmt.Sprint(v)
	out := ""
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out += "."
		}
		out += string(r)
	}
	if neg {
		out = "-" + out
	}
	return "$" + out
}

// Evaluate judges c with the owner's financial assumptions and hunter rules.
func Evaluate(c store.Candidate, f config.FinanzasConf, r config.CazadorConf) Verdict {
	v := Verdict{Checks: []Check{}}
	if c.Cost <= 0 || c.SalePrice <= 0 {
		v.Level, v.Title = Missing, "Faltan datos"
		v.Summary = "Para calcular necesito el costo del proveedor y el precio al que lo venderías al cliente."
		return v
	}
	p := rules.Profit(rules.ProfitInput{Price: c.SalePrice, UnitCost: c.Cost, Units: 1, Freight: c.Freight, ReturnRate: f.TasaDevolucion})
	v.ProfitIfDelivered, v.LossIfReturned, v.BreakEvenCPA, v.MarginPct = p.ProfitIfDelivered, p.LossIfReturned, p.BreakEvenCPA, p.MarginPct
	v.TargetCPA, v.TargetLead = rules.MaxCPA(p.BreakEvenCPA, f.MargenSeguridad, f.TasaConfirmacion)

	add := func(name, state, detail string) { v.Checks = append(v.Checks, Check{name, state, detail}) }
	bad, warn := false, false
	mark := func(state string) {
		switch state {
		case StBad:
			bad = true
		case StWarn, StUnknown:
			warn = true
		}
	}

	// 1. Money.
	switch {
	case p.ProfitIfDelivered <= 0:
		add("Ganancia", StBad, fmt.Sprintf("El precio no cubre ni el costo ni el flete: aunque se entregue pierdes %s por pedido.", cop(-p.ProfitIfDelivered)))
	case p.BreakEvenCPA <= 0:
		add("Ganancia", StBad, fmt.Sprintf("Si se entrega ganas %s, pero como una parte de los pedidos regresa (y el flete de ida y vuelta lo pagas tú), en promedio pierdes plata incluso sin gastar nada en anuncios.", cop(p.ProfitIfDelivered)))
	case v.TargetCPA < r.CPAMinimo:
		add("Ganancia", StBad, fmt.Sprintf("Si se entrega ganas %s, pero descontando devoluciones y el margen de seguridad solo podrías gastar %s en anuncios por pedido, y el mínimo que pediste es %s. Casi no hay espacio para publicidad.",
			cop(p.ProfitIfDelivered), cop(v.TargetCPA), cop(r.CPAMinimo)))
	default:
		add("Ganancia", StOK, fmt.Sprintf("Si se entrega ganas %s. Contando devoluciones puedes gastar hasta %s en anuncios por pedido (unos %s por cada chat).",
			cop(p.ProfitIfDelivered), cop(v.TargetCPA), cop(v.TargetLead)))
	}
	if len(v.Checks) > 0 {
		mark(v.Checks[len(v.Checks)-1].State)
	}

	// 2. Freight.
	if c.Freight <= 0 {
		add("Flete", StWarn, "Falta el flete. Sin él la ganancia sale más bonita de lo real: pon un valor aproximado (o cotízalo en Envíos).")
		mark(StWarn)
	}

	// 3. Demand.
	switch {
	case c.Sales7d < 0:
		add("Demanda", StUnknown, "No sé cuánto se vende. Mira las ventas de los últimos 7 días que DropKiller muestra bajo el producto en Dropi.")
	case c.Sales7d < r.MinVentas7d:
		add("Demanda", StWarn, fmt.Sprintf("Se vendieron %d en 7 días; tu mínimo es %d. Poca gente lo está comprando.", c.Sales7d, r.MinVentas7d))
	default:
		add("Demanda", StOK, fmt.Sprintf("Se vendieron %d en 7 días (tu mínimo es %d). Importa que las ventas estén repartidas en varios días y no en uno solo.", c.Sales7d, r.MinVentas7d))
	}
	mark(v.Checks[len(v.Checks)-1].State)

	// 4. Stock.
	switch {
	case c.Stock < 0:
		add("Stock", StUnknown, "No sé si el proveedor tiene stock. Míralo en Dropi antes de pautar.")
	case c.Stock == 0:
		add("Stock", StBad, "El proveedor no tiene stock. Pautar así es botar plata.")
	default:
		add("Stock", StOK, fmt.Sprintf("El proveedor tiene %d unidades.", c.Stock))
	}
	mark(v.Checks[len(v.Checks)-1].State)

	// 5. Competition (only when the owner set a limit).
	if r.MaxAnunciantes > 0 {
		switch {
		case c.Advertisers < 0:
			add("Competencia", StUnknown, "No sé cuántos vendedores lo están anunciando. Míralo en la biblioteca de anuncios de Meta con la extensión.")
		case c.Advertisers > r.MaxAnunciantes:
			add("Competencia", StWarn, fmt.Sprintf("Lo anuncian %d vendedores; tu límite es %d. Está saturado: los anuncios te saldrán más caros.", c.Advertisers, r.MaxAnunciantes))
		default:
			add("Competencia", StOK, fmt.Sprintf("Lo anuncian %d vendedores (tu límite es %d). Hay espacio.", c.Advertisers, r.MaxAnunciantes))
		}
		mark(v.Checks[len(v.Checks)-1].State)
	}

	switch {
	case bad:
		v.Level, v.Title = Bad, "No lo recomiendo"
		v.Summary = "Hay al menos un problema serio (en rojo). Si no lo puedes arreglar, mejor busca otro producto."
	case warn:
		v.Level, v.Title = Doubtful, "Dudoso: revísalo"
		v.Summary = "Los números principales no están mal, pero hay datos que faltan o puntos por revisar antes de gastar en anuncios."
	default:
		v.Level, v.Title = Good, "Vale la pena probarlo"
		v.Summary = fmt.Sprintf("Todo cuadra. Pruébalo con poco presupuesto y vigila que cada pedido no te cueste más de %s en anuncios.", cop(v.TargetCPA))
	}
	return v
}
