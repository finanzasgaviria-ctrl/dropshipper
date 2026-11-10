# Propuesta: **Dropshipper** — tu equipo de agentes IA para dropshipping COD en Colombia

> Documento de propuesta v1 · 2026-10-27 · Medellín, Colombia
> Basado en la lluvia de ideas del fundador + revisión técnica de `arxi-tui`, DropKiller MCP y la API de Dropi.

---

## 1. La idea en una frase

Una aplicación de escritorio (y luego móvil) donde un **equipo de agentes de IA especializados**
atiende y cierra ventas por WhatsApp, arma los pedidos en Dropi, busca productos ganadores en
DropKiller, propone campañas y landings, y calcula la rentabilidad. **Tú solo apruebas con
un botón** lo que importa (despachos, gasto en publicidad, productos nuevos) y tomas el control
de cualquier conversación cuando quieras.

No es un chatbot. Es una **"oficina virtual"** con un jefe (tú), un gerente IA que te habla
y empleados IA que trabajan 24/7.

---

## 2. El problema real (por orden de dolor)

| # | Dolor hoy | Costo | Qué lo resuelve |
|---|-----------|-------|-----------------|
| 1 | Clientes que escriben y no reciben respuesta a tiempo | **Ventas perdidas** (el dolor nº1) | Agente Vendedor 24/7 por WhatsApp |
| 2 | Pasar a mano los datos del chat a Dropi | Tiempo + errores de dirección | Pedido borrador automático + botón "Aprobar" |
| 3 | Devoluciones COD que se comen la ganancia | Pérdida directa | Reglas de zona (contraentrega vs. pago anticipado) + confirmación |
| 4 | No saber cuánto invertir, si subir el precio o si un producto sirve | Dinero quemado en anuncios | Agente Financiero + Media Buyer |
| 5 | Investigar productos y videos a mano | Horas por semana | Agente Cazador (DropKiller MCP) + sección de Propuestas |
| 6 | Crear landings y textos | Tiempo + baja conversión | Agente Landing/Copy con psicología de ventas |

**Regla de prioridad:** primero lo que **recupera dinero que hoy se pierde** (1–3), después lo que
**multiplica** (4–6).

---

## 3. Decisión de arquitectura clave (léela primero)

### ⚠️ Un bot de WhatsApp no puede vivir solo en una app de escritorio

Si el PC se apaga o la app se cierra, **nadie responde**, y eso es justo el problema que queremos
resolver. Además, WhatsApp, Dropi y los pagos avisan por **webhooks**, que necesitan un servidor
encendido y con dirección pública.

**Por eso la propuesta separa dos piezas:**

```
┌─────────────────────────────────────────────────────────────────────┐
│  MOTOR (servidor 24/7)  — Go + núcleo Arxi                            │
│  • Recibe WhatsApp / pagos / estados de Dropi (webhooks)             │
│  • Ejecuta los agentes (DeepSeek)                                    │
│  • Guarda clientes, pedidos, métricas, conocimiento                  │
│  • Arranca en tu PC (con Cloudflare Tunnel) → luego un VPS ~5-10 USD │
└───────────────▲──────────────────────────────▲──────────────────────┘
                │ API segura                    │
   ┌────────────┴─────────┐          ┌──────────┴───────────┐
   │ APP DE ESCRITORIO    │          │ APP MÓVIL (después)  │
   │ (Wails: Go + React)  │          │ PWA → Capacitor      │
   │ Panel de control,    │          │ Misma interfaz React │
   │ aprobaciones, chat   │          │ Aprobar desde el     │
   │ con el Gerente IA    │          │ celular              │
   └──────────────────────┘          └──────────────────────┘
```

- El **Motor** es el cerebro y nunca se apaga.
- La **app de escritorio** es el panel de control. Si la cierras, las ventas siguen.
- La **app móvil** sale casi gratis porque reutiliza la misma interfaz web (React). Así apruebas
  despachos desde el celular.

---

## 4. ¿Sirve Arxi? Evaluación honesta

Revisé `github.com/michiTrader/arxi-tui` (commit `d47ef27`, oct-2026). Contiene dos cosas:

1. **`arxi` core** (`core/`, módulo `github.com/michiTrader/arxi`): un motor de agentes en Go.
2. **`arxi-tui`**: una interfaz de terminal muy configurable sobre ese motor.

### Lo que encaja muy bien con este negocio

| Capacidad de Arxi | Para qué la usamos en Dropshipper |
|---|---|
| `host/v1`: API para embeber el motor (submit, inspect, cancel, **approve/reject/answer**, subscribe) | El Motor integra Arxi como librería, sin depender de la terminal |
| **Inbox de aprobaciones humanas** + política de herramientas `allow / ask / deny` | Exactamente el "**botón de aceptar**": `crear_pedido_dropi` = `ask`, `responder_whatsapp` = `allow` |
| **Presupuesto por árbol de agentes** (techo de gasto en USD) | Controla cuántos tokens de DeepSeek gasta cada tarea, así no hay sorpresas |
| **Blueprints de equipos** (miembros, etapas, `advance_when`, watchers) | Definir el "equipo": Cazador → Analista → Creativo → propuesta para ti |
| **Triggers / scheduler** (cron) | "Cada mañana busca productos", "cada 6 h revisa métricas de anuncios" |
| **Memoria gobernada** (confianza, procedencia, retención) | La "**bola de nieve de conocimiento**" sin que la IA se invente verdades |
| `run why`, `replay`, `--sim` (reductor puro) | Auditar **por qué** la IA ofreció un descuento o eligió una transportadora, y probar el vendedor gratis en simulación |
| Proveedor **DeepSeek** ya soportado | Usas tu API actual sin cambios |

### Lo que le falta (y cómo lo resolvemos)

| Falta | Solución |
|---|---|
| No es una app gráfica (es TUI) | La interfaz es Wails + React; Arxi queda como motor |
| Cliente MCP (para DropKiller) no está claro | Lo agregamos como herramienta del Motor o lo contribuimos a Arxi |
| Muchas conversaciones simultáneas de WhatsApp (¿es su caso de uso?) | **Spike en la Fase 0**: medir latencia y concurrencia con 20 chats simulados |
| Está en desarrollo activo | Encapsulamos detrás de una interfaz `AgentEngine`. Si algo falla, el plan B es llamar a DeepSeek directo con el mismo contrato |

### Recomendación

**Sí, usar el núcleo de Arxi como motor de agentes**, con Dropshipper como un "product adapter"
(el mismo patrón que tu roadmap describe para *Asha*): Dropshipper **no** modifica el kernel, solo
implementa los puertos (`TextProvider`, `ToolExecutor`, `JobStorage`) y las herramientas del
negocio. Bonus: `arxi-tui` sirve como **consola avanzada** para depurar agentes mientras
desarrollamos.

---

## 5. El equipo de agentes

Un **Gerente IA** (orquestador) habla contigo. Debajo trabajan especialistas:

| Agente | Qué hace | Herramientas | Autonomía por defecto |
|---|---|---|---|
| 🗣️ **Vendedor** | Atiende WhatsApp, resuelve dudas, maneja objeciones, cierra la venta, pide datos | WhatsApp, catálogo, reglas de zona, KB de ventas | **Auto** (responde solo) |
| 📦 **Despachador** | Valida y normaliza la dirección, elige transportadora, decide contraentrega o anticipado, crea el pedido en Dropi | Dropi API, tabla de zonas, Wompi | **Pide aprobación** (botón) |
| 💳 **Cobrador** | Envía el link de pago anticipado, confirma el pago y avisa al Despachador | Wompi / Bold / Nequi (links + webhook) | Auto |
| 🔁 **Postventa** | Avisos de guía y seguimiento, novedades de Dropi, rescate de devoluciones | Dropi (estados/novedades), WhatsApp | Auto, y escala a ti si hay problema |
| 🔎 **Cazador** | Busca productos ganadores y sus mejores videos | **DropKiller MCP** (`find_winning_products`, `search_ads`, `analyze_ad`, `daily_radar`…) | Solo **propone** |
| 🎬 **Analista creativo** | Analiza hooks, ángulos, "frena-scroll", saturación y número de creativos | DropKiller, modelo multimodal | Propone |
| 💰 **Financiero** | Rentabilidad, CPA máximo, precio ideal, COP↔USD, escenarios con devoluciones | Datos internos + Meta Insights | Recomienda |
| 📈 **Media Buyer** | Arma campañas en Meta (siempre en PAUSA) y recomienda escalar, mantener o cortar | Meta Marketing API | **Pide aprobación** |
| 🎨 **Landing / Copy** | Landing por producto: estructura, textos, colores, prueba social, precio, FAQ | Shopify Admin API o landing propia | Propone, tú publicas |
| 📚 **Bibliotecario** | Aprende de cada chat ganado o perdido y de cada campaña. Mantiene el playbook | Memoria Arxi | Propone lecciones, tú las apruebas |

**Niveles de autonomía** (configurables por acción, se mapean a `allow / ask / deny` de Arxi):
- **Nivel 0, Sugerir:** la IA prepara y tú haces.
- **Nivel 1, Aprobar con un botón:** la IA deja todo listo, tú revisas y das "Aceptar". *Es el valor por defecto para despachos y gasto.*
- **Nivel 2, Automático con límites:** p. ej. "despacha solo si es contraentrega en ciudad principal, cliente nuevo y valor < $150.000".

**Botón "Yo tomo"**: en cualquier conversación pausas a la IA y escribes tú, o la llamas. La IA
también te escala sola cuando el cliente pide hablar con una persona, está molesto o hay un caso raro.

---

## 6. Flujo estrella: de "Hola" a pedido despachado

```
Cliente escribe por WhatsApp (viene del anuncio)
        │
        ▼
[Vendedor] saluda en <30 s, detecta producto/anuncio, responde, maneja objeciones
        │  pide: nombre, cédula, teléfono, departamento, ciudad, barrio, dirección, referencias
        ▼
[Despachador] normaliza la dirección (municipio DANE) → consulta la regla de zona
        │
        ├── Zona normal + cobertura COD ─────────► CONTRAENTREGA
        ├── Zona lejana / difícil acceso ────────► ANTICIPO PARCIAL (p. ej. el flete o 20-30 %)
        └── Zona roja / historial de devolución ─► PAGO 100 % ANTICIPADO
        │
        ▼ (si aplica anticipo)
[Cobrador] envía el link de pago → webhook "pagado" → continúa
        │
        ▼
[Vendedor] confirma por WhatsApp un resumen: producto, total, dirección, forma de pago
        │  ("¿Confirmas que todo está correcto? ✅") ← baja mucho las devoluciones
        ▼
📥 TARJETA EN "APROBACIONES": pedido borrador + transportadora + rentabilidad estimada
        │      [ Aceptar ]  [ Editar ]  [ Rechazar ]
        ▼
[Despachador] crea la orden en Dropi → guía → [Postventa] envía el seguimiento al cliente
        │
        ▼
Entregado ✅ / Novedad ⚠️ → Postventa intenta rescatar / Devuelto ❌ → aprende (zona, cliente, producto)
```

### Reglas de zona (editables desde la app)

Tabla por **departamento → ciudad → barrio** con:
- transportadoras con cobertura (Interrapidísimo, Envía, Coordinadora, Servientrega… según lo que ofrezca Dropi),
- ¿acepta contraentrega?, nivel de riesgo (normal / lejano / difícil acceso / zona roja),
- política de pago (COD / anticipo % / 100 %), transportadora preferida.

Se empieza con una tabla base (capitales y áreas metropolitanas = COD). La tabla **aprende** de
tus entregas y devoluciones reales: si en un municipio se devuelve mucho, la app te propone subirlo a "anticipo".

---

## 7. El cerebro financiero (que una devolución no te deje mal)

### Fórmula base por pedido despachado (COD)

```
Ganancia esperada = (1 − d) × (Precio − Costo − Flete) − d × (Flete ida + Flete devolución) − CPA
```
`d` = tasa de devolución esperada (en COD en Colombia suele estar entre 15 % y 35 %; **se mide con tus datos**).

**Ejemplo** (valores ilustrativos):
| Concepto | Valor |
|---|---|
| Precio de venta | $89.900 |
| Costo proveedor Dropi | $35.000 |
| Flete | $15.000 |
| Devolución esperada `d` | 25 % |
| Ganancia si entrega | $39.900 |
| Pérdida si devuelve (ida + vuelta) | −$30.000 |
| **Ganancia esperada antes de anuncios** | 0,75 × 39.900 − 0,25 × 30.000 = **$22.425** |
| **CPA máximo (punto de equilibrio)** | **$22.425** por pedido despachado |
| CPA objetivo (margen de seguridad 30 %) | ≈ $15.700 |

La app hace esto **por producto, ciudad y forma de pago**, y te dice:
- **precio mínimo** para que el producto sea viable con la devolución real,
- **CPA máximo** para Meta Ads (ajustado por la tasa de confirmación: no todo lead termina despachado),
- todo en **COP** con conversión a **USD** (los anuncios de Meta suelen cobrarse en USD).

### Recomendaciones de inversión (heurísticas iniciales, se afinan con tus datos)

| Situación (tras gastar ≥ 3× el CPA objetivo) | Recomendación |
|---|---|
| CPA real ≤ 70 % del equilibrio, confirmación ≥ 80 % | **Escalar**: subir presupuesto 20-30 % cada 48-72 h |
| CPA entre 70 % y 100 % del equilibrio | **Mantener** y probar nuevos creativos y ángulos |
| CPA > equilibrio con buen CTR | Problema de landing u oferta: **probar precio o landing** |
| CPA > equilibrio con CTR bajo | Problema de creativo: **cambiar el hook** |
| Muchos chats pero pocas ventas | Problema de cierre: revisar el guion del Vendedor (la IA te muestra los chats perdidos) |

El embudo completo que medimos: **Impresiones → Clics (CTR, CPC) → Chats → Ventas confirmadas →
Despachados → Entregados → Ganancia neta.** Es lo único que dice la verdad en COD.

---

## 8. Sección "Propuestas" (tú decides)

Una bandeja tipo tarjetas donde la IA deja sus ideas y tú respondes **Aceptar / Rechazar / Pedir cambios**:

- 🆕 **Producto ganador**: datos de DropKiller (ventas 7/30 días, stock, saturación, nº de anunciantes,
  precio en Dropi) + rentabilidad calculada + top 3 videos con su hook analizado.
- 📣 **Campaña**: creativo + copy + público + presupuesto sugerido → al aceptar se crea **en pausa** en Meta.
- 🎨 **Landing**: vista previa → al aceptar se publica en Shopify.
- 💲 **Cambio de precio / presupuesto**: "Subir el producto X a $99.900: el CPA lo permite y la competencia está en $109.900".
- 📚 **Nueva lección para el playbook**: "En Costa Atlántica funciona mejor ofrecer el anticipo como 'reserva'".

Y al revés: **tú le pides** desde el chat con el Gerente IA, por ejemplo: *"Búscame 5 productos de
hogar con menos de 10 anunciantes en Colombia"* o *"Hazme una landing para el producto X con ángulo de regalo navideño"*.

---

## 9. La "bola de nieve" de conocimiento

Tres capas, gobernadas con la memoria de Arxi (con confianza y procedencia, y nada se vuelve
"verdad" sin tu aprobación):

1. **Base curada** (resúmenes propios de principios, no copias de libros):
   - *Oferta y precio*: Alex Hormozi (ecuación de valor, ofertas irresistibles).
   - *Persuasión*: Robert Cialdini (reciprocidad, prueba social, autoridad, escasez **real**, compromiso, simpatía, unidad).
   - *Copy*: Eugene Schwartz (niveles de consciencia y sofisticación), Ogilvy, Gary Halbert, Joanna Wiebe, StoryBrand.
   - *Cierre y negociación*: Chris Voss (empatía táctica), SPIN Selling, manejo de objeciones COD.
   - *Decisión humana*: Kahneman (sesgos, anclaje de precio, aversión a la pérdida).
   - *Habilidades para la vida (OMS)*: autoconocimiento, empatía, comunicación asertiva, relaciones
     interpersonales, toma de decisiones, solución de problemas, pensamiento creativo y crítico, manejo
     de emociones y del estrés. Es la lista de tu lluvia de ideas y se usa en el tono del Vendedor y en las landings.
   - *Landings*: jerarquía visual, hook en el primer pantallazo, problema → agitación → solución,
     beneficios antes que características, prueba social, garantía, FAQ que mata objeciones, CTA
     repetido, precio anclado y colores por emoción.
   - *Contexto Colombia*: modismos paisa, costeño y rolo (el Vendedor adapta el tono a la región del cliente), confianza en COD y miedo a la estafa.
2. **Conocimiento del producto**: ficha, dolores, beneficios, objeciones típicas, garantías y tiempos de entrega.
3. **Aprendizaje propio**: cada chat ganado o perdido, cada campaña y cada devolución deja lecciones
   que el Bibliotecario propone y tú apruebas. Así el playbook crece y es **tuyo**.

---

## 10. Pantallas de la aplicación

1. **Inicio**: ventas de hoy, chats activos, pendientes de aprobar, ganancia neta, gasto en anuncios, alertas.
2. **Aprobaciones**: la bandeja de un clic (pedidos, campañas, precios). Es la pantalla más usada.
3. **Conversaciones**: WhatsApp en vivo con etiqueta de estado (nuevo, interesado, objeción, esperando pago, cerrado) y botón **"Yo tomo"**.
4. **Pedidos**: kanban `Borrador → Esperando pago → Por aprobar → En Dropi → En tránsito → Entregado / Novedad / Devuelto`.
5. **Productos y Laboratorio**: catálogo propio, rentabilidad por producto y análisis de videos y competencia.
6. **Propuestas**: lo que proponen el Cazador, el Creativo, el Media Buyer y el Bibliotecario.
7. **Campañas**: métricas de Meta y recomendaciones de escalar, mantener o cortar.
8. **Finanzas**: P&L en COP/USD, CPA, ROAS, tasa de devolución por zona, producto y transportadora.
9. **Gerente IA**: chat directo para pedir análisis, propuestas o explicaciones ("¿por qué elegiste Interrapidísimo?").
10. **Ajustes**: conexiones, reglas de zona, niveles de autonomía, tono del vendedor, límites de gasto.

---

## 11. Integraciones (qué existe hoy y qué hay que confirmar)

| Servicio | Vía | Estado / nota |
|---|---|---|
| **DeepSeek** | API compatible con OpenAI (ya soportado por Arxi) | ✅ Ya tienes la API. Modelo principal para chats y razonamiento (barato) |
| **WhatsApp** | **WhatsApp Business Cloud API (Meta) oficial** o **WhatsApp Web** (teléfono vinculado con código QR, no oficial) | ✅ El Motor soporta **ambos canales** y se elige en Conexiones. La API oficial es la recomendada y estable, pero Meta exige documentos del negocio para dar el token permanente. WhatsApp Web (librería `whatsmeow`) permite empezar sin esos papeles, pero **no es oficial**: WhatsApp puede restringir el número si envía muchos mensajes o escribe a desconocidos. Úsalo con un número dedicado y solo para responder a quien te escribe; migra a la API oficial cuando tengas la verificación |
| **Dropi** | API de integraciones (token generado en Dropi + `POST /integrations/login`) | ⚠️ Hay que **solicitar el acceso** al equipo de Dropi y confirmar los endpoints de crear orden, transportadoras, cotización y estados |
| **DropKiller** | **Servidor MCP oficial** `https://app.dropkiller.com/api/mcp` (OAuth 2.1, 33 herramientas) | ✅ Existe. ⚠️ Requiere **plan Advanced** |
| **Kalodata** | **API abierta** (POST + JSON, clave en la cabecera, base `https://www.kalodata.com/openapi/v1/tiktok/...`). Modulos: Categoria, Tienda, Creador, Producto, Video y Livestream; endpoints de *ranking* y de *detalle*; 15 mercados. **No hay MCP oficial.** | ⚠️ Segun fuentes de terceros (mayo 2026) la API solo se da con el plan **Enterprise (desde ~US$570/mes)** y el plan de 7 dias de prueba **no incluye API**. Confirmar precio y acceso en el Open Center de Kalodata antes de construir. Nota: Kalodata analiza **TikTok Shop**, no Colombia ni Dropi: sirve para detectar tendencias de producto, no para validar demanda local |
| **Shopify** | Admin API (GraphQL) | ✅ Productos, páginas y landings |
| **Meta Ads** | Marketing API | ✅ Crear campañas, conjuntos y anuncios (en pausa) y leer Insights. Requiere app de Meta + permisos `ads_management` |
| **Pagos anticipados** | Wompi (Bancolombia) / Bold / Mercado Pago: links de pago + webhook | ✅ La confirmación automática del pago es clave |
| **Envía** | Dentro de Dropi como transportadora (y Envia.com tiene API de cotización multi-transportadora) | ⚠️ Ver si basta con lo que expone Dropi |
| **Análisis de video** | DeepSeek **no ve video** → `analyze_ad` de DropKiller + un modelo multimodal económico para frames y transcripción | ⚠️ Costo bajo, pero es otro proveedor |
| **Tasa COP/USD** | API pública de tasas (TRM) | ✅ |

---

## 12. Stack técnico propuesto

| Capa | Tecnología | Por qué |
|---|---|---|
| Motor | **Go** + `github.com/michiTrader/arxi` (`host/v1`) | Mismo lenguaje que Arxi, un solo binario, muy eficiente |
| Base de datos | SQLite (inicio) → PostgreSQL (escala) | Simple al empezar |
| Escritorio | **Wails** (Go + React + TypeScript) | Binario liviano para Windows/Mac, mismo lenguaje que el Motor |
| Móvil | La misma UI React como **PWA** → **Capacitor** | Reutilización casi total |
| Webhooks en desarrollo | Cloudflare Tunnel | WhatsApp y Wompi llegan a tu PC sin abrir puertos |
| Producción | VPS pequeño (Hetzner, DigitalOcean) o Railway | 24/7 por ~5-10 USD/mes |
| Secretos | Bóveda cifrada del Motor (`secretstore` de Arxi) | Las API keys no viven en la app de escritorio |

Estructura del repo prevista:
```
dropshipper/
├── motor/            # servidor Go: webhooks, agentes (Arxi), reglas, BD
│   ├── agents/       # blueprints y prompts de cada agente
│   ├── tools/        # whatsapp, dropi, dropkiller_mcp, shopify, meta, wompi
│   ├── rules/        # zonas, rentabilidad, autonomía
│   └── knowledge/    # base de conocimiento curada
├── app/              # Wails + React (escritorio; luego PWA/móvil)
└── docs/             # esta propuesta, decisiones, guías
```

---

## 13. Hoja de ruta por fases

| Fase | Entregable | Resultado de negocio | Tiempo estimado |
|---|---|---|---|
| **0. Cimientos** | Repo, Motor base, número WhatsApp Cloud API, DeepSeek conectado, **spike de Arxi** (20 chats simulados), solicitud de API a Dropi | Validar la arquitectura | 1 semana |
| **1. MVP Vendedor** ⭐ | Vendedor IA responde y cierra 24/7, extrae datos, crea **pedido borrador**, botón Aprobar → Dropi, "Yo tomo", reglas de zona básicas, app de escritorio con Conversaciones + Aprobaciones + Pedidos | **Dejas de perder clientes** | 3-4 semanas |
| **2. Cobro y postventa** | Links de pago anticipado + confirmación automática, seguimiento de guía, novedades y rescate de devoluciones | Menos devoluciones, más flujo de caja | 2 semanas |
| **3. Finanzas** | Rentabilidad por pedido y producto, CPA máximo, COP/USD, lectura de Meta Insights, recomendaciones de escalar | Saber cuánto invertir | 2 semanas |
| **4. Cazador + Propuestas** | DropKiller MCP **y Kalodata MCP (pendiente; ambos proveedores deben coexistir, probar con los 7 días gratis de Kalodata)**, análisis de videos y hooks, bandeja de Propuestas, búsquedas programadas | Nuevos productos con datos | 2 semanas |
| **5. Landings** | Generador de landing por producto (copy + psicología + diseño) → Shopify | Lanzar productos en horas | 2 semanas |
| **6. Media Buyer** | Crear campañas en Meta (en pausa) desde una propuesta + recomendaciones diarias de presupuesto | Lanzar y escalar anuncios con un clic | 2-3 semanas |
| **7. Móvil + bola de nieve** | PWA/app móvil, Bibliotecario con lecciones aprobadas | Operar desde el celular | continuo |

**El MVP (Fase 1) es lo primero porque ataca directamente el dolor nº1**: los clientes perdidos.

---

## 14. Reglas de oro (calidad, legal y ética)

- **Humano en el bucle para el dinero**: despachos, gasto en anuncios y cambios de precio pasan por tu aprobación hasta que tú decidas lo contrario.
- **Natural sin engañar**: el Vendedor escribe como una persona (tono cálido, mensajes cortos, emojis con moderación, sin respuestas de robot). Pero **no niega ser un asistente si se le pregunta directamente**, no promete lo que el producto no hace y **no usa escasez falsa**. Esto protege tu cuenta de WhatsApp y Meta y evita problemas con el **Estatuto del Consumidor (Ley 1480 de 2011)**.
- **Datos personales**: cédula, dirección y teléfono se manejan según la **Ley 1581 de 2012 (Habeas Data)**, con mensaje de aceptación de tratamiento de datos.
- **Políticas de Meta**: nada de afirmaciones médicas o "antes/después" prohibidos en anuncios y landings. El Creativo lo revisa antes de proponer.
- **Todo auditable**: cada decisión de la IA queda registrada (gracias a Arxi: `why`, `replay`).
- **Límites de gasto**: techo diario de tokens de IA y de gasto publicitario que la IA no puede superar.

---

## 15. Costos operativos aproximados (a verificar)

| Rubro | Estimado |
|---|---|
| DeepSeek | Muy bajo: centavos de USD por conversación completa |
| WhatsApp Cloud API | Las respuestas dentro de las 24 h de un mensaje del cliente no tienen costo de plantilla. Las plantillas fuera de esa ventana (p. ej. avisos de envío) cobran por mensaje según la tarifa de Meta para Colombia |
| Servidor | ~5-10 USD/mes (VPS) |
| DropKiller | Plan Advanced (requerido para MCP) |
| Pasarela de pagos | Comisión por transacción (Wompi, Bold, etc.) |

---

## 16. Preguntas abiertas para decidir juntos

1. ¿Tienes (o puedes conseguir) un **número de WhatsApp dedicado** para la Cloud API? El número no puede estar a la vez en la app normal de WhatsApp.
2. ¿Ya pediste el **token de integraciones de Dropi**? ¿Tu cuenta Dropi es de dropshipper con Shopify conectado?
3. ¿Tu plan de DropKiller es **Advanced**?
4. ¿Prefieres empezar con el Motor **en tu PC** (gratis, con túnel) o directo en un **VPS**?
5. ¿Qué pasarela usas o prefieres para anticipos: Wompi, Bold, Nequi, Mercado Pago?
6. Para la Fase 1, ¿con cuál producto arrancamos? (el de tu primera venta es ideal para calibrar al Vendedor).
7. ¿El repo de Arxi core (`github.com/michiTrader/arxi`) es público o lo consumimos desde `arxi-tui/core`?
