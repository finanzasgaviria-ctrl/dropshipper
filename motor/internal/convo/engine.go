// Package convo is the conversation engine: it receives every inbound
// message (WhatsApp webhook or the simulator), stores it, decides whether the
// AI should answer, and delivers the answer bubble by bubble.
//
// Rules it enforces, so no channel can skip them:
//   - Customers often send 3 short messages in a row; the engine waits a few
//     seconds after the last one and answers them together (debounce).
//   - One answer at a time per customer (no crossed replies).
//   - "Yo tomo": when the AI is paused for a chat, inbound messages are stored
//     and shown to the owner, and the AI stays silent.
//   - An image received while an order waits for its advance is registered as
//     a payment receipt for the owner to verify.
package convo

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/michiTrader/dropshipper/motor/internal/agent"
	"github.com/michiTrader/dropshipper/motor/internal/config"
	"github.com/michiTrader/dropshipper/motor/internal/llm"
	"github.com/michiTrader/dropshipper/motor/internal/sales"
	"github.com/michiTrader/dropshipper/motor/internal/store"
)

// Sender delivers messages to real WhatsApp users. Nil = simulator only.
type Sender interface {
	SendText(ctx context.Context, to, text string) (waID string, err error)
	MarkRead(ctx context.Context, waID string, typing bool) error
}

// Responder is the AI agent (agent.Seller in production, a fake in tests).
type Responder interface {
	Respond(ctx context.Context, phone string) (agent.Reply, error)
}

// Inbound is one message from a customer.
type Inbound struct {
	Phone     string
	Name      string
	Kind      string // text, image, audio, video, document, sticker, location
	Text      string
	MediaPath string
	WAID      string
	Simulated bool
	At        time.Time
}

// Engine wires storage, the agent, the sales service and the channels.
type Engine struct {
	Store   *store.Store
	Agent   Responder
	Sales   *sales.Service
	WA      Sender
	Negocio *config.Live
	Events  *Broker
	Log     *log.Logger

	// Debounce and BubblePause override the live business settings (tests).
	// Zero = use the settings; BubblePause < 0 disables the pause.
	Debounce    time.Duration
	BubblePause time.Duration

	mu      sync.Mutex
	timers  map[string]*time.Timer
	locks   map[string]*sync.Mutex
	pending map[string]string // last inbound WhatsApp id per phone (for read receipts)
	wg      sync.WaitGroup
	closed  bool
}

// NewEngine builds an engine with its own event broker.
func NewEngine(st *store.Store, ag Responder, sv *sales.Service, wa Sender, neg *config.Live, lg *log.Logger) *Engine {
	if lg == nil {
		lg = log.Default()
	}
	return &Engine{
		Store: st, Agent: ag, Sales: sv, WA: wa, Negocio: neg, Events: NewBroker(), Log: lg,
		timers: map[string]*time.Timer{}, locks: map[string]*sync.Mutex{}, pending: map[string]string{},
	}
}

func (e *Engine) neg() config.Negocio {
	if e.Negocio == nil {
		return config.DefaultNegocio()
	}
	return e.Negocio.Get()
}

func (e *Engine) debounce() time.Duration {
	if e.Debounce != 0 {
		if e.Debounce < 0 {
			return 0
		}
		return e.Debounce
	}
	return e.neg().Conversacion.AgruparDuration()
}

func (e *Engine) bubblePause() time.Duration {
	if e.BubblePause != 0 {
		if e.BubblePause < 0 {
			return 0
		}
		return e.BubblePause
	}
	return time.Duration(e.neg().Conversacion.PausaEntreMensajesMs) * time.Millisecond
}

// waReady reports whether WhatsApp can send right now. The sender may be a
// hub whose credentials change while the Motor runs.
func (e *Engine) waReady() bool {
	if e.WA == nil {
		return false
	}
	if a, ok := e.WA.(interface{ Available() bool }); ok {
		return a.Available()
	}
	return true
}

// HandleInbound stores a customer message and schedules the AI answer.
func (e *Engine) HandleInbound(ctx context.Context, in Inbound) error {
	if in.Phone == "" {
		return errors.New("mensaje sin teléfono")
	}
	if in.Kind == "" {
		in.Kind = "text"
	}
	if _, err := e.Store.EnsureContact(ctx, in.Phone, in.Name, in.Simulated); err != nil {
		return err
	}
	msg, err := e.Store.AddMessage(ctx, store.Message{
		Phone: in.Phone, Direction: store.DirIn, Sender: store.SenderCustomer,
		Kind: in.Kind, Body: in.Text, MediaPath: in.MediaPath, WAID: in.WAID, CreatedAt: in.At,
	})
	if errors.Is(err, store.ErrDuplicate) {
		return nil // Meta retried the webhook: already handled.
	}
	if err != nil {
		return err
	}
	e.Events.Publish(Event{Type: "mensaje", Phone: in.Phone, ID: msg.ID})

	if in.Kind == "image" || in.Kind == "document" {
		if o, ok, err := e.Sales.RegisterProof(ctx, in.Phone, in.MediaPath); err != nil {
			e.Log.Printf("comprobante %s: %v", in.Phone, err)
		} else if ok {
			e.Events.Publish(Event{Type: "pedido", Phone: in.Phone, ID: o.ID})
		}
	}

	contact, err := e.Store.GetContact(ctx, in.Phone)
	if err != nil {
		return err
	}
	if contact.Archived {
		// A customer writing again brings the chat back to the inbox so the
		// archived ones never hide a live conversation.
		if err := e.Store.SetArchived(ctx, in.Phone, false); err == nil {
			contact.Archived = false
			e.Events.Publish(Event{Type: "contacto", Phone: in.Phone})
		}
	}
	if contact.AIPaused {
		e.Events.Publish(Event{Type: "alerta", Phone: in.Phone})
		return nil
	}
	if in.WAID != "" {
		e.mu.Lock()
		e.pending[in.Phone] = in.WAID
		e.mu.Unlock()
	}
	e.schedule(in.Phone)
	return nil
}

// schedule (re)starts the debounce timer of a phone.
func (e *Engine) schedule(phone string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return
	}
	if t, ok := e.timers[phone]; ok {
		t.Stop()
	}
	e.timers[phone] = time.AfterFunc(e.debounce(), func() {
		e.mu.Lock()
		delete(e.timers, phone)
		if e.closed {
			e.mu.Unlock()
			return
		}
		e.wg.Add(1)
		e.mu.Unlock()
		defer e.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		e.Reply(ctx, phone)
	})
}

func (e *Engine) lockFor(phone string) *sync.Mutex {
	e.mu.Lock()
	defer e.mu.Unlock()
	l, ok := e.locks[phone]
	if !ok {
		l = &sync.Mutex{}
		e.locks[phone] = l
	}
	return l
}

// Reply runs the agent for a phone and delivers its answer. Exported so the
// panel can force an answer ("Responder ahora") after resuming the AI.
func (e *Engine) Reply(ctx context.Context, phone string) {
	l := e.lockFor(phone)
	l.Lock()
	defer l.Unlock()

	contact, err := e.Store.GetContact(ctx, phone)
	if err != nil || contact.AIPaused {
		return
	}
	e.mu.Lock()
	waID := e.pending[phone]
	delete(e.pending, phone)
	e.mu.Unlock()
	if waID != "" && e.waReady() && !contact.Simulated {
		if err := e.WA.MarkRead(ctx, waID, true); err != nil {
			e.Log.Printf("marcar leído %s: %v", phone, err)
		}
	}
	e.Events.Publish(Event{Type: "escribiendo", Phone: phone})
	// Whatever happens below (answer, error, panic), the panel must stop
	// showing "escribiendo...".
	defer e.Events.Publish(Event{Type: "listo", Phone: phone})

	reply, err := e.Agent.Respond(ctx, phone)
	if err != nil {
		e.Log.Printf("agente %s: %v", phone, err)
		e.Store.Audit(ctx, phone, "sistema", "error_ia", err.Error())
		msg := "La IA no pudo responder: " + llm.Friendly(err) + " Responde tú este chat mientras tanto."
		if errors.Is(err, llm.ErrNoAPIKey) {
			msg = "La IA no está configurada (falta el proveedor o su API key en Conexiones). Responde tú este chat."
		}
		_, _ = e.Store.AddAlert(ctx, store.Alert{Phone: phone, Kind: store.AlertError, Message: msg})
		e.Events.Publish(Event{Type: "alerta", Phone: phone})
		return
	}
	if reply.Usage.TotalTokens > 0 {
		e.Store.Audit(ctx, phone, "ia", "tokens", fmt.Sprintf("entrada=%d salida=%d herramientas=%s",
			reply.Usage.PromptTokens, reply.Usage.CompletionTokens, strings.Join(reply.ToolCalls, ",")))
	}
	for i, b := range reply.Bubbles {
		if i > 0 {
			e.pause(ctx, b)
		}
		e.deliver(ctx, contact, store.SenderAI, b)
	}
	for _, b := range reply.After {
		e.pause(ctx, b)
		e.deliver(ctx, contact, store.SenderSystem, b)
	}
	if reply.Escalated {
		e.Events.Publish(Event{Type: "alerta", Phone: phone})
	}
}

// pause waits a human-like time before a bubble (longer text, longer pause).
func (e *Engine) pause(ctx context.Context, text string) {
	d := e.bubblePause()
	if d <= 0 {
		return
	}
	extra := time.Duration(len([]rune(text))) * 15 * time.Millisecond
	if extra > 2*time.Second {
		extra = 2 * time.Second
	}
	t := time.NewTimer(d + extra)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

// deliver stores an outbound message and sends it over WhatsApp when the
// contact is real.
func (e *Engine) deliver(ctx context.Context, c store.Contact, sender, text string) error {
	m, err := e.Store.AddMessage(ctx, store.Message{Phone: c.Phone, Direction: store.DirOut, Sender: sender, Body: text})
	if err != nil {
		return err
	}
	defer e.Events.Publish(Event{Type: "mensaje", Phone: c.Phone, ID: m.ID})
	if c.Simulated {
		return e.Store.SetMessageWAID(ctx, m.ID, "", "simulado")
	}
	if !e.waReady() {
		_ = e.Store.SetMessageWAID(ctx, m.ID, "", "sin_whatsapp")
		return errors.New("WhatsApp no está configurado")
	}
	waID, err := e.WA.SendText(ctx, c.Phone, text)
	if err != nil {
		_ = e.Store.SetMessageWAID(ctx, m.ID, "", "fallido")
		e.Store.Audit(ctx, c.Phone, "sistema", "error_envio_whatsapp", err.Error())
		_, _ = e.Store.AddAlert(ctx, store.Alert{Phone: c.Phone, Kind: store.AlertError,
			Message: "No se pudo enviar un mensaje por WhatsApp: " + err.Error()})
		return err
	}
	return e.Store.SetMessageWAID(ctx, m.ID, waID, "enviado")
}

// OwnerSend sends a message written by the owner from the panel. Writing in a
// chat implies "Yo tomo": the AI is paused so it does not talk over the owner.
func (e *Engine) OwnerSend(ctx context.Context, phone, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("mensaje vacío")
	}
	c, err := e.Store.GetContact(ctx, phone)
	if err != nil {
		return err
	}
	if !c.AIPaused {
		if err := e.SetPaused(ctx, phone, true); err != nil {
			return err
		}
	}
	e.Store.Audit(ctx, phone, "dueno", "mensaje_manual", text)
	return e.deliver(ctx, c, store.SenderOwner, text)
}

// SetPaused pauses ("Yo tomo") or resumes the AI for a chat.
func (e *Engine) SetPaused(ctx context.Context, phone string, paused bool) error {
	if err := e.Store.SetAIPaused(ctx, phone, paused); err != nil {
		return err
	}
	if paused {
		e.mu.Lock()
		if t, ok := e.timers[phone]; ok {
			t.Stop()
			delete(e.timers, phone)
		}
		e.mu.Unlock()
		e.Store.Audit(ctx, phone, "dueno", "yo_tomo", "IA pausada")
	} else {
		_ = e.Store.ResolvePhoneAlerts(ctx, phone, store.AlertEscalation)
		e.Store.Audit(ctx, phone, "dueno", "ia_reanudada", "")
	}
	e.Events.Publish(Event{Type: "contacto", Phone: phone})
	return nil
}

// NotifyCustomer implements sales.Notifier: system messages (approval,
// tracking number, payment received) go into the chat and over WhatsApp.
func (e *Engine) NotifyCustomer(ctx context.Context, phone, text string) error {
	c, err := e.Store.GetContact(ctx, phone)
	if err != nil {
		return err
	}
	return e.deliver(ctx, c, store.SenderSystem, text)
}

// NotifyOwner implements sales.Notifier: alerts to the owner's own WhatsApp
// (if configured) and to the open panels.
func (e *Engine) NotifyOwner(ctx context.Context, text string) {
	e.Events.Publish(Event{Type: "alerta", Text: text})
	owner := strings.TrimSpace(e.neg().Notificar.WhatsAppDueno)
	if owner == "" || !e.waReady() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := e.WA.SendText(ctx, owner, "Dropshipper: "+text); err != nil {
			// Usually the owner has not written to the business number in the
			// last 24 h (WhatsApp only allows free text inside that window).
			e.Log.Printf("aviso al dueño: %v", err)
		}
	}()
}

// Close stops pending timers and waits for running answers.
func (e *Engine) Close() {
	e.mu.Lock()
	e.closed = true
	for _, t := range e.timers {
		t.Stop()
	}
	e.mu.Unlock()
	e.wg.Wait()
}
