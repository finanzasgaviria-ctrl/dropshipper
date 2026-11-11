# Guia de configuracion y uso (Fase 1: Vendedor IA por WhatsApp)

Esta guia te lleva de cero a tener la vendedora IA atendiendo tu WhatsApp y dejando los pedidos listos para que
tu los apruebes.

**Solo necesitas encender el Motor. Todo lo demas se hace desde el panel web** (`http://localhost:8080`):
conexiones (IA, WhatsApp, Dropi, cobros), productos, transportadoras, pruebas y ajustes. Cada conexion trae un
boton **Probar conexion** y mensajes de error claros. No hay que escribir comandos ni editar archivos.

Los datos viven en la base de datos del Motor (`data/`) y los cambios se aplican al instante, sin reiniciar.
El archivo `.env` es **opcional**: solo siembra valores en el primer arranque (util para servidores sin nadie que
abra el panel).

---

## 1. Instalar y arrancar el Motor

1. Instala **Go 1.26 o superior**: <https://go.dev/dl/>.
2. Descarga el repo:
   ```bash
   git clone https://github.com/michiTrader/dropshipper.git
   cd dropshipper/motor
   ```
3. Compila (o usa `go run ./cmd/motor` en su lugar):
   ```bash
   go build -o motor ./cmd/motor        # en Windows: motor.exe
   ```
4. Enciende el Motor:
   ```bash
   ./motor
   ```
5. Abre **http://localhost:8080**. Veras **Inicio** con una lista de **puesta en marcha** que te dice que falta.

> **Seguridad por defecto.** Mientras no exista un token de acceso, el panel solo abre desde el mismo computador
> donde corre el Motor. Cualquier otro equipo recibe "acceso restringido". Cuando quieras entrar desde el celular o
> publicar el Motor en internet, crea un token desde el panel (seccion 6).

---

## 2. Conectar la inteligencia artificial

Sirve **cualquier proveedor compatible con la API de OpenAI**. Solo necesitas tres datos: la direccion de la API,
el modelo y la clave.

**En el panel:** Conexiones > Inteligencia artificial > **Agregar proveedor**.

1. Elige un punto de partida (DeepSeek, OpenAI, OpenRouter, Groq, Gemini, Ollama local o Personalizado).
   Para **Arxi** u otro servicio propio elige *Personalizado* y escribe su URL.
2. Pega la clave de API.
3. Pulsa **Cargar modelos** para elegir el modelo de la lista que ofrece el proveedor (o escribelo).
4. Pulsa **Probar conexion**: hace una llamada real y te dice si funciona o por que no
   (clave incorrecta, saldo agotado, modelo inexistente, URL equivocada, servidor caido...).
5. **Guardar y usar**.

Puedes tener varios proveedores guardados y cambiar el que esta en uso con un clic.

**Sobre la URL:** debe ser la direccion base de una API compatible con OpenAI, normalmente terminada en `/v1` (por ejemplo `https://api.ejemplo.com/v1`). Si pegas solo el dominio (`https://api.ejemplo.com`) y el proveedor responde "Route ... not found" (404), el Motor reintenta solo con `/v1`, recuerda la que funciona y, al probar la conexion, te sugiere guardar la direccion corregida. No incluyas `/chat/completions`.

Si la IA falla al responder, el Motor crea una alerta con la causa en espanol (clave rechazada, sin saldo, URL incorrecta, modelo inexistente...) y deja el chat para que respondas tu.

La clave nunca se vuelve a mostrar completa: el panel solo muestra los ultimos 4 caracteres, y al editar un
proveedor, dejar el campo vacio conserva la clave guardada.

---

## 3. Productos

El Motor vende **varios productos**. En **Productos** ves la lista; al abrir uno tienes cuatro pestanas:

| Pestana | Que contiene |
|---|---|
| **Ficha** | Nombre, descripcion, modo de uso, ingredientes, precauciones, envio y garantia. La IA no inventa nada que no este aqui |
| **Ofertas y costos** | Costo del producto, flete, ofertas (1, 2, 3 unidades...) y la casilla **Costo confirmado** |
| **Finanzas** | Ganancia por oferta (si entrega, si devuelve, esperada), CPA maximo para anuncios y los resultados reales de ese producto. Se recalcula mientras escribes |
| **Argumentos de venta** | Dolores del cliente, beneficios y como responder a cada objecion |

Un producto nuevo se crea **inactivo**; pulsa **Activar para vender** cuando este listo. Mientras el costo no este
confirmado con el proveedor, la rentabilidad se marca como estimada.

Un producto con pedidos no se puede borrar (desactivalo), y siempre debe quedar al menos un producto activo.

La **Guia de ventas** (boton en la lista) son las instrucciones generales de la IA para todos los productos.

---

## 4. Datos para cobrar anticipos

En zonas de riesgo el cliente paga un anticipo antes del despacho.

**En el panel:** Conexiones > **Cobro de anticipos**. Elige el metodo:

- **Manual**: pones tu llave Bre-B y/o los datos de tu cuenta. El sistema (no la IA) envia al cliente el valor
  exacto y una referencia. El cliente manda el comprobante, te llega una alerta, **verificas en tu banco** y
  pulsas **Pago recibido** en Aprobaciones. La IA nunca da un pago por recibido solo por una captura.
- **Wompi** (opcional): confirmacion automatica, sin revisar capturas. Abre una cuenta en
  <https://comercios.wompi.co>, copia tus llaves en el bloque **Wompi** y pulsa **Probar conexion**.
  En Wompi, registra la **URL de eventos** que muestra el panel. Wompi cobra comision por transaccion.

---

## 5. Conectar WhatsApp

El Motor ofrece **dos canales**. Se elige en **Conexiones > WhatsApp** y se puede cambiar cuando quieras (las
credenciales de la API oficial se conservan al cambiar de canal):

| Canal | Cuando usarlo | Ventajas | Riesgos |
|---|---|---|---|
| **WhatsApp Web (codigo QR)** | Aun no tienes los documentos del negocio que pide Meta | Funciona en minutos, sin token ni webhook ni direccion publica | **No es oficial**: WhatsApp puede restringir o bloquear el numero |
| **API oficial de Meta** | Cuando tengas el negocio verificado | Estable, sin riesgo de bloqueo por el canal | Meta pide documentos del negocio para el token permanente |

### 5.0 WhatsApp Web (codigo QR)

1. En el panel: **Conexiones > WhatsApp** y elige **WhatsApp Web (codigo QR)**.
2. Pulsa **Vincular telefono**: aparece un codigo QR que se renueva solo.
3. En el telefono: **WhatsApp > Ajustes > Dispositivos vinculados > Vincular un dispositivo** y escanea el codigo.
   (Alternativa: abre "Vincular con un codigo", escribe el numero con el codigo del pais, por ejemplo `573001234567`,
   y en el telefono elige **Vincular con el numero de telefono** e ingresa el codigo de 8 caracteres.)
4. Cuando el estado diga **Conectado**, la IA ya recibe y responde los mensajes de ese numero. Aparece en el telefono
   como el dispositivo "Dropshipper Motor".

Notas importantes:
- La sesion se guarda en la carpeta de datos del Motor (`whatsapp_web.db`): no hace falta volver a escanear al reiniciar.
  Haz copia de seguridad de esa carpeta como de cualquier dato sensible: quien la tenga controla el WhatsApp vinculado.
- Si el telefono pasa unos 14 dias sin conexion, o cierras la sesion desde el telefono, el estado pasa a
  **Sesion cerrada** y hay que vincular de nuevo.
- Solo se atienden chats individuales (se ignoran grupos, estados y canales). Los comprobantes de pago (imagen o PDF)
  se guardan igual que con la API oficial.
- **Para reducir el riesgo de bloqueo:** usa un numero dedicado al negocio, responde solo a quien te escribe primero,
  no hagas envios masivos ni escribas a desconocidos, y evita enviar muchos mensajes seguidos.
- No necesitas direccion publica ni webhooks para este canal. Si luego pasas a la API oficial, sigue los pasos 5.1 a 5.4.

## 5.1 a 5.4 API oficial de Meta

> Usa un numero **dedicado**. Ese numero **no puede** estar al mismo tiempo en la app normal de WhatsApp o
> WhatsApp Business. Si ya lo tienes instalado ahi, primero borra la cuenta de esa app.

### 5.1 Direccion publica con ngrok (para recibir mensajes)

Meta y Wompi necesitan una URL **https** publica para avisarle al Motor. La direccion `http://localhost:8080/...` que
ves en el panel **solo existe en tu computador**: Meta no puede llegar a ella. Con ngrok el Motor abre el tunel por ti,
**sin terminal**:

1. Crea una cuenta gratis en <https://dashboard.ngrok.com> y copia tu **Authtoken** (seccion *Your Authtoken*).
2. Recomendado: en ngrok > **Domains** reclama tu dominio gratis (algo como `mi-tienda.ngrok-free.app`). Asi la
   direccion **no cambia** al reiniciar y no tienes que tocar Meta de nuevo.
3. En el panel: **Conexiones > Direccion publica (ngrok)**. Pega el Authtoken (sirve tambien el comando completo
   `ngrok config add-authtoken ...`), escribe el dominio si lo reclamaste y pulsa **Guardar y activar**.
4. Cuando el estado diga **Conectado** el panel muestra las direcciones exactas para copiar:
   `https://TU-DOMINIO/webhook/whatsapp` (Meta) y `https://TU-DOMINIO/webhook/wompi` (Wompi). El bloque de WhatsApp
   tambien cambia solo a esa direccion.

Cosas utiles:
- **Tu panel no queda expuesto.** Por el tunel solo pasan `/webhook/*` y `/salud`; todo lo demas responde 404 desde
  internet. El panel sigue abriendose solo desde tu computador (o con tu token de acceso).
- Si ya tenias `ngrok` abierto en una terminal con la misma cuenta, puede que la cuenta gratis no deje una segunda
  sesion o que el dominio ya este en uso: el panel te lo dice. Cierra ese ngrok y pulsa **Reintentar**.
- Sin dominio fijo, ngrok da una direccion al azar que **cambia cada vez que reinicias el Motor** y habria que
  actualizarla en Meta. Por eso conviene el dominio fijo.
- **Apagar tunel** lo detiene pero conserva el token; **Quitar token** lo borra.
- El tunel solo funciona mientras el Motor esta encendido. Para vender 24/7 el Motor debe estar siempre prendido
  (por ejemplo en un servidor); ahi ya no necesitas ngrok y puedes guardar la direccion en **Seguridad y acceso**.
- Si prefieres otro tunel (Cloudflare Tunnel, tu propio dominio), pega su direccion en **Conexiones > Seguridad y
  acceso > Direccion publica**. El tunel de ngrok, cuando esta conectado, tiene prioridad sobre esa direccion.
- El Authtoken se guarda en la base de datos del Motor y nunca se vuelve a mostrar completo en el panel.

### 5.2 Crear la app en Meta

1. <https://developers.facebook.com/apps> > **Crear app** > tipo **Empresa (Business)**.
2. Agrega el producto **WhatsApp** y vincula tu cuenta de Meta Business.
3. En **WhatsApp > Configuracion de la API**: agrega tu numero dedicado, verificalo y copia el **ID del numero de telefono**.
4. Crea un **token permanente**: Meta Business Suite > Configuracion del negocio > **Usuarios del sistema** > crea un
   administrador > **Generar token** con los permisos `whatsapp_business_messaging` y `whatsapp_business_management`.
   (El token temporal de la consola dura 24 h y no sirve para produccion.)
5. En **Configuracion de la app > Basica** copia el **Secreto de la app**.

### 5.3 Pegar los datos en el Motor

**En el panel:** Conexiones > **WhatsApp** > **API oficial de Meta**. Completa el token, el ID del numero y el secreto de la app.
En **Token de verificacion** pulsa **Generar** (o inventa uno). Pulsa **Probar conexion** y luego **Guardar**.

### 5.4 Registrar el webhook en Meta

En **WhatsApp > Configuracion > Webhook**:
- **URL de devolucion de llamada:** la que muestra el panel (`https://TU_DIRECCION/webhook/whatsapp`).
- **Token de verificacion:** el mismo que guardaste en el Motor.
- Pulsa **Verificar y guardar** y suscribete al campo **messages**.

Escribe desde otro celular a tu numero dedicado: la vendedora debe responder y el chat aparece en **Conversaciones**.
No necesitas reiniciar el Motor en ningun paso.

**Verificacion del negocio:** para superar los limites iniciales de mensajes, Meta pide verificar tu negocio
(documentos) en Meta Business Suite. Las respuestas a clientes que te escribieron en las ultimas 24 h no requieren plantillas.

---

## 6. Seguridad y acceso desde otros equipos

**En el panel:** Conexiones > **Seguridad y acceso** > **Generar token de acceso**. El token se muestra una sola vez:
guardalo. Este navegador ya queda con la sesion iniciada; en otros dispositivos pega el token en la pantalla de acceso.

Antes de publicar el Motor en internet, **define siempre un token**.

---

## 7. Tu dia a dia

| Pantalla | Para que |
|---|---|
| **Inicio** | Ventas, pendientes, alertas y la lista de puesta en marcha |
| **Aprobaciones** | **Aprobar** pedidos, **confirmar pagos**, **copiar datos para Dropi** y **registrar la guia** |
| **Conversaciones** | Ver los chats en vivo. **Tomar el chat** pausa la IA; **Que hizo la IA** muestra cada decision |
| **Pedidos** | Historial y cambio de estado (en transito, entregado, novedad, devuelto) |
| **Simulador** | Probar la vendedora sin usar WhatsApp |
| **Productos** | Lista de productos; ficha, ofertas y finanzas de cada uno; consulta de zonas de envio; boton **Traer costos de Dropi** |
| **Envios** | Transportadoras (con notas para la IA), **cotizador** de fletes y **simulacion de pedido en Dropi** |
| **Conexiones** | IA, **Dropi**, WhatsApp, cobros y seguridad, con pruebas de conexion |
| **Ajustes** | Persona de la IA, autonomia, supuestos financieros, ritmo de la conversacion y avisos |

El boton **Tema** (abajo a la izquierda) alterna entre claro, oscuro y automatico (sigue al sistema); se recuerda en cada navegador.

**Flujo de un pedido (Dropi todavia es manual):**
1. La vendedora conversa, toma los datos y el cliente confirma el resumen.
2. Te llega **Pedido por aprobar**: revisas y pulsas **Aprobar**. El cliente recibe la confirmacion.
3. **Copiar datos para Dropi**: creas la orden en Dropi y pegas los datos.
4. **Registrar guia**: el cliente recibe la guia y el valor a pagar al recibir.

**Cuando la IA te necesita:** si el cliente pide hablar con una persona, quiere una llamada, tiene un reclamo o hay un
tema de salud delicado, la IA **se pausa sola**, avisa al cliente y te deja una alerta. Si escribes en un chat desde el
panel, la IA tambien se pausa en ese chat; para devolverlo usa **Devolver a la IA**.

### Ajustes del negocio

En **Ajustes** cambias, sin reiniciar:

- **Persona**: nombre, rol y tono de la vendedora.
- **Autonomia**: si la IA aprueba sola los pedidos de bajo riesgo y con que limites. Por defecto, tu apruebas todo.
- **Supuestos financieros** (en %): devoluciones esperadas, pedidos que se confirman y margen de seguridad.
- **Conversacion**: la IA espera **60 segundos** (1 minuto) desde el ultimo mensaje del cliente antes de contestar,
  para que pueda escribir varios mensajes seguidos y recibir una sola respuesta. Puedes cambiar ese tiempo
  (0 = responde enseguida), la pausa entre mensajes y la memoria.
- **Avisos**: tu WhatsApp para recibir avisos de pedidos nuevos.

### Que hace la IA con los datos del cliente y los pedidos

- **Revisa el formato**: si el cliente escribe un correo como `samuelarrobagmail.com`, o un celular, nombre o ciudad
  dudosos, la IA **no los guarda tal cual**: le pregunta si quiso decir `samuel@gmail.com` y espera su confirmacion.
- **No duplica pedidos**: si el cliente ya tiene un pedido aprobado o en camino y vuelve a escribir "si" o "mandame la
  guia", la IA le explica el estado (por ejemplo, que la guia llega cuando tu la registres). Solo crea un segundo
  pedido cuando el cliente confirma de forma explicita que quiere **otro**.

---

## 8. Reglas de zona (contraentrega vs. anticipo)

Archivo: `motor/knowledge/zonas.yaml`. Prioridad: **barrio > municipio > departamento > por defecto**.

| Nivel | Pago por defecto |
|---|---|
| `normal` | Contraentrega |
| `lejano` | Anticipo del 20 % (minimo $20.000) |
| `dificil_acceso` | Anticipo del 50 % (minimo $30.000) |
| `zona_roja` | 100 % anticipado |

Un departamento que no aparece **nunca** queda en contraentrega: se usa `por_defecto: lejano`. Agrega barrios
problematicos con tu experiencia real, por ejemplo `"medellin|nombre del barrio": zona_roja`. Despues de editar,
reinicia el Motor. Para comprobar que haria el Motor con un destino, usa **Productos > Consultar zona de envio**.

---

## 9. Lo que falta conectar (siguientes fases)

- **Guias y estados de Dropi solos**: hoy **Aprobar** ya crea la orden en Dropi, pero la guia y los cambios de estado
  (en transito, entregado, novedad, devuelto) todavia se registran a mano. Falta leerlos de Dropi (webhook o consulta).
- **DropKiller (MCP)**: requiere el plan **Advanced**.
- **Kalodata**: tiene API abierta (solo plan Enterprise, unos US$570/mes); no hay MCP oficial. Ver `docs/PROPUESTA.md`.
- **Meta Ads, landings y Cazador de productos**: fases 3 a 6 de la propuesta.

## Dropi y transportadoras

Todo esto se hace desde el panel; no hay que usar la terminal.

### Conectar Dropi

1. En Dropi entra a **Configuracion > Integraciones**, crea una integracion y copia el **token**.
2. En el panel abre **Conexiones > Dropi** (es la tarjeta debajo de Inteligencia artificial).
3. Pega el token y escribe el **departamento y la ciudad de origen** (desde donde sale tu mercancia).
4. Deja **sin marcar** el entorno de pruebas: el de pruebas de Dropi **no tiene transportadoras**, sirve solo para
   validar el token pero no para cotizar fletes.
5. Pulsa **Probar conexion** y luego **Guardar Dropi**. Con la conexion lista puedes activar
   **Crear los pedidos aprobados directamente en Dropi**; si lo dejas apagado, el Motor te entrega los datos para
   copiar y tu registras la guia a mano.

Para desconectar, pulsa **Desconectar** en la misma tarjeta. Los fletes vuelven a ser los estimados.

### Aprobar crea la orden en Dropi

Con Dropi conectado (entorno real, no el de pruebas) y **Crear los pedidos aprobados directamente en Dropi** encendido:

1. Pulsas **Aprobar**: el cliente recibe su confirmacion y el Motor crea la orden en Dropi con la transportadora elegida.
2. Si Dropi ya devuelve la guia, se registra sola y el cliente la recibe por WhatsApp. Si no, el pedido queda en
   **Falta la guia**: cuando salga, usa **Registrar guia**.
3. Si Dropi falla, **el pedido sigue aprobado** (el cliente ya fue avisado), aparece una alerta y un boton **Crear en Dropi**
   para reintentar. Un pedido **nunca se crea dos veces** en Dropi. Si Dropi tarda en responder, el mensaje te pide revisar
   en Dropi antes de reintentar, porque pudo haberlo creado.
4. Antes de llamar a Dropi se revisa que el producto tenga **ID de Dropi** y que el pedido tenga todos los datos; si falta algo, te dice que.

> Los nombres de los campos de la orden siguen la documentacion de Dropi pero **no estan verificados contra una cuenta real**.
> Haz el primer pedido real con un producto barato y revisa en Dropi que llegue bien (nombre, direccion, recaudo, transportadora).

### Que transportadora se recomienda (y en cuantos dias llega)

En **Envios > Como se recomienda la transportadora** defines:

- **Plazo maximo de entrega** (dias). Nunca se recomienda una transportadora mas lenta que tu plazo si otra si llega a tiempo.
  Si ninguna llega, se recomienda la mas rapida y se te avisa.
- **Criterio** entre las que llegan a tiempo: *mas rentable*, *mas rapida* o *mas barata*. Con rapida o barata, nunca se
  recomienda una que te haga perder dinero habiendo otra que no.

Los **dias de entrega** vienen de Dropi cuando la cotizacion los incluye. Si no, se usan los **tiempos tipicos** que tu escribes
por transportadora (ciudades y zonas lejanas) y se muestran con **~** (por ejemplo "~3 dias"). Los valores por defecto son
aproximados: ajustalos con lo que veas en tus envios. En cada pedido puedes **elegir otra transportadora** (se recalcula la
ganancia) y tu eleccion se conserva si luego editas el pedido.

### Costos de los productos y "Costo sin confirmar"

Un producto muestra **Costo sin confirmar** cuando su costo lo escribiste tu como estimado. Se confirma de dos formas:

- Marcando **Costo confirmado** en **Productos > Ofertas y costos**, o
- Escribiendo el **ID de Dropi** del producto y pulsando **Traer costos de Dropi** (copia el costo real y lo marca
  como confirmado).

Conectar Dropi por si solo **no** confirma los costos.

### Transportadoras

En **Envios > Transportadoras** estan las diez disponibles en Dropi Colombia: Veloces, Envia, Interrapidisimo,
Domina, Coordinadora, 99minutos, TCC, Wiilog, Jamv-drive y Derocha-express. Para cada una puedes:

- **Activarla o apagarla**: el Motor solo cotiza las activas.
- Escribir **notas para la IA** (cobertura, tiempos, si recoge efectivo, experiencias): la IA las lee al conversar.
- Indicar su **tasa de devolucion** propia (0 = usa la general) y su **prioridad** (la menor gana si empatan).

El Motor compara las activas con la formula de ganancia (**ganancia si entrega**, **perdida si devuelve** y
**ganancia esperada**) y elige la de mayor ganancia esperada. En **Aprobaciones** cada pedido muestra la
transportadora elegida, si el flete es real de Dropi o estimado, y la comparacion con las demas.

### Probar sin crear nada en Dropi

En **Envios > Cotizar y simular un pedido** elige un producto, una oferta y un destino:

- **Cotizar** compara las transportadoras.
- **Simular pedido en Dropi** ademas verifica la conexion, el flete cotizado frente al estimado, el costo frente al
  de Dropi, si el costo esta confirmado, la rentabilidad y el codigo DANE del municipio, y muestra el pedido que se
  enviaria. **Nunca crea nada en Dropi.**

> Nota: las respuestas de la API de Dropi se interpretan de forma tolerante, pero no se han verificado contra una
> cuenta real. Si algun dato no aparece, la simulacion lo marca como aviso en lugar de fallar.

### Datos del cliente

La IA pide **correo electronico**, no cedula.


## Cazador de productos (gratis, manual)

Menú **Cazador**. Sirve para decidir qué producto probar antes de gastar en anuncios.

1. Instala la extensión gratuita de DropKiller en Chrome (Chrome Web Store, busca "Dropkiller") e inicia sesión con una cuenta gratis.
2. En el catálogo de Dropi la extensión muestra bajo cada producto las ventas de los últimos 7 días.
3. En el Motor: **Agregar producto** y escribe nombre, costo, precio de venta, flete y lo que veas (stock, ventas, vendedores que lo anuncian). Lo que no sepas se deja vacío.
4. El Motor responde **Vale la pena probarlo / Dudoso / No lo recomiendo** y explica cada punto: ganancia, demanda, stock, competencia. Nunca dice "bueno" si faltan datos.
5. Con Dropi conectado, **Traer de Dropi** copia el costo y el stock reales usando el ID del producto.
6. **Crear ficha de producto** crea el producto APAGADO en Productos; lo completas y lo enciendes tú.

Las reglas se cambian en la misma pantalla (**Mis reglas para elegir productos**): ventas mínimas en 7 días, mínimo para anuncios por pedido y máximo de vendedores anunciándolo. Las devoluciones esperadas y el margen de seguridad salen de **Ajustes**.
