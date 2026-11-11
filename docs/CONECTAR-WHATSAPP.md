# Conectar WhatsApp con un solo botón

El Motor hace solo los dos pasos en los que todos se atascan dentro de Meta:

1. **Registrar el webhook** de la app (URL + token + campo `messages`).
2. **Suscribir la app a la cuenta de WhatsApp Business (WABA)**. Sin esto Meta
   recibe los mensajes del cliente pero **nunca los reenvía**: el panel de Meta
   se ve perfecto, el botón "Probar" funciona y la IA no contesta nada.

## Lo que se necesita (una sola vez)

| Dato | Dónde está en Meta |
|---|---|
| Token de acceso permanente | WhatsApp > Configuración de la API > token de usuario del sistema |
| ID del número de teléfono | la misma pantalla (ID numérico, no el número) |
| Clave secreta de la app (32 caracteres) | Configuración de la app > Información básica > Mostrar |
| ID de la cuenta (WABA) | WhatsApp > Configuración de la API > «Identificador de la cuenta de WhatsApp Business» |
| ID de la app | Información básica (si se deja vacío, el Motor lo detecta con el token) |

Además:

- **Una dirección pública** que Meta pueda alcanzar: el túnel de ngrok integrado
  (Conexiones > Túnel público) o una dirección propia en Seguridad y acceso.
- **App en modo Activo**, con URL de política de privacidad, de términos y de
  eliminación de datos. El Motor sirve las tres en
  `/privacidad.html`, `/terminos.html` y `/eliminar-datos.html`.

## Los dos caminos

**Panel web:** Conexiones > WhatsApp > API oficial de Meta > pegar las
credenciales > **Conectar automáticamente**. Sale un informe con un renglón por
paso; si algo falla dice cuál y cómo se arregla.

**Terminal:**

```
motor whatsapp configurar --token EAAG... - --telefono-id 1302... --app-secret ... - --cuenta 9189...
motor whatsapp conectar
motor whatsapp probar
```

## Diagnóstico rápido

```
GET /api/whatsapp/estado
```

Devuelve `webhook_ok`, `app_suscrita` y la lista de apps suscritas. Si
`app_suscrita` es `false`, Meta no entregará ni un mensaje.

| Síntoma | Causa |
|---|---|
| Meta manda el webhook y el Motor contesta 401 | la clave secreta de la app no es la real (32 caracteres hex) |
| «Probar» llega, los mensajes reales no | la app no está suscrita a la WABA |
| Nada llega en modo Desarrollo | Meta solo entrega mensajes de números autorizados |
| `verificación fallida` (403) al guardar en Meta | el token de verificación no coincide |
| La URL guardada en Meta no es la del túnel | se registró una dirección vieja |

## Pasarle el Motor a otra persona

Tres formas, de la más simple a la más completa:

1. **Canal WhatsApp Web (QR).** Sin app de Meta, sin tokens: escanea el código
   QR desde el panel y listo. Es lo más fácil para alguien no técnico, pero es
   un canal no oficial: Meta puede bloquear el número. Úsalo para probar.
2. **Cada negocio con su propia app de Meta.** Es lo correcto con la API
   oficial. Son 5 valores y un botón: el Motor ya hace el resto. Deja la app en
   modo Activo y publica las tres páginas legales que el propio Motor sirve.
3. **Tú como socio (varios clientes con tu app).** Requiere Embedded Signup y
   alta de cada negocio como cliente de tu app; el secreto de la app no se
   comparte y cada WABA se suscribe por separado. Es trabajo de producto, no de
   configuración: no lo intentes copiando tu secreto a otros equipos.
