package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/michiTrader/dropshipper/motor/internal/catalog"
	"github.com/michiTrader/dropshipper/motor/internal/config"
	"github.com/michiTrader/dropshipper/motor/internal/dropi"
	"github.com/michiTrader/dropshipper/motor/internal/geo"
	"github.com/michiTrader/dropshipper/motor/internal/hub"
	"github.com/michiTrader/dropshipper/motor/internal/rules"
	"github.com/michiTrader/dropshipper/motor/internal/settings"
	"github.com/michiTrader/dropshipper/motor/internal/shipping"
)

func (s *Server) dropiRoutes(api *http.ServeMux) {
	api.HandleFunc("PUT /api/ajustes/dropi", s.putDropi)
	api.HandleFunc("DELETE /api/ajustes/dropi", s.delDropi)
	api.HandleFunc("POST /api/dropi/probar", s.testDropi)
	api.HandleFunc("POST /api/envios/cotizar", s.quoteShipping)
	api.HandleFunc("POST /api/dropi/simular", s.simulateDropi)
	api.HandleFunc("POST /api/productos/{sku}/costos-dropi", s.pullDropiCosts)
	s.dropiOrderRoutes(api)
}

type dropiIn struct {
	Token      string `json:"token"`
	Sandbox    bool   `json:"sandbox"`
	OriginDept string `json:"origen_departamento"`
	OriginCity string `json:"origen_ciudad"`
}

func (in dropiIn) apply(old config.DropiConfig) config.DropiConfig {
	return config.DropiConfig{
		Token:      keep(in.Token, old.Token),
		Sandbox:    in.Sandbox,
		OriginDept: strings.TrimSpace(in.OriginDept),
		OriginCity: strings.TrimSpace(in.OriginCity),
	}
}

func (s *Server) putDropi(w http.ResponseWriter, r *http.Request) {
	var in dropiIn
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if s.update(w, r, func(st *settings.Settings) error {
		st.Dropi = in.apply(st.Dropi)
		if st.Dropi.OriginCity != "" {
			if _, ok := geo.Find(st.Dropi.OriginDept, st.Dropi.OriginCity); !ok {
				return fmt.Errorf("no reconozco la ciudad de origen %q; escríbela como aparece en Dropi (por ejemplo Bogotá o Medellín)", st.Dropi.OriginCity)
			}
		}
		if st.Dropi.Enabled() {
			st.Negocio.Dropi.Modo = "api"
		}
		return nil
	}) {
		writeJSON(w, 200, map[string]any{"ok": true})
	}
}

func (s *Server) delDropi(w http.ResponseWriter, r *http.Request) {
	if s.update(w, r, func(st *settings.Settings) error {
		st.Dropi = config.DropiConfig{Sandbox: st.Dropi.Sandbox, OriginDept: st.Dropi.OriginDept, OriginCity: st.Dropi.OriginCity}
		st.Negocio.Dropi.Modo = "manual"
		return nil
	}) {
		writeJSON(w, 200, map[string]any{"ok": true})
	}
}

func dropiAdvice(err error) string {
	var ae *dropi.APIError
	if errors.As(err, &ae) && ae.IsAuth() {
		return "Dropi rechazó el token. Créalo en Dropi > Configuración > Integraciones, cópialo completo y pégalo aquí (si usas el entorno de pruebas, activa el interruptor de pruebas)."
	}
	low := strings.ToLower(err.Error())
	if strings.Contains(low, "no such host") || strings.Contains(low, "connection refused") || strings.Contains(low, "timeout") || strings.Contains(low, "deadline") {
		return "No se pudo conectar con Dropi. Revisa tu conexión a internet e inténtalo de nuevo."
	}
	return err.Error()
}

func (s *Server) testDropi(w http.ResponseWriter, r *http.Request) {
	var in dropiIn
	if err := readJSON(r, &in); err != nil {
		testFail(w, "los datos enviados no son válidos")
		return
	}
	cfg := in.apply(s.Settings.Get().Dropi)
	if !cfg.Enabled() {
		testFail(w, "Pega el token de integración de Dropi y vuelve a probar.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), hub.TestTimeout)
	defer cancel()
	n, err := dropi.New(cfg.Token, cfg.Sandbox).Ping(ctx)
	if err != nil {
		testFail(w, dropiAdvice(err))
		return
	}
	msg := "Conectado a Dropi"
	if n > 0 {
		msg += fmt.Sprintf(" (%d departamentos disponibles)", n)
	}
	if cfg.Sandbox {
		msg += ". Estás en el entorno de pruebas: allí no hay transportadoras, así que no se pueden cotizar fletes; para eso usa el entorno real."
	}
	if cfg.OriginCity == "" {
		msg += " Falta indicar tu ciudad de origen para cotizar fletes."
	}
	writeJSON(w, 200, map[string]any{"ok": true, "mensaje": msg})
}

// ScenarioIn is how the panel (and the CLI over the same code) describes an order.
type ScenarioIn struct {
	SKU     string `json:"producto_sku"`
	OfferID string `json:"oferta_id"`
	Dept    string `json:"departamento"`
	City    string `json:"ciudad"`
	Hood    string `json:"barrio"`
}

func (s *Server) scenarioFrom(in ScenarioIn) (catalog.Product, catalog.Offer, shipping.Scenario, rules.Decision, error) {
	p := s.Catalog.Default()
	if sku := strings.TrimSpace(in.SKU); sku != "" {
		q, ok := s.Catalog.Get(sku)
		if !ok {
			return p, catalog.Offer{}, shipping.Scenario{}, rules.Decision{}, fmt.Errorf("el producto %q no existe", sku)
		}
		p = q
	}
	if len(p.Offers) == 0 {
		return p, catalog.Offer{}, shipping.Scenario{}, rules.Decision{}, errors.New("el producto no tiene ofertas")
	}
	offer, ok := p.Offer(in.OfferID)
	if !ok {
		offer = p.Offers[0]
		for _, o := range p.Offers {
			if o.Highlight {
				offer = o
			}
		}
	}
	if strings.TrimSpace(in.City) == "" {
		return p, offer, shipping.Scenario{}, rules.Decision{}, errors.New("escribe la ciudad de destino")
	}
	d := s.Zones.Decide(rules.Destination{Department: in.Dept, City: in.City, Neighborhood: in.Hood}, offer.Price)
	sc := shipping.Scenario{
		Department: in.Dept, City: in.City, Price: offer.Price, UnitCost: p.UnitCost, Units: offer.Units,
		EstimatedFreight: p.Freight, Risk: d.Risk, Policy: d.Policy, AdvancePaid: d.AdvanceAmount, ZoneCarriers: d.Carriers,
	}
	return p, offer, sc, d, nil
}

func (s *Server) quoteShipping(w http.ResponseWriter, r *http.Request) {
	var in ScenarioIn
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	out, err := s.Quote(r.Context(), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, out)
}

// QuoteResult is the answer to "which carrier conviene for this order".
type QuoteResult struct {
	Product       string              `json:"producto"`
	Offer         string              `json:"oferta"`
	Value         int64               `json:"valor"`
	Shipping      rules.Decision      `json:"envio"`
	CostConfirmed bool                `json:"costo_confirmado"`
	Comparison    shipping.Comparison `json:"comparacion"`
}

// Quote compares carriers for an order (used by the panel and the CLI).
func (s *Server) Quote(ctx context.Context, in ScenarioIn) (QuoteResult, error) {
	p, offer, sc, d, err := s.scenarioFrom(in)
	if err != nil {
		return QuoteResult{}, err
	}
	return QuoteResult{p.Name, offer.Label, offer.Price, d, p.CostConfirmed, s.Shipping.Compare(ctx, sc)}, nil
}

type Check struct {
	Name   string `json:"nombre"`
	Status string `json:"estado"` // ok | aviso | error
	Detail string `json:"detalle"`
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// simulateDropi dry-runs an order against the real Dropi API: it verifies the
// connection, compares the freight Dropi charges with the one assumed in the
// product, checks the supplier cost, and builds (without sending) the payload
// that would create the order. Nothing is created in Dropi.
func (s *Server) simulateDropi(w http.ResponseWriter, r *http.Request) {
	var in ScenarioIn
	if err := readJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	out, err := s.Simulate(r.Context(), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, out)
}

// SimResult is the report of a simulated order.
type SimResult struct {
	OK         bool                `json:"ok"`
	Simulation bool                `json:"simulacion"`
	Message    string              `json:"mensaje"`
	Checks     []Check             `json:"verificaciones"`
	Comparison shipping.Comparison `json:"comparacion"`
	Shipping   rules.Decision      `json:"envio"`
	Payload    map[string]any      `json:"pedido_dropi"`
}

// Simulate dry-runs an order (see simulateDropi).
func (s *Server) Simulate(ctx context.Context, in ScenarioIn) (SimResult, error) {
	p, offer, sc, d, err := s.scenarioFrom(in)
	if err != nil {
		return SimResult{}, err
	}
	var checks []Check
	add := func(name, status, detail string) { checks = append(checks, Check{name, status, detail}) }
	st := s.Settings.Get()
	client := s.Hub.Dropi()
	ctx, cancel := context.WithTimeout(ctx, hub.TestTimeout)
	defer cancel()

	if client == nil {
		add("Conexión con Dropi", "error", "Falta el token de Dropi (Conexiones > Dropi).")
	} else if n, err := client.Ping(ctx); err != nil {
		add("Conexión con Dropi", "error", dropiAdvice(err))
	} else {
		add("Conexión con Dropi", "ok", fmt.Sprintf("Token válido (%d departamentos).", n))
	}

	cmp := s.Shipping.Compare(ctx, sc)
	switch {
	case cmp.Best == nil:
		add("Flete y transportadora", "error", "No hay transportadoras activas para cotizar.")
	case cmp.Source == shipping.SourceDropi:
		detail := fmt.Sprintf("Dropi cotiza %s en $%s.", cmp.Best.Carrier, catalog.FormatCOP(cmp.Best.Freight))
		status := "ok"
		if p.Freight > 0 {
			diff := abs64(cmp.Best.Freight - p.Freight)
			if float64(diff)/float64(p.Freight) >= 0.15 {
				status = "aviso"
				detail += fmt.Sprintf(" Tu flete estimado del producto es $%s: difiere %d%%; actualízalo para que la rentabilidad sea realista.", catalog.FormatCOP(p.Freight), diff*100/p.Freight)
			} else {
				detail += " Coincide con tu flete estimado."
			}
		}
		add("Flete y transportadora", status, detail)
	default:
		msg := "Se usó el flete estimado del producto, no el de Dropi."
		if cmp.Warning != "" {
			msg = cmp.Warning
		}
		add("Flete y transportadora", "aviso", msg)
	}

	if strings.TrimSpace(p.DropiProductID) == "" {
		add("Costo del producto", "aviso", "El producto no tiene ID de Dropi: escríbelo en Productos para comparar el costo con el de Dropi.")
	} else if client == nil {
		add("Costo del producto", "aviso", "Sin conexión a Dropi no se puede verificar el costo.")
	} else if dp, err := client.Product(ctx, p.DropiProductID); err != nil {
		add("Costo del producto", "error", "No se pudo leer el producto en Dropi: "+dropiAdvice(err))
	} else if dp.Cost > 0 && dp.Cost != p.UnitCost {
		add("Costo del producto", "aviso", fmt.Sprintf("Dropi cobra $%s por unidad y tu costo guardado es $%s. Usa \"Traer costos de Dropi\" en el producto.", catalog.FormatCOP(dp.Cost), catalog.FormatCOP(p.UnitCost)))
	} else {
		add("Costo del producto", "ok", fmt.Sprintf("El costo coincide con Dropi ($%s por unidad).", catalog.FormatCOP(dp.Cost)))
	}

	if !p.CostConfirmed {
		add("Costo confirmado", "aviso", "El costo del producto aún está \"sin confirmar\": las ganancias mostradas son una estimación hasta que lo confirmes (o lo traigas de Dropi).")
	} else {
		add("Costo confirmado", "ok", "El costo del producto está confirmado.")
	}

	if cmp.Best != nil {
		if cmp.Best.Viable {
			add("Rentabilidad", "ok", fmt.Sprintf("Ganancia si entrega $%s, pérdida si devuelve $%s, esperada $%s (devolución %d%%).",
				catalog.FormatCOP(cmp.Best.ProfitIfDelivered), catalog.FormatCOP(abs64(cmp.Best.LossIfReturned)), catalog.FormatCOP(cmp.Best.ExpectedProfit), int(cmp.Best.ReturnRate*100+0.5)))
		} else {
			add("Rentabilidad", "error", fmt.Sprintf("Con esta transportadora la ganancia esperada es $%s: no conviene vender a este destino con este precio.", catalog.FormatCOP(cmp.Best.ExpectedProfit)))
		}
	}

	if _, ok := geo.Find(in.Dept, in.City); !ok {
		add("Destino", "aviso", fmt.Sprintf("No reconozco %q en la tabla DANE: Dropi no podrá cotizar ni crear la guía.", in.City))
	} else {
		add("Destino", "ok", "Municipio reconocido por código DANE.")
	}

	hasErr := false
	for _, c := range checks {
		if c.Status == "error" {
			hasErr = true
		}
	}
	best := ""
	if cmp.Best != nil {
		best = cmp.Best.Carrier
	}
	return SimResult{
		OK: !hasErr, Simulation: true,
		Message: "SIMULACIÓN: no se creó ningún pedido en Dropi.",
		Checks:  checks, Comparison: cmp, Shipping: d,
		Payload: buildOrderPayload(p, offer, in, d, best, st.Dropi),
	}, nil
}

// buildOrderPayload is the body that /orders/myorders would receive. It is only
// shown in simulations; field names follow Dropi's integration docs and should
// be double-checked with the first real order.
func buildOrderPayload(p catalog.Product, o catalog.Offer, in ScenarioIn, d rules.Decision, carrier string, dc config.DropiConfig) map[string]any {
	rate := "CON RECAUDO"
	if d.Policy == rules.PayFullAdvance {
		rate = "SIN RECAUDO"
	}
	total := d.CODAmount
	if d.Policy == rules.PayFullAdvance {
		total = 0
	}
	return map[string]any{
		"type": "FINAL_ORDER", "rate_type": rate, "total_order": total,
		"name": "Cliente", "surname": "Simulado", "phone": "3000000000", "client_email": "cliente@ejemplo.com",
		"country": "COLOMBIA", "state": in.Dept, "city": in.City, "dir": "Calle 1 # 2-3 (simulado)",
		"distributionCompany": carrier,
		"notes":               "SIMULACIÓN - " + p.Name + " " + o.Label,
		"products":            []map[string]any{{"id": p.DropiProductID, "price": o.Price / int64(max1(o.Units)), "quantity": o.Units}},
		"origen":              strings.TrimSpace(dc.OriginCity + " " + dc.OriginDept),
	}
}

func max1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}

// pullDropiCosts copies the supplier cost from Dropi into the product and marks
// it as confirmed.
func (s *Server) pullDropiCosts(w http.ResponseWriter, r *http.Request) {
	out, err := s.PullCosts(r.Context(), r.PathValue("sku"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, out)
}

// PullCosts copies the supplier cost from Dropi into the product and marks it
// as confirmed.
func (s *Server) PullCosts(ctx context.Context, sku string) (map[string]any, error) {
	p, ok := s.Catalog.Get(sku)
	if !ok {
		return nil, errors.New("el producto no existe")
	}
	client := s.Hub.Dropi()
	if client == nil {
		return nil, errors.New("conecta Dropi primero (Conexiones > Dropi)")
	}
	if strings.TrimSpace(p.DropiProductID) == "" {
		return nil, errors.New("el producto no tiene ID de Dropi; escríbelo en la ficha del producto")
	}
	cctx, cancel := context.WithTimeout(ctx, hub.TestTimeout)
	defer cancel()
	dp, err := client.Product(cctx, p.DropiProductID)
	if err != nil {
		return nil, errors.New(dropiAdvice(err))
	}
	if dp.Cost <= 0 {
		return nil, errors.New("Dropi no devolvió un costo para ese producto")
	}
	before := p.UnitCost
	p.UnitCost, p.CostConfirmed = dp.Cost, true
	if _, err := s.Catalog.Save(ctx, p); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true, "costo_anterior": before, "costo": dp.Cost, "nombre_dropi": dp.Name, "stock": dp.Stock}, nil
}
