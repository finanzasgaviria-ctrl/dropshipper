package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Order statuses. The lifecycle the owner sees as a kanban:
//
//	borrador -> esperando_pago -> por_aprobar -> aprobado -> en_dropi -> en_transito -> entregado
//	                                         \-> rechazado                         \-> novedad / devuelto
//	cancelado (any time before en_dropi)
const (
	OrderDraft          = "borrador"
	OrderAwaitPayment   = "esperando_pago"
	OrderPendingApprove = "por_aprobar"
	OrderApproved       = "aprobado"
	OrderInDropi        = "en_dropi"
	OrderInTransit      = "en_transito"
	OrderDelivered      = "entregado"
	OrderIssue          = "novedad"
	OrderReturned       = "devuelto"
	OrderRejected       = "rechazado"
	OrderCancelled      = "cancelado"
)

// Payment statuses.
const (
	PayNotApplicable = "no_aplica"
	PayPending       = "pendiente"
	PayProofReceived = "comprobante_recibido"
	PayConfirmed     = "confirmado"
	PayFailed        = "fallido"
)

// Order is a sale being prepared or shipped.
type Order struct {
	ID               int64      `json:"id"`
	Phone            string     `json:"phone"`
	Status           string     `json:"status"`
	ProductSKU       string     `json:"product_sku"`
	ProductName      string     `json:"product_name"`
	OfferID          string     `json:"offer_id"`
	OfferLabel       string     `json:"offer_label"`
	Units            int        `json:"units"`
	Total            int64      `json:"total"`
	CustomerName     string     `json:"customer_name"`
	CustomerIDNumber string     `json:"customer_id_number"`
	CustomerPhone    string     `json:"customer_phone"`
	Department       string     `json:"department"`
	City             string     `json:"city"`
	Neighborhood     string     `json:"neighborhood"`
	Address          string     `json:"address"`
	AddressRefs      string     `json:"address_refs"`
	Email            string     `json:"email"`
	Risk             string     `json:"risk"`
	PaymentPolicy    string     `json:"payment_policy"`
	AdvanceAmount    int64      `json:"advance_amount"`
	CODAmount        int64      `json:"cod_amount"`
	PaymentStatus    string     `json:"payment_status"`
	PaymentRef       string     `json:"payment_ref"`
	PaymentLink      string     `json:"payment_link"`
	PaymentProof     string     `json:"payment_proof"`
	Carrier          string     `json:"carrier"`
	ZoneReason       string     `json:"zone_reason"`
	ProfitJSON       string     `json:"profit_json"`
	Notes            string     `json:"notes"`
	DropiOrderID     string     `json:"dropi_order_id"`
	TrackingNumber   string     `json:"tracking_number"`
	ConfirmedAt      *time.Time `json:"confirmed_at"`
	ApprovedAt       *time.Time `json:"approved_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// Open reports whether the order is still being worked on before shipping.
func (o Order) Open() bool {
	switch o.Status {
	case OrderDraft, OrderAwaitPayment, OrderPendingApprove:
		return true
	}
	return false
}

const orderCols = `id, phone, status, product_sku, product_name, offer_id, offer_label, units, total,
	customer_name, customer_id_number, customer_phone, department, city, neighborhood, address, address_refs, email,
	risk, payment_policy, advance_amount, cod_amount, payment_status, payment_ref, payment_link, payment_proof,
	carrier, zone_reason, profit_json, notes, dropi_order_id, tracking_number, confirmed_at, approved_at, created_at, updated_at`

func scanOrder(row interface{ Scan(...any) error }) (Order, error) {
	var o Order
	var conf, appr sql.NullTime
	err := row.Scan(&o.ID, &o.Phone, &o.Status, &o.ProductSKU, &o.ProductName, &o.OfferID, &o.OfferLabel, &o.Units, &o.Total,
		&o.CustomerName, &o.CustomerIDNumber, &o.CustomerPhone, &o.Department, &o.City, &o.Neighborhood, &o.Address, &o.AddressRefs, &o.Email,
		&o.Risk, &o.PaymentPolicy, &o.AdvanceAmount, &o.CODAmount, &o.PaymentStatus, &o.PaymentRef, &o.PaymentLink, &o.PaymentProof,
		&o.Carrier, &o.ZoneReason, &o.ProfitJSON, &o.Notes, &o.DropiOrderID, &o.TrackingNumber, &conf, &appr, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return o, ErrNotFound
		}
		return o, err
	}
	if conf.Valid {
		t := conf.Time
		o.ConfirmedAt = &t
	}
	if appr.Valid {
		t := appr.Time
		o.ApprovedAt = &t
	}
	return o, nil
}

// SaveOrder inserts (ID == 0) or fully updates an order.
func (s *Store) SaveOrder(ctx context.Context, o Order) (Order, error) {
	o.UpdatedAt = now()
	if o.PaymentStatus == "" {
		o.PaymentStatus = PayNotApplicable
	}
	if o.Units <= 0 {
		o.Units = 1
	}
	if o.ID == 0 {
		if o.Status == "" {
			o.Status = OrderDraft
		}
		o.CreatedAt = o.UpdatedAt
		res, err := s.db.ExecContext(ctx, `INSERT INTO orders(phone, status, product_sku, product_name, offer_id, offer_label, units, total,
			customer_name, customer_id_number, customer_phone, department, city, neighborhood, address, address_refs, email,
			risk, payment_policy, advance_amount, cod_amount, payment_status, payment_ref, payment_link, payment_proof,
			carrier, zone_reason, profit_json, notes, dropi_order_id, tracking_number, confirmed_at, approved_at, created_at, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			o.Phone, o.Status, o.ProductSKU, o.ProductName, o.OfferID, o.OfferLabel, o.Units, o.Total,
			o.CustomerName, o.CustomerIDNumber, o.CustomerPhone, o.Department, o.City, o.Neighborhood, o.Address, o.AddressRefs, o.Email,
			o.Risk, o.PaymentPolicy, o.AdvanceAmount, o.CODAmount, o.PaymentStatus, o.PaymentRef, o.PaymentLink, o.PaymentProof,
			o.Carrier, o.ZoneReason, o.ProfitJSON, o.Notes, o.DropiOrderID, o.TrackingNumber, o.ConfirmedAt, o.ApprovedAt, o.CreatedAt, o.UpdatedAt)
		if err != nil {
			return o, fmt.Errorf("crear pedido: %w", err)
		}
		o.ID, _ = res.LastInsertId()
		return o, nil
	}
	_, err := s.db.ExecContext(ctx, `UPDATE orders SET phone=?, status=?, product_sku=?, product_name=?, offer_id=?, offer_label=?, units=?, total=?,
		customer_name=?, customer_id_number=?, customer_phone=?, department=?, city=?, neighborhood=?, address=?, address_refs=?, email=?,
		risk=?, payment_policy=?, advance_amount=?, cod_amount=?, payment_status=?, payment_ref=?, payment_link=?, payment_proof=?,
		carrier=?, zone_reason=?, profit_json=?, notes=?, dropi_order_id=?, tracking_number=?, confirmed_at=?, approved_at=?, updated_at=?
		WHERE id=?`,
		o.Phone, o.Status, o.ProductSKU, o.ProductName, o.OfferID, o.OfferLabel, o.Units, o.Total,
		o.CustomerName, o.CustomerIDNumber, o.CustomerPhone, o.Department, o.City, o.Neighborhood, o.Address, o.AddressRefs, o.Email,
		o.Risk, o.PaymentPolicy, o.AdvanceAmount, o.CODAmount, o.PaymentStatus, o.PaymentRef, o.PaymentLink, o.PaymentProof,
		o.Carrier, o.ZoneReason, o.ProfitJSON, o.Notes, o.DropiOrderID, o.TrackingNumber, o.ConfirmedAt, o.ApprovedAt, o.UpdatedAt, o.ID)
	if err != nil {
		return o, fmt.Errorf("actualizar pedido %d: %w", o.ID, err)
	}
	return o, nil
}

// GetOrder returns one order.
func (s *Store) GetOrder(ctx context.Context, id int64) (Order, error) {
	return scanOrder(s.db.QueryRowContext(ctx, `SELECT `+orderCols+` FROM orders WHERE id = ?`, id))
}

// OpenOrderFor returns the most recent still-open order of a phone.
func (s *Store) OpenOrderFor(ctx context.Context, phone string) (Order, error) {
	return scanOrder(s.db.QueryRowContext(ctx, `SELECT `+orderCols+` FROM orders
		WHERE phone = ? AND status IN (?,?,?) ORDER BY id DESC LIMIT 1`,
		phone, OrderDraft, OrderAwaitPayment, OrderPendingApprove))
}

// OrdersFor returns every order of a phone, newest first.
func (s *Store) OrdersFor(ctx context.Context, phone string) ([]Order, error) {
	return s.queryOrders(ctx, `SELECT `+orderCols+` FROM orders WHERE phone = ? ORDER BY id DESC`, phone)
}

// FindOrderByPaymentRef looks an order up by its payment reference.
func (s *Store) FindOrderByPaymentRef(ctx context.Context, ref string) (Order, error) {
	return scanOrder(s.db.QueryRowContext(ctx, `SELECT `+orderCols+` FROM orders WHERE payment_ref = ? ORDER BY id DESC LIMIT 1`, ref))
}

// ListOrders returns orders filtered by status (empty = all), newest first.
func (s *Store) ListOrders(ctx context.Context, statuses ...string) ([]Order, error) {
	if len(statuses) == 0 {
		return s.queryOrders(ctx, `SELECT `+orderCols+` FROM orders ORDER BY id DESC LIMIT 1000`)
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",")
	args := make([]any, len(statuses))
	for i, st := range statuses {
		args[i] = st
	}
	return s.queryOrders(ctx, `SELECT `+orderCols+` FROM orders WHERE status IN (`+ph+`) ORDER BY id DESC LIMIT 1000`, args...)
}

func (s *Store) queryOrders(ctx context.Context, q string, args ...any) ([]Order, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		o, err := scanOrder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// Stats is the dashboard summary.
type Stats struct {
	ChatsToday         int   `json:"chats_hoy"`
	ActiveChats24h     int   `json:"chats_activos_24h"`
	AIPaused           int   `json:"ia_pausada"`
	PendingApproval    int   `json:"por_aprobar"`
	AwaitingPayment    int   `json:"esperando_pago"`
	Drafts             int   `json:"borradores"`
	ApprovedToday      int   `json:"aprobados_hoy"`
	SalesToday         int64 `json:"ventas_hoy"`
	SalesMonth         int64 `json:"ventas_mes"`
	OrdersMonth        int   `json:"pedidos_mes"`
	OpenAlerts         int   `json:"alertas_abiertas"`
	MessagesInToday    int   `json:"mensajes_entrantes_hoy"`
	MessagesOutAIToday int   `json:"mensajes_ia_hoy"`
}

// Stats computes the dashboard counters. "Today" uses the Colombia day (UTC-5).
func (s *Store) Stats(ctx context.Context) (Stats, error) {
	var st Stats
	loc := time.FixedZone("COT", -5*3600)
	nowCO := time.Now().In(loc)
	dayStart := time.Date(nowCO.Year(), nowCO.Month(), nowCO.Day(), 0, 0, 0, 0, loc).UTC()
	monthStart := time.Date(nowCO.Year(), nowCO.Month(), 1, 0, 0, 0, 0, loc).UTC()
	sold := []any{OrderApproved, OrderInDropi, OrderInTransit, OrderDelivered}

	q := func(dst any, query string, args ...any) error {
		return s.db.QueryRowContext(ctx, query, args...).Scan(dst)
	}
	steps := []error{
		q(&st.ChatsToday, `SELECT COUNT(DISTINCT phone) FROM messages WHERE direction='in' AND created_at >= ?`, dayStart),
		q(&st.ActiveChats24h, `SELECT COUNT(*) FROM contacts WHERE last_inbound_at >= ?`, now().Add(-24*time.Hour)),
		q(&st.AIPaused, `SELECT COUNT(*) FROM contacts WHERE ai_paused = 1`),
		q(&st.PendingApproval, `SELECT COUNT(*) FROM orders WHERE status = ?`, OrderPendingApprove),
		q(&st.AwaitingPayment, `SELECT COUNT(*) FROM orders WHERE status = ?`, OrderAwaitPayment),
		q(&st.Drafts, `SELECT COUNT(*) FROM orders WHERE status = ?`, OrderDraft),
		q(&st.ApprovedToday, `SELECT COUNT(*) FROM orders WHERE approved_at >= ?`, dayStart),
		q(&st.SalesToday, `SELECT COALESCE(SUM(total),0) FROM orders WHERE approved_at >= ? AND status IN (?,?,?,?)`, append([]any{dayStart}, sold...)...),
		q(&st.SalesMonth, `SELECT COALESCE(SUM(total),0) FROM orders WHERE approved_at >= ? AND status IN (?,?,?,?)`, append([]any{monthStart}, sold...)...),
		q(&st.OrdersMonth, `SELECT COUNT(*) FROM orders WHERE approved_at >= ? AND status IN (?,?,?,?)`, append([]any{monthStart}, sold...)...),
		q(&st.OpenAlerts, `SELECT COUNT(*) FROM alerts WHERE resolved = 0`),
		q(&st.MessagesInToday, `SELECT COUNT(*) FROM messages WHERE direction='in' AND created_at >= ?`, dayStart),
		q(&st.MessagesOutAIToday, `SELECT COUNT(*) FROM messages WHERE sender='ia' AND created_at >= ?`, dayStart),
	}
	for _, err := range steps {
		if err != nil {
			return st, err
		}
	}
	return st, nil
}

// DeleteOrder removes an order and its alerts (used to clean test orders).
// The conversation and the customer are left untouched. The removal is
// written to the audit trail so it is never silent.
func (s *Store) DeleteOrder(ctx context.Context, id int64, by string) (Order, error) {
	o, err := s.GetOrder(ctx, id)
	if err != nil {
		return o, err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM alerts WHERE order_id = ?`, id); err != nil {
		return o, err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM orders WHERE id = ?`, id); err != nil {
		return o, err
	}
	s.Audit(ctx, o.Phone, by, "pedido_borrado", fmt.Sprintf("pedido #%d (%s, %s) borrado", o.ID, o.Status, o.ProductName))
	return o, nil
}

// DeleteSimOrders removes every order created from the simulator (phones
// "sim-…") that never reached Dropi. Returns how many were deleted.
func (s *Store) DeleteSimOrders(ctx context.Context, by string) (int, error) {
	all, err := s.ListOrders(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, o := range all {
		if !strings.HasPrefix(o.Phone, "sim-") || o.DropiOrderID != "" {
			continue
		}
		if _, err := s.DeleteOrder(ctx, o.ID, by); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
