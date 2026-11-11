// Dropshipper Motor · panel del dueño. Sin dependencias, sin emojis: iconos SVG en línea.
'use strict';

const S = {
  token: localStorage.getItem('ds_token') || '',
  view: 'inicio',
  status: null,
  contacts: [],
  chatPhone: null,
  chatLastId: 0,
  simPhone: localStorage.getItem('ds_sim') || '',
  simLastId: 0,
  typing: {},
  proofs: 0,
};

// ---------- helpers ----------
const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const cop = (n) => '$' + Math.round(Number(n) || 0).toLocaleString('es-CO');
const usd = (n) => 'US$' + (Number(n) || 0).toFixed(2);
const pct = (f) => Math.round((Number(f) || 0) * 1000) / 10 + '%';

// Iconos estilo Feather (trazo). Se usan con ic('nombre') o <i data-ic="nombre"></i>.
const ICONS = {
  box: '<path d="M21 8 12 3 3 8v8l9 5 9-5z"/><path d="m3 8 9 5 9-5M12 13v8"/>',
  home: '<path d="m3 11 9-8 9 8"/><path d="M5 10v10h14V10"/>',
  'check-circle': '<circle cx="12" cy="12" r="9"/><path d="m8 12 3 3 5-6"/>',
  check: '<path d="m5 12 5 5L20 7"/>',
  message: '<path d="M21 12a8 8 0 0 1-11.6 7.1L4 20l1-4.6A8 8 0 1 1 21 12z"/>',
  package: '<path d="M16.5 9.4 7.5 4.2"/><path d="M21 8 12 3 3 8v8l9 5 9-5z"/><path d="m3.3 7 8.7 5 8.7-5M12 22V12"/>',
  flask: '<path d="M9 3h6M10 3v6L4.5 19a2 2 0 0 0 1.8 3h11.4a2 2 0 0 0 1.8-3L14 9V3"/><path d="M7.5 15h9"/>',
  tag: '<path d="M20 12 12 20a2 2 0 0 1-2.8 0L3 13.8V4h9.8L20 11.2a1.4 1.4 0 0 1 0 1z"/><circle cx="7.5" cy="8.5" r="1.2"/>',
  plug: '<path d="M9 2v6M15 2v6M6 8h12v4a6 6 0 0 1-12 0z"/><path d="M12 18v4"/>',
  sliders: '<path d="M4 6h10M18 6h2M4 12h4M12 12h8M4 18h12M20 18h0"/><circle cx="16" cy="6" r="2"/><circle cx="10" cy="12" r="2"/><circle cx="18" cy="18" r="2"/>',
  terminal: '<path d="m4 17 6-5-6-5M12 19h8"/>',
  image: '<rect x="3" y="4" width="18" height="16" rx="2"/><circle cx="9" cy="10" r="1.6"/><path d="m21 16-5-5-9 9"/>',
  bell: '<path d="M18 16V11a6 6 0 0 0-12 0v5l-2 2h16z"/><path d="M10 21h4"/>',
  user: '<circle cx="12" cy="8" r="4"/><path d="M4 21a8 8 0 0 1 16 0"/>',
  hand: '<path d="M8 13V5.5a1.5 1.5 0 0 1 3 0V11M11 10.5V4a1.5 1.5 0 0 1 3 0v7M14 11V5.5a1.5 1.5 0 0 1 3 0V14a7 7 0 0 1-7 7c-3 0-4.5-1.5-6.2-4.4L3 15a1.6 1.6 0 0 1 2.7-1.7L8 15.5"/>',
  bot: '<rect x="4" y="8" width="16" height="12" rx="3"/><path d="M12 4v4M9 14h.01M15 14h.01M2 14h2M20 14h2"/>',
  wallet: '<path d="M3 7a2 2 0 0 1 2-2h13v4"/><path d="M3 7v11a2 2 0 0 0 2 2h14a1 1 0 0 0 1-1V9a1 1 0 0 0-1-1H5a2 2 0 0 1-2-1z"/><path d="M16 14h.01"/>',
  clock: '<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
  truck: '<path d="M2 6h11v10H2zM13 9h4l4 4v3h-8"/><circle cx="6.5" cy="18" r="1.8"/><circle cx="17" cy="18" r="1.8"/>',
  copy: '<rect x="9" y="9" width="12" height="12" rx="2"/><path d="M5 15V5a2 2 0 0 1 2-2h8"/>',
  edit: '<path d="M4 20h4L19 9a2.8 2.8 0 0 0-4-4L4 16z"/><path d="m13.5 6.5 4 4"/>',
  x: '<path d="M6 6l12 12M18 6 6 18"/>',
  alert: '<path d="M10.3 3.9 2.4 18a2 2 0 0 0 1.7 3h15.8a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z"/><path d="M12 9v4M12 17h.01"/>',
  info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  trash: '<path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/>',
  key: '<circle cx="8" cy="15" r="4"/><path d="m11 12 9-9M16 7l3 3M14 9l2 2"/>',
  link: '<path d="M10 14a4.5 4.5 0 0 0 6.4 0l3-3a4.5 4.5 0 0 0-6.4-6.4l-1 1"/><path d="M14 10a4.5 4.5 0 0 0-6.4 0l-3 3a4.5 4.5 0 0 0 6.4 6.4l1-1"/>',
  refresh: '<path d="M20 11a8 8 0 0 0-14-4L4 9M4 4v5h5M4 13a8 8 0 0 0 14 4l2-2M20 20v-5h-5"/>',
  shield: '<path d="M12 3 4 6v6c0 5 3.5 8 8 9 4.5-1 8-4 8-9V6z"/><path d="m9 12 2 2 4-4"/>',
  back: '<path d="M19 12H5M11 6l-6 6 6 6"/>',
  chevron: '<path d="m9 6 6 6-6 6"/>',
  eye: '<path d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>',
  dollar: '<path d="M12 3v18M16.5 7.5C15.5 6.4 14 6 12 6c-2.4 0-4 1.1-4 3s1.6 2.6 4 3 4 1.1 4 3-1.6 3-4 3c-2 0-3.6-.5-4.6-1.7"/>',
  lock: '<rect x="5" y="11" width="14" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/>',
  search: '<circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/>',
  send: '<path d="m21 3-9.5 18-2.5-8-8-2.5z"/><path d="M21 3 9 13"/>',
  brain: '<path d="M9 4a3 3 0 0 0-3 3 3 3 0 0 0-2 5 3.5 3.5 0 0 0 3 5.5A3 3 0 0 0 12 19V5a3 3 0 0 0-3-1z"/><path d="M15 4a3 3 0 0 1 3 3 3 3 0 0 1 2 5 3.5 3.5 0 0 1-3 5.5A3 3 0 0 1 12 19"/>',
  list: '<path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/>',
  chat: '<path d="M4 5h16v11H9l-5 4z"/>',
  cpu: '<rect x="6" y="6" width="12" height="12" rx="2"/><path d="M9 2v4M15 2v4M9 18v4M15 18v4M2 9h4M2 15h4M18 9h4M18 15h4"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/>',
  moon: '<path d="M20 14.5A8 8 0 0 1 9.5 4 8 8 0 1 0 20 14.5z"/>',
  monitor: '<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/>',
  database: '<ellipse cx="12" cy="6" rx="8" ry="3"/><path d="M4 6v12c0 1.7 3.6 3 8 3s8-1.3 8-3V6M4 12c0 1.7 3.6 3 8 3s8-1.3 8-3"/>',
};
const ic = (name, size) => `<svg class="ic" viewBox="0 0 24 24" aria-hidden="true"${size ? ` style="width:${size}px;height:${size}px"` : ''}>${ICONS[name] || ICONS.info}</svg>`;
function hydrateIcons(root = document) {
  $$('i[data-ic]', root).forEach((el) => { el.outerHTML = ic(el.dataset.ic); });
}

function fmtTime(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  const same = d.toDateString() === new Date().toDateString();
  const hm = d.toLocaleTimeString('es-CO', { hour: '2-digit', minute: '2-digit' });
  return same ? hm : d.toLocaleDateString('es-CO', { day: '2-digit', month: 'short' }) + ' ' + hm;
}

// Formato WhatsApp sobre texto escapado: *negrita*, _cursiva_, enlaces.
function waFormat(text) {
  let h = esc(text);
  h = h.replace(/\*([^*\n]+)\*/g, '<b>$1</b>').replace(/(^|\s)_([^_\n]+)_/g, '$1<i>$2</i>');
  h = h.replace(/(https?:\/\/[^\s<]+)/g, '<a href="$1" target="_blank" rel="noopener">$1</a>');
  return h;
}

function toast(msg, bad = false) {
  const t = $('#toast');
  t.textContent = msg;
  t.className = 'toast' + (bad ? ' bad' : '');
  clearTimeout(toast._t);
  toast._t = setTimeout(() => t.classList.add('hidden'), bad ? 6000 : 3500);
}

class ApiError extends Error {}

async function api(path, opts = {}) {
  const headers = { 'Content-Type': 'application/json' };
  if (S.token) headers.Authorization = 'Bearer ' + S.token;
  let res;
  try {
    res = await fetch(path, {
      ...opts, headers: { ...headers, ...(opts.headers || {}) },
      body: opts.body && typeof opts.body !== 'string' ? JSON.stringify(opts.body) : opts.body,
    });
  } catch (_) {
    throw new ApiError('No se pudo contactar con el Motor. Verifica que siga encendido.');
  }
  let data = null;
  try { data = await res.json(); } catch (_) { /* cuerpo vacío */ }
  if (res.status === 401) {
    showLogin(data && data.sin_token ? 'sin_token' : (S.token ? 'invalido' : 'pedir'), data && data.error);
    throw new ApiError((data && data.error) || 'Token inválido');
  }
  if (!res.ok) throw new ApiError((data && data.error) || ('Error ' + res.status));
  return data;
}
const post = (path, body) => api(path, { method: 'POST', body: body || {} });
const put = (path, body) => api(path, { method: 'PUT', body: body || {} });
const del = (path) => api(path, { method: 'DELETE' });

const mediaURL = (file) => '/media/' + encodeURIComponent(file) + (S.token ? '?token=' + encodeURIComponent(S.token) : '');

const STATUS = {
  borrador: ['Borrador', ''], esperando_pago: ['Esperando pago', 'warn'], por_aprobar: ['Por aprobar', 'brand'],
  aprobado: ['Aprobado', 'info'], en_dropi: ['En Dropi', 'info'], en_transito: ['En tránsito', 'info'],
  entregado: ['Entregado', 'ok'], novedad: ['Novedad', 'warn'], devuelto: ['Devuelto', 'bad'],
  rechazado: ['Rechazado', 'bad'], cancelado: ['Cancelado', 'bad'],
};
const tag = (map, k) => { if (!k) return ''; const [l, c] = map[k] || [k, '']; return `<span class="tag ${c}">${esc(l)}</span>`; };
const statusTag = (s) => tag(STATUS, s);
const PAY = { contraentrega: ['Contraentrega', 'ok'], anticipo_parcial: ['Anticipo parcial', 'warn'], anticipado_total: ['Pago anticipado', 'bad'] };
const payTag = (p) => tag(PAY, p);
const RISK = { normal: ['Riesgo normal', 'ok'], lejano: ['Zona lejana', 'warn'], dificil_acceso: ['Difícil acceso', 'warn'], zona_roja: ['Zona roja', 'bad'] };
const riskTag = (r) => tag(RISK, r);
const STAGE = { nuevo: 'Nuevo', datos: 'Tomando datos', esperando_pago: 'Esperando pago', cerrado: 'Venta cerrada', perdido: 'Perdido' };

const contactLabel = (p) => (p.startsWith('sim-') ? 'Prueba ' + p.slice(4) : '+' + p);
const contactName = (c) => c.name || contactLabel(c.phone);

// Bloques de interfaz reutilizables.
const notice = (kind, html) => `<div class="notice ${kind}">${ic({ ok: 'check-circle', warn: 'alert', bad: 'alert', info: 'info' }[kind] || 'info')}<div>${html}</div></div>`;
const fld = (name, label, value, o = {}) => `<label class="field${o.full ? ' full' : ''}"><span>${label}</span>
  <input name="${name}" type="${o.type || 'text'}" value="${esc(value ?? '')}"${o.ph ? ` placeholder="${esc(o.ph)}"` : ''}${o.ro ? ' readonly' : ''}${o.auto ? ` autocomplete="${o.auto}"` : ''}>${o.help ? `<small>${o.help}</small>` : ''}</label>`;
const formData = (form) => Object.fromEntries(new FormData(form).entries());

// Deshabilita un botón mientras corre una acción asíncrona.
async function busy(btn, fn) {
  const html = btn.innerHTML;
  btn.disabled = true;
  btn.innerHTML = '<span class="spin"></span> ' + btn.textContent.trim();
  try { return await fn(); } finally { btn.disabled = false; btn.innerHTML = html; }
}

// ---------- acceso ----------
function showLogin(mode, msg) {
  $('#app').classList.add('hidden');
  $('#login').classList.remove('hidden');
  const help = $('#loginHelp');
  const row = $('#loginTokenRow');
  if (mode === 'sin_token') {
    row.classList.add('hidden');
    help.innerHTML = 'Por seguridad, mientras no exista un token de acceso el panel solo se abre desde el mismo computador donde corre el Motor. Abre el panel desde ese computador (dirección http://localhost), entra a Conexiones → Seguridad y acceso, crea el token y úsalo aquí.';
  } else {
    row.classList.remove('hidden');
    help.textContent = mode === 'invalido' ? 'El token no es correcto. Inténtalo de nuevo.' : 'Escribe el token de administrador.';
  }
  $('#loginError').textContent = mode === 'invalido' ? (msg || '') : '';
  hydrateIcons($('#login'));
}
$('#loginForm').addEventListener('submit', async (e) => {
  e.preventDefault();
  S.token = $('#tokenInput').value.trim();
  localStorage.setItem('ds_token', S.token);
  $('#loginError').textContent = '';
  try { await boot(); } catch (err) { $('#loginError').textContent = err.message; }
});

// ---------- rutas ----------
const VIEWS = {
  inicio: () => renderInicio(),
  aprobaciones: () => renderAprobaciones(),
  chats: () => renderChats(),
  pedidos: () => renderPedidos(),
  simulador: () => window.renderSimulador(),
  productos: (arg) => window.renderProductos(arg),
  cazador: () => window.renderCazador(),
  envios: () => window.renderEnvios(),
  conexiones: () => window.renderConexiones(),
  ajustes: () => window.renderAjustes(),
};

function route() {
  const [view, ...rest] = location.hash.slice(1).split('/');
  return { view: VIEWS[view] ? view : 'inicio', arg: rest.join('/') };
}

function go() {
  const { view, arg } = route();
  S.view = view;
  S.arg = arg;
  $$('.view').forEach((v) => v.classList.toggle('active', v.id === 'view-' + view));
  $$('#nav a').forEach((a) => a.classList.toggle('active', a.dataset.view === view));
  Promise.resolve(VIEWS[view](arg)).catch((err) => toast(err.message, true));
}
window.addEventListener('hashchange', go);

// ---------- eventos en vivo ----------
let es = null;
function connectEvents() {
  if (es) es.close();
  es = new EventSource('/api/eventos' + (S.token ? '?token=' + encodeURIComponent(S.token) : ''));
  es.onopen = () => { $('#liveDot').classList.add('on'); $('#liveText').textContent = 'En vivo'; };
  es.onerror = () => { $('#liveDot').classList.remove('on'); $('#liveText').textContent = 'Sin conexión en vivo'; };
  es.onmessage = (m) => { try { onEvent(JSON.parse(m.data)); } catch (_) { /* ignorar */ } };
}

let refreshTimer = null;
function scheduleRefresh() {
  clearTimeout(refreshTimer);
  refreshTimer = setTimeout(() => {
    refreshBadges();
    const safe = (p) => Promise.resolve(p).catch(() => {});
    if (S.view === 'inicio') safe(renderInicio());
    if (S.view === 'aprobaciones') safe(renderAprobaciones());
    if (S.view === 'pedidos') safe(renderPedidos());
    if (S.view === 'chats') safe(loadContacts());
    if (S.view === 'simulador') safe(window.loadSimSide());
  }, 350);
}

function onEvent(ev) {
  if (ev.type === 'escribiendo') S.typing[ev.phone] = true;
  if (ev.type === 'listo') delete S.typing[ev.phone];
  if (ev.phone && ev.phone === S.chatPhone && S.view === 'chats') window.loadChatMessages();
  if (ev.phone && ev.phone === S.simPhone && S.view === 'simulador') window.loadSimMessages();
  if (ev.type === 'alerta' && ev.text) toast(ev.text);
  if (ev.type === 'whatsapp') { refreshBadges(); return; }
  if (ev.type !== 'escribiendo') scheduleRefresh();
}

async function refreshBadges() {
  try {
    const [st, es2] = await Promise.all([api('/api/stats'), api('/api/estado')]);
    S.stats = st; S.status = es2;
    $('#badgeAprob').textContent = (st.por_aprobar + S.proofs) || '';
    $('#badgeChats').textContent = S.contacts.reduce((n, c) => n + (c.unread || 0), 0) || '';
    $('#badgeConn').textContent = setupSteps(es2).filter((x) => !x.done && x.required).length || '';
  } catch (_) { /* sin conexión */ }
}

// ---------- INICIO ----------
function setupSteps(st) {
  const ia = st.ia || {}; const wa = st.whatsapp || {}; const pg = st.pagos || {}; const pr = st.productos || {};
  return [
    { done: ia.configurada, required: true, title: 'Conectar la inteligencia artificial',
      desc: ia.configurada ? `${ia.proveedor || 'Proveedor'} · modelo ${ia.modelo}` : 'Elige un proveedor (DeepSeek, OpenAI, Arxi u otro compatible), pega la clave y prueba la conexión.', go: 'conexiones' },
    { done: pr.activos > 0, required: true, title: 'Tener al menos un producto activo',
      desc: pr.activos > 0 ? `${pr.activos} activo(s) de ${pr.total}` : 'Crea o activa un producto con su costo, flete y ofertas.', go: 'productos' },
    { done: pr.activos > 0 && pr.costo_confirmado >= pr.activos, required: false, title: 'Confirmar costos con el proveedor',
      desc: 'Mientras el costo no esté confirmado, la rentabilidad mostrada es estimada.', go: 'productos' },
    { done: !!(st.dropi && st.dropi.conectado), required: false, title: 'Conectar Dropi (opcional)',
      desc: st.dropi && st.dropi.conectado ? 'Conectado: el Motor cotiza fletes reales y compara transportadoras' : 'Pega tu token de Dropi en Conexiones para cotizar fletes reales y traer los costos de tus productos.', go: 'conexiones' },
    { done: pg.listo, required: true, title: 'Definir cómo cobrar los anticipos',
      desc: pg.listo ? (pg.metodo === 'wompi' ? 'Wompi con confirmación automática' : 'Pago manual con comprobante') : 'Llave Bre-B o cuenta bancaria, o llaves de Wompi.', go: 'conexiones' },
    { done: wa.configurado, required: true, title: 'Conectar WhatsApp',
      desc: wa.configurado ? (wa.canal === 'web' ? 'Conectado por WhatsApp Web' + (wa.web && wa.web.numero ? ' (+' + wa.web.numero + ')' : '') : 'Conectado a la API oficial de Meta')
        : 'Sin esto solo funciona el Simulador. Vincula tu teléfono con un código QR (WhatsApp Web) o usa la API oficial de Meta.', go: 'conexiones' },
    { done: st.requiere_token, required: false, title: 'Proteger el panel con un token',
      desc: st.requiere_token ? 'El panel pide token' : 'Imprescindible antes de publicar el Motor en internet.', go: 'conexiones' },
  ];
}

async function renderInicio() {
  $('#today').textContent = new Date().toLocaleDateString('es-CO', { weekday: 'long', day: 'numeric', month: 'long' });
  const [st, alerts, contacts, estado] = await Promise.all([api('/api/stats'), api('/api/alertas'), api('/api/contactos'), api('/api/estado')]);
  S.contacts = contacts; S.status = estado;

  const steps = setupSteps(estado);
  const pending = steps.filter((s) => !s.done);
  const doneN = steps.length - pending.length;
  $('#setupBox').innerHTML = pending.length ? `<div class="card" style="margin-bottom:18px">
    <div class="card-head"><div><h3>Puesta en marcha</h3><div class="desc">${doneN} de ${steps.length} pasos completados. Todo se hace desde este panel.</div></div></div>
    <div class="progress"><i style="width:${Math.round(doneN / steps.length * 100)}%"></i></div>
    <div class="steps">${steps.map((s) => `<div class="step ${s.done ? 'done' : ''}">
      <span class="dot">${s.done ? ic('check') : ''}</span>
      <div class="txt"><b>${esc(s.title)}</b><span>${esc(s.desc)}</span></div>
      ${s.done ? '' : `<a class="btn small" href="#${s.go}">Configurar</a>`}</div>`).join('')}</div></div>` : '';

  const k = (label, value, hot) => `<div class="kpi ${hot ? 'hot' : ''}"><div class="l">${label}</div><div class="v">${value}</div></div>`;
  $('#kpis').innerHTML = [
    k('Ventas aprobadas hoy', cop(st.ventas_hoy)),
    k('Ventas del mes', cop(st.ventas_mes) + `<div class="small muted" style="font-weight:400">${st.pedidos_mes} pedidos</div>`),
    k('Por aprobar', st.por_aprobar, st.por_aprobar > 0),
    k('Esperando pago', st.esperando_pago, st.esperando_pago > 0),
    k('Chats hoy', st.chats_hoy),
    k('Activos en 24 h', st.chats_activos_24h),
    k('Mensajes de la IA hoy', st.mensajes_ia_hoy),
    k('Chats con IA en pausa', st.ia_pausada, st.ia_pausada > 0),
  ].join('');

  $('#alertList').innerHTML = alerts.length ? alerts.map((a) => `
    <div class="item" data-alert="${a.id}" data-phone="${esc(a.phone)}" data-kind="${esc(a.kind)}">
      <div class="row"><b>${ic(alertIcon(a.kind))} ${esc(alertLabel(a.kind))}</b><span class="spacer"></span><span class="small muted">${fmtTime(a.created_at)}</span></div>
      <div class="small" style="margin-top:3px">${esc(a.message)}</div>
      <div class="actions" style="margin-top:8px"><button class="btn small" data-open>Ver</button><button class="btn small ghost" data-resolve>Marcar como resuelto</button></div>
    </div>`).join('') : '<div class="empty">No hay nada pendiente.</div>';
  $$('#alertList .item').forEach((el) => {
    $('[data-resolve]', el).onclick = async () => { await post(`/api/alertas/${el.dataset.alert}/resolver`); renderInicio(); refreshBadges(); };
    $('[data-open]', el).onclick = () => {
      const kind = el.dataset.kind;
      if (kind === 'aprobacion' || kind === 'comprobante') location.hash = 'aprobaciones';
      else if (el.dataset.phone) openChat(el.dataset.phone);
    };
  });

  $('#recentChats').innerHTML = contacts.slice(0, 8).map(contactItem).join('') || '<div class="empty">Aún no hay conversaciones. Prueba el Simulador.</div>';
  $$('#recentChats .item').forEach((el) => { el.onclick = () => openChat(el.dataset.phone); });
  refreshBadges();
}

const alertIcon = (k) => ({ escalamiento: 'hand', aprobacion: 'package', comprobante: 'image', error: 'alert', info: 'info' }[k] || 'bell');
const alertLabel = (k) => ({ escalamiento: 'El cliente pide atención humana', aprobacion: 'Pedido por aprobar', comprobante: 'Comprobante de pago', error: 'Error', info: 'Aviso' }[k] || k);

function contactItem(c) {
  const flags = [c.ai_paused ? '<span class="tag warn">IA en pausa</span>' : '', c.simulated ? '<span class="tag">prueba</span>' : '',
    c.stage && c.stage !== 'nuevo' ? `<span class="tag info">${esc(STAGE[c.stage] || c.stage)}</span>` : ''].join(' ');
  return `<div class="item ${c.phone === S.chatPhone ? 'active' : ''}" data-phone="${esc(c.phone)}" style="cursor:pointer">
    <div class="row"><b>${esc(contactName(c))}</b><span class="spacer"></span><span class="small muted">${fmtTime(c.last_message_at)}</span></div>
    <div class="small muted" style="white-space:nowrap;overflow:hidden;text-overflow:ellipsis">${esc(c.last_preview || '')}</div>
    <div class="row gap small" style="margin-top:4px">${flags}${c.unread ? `<span class="badge" style="margin-left:auto">${c.unread}</span>` : ''}</div>
  </div>`;
}

// ---------- tarjetas de pedido ----------
function profitOf(o) { try { return JSON.parse(o.profit_json || '{}'); } catch (_) { return {}; } }

function orderKV(o) {
  const addr = [o.address, o.address_refs && '(' + o.address_refs + ')'].filter(Boolean).join(' ');
  return `<dl class="kv">
    <dt>Producto</dt><dd>${esc(o.product_name)} · ${esc(o.offer_label || 'sin oferta')}</dd>
    <dt>Cliente</dt><dd>${esc(o.customer_name || '-')}</dd>
    <dt>Correo</dt><dd>${esc(o.email || '-')}</dd>
    <dt>Celular</dt><dd>${esc(o.customer_phone || '-')}</dd>
    <dt>Dirección</dt><dd>${esc(addr || '-')}</dd>
    <dt>Barrio / ciudad</dt><dd>${esc(o.neighborhood || '-')} · ${esc(o.city || '-')}, ${esc(o.department || '-')}</dd>
    <dt>Transportadora</dt><dd>${esc(o.carrier || '-')} ${deliveryTag(o)} ${carrierSrc(o)}</dd>
    <dt>Regla de zona</dt><dd class="small">${esc(o.zone_reason || '-')}</dd>
    ${o.notes ? `<dt>Notas</dt><dd class="small">${esc(o.notes)}</dd>` : ''}
  </dl>`;
}

// De dónde sale el flete mostrado: Dropi (real) o el estimado del producto.
function carrierSrc(o) {
  const p = profitOf(o);
  if (p.fuente_flete === 'dropi') return '<span class="tag ok">Flete real de Dropi</span>';
  if (p.fuente_flete === 'estimado') return '<span class="tag warn">Flete estimado</span>';
  return '<span class="small muted">(sugerida)</span>';
}

// "Llega en ~3 días" (con aviso si el número es un estimado tuyo y no de Dropi).
const daysText = (d) => (d === 1 ? '1 día' : `${d} días`);
function deliveryTag(o) {
  const p = profitOf(o);
  if (!p.dias_entrega) return '';
  const est = p.fuente_dias === 'estimado';
  return `<span class="tag info" title="${est ? 'Tiempo típico que configuraste para esta transportadora (Envíos). Dropi no informó uno para esta ruta.' : 'Tiempo informado por Dropi para esta ruta.'}">Llega en ${est ? '~' : ''}${daysText(p.dias_entrega)}</span>`;
}

// Etiquetas de una opción del comparador.
function optionTags(t, recommendedId) {
  const tags = [];
  if (t.id === recommendedId) tags.push('<span class="tag brand">Recomendada</span>');
  if (t.mas_rapida) tags.push('<span class="tag ok">Más rápida</span>');
  if (t.mas_barata) tags.push('<span class="tag ok">Más barata</span>');
  if (t.dentro_del_limite === false) tags.push('<span class="tag bad">Fuera de tu plazo</span>');
  if (t.viable === false) tags.push('<span class="tag bad">No conviene</span>');
  return tags.join(' ');
}

const canPickCarrier = (o) => ['borrador', 'esperando_pago', 'por_aprobar', 'aprobado'].includes(o.status);

function carrierOptions(o) {
  const p = profitOf(o); const list = p.transportadoras || [];
  const warn = p.advertencia_transportadora ? `<div class="small" style="color:var(--warn)">${ic('alert', 13)} ${esc(p.advertencia_transportadora)}</div>` : '';
  if (!list.length) return warn;
  const manual = p.transportadora_manual || '';
  const pick = canPickCarrier(o) && list.length > 1 ? `<label class="field carrier-pick"><span>Transportadora de este pedido</span>
      <select data-carrier-order="${o.id}">
        <option value="" ${manual ? '' : 'selected'}>Automática${p.recomendada_id ? ' (la recomendada)' : ''}</option>
        ${list.map((t) => `<option value="${esc(t.id)}" ${manual === t.id ? 'selected' : ''}>${esc(t.transportadora)} · ${t.fuente_dias === 'estimado' ? '~' : ''}${daysText(t.dias)} · flete ${cop(t.flete)}${t.dentro_del_limite === false ? ' · fuera de plazo' : ''}</option>`).join('')}
      </select>
      <small>${manual ? 'La elegiste tú: el Motor ya no la cambia sola.' : 'El Motor recomienda según tu criterio y plazo (Envíos). Puedes cambiarla.'}</small></label>` : '';
  const limit = p.max_dias ? ` Tu plazo máximo es ${p.max_dias} días.` : '';
  return `${pick}<details class="carr-opts"><summary>${ic('truck', 13)} Comparar transportadoras (${list.length})</summary>
    <div class="table-wrap"><table class="qtable"><thead><tr><th>Transportadora</th><th>Llega en</th><th>Flete</th><th>Ganancia esperada</th></tr></thead><tbody>
    ${list.map((t) => `<tr class="${t.id === (manual || p.recomendada_id) ? 'best' : ''}"><td>${esc(t.transportadora)} ${optionTags(t, p.recomendada_id)}</td><td>${t.fuente_dias === 'estimado' ? '~' : ''}${daysText(t.dias)}</td><td>${cop(t.flete)}</td><td>${cop(t.ganancia_esperada)}</td></tr>`).join('')}</tbody></table></div>
    <div class="small muted" style="margin-top:6px">Los días marcados con ~ son tiempos típicos que tú configuras en Envíos (Dropi no los informó).${limit}</div>
    ${warn}</details>`;
}

// La cuenta del pedido, en lenguaje de todos los días: qué entra, qué sale y
// cuánto te queda (el flete de la transportadora ya está descontado).
function moneyBox(o) {
  const p = profitOf(o); const r = p.rentabilidad || {};
  const flete = p.flete || 0;
  const costo = p.costo_producto != null ? p.costo_producto : Math.max(0, (o.total || 0) - (r.ganancia_si_entrega || 0) - flete);
  const dev = Math.round((r.tasa_devolucion || 0) * 100);
  const warn = [];
  if (p.costo_confirmado === false) warn.push('Costo del producto sin confirmar: las cifras son estimadas.');
  if (p.fuente_flete === 'estimado') warn.push('El flete es un estimado (no es el precio real de la transportadora).');
  if (r.advertencia) warn.push(r.advertencia);
  const row = (label, value, opts = {}) => `<div class="cuenta-row ${opts.cls || ''}"><div class="l">${label}${opts.hint ? `<small>${opts.hint}</small>` : ''}</div><div class="v">${value}</div></div>`;
  const paga = o.advance_amount
    ? row('Ya pagó por adelantado', cop(o.advance_amount), { hint: 'Anticipo que el cliente ya te transfirió.' }) + row('Paga al recibir', cop(o.cod_amount), { hint: 'La transportadora lo cobra en la puerta y luego te lo gira.' })
    : row('Paga al recibir (contraentrega)', cop(o.cod_amount), { hint: 'La transportadora lo cobra en la puerta y luego te lo gira.' });
  return `<div class="cuenta">
      <div class="cuenta-title">Cuánto cobra el cliente</div>
      ${row('Total del pedido', cop(o.total), { cls: 'strong' })}${paga}
      <div class="cuenta-title">Cuánto te cuesta</div>
      ${row('Costo del producto (lo que cobra Dropi)', '- ' + cop(costo))}
      ${row('Envío por la transportadora' + (o.carrier ? ' (' + esc(o.carrier) + ')' : ''), '- ' + cop(flete), { hint: p.fuente_flete === 'dropi' ? 'Precio real cotizado por Dropi.' : 'Estimado: aún no es el precio real.' })}
      <div class="cuenta-title">Cuánto te queda</div>
      ${row('Ganancia si el cliente recibe', cop(r.ganancia_si_entrega), { cls: (r.ganancia_si_entrega || 0) >= 0 ? 'pos strong' : 'neg strong', hint: 'Total - producto - envío.' })}
      ${row('Si el cliente lo rechaza o no lo recibe', '- ' + cop(Math.abs(r.perdida_si_devuelve || 0)), { cls: 'neg', hint: 'Pagas el envío de ida y vuelta' + (o.advance_amount ? ' (el anticipo ya cobrado te compensa)' : '') + '.' })}
      ${row('Ganancia esperada', cop(r.ganancia_esperada), { cls: (r.ganancia_esperada || 0) >= 0 ? 'pos strong' : 'neg strong', hint: `Promedio contando que ~${dev} de cada 100 pedidos regresan.` })}
      ${row('Máximo para gastar en anuncios por este pedido', cop(p.cpa_objetivo), { hint: 'Si pagas más que esto por conseguir un pedido como este, dejas de ganar. Ya incluye tu margen de seguridad.' })}
    </div>${carrierOptions(o)}${warn.map((x) => `<div class="small" style="color:var(--warn)">${ic('alert', 13)} ${esc(x)}</div>`).join('')}`;
}

// Elegir transportadora a mano (delegado: funciona en tarjetas y en ventanas).
document.addEventListener('change', async (e) => {
  const sel = e.target.closest && e.target.closest('select[data-carrier-order]');
  if (!sel) return;
  sel.disabled = true;
  try {
    await post(`/api/pedidos/${sel.dataset.carrierOrder}/transportadora`, { transportadora: sel.value });
    toast(sel.value ? 'Transportadora cambiada. Ya se recalculó la ganancia.' : 'Vuelve la transportadora recomendada.');
    closeModal(); scheduleRefresh();
  } catch (err) { toast(err.message, true); sel.disabled = false; }
});

function orderCard(o, actions) {
  return `<div class="card ocard" data-order="${o.id}">
    <div class="head"><b>#${o.id} · ${esc(o.customer_name || contactLabel(o.phone))}</b>${statusTag(o.status)}</div>
    <div class="row gap wrap">${payTag(o.payment_policy)} ${riskTag(o.risk)} <span class="small muted">${fmtTime(o.created_at)}</span></div>
    ${orderKV(o)}${moneyBox(o)}
    <div class="actions">${actions}<button class="btn small ghost" data-chat>${ic('message', 14)} Ver chat</button></div>
  </div>`;
}

// ---------- APROBACIONES ----------
async function renderAprobaciones() {
  const [pending, waiting, approved, cfg] = await Promise.all([
    api('/api/pedidos?estado=por_aprobar'), api('/api/pedidos?estado=esperando_pago'), api('/api/pedidos?estado=aprobado,en_dropi'), api('/api/ajustes'),
  ]);
  const proofs = waiting.filter((o) => o.payment_status === 'comprobante_recibido');
  S.proofs = proofs.length;

  $('#approvalCards').innerHTML = pending.length ? pending.map((o) => orderCard(o,
    `<button class="btn ok" data-approve>${ic('check', 14)} Aprobar</button><button class="btn" data-edit>${ic('edit', 14)} Editar</button><button class="btn danger" data-reject>Rechazar</button>`)).join('')
    : '<div class="empty card">No hay pedidos por aprobar.</div>';

  const waitingAll = [...proofs, ...waiting.filter((o) => o.payment_status !== 'comprobante_recibido')];
  $('#paymentCards').innerHTML = waitingAll.length ? waitingAll.map((o) => {
    const proof = o.payment_status === 'comprobante_recibido';
    const img = o.payment_proof && /\.(jpe?g|png|webp)$/i.test(o.payment_proof) ? `<img class="proof" src="${mediaURL(o.payment_proof)}" alt="comprobante">` : '';
    const info = proof
      ? `<div style="width:100%">${notice('info', `El cliente envió un comprobante. <b>Verifica en tu banco</b> que llegaron ${cop(o.advance_amount)} (ref. ${esc(o.payment_ref)}) antes de confirmar.`)}${img}</div>`
      : `<div class="small muted" style="width:100%">Esperando que el cliente pague ${cop(o.advance_amount)} (ref. ${esc(o.payment_ref)}).${o.payment_link ? ` Enlace: <a href="${esc(o.payment_link)}" target="_blank" rel="noopener">${esc(o.payment_link)}</a>` : ''}</div>`;
    return orderCard(o, `${info}<button class="btn ok" data-payok>${ic('dollar', 14)} Pago recibido</button>${proof ? '<button class="btn danger" data-paybad>No llegó el pago</button>' : ''}<button class="btn" data-edit>${ic('edit', 14)} Editar</button><button class="btn danger" data-reject>Cancelar pedido</button>`);
  }).join('') : '<div class="empty card">No hay pagos pendientes.</div>';

  const dropiOn = !!(cfg.dropi && cfg.dropi.configurado && !cfg.dropi.sandbox && (cfg.negocio.dropi || {}).modo === 'api');
  $('#dispatchCards').innerHTML = approved.length ? approved.map((o) => {
    const inDropi = !!o.dropi_order_id;
    const create = !inDropi && dropiOn ? `<button class="btn primary" data-dropi-create>${ic('truck', 14)} Crear en Dropi</button>` : '';
    const copy = inDropi ? `<span class="tag ok">Orden en Dropi #${esc(o.dropi_order_id)}</span>` : `<button class="btn ${create ? '' : 'primary'}" data-copy>${ic('copy', 14)} Copiar datos para Dropi</button>`;
    return orderCard(o, `${create}${copy}<button class="btn ok" data-dispatch>${ic('truck', 14)} Registrar guía</button>${o.tracking_number ? `<span class="tag info">Guía ${esc(o.tracking_number)}</span>` : (inDropi ? '<span class="tag warn">Falta la guía</span>' : '')}`);
  }).join('')
    : '<div class="empty card">No hay pedidos aprobados pendientes de despacho.</div>';

  bindOrderButtons($('#view-aprobaciones'), [...pending, ...waiting, ...approved]);
  refreshBadges();
}

function bindOrderButtons(root, orders) {
  const byId = Object.fromEntries(orders.map((o) => [o.id, o]));
  $$('[data-order]', root).forEach((card) => {
    const o = byId[card.dataset.order];
    if (!o) return;
    const act = async (btn, fn, okMsg) => {
      btn.disabled = true;
      try { await fn(); toast(okMsg); scheduleRefresh(); } catch (err) { toast(err.message, true); btn.disabled = false; }
    };
    const on = (sel, fn) => { const b = $(sel, card); if (b) b.onclick = () => fn(b); };
    on('[data-approve]', async (b) => {
      b.disabled = true;
      try {
        const r = await post(`/api/pedidos/${o.id}/aprobar`); const d = r.dropi || {};
        if (d.estado === 'creado') toast(`Pedido #${o.id} aprobado y creado en Dropi. ${d.mensaje}`);
        else if (d.estado === 'error') toast(`Pedido #${o.id} aprobado, pero NO se creó en Dropi: ${d.mensaje}`, true);
        else toast(`Pedido #${o.id} aprobado. El cliente fue avisado. Crea la orden en Dropi con "Copiar datos para Dropi".`);
        scheduleRefresh();
      } catch (err) { toast(err.message, true); b.disabled = false; }
    });
    on('[data-dropi-create]', (b) => act(b, async () => { const r = await post(`/api/pedidos/${o.id}/crear-en-dropi`); if (r.dropi) toast(r.dropi.mensaje); }, `Pedido #${o.id} creado en Dropi.`));
    on('[data-payok]', (b) => { if (confirm(`¿Confirmas que recibiste ${cop(o.advance_amount)} en tu cuenta?`)) act(b, () => post(`/api/pedidos/${o.id}/pago-confirmado`), 'Pago confirmado.'); });
    on('[data-paybad]', (b) => act(b, () => post(`/api/pedidos/${o.id}/pago-rechazado`), 'Comprobante marcado como no válido. Escríbele al cliente.'));
    on('[data-reject]', (b) => { const m = prompt('Motivo (no se envía al cliente):', ''); if (m !== null) act(b, () => post(`/api/pedidos/${o.id}/rechazar`, { motivo: m }), 'Pedido rechazado.'); });
    on('[data-edit]', () => openEditOrder(o));
    on('[data-dispatch]', () => openDispatch(o));
    on('[data-copy]', () => copyForDropi(o));
    on('[data-chat]', () => openChat(o.phone));
  });
}

function dropiText(o) {
  return [
    `Nombre: ${o.customer_name}`, `Correo: ${o.email || ''}`, `Celular: ${o.customer_phone}`,
    `Departamento: ${o.department}`, `Ciudad: ${o.city}`, `Barrio: ${o.neighborhood}`,
    `Dirección: ${o.address}`, o.address_refs ? `Referencias: ${o.address_refs}` : '',
    `Producto: ${o.product_name} x${o.units} (${o.offer_label})`, `Total: ${o.total}`,
    `Valor a recaudar (contraentrega): ${o.cod_amount}`, o.advance_amount ? `Anticipo pagado: ${o.advance_amount}` : '',
    `Transportadora sugerida: ${o.carrier}`, o.notes ? `Notas: ${o.notes}` : '',
  ].filter(Boolean).join('\n');
}

async function copyForDropi(o) {
  const text = dropiText(o);
  try { await navigator.clipboard.writeText(text); toast('Datos copiados. Pégalos al crear la orden en Dropi.'); }
  catch (_) { openModal(`<h3>Datos para Dropi</h3><textarea rows="14" readonly style="width:100%">${esc(text)}</textarea><div class="actions"><button class="btn" data-close>Cerrar</button></div>`); }
}

// ---------- modal ----------
function openModal(html, wide) {
  $('#modalBox').className = 'card modal-box' + (wide ? ' wide' : '');
  $('#modalBox').innerHTML = html;
  $('#modal').classList.remove('hidden');
  $$('[data-close]', $('#modalBox')).forEach((b) => { b.onclick = closeModal; });
  hydrateIcons($('#modalBox'));
}
function closeModal() { $('#modal').classList.add('hidden'); }
$('#modal').addEventListener('mousedown', (e) => { if (e.target.id === 'modal') closeModal(); });
document.addEventListener('keydown', (e) => { if (e.key === 'Escape') closeModal(); });

async function openEditOrder(o) {
  let offers = [];
  try { offers = (await api('/api/productos/' + encodeURIComponent(o.product_sku))).producto.ofertas || []; } catch (_) { /* producto borrado */ }
  openModal(`<h3>Editar pedido #${o.id}</h3>
    <form id="editForm">
      <label class="field"><span>Oferta</span><select name="oferta_id">${offers.map((x) => `<option value="${esc(x.id)}" ${x.id === o.offer_id ? 'selected' : ''}>${esc(x.etiqueta)} · ${cop(x.precio)}</option>`).join('')}</select></label>
      <div class="fgrid">
      ${fld('nombre_completo', 'Nombre', o.customer_name)}${fld('email', 'Correo', o.email)}${fld('celular', 'Celular', o.customer_phone)}
      ${fld('departamento', 'Departamento', o.department)}${fld('ciudad', 'Ciudad', o.city)}${fld('barrio', 'Barrio', o.neighborhood)}
      ${fld('direccion', 'Dirección', o.address, { full: true })}${fld('referencias', 'Referencias', o.address_refs)}${fld('notas', 'Notas', o.notes)}
      </div>
      <label class="field"><span>Riesgo de la zona (cambia la forma de pago)</span><select name="riesgo">
        <option value="">Automático según reglas (${esc(o.risk || '-')})</option>
        <option value="normal">Normal: contraentrega</option><option value="lejano">Lejano: anticipo parcial</option>
        <option value="dificil_acceso">Difícil acceso: anticipo mayor</option><option value="zona_roja">Zona roja: pago 100% anticipado</option>
      </select></label>
      <div class="form-actions"><button class="btn primary" type="submit">Guardar</button><button class="btn" type="button" data-close>Cancelar</button></div>
    </form>`);
  $('#editForm').onsubmit = async (e) => {
    e.preventDefault();
    try { await post(`/api/pedidos/${o.id}/editar`, formData(e.target)); closeModal(); toast('Pedido actualizado.'); scheduleRefresh(); }
    catch (err) { toast(err.message, true); }
  };
}

function openDispatch(o) {
  openModal(`<h3>Registrar despacho #${o.id}</h3>
    <p class="small muted">Crea la orden en Dropi (con "Copiar datos para Dropi") y pega aquí el número de guía. Al guardar, el cliente recibe la guía por WhatsApp.</p>
    <form id="dispForm">
      ${fld('dropi_id', 'ID de la orden en Dropi', o.dropi_order_id)}
      ${fld('guia', 'Número de guía', o.tracking_number)}
      ${fld('transportadora', 'Transportadora', o.carrier)}
      <div class="form-actions"><button class="btn ok" type="submit">Guardar y avisar al cliente</button><button class="btn" type="button" data-close>Cancelar</button></div>
    </form>`);
  $('#dispForm').onsubmit = async (e) => {
    e.preventDefault();
    try { await post(`/api/pedidos/${o.id}/despachar`, formData(e.target)); closeModal(); toast('Despacho registrado.'); scheduleRefresh(); }
    catch (err) { toast(err.message, true); }
  };
}

// ---------- PEDIDOS ----------
async function renderPedidos() {
  const f = $('#orderFilter').value;
  const orders = await api('/api/pedidos' + (f ? '?estado=' + encodeURIComponent(f) : ''));
  $('#orderTable').innerHTML = `<thead><tr><th>#</th><th>Fecha</th><th>Cliente</th><th>Ciudad</th><th>Producto</th><th>Total</th><th>Pago</th><th>Estado</th><th>Guía</th></tr></thead>
    <tbody>${orders.map((o) => `<tr data-id="${o.id}">
      <td>${o.id}</td><td>${fmtTime(o.created_at)}</td><td>${esc(o.customer_name || contactLabel(o.phone))}</td><td>${esc(o.city)}</td>
      <td>${esc(o.product_name || '-')} <span class="muted small">${esc(o.offer_label || '')}</span></td><td>${cop(o.total)}</td><td>${payTag(o.payment_policy)}</td><td>${statusTag(o.status)}</td><td>${esc(o.tracking_number || '')}</td>
    </tr>`).join('') || '<tr><td colspan="9"><div class="empty">Sin pedidos</div></td></tr>'}</tbody>`;
  $$('#orderTable tbody tr[data-id]').forEach((tr) => { tr.onclick = () => openOrderDetail(orders.find((o) => String(o.id) === tr.dataset.id)); });
}
$('#orderCleanSim').addEventListener('click', async () => {
  if (!confirm('Se borran todos los pedidos que salieron del Simulador (los de WhatsApp reales no se tocan). ¿Seguro?')) return;
  try {
    const r = await post('/api/pedidos/borrar-simulador');
    toast(r.borrados ? `Listo: ${r.borrados} pedido(s) de prueba borrado(s).` : 'No había pedidos del simulador.');
    renderPedidos();
  } catch (err) { toast(err.message, true); }
});
$('#orderFilter').addEventListener('change', () => renderPedidos().catch((e) => toast(e.message, true)));

function openOrderDetail(o) {
  const statusBtns = ['en_transito', 'entregado', 'novedad', 'devuelto'].map((s) => `<button class="btn small" data-st="${s}">${STATUS[s][0]}</button>`).join('');
  const shipped = ['aprobado', 'en_dropi', 'en_transito', 'novedad'].includes(o.status);
  openModal(`<div class="row gap"><h3 style="margin:0">Pedido #${o.id}</h3>${statusTag(o.status)}</div>
    <div class="row gap wrap" style="margin-top:6px">${payTag(o.payment_policy)} ${riskTag(o.risk)}</div>
    ${orderKV(o)}${moneyBox(o)}
    ${o.tracking_number ? `<p>${ic('truck')} ${esc(o.carrier)} · guía <b>${esc(o.tracking_number)}</b>${o.dropi_order_id ? ' · Dropi ' + esc(o.dropi_order_id) : ''}</p>` : ''}
    ${shipped ? `<h4>Actualizar estado</h4><div class="actions">${statusBtns}</div>` : ''}
    <div class="form-actions"><button class="btn" data-copy>${ic('copy', 14)} Copiar datos</button><button class="btn" data-chatm>${ic('message', 14)} Ver chat</button><button class="btn danger" data-delorder>Borrar pedido</button><button class="btn" data-close>Cerrar</button></div>`);
  $$('[data-st]', $('#modalBox')).forEach((b) => {
    b.onclick = async () => {
      try { await post(`/api/pedidos/${o.id}/estado`, { estado: b.dataset.st }); closeModal(); toast('Estado actualizado.'); renderPedidos(); } catch (err) { toast(err.message, true); }
    };
  });
  $('[data-copy]', $('#modalBox')).onclick = () => copyForDropi(o);
  $('[data-chatm]', $('#modalBox')).onclick = () => { closeModal(); openChat(o.phone); };
  $('[data-delorder]', $('#modalBox')).onclick = async () => {
    const warn = o.dropi_order_id
      ? `Este pedido YA está en Dropi (${o.dropi_order_id}). Borrarlo aquí NO lo cancela en Dropi: tendrías que cancelarlo allá tú. ¿Borrarlo igual del panel?`
      : `¿Borrar el pedido #${o.id}? No se puede deshacer. El chat y el cliente se quedan.`;
    if (!confirm(warn)) return;
    try { await del(`/api/pedidos/${o.id}`); closeModal(); toast('Pedido borrado.'); renderPedidos(); } catch (err) { toast(err.message, true); }
  };
}

// ---------- conversaciones (lista; el detalle está en views.js) ----------
async function loadContacts() {
  S.contacts = await api('/api/contactos');
  const q = ($('#chatSearch').value || '').toLowerCase();
  const list = S.contacts.filter((c) => !q || contactName(c).toLowerCase().includes(q) || c.phone.includes(q));
  $('#contactList').innerHTML = list.map(contactItem).join('') || '<div class="empty">Sin conversaciones</div>';
  $$('#contactList .item').forEach((el) => { el.onclick = () => openChat(el.dataset.phone); });
  refreshBadges();
}
$('#chatSearch').addEventListener('input', () => loadContacts().catch(() => {}));

async function renderChats() {
  await loadContacts();
  if (S.chatPhone) window.showChat(S.chatPhone);
}

function openChat(phone) {
  S.chatPhone = phone;
  S.chatLastId = 0;
  if (location.hash !== '#chats') location.hash = 'chats'; else renderChats();
}

// ---------- arranque ----------
async function boot() {
  S.status = await api('/api/estado');
  $('#login').classList.add('hidden');
  $('#app').classList.remove('hidden');
  connectEvents();
  go();
  refreshBadges();
}

Object.assign(window, {
  S, $, $$, esc, cop, usd, pct, ic, hydrateIcons, fmtTime, waFormat, toast, api, post, put, del, mediaURL, statusTag, payTag, riskTag,
  contactName, contactLabel, openChat, openModal, closeModal, loadContacts, profitOf, orderKV, moneyBox, STATUS, STAGE,
  scheduleRefresh, boot, notice, fld, formData, busy, setupSteps, refreshBadges, go,
});
