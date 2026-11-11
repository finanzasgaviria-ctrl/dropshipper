// Package store is the Motor's persistence layer (SQLite, pure Go, no CGO).
//
// Everything the business needs to survive a restart lives here: contacts,
// every message in and out, orders with their full lifecycle, alerts for the
// owner and an audit trail. SQLite is enough for one store with thousands of
// chats; the same schema moves to PostgreSQL when it stops being enough.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("no encontrado")

// Store wraps the database handle.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the database at dir/dropshipper.db and migrates it.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("crear %s: %w", dir, err)
	}
	return OpenPath(filepath.Join(dir, "dropshipper.db"))
}

// OpenPath opens a database at an explicit path (":memory:" works for tests).
func OpenPath(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		dsn = "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite allows one writer; a single connection keeps things simple and
	// correct (and :memory: needs it to share one database).
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS contacts (
			phone TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			ai_paused INTEGER NOT NULL DEFAULT 0,
			stage TEXT NOT NULL DEFAULT 'nuevo',
			simulated INTEGER NOT NULL DEFAULT 0,
			notes TEXT NOT NULL DEFAULT '',
			unread INTEGER NOT NULL DEFAULT 0,
			last_message_at TIMESTAMP,
			last_inbound_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			phone TEXT NOT NULL,
			direction TEXT NOT NULL,
			sender TEXT NOT NULL,
			kind TEXT NOT NULL DEFAULT 'text',
			body TEXT NOT NULL DEFAULT '',
			media_path TEXT NOT NULL DEFAULT '',
			wa_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_phone ON messages(phone, id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_wa ON messages(wa_id) WHERE wa_id <> ''`,
		`CREATE TABLE IF NOT EXISTS orders (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			phone TEXT NOT NULL,
			status TEXT NOT NULL,
			product_sku TEXT NOT NULL,
			product_name TEXT NOT NULL,
			offer_id TEXT NOT NULL DEFAULT '',
			offer_label TEXT NOT NULL DEFAULT '',
			units INTEGER NOT NULL DEFAULT 1,
			total INTEGER NOT NULL,
			customer_name TEXT NOT NULL DEFAULT '',
			customer_id_number TEXT NOT NULL DEFAULT '',
			customer_phone TEXT NOT NULL DEFAULT '',
			department TEXT NOT NULL DEFAULT '',
			city TEXT NOT NULL DEFAULT '',
			neighborhood TEXT NOT NULL DEFAULT '',
			address TEXT NOT NULL DEFAULT '',
			address_refs TEXT NOT NULL DEFAULT '',
			email TEXT NOT NULL DEFAULT '',
			risk TEXT NOT NULL DEFAULT '',
			payment_policy TEXT NOT NULL DEFAULT '',
			advance_amount INTEGER NOT NULL DEFAULT 0,
			cod_amount INTEGER NOT NULL DEFAULT 0,
			payment_status TEXT NOT NULL DEFAULT 'no_aplica',
			payment_ref TEXT NOT NULL DEFAULT '',
			payment_link TEXT NOT NULL DEFAULT '',
			payment_proof TEXT NOT NULL DEFAULT '',
			carrier TEXT NOT NULL DEFAULT '',
			zone_reason TEXT NOT NULL DEFAULT '',
			profit_json TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			dropi_order_id TEXT NOT NULL DEFAULT '',
			tracking_number TEXT NOT NULL DEFAULT '',
			confirmed_at TIMESTAMP,
			approved_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status)`,
		`CREATE INDEX IF NOT EXISTS idx_orders_phone ON orders(phone)`,
		`CREATE TABLE IF NOT EXISTS alerts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			phone TEXT NOT NULL DEFAULT '',
			order_id INTEGER NOT NULL DEFAULT 0,
			kind TEXT NOT NULL,
			message TEXT NOT NULL,
			resolved INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS audit (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			phone TEXT NOT NULL DEFAULT '',
			actor TEXT NOT NULL,
			action TEXT NOT NULL,
			detail TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_phone ON audit(phone, id)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			rev INTEGER NOT NULL DEFAULT 0,
			updated_at TIMESTAMP NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS products (
			sku TEXT PRIMARY KEY,
			data TEXT NOT NULL,
			active INTEGER NOT NULL DEFAULT 1,
			position INTEGER NOT NULL DEFAULT 0,
			updated_at TIMESTAMP NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS candidates (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			dropi_product_id TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL DEFAULT '',
			cost INTEGER NOT NULL DEFAULT 0,
			sale_price INTEGER NOT NULL DEFAULT 0,
			freight INTEGER NOT NULL DEFAULT 0,
			stock INTEGER NOT NULL DEFAULT -1,
			sales7d INTEGER NOT NULL DEFAULT -1,
			advertisers INTEGER NOT NULL DEFAULT -1,
			link TEXT NOT NULL DEFAULT '',
			notes TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'nuevo',
			product_sku TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`,
	}
	for _, st := range stmts {
		if _, err := s.db.Exec(st); err != nil {
			return fmt.Errorf("migrar: %w\n%s", err, st)
		}
	}
	return nil
}

func now() time.Time { return time.Now().UTC() }

// ---------- contacts ----------

// Contact is one WhatsApp conversation partner.
type Contact struct {
	Phone         string     `json:"phone"`
	Name          string     `json:"name"`
	AIPaused      bool       `json:"ai_paused"`
	Stage         string     `json:"stage"`
	Simulated     bool       `json:"simulated"`
	Notes         string     `json:"notes"`
	Unread        int        `json:"unread"`
	LastMessageAt *time.Time `json:"last_message_at"`
	LastInboundAt *time.Time `json:"last_inbound_at"`
	CreatedAt     time.Time  `json:"created_at"`
	LastPreview   string     `json:"last_preview,omitempty"`
}

// EnsureContact creates the contact if missing and fills the name if empty.
func (s *Store) EnsureContact(ctx context.Context, phone, name string, simulated bool) (Contact, error) {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO contacts(phone, name, simulated, created_at) VALUES(?,?,?,?)
		 ON CONFLICT(phone) DO UPDATE SET name = CASE WHEN excluded.name <> '' AND contacts.name = '' THEN excluded.name ELSE contacts.name END`,
		phone, name, boolInt(simulated), now())
	if err != nil {
		return Contact{}, err
	}
	return s.GetContact(ctx, phone)
}

const contactCols = `phone, name, ai_paused, stage, simulated, notes, unread, last_message_at, last_inbound_at, created_at`

func scanContact(row interface{ Scan(...any) error }) (Contact, error) {
	var c Contact
	var paused, sim int
	var lm, li sql.NullTime
	if err := row.Scan(&c.Phone, &c.Name, &paused, &c.Stage, &sim, &c.Notes, &c.Unread, &lm, &li, &c.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c, ErrNotFound
		}
		return c, err
	}
	c.AIPaused, c.Simulated = paused == 1, sim == 1
	if lm.Valid {
		t := lm.Time
		c.LastMessageAt = &t
	}
	if li.Valid {
		t := li.Time
		c.LastInboundAt = &t
	}
	return c, nil
}

// GetContact returns one contact.
func (s *Store) GetContact(ctx context.Context, phone string) (Contact, error) {
	return scanContact(s.db.QueryRowContext(ctx, `SELECT `+contactCols+` FROM contacts WHERE phone = ?`, phone))
}

// ListContacts returns contacts ordered by most recent activity, with a preview.
func (s *Store) ListContacts(ctx context.Context) ([]Contact, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+contactCols+` FROM contacts ORDER BY COALESCE(last_message_at, created_at) DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	var out []Contact
	for rows.Next() {
		c, err := scanContact(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		var body, kind string
		err := s.db.QueryRowContext(ctx, `SELECT body, kind FROM messages WHERE phone = ? ORDER BY id DESC LIMIT 1`, out[i].Phone).Scan(&body, &kind)
		if err == nil {
			if body == "" {
				body = "[" + kind + "]"
			}
			out[i].LastPreview = truncate(body, 80)
		}
	}
	return out, nil
}

// SetAIPaused pauses or resumes the AI for a contact ("Yo tomo").
func (s *Store) SetAIPaused(ctx context.Context, phone string, paused bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE contacts SET ai_paused = ? WHERE phone = ?`, boolInt(paused), phone)
	return err
}

// SetStage records the sales stage of a contact.
func (s *Store) SetStage(ctx context.Context, phone, stage string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE contacts SET stage = ? WHERE phone = ?`, stage, phone)
	return err
}

// SetContactName updates the display name.
func (s *Store) SetContactName(ctx context.Context, phone, name string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE contacts SET name = ? WHERE phone = ?`, name, phone)
	return err
}

// SetNotes stores the agent's private notes about a customer.
func (s *Store) SetNotes(ctx context.Context, phone, notes string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE contacts SET notes = ? WHERE phone = ?`, truncate(notes, 2000), phone)
	return err
}

// MarkRead clears the unread counter.
func (s *Store) MarkRead(ctx context.Context, phone string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE contacts SET unread = 0 WHERE phone = ?`, phone)
	return err
}

// DeleteContact removes a contact and everything attached (simulator resets).
func (s *Store) DeleteContact(ctx context.Context, phone string) error {
	for _, q := range []string{
		`DELETE FROM messages WHERE phone = ?`, `DELETE FROM orders WHERE phone = ?`,
		`DELETE FROM alerts WHERE phone = ?`, `DELETE FROM audit WHERE phone = ?`,
		`DELETE FROM contacts WHERE phone = ?`,
	} {
		if _, err := s.db.ExecContext(ctx, q, phone); err != nil {
			return err
		}
	}
	return nil
}

// ---------- messages ----------

// Message directions and senders.
const (
	DirIn  = "in"
	DirOut = "out"

	SenderCustomer = "cliente"
	SenderAI       = "ia"
	SenderOwner    = "dueno"
	SenderSystem   = "sistema"
)

// Message is one chat bubble.
type Message struct {
	ID        int64     `json:"id"`
	Phone     string    `json:"phone"`
	Direction string    `json:"direction"`
	Sender    string    `json:"sender"`
	Kind      string    `json:"kind"`
	Body      string    `json:"body"`
	MediaPath string    `json:"media_path,omitempty"`
	WAID      string    `json:"wa_id,omitempty"`
	Status    string    `json:"status,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ErrDuplicate is returned when a WhatsApp message id was already stored
// (Meta retries webhooks; the same message must never be answered twice).
var ErrDuplicate = errors.New("mensaje duplicado")

// AddMessage stores a message and updates contact activity counters.
func (s *Store) AddMessage(ctx context.Context, m Message) (Message, error) {
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now()
	}
	if m.Kind == "" {
		m.Kind = "text"
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO messages(phone, direction, sender, kind, body, media_path, wa_id, status, created_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		m.Phone, m.Direction, m.Sender, m.Kind, m.Body, m.MediaPath, m.WAID, m.Status, m.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return m, ErrDuplicate
		}
		return m, err
	}
	m.ID, _ = res.LastInsertId()
	if m.Direction == DirIn {
		_, err = s.db.ExecContext(ctx, `UPDATE contacts SET last_message_at = ?, last_inbound_at = ?, unread = unread + 1 WHERE phone = ?`, m.CreatedAt, m.CreatedAt, m.Phone)
	} else {
		_, err = s.db.ExecContext(ctx, `UPDATE contacts SET last_message_at = ? WHERE phone = ?`, m.CreatedAt, m.Phone)
	}
	return m, err
}

// UpdateMessageStatus updates delivery status by WhatsApp id.
func (s *Store) UpdateMessageStatus(ctx context.Context, waID, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET status = ? WHERE wa_id = ?`, status, waID)
	return err
}

// SetMessageWAID records the WhatsApp id assigned to an outbound message.
func (s *Store) SetMessageWAID(ctx context.Context, id int64, waID, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE messages SET wa_id = ?, status = ? WHERE id = ?`, waID, status, id)
	return err
}

func (s *Store) queryMessages(ctx context.Context, q string, args ...any) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Phone, &m.Direction, &m.Sender, &m.Kind, &m.Body, &m.MediaPath, &m.WAID, &m.Status, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Messages returns messages for a phone with id > afterID (0 = all), oldest first.
func (s *Store) Messages(ctx context.Context, phone string, afterID int64, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 500
	}
	return s.queryMessages(ctx, `SELECT id, phone, direction, sender, kind, body, media_path, wa_id, status, created_at FROM messages
		 WHERE phone = ? AND id > ? ORDER BY id ASC LIMIT ?`, phone, afterID, limit)
}

// RecentMessages returns the last n messages for a phone, oldest first.
func (s *Store) RecentMessages(ctx context.Context, phone string, n int) ([]Message, error) {
	return s.queryMessages(ctx, `SELECT id, phone, direction, sender, kind, body, media_path, wa_id, status, created_at FROM
		 (SELECT * FROM messages WHERE phone = ? ORDER BY id DESC LIMIT ?) ORDER BY id ASC`, phone, n)
}

// GetMessage returns one message.
func (s *Store) GetMessage(ctx context.Context, id int64) (Message, error) {
	ms, err := s.queryMessages(ctx, `SELECT id, phone, direction, sender, kind, body, media_path, wa_id, status, created_at FROM messages WHERE id = ?`, id)
	if err != nil {
		return Message{}, err
	}
	if len(ms) == 0 {
		return Message{}, ErrNotFound
	}
	return ms[0], nil
}

// ---------- alerts ----------

// Alert kinds.
const (
	AlertEscalation   = "escalamiento"
	AlertApproval     = "aprobacion"
	AlertPaymentProof = "comprobante"
	AlertError        = "error"
	AlertInfo         = "info"
)

// Alert is something the owner should look at.
type Alert struct {
	ID        int64     `json:"id"`
	Phone     string    `json:"phone"`
	OrderID   int64     `json:"order_id"`
	Kind      string    `json:"kind"`
	Message   string    `json:"message"`
	Resolved  bool      `json:"resolved"`
	CreatedAt time.Time `json:"created_at"`
}

// AddAlert records an alert.
func (s *Store) AddAlert(ctx context.Context, a Alert) (Alert, error) {
	a.CreatedAt = now()
	res, err := s.db.ExecContext(ctx, `INSERT INTO alerts(phone, order_id, kind, message, created_at) VALUES(?,?,?,?,?)`,
		a.Phone, a.OrderID, a.Kind, a.Message, a.CreatedAt)
	if err != nil {
		return a, err
	}
	a.ID, _ = res.LastInsertId()
	return a, nil
}

// OpenAlerts lists unresolved alerts, newest first.
func (s *Store) OpenAlerts(ctx context.Context) ([]Alert, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, phone, order_id, kind, message, resolved, created_at FROM alerts WHERE resolved = 0 ORDER BY id DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		var a Alert
		var r int
		if err := rows.Scan(&a.ID, &a.Phone, &a.OrderID, &a.Kind, &a.Message, &r, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.Resolved = r == 1
		out = append(out, a)
	}
	return out, rows.Err()
}

// ResolveAlert marks an alert as handled.
func (s *Store) ResolveAlert(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE alerts SET resolved = 1 WHERE id = ?`, id)
	return err
}

// ResolveOrderAlerts closes every alert attached to an order.
func (s *Store) ResolveOrderAlerts(ctx context.Context, orderID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE alerts SET resolved = 1 WHERE order_id = ?`, orderID)
	return err
}

// ResolvePhoneAlerts closes the escalation alerts of a phone.
func (s *Store) ResolvePhoneAlerts(ctx context.Context, phone, kind string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE alerts SET resolved = 1 WHERE phone = ? AND kind = ?`, phone, kind)
	return err
}

// ---------- audit ----------

// Audit records who did what (AI tool calls, owner actions). It is the
// "why did the AI do that" trail.
func (s *Store) Audit(ctx context.Context, phone, actor, action, detail string) {
	_, _ = s.db.ExecContext(ctx, `INSERT INTO audit(phone, actor, action, detail, created_at) VALUES(?,?,?,?,?)`,
		phone, actor, action, truncate(detail, 4000), now())
}

// AuditEntry is one audit row.
type AuditEntry struct {
	ID        int64     `json:"id"`
	Phone     string    `json:"phone"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Detail    string    `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}

// AuditFor returns the audit trail of a phone (oldest first).
func (s *Store) AuditFor(ctx context.Context, phone string, limit int) ([]AuditEntry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, phone, actor, action, detail, created_at FROM
		(SELECT * FROM audit WHERE phone = ? ORDER BY id DESC LIMIT ?) ORDER BY id ASC`, phone, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var a AuditEntry
		if err := rows.Scan(&a.ID, &a.Phone, &a.Actor, &a.Action, &a.Detail, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
