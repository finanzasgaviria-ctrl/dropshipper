package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/michiTrader/dropshipper/motor/internal/catalog"
	"github.com/michiTrader/dropshipper/motor/internal/config"
	"github.com/michiTrader/dropshipper/motor/internal/llm"
	"github.com/michiTrader/dropshipper/motor/internal/payments"
	"github.com/michiTrader/dropshipper/motor/internal/settings"
	"github.com/michiTrader/dropshipper/motor/internal/whatsapp"
)

const helpText = `Dropshipper Motor: todo se configura desde el panel web o desde esta terminal.
Los dos editan los mismos datos; un Motor encendido ve los cambios en pocos segundos.

Uso:  motor [-dir CARPETA] [comando]

  (sin comando)            enciende el Motor (igual que "motor servir")
  estado                   que esta conectado y que falta

  ia                       proveedores de IA (cualquier API compatible con OpenAI)
    ia listar | presets
    ia agregar  [--preset deepseek|openai|openrouter|groq|gemini|ollama|custom]
                [--nombre N] [--url URL] [--modelo M] [--clave K|-] [--temperatura T] [--activar]
    ia editar ID [--nombre N] [--url URL] [--modelo M] [--clave K|-] [--temperatura T]
    ia activar ID | quitar ID
    ia modelos [ID]          lista los modelos que ofrece el proveedor
    ia probar  [ID]          hace una llamada real de prueba

  whatsapp                 WhatsApp Business Cloud API
    whatsapp ver | probar | quitar
    whatsapp configurar [--token T|-] [--telefono-id ID] [--verify-token V]
                        [--app-secret S|-] [--version v21.0]
    whatsapp conectar [--cuenta WABA] [--app-id ID] [--url https://...]
                      (registra el webhook y suscribe la app a la cuenta por ti)

  wompi                    links de pago Wompi (opcional)
    wompi ver | probar | quitar
    wompi configurar [--llave-publica P] [--llave-privada K|-] [--eventos S|-]
                     [--produccion | --pruebas]

  dropi                    conexion con Dropi (fletes, costos, simulacion)
    dropi ver | probar | quitar
    dropi configurar [--token T|-] [--ciudad-origen C] [--departamento-origen D]
                     [--produccion | --pruebas]
    dropi cotizar  --ciudad C [--departamento D] [--producto SKU] [--oferta ID]
                   compara transportadoras: flete, ganancia, perdida si devuelve
    dropi simular  --ciudad C [--departamento D] [--producto SKU] [--oferta ID]
                   ensayo de pedido: verifica fletes y costos SIN crear nada en Dropi
    dropi costos SKU         trae el costo real del producto desde Dropi y lo confirma

  transportadoras          transportadoras que usa la IA (Veloces, Envia, Coordinadora...)
    transportadoras ver
    transportadoras activar ID | desactivar ID
    transportadoras notas ID TEXTO    lo que la IA debe saber (cobertura, tiempos, contraentrega)
    transportadoras tasa ID 18%       devolucion propia (0 = tasa general)
    transportadoras prioridad ID N    menor = se prefiere en empates

  token                    acceso al panel desde otros equipos
    token ver | generar | quitar
    token poner TOKEN      (minimo 8 caracteres)
  url [URL_PUBLICA]        ver o fijar la direccion publica (para los webhooks)

  negocio                  datos y reglas del negocio
    negocio ver
    negocio poner CLAVE VALOR     (ej.: persona.nombre Sofia, finanzas.tasa_devolucion 25%)

  productos                catalogo (varios productos)
    productos listar | ver SKU | activar SKU | desactivar SKU | borrar SKU
    productos agregar --sku S --nombre N --costo C --flete F --oferta ID:UNIDADES:PRECIO [--oferta ...]
    productos editar SKU [--nombre N] [--costo C] [--flete F] [--costo-confirmado=true|false]
    productos importar ARCHIVO.yaml | exportar SKU
  guia ver | poner ARCHIVO.md   guia de ventas de la IA

Donde dice "K|-", un guion (-) lee el valor desde la entrada estandar para que
no quede en el historial de la terminal:  echo $CLAVE | motor ia agregar --preset deepseek --clave -
`

var errUsage = errors.New("comando incompleto; usa 'motor ayuda'")

// runCLI executes one terminal command against the shared database.
func runCLI(dir string, args []string, out io.Writer) error {
	switch args[0] {
	case "ayuda", "help", "-h", "--help":
		fmt.Fprint(out, helpText)
		return nil
	}
	a, err := openApp(dir)
	if err != nil {
		return err
	}
	defer a.close()
	c := &cli{app: a, out: out, ctx: context.Background()}
	rest := args[1:]
	switch args[0] {
	case "estado":
		return c.estado()
	case "ia":
		return c.ia(rest)
	case "whatsapp":
		return c.whatsapp(rest)
	case "wompi":
		return c.wompi(rest)
	case "dropi":
		return c.dropi(rest)
	case "transportadoras":
		return c.transportadoras(rest)
	case "token":
		return c.token(rest)
	case "url":
		return c.url(rest)
	case "negocio":
		return c.negocio(rest)
	case "productos":
		return c.productos(rest)
	case "guia":
		return c.guia(rest)
	}
	return fmt.Errorf("no conozco el comando %q; usa 'motor ayuda'", args[0])
}

type cli struct {
	*app
	out io.Writer
	ctx context.Context
}

func (c *cli) p(format string, a ...any) { fmt.Fprintf(c.out, format+"\n", a...) }

// applied tells the user the change is saved and will reach a running Motor.
func (c *cli) applied(msg string) {
	c.p("%s", msg)
	c.p("Guardado. Si el Motor esta encendido, lo aplica en unos segundos.")
}

// ---------- argument helpers ----------

// parse accepts flags before or after positional arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, fmt.Errorf("opcion no valida: %v", err)
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// secret returns v, or a line read from stdin when v is "-".
func secret(v string) (string, error) {
	if v != "-" {
		return strings.TrimSpace(v), nil
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", errors.New("no pude leer el valor desde la entrada estandar")
	}
	return strings.TrimSpace(line), nil
}

func visited(fs *flag.FlagSet) map[string]bool {
	m := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { m[f.Name] = true })
	return m
}

func hostOf(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return strings.ToLower(p.Host)
}

func yesNo(b bool) string {
	if b {
		return "si"
	}
	return "no"
}

func maskOr(s string) string {
	if s == "" {
		return "(sin configurar)"
	}
	return settings.Mask(s)
}

func orDash(s string) string {
	if s == "" {
		return "(se detecta solo)"
	}
	return s
}

func (c *cli) tw() *tabwriter.Writer { return tabwriter.NewWriter(c.out, 0, 4, 2, ' ', 0) }

// ---------- estado ----------

func (c *cli) estado() error {
	s := c.set.Get()
	n := s.Negocio
	mark := func(b bool) string {
		if b {
			return "[ok]   "
		}
		return "[falta]"
	}
	c.p("%s - vendedora: %s", n.NombreTienda, n.Persona.Nombre)
	if p, ok := s.Active(); ok {
		c.p("%s IA: %s (%s, %s)", mark(p.Ready()), p.Name, p.Model, p.BaseURL)
	} else {
		c.p("%s IA: sin proveedor. Agrega uno: motor ia agregar --preset deepseek --clave -", mark(false))
	}
	if s.WhatsApp.UsesWeb() {
		c.p("%s WhatsApp Web: vincula el telefono con el codigo QR desde el panel (Conexiones)", mark(false))
	} else {
		c.p("%s WhatsApp: %s", mark(s.WhatsApp.Enabled()), map[bool]string{true: "conectado", false: "motor whatsapp configurar ..."}[s.WhatsApp.Enabled()])
	}
	pagosOK := (n.Pagos.Metodo == "wompi" && s.Wompi.Enabled()) ||
		(n.Pagos.Metodo == "manual" && (n.Pagos.Manual.LlaveBreB != "" || n.Pagos.Manual.NumeroCuenta != ""))
	switch {
	case s.Dropi.Enabled() && s.Dropi.OriginCity != "":
		c.p("%s Dropi: conectado (%s), origen %s", mark(true), map[bool]string{true: "pruebas", false: "produccion"}[s.Dropi.Sandbox], s.Dropi.OriginCity)
	case s.Dropi.Enabled():
		c.p("%s Dropi: conectado pero falta la ciudad de origen (motor dropi configurar --ciudad-origen ...)", mark(false))
	default:
		c.p("%s Dropi: sin conectar; los fletes son estimados (motor dropi configurar --token -)", mark(false))
	}
	c.p("%s Pagos anticipados: %s", mark(pagosOK), n.Pagos.Metodo)
	act := c.cat.Active()
	conf := 0
	for _, p := range act {
		if p.CostConfirmed {
			conf++
		}
	}
	c.p("%s Productos activos: %d de %d (costo confirmado: %d)", mark(len(act) > 0 && conf == len(act)), len(act), len(c.cat.List()), conf)
	if s.General.AdminToken == "" {
		c.p("[aviso] Sin token: el panel solo abre desde este computador (motor token generar).")
	} else {
		c.p("[ok]    Token del panel: %s", settings.Mask(s.General.AdminToken))
	}
	if s.General.PublicURL != "" {
		c.p("Webhook WhatsApp: %s/webhook/whatsapp", s.General.PublicURL)
	}
	return nil
}

// ---------- ia ----------

func (c *cli) provider(id string) (settings.Provider, error) {
	ia := c.set.Get().IA
	if id == "" {
		id = ia.ActiveID
	}
	if id == "" {
		return settings.Provider{}, errors.New("no hay proveedor activo; indica un ID (motor ia listar)")
	}
	p, ok := ia.Provider(id)
	if !ok {
		return p, fmt.Errorf("no existe el proveedor %q (motor ia listar)", id)
	}
	return p, nil
}

func clientFor(p settings.Provider) *llm.Client {
	cl := llm.NewClient(p.BaseURL, p.APIKey)
	cl.AllowNoKey = p.NoKey
	return cl
}

func (c *cli) ia(args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "presets":
		w := c.tw()
		fmt.Fprintln(w, "PRESET\tNOMBRE\tURL\tMODELO")
		for _, p := range settings.Presets {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.ID, p.Name, p.BaseURL, p.Model)
		}
		return w.Flush()
	case "listar":
		ia := c.set.Get().IA
		if len(ia.Providers) == 0 {
			c.p("No hay proveedores. Agrega uno: motor ia agregar --preset deepseek --clave -")
			return nil
		}
		w := c.tw()
		fmt.Fprintln(w, "ACTIVO\tID\tNOMBRE\tMODELO\tURL\tCLAVE")
		for _, p := range ia.Providers {
			act := ""
			if p.ID == ia.ActiveID {
				act = "*"
			}
			key := maskOr(p.APIKey)
			if p.NoKey {
				key = "(no necesita)"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", act, p.ID, p.Name, p.Model, p.BaseURL, key)
		}
		return w.Flush()
	case "agregar", "editar":
		return c.iaSave(sub == "agregar", rest)
	case "activar", "quitar":
		if len(rest) != 1 {
			return fmt.Errorf("uso: motor ia %s ID", sub)
		}
		id := rest[0]
		err := c.set.Update(c.ctx, func(s *settings.Settings) error {
			if _, ok := s.IA.Provider(id); !ok {
				return fmt.Errorf("no existe el proveedor %q (motor ia listar)", id)
			}
			if sub == "activar" {
				s.IA.ActiveID = id
				return nil
			}
			var keep []settings.Provider
			for _, p := range s.IA.Providers {
				if p.ID != id {
					keep = append(keep, p)
				}
			}
			s.IA.Providers = keep
			if s.IA.ActiveID == id {
				s.IA.ActiveID = ""
				if len(keep) > 0 {
					s.IA.ActiveID = keep[0].ID
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		c.applied("Proveedor " + id + ": " + sub + " hecho.")
		return nil
	case "modelos":
		id := ""
		if len(rest) > 0 {
			id = rest[0]
		}
		p, err := c.provider(id)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(c.ctx, 25*time.Second)
		defer cancel()
		cl := clientFor(p)
		models, err := cl.ListModels(ctx)
		if err != nil {
			return errors.New(llm.Friendly(err))
		}
		for _, m := range models {
			c.p("%s", m)
		}
		c.urlHint(p, cl.EffectiveBaseURL())
		return nil
	case "probar":
		id := ""
		if len(rest) > 0 {
			id = rest[0]
		}
		p, err := c.provider(id)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(c.ctx, 25*time.Second)
		defer cancel()
		res, err := clientFor(p).Check(ctx, p.Model)
		if err != nil {
			return errors.New(llm.Friendly(err))
		}
		c.p("Conexion correcta con %s (%s): respondio %q en %d ms.", p.Name, p.Model, res.Reply, res.LatencyMs)
		c.urlHint(p, res.BaseURL)
		return nil
	}
	return fmt.Errorf("no conozco 'ia %s'; usa 'motor ayuda'", sub)
}

// urlHint tells the user when the provider only answers under another
// address (usually the same one plus /v1) and how to store it. The motor
// already works either way; this just makes the saved address exact.
func (c *cli) urlHint(p settings.Provider, worked string) {
	typed := llm.NormalizeBaseURL(p.BaseURL)
	if worked == "" || worked == typed {
		return
	}
	c.p("Aviso: ese proveedor responde en %s (se anadio /v1). Para dejarlo fijo: motor ia editar %s --url %s", worked, p.ID, worked)
}

func (c *cli) iaSave(create bool, args []string) error {
	fs := flag.NewFlagSet("ia", flag.ContinueOnError)
	preset := fs.String("preset", "", "")
	name := fs.String("nombre", "", "")
	baseURL := fs.String("url", "", "")
	model := fs.String("modelo", "", "")
	key := fs.String("clave", "", "")
	temp := fs.Float64("temperatura", -1, "")
	activate := fs.Bool("activar", false, "")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	seen := visited(fs)
	var apiKey string
	if seen["clave"] {
		if apiKey, err = secret(*key); err != nil {
			return err
		}
	}
	var savedID string
	err = c.set.Update(c.ctx, func(s *settings.Settings) error {
		var p settings.Provider
		if create {
			if len(pos) > 0 {
				return errors.New("sobra un argumento: " + pos[0])
			}
			if *preset == "" {
				*preset = "custom"
				if *baseURL == "" {
					return errors.New("indica --preset (deepseek, openai...) o --url con la direccion de tu proveedor")
				}
			}
			if _, ok := settings.PresetByID(*preset); !ok {
				return fmt.Errorf("el preset %q no existe (motor ia presets)", *preset)
			}
			p = settings.Provider{Preset: *preset}
		} else {
			if len(pos) != 1 {
				return errors.New("uso: motor ia editar ID [opciones]")
			}
			old, ok := s.IA.Provider(pos[0])
			if !ok {
				return fmt.Errorf("no existe el proveedor %q (motor ia listar)", pos[0])
			}
			p = old
		}
		oldHost := hostOf(p.BaseURL)
		if seen["nombre"] {
			p.Name = *name
		}
		if seen["url"] {
			p.BaseURL = *baseURL
		}
		if seen["modelo"] {
			p.Model = *model
		}
		if seen["clave"] {
			p.APIKey = apiKey
		} else if !create && seen["url"] && hostOf(llm.NormalizeBaseURL(p.BaseURL)) != oldHost {
			p.APIKey = "" // a saved key never travels to a different server
		}
		if seen["temperatura"] {
			t := *temp
			p.Temperature = &t
		}
		id := p.ID
		if create {
			p.ID = ""
		}
		if err := p.Validate(); err != nil {
			return err
		}
		if create {
			p.ID = s.IA.NewID(p.Name)
			s.IA.Providers = append(s.IA.Providers, p)
		} else {
			for i := range s.IA.Providers {
				if s.IA.Providers[i].ID == id {
					s.IA.Providers[i] = p
				}
			}
		}
		if *activate || s.IA.ActiveID == "" {
			s.IA.ActiveID = p.ID
		}
		savedID = p.ID
		return nil
	})
	if err != nil {
		return err
	}
	c.applied(fmt.Sprintf("Proveedor %q listo. Prueba la conexion con: motor ia probar %s", savedID, savedID))
	return nil
}

// ---------- whatsapp ----------

func (c *cli) whatsapp(args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	sub, rest := args[0], args[1:]
	w := c.set.Get().WhatsApp
	switch sub {
	case "ver":
		c.p("Canal:            %s", map[string]string{"cloud": "API oficial (Cloud API)", "web": "WhatsApp Web (QR, desde el panel)"}[w.Channel()])
		c.p("Conectado:        %s", yesNo(w.Enabled()))
		c.p("Token:            %s", maskOr(w.Token))
		c.p("ID del telefono:  %s", w.PhoneNumberID)
		c.p("Verify token:     %s", maskOr(w.VerifyToken))
		c.p("App secret:       %s", maskOr(w.AppSecret))
		c.p("App ID:           %s", orDash(w.AppID))
		c.p("Cuenta (WABA):    %s", orDash(w.WABAID))
		c.p("Version Graph:    %s", w.GraphVersion)
		base := c.set.Get().General.PublicURL
		if base == "" {
			base = "https://TU-DOMINIO"
		}
		c.p("URL del webhook:  %s/webhook/whatsapp", base)
		return nil
	case "quitar":
		if err := c.set.Update(c.ctx, func(s *settings.Settings) error {
			s.WhatsApp = config.WhatsAppConfig{GraphVersion: s.WhatsApp.GraphVersion}
			return nil
		}); err != nil {
			return err
		}
		c.applied("WhatsApp desconectado.")
		return nil
	case "probar":
		if !w.Enabled() {
			return errors.New("WhatsApp no esta configurado (motor whatsapp configurar --token ... --telefono-id ...)")
		}
		ctx, cancel := context.WithTimeout(c.ctx, 25*time.Second)
		defer cancel()
		info, err := whatsapp.New(w.Token, w.PhoneNumberID, w.GraphVersion).Check(ctx)
		if err != nil {
			return err
		}
		c.p("Conexion correcta: numero %s, nombre verificado %q, calidad %s.", info.DisplayPhone, info.VerifiedName, info.Quality)
		return nil
	case "configurar":
		fs := flag.NewFlagSet("whatsapp", flag.ContinueOnError)
		token := fs.String("token", "", "")
		phone := fs.String("telefono-id", "", "")
		verify := fs.String("verify-token", "", "")
		appSecret := fs.String("app-secret", "", "")
		ver := fs.String("version", "", "")
		if _, err := parse(fs, rest); err != nil {
			return err
		}
		seen := visited(fs)
		err := c.set.Update(c.ctx, func(s *settings.Settings) error {
			var err error
			wa := &s.WhatsApp
			if seen["token"] {
				if wa.Token, err = secret(*token); err != nil {
					return err
				}
			}
			if seen["app-secret"] {
				if wa.AppSecret, err = secret(*appSecret); err != nil {
					return err
				}
			}
			if seen["telefono-id"] {
				wa.PhoneNumberID = strings.TrimSpace(*phone)
			}
			if seen["verify-token"] {
				wa.VerifyToken = strings.TrimSpace(*verify)
			}
			if seen["version"] {
				wa.GraphVersion = strings.TrimSpace(*ver)
			}
			if wa.VerifyToken == "" {
				wa.VerifyToken = settings.NewToken()
				c.p("Verify token generado: %s (pegalo en Meta al configurar el webhook)", wa.VerifyToken)
			}
			return nil
		})
		if err != nil {
			return err
		}
		c.applied("WhatsApp configurado. Verifica con: motor whatsapp probar")
		return nil
	case "conectar":
		fs := flag.NewFlagSet("whatsapp", flag.ContinueOnError)
		waba := fs.String("cuenta", "", "")
		appID := fs.String("app-id", "", "")
		publico := fs.String("url", "", "")
		if _, err := parse(fs, rest); err != nil {
			return err
		}
		return c.connectWhatsApp(*waba, *appID, *publico)
	}
	return fmt.Errorf("no conozco 'whatsapp %s'; usa 'motor ayuda'", sub)
}

// connectWhatsApp does the two Meta-side steps (webhook registration and
// app-to-account subscription) so a new shop needs only credentials.
func (c *cli) connectWhatsApp(waba, appID, publico string) error {
	st := c.set.Get()
	cfg := st.WhatsApp
	if cfg.Token == "" || cfg.PhoneNumberID == "" {
		return errors.New("faltan el token y el ID del numero: motor whatsapp configurar -token ... -telefono-id ...")
	}
	if cfg.AppSecret == "" {
		return errors.New("falta la clave secreta de la app: motor whatsapp configurar -app-secret ...")
	}
	if strings.TrimSpace(waba) != "" {
		cfg.WABAID = strings.TrimSpace(waba)
	}
	if strings.TrimSpace(appID) != "" {
		cfg.AppID = strings.TrimSpace(appID)
	}
	base := strings.TrimRight(strings.TrimSpace(publico), "/")
	if base == "" {
		base = strings.TrimRight(st.General.PublicURL, "/")
	}
	if base == "" {
		return errors.New("no hay direccion publica: usa -url https://... o activa el tunel de ngrok en el panel")
	}
	if cfg.VerifyToken == "" {
		cfg.VerifyToken = settings.NewToken()[:24]
	}
	if cfg.GraphVersion == "" {
		cfg.GraphVersion = "v21.0"
	}
	ctx, cancel := context.WithTimeout(c.ctx, 90*time.Second)
	defer cancel()
	cl := whatsapp.New(cfg.Token, cfg.PhoneNumberID, cfg.GraphVersion)

	info, err := cl.Check(ctx)
	if err != nil {
		return fmt.Errorf("el numero no responde: %w", err)
	}
	c.p("[ok]    Numero: %s %s", info.DisplayPhone, info.VerifiedName)

	if cfg.AppID == "" {
		id, err := cl.AppID(ctx)
		if err != nil {
			return fmt.Errorf("no se pudo detectar la app del token: %w", err)
		}
		cfg.AppID = id
	}
	c.p("[ok]    App de Meta: %s", cfg.AppID)

	hook := base + "/webhook/whatsapp"
	if err := cl.SetWebhook(ctx, cfg.AppID, cfg.AppSecret, hook, cfg.VerifyToken, []string{"messages"}); err != nil {
		return fmt.Errorf("Meta no acepto el webhook %s: %w", hook, err)
	}
	spec, err := cl.Webhook(ctx, cfg.AppID, cfg.AppSecret)
	switch {
	case err != nil:
		return fmt.Errorf("Meta no confirmo el webhook: %w", err)
	case !spec.Active || spec.CallbackURL != hook:
		return fmt.Errorf("Meta tiene el webhook en %q (activo=%v)", spec.CallbackURL, spec.Active)
	}
	c.p("[ok]    Webhook: %s (campo messages)", hook)

	if cfg.WABAID == "" {
		return errors.New("falta el ID de la cuenta de WhatsApp Business: pasalo con -cuenta <id> (lo muestra Meta en Configuracion de la API)")
	}
	subs, err := cl.SubscribedApps(ctx, cfg.WABAID)
	if err != nil {
		return fmt.Errorf("Meta no deja leer la cuenta %s: %w", cfg.WABAID, err)
	}
	if !slices.Contains(subs, cfg.AppID) {
		if err := cl.SubscribeApp(ctx, cfg.WABAID); err != nil {
			return fmt.Errorf("no se pudo suscribir la app a la cuenta: %w", err)
		}
		c.p("[ok]    App suscrita a la cuenta %s", cfg.WABAID)
	} else {
		c.p("[ok]    App ya estaba suscrita a la cuenta %s", cfg.WABAID)
	}

	err = c.set.Update(c.ctx, func(s *settings.Settings) error {
		if s.WhatsApp.Canal == "" {
			s.WhatsApp.Canal = config.ChannelCloud
		}
		s.WhatsApp = cfg
		return nil
	})
	if err != nil {
		return err
	}
	c.p("[ok]    Configuracion guardada")
	fmt.Fprintln(c.out, "\nWhatsApp conectado de punta a punta. Escribe al numero desde un celular.")
	return nil
}

// ---------- wompi ----------

func (c *cli) wompi(args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	sub, rest := args[0], args[1:]
	w := c.set.Get().Wompi
	switch sub {
	case "ver":
		c.p("Listo para links:  %s", yesNo(w.Enabled()))
		c.p("Llave publica:     %s", w.PublicKey)
		c.p("Llave privada:     %s", maskOr(w.PrivateKey))
		c.p("Secreto eventos:   %s", maskOr(w.EventsSecret))
		c.p("Modo:              %s", map[bool]string{true: "pruebas (sandbox)", false: "produccion"}[w.Sandbox])
		base := c.set.Get().General.PublicURL
		if base == "" {
			base = "https://TU-DOMINIO"
		}
		c.p("URL de eventos:    %s/webhook/wompi", base)
		return nil
	case "quitar":
		if err := c.set.Update(c.ctx, func(s *settings.Settings) error {
			s.Wompi = config.WompiConfig{Sandbox: true}
			if s.Negocio.Pagos.Metodo == "wompi" {
				s.Negocio.Pagos.Metodo = "manual"
			}
			return nil
		}); err != nil {
			return err
		}
		c.applied("Wompi desconectado (los pagos pasan a modo manual si estaban en Wompi).")
		return nil
	case "probar":
		if w.PublicKey == "" {
			return errors.New("falta la llave publica (motor wompi configurar --llave-publica ...)")
		}
		if !w.Enabled() {
			return errors.New("falta la llave privada (motor wompi configurar --llave-privada -)")
		}
		ctx, cancel := context.WithTimeout(c.ctx, 25*time.Second)
		defer cancel()
		info, err := payments.NewWompi(w.PrivateKey, w.EventsSecret, w.Sandbox).CheckMerchant(ctx, w.PublicKey)
		if err != nil {
			return err
		}
		c.p("Conexion correcta: comercio %q (%s), modo pruebas: %s.", info.Name, info.Email, yesNo(info.Sandbox))
		return nil
	case "configurar":
		fs := flag.NewFlagSet("wompi", flag.ContinueOnError)
		pub := fs.String("llave-publica", "", "")
		priv := fs.String("llave-privada", "", "")
		ev := fs.String("eventos", "", "")
		prod := fs.Bool("produccion", false, "")
		test := fs.Bool("pruebas", false, "")
		if _, err := parse(fs, rest); err != nil {
			return err
		}
		if *prod && *test {
			return errors.New("elige --produccion o --pruebas, no los dos")
		}
		seen := visited(fs)
		err := c.set.Update(c.ctx, func(s *settings.Settings) error {
			var err error
			if seen["llave-publica"] {
				s.Wompi.PublicKey = strings.TrimSpace(*pub)
			}
			if seen["llave-privada"] {
				if s.Wompi.PrivateKey, err = secret(*priv); err != nil {
					return err
				}
			}
			if seen["eventos"] {
				if s.Wompi.EventsSecret, err = secret(*ev); err != nil {
					return err
				}
			}
			if *prod {
				s.Wompi.Sandbox = false
			}
			if *test {
				s.Wompi.Sandbox = true
			}
			return nil
		})
		if err != nil {
			return err
		}
		c.applied("Wompi configurado. Para usarlo: motor negocio poner pagos.metodo wompi. Verifica con: motor wompi probar")
		return nil
	}
	return fmt.Errorf("no conozco 'wompi %s'; usa 'motor ayuda'", sub)
}

// ---------- token / url ----------

func (c *cli) token(args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "ver":
		t := c.set.Get().General.AdminToken
		if t == "" {
			c.p("Sin token: el panel solo abre desde este computador.")
		} else {
			c.p("Token configurado: %s", settings.Mask(t))
		}
		return nil
	case "generar":
		t := settings.NewToken()
		if err := c.set.Update(c.ctx, func(s *settings.Settings) error { s.General.AdminToken = t; return nil }); err != nil {
			return err
		}
		c.p("Token nuevo (guardalo, no se vuelve a mostrar completo):\n\n  %s\n", t)
		c.applied("Abre el panel con ?token=" + t + " la primera vez.")
		return nil
	case "poner":
		if len(args) != 2 {
			return errors.New("uso: motor token poner TOKEN")
		}
		v, err := secret(args[1])
		if err != nil {
			return err
		}
		if err := c.set.Update(c.ctx, func(s *settings.Settings) error { s.General.AdminToken = v; return nil }); err != nil {
			return err
		}
		c.applied("Token actualizado.")
		return nil
	case "quitar":
		if err := c.set.Update(c.ctx, func(s *settings.Settings) error { s.General.AdminToken = ""; return nil }); err != nil {
			return err
		}
		c.applied("Token quitado: el panel solo abre desde este computador.")
		return nil
	}
	return fmt.Errorf("no conozco 'token %s'; usa 'motor ayuda'", args[0])
}

func (c *cli) url(args []string) error {
	if len(args) == 0 {
		g := c.set.Get().General
		if g.PublicURL == "" {
			c.p("Sin direccion publica. Fijala con: motor url https://tienda.midominio.com")
		} else {
			c.p("%s", g.PublicURL)
		}
		return nil
	}
	v := args[0]
	if v == "quitar" {
		v = ""
	}
	if err := c.set.Update(c.ctx, func(s *settings.Settings) error { s.General.PublicURL = v; return nil }); err != nil {
		return err
	}
	c.applied("Direccion publica actualizada.")
	return nil
}

// ---------- negocio ----------

type field struct {
	key  string
	ptr  any
	pct  bool // stored as 0..1, shown/entered as percent
	help string
}

func negocioFields(n *config.Negocio) []field {
	return []field{
		{"nombre_tienda", &n.NombreTienda, false, "nombre de la tienda"},
		{"ciudad", &n.Ciudad, false, "ciudad base"},
		{"persona.nombre", &n.Persona.Nombre, false, "nombre de la vendedora"},
		{"persona.rol", &n.Persona.Rol, false, "rol"},
		{"persona.tono", &n.Persona.Tono, false, "tono de voz"},
		{"pagos.metodo", &n.Pagos.Metodo, false, "manual | wompi"},
		{"pagos.llave_bre_b", &n.Pagos.Manual.LlaveBreB, false, "llave Bre-B"},
		{"pagos.banco", &n.Pagos.Manual.Banco, false, "banco"},
		{"pagos.tipo_cuenta", &n.Pagos.Manual.TipoCuenta, false, "tipo de cuenta"},
		{"pagos.numero_cuenta", &n.Pagos.Manual.NumeroCuenta, false, "numero de cuenta"},
		{"pagos.titular", &n.Pagos.Manual.Titular, false, "titular"},
		{"pagos.instrucciones", &n.Pagos.Manual.Instrucciones, false, "instrucciones de pago"},
		{"pagos.horas_para_pagar", &n.Pagos.HorasParaPagar, false, "horas antes de marcar un pago atrasado"},
		{"autonomia.auto_aprobar_pedidos", &n.Autonomia.AutoAprobarPedidos, false, "aprobar pedidos sin el dueno (si/no)"},
		{"autonomia.auto_max_valor", &n.Autonomia.AutoMaxValor, false, "valor maximo para aprobar solo (COP)"},
		{"autonomia.auto_solo_contraentrega", &n.Autonomia.AutoSoloCOD, false, "solo contraentrega (si/no)"},
		{"autonomia.auto_solo_riesgo_normal", &n.Autonomia.AutoSoloRiesgoBajo, false, "solo zonas de riesgo normal (si/no)"},
		{"conversacion.espera_agrupar_seg", &n.Conversacion.EsperaAgruparSeg, false, "segundos de espera para agrupar mensajes"},
		{"conversacion.pausa_entre_mensajes_ms", &n.Conversacion.PausaEntreMensajesMs, false, "pausa entre burbujas (ms)"},
		{"conversacion.historial_maximo", &n.Conversacion.HistorialMaximo, false, "mensajes que recuerda la IA"},
		{"finanzas.tasa_devolucion", &n.Finanzas.TasaDevolucion, true, "devoluciones esperadas (%)"},
		{"finanzas.tasa_confirmacion", &n.Finanzas.TasaConfirmacion, true, "pedidos que se confirman (%)"},
		{"finanzas.margen_seguridad", &n.Finanzas.MargenSeguridad, true, "margen de seguridad (%)"},
		{"finanzas.trm_cop_usd", &n.Finanzas.TRM, false, "pesos por dolar"},
		{"dropi.modo", &n.Dropi.Modo, false, "manual | api"},
		{"notificar.whatsapp_dueno", &n.Notificar.WhatsAppDueno, false, "WhatsApp del dueno para avisos"},
	}
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "si", "sí", "s", "true", "1", "on", "yes":
		return true, nil
	case "no", "n", "false", "0", "off":
		return false, nil
	}
	return false, fmt.Errorf("%q no es si/no", v)
}

func (f field) show() string {
	switch p := f.ptr.(type) {
	case *string:
		return *p
	case *int:
		return strconv.Itoa(*p)
	case *int64:
		return strconv.FormatInt(*p, 10)
	case *bool:
		return yesNo(*p)
	case *float64:
		if f.pct {
			return strconv.FormatFloat(*p*100, 'f', -1, 64) + "%"
		}
		return strconv.FormatFloat(*p, 'f', -1, 64)
	}
	return ""
}

func (f field) set(v string) error {
	v = strings.TrimSpace(v)
	switch p := f.ptr.(type) {
	case *string:
		*p = v
	case *int:
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s: %q no es un numero entero", f.key, v)
		}
		*p = n
	case *int64:
		n, err := strconv.ParseInt(strings.NewReplacer(".", "", ",", "").Replace(v), 10, 64)
		if err != nil {
			return fmt.Errorf("%s: %q no es un numero entero", f.key, v)
		}
		*p = n
	case *bool:
		b, err := parseBool(v)
		if err != nil {
			return fmt.Errorf("%s: %v", f.key, err)
		}
		*p = b
	case *float64:
		raw := strings.TrimSuffix(v, "%")
		x, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64)
		if err != nil {
			return fmt.Errorf("%s: %q no es un numero", f.key, v)
		}
		if f.pct && (strings.HasSuffix(v, "%") || x > 1) {
			x /= 100
		}
		*p = x
	}
	return nil
}

func (c *cli) negocio(args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "ver":
		n := c.set.Get().Negocio
		w := c.tw()
		for _, f := range negocioFields(&n) {
			fmt.Fprintf(w, "%s\t%s\t(%s)\n", f.key, f.show(), f.help)
		}
		return w.Flush()
	case "poner":
		if len(args) < 3 {
			return errors.New("uso: motor negocio poner CLAVE VALOR  (claves: motor negocio ver)")
		}
		key, val := args[1], strings.Join(args[2:], " ")
		err := c.set.Update(c.ctx, func(s *settings.Settings) error {
			for _, f := range negocioFields(&s.Negocio) {
				if f.key == key {
					return f.set(val)
				}
			}
			return fmt.Errorf("la clave %q no existe; mira las claves con: motor negocio ver", key)
		})
		if err != nil {
			return err
		}
		c.applied(key + " = " + val)
		return nil
	}
	return fmt.Errorf("no conozco 'negocio %s'; usa 'motor ayuda'", args[0])
}

// ---------- productos / guia ----------

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func parseOffer(s string) (catalog.Offer, error) {
	parts := strings.Split(s, ":")
	bad := fmt.Errorf("oferta %q no valida: usa ID:UNIDADES:PRECIO, por ejemplo 2x:2:149900", s)
	if len(parts) != 3 {
		return catalog.Offer{}, bad
	}
	u, err1 := strconv.Atoi(parts[1])
	pr, err2 := strconv.ParseInt(strings.NewReplacer(".", "", ",", "").Replace(parts[2]), 10, 64)
	if err1 != nil || err2 != nil {
		return catalog.Offer{}, bad
	}
	return catalog.Offer{ID: strings.TrimSpace(parts[0]), Label: strings.TrimSpace(parts[0]), Units: u, Price: pr}, nil
}

func (c *cli) productos(args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	sub, rest := args[0], args[1:]
	need := func() (string, error) {
		if len(rest) != 1 {
			return "", fmt.Errorf("uso: motor productos %s SKU", sub)
		}
		return rest[0], nil
	}
	switch sub {
	case "listar":
		list := c.cat.List()
		if len(list) == 0 {
			c.p("No hay productos. Crea uno: motor productos agregar --sku ... o importa un YAML.")
			return nil
		}
		w := c.tw()
		fmt.Fprintln(w, "SKU\tNOMBRE\tACTIVO\tCOSTO\tFLETE\tCOSTO CONFIRMADO\tOFERTAS")
		for _, p := range list {
			var offs []string
			for _, o := range p.Offers {
				offs = append(offs, fmt.Sprintf("%s=%s", o.ID, catalog.FormatCOP(o.Price)))
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", p.SKU, p.Name, yesNo(p.Active),
				catalog.FormatCOP(p.UnitCost), catalog.FormatCOP(p.Freight), yesNo(p.CostConfirmed), strings.Join(offs, " "))
		}
		return w.Flush()
	case "ver", "exportar":
		sku, err := need()
		if err != nil {
			return err
		}
		p, ok := c.cat.Get(sku)
		if !ok {
			return fmt.Errorf("no existe el producto %q (motor productos listar)", sku)
		}
		raw, err := yaml.Marshal(p)
		if err != nil {
			return err
		}
		_, err = c.out.Write(raw)
		return err
	case "activar", "desactivar":
		sku, err := need()
		if err != nil {
			return err
		}
		if err := c.cat.SetActive(c.ctx, sku, sub == "activar"); err != nil {
			return err
		}
		c.applied("Producto " + catalog.NormalizeSKU(sku) + ": " + sub + " hecho.")
		return nil
	case "borrar":
		sku, err := need()
		if err != nil {
			return err
		}
		if err := c.cat.Delete(c.ctx, sku); err != nil {
			return err
		}
		c.applied("Producto " + catalog.NormalizeSKU(sku) + " borrado.")
		return nil
	case "importar":
		if len(rest) != 1 {
			return errors.New("uso: motor productos importar ARCHIVO.yaml")
		}
		raw, err := os.ReadFile(rest[0])
		if err != nil {
			return err
		}
		p, err := catalog.ReadYAML(raw)
		if err != nil {
			return err
		}
		if old, ok := c.cat.Get(p.SKU); ok {
			p.Active = old.Active
		}
		saved, err := c.cat.Save(c.ctx, p)
		if err != nil {
			return err
		}
		c.applied("Producto " + saved.SKU + " importado.")
		return nil
	case "agregar", "editar":
		return c.productSave(sub == "agregar", rest)
	}
	return fmt.Errorf("no conozco 'productos %s'; usa 'motor ayuda'", sub)
}

func (c *cli) productSave(create bool, args []string) error {
	fs := flag.NewFlagSet("productos", flag.ContinueOnError)
	sku := fs.String("sku", "", "")
	name := fs.String("nombre", "", "")
	cost := fs.String("costo", "", "")
	freight := fs.String("flete", "", "")
	confirmed := fs.Bool("costo-confirmado", false, "")
	var offers multi
	fs.Var(&offers, "oferta", "")
	pos, err := parse(fs, args)
	if err != nil {
		return err
	}
	seen := visited(fs)
	var p catalog.Product
	if create {
		p.SKU = *sku
		p.Name = *name
	} else {
		if len(pos) != 1 {
			return errors.New("uso: motor productos editar SKU [opciones]")
		}
		old, ok := c.cat.Get(pos[0])
		if !ok {
			return fmt.Errorf("no existe el producto %q (motor productos listar)", pos[0])
		}
		p = old
		if seen["nombre"] {
			p.Name = *name
		}
	}
	num := func(label, v string) (int64, error) {
		n, err := strconv.ParseInt(strings.NewReplacer(".", "", ",", "").Replace(strings.TrimSpace(v)), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("--%s: %q no es un numero", label, v)
		}
		return n, nil
	}
	if seen["costo"] {
		if p.UnitCost, err = num("costo", *cost); err != nil {
			return err
		}
	}
	if seen["flete"] {
		if p.Freight, err = num("flete", *freight); err != nil {
			return err
		}
	}
	if seen["costo-confirmado"] {
		p.CostConfirmed = *confirmed
	}
	if len(offers) > 0 {
		p.Offers = nil
		for _, s := range offers {
			o, err := parseOffer(s)
			if err != nil {
				return err
			}
			p.Offers = append(p.Offers, o)
		}
	}
	if create {
		if _, exists := c.cat.Get(p.SKU); exists {
			return fmt.Errorf("ya existe un producto con el codigo %s; usa 'productos editar'", catalog.NormalizeSKU(p.SKU))
		}
		p.Active = true
	}
	saved, err := c.cat.Save(c.ctx, p)
	if err != nil {
		return err
	}
	c.applied("Producto " + saved.SKU + " guardado.")
	if !saved.CostConfirmed {
		c.p("Aviso: el costo aun no esta confirmado; confirmalo con: motor productos editar %s --costo-confirmado=true", saved.SKU)
	}
	return nil
}

func (c *cli) guia(args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "ver":
		c.p("%s", c.cat.Playbook())
		return nil
	case "poner":
		if len(args) != 2 {
			return errors.New("uso: motor guia poner ARCHIVO.md")
		}
		raw, err := os.ReadFile(args[1])
		if err != nil {
			return err
		}
		if err := c.cat.SetPlaybook(c.ctx, string(raw)); err != nil {
			return err
		}
		c.applied("Guia de ventas actualizada.")
		return nil
	}
	return fmt.Errorf("no conozco 'guia %s'; usa 'motor ayuda'", args[0])
}
