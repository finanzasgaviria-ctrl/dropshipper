// Dropshipper Motor · envíos: transportadoras (con notas para la IA), cotizador de fletes y simulación de pedido.
'use strict';

const EV = { cfg: null, products: [], carriers: [] };

const srcTag = (s) => (s === 'dropi' ? '<span class="tag ok">Flete real de Dropi</span>' : '<span class="tag warn">Flete estimado</span>');

async function renderEnvios() {
  const [cfg, products] = await Promise.all([api('/api/ajustes'), api('/api/productos')]);
  EV.cfg = cfg; EV.products = products;
  EV.carriers = JSON.parse(JSON.stringify(cfg.negocio.transportadoras || []));
  const d = cfg.dropi || {};
  $('#envios').innerHTML = `
    <div class="view-head"><div><h2>Envíos</h2><div class="sub">Con qué transportadoras trabajas, cuál conviene para cada pedido y una prueba completa sin crear nada en Dropi.</div></div></div>
    ${d.configurado
      ? (d.sandbox ? notice('warn', 'Dropi está en <b>entorno de pruebas</b>: allí no hay transportadoras, así que los fletes serán estimados. Para fletes reales, desmarca el entorno de pruebas en <a href="#conexiones">Conexiones</a>.') : notice('ok', 'Dropi conectado: los fletes se cotizan en vivo.'))
      : notice('info', 'Dropi no está conectado: se usa el flete estimado de cada producto. Conéctalo en <a href="#conexiones">Conexiones</a> para cotizar el flete real.')}
    <div id="evRules"></div>
    <div id="evCarriers" style="margin-top:16px"></div>
    <div id="evQuote" style="margin-top:16px"></div>`;
  drawRules(); drawCarriers(); drawQuote();
}

// =============== Cómo se recomienda la transportadora ===============
function drawRules() {
  const e = (EV.cfg.negocio.envio || {}); const crit = e.criterio || 'rentable';
  const opt = (v, t) => `<option value="${v}" ${crit === v ? 'selected' : ''}>${t}</option>`;
  $('#evRules').innerHTML = `<div class="card">
    <div class="card-head"><span class="ico">${ic('clock', 18)}</span><div><h3>Cómo se recomienda la transportadora</h3>
      <div class="desc">El Motor nunca recomienda una transportadora más lenta que tu plazo si otra sí llega a tiempo. Entre las que llegan a tiempo, elige según tu criterio.</div></div></div>
    <div class="fgrid">
      <label class="field"><span>Plazo máximo de entrega (días)</span><input id="evMax" type="number" min="0" max="60" step="1" value="${e.max_dias || 0}"><small>0 = sin límite. Si ninguna llega a tiempo, se recomienda la más rápida y se te avisa.</small></label>
      <label class="field"><span>Entre las que llegan a tiempo, elegir la</span><select id="evCrit">${opt('rentable', 'Más rentable (la que más ganancia deja)')}${opt('rapido', 'Más rápida')}${opt('barato', 'Más barata (menor flete)')}</select>
        <small>Si eliges rápida o barata, nunca se recomienda una que te haga perder dinero habiendo otra que no.</small></label>
    </div>
    <div class="form-actions"><span class="spacer"></span><button class="btn primary" id="evRulesSave">Guardar</button></div></div>`;
  $('#evRulesSave').onclick = (ev) => busy(ev.currentTarget, async () => {
    try {
      const max = Math.min(60, Math.max(0, parseInt($('#evMax').value, 10) || 0));
      await put('/api/ajustes/negocio', { envio: { max_dias: max, criterio: $('#evCrit').value } });
      EV.cfg.negocio.envio = { max_dias: max, criterio: $('#evCrit').value };
      toast('Listo. Los próximos pedidos y cotizaciones usan este criterio.');
    } catch (err) { toast(err.message, true); }
  });
}

// =============== Transportadoras ===============
function drawCarriers() {
  const rows = EV.carriers.map((c, i) => `<div class="carrier ${c.activa ? '' : 'off'}" data-i="${i}">
      <div class="carrier-top"><label class="switch" style="margin:0"><input type="checkbox" data-k="activa" ${c.activa ? 'checked' : ''}><div><b>${esc(c.nombre)}</b></div></label>
        ${c.activa ? '<span class="tag ok">Activa</span>' : '<span class="tag">Apagada</span>'}<span class="spacer"></span>
        <span class="small muted">${(c.zonas || []).length ? 'Fuerte en: ' + esc(c.zonas.join(', ')) : ''}</span></div>
      <div class="carrier-fields">
        <label class="field"><span>Prioridad</span><input data-k="prioridad" type="number" min="1" step="1" value="${c.prioridad || 99}"><small>Menor gana si empatan.</small></label>
        <label class="field"><span>Días de entrega (ciudades)</span><input data-k="dias" type="number" min="0" max="60" step="1" value="${c.dias_entrega || ''}" placeholder="${((EV.cfg.dias_tipicos || {})[c.id] || [])[0] || ''}"><small>Lo que suele tardar a ciudades principales. Vacío = valor típico.</small></label>
        <label class="field"><span>Días de entrega (zonas lejanas)</span><input data-k="diasl" type="number" min="0" max="60" step="1" value="${c.dias_lejano || ''}" placeholder="${((EV.cfg.dias_tipicos || {})[c.id] || [])[1] || ''}"><small>Municipios apartados o de difícil acceso.</small></label>
        <label class="field"><span>Devoluciones</span><div class="suffix"><input data-k="tasa" type="number" min="0" max="100" step="0.5" value="${Math.round((c.tasa_devolucion || 0) * 1000) / 10}"><em>%</em></div><small>0 = usar el general.</small></label>
        <label class="field"><span>Qué debe saber la IA de esta transportadora</span><textarea data-k="notas" rows="2">${esc(c.notas)}</textarea></label>
      </div></div>`).join('');
  $('#evCarriers').innerHTML = `<div class="card">
    <div class="card-head"><span class="ico">${ic('truck', 18)}</span><div><h3>Transportadoras</h3>
      <div class="desc">Enciende las que usas. El Motor cotiza solo las activas y elige la de mayor ganancia esperada. Las notas se las explican a la IA (cobertura, tiempos, contraentrega) para que responda con precisión.</div></div></div>
    ${rows || '<div class="empty">No hay transportadoras configuradas.</div>'}
    <div class="form-actions"><span class="spacer"></span><button class="btn primary" id="evSave">Guardar transportadoras</button></div></div>`;
  $$('#evCarriers .carrier').forEach((el) => {
    const c = EV.carriers[Number(el.dataset.i)];
    el.addEventListener('input', (e) => {
      const k = e.target.dataset.k;
      if (k === 'activa') { c.activa = e.target.checked; el.classList.toggle('off', !c.activa); }
      if (k === 'prioridad') c.prioridad = Math.max(1, parseInt(e.target.value, 10) || 99);
      if (k === 'dias') c.dias_entrega = Math.min(60, Math.max(0, parseInt(e.target.value, 10) || 0));
      if (k === 'diasl') c.dias_lejano = Math.min(60, Math.max(0, parseInt(e.target.value, 10) || 0));
      if (k === 'tasa') c.tasa_devolucion = Math.min(100, Math.max(0, Number(e.target.value) || 0)) / 100;
      if (k === 'notas') c.notas = e.target.value;
    });
  });
  $('#evSave').onclick = (e) => busy(e.currentTarget, async () => {
    if (!EV.carriers.some((c) => c.activa)) { toast('Deja al menos una transportadora activa.', true); return; }
    try { await put('/api/ajustes/negocio', { transportadoras: EV.carriers }); toast('Transportadoras guardadas. La IA las usa desde el próximo mensaje.'); } catch (err) { toast(err.message, true); }
  });
}

// =============== Cotizador y simulación ===============
function drawQuote() {
  const prods = EV.products;
  if (!prods.length) { $('#evQuote').innerHTML = `<div class="card"><div class="empty">Crea un producto primero para poder cotizar un envío.</div></div>`; return; }
  $('#evQuote').innerHTML = `<div class="card">
    <div class="card-head"><span class="ico">${ic('flask', 18)}</span><div><h3>Cotizar y simular un pedido</h3>
      <div class="desc">Elige un producto y un destino. <b>Cotizar</b> compara las transportadoras. <b>Simular pedido en Dropi</b> hace además todas las verificaciones y muestra el pedido que se enviaría, <b>sin crear nada</b>.</div></div></div>
    <form id="evForm" autocomplete="off"><div class="fgrid">
      <label class="field"><span>Producto</span><select name="producto_sku">${prods.map((p) => `<option value="${esc(p.sku)}">${esc(p.nombre)}</option>`).join('')}</select></label>
      <label class="field"><span>Oferta</span><select name="oferta_id"></select></label></div>
      <div class="fgrid c3">${fld('departamento', 'Departamento', '', { ph: 'Antioquia' })}${fld('ciudad', 'Ciudad', '', { ph: 'Medellín' })}${fld('barrio', 'Barrio (opcional)', '')}
    </div>
    <div class="form-actions"><button class="btn primary" type="submit" data-m="cotizar">${ic('refresh', 14)} Cotizar</button>
      <button class="btn" type="submit" data-m="simular">${ic('check-circle', 14)} Simular pedido en Dropi</button></div></form>
    <div id="evOut"></div></div>`;
  const f = $('#evForm');
  const loadOffers = async () => {
    try {
      const p = (await api('/api/productos/' + encodeURIComponent(f.producto_sku.value))).producto;
      f.oferta_id.innerHTML = (p.ofertas || []).map((o) => `<option value="${esc(o.id)}" ${o.destacada ? 'selected' : ''}>${esc(o.etiqueta)} · ${cop(o.precio)}</option>`).join('');
    } catch (_) { f.oferta_id.innerHTML = ''; }
  };
  f.producto_sku.onchange = loadOffers; loadOffers();
  f.onsubmit = async (e) => {
    e.preventDefault();
    const mode = e.submitter && e.submitter.dataset.m;
    const b = formData(f);
    if (!b.ciudad.trim()) { toast('Escribe la ciudad de destino.', true); return; }
    await busy(e.submitter, async () => {
      $('#evOut').innerHTML = '';
      try {
        if (mode === 'simular') drawSimulation(await post('/api/dropi/simular', b));
        else drawQuoteResult(await post('/api/envios/cotizar', b));
      } catch (err) { $('#evOut').innerHTML = notice('bad', esc(err.message)); hydrateIcons($('#evOut')); }
    });
  };
}

function quoteTable(cmp) {
  const opts = cmp.opciones || [];
  if (!opts.length) return notice('bad', esc(cmp.advertencia || 'No hay transportadoras activas para cotizar.'));
  const crit = { rentable: 'la más rentable', rapido: 'la más rápida', barato: 'la más barata' }[cmp.criterio] || 'la más rentable';
  const plazo = cmp.max_dias ? `plazo máximo ${cmp.max_dias} días` : 'sin plazo máximo';
  const days = (o) => `${o.fuente_dias === 'estimado' ? '~' : ''}${o.dias === 1 ? '1 día' : o.dias + ' días'}`;
  const tags = (o) => [
    o.mejor ? '<span class="tag brand">Recomendada</span>' : '',
    o.mas_rapida ? '<span class="tag ok">Más rápida</span>' : '',
    o.mas_barata ? '<span class="tag ok">Más barata</span>' : '',
    o.dentro_del_limite === false ? '<span class="tag bad">Fuera de tu plazo</span>' : '',
    o.viable ? '' : '<span class="tag bad">No conviene</span>',
  ].filter(Boolean).join(' ');
  return `<div class="row gap wrap" style="margin-top:12px">${srcTag(cmp.fuente_flete)}<span class="small muted">Recomendación: ${crit} · ${plazo}</span>${cmp.advertencia ? `<span class="small" style="color:var(--warn)">${ic('alert', 13)} ${esc(cmp.advertencia)}</span>` : ''}</div>
    <div class="table-wrap" style="margin-top:6px"><table class="qtable"><thead><tr><th>Transportadora</th><th>Llega en</th><th>Envío (flete)</th><th>Ganancia si el cliente recibe</th><th>Pierdes si lo devuelve</th><th>Ganancia esperada</th><th>Devoluciones</th></tr></thead><tbody>
    ${opts.map((o) => `<tr class="${o.mejor ? 'best' : ''}"><td>${esc(o.transportadora)} ${tags(o)}</td>
      <td>${days(o)}</td><td>${cop(o.flete)}</td><td class="${o.ganancia_si_entrega >= 0 ? 'pos' : 'neg'}">${cop(o.ganancia_si_entrega)}</td><td class="neg">${cop(Math.abs(o.perdida_si_devuelve))}</td>
      <td class="${o.ganancia_esperada >= 0 ? 'pos' : 'neg'}">${cop(o.ganancia_esperada)}</td><td>${pct(o.tasa_devolucion)}</td></tr>`).join('')}</tbody></table></div>
    <div class="small muted" style="margin-top:6px">Los días con ~ son los tiempos típicos que tú configuraste arriba; Dropi no informó el tiempo para esa transportadora.</div>`;
}

function drawQuoteResult(q) {
  const e = q.envio || {};
  $('#evOut').innerHTML = `<div style="margin-top:14px"><div class="row gap wrap"><b>${esc(q.producto)} · ${esc(q.oferta)} · ${cop(q.valor)}</b>${riskTag(e.riesgo)} ${payTag(e.politica_pago)}
      ${q.costo_confirmado ? '' : '<span class="tag warn">Costo sin confirmar</span>'}</div>
    <div class="small muted" style="margin-top:4px">${esc(e.motivo || '')}</div>
    ${q.costo_confirmado ? '' : `<div class="small" style="color:var(--warn);margin-top:6px">${ic('alert', 13)} El costo del producto aún no está confirmado: las ganancias son estimadas.</div>`}
    ${quoteTable(q.comparacion || {})}</div>`;
  hydrateIcons($('#evOut'));
}

function drawSimulation(r) {
  const icon = { ok: 'check-circle', aviso: 'alert', error: 'alert' };
  $('#evOut').innerHTML = `<div class="simbanner">${ic('flask', 15)} ${esc(r.mensaje)}</div>
    <div class="checks">${(r.verificaciones || []).map((c) => `<div class="check ${esc(c.estado)}">${ic(icon[c.estado] || 'info', 16)}<div><b>${esc(c.nombre)}</b>${esc(c.detalle)}</div></div>`).join('')}</div>
    ${r.ok ? notice('ok', 'Todo está listo para crear pedidos reales con este destino.') : notice('bad', 'Hay problemas por resolver antes de crear pedidos reales (marcados en rojo).')}
    ${quoteTable(r.comparacion || {})}
    <details class="term" style="margin-top:12px"><summary>${ic('eye', 14)} Pedido que se enviaría a Dropi (solo vista previa)</summary>
      <pre class="json">${esc(JSON.stringify(r.pedido_dropi || {}, null, 2))}</pre></details>`;
  hydrateIcons($('#evOut'));
}

Object.assign(window, { renderEnvios });
