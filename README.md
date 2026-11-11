# Dropshipper

Equipo de agentes de IA para dropshipping contraentrega en Colombia. Primer producto: **LullaBites**.

## Estado: Fase 1, Vendedor IA por WhatsApp (codigo listo, falta conectar tus cuentas)

El **Motor** (`motor/`) es un solo programa en Go que:
- atiende WhatsApp (por la **API oficial de Meta** o por **WhatsApp Web vinculando tu telefono con un codigo QR**) con una vendedora IA que usa **cualquier proveedor compatible con OpenAI** (DeepSeek, OpenAI, OpenRouter, Groq, Gemini, Ollama local, Arxi u otro: basta URL, modelo y clave);
- vende **varios productos**, cada uno con su ficha, ofertas, costos y finanzas;
- toma los datos de envio y decide **contraentrega o anticipo** segun `knowledge/zonas.yaml`;
- cobra anticipos por **llave Bre-B / cuenta bancaria** (verificas el comprobante) o por **Wompi** (automatico);
- deja cada pedido en **Aprobaciones** con su rentabilidad (incluye el riesgo de devolucion) para que lo apruebes;
- permite **tomar el control** de cualquier chat y registra **por que** la IA hizo cada cosa;
- trae un **panel web** (escritorio y celular, instalable) y un **simulador** para probar sin WhatsApp.

## WhatsApp: dos canales

- **WhatsApp Web (codigo QR)**: no pide documentos del negocio. En **Conexiones > WhatsApp** pulsa *Vincular telefono* y
  escanea el QR desde *Ajustes > Dispositivos vinculados*. **No es oficial**: usa un numero dedicado, no hagas envios masivos.
- **API oficial de Meta**: la opcion estable y recomendada cuando tengas el negocio verificado.

Puedes cambiar de canal en cualquier momento; las credenciales de la API oficial se conservan.

### Direccion publica con ngrok (para Meta y Wompi)

La direccion `localhost:8080/webhook/...` solo existe en tu computador. En **Conexiones > Direccion publica (ngrok)** pegas
tu Authtoken de ngrok (y, si quieres, tu dominio fijo gratis) y el Motor abre el tunel solo, sin terminal. El panel muestra
las direcciones exactas del webhook de WhatsApp y de Wompi. Por el tunel **solo pasan los webhooks**: el panel nunca queda
expuesto. Detalles en [`docs/CONFIGURACION.md`](docs/CONFIGURACION.md).

## Estado del proyecto por fases

| Fase | Estado |
|---|---|
| 0. Cimientos | Hecha (Motor, IA generica, panel, base de datos) |
| 1. MVP Vendedor | Hecha (vendedora IA, pedidos borrador, aprobaciones, reglas de zona); falta conectar tus cuentas reales |
| 2. Cobro y postventa | Parcial (Bre-B/Wompi y comprobantes listos; **Aprobar crea la orden en Dropi** y la IA recomienda transportadora con tiempo de entrega; falta recibir guias y estados de Dropi solos y rescatar devoluciones) |
| 3. Finanzas | Parcial (rentabilidad por pedido y producto, fletes de Dropi; falta Meta Insights) |
| 4. Cazador + Propuestas | **Parcial.** Pestaña **Cazador** (manual, gratis): apuntas un producto con los datos de Dropi y de la extensión gratuita de DropKiller, y el Motor dice si vale la pena probarlo y cuánto gastar en anuncios. Pendiente: que la IA cace sola (DropKiller Advanced o similar, sin pagar Kalodata Enterprise). Ver `docs/PROPUESTA.md` |
| 5. Landings, 6. Media Buyer, 7. Movil | Pendientes |

## Todo se configura desde el panel

Solo tienes que encender el Motor y abrir el panel: **Dropi, IA, WhatsApp, cobros, transportadoras, simulacion de envios, costos y tema claro/oscuro se configuran desde la interfaz**, sin reiniciar y sin tocar la terminal. Los comandos `motor ...` siguen existiendo (opcionales, para administradores) y editan los mismos datos.

```bash
cd motor
go run ./cmd/motor            # enciende el Motor: abre http://localhost:8080
```

En el panel: **Conexiones** (IA, **Dropi**, WhatsApp, cobros, seguridad, cada una con boton de prueba),
**Envios** (transportadoras con notas para la IA, cotizador de fletes y simulacion de pedido en Dropi, que no crea nada),
**Productos** (lista, detalle, finanzas y boton "Traer costos de Dropi") y **Ajustes** (persona de la IA, autonomia,
supuestos financieros, espera antes de responder -60 s por defecto- y apariencia clara/oscura/automatica).

Para administradores que prefieran automatizar, el mismo programa trae comandos opcionales (`motor ayuda`) que editan los mismos datos. No son necesarios.

**Seguridad:** mientras no exista un token, el panel solo abre desde el mismo computador. Los secretos nunca
vuelven al navegador (se muestran enmascarados).

Guia paso a paso (WhatsApp, Bre-B, Dropi): [`docs/CONFIGURACION.md`](docs/CONFIGURACION.md)
Propuesta completa: [`docs/PROPUESTA.md`](docs/PROPUESTA.md)
Por que Arxi todavia no es el motor de agentes: [`docs/DECISION-ARXI.md`](docs/DECISION-ARXI.md)

## Estructura

```
motor/
├── cmd/motor/            # programa principal y terminal (cli.go)
├── internal/
│   ├── agent/            # vendedora IA: loop de herramientas sobre la API compatible con OpenAI
│   ├── llm/              # cliente OpenAI-compatible, presets, listar modelos, errores en espanol
│   ├── settings/         # ajustes en base de datos, validacion, vistas enmascaradas
│   ├── hub/              # integraciones vivas (IA, WhatsApp, Wompi) que se reconstruyen al cambiar ajustes
│   ├── catalog/          # catalogo multiproducto en base de datos
│   ├── sales/            # ciclo de vida del pedido (la IA nunca decide dinero)
│   ├── rules/            # zonas/anticipos y rentabilidad con devoluciones (con pruebas)
│   ├── convo/            # orquestador: agrupar mensajes, Yo tomo, comprobantes, eventos en vivo
│   ├── whatsapp/         # WhatsApp Cloud API oficial + verificación de firma
│   ├── whatsweb/         # WhatsApp Web (whatsmeow): vinculación por QR, envío y recepción
│   ├── tunnel/           # túnel ngrok propio del Motor: dirección pública solo para los webhooks
│   ├── payments/         # Wompi: links de pago + verificación de eventos
│   ├── store/            # SQLite (sin CGO, compila igual en Windows)
│   └── api/              # API HTTP + panel web embebido
├── knowledge/            # semilla inicial: playbook, ficha de LullaBites, reglas de zona
└── negocio.yaml          # semilla inicial de ajustes (despues manda la base de datos)
```

## Pruebas

```bash
cd motor && go test ./...
```
Cubren las reglas de zona y rentabilidad, la firma de Wompi y WhatsApp, el cliente de IA y sus presets, los ajustes y
secretos enmascarados, el catalogo multiproducto, el acceso local sin token y el flujo de venta
completo (contraentrega en Medellín, anticipo en zona lejana, zona roja, Yo tomo, escalamiento, mensajes agrupados
y cambios después de confirmar).
