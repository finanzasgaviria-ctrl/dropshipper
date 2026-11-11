package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Candidate statuses.
const (
	CandNew       = "nuevo"
	CandTesting   = "probando"
	CandDiscarded = "descartado"
	CandConverted = "convertido"
)

// Candidate is a product the owner is considering selling. The numbers come
// from what the owner sees in Dropi and in DropKiller; -1 means "not known".
type Candidate struct {
	ID             int64     `json:"id"`
	Name           string    `json:"nombre"`
	DropiProductID string    `json:"dropi_product_id"`
	Source         string    `json:"fuente"`
	Cost           int64     `json:"costo"`
	SalePrice      int64     `json:"precio_venta"`
	Freight        int64     `json:"flete"`
	Stock          int64     `json:"stock"`
	Sales7d        int       `json:"ventas_7d"`
	Advertisers    int       `json:"anunciantes"`
	Link           string    `json:"enlace"`
	Notes          string    `json:"notas"`
	Status         string    `json:"estado"`
	ProductSKU     string    `json:"producto_sku"`
	CreatedAt      time.Time `json:"creado"`
	UpdatedAt      time.Time `json:"actualizado"`
}

const candCols = `id, name, dropi_product_id, source, cost, sale_price, freight, stock, sales7d, advertisers, link, notes, status, product_sku, created_at, updated_at`

func scanCandidate(row interface{ Scan(...any) error }) (Candidate, error) {
	var c Candidate
	err := row.Scan(&c.ID, &c.Name, &c.DropiProductID, &c.Source, &c.Cost, &c.SalePrice, &c.Freight, &c.Stock,
		&c.Sales7d, &c.Advertisers, &c.Link, &c.Notes, &c.Status, &c.ProductSKU, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

func normCandidate(c *Candidate) {
	c.Name = truncate(strings.TrimSpace(c.Name), 200)
	c.DropiProductID = truncate(strings.TrimSpace(c.DropiProductID), 60)
	c.Source = truncate(strings.TrimSpace(c.Source), 40)
	c.Link = truncate(strings.TrimSpace(c.Link), 500)
	c.Notes = truncate(strings.TrimSpace(c.Notes), 2000)
	if c.Stock < -1 {
		c.Stock = -1
	}
	if c.Sales7d < -1 {
		c.Sales7d = -1
	}
	if c.Advertisers < -1 {
		c.Advertisers = -1
	}
	if c.Cost < 0 {
		c.Cost = 0
	}
	if c.SalePrice < 0 {
		c.SalePrice = 0
	}
	if c.Freight < 0 {
		c.Freight = 0
	}
	switch c.Status {
	case CandNew, CandTesting, CandDiscarded, CandConverted:
	default:
		c.Status = CandNew
	}
}

// SaveCandidate inserts (ID 0) or updates a candidate.
func (s *Store) SaveCandidate(ctx context.Context, c Candidate) (Candidate, error) {
	normCandidate(&c)
	if c.Name == "" {
		return c, errors.New("ponle un nombre al producto")
	}
	t := now()
	if c.ID == 0 {
		c.CreatedAt, c.UpdatedAt = t, t
		res, err := s.db.ExecContext(ctx, `INSERT INTO candidates(name, dropi_product_id, source, cost, sale_price, freight, stock, sales7d, advertisers, link, notes, status, product_sku, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			c.Name, c.DropiProductID, c.Source, c.Cost, c.SalePrice, c.Freight, c.Stock, c.Sales7d, c.Advertisers, c.Link, c.Notes, c.Status, c.ProductSKU, c.CreatedAt, c.UpdatedAt)
		if err != nil {
			return c, err
		}
		c.ID, _ = res.LastInsertId()
		return c, nil
	}
	c.UpdatedAt = t
	res, err := s.db.ExecContext(ctx, `UPDATE candidates SET name=?, dropi_product_id=?, source=?, cost=?, sale_price=?, freight=?, stock=?, sales7d=?, advertisers=?, link=?, notes=?, status=?, product_sku=?, updated_at=? WHERE id=?`,
		c.Name, c.DropiProductID, c.Source, c.Cost, c.SalePrice, c.Freight, c.Stock, c.Sales7d, c.Advertisers, c.Link, c.Notes, c.Status, c.ProductSKU, c.UpdatedAt, c.ID)
	if err != nil {
		return c, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return c, ErrNotFound
	}
	return s.GetCandidate(ctx, c.ID)
}

// GetCandidate reads one candidate.
func (s *Store) GetCandidate(ctx context.Context, id int64) (Candidate, error) {
	return scanCandidate(s.db.QueryRowContext(ctx, `SELECT `+candCols+` FROM candidates WHERE id = ?`, id))
}

// ListCandidates returns every candidate, newest first.
func (s *Store) ListCandidates(ctx context.Context) ([]Candidate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+candCols+` FROM candidates ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Candidate{}
	for rows.Next() {
		c, err := scanCandidate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteCandidate removes a candidate (the product created from it, if any, stays).
func (s *Store) DeleteCandidate(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM candidates WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
