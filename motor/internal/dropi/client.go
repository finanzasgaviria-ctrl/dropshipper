// Package dropi is a small client for the Dropi Integrations API
// (https://api.dropi.co/integrations). Authentication is one header,
// "dropi-integration-key", with the token the owner creates in
// Dropi > Configuración > Integraciones.
//
// The response shape of the freight quote is not documented precisely, so the
// parser is tolerant (see parseQuotes) and every call can be exercised against
// a fake server in tests.
package dropi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	ProdBase    = "https://api.dropi.co/integrations"
	SandboxBase = "https://test-api.dropi.co/integrations"
)

// BaseURLOverride lets tests point every new client to a fake server.
var BaseURLOverride string

// Client talks to the Dropi Integrations API.
type Client struct {
	Base  string
	Token string
	HTTP  *http.Client
}

// New builds a client for production or the test environment.
func New(token string, sandbox bool) *Client {
	base := ProdBase
	if sandbox {
		base = SandboxBase
	}
	if BaseURLOverride != "" {
		base = BaseURLOverride
	}
	return &Client{Base: strings.TrimRight(base, "/"), Token: strings.TrimSpace(token), HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// APIError is a non-2xx answer.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string { return fmt.Sprintf("Dropi HTTP %d: %s", e.Status, e.Body) }

// IsAuth reports a rejected token.
func (e *APIError) IsAuth() bool {
	return e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden
}

func (c *Client) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("dropi-integration-key", c.Token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return raw, &APIError{Status: resp.StatusCode, Body: snippet(raw)}
	}
	// Dropi sometimes answers 200 with {"isSuccess":false,"message":"..."}.
	var env struct {
		IsSuccess *bool  `json:"isSuccess"`
		Message   string `json:"message"`
	}
	if json.Unmarshal(raw, &env) == nil && env.IsSuccess != nil && !*env.IsSuccess {
		st := http.StatusBadRequest
		if strings.Contains(strings.ToLower(env.Message), "denied") {
			st = http.StatusUnauthorized
		}
		return raw, &APIError{Status: st, Body: env.Message}
	}
	return raw, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

// Ping checks the token by listing departments. It returns how many came back.
func (c *Client) Ping(ctx context.Context) (int, error) {
	raw, err := c.do(ctx, http.MethodGet, "/department", nil)
	if err != nil {
		return 0, err
	}
	items := listFrom(raw)
	return len(items), nil
}

// QuoteInput is one freight query.
type QuoteInput struct {
	COD        bool   // cash on delivery ("EnvioConCobro")
	Amount     int64  // declared value / amount to collect
	OriginDANE string // 7-digit DANE
	DestDANE   string // 7-digit DANE
}

// Quote is one carrier's price for a route.
type Quote struct {
	Carrier string `json:"transportadora"`
	Freight int64  `json:"flete"`
	// Days is the delivery time in days when Dropi reports one (0 = unknown).
	Days int `json:"dias,omitempty"`
}

// Quote asks Dropi for the freight of every carrier it can use for the route.
func (c *Client) Quote(ctx context.Context, in QuoteInput) ([]Quote, error) {
	body := map[string]any{
		"EnvioConCobro":    in.COD,
		"amount":           in.Amount,
		"ciudad_destino":   map[string]any{"cod_dane": in.DestDANE},
		"ciudad_remitente": map[string]any{"cod_dane": in.OriginDANE},
	}
	raw, err := c.do(ctx, http.MethodPost, "/orders/cotizaEnvioTransportadoraV2", body)
	if err != nil {
		return nil, err
	}
	qs := parseQuotes(raw)
	if len(qs) == 0 {
		return nil, fmt.Errorf("Dropi no devolvió transportadoras para esa ruta: %s", snippet(raw))
	}
	return qs, nil
}

// Product is what the panel needs from a Dropi catalog entry.
type Product struct {
	ID        string `json:"id"`
	Name      string `json:"nombre"`
	Cost      int64  `json:"costo"`
	SalePrice int64  `json:"precio_sugerido"`
	Stock     int64  `json:"stock"`
}

// Product reads a product by its Dropi id.
func (c *Client) Product(ctx context.Context, id string) (Product, error) {
	raw, err := c.do(ctx, http.MethodGet, "/products/v2/"+url.PathEscape(strings.TrimSpace(id)), nil)
	if err != nil {
		return Product{}, err
	}
	var any0 any
	if err := json.Unmarshal(raw, &any0); err != nil {
		return Product{}, fmt.Errorf("respuesta de Dropi inválida: %w", err)
	}
	obj := firstObject(any0)
	if obj == nil {
		return Product{}, errors.New("Dropi no devolvió el producto")
	}
	p := Product{ID: strings.TrimSpace(id)}
	p.Name = str(obj, "name", "nombre", "title")
	p.Cost = num(obj, "sale_price", "cost", "costo", "price", "precio_proveedor", "supplier_price")
	p.SalePrice = num(obj, "suggested_price", "precio_sugerido", "sugested_price")
	p.Stock = num(obj, "stock", "total_stock", "quantity")
	if p.Name == "" && p.Cost == 0 {
		return Product{}, errors.New("el producto de Dropi llegó sin nombre ni costo")
	}
	return p, nil
}

// OrderResult is what the Motor keeps from Dropi's answer to a created order.
type OrderResult struct {
	ID       string `json:"id"`
	Tracking string `json:"guia"`
	Carrier  string `json:"transportadora"`
	Raw      string `json:"-"`
}

// CreateOrder posts an order payload. Only used when the owner switches Dropi
// to "api" mode; simulations never call it. The answer is parsed tolerantly:
// the order id is mandatory (without it the order cannot be tracked), the
// tracking number and carrier are kept when Dropi already assigned them.
func (c *Client) CreateOrder(ctx context.Context, payload map[string]any) (OrderResult, error) {
	raw, err := c.do(ctx, http.MethodPost, "/orders/myorders", payload)
	if err != nil {
		return OrderResult{}, err
	}
	return parseOrderResult(raw)
}

func parseOrderResult(raw []byte) (OrderResult, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return OrderResult{}, fmt.Errorf("respuesta de Dropi inválida: %w", err)
	}
	res := OrderResult{Raw: snippet(raw)}
	// The answer may be {"objects":{...}}, {"data":{...}} or the order itself.
	for _, m := range candidateObjects(v) {
		if res.ID == "" {
			res.ID = idStr(m, "id", "order_id", "orderId", "orden_id")
		}
		if res.Tracking == "" {
			res.Tracking = str(m, "shipping_guide", "guia", "tracking_number", "trackingNumber", "guide", "numero_guia")
		}
		if res.Carrier == "" {
			res.Carrier = str(m, "distribution_company", "distributionCompany", "transportadora", "carrier")
		}
	}
	if res.ID == "" {
		return res, fmt.Errorf("Dropi respondió sin número de orden; revisa el pedido en Dropi antes de reintentar (para no duplicarlo): %s", res.Raw)
	}
	return res, nil
}

// candidateObjects lists the objects an answer may hold the order in, outermost
// last so the most specific wins.
func candidateObjects(v any) []map[string]any {
	var out []map[string]any
	var walk func(x any, depth int)
	walk = func(x any, depth int) {
		if depth > 3 {
			return
		}
		switch t := x.(type) {
		case map[string]any:
			out = append(out, t)
			for _, k := range []string{"objects", "data", "result", "order"} {
				if y, ok := t[k]; ok {
					walk(y, depth+1)
				}
			}
		case []any:
			if len(t) > 0 {
				walk(t[0], depth+1)
			}
		}
	}
	walk(v, 0)
	// Inner objects first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// idStr reads an id that may come as a number or a string.
func idStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		switch t := m[k].(type) {
		case float64:
			if t > 0 {
				return fmt.Sprintf("%.0f", t)
			}
		case string:
			if s := strings.TrimSpace(t); s != "" {
				return s
			}
		}
	}
	return ""
}

// ---------- tolerant parsing ----------

func listFrom(raw []byte) []any {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	return asList(v)
}

func asList(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case map[string]any:
		for _, k := range []string{"objects", "data", "result", "results", "items", "departments", "quotes"} {
			if x, ok := t[k]; ok {
				if l := asList(x); len(l) > 0 {
					return l
				}
			}
		}
	}
	return nil
}

func firstObject(v any) map[string]any {
	switch t := v.(type) {
	case map[string]any:
		for _, k := range []string{"objects", "data", "result", "product"} {
			if x, ok := t[k]; ok {
				if o := firstObject(x); o != nil {
					return o
				}
			}
		}
		return t
	case []any:
		if len(t) > 0 {
			return firstObject(t[0])
		}
	}
	return nil
}

func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			switch t := v.(type) {
			case string:
				if strings.TrimSpace(t) != "" {
					return strings.TrimSpace(t)
				}
			case map[string]any:
				if s := str(t, "name", "nombre"); s != "" {
					return s
				}
			}
		}
	}
	return ""
}

func num(m map[string]any, keys ...string) int64 {
	for _, k := range keys {
		switch t := m[k].(type) {
		case float64:
			if t > 0 {
				return int64(t + 0.5)
			}
		case string:
			var f float64
			if _, err := fmt.Sscanf(strings.ReplaceAll(strings.TrimSpace(t), ",", ""), "%f", &f); err == nil && f > 0 {
				return int64(f + 0.5)
			}
		}
	}
	return 0
}

// parseQuotes accepts the documented-looking shapes: a list (possibly nested
// under objects/data/result) of carriers with a price, or a single carrier.
func parseQuotes(raw []byte) []Quote {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil
	}
	items := asList(v)
	if len(items) == 0 {
		if m, ok := v.(map[string]any); ok {
			items = []any{m}
		}
	}
	var out []Quote
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		// A nested "objects" wrapper around one carrier.
		if inner, ok := m["objects"].(map[string]any); ok {
			m = inner
		}
		name := str(m, "distribution_company", "transportadora", "carrier", "company", "name", "nombre")
		price := num(m, "shipping_amount", "shippingAmount", "flete", "precio", "price", "valor", "amount", "cost", "total")
		if name == "" || price <= 0 {
			continue
		}
		days := int(num(m, "delivery_days", "deliveryDays", "dias_entrega", "tiempo_entrega", "days", "estimated_days", "estimatedDays", "delivery_time", "deliveryTime", "tiempo_estimado"))
		if days < 0 || days > 60 {
			days = 0
		}
		out = append(out, Quote{Carrier: name, Freight: price, Days: days})
	}
	return out
}
