// Command motor runs the Dropshipper Motor: the always-on server that answers
// WhatsApp with the AI seller, keeps orders and serves the owner's panel. The
// same binary is also the terminal tool to configure it:
//
//	motor                     # starts the server (same as "motor servir")
//	motor estado              # what is connected and what is missing
//	motor ia agregar ...      # add an AI provider (see "motor ayuda")
//
// Everything configurable in the panel is configurable here, and both edit
// the same data: changes from the terminal reach a running Motor in seconds.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/michiTrader/dropshipper/motor/internal/agent"
	"github.com/michiTrader/dropshipper/motor/internal/api"
	"github.com/michiTrader/dropshipper/motor/internal/catalog"
	"github.com/michiTrader/dropshipper/motor/internal/config"
	"github.com/michiTrader/dropshipper/motor/internal/convo"
	"github.com/michiTrader/dropshipper/motor/internal/hub"
	"github.com/michiTrader/dropshipper/motor/internal/rules"
	"github.com/michiTrader/dropshipper/motor/internal/sales"
	"github.com/michiTrader/dropshipper/motor/internal/settings"
	"github.com/michiTrader/dropshipper/motor/internal/shipping"
	"github.com/michiTrader/dropshipper/motor/internal/store"
	"github.com/michiTrader/dropshipper/motor/internal/tunnel"
	"github.com/michiTrader/dropshipper/motor/internal/whatsweb"
)

func main() {
	dir := flag.String("dir", ".", "carpeta del Motor (donde están .env, negocio.yaml y knowledge/)")
	flag.Usage = func() { fmt.Fprint(os.Stderr, helpText) }
	flag.Parse()
	args := flag.Args()

	if len(args) == 0 || args[0] == "servir" {
		lg := log.New(os.Stdout, "motor ", log.LstdFlags)
		if err := serve(*dir, lg); err != nil {
			lg.Fatalf("error: %v", err)
		}
		return
	}
	if err := runCLI(*dir, args, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// app is what the server and the terminal share: the database with the
// settings manager and the catalog on top of it.
type app struct {
	cfg *config.Config
	st  *store.Store
	set *settings.Manager
	cat *catalog.Catalog
}

func (a *app) close() { _ = a.st.Close() }

// openApp loads .env/negocio.yaml (first-run seed only), opens the database
// and the settings/catalog stored in it.
func openApp(dir string) (*app, error) {
	cfg, err := config.Load(dir)
	if err != nil {
		return nil, err
	}
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	set, err := settings.Open(ctx, st, func() settings.Settings { return settings.Seed(cfg) })
	if err != nil {
		st.Close()
		return nil, err
	}
	cat, err := catalog.Open(ctx, st, cfg.KnowledgeDir)
	if err != nil {
		st.Close()
		return nil, err
	}
	return &app{cfg: cfg, st: st, set: set, cat: cat}, nil
}

func serve(dir string, lg *log.Logger) error {
	a, err := openApp(dir)
	if err != nil {
		return err
	}
	defer a.close()
	cfg := a.cfg

	zones, err := rules.LoadZones(filepath.Join(cfg.KnowledgeDir, "zonas.yaml"))
	if err != nil {
		return err
	}

	h := hub.New()
	mediaDir := filepath.Join(cfg.DataDir, "media")
	// WhatsApp Web (linked phone). It stays idle until that channel is selected.
	h.Web = whatsweb.New(cfg.DataDir, mediaDir, lg)
	defer h.Web.Close()
	// Public address for the webhooks (ngrok). Idle until it is switched on in the panel.
	h.Tunnel = tunnel.New(lg, nil)
	defer h.Tunnel.Close()
	live := config.NewLive(a.set.Get().Negocio)
	a.set.OnChange(func(s settings.Settings) {
		h.Apply(s)
		live.Set(s.Negocio)
	})

	ship := &shipping.Service{
		Negocio: live,
		Client: func() shipping.Quoter {
			if c := h.Dropi(); c != nil {
				return c
			}
			return nil
		},
		Origin: func() (string, string) { d := h.Settings().Dropi; return d.OriginDept, d.OriginCity },
	}
	a.set.OnChange(func(settings.Settings) { ship.Clear() })
	sv := &sales.Service{Store: a.st, Catalog: a.cat, Zones: zones, Negocio: live, Payments: h, Shipping: ship}
	seller := &agent.Seller{LLM: h.AI, Sales: sv, Store: a.st, Catalog: a.cat, Zones: zones, Negocio: live}
	engine := convo.NewEngine(a.st, seller, sv, h, live, lg)
	sv.Notifier = engine
	defer engine.Close()
	h.Web.SetHandlers(
		func(ctx context.Context, m whatsweb.Message) {
			in := convo.Inbound{Phone: m.Phone, Name: m.Name, Kind: m.Kind, Text: m.Text, MediaPath: m.MediaPath, WAID: m.WAID, At: m.At}
			if err := engine.HandleInbound(ctx, in); err != nil {
				lg.Printf("mensaje de %s: %v", m.Phone, err)
			}
		},
		func(ids []string, status string) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			for _, id := range ids {
				_ = a.st.UpdateMessageStatus(ctx, id, status)
			}
		},
		func() { engine.Events.Publish(convo.Event{Type: "whatsapp"}) },
	)

	srv := &api.Server{
		Settings: a.set, Hub: h, Store: a.st, Engine: engine, Sales: sv, Catalog: a.cat, Zones: zones, Shipping: ship,
		MediaDir: mediaDir, Log: lg, Started: time.Now().UTC(),
	}
	// Through the tunnel only the webhooks are reachable, never the panel.
	h.Tunnel.SetHandler(srv.Handler())
	h.Tunnel.OnChange(func() { engine.Events.Publish(convo.Event{Type: "whatsapp"}) })
	httpSrv := &http.Server{
		Addr:              fmt.Sprintf("0.0.0.0:%d", cfg.Port),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Changes made from the terminal (another process) reach this one here.
	go a.set.Watch(ctx, 3*time.Second, func() {
		if err := a.cat.Reload(ctx); err != nil {
			lg.Printf("recargar catálogo: %v", err)
		}
		lg.Printf("configuración actualizada desde la terminal u otro proceso")
	})
	go func() {
		t := time.NewTicker(15 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				sv.FlagStalePayments(ctx)
			}
		}
	}()

	printBanner(lg, a, h, cfg.Port)
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		lg.Printf("apagando...")
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(sctx)
	}
	return nil
}

func printBanner(lg *log.Logger, a *app, h *hub.Hub, port int) {
	mark := func(b bool) string {
		if b {
			return "[ok]   "
		}
		return "[falta]"
	}
	s := a.set.Get()
	n := s.Negocio
	lg.Printf("Dropshipper Motor %s - %s - vendedora: %s", api.Version, n.NombreTienda, n.Persona.Nombre)
	if p, ok := s.Active(); ok {
		lg.Printf("%s IA: %s (%s, %s)", mark(h.AI.Ready()), p.Name, p.Model, p.BaseURL)
	} else {
		lg.Printf("%s IA: sin proveedor (panel > Conexiones, o: motor ia agregar)", mark(false))
	}
	if s.WhatsApp.UsesWeb() {
		lg.Printf("%s WhatsApp Web (teléfono vinculado con QR; estado en el panel > Conexiones)", mark(h.Available()))
	} else {
		lg.Printf("%s WhatsApp Cloud API", mark(h.WhatsApp() != nil))
	}
	pagosOK := (n.Pagos.Metodo == "wompi" && h.Wompi() != nil) ||
		(n.Pagos.Metodo == "manual" && (n.Pagos.Manual.LlaveBreB != "" || n.Pagos.Manual.NumeroCuenta != ""))
	lg.Printf("%s Pagos anticipados: %s", mark(pagosOK), n.Pagos.Metodo)
	act := a.cat.Active()
	conf := 0
	for _, p := range act {
		if p.CostConfirmed {
			conf++
		}
	}
	lg.Printf("%s Productos activos: %d (con costo confirmado: %d)", mark(len(act) > 0 && conf == len(act)), len(act), conf)
	if s.General.AdminToken == "" {
		lg.Printf("[aviso] Sin token: el panel solo abre desde este computador. Para exponerlo: motor token generar")
	}
	lg.Printf("Panel: http://localhost:%d", port)
	switch {
	case s.Ngrok.Enabled:
		lg.Printf("Túnel ngrok activado: conectando (la dirección pública aparece en el panel > Conexiones y aquí al conectar)")
	case s.General.PublicURL != "":
		lg.Printf("Webhook WhatsApp: %s/webhook/whatsapp", s.General.PublicURL)
	}
}
