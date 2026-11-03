package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/michiTrader/dropshipper/motor/internal/convo"
	"github.com/michiTrader/dropshipper/motor/internal/payments"
	"github.com/michiTrader/dropshipper/motor/internal/settings"
	"github.com/michiTrader/dropshipper/motor/internal/store"
	"github.com/michiTrader/dropshipper/motor/internal/whatsapp"
)

// waVerify answers Meta's webhook verification handshake.
func (s *Server) waVerify(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	want := s.Settings.Get().WhatsApp.VerifyToken
	if q.Get("hub.mode") == "subscribe" && want != "" &&
		subtle.ConstantTimeCompare([]byte(q.Get("hub.verify_token")), []byte(want)) == 1 {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, q.Get("hub.challenge"))
		return
	}
	http.Error(w, "verificación fallida", http.StatusForbidden)
}

// waWebhook receives messages and delivery statuses from WhatsApp. It always
// answers 200 quickly (Meta retries otherwise) and processes in background.
func (s *Server) waWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		http.Error(w, "cuerpo inválido", 400)
		return
	}
	if os.Getenv("MOTOR_WEBHOOK_DEBUG") != "" {
		s.Log.Printf("webhook crudo: %s", body)
	}
	if !whatsapp.VerifySignature(s.Settings.Get().WhatsApp.AppSecret, body, r.Header.Get("X-Hub-Signature-256")) {
		s.Log.Printf("webhook WhatsApp con firma inválida (revisa el App Secret en Conexiones > WhatsApp)")
		http.Error(w, "firma inválida", http.StatusUnauthorized)
		return
	}
	msgs, statuses, err := whatsapp.ParseWebhook(body)
	if err != nil {
		s.Log.Printf("webhook WhatsApp ilegible: %v", err)
		w.WriteHeader(200)
		return
	}
	w.WriteHeader(200)

	// Remember the WhatsApp Business account the first time Meta tells us: it
	// is the identifier needed to subscribe the app, and asking for it by hand
	// is the step that most often leaves a shop silently dead.
	if ids := whatsapp.AccountIDs(body); len(ids) > 0 && s.Settings.Get().WhatsApp.WABAID == "" {
		go func(id string) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.Settings.Update(ctx, func(st *settings.Settings) error {
				if st.WhatsApp.WABAID == "" {
					st.WhatsApp.WABAID = id
				}
				return nil
			}); err == nil {
				s.Log.Printf("cuenta de WhatsApp Business detectada: %s", id)
			}
		}(ids[0])
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		for _, st := range statuses {
			_ = s.Store.UpdateMessageStatus(ctx, st.ID, st.Status)
			if st.Status == "failed" {
				s.Log.Printf("WhatsApp no entregó %s: %s", st.ID, st.Error)
			}
		}
		for _, m := range msgs {
			in := convo.Inbound{Phone: m.From, Name: m.Name, Kind: m.Type, Text: m.Text, WAID: m.ID, At: m.Timestamp}
			if wa := s.Hub.WhatsApp(); m.MediaID != "" && wa != nil && (m.Type == "image" || m.Type == "document" || m.Type == "audio") {
				if name, err := wa.DownloadMedia(ctx, m.MediaID, s.MediaDir); err != nil {
					s.Log.Printf("descargar media de %s: %v", m.From, err)
				} else {
					in.MediaPath = name
				}
			}
			if err := s.Engine.HandleInbound(ctx, in); err != nil {
				s.Log.Printf("mensaje de %s: %v", m.From, err)
			}
		}
	}()
}

// wompiWebhook receives Wompi transaction events. Only checksum-verified
// APPROVED events with the exact advance amount confirm a payment.
func (s *Server) wompiWebhook(w http.ResponseWriter, r *http.Request) {
	wompi := s.Hub.Wompi()
	if wompi == nil {
		w.WriteHeader(200)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "cuerpo inválido", 400)
		return
	}
	ev, err := wompi.ParseEvent(body, r.Header.Get("X-Event-Checksum"))
	if err != nil {
		s.Log.Printf("evento Wompi rechazado: %v", err)
		if errors.Is(err, payments.ErrBadChecksum) {
			http.Error(w, "checksum inválido", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(200)
		return
	}
	w.WriteHeader(200)
	if ev.Type != "transaction.updated" {
		return
	}
	ctx := r.Context()
	ref := ev.PaymentLinkID
	if ref == "" {
		ref = ev.Reference
	}
	o, err := s.Store.FindOrderByPaymentRef(ctx, ref)
	if err != nil {
		s.Log.Printf("pago Wompi %s sin pedido (ref %s)", ev.TransactionID, ref)
		return
	}
	switch strings.ToUpper(ev.Status) {
	case "APPROVED":
		if ev.AmountInCents < o.AdvanceAmount*100 {
			_, _ = s.Store.AddAlert(ctx, store.Alert{Phone: o.Phone, OrderID: o.ID, Kind: store.AlertError,
				Message: "Wompi aprobó un pago MENOR al anticipo del pedido; revísalo antes de despachar."})
			return
		}
		if _, err := s.Sales.ConfirmPayment(ctx, o.ID, "wompi"); err != nil {
			s.Log.Printf("confirmar pago %d: %v", o.ID, err)
		}
	case "DECLINED", "ERROR", "VOIDED":
		s.Store.Audit(ctx, o.Phone, "wompi", "pago_"+strings.ToLower(ev.Status), ev.TransactionID)
		_, _ = s.Store.AddAlert(ctx, store.Alert{Phone: o.Phone, OrderID: o.ID, Kind: store.AlertInfo,
			Message: "El pago del pedido fue " + strings.ToLower(ev.Status) + " en Wompi. El cliente puede reintentar con el mismo link."})
	}
	s.Engine.Events.Publish(convo.Event{Type: "pedido", Phone: o.Phone, ID: o.ID})
}
