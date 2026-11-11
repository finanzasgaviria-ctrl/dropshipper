package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode"

	"github.com/michiTrader/dropshipper/motor/internal/catalog"
	"github.com/michiTrader/dropshipper/motor/internal/convo"
	"github.com/michiTrader/dropshipper/motor/internal/hub"
	"github.com/michiTrader/dropshipper/motor/internal/hunter"
	"github.com/michiTrader/dropshipper/motor/internal/store"
)

// hunterRoutes: the "Cazador" tab. The owner types what DropKiller and Dropi
// show; the Motor does the math and says whether the product is worth trying.
func (s *Server) hunterRoutes(api *http.ServeMux) {
	api.HandleFunc("GET /api/candidatos", s.listCandidates)
	api.HandleFunc("POST /api/candidatos", s.saveCandidate)
	api.HandleFunc("POST /api/candidatos/calcular", s.calcCandidate)
	api.HandleFunc("PUT /api/candidatos/{id}", s.saveCandidate)
	api.HandleFunc("DELETE /api/candidatos/{id}", s.deleteCandidate)
	api.HandleFunc("POST /api/candidatos/{id}/traer-dropi", s.candidateFromDropi)
	api.HandleFunc("POST /api/candidatos/{id}/crear-producto", s.candidateToProduct)
}

type candidateOut struct {
	store.Candidate
	Verdict hunter.Verdict `json:"veredicto"`
}

func (s *Server) judge(c store.Candidate) candidateOut {
	n := s.Settings.Get().Negocio
	return candidateOut{Candidate: c, Verdict: hunter.Evaluate(c, n.Finanzas, n.Cazador)}
}

func (s *Server) listCandidates(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.ListCandidates(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]candidateOut, 0, len(list))
	for _, c := range list {
		out = append(out, s.judge(c))
	}
	writeJSON(w, 200, out)
}

func readCandidate(r *http.Request) (store.Candidate, error) {
	// Unknown numbers arrive as null/absent: they must stay "-1 = unknown".
	in := struct {
		store.Candidate
		Stock       *int64 `json:"stock"`
		Sales7d     *int   `json:"ventas_7d"`
		Advertisers *int   `json:"anunciantes"`
	}{}
	if err := readJSON(r, &in); err != nil {
		return store.Candidate{}, errors.New("los datos del producto no son válidos")
	}
	c := in.Candidate
	c.Stock, c.Sales7d, c.Advertisers = -1, -1, -1
	if in.Stock != nil {
		c.Stock = *in.Stock
	}
	if in.Sales7d != nil {
		c.Sales7d = *in.Sales7d
	}
	if in.Advertisers != nil {
		c.Advertisers = *in.Advertisers
	}
	return c, nil
}

func (s *Server) calcCandidate(w http.ResponseWriter, r *http.Request) {
	c, err := readCandidate(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, s.judge(c))
}

func (s *Server) saveCandidate(w http.ResponseWriter, r *http.Request) {
	c, err := readCandidate(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	c.ID, c.ProductSKU = 0, "" // never trusted from the client
	if v := r.PathValue("id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeErr(w, errors.New("id inválido"))
			return
		}
		old, err := s.Store.GetCandidate(r.Context(), id)
		if err != nil {
			writeErr(w, err)
			return
		}
		c.ID, c.ProductSKU, c.CreatedAt = id, old.ProductSKU, old.CreatedAt
		if c.Status == "" {
			c.Status = old.Status
		}
	}
	saved, err := s.Store.SaveCandidate(r.Context(), c)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, s.judge(saved))
}

func (s *Server) deleteCandidate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Store.DeleteCandidate(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// candidateFromDropi fills name, cost and stock from the Dropi catalog.
func (s *Server) candidateFromDropi(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	c, err := s.Store.GetCandidate(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	client := s.Hub.Dropi()
	if client == nil {
		writeErr(w, errors.New("conecta Dropi primero (Conexiones > Dropi)"))
		return
	}
	if strings.TrimSpace(c.DropiProductID) == "" {
		writeErr(w, errors.New("escribe el ID del producto en Dropi (aparece en la página del producto)"))
		return
	}
	cctx, cancel := context.WithTimeout(r.Context(), hub.TestTimeout)
	defer cancel()
	dp, err := client.Product(cctx, c.DropiProductID)
	if err != nil {
		writeErr(w, errors.New(dropiAdvice(err)))
		return
	}
	if dp.Cost > 0 {
		c.Cost = dp.Cost
	}
	c.Stock = dp.Stock
	if dp.SalePrice > 0 && c.SalePrice == 0 {
		c.SalePrice = dp.SalePrice
	}
	if c.Name == "" || strings.HasPrefix(c.Name, "(sin nombre)") {
		c.Name = dp.Name
	}
	saved, err := s.Store.SaveCandidate(r.Context(), c)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, s.judge(saved))
}

// skuFrom builds a catalog code ("LULLA 60!" -> "LULLA-60").
func skuFrom(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToUpper(name) {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 24 {
		out = strings.Trim(out[:24], "-")
	}
	if out == "" {
		out = "PRODUCTO"
	}
	return out
}

// candidateToProduct creates an INACTIVE product sheet from the candidate so
// the owner can finish it (offers, arguments) before the AI sells it.
func (s *Server) candidateToProduct(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	c, err := s.Store.GetCandidate(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if c.ProductSKU != "" {
		if _, ok := s.Catalog.Get(c.ProductSKU); ok {
			writeErr(w, fmt.Errorf("este producto ya se creó como %s; no se crea otro", c.ProductSKU))
			return
		}
	}
	if c.Cost <= 0 || c.SalePrice <= 0 {
		writeErr(w, errors.New("falta el costo o el precio de venta"))
		return
	}
	base := skuFrom(c.Name)
	sku := base
	for i := 2; ; i++ {
		if _, exists := s.Catalog.Get(sku); !exists {
			break
		}
		sku = fmt.Sprintf("%s-%d", base, i)
	}
	p := catalog.Product{
		SKU: sku, Name: c.Name, UnitCost: c.Cost, Freight: c.Freight, DropiProductID: c.DropiProductID,
		Offers: []catalog.Offer{{ID: "x1", Label: "1 unidad", Units: 1, Price: c.SalePrice, Highlight: true}},
	}
	cleanProduct(&p)
	if _, err := s.Catalog.Save(r.Context(), p); err != nil {
		writeErr(w, err)
		return
	}
	// A new product is never sold until the owner turns it on: Save keeps it
	// inactive except when it is the very first product of the catalog.
	c.ProductSKU, c.Status = sku, store.CandConverted
	saved, err := s.Store.SaveCandidate(r.Context(), c)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.Engine.Events.Publish(convo.Event{Type: "producto"})
	out := s.judge(saved)
	writeJSON(w, 200, map[string]any{"candidato": out, "sku": sku})
}
