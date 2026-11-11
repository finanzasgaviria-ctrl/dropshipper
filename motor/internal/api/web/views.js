// Dropshipper Motor · conversación, simulador y auditoría de la IA.
'use strict';

// ---------- CHAT (detalle) ----------
const SENDER_LABEL = { cliente: '', ia: 'IA', dueno: 'Tú', sistema: 'Sistema' };

function msgHTML(m, simulator) {
  const side = m.direction === 'in' ? 'in' : 'out';
  const who = SENDER_LABEL[m.sender] || '';
  let body = waFormat(m.body || '');
  if (m.kind === 'image') {
    body = (m.media_path ? `<img src="${mediaURL(m.media_path)}" alt="imagen">` : '<i>Imagen</i>') + (m.body ? '<div>' + waFormat(m.body) + '</div>' : '');
  } else if (m.kind === 'audio') {
    body = '<i>Nota de voz</i>';
  } else if (m.kind !== 'text' && !m.body) {
    body = `<i>[${esc(m.kind)}]</i>`;
  }
  const ticks = { enviado: 'enviado', delivered: 'entregado', read: 'leído', fallido: 'no se pudo enviar', failed: 'no entregado', simulado: '', sin_whatsapp: 'sin WhatsApp' }[m.status] ?? '';
  const meta = [simulator && m.sender === 'cliente' ? '' : who, fmtTime(m.created_at), side === 'out' ? ticks : ''].filter(Boolean).join(' · ');
  return `<div class="msg ${side} ${esc(m.sender)}" data-id="${m.id}">${body}<span class="meta">${esc(meta)}</span></div>`;
}

async function showChat(phone) {
  const pane = $('#chatPane');
  $('.chat-layout').classList.add('has-chat');
  let data;
  try { data = await api('/api/contactos/' + encodeURIComponent(phone)); }
  catch (err) { pane.innerHTML = `<div class="empty">${esc(err.message)}</div>`; return; }
  const c = data.contacto;
  const open = data.pedidos.find((o) => ['borrador', 'esperando_pago', 'por_aprobar'].includes(o.status)) || data.pedidos[0];
  pane.innerHTML = `
    <div class="chat-head">
      <button class="btn small icon" id="chatBack" title="Volver">${ic('back')}</button>
      <div class="who"><b>${esc(contactName(c))}</b><div class="small muted">${c.simulated ? 'Chat de prueba' : '+' + esc(c.phone)} · ${esc(STAGE[c.stage] || c.stage)}${c.archived ? ' · archivada' : ''}</div></div>
      ${open ? `<span class="row gap">${statusTag(open.status)} <span class="small">#${open.id} ${cop(open.total)}</span></span>` : ''}
      <button class="btn small" id="chatAudit">${ic('brain', 14)} Qué hizo la IA</button>
      <button class="btn small" id="chatArchive" title="${c.archived ? 'Devolverla a la bandeja: el chat sigue completo' : 'Guardarla fuera de la bandeja sin borrar nada'}">${ic('archive', 14)} ${c.archived ? 'Desarchivar' : 'Archivar'}</button>
      ${c.ai_paused
        ? `<button class="btn small ok" id="chatResume">${ic('bot', 14)} Devolver a la IA</button>`
        : `<button class="btn small" id="chatTake">${ic('hand', 14)} Tomar el chat</button>`}
    </div>
    ${c.ai_paused ? '<div class="paused-bar">La IA está en pausa en este chat: respondes tú. <button class="btn small" id="chatResume2">Devolver a la IA</button></div>' : ''}
    <div class="chat-body" id="chatBody"></div>
    <form class="chat-input" id="chatForm">
      <textarea id="chatText" rows="1" placeholder="${c.ai_paused ? 'Escribe tu mensaje' : 'Al escribir aquí pausas la IA en este chat'}"></textarea>
      <button class="btn primary" type="submit">Enviar</button>
    </form>`;
  $('#chatBack').onclick = () => { S.chatPhone = null; $('.chat-layout').classList.remove('has-chat'); pane.innerHTML = '<div class="empty">Selecciona una conversación</div>'; loadContacts(); };
  const setPaused = async (paused) => {
    try {
      await post(`/api/contactos/${encodeURIComponent(phone)}/pausar`, { paused });
      if (!paused && confirm('¿Quieres que la IA responda ahora el último mensaje del cliente?')) {
        await post(`/api/contactos/${encodeURIComponent(phone)}/responder`);
      }
      toast(paused ? 'Tomaste el chat: la IA no responderá aquí.' : 'La IA vuelve a atender este chat.');
      showChat(phone);
    } catch (err) { toast(err.message, true); }
  };
  const t = $('#chatTake'); if (t) t.onclick = () => setPaused(true);
  ['#chatResume', '#chatResume2'].forEach((id) => { const b = $(id); if (b) b.onclick = () => setPaused(false); });
  $('#chatAudit').onclick = () => showAudit(phone);
  $('#chatArchive').onclick = async () => {
    const archivar = !c.archived;
    try {
      await post(`/api/contactos/${encodeURIComponent(phone)}/archivar`, { archivado: archivar });
      toast(archivar ? 'Conversación archivada: sigue completa en Archivadas.' : 'Conversación devuelta a la bandeja.');
      leaveChat();
      await loadContacts();
    } catch (err) { toast(err.message, true); }
  };
  const ta = $('#chatText');
  ta.addEventListener('keydown', (e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); $('#chatForm').requestSubmit(); } });
  $('#chatForm').onsubmit = async (e) => {
    e.preventDefault();
    const text = ta.value.trim();
    if (!text) return;
    ta.value = '';
    try { await post(`/api/contactos/${encodeURIComponent(phone)}/enviar`, { text }); if (!c.ai_paused) showChat(phone); else loadChatMessages(); }
    catch (err) { toast(err.message, true); ta.value = text; }
  };
  S.chatLastId = 0;
  $('#chatBody').innerHTML = '';
  await loadChatMessages();
  post(`/api/contactos/${encodeURIComponent(phone)}/leido`).then(() => loadContacts()).catch(() => {});
}

async function loadChatMessages() {
  const phone = S.chatPhone;
  const body = $('#chatBody');
  if (!phone || !body) return;
  const msgs = await api(`/api/contactos/${encodeURIComponent(phone)}/mensajes?after=${S.chatLastId}`);
  if (phone !== S.chatPhone) return;
  const nearBottom = body.scrollHeight - body.scrollTop - body.clientHeight < 120;
  $('.typing', body)?.remove();
  for (const m of msgs) {
    if (!$(`.msg[data-id="${m.id}"]`, body)) body.insertAdjacentHTML('beforeend', msgHTML(m, false));
    S.chatLastId = Math.max(S.chatLastId, m.id);
  }
  if (S.typing[phone]) body.insertAdjacentHTML('beforeend', '<div class="typing">La IA está escribiendo…</div>');
  if (nearBottom || msgs.length) body.scrollTop = body.scrollHeight;
}

async function showAudit(phone) {
  const rows = await api(`/api/contactos/${encodeURIComponent(phone)}/auditoria`);
  openModal(`<h3>Lo que hizo la IA en este chat</h3>
    <p class="small muted">Cada herramienta que usó la IA, con los datos que envió y lo que le respondió el sistema. Sirve para entender por qué decidió algo.</p>
    <div class="audit">${auditRows(rows) || '<div>Sin registros todavía.</div>'}</div>
    <div class="form-actions"><button class="btn" data-close>Cerrar</button></div>`, true);
}

function auditRows(rows) {
  return rows.slice().reverse().map((a) => {
    let detail = a.detail || '';
    if (detail.length > 600) detail = detail.slice(0, 600) + '…';
    return `<div><b>${esc(a.action.replace('herramienta:', 'Herramienta: '))}</b> <span>(${esc(a.actor)}, ${fmtTime(a.created_at)})</span><br>${esc(detail)}</div>`;
  }).join('');
}

// ---------- SIMULADOR ----------
// Frases genéricas: sirven para cualquier producto.
const SIM_IDEAS = [
  'Hola, vi su anuncio y quiero más información',
  '¿Cuánto vale?',
  '¿Cómo funciona exactamente?',
  '¿Tiene alguna contraindicación o efectos secundarios?',
  'Está muy caro',
  '¿Y si no me funciona?',
  '¿Eres un bot?',
  'Quiero hablar con una persona',
  'Me llevo la oferta más grande',
  'Ana María Restrepo, cc 1036123456, cel 3001234567, Medellín Antioquia, barrio Laureles, Cra 76 # 33-20 apto 301',
  'Soy de Leticia, Amazonas, ¿me llega?',
  'Vivo en Vigía del Fuerte, Antioquia',
  'Sí, todo correcto, confirmo',
];

function newSimPhone() {
  S.simPhone = 'sim-' + Math.random().toString(36).slice(2, 7);
  localStorage.setItem('ds_sim', S.simPhone);
  S.simLastId = 0;
}

async function renderSimulador() {
  if (!S.simPhone) newSimPhone();
  $('#simTitle').textContent = 'Cliente de prueba · ' + S.simPhone.slice(4);
  $('#simIdeas').innerHTML = SIM_IDEAS.map((t) => `<span class="chip">${esc(t)}</span>`).join('');
  $$('#simIdeas .chip').forEach((ch) => { ch.onclick = () => { $('#simText').value = ch.textContent; $('#simText').focus(); }; });
  S.simLastId = 0;
  const st = S.status;
  const aviso = st && st.ia && !st.ia.configurada
    ? notice('warn', 'La IA aún no está conectada, así que no responderá. <a href="#conexiones">Conéctala en Conexiones</a>.') : '';
  $('#simBody').innerHTML = aviso + '<div class="empty small">Escribe como si fueras un cliente que llega desde el anuncio. La IA responde igual que en WhatsApp. Tus mensajes aparecen a la derecha.</div>';
  await loadSimMessages();
  await loadSimSide();
}

async function loadSimMessages() {
  const body = $('#simBody');
  if (!S.simPhone || !body) return;
  let msgs = [];
  try { msgs = await api(`/api/contactos/${encodeURIComponent(S.simPhone)}/mensajes?after=${S.simLastId}`); } catch (_) { return; }
  if (msgs.length && S.simLastId === 0) body.innerHTML = '';
  $('.typing', body)?.remove();
  for (const m of msgs) {
    // Dos cargas simultáneas (evento en vivo + envío) pueden traer los mismos mensajes: se pinta cada uno una sola vez.
    if (!$(`.msg[data-id="${m.id}"]`, body)) body.insertAdjacentHTML('beforeend', msgHTML(m, true));
    S.simLastId = Math.max(S.simLastId, m.id);
  }
  if (S.typing[S.simPhone]) body.insertAdjacentHTML('beforeend', '<div class="typing">escribiendo…</div>');
  $('#simStatus').textContent = S.typing[S.simPhone] ? 'escribiendo…' : 'en línea';
  body.scrollTop = body.scrollHeight;
}

async function loadSimSide() {
  if (!S.simPhone) return;
  try {
    const [data, audit] = await Promise.all([
      api('/api/contactos/' + encodeURIComponent(S.simPhone)),
      api(`/api/contactos/${encodeURIComponent(S.simPhone)}/auditoria`),
    ]);
    const o = data.pedidos[0];
    $('#simOrder').innerHTML = o ? `<div class="row gap wrap">${statusTag(o.status)} ${payTag(o.payment_policy)} ${riskTag(o.risk)}</div>${orderKV(o)}${o.total ? moneyBox(o) : ''}
      <div class="actions">${o.status === 'por_aprobar' ? `<button class="btn ok small" id="simApprove">${ic('check', 14)} Aprobar (como dueño)</button>` : ''}
      ${o.status === 'esperando_pago' ? `<button class="btn ok small" id="simPaid">${ic('dollar', 14)} Marcar pago recibido</button>` : ''}</div>` : '<span class="muted">Aún no hay pedido.</span>';
    const ap = $('#simApprove'); if (ap) ap.onclick = async () => { try { await post(`/api/pedidos/${o.id}/aprobar`); toast('Pedido aprobado.'); } catch (e) { toast(e.message, true); } };
    const pd = $('#simPaid'); if (pd) pd.onclick = async () => { try { await post(`/api/pedidos/${o.id}/pago-confirmado`); toast('Pago confirmado.'); } catch (e) { toast(e.message, true); } };
    $('#simAudit').innerHTML = auditRows(audit.filter((a) => a.action !== 'tokens')) || '<div>La IA aún no ha usado herramientas.</div>';
  } catch (_) {
    $('#simOrder').innerHTML = '<span class="muted">Aún no hay pedido.</span>';
    $('#simAudit').innerHTML = '';
  }
}

async function simSend(text, kind = 'text') {
  try {
    await post('/api/simulador/mensaje', { phone: S.simPhone, name: '', text, kind });
    await loadSimMessages();
  } catch (err) { toast(err.message, true); }
}

$('#simForm').addEventListener('submit', (e) => {
  e.preventDefault();
  const t = $('#simText').value.trim();
  if (!t) return;
  $('#simText').value = '';
  simSend(t);
});
$('#simPhoto').addEventListener('click', () => simSend('[foto del comprobante de pago]', 'image'));
$('#simNew').addEventListener('click', () => { newSimPhone(); renderSimulador(); });
$('#simReset').addEventListener('click', async () => {
  if (!confirm('¿Borrar este chat de prueba y su pedido?')) return;
  try { await del('/api/simulador/' + encodeURIComponent(S.simPhone)); } catch (_) { /* puede no existir */ }
  newSimPhone();
  renderSimulador();
});

Object.assign(window, { showChat, loadChatMessages, renderSimulador, loadSimMessages, loadSimSide });
