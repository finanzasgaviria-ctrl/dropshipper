// Dropshipper Motor · Cazador: evalúa productos candidatos con los datos que ves en Dropi y DropKiller.
'use strict';

const CZ = { list: [], cfg: null, timer: null };
const LEVEL = { bueno: ['ok', 'Vale la pena probarlo'], dudoso: ['warn', 'Dudoso: revísalo'], no: ['bad', 'No lo recomiendo'], faltan_datos: ['', 'Faltan datos'] };
const CHECK_ICON = { ok: 'check-circle', warn: 'alert', bad: 'alert', unknown: 'info' };

async function renderCazador() {
  const [list, cfg] = await Promise.all([api('/api/candidatos'), api('/api/ajustes')]);
  CZ.list = list; CZ.cfg = cfg;
  const live = list.filter((c) => c.estado !== 'descartado');
  const off = list.filter((c) => c.estado === 'descartado');
  $('#cazador').innerHTML = `
    <div class="view-head"><div><h2>Cazador de productos</h2><div class="sub">Apunta un producto que te llamó la atención y el Motor te dice si vale la pena probarlo, con tus costos y tus reglas.</div></div>
      <span class="spacer"></span><button class="btn primary" id="czNew">${ic('plus', 14)} Agregar producto</button></div>
    ${notice('info', `<b>Cómo se usa (gratis).</b> 1) Instala la extensión gratuita de <b>DropKiller</b> en Chrome (en la Chrome Web Store busca "Dropkiller"). 2) Entra al catálogo de Dropi: bajo cada producto verás sus <b>ventas de los últimos 7 días</b>. 3) Cuando uno te guste, toca <b>Agregar producto</b> y escribe lo que ves. El Motor hace las cuentas con tus devoluciones y tu margen de seguridad. <br><span class="small">Más de 10 ventas en 7 días, repartidas en varios días, suele ser buena señal. Una cifra de un solo día engaña.</span>`)}
    <div id="czRules"></div>
    <div class="section-title">Productos que estás evaluando</div>
    ${live.length ? `<div class="cards">${live.map(czCard).join('')}</div>` : '<div class="card"><div class="empty">Aún no has agregado ningún producto. Toca <b>Agregar producto</b>.</div></div>'}
    ${off.length ? `<div class="section-title">Descartados</div><div class="cards">${off.map(czCard).join('')}</div>` : ''}`;
  $('#czNew').onclick = () => openCandidate(null);
  $$('#cazador .czcard').forEach((el) => { el.onclick = () => openCandidate(CZ.list.find((c) => String(c.id) === el.dataset.id)); });
  drawCzRules();
}

function czCard(c) {
  const v = c.veredicto || {}; const lv = LEVEL[v.nivel] || ['', v.titulo || ''];
  return `<div class="card pcard czcard ${c.estado === 'descartado' ? 'off' : ''}" data-id="${c.id}">
    <div class="top"><div><div class="nm">${esc(c.nombre)}</div><div class="sku">${c.dropi_product_id ? 'Dropi ' + esc(c.dropi_product_id) : 'Sin ID de Dropi'}${c.producto_sku ? ' · ya creado como ' + esc(c.producto_sku) : ''}</div></div>
      <div class="row gap wrap" style="justify-content:flex-end"><span class="tag ${lv[0]}">${esc(lv[1])}</span>${c.estado === 'convertido' ? '<span class="tag info">Creado</span>' : ''}</div></div>
    ${v.nivel === 'faltan_datos' ? `<div class="small muted">${esc(v.resumen || '')}</div>` : `<dl class="kv">
      <dt>Si se entrega ganas</dt><dd>${cop(v.ganancia_si_entrega)}</dd>
      <dt>Máx. en anuncios por pedido</dt><dd><b>${cop(v.cpa_objetivo)}</b></dd>
      <dt>Ventas en 7 días</dt><dd>${c.ventas_7d >= 0 ? c.ventas_7d : '<span class="muted">sin dato</span>'}</dd></dl>`}</div>`;
}

function drawCzRules() {
  const z = (CZ.cfg.negocio.cazador || {});
  $('#czRules').innerHTML = `<details class="card" style="margin-bottom:6px"><summary style="cursor:pointer;font-weight:600">Mis reglas para elegir productos</summary>
    <div class="fgrid" style="margin-top:12px">
      <label class="field"><span>Ventas mínimas en 7 días</span><input id="czMin" type="number" min="0" step="1" value="${z.min_ventas_7d || 10}"><small>Menos que esto = poca demanda.</small></label>
      <label class="field"><span>Mínimo para anuncios por pedido (COP)</span><input id="czCpa" type="number" min="0" step="1000" value="${z.cpa_minimo || 15000}"><small>Si después de costos y devoluciones queda menos que esto para publicidad, el producto no sirve.</small></label>
      <label class="field"><span>Máximo de vendedores anunciándolo</span><input id="czAds" type="number" min="0" step="1" value="${z.max_anunciantes || 0}"><small>0 = no revisar. Más que esto = producto saturado.</small></label>
    </div><div class="form-actions"><span class="spacer"></span><button class="btn primary" id="czRulesSave">Guardar reglas</button></div>
    <div class="small muted">Las devoluciones esperadas, el margen de seguridad y la confirmación se cambian en <a href="#ajustes">Ajustes</a>.</div></details>`;
  $('#czRulesSave').onclick = (ev) => busy(ev.currentTarget, async () => {
    try {
      const nz = { min_ventas_7d: Math.max(0, parseInt($('#czMin').value, 10) || 0), cpa_minimo: Math.max(0, parseInt($('#czCpa').value, 10) || 0), max_anunciantes: Math.max(0, parseInt($('#czAds').value, 10) || 0) };
      await put('/api/ajustes/negocio', { cazador: nz });
      toast('Listo. Tus reglas ya se usan para juzgar los productos.');
      renderCazador();
    } catch (err) { toast(err.message, true); }
  });
}

const numOrNull = (v) => { v = String(v ?? '').trim(); return v === '' ? null : Number(v); };

function openCandidate(c) {
  const isNew = !c; c = c || { nombre: '', dropi_product_id: '', costo: 0, precio_venta: 0, flete: 0, stock: -1, ventas_7d: -1, anunciantes: -1, enlace: '', notas: '', estado: 'nuevo' };
  const known = (v) => (v >= 0 ? v : '');
  const inp = (name, label, val, help, o = {}) => `<label class="field${o.full ? ' full' : ''}"><span>${label}</span><input data-k="${name}" type="${o.type || 'number'}" min="0" step="1" value="${esc(val ?? '')}" ${o.ph ? `placeholder="${esc(o.ph)}"` : ''}><small>${help}</small></label>`;
  openModal(`<h3>${isNew ? 'Agregar producto' : 'Producto: ' + esc(c.nombre)}</h3>
    <div class="fgrid">
      ${inp('nombre', 'Nombre', c.nombre, 'Como lo llamas tú.', { type: 'text', full: true })}
      <label class="field"><span>ID del producto en Dropi</span><div class="inline"><input data-k="dropi_product_id" value="${esc(c.dropi_product_id)}"><button class="btn" type="button" id="czPull" ${isNew ? 'disabled title="Guarda primero"' : ''}>${ic('refresh', 14)} Traer de Dropi</button></div><small>Opcional. Con Dropi conectado, trae costo y stock reales.</small></label>
      ${inp('enlace', 'Enlace (opcional)', c.enlace, 'Dónde lo viste.', { type: 'text' })}
      ${inp('costo', 'Costo del proveedor (COP)', c.costo || '', 'Lo que te cobra Dropi por unidad.')}
      ${inp('precio_venta', 'Precio al que lo venderías (COP)', c.precio_venta || '', 'Lo que pagaría el cliente.')}
      ${inp('flete', 'Flete aproximado (COP)', c.flete || '', 'Lo que cobra la transportadora. Si no sabes, pon unos 15.000 a 20.000.')}
      ${inp('stock', 'Stock del proveedor', known(c.stock), 'Unidades disponibles en Dropi. Vacío = no lo sé.')}
      ${inp('ventas_7d', 'Ventas en los últimos 7 días', known(c.ventas_7d), 'Lo muestra DropKiller bajo el producto en Dropi. Vacío = no lo sé.')}
      ${inp('anunciantes', 'Vendedores que lo anuncian', known(c.anunciantes), 'Lo ves en la biblioteca de anuncios de Meta. Vacío = no lo sé.')}
      <label class="field full"><span>Notas</span><textarea data-k="notas" rows="2" style="width:100%">${esc(c.notas)}</textarea></label>
    </div>
    <div id="czVerdict"></div>
    <div class="form-actions"><button class="btn primary" id="czSave">Guardar</button>
      ${!isNew && c.estado !== 'convertido' ? `<button class="btn" id="czMake">${ic('tag', 14)} Crear ficha de producto</button>` : ''}
      ${!isNew && c.estado !== 'descartado' ? '<button class="btn" id="czDrop">Descartar</button>' : ''}
      ${!isNew && c.estado === 'descartado' ? '<button class="btn" id="czBack">Volver a evaluarlo</button>' : ''}
      ${!isNew ? '<button class="btn danger" id="czDel">Borrar</button>' : ''}<button class="btn" data-close>Cerrar</button></div>`, true);
  const box = $('#modalBox');
  const read = () => {
    const o = {}; $$('[data-k]', box).forEach((e) => { o[e.dataset.k] = e.dataset.k.match(/^(nombre|dropi_product_id|enlace|notas)$/) ? e.value : numOrNull(e.value); });
    o.costo = o.costo || 0; o.precio_venta = o.precio_venta || 0; o.flete = o.flete || 0; o.estado = c.estado;
    return { ...o, link: undefined, enlace: o.enlace };
  };
  const paint = (d) => {
    const v = d.veredicto; const lv = LEVEL[v.nivel] || ['', v.titulo];
    $('#czVerdict', box).innerHTML = `<div class="card" style="margin-top:14px"><div class="row gap"><b>Veredicto</b><span class="tag ${lv[0]}">${esc(v.titulo)}</span></div>
      <div class="small" style="margin:6px 0 10px">${esc(v.resumen)}</div>
      ${(v.revision || []).map((k) => `<div class="small" style="display:flex;gap:8px;margin-bottom:6px"><span class="tag ${k.estado === 'unknown' ? 'info' : k.estado}" style="min-width:86px;justify-content:center">${esc(k.nombre)}</span><span>${esc(k.detalle)}</span></div>`).join('')}</div>`;
  };
  const calc = () => {
    clearTimeout(CZ.timer);
    CZ.timer = setTimeout(async () => { try { paint(await post('/api/candidatos/calcular', read())); } catch (_) { /* se reintenta */ } }, 300);
  };
  $$('[data-k]', box).forEach((e) => e.addEventListener('input', calc));
  calc();
  const save = async () => (isNew ? post('/api/candidatos', read()) : put('/api/candidatos/' + c.id, read()));
  const act = (sel, fn, msg) => { const b = $(sel, box); if (b) b.onclick = () => busy(b, async () => { try { await fn(); if (msg) toast(msg); closeModal(); renderCazador(); } catch (err) { toast(err.message, true); } }); };
  act('#czSave', save, 'Guardado.');
  act('#czDel', async () => { if (!confirm('¿Borrar este producto de la lista? No se puede deshacer.')) throw new Error('Cancelado'); await del('/api/candidatos/' + c.id); }, 'Borrado.');
  act('#czDrop', async () => { await put('/api/candidatos/' + c.id, { ...read(), estado: 'descartado' }); }, 'Descartado.');
  act('#czBack', async () => { await put('/api/candidatos/' + c.id, { ...read(), estado: 'nuevo' }); }, 'Listo, vuelve a la lista.');
  act('#czMake', async () => {
    await save();
    const r = await post(`/api/candidatos/${c.id}/crear-producto`);
    toast(`Ficha creada como ${r.sku}. Está apagada: complétala en Productos y enciéndela cuando esté lista.`);
  });
  const pull = $('#czPull', box);
  if (pull) pull.onclick = () => busy(pull, async () => {
    try { await save(); paint(await post(`/api/candidatos/${c.id}/traer-dropi`)); toast('Datos traídos de Dropi. Cierra y vuelve a abrir para verlos en los campos.'); } catch (err) { toast(err.message, true); }
  });
}

Object.assign(window, { renderCazador });
