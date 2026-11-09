'use strict';
/* openv6 panel — vanilla JS single page app (no build step). */

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

function store(k, v) {
  try { if (v === undefined) return localStorage.getItem(k); localStorage.setItem(k, v); } catch (e) { return null; }
}

let LANG = store('v6p_lang') || ((navigator.language || 'vi').startsWith('vi') ? 'vi' : 'en');
function t(key, vars) {
  let s = (I18N[LANG] && I18N[LANG][key]) || I18N.en[key] || key;
  if (vars) for (const k in vars) s = s.replaceAll('{' + k + '}', vars[k]);
  return s;
}

const state = { user: null, page: 'dashboard', nodes: [], proxies: [], selected: new Set(), filter: { node: '', tag: '', status: '', q: '' }, timer: null };

// ---------- API ----------
async function api(method, path, body) {
  const opt = { method, headers: {} };
  if (body !== undefined) { opt.headers['Content-Type'] = 'application/json'; opt.body = JSON.stringify(body); }
  const r = await fetch(path, opt);
  let data = null;
  try { data = await r.json(); } catch (e) { /* empty */ }
  if (r.status === 401 && path !== '/api/login') { state.user = null; renderLogin(false); throw new Error(t('login_required')); }
  if (!r.ok) throw new Error((data && data.error) || r.statusText);
  return data;
}

function toast(msg, err) {
  const el = document.createElement('div');
  el.className = 'toast' + (err ? ' err' : '');
  el.textContent = msg;
  document.body.appendChild(el);
  setTimeout(() => el.remove(), err ? 5000 : 2200);
}

async function copyText(text) {
  try { await navigator.clipboard.writeText(text); }
  catch (e) {
    const ta = document.createElement('textarea'); ta.value = text; document.body.appendChild(ta); ta.select();
    document.execCommand('copy'); ta.remove();
  }
  toast(t('copied'));
}

function fmtBytes(n) {
  n = Number(n || 0);
  const u = ['B', 'KB', 'MB', 'GB', 'TB']; let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return (i ? n.toFixed(1) : n) + ' ' + u[i];
}
function fmtTime(ts) {
  if (!ts) return '—';
  return new Date(ts * 1000).toLocaleString(LANG === 'vi' ? 'vi-VN' : 'en-GB');
}
function ago(ts) {
  if (!ts) return t('never');
  const s = Math.max(0, Math.floor(Date.now() / 1000 - ts));
  if (s < 60) return t('sec_ago', { n: s });
  if (s < 3600) return t('min_ago', { n: Math.floor(s / 60) });
  if (s < 86400) return t('hour_ago', { n: Math.floor(s / 3600) });
  return t('day_ago', { n: Math.floor(s / 86400) });
}
const badge = (st) => `<span class="badge ${esc(st)}">${esc(t('st_' + st))}</span>`;

function applyTheme() {
  const th = store('v6p_theme');
  if (th) document.documentElement.setAttribute('data-theme', th); else document.documentElement.removeAttribute('data-theme');
}

// ---------- modal ----------
function modal(html, onMount) {
  closeModal();
  const bg = document.createElement('div');
  bg.className = 'modal-bg'; bg.id = 'modal';
  bg.innerHTML = `<div class="modal" role="dialog">${html}</div>`;
  bg.addEventListener('mousedown', (e) => { if (e.target === bg) closeModal(); });
  document.body.appendChild(bg);
  if (onMount) onMount($('.modal', bg));
  const first = $('input,select,textarea', bg); if (first) first.focus();
}
function closeModal() { const m = $('#modal'); if (m) m.remove(); }
document.addEventListener('keydown', (e) => { if (e.key === 'Escape') closeModal(); });

function confirmBox(msg) {
  return new Promise((resolve) => {
    modal(`<h2>${esc(t('confirm'))}</h2><p>${esc(msg)}</p>
      <div class="actions"><button data-a="no">${t('cancel')}</button><button class="primary" data-a="yes">${t('ok')}</button></div>`,
    (m) => {
      $('[data-a=no]', m).onclick = () => { closeModal(); resolve(false); };
      $('[data-a=yes]', m).onclick = () => { closeModal(); resolve(true); };
    });
  });
}

// ---------- login / setup ----------
function renderLogin(setup) {
  stopTimer();
  $('#app').innerHTML = `<div class="center"><form class="card login" id="loginForm">
    <div class="brand" style="padding:0">open<span>v6</span></div>
    <h2>${setup ? t('setup_title') : t('login_title')}</h2>
    ${setup ? `<p class="muted small">${t('setup_hint')}</p>` : ''}
    <label class="field">${t('username')}<input name="username" autocomplete="username" required value="${setup ? 'admin' : ''}"></label>
    <label class="field">${t('password')}<input name="password" type="password" autocomplete="${setup ? 'new-password' : 'current-password'}" required minlength="${setup ? 8 : 1}"></label>
    <div id="loginErr"></div>
    <button class="primary" type="submit">${setup ? t('create_account') : t('login')}</button>
    <div class="row small"><a href="#" id="langToggle">${LANG === 'vi' ? 'English' : 'Tiếng Việt'}</a></div>
  </form></div>`;
  $('#langToggle').onclick = (e) => { e.preventDefault(); LANG = LANG === 'vi' ? 'en' : 'vi'; store('v6p_lang', LANG); renderLogin(setup); };
  $('#loginForm').onsubmit = async (e) => {
    e.preventDefault();
    const f = new FormData(e.target);
    try {
      const r = await api('POST', setup ? '/api/setup' : '/api/login', { username: f.get('username'), password: f.get('password') });
      state.user = r.user; renderShell();
    } catch (err) { $('#loginErr').innerHTML = `<div class="alert err">${esc(err.message)}</div>`; }
  };
}

// ---------- shell ----------
const PAGES = ['dashboard', 'proxies', 'nodes', 'events', 'settings'];

function renderShell() {
  const page = (location.hash.slice(1) || 'dashboard');
  state.page = PAGES.includes(page) ? page : 'dashboard';
  document.documentElement.lang = LANG;
  $('#app').innerHTML = `<div class="layout">
    <aside class="sidebar">
      <div class="brand">open<span>v6</span></div>
      <nav class="nav">${PAGES.map((p) => `<a href="#${p}" class="${p === state.page ? 'active' : ''}">${t('nav_' + p)}</a>`).join('')}</nav>
      <div class="bottom">
        <button class="small" id="langBtn">${LANG === 'vi' ? 'EN' : 'VI'}</button>
        <button class="small" id="themeBtn" title="${t('theme')}">${t('theme_short')}</button>
        <button class="small" id="logoutBtn">${t('logout')}</button>
      </div>
    </aside>
    <main class="main" id="page"></main>
  </div>`;
  $('#langBtn').onclick = () => { LANG = LANG === 'vi' ? 'en' : 'vi'; store('v6p_lang', LANG); api('PUT', '/api/settings', { lang: LANG }).catch(() => {}); renderShell(); };
  $('#themeBtn').onclick = () => {
    const cur = document.documentElement.getAttribute('data-theme');
    const dark = cur ? cur === 'dark' : matchMedia('(prefers-color-scheme: dark)').matches;
    store('v6p_theme', dark ? 'light' : 'dark'); applyTheme();
  };
  $('#logoutBtn').onclick = async () => { await api('POST', '/api/logout', {}).catch(() => {}); state.user = null; renderLogin(false); };
  renderPage();
}

window.addEventListener('hashchange', () => { if (state.user) { closeModal(); renderShell(); } });

function stopTimer() { if (state.timer) { clearInterval(state.timer); state.timer = null; } }
function every(ms, fn) { stopTimer(); state.timer = setInterval(() => { if (!$('#modal') && !document.hidden) fn(); }, ms); }

function renderPage() {
  stopTimer();
  const fn = { dashboard: pageDashboard, proxies: pageProxies, nodes: pageNodes, events: pageEvents, settings: pageSettings }[state.page];
  fn().catch((e) => { $('#page').innerHTML = `<div class="alert err">${esc(e.message)}</div>`; });
}

// ---------- dashboard ----------
async function pageDashboard() {
  const draw = async () => {
    const [d, nodes, u] = await Promise.all([api('GET', '/api/dashboard'), api('GET', '/api/nodes'), api('GET', '/api/update').catch(() => ({}))]);
    const st = d.status || {};
    const warnings = [];
    for (const n of nodes) {
      if (!n.online) warnings.push(`<div class="alert warn">${esc(t('warn_node_offline', { name: n.name }))}</div>`);
      for (const e of n.errors || []) warnings.push(`<div class="alert err"><b>${esc(n.name)}:</b> ${esc(e)}</div>`);
      if (n.online) for (const c of (n.checks || []).filter((c) => !c.ok && !c.warn)) {
        warnings.push(`<div class="alert err"><b>${esc(n.name)} — ${esc(t('chk_' + c.id))}:</b> ${esc(t('chkfix_' + c.id))} <a href="#nodes">${t('nav_nodes')} →</a></div>`);
      }
    }
    if (u.newer) warnings.unshift(`<div class="alert info">${t('update_available', { v: esc(u.latest) })} <a href="#settings">${t('nav_settings')} →</a></div>`);
    const failing = d.failing || 0;
    if (failing) warnings.push(`<div class="alert err">${t('warn_failing', { n: failing })} <a href="#proxies">${t('nav_proxies')} →</a></div>`);
    if (d.proxies > 1 && d.subnets === 1) warnings.push(`<div class="alert info">${t('warn_same_64')}</div>`);
    $('#page').innerHTML = `<h1>${t('nav_dashboard')}</h1>
      ${warnings.join('')}
      <div class="grid stats">
        <div class="card stat"><div class="label">${t('nodes')}</div><div class="value">${d.nodes_online}/${d.nodes}</div><div class="sub">${t('online')}</div></div>
        <div class="card stat"><div class="label">${t('proxies')}</div><div class="value">${st.ok || 0}/${d.proxies}</div><div class="sub">${t('running')}${st.error ? ' · ' + st.error + ' ' + t('st_error') : ''}</div></div>
        <div class="card stat"><div class="label">${t('distinct_64')}</div><div class="value">${d.subnets}</div><div class="sub">${t('distinct_64_sub')}</div></div>
        <div class="card stat"><div class="label">${t('traffic')}</div><div class="value">${fmtBytes(d.bytes_up + d.bytes_down)}</div><div class="sub">↑ ${fmtBytes(d.bytes_up)} · ↓ ${fmtBytes(d.bytes_down)}</div></div>
        <div class="card stat"><div class="label">${t('active_conns')}</div><div class="value">${d.active}</div><div class="sub">v${esc(d.version)}</div></div>
      </div>
      ${d.proxies === 0 ? `<div class="card" style="margin-bottom:16px"><h2>${t('getting_started')}</h2><ol class="small">
        <li>${t('gs1')}</li><li>${t('gs2')}</li><li>${t('gs3')}</li></ol>
        <a class="btn primary" href="#proxies">${t('create_proxies')}</a></div>` : ''}
      <div class="card"><h2>${t('recent_events')}</h2>${eventsTable(d.events, nodes)}</div>`;
  };
  await draw();
  every(5000, () => draw().catch(() => {}));
}

function eventsTable(events, nodes) {
  if (!events.length) return `<p class="muted">${t('no_events')}</p>`;
  const nm = Object.fromEntries((nodes || []).map((n) => [n.id, n.name]));
  return `<div class="table-wrap" style="border:none"><table><thead><tr><th>${t('time')}</th><th>${t('level')}</th><th>${t('node')}</th><th>${t('message')}</th></tr></thead><tbody>
    ${events.map((e) => `<tr><td class="small">${fmtTime(e.at)}</td><td><span class="badge ${e.level === 'info' ? 'ok' : 'warn'}">${esc(e.level)}</span></td>
    <td class="small">${esc(nm[e.node_id] || '—')}</td><td class="wrap">${esc(e.message)}</td></tr>`).join('')}</tbody></table></div>`;
}

// ---------- proxies ----------
async function loadProxies() {
  const [nodes, proxies] = await Promise.all([api('GET', '/api/nodes'), api('GET', '/api/proxies')]);
  state.nodes = nodes; state.proxies = proxies;
  const ids = new Set(proxies.map((p) => p.id));
  for (const id of [...state.selected]) if (!ids.has(id)) state.selected.delete(id);
}

function filteredProxies() {
  const f = state.filter; const q = f.q.trim().toLowerCase();
  return state.proxies.filter((p) => (!f.node || String(p.node_id) === f.node) && (!f.tag || p.tag === f.tag) &&
    (!f.status || p.status === f.status) &&
    (!q || [p.port, p.label, p.ip, p.auth_user, p.note, p.tag, p.node_name].join(' ').toLowerCase().includes(q)));
}

async function pageProxies() {
  await loadProxies();
  $('#page').innerHTML = `<div class="row" style="margin-bottom:12px"><h1 style="margin:0">${t('nav_proxies')}</h1><div class="spacer"></div>
      <button class="primary" id="btnCreate">+ ${t('create_proxies')}</button></div>
    <div class="toolbar">
      <select id="fNode"></select><select id="fTag"></select>
      <select id="fStatus"><option value="">${t('all_status')}</option>${['ok', 'error', 'pending', 'offline', 'disabled', 'expired'].map((s) => `<option value="${s}">${t('st_' + s)}</option>`).join('')}</select>
      <input id="fQ" placeholder="${t('search')}" style="min-width:180px">
      <div class="spacer"></div>
      <span class="muted small" id="selInfo"></span>
      <select id="bulkAction"><option value="">${t('bulk_actions')}</option>
        <option value="test">${t('act_test')}</option><option value="enable">${t('act_enable')}</option><option value="disable">${t('act_disable')}</option>
        <option value="regen_ip">${t('act_regen_ip')}</option><option value="regen_pass">${t('act_regen_pass')}</option><option value="remove_auth">${t('act_remove_auth')}</option>
        <option value="set_tag">${t('act_set_tag')}</option><option value="set_protocol">${t('act_set_protocol')}</option><option value="set_scope">${t('act_set_scope')}</option>
        <option value="fallback_on">${t('act_fallback_on')}</option><option value="fallback_off">${t('act_fallback_off')}</option>
        <option value="reset_stats">${t('act_reset_stats')}</option><option value="delete">${t('act_delete')}</option></select>
      <button id="btnCheckAll" title="${t('check_all_hint')}">${t('check_all')}</button>
      <button id="btnExport">${t('export')}</button>
    </div>
    <div class="table-wrap"><table><thead><tr>
      <th><input type="checkbox" id="selAll"></th><th>${t('name_port')}</th><th>${t('connect_to')}</th><th>${t('auth')}</th><th>${t('ipv6_out')}</th>
      <th>${t('protocol')}</th><th>${t('status')}</th><th>${t('traffic')}</th><th>${t('conns')}</th><th>${t('tag_note')}</th><th>${t('last_check')}</th><th></th>
    </tr></thead><tbody id="ptbody"></tbody></table></div>`;
  const f = state.filter;
  $('#fQ').value = f.q; $('#fStatus').value = f.status;
  $('#fNode').onchange = (e) => { f.node = e.target.value; drawProxyRows(); };
  $('#fTag').onchange = (e) => { f.tag = e.target.value; drawProxyRows(); };
  $('#fStatus').onchange = (e) => { f.status = e.target.value; drawProxyRows(); };
  $('#fQ').oninput = (e) => { f.q = e.target.value; drawProxyRows(); };
  $('#selAll').onchange = (e) => { for (const p of filteredProxies()) e.target.checked ? state.selected.add(p.id) : state.selected.delete(p.id); drawProxyRows(); };
  $('#btnCreate').onclick = () => openCreate();
  $('#btnExport').onclick = () => openExport();
  $('#btnCheckAll').onclick = async (e) => {
    e.target.disabled = true;
    try { const r = await api('POST', '/api/checks/run', {}); toast(t('check_queued', { n: r.queued })); } catch (err) { toast(err.message, true); }
    setTimeout(() => { e.target.disabled = false; }, 5000);
  };
  $('#bulkAction').onchange = (e) => { const a = e.target.value; e.target.value = ''; if (a) bulkAction(a); };
  drawProxyRows();
  every(5000, async () => { try { await loadProxies(); drawProxyRows(); } catch (e) { /* ignore */ } });
}

// errInfo explains an error code (from the agent) in the user's language,
// with a concrete fix. Falls back to the raw message.
function errInfo(code, raw) {
  if (!code) return esc(raw || '');
  const title = I18N[LANG]['err_' + code] || I18N.en['err_' + code];
  if (!title) return esc(raw || code);
  const fix = I18N[LANG]['errfix_' + code] || I18N.en['errfix_' + code] || '';
  return `<b>${esc(title)}</b>${fix ? `<div>${esc(fix)}</div>` : ''}${raw ? `<details><summary>${t('details')}</summary><code>${esc(raw)}</code></details>` : ''}`;
}

const SCOPES = ['lan', 'local', 'any'];
function scopeSelect(name, value) {
  return `<select name="${name}">${SCOPES.map((v) => `<option value="${v}" ${v === value ? 'selected' : ''}>${t('scope_' + v)}</option>`).join('')}</select>`;
}

function fmtDuration(sec) {
  if (!sec) return '—';
  const d = Math.floor(sec / 86400), h = Math.floor(sec % 86400 / 3600), m = Math.floor(sec % 3600 / 60);
  return d ? `${d}d ${h}h` : h ? `${h}h ${m}m` : `${m}m`;
}

function shortOrg(o) { return String(o || '').replace(/^AS\d+\s+/, '').replace(/ (Company|Corporation|Joint Stock).*$/i, ''); }

function checkCell(p) {
  if (p.check_ok === 1) {
    return `<span title="${esc(p.check_org)} · ${fmtTime(p.check_at)}"><span style="color:var(--ok)">✓</span> ${p.check_latency} ms
      <div class="muted">${esc(p.check_country || '')}${p.check_org ? ' · ' + esc(shortOrg(p.check_org)) : ''} · ${ago(p.check_at)}</div></span>`;
  }
  if (p.check_ok === 0) {
    return `<span><span style="color:var(--err)">✗</span> ${esc(t('check_failed_n', { n: p.fail_streak }))} · ${ago(p.check_at)}<div class="small errbox">${errInfo(p.check_code, p.check_result.replace(/^FAIL /, ''))}</div></span>`;
  }
  return '<span class="muted">—</span>';
}

function drawProxyRows() {
  const f = state.filter;
  const nodeSel = $('#fNode'); if (!nodeSel) return;
  nodeSel.innerHTML = `<option value="">${t('all_nodes')}</option>` + state.nodes.map((n) => `<option value="${n.id}">${esc(n.name)}</option>`).join('');
  nodeSel.value = f.node;
  const tags = [...new Set(state.proxies.map((p) => p.tag).filter(Boolean))].sort();
  $('#fTag').innerHTML = `<option value="">${t('all_tags')}</option>` + tags.map((x) => `<option>${esc(x)}</option>`).join('');
  $('#fTag').value = f.tag;
  const list = filteredProxies();
  $('#selInfo').textContent = state.selected.size ? t('selected_n', { n: state.selected.size }) : '';
  $('#selAll').checked = list.length > 0 && list.every((p) => state.selected.has(p.id));
  if (!list.length) {
    $('#ptbody').innerHTML = `<tr><td colspan="12" class="muted" style="text-align:center;padding:24px">${state.proxies.length ? t('no_match') : t('no_proxies')}</td></tr>`;
    return;
  }
  $('#ptbody').innerHTML = list.map((p) => {
    const hp = (p.host.includes(':') ? `[${p.host}]` : p.host) + ':' + p.port;
    const line = p.auth_user ? `${hp}:${p.auth_user}:${p.auth_pass}` : hp;
    const check = checkCell(p);
    return `<tr data-id="${p.id}">
      <td><input type="checkbox" class="sel" ${state.selected.has(p.id) ? 'checked' : ''}></td>
      <td>${p.label ? `<b>${esc(p.label)}</b><div class="muted small">:${p.port}</div>` : `<b>${p.port}</b>`}</td>
      <td><span class="copy mono" data-copy="${esc(line)}" title="${t('click_copy')}">${esc(hp)}</span><div class="muted small">${esc(p.node_name)}</div></td>
      <td class="mono small">${p.auth_user ? `${esc(p.auth_user)}<br><span class="muted">${esc(p.auth_pass)}</span>` : `<span class="muted">${t('no_auth')}</span>`}</td>
      <td class="mono small"><span class="copy" data-copy="${esc(p.ip)}">${esc(p.ip || '—')}</span>${p.ipv4_fallback ? ` <span class="badge warn" title="${t('fallback_hint')}">v4</span>` : ''}</td>
      <td class="small">${p.protocol === 'both' ? 'HTTP+SOCKS5' : p.protocol.toUpperCase()}</td>
      <td>${badge(p.status)} <span class="badge offline" title="${esc(t('scope_' + p.scope + '_hint'))}">${esc(t('scope_short_' + p.scope))}</span>${p.error ? `<div class="small errbox">${errInfo(p.code, p.error)}</div>` : ''}</td>
      <td class="small">↑${fmtBytes(p.bytes_up)}<br>↓${fmtBytes(p.bytes_down)}</td>
      <td class="small">${p.active} / ${p.conns_total}</td>
      <td class="small wrap" style="max-width:200px">${p.tag ? `<span class="badge offline">${esc(p.tag)}</span> ` : ''}${esc(p.note)}${p.expires_at ? `<div class="muted">${t('expires')}: ${fmtTime(p.expires_at)}</div>` : ''}</td>
      <td class="small">${check}</td>
      <td><div class="row" style="flex-wrap:nowrap"><button class="small" data-act="test">${t('test')}</button><button class="small" data-act="edit">${t('edit')}</button></div></td>
    </tr>`;
  }).join('');
  for (const tr of $$('#ptbody tr[data-id]')) {
    const id = Number(tr.dataset.id);
    $('.sel', tr).onchange = (e) => { e.target.checked ? state.selected.add(id) : state.selected.delete(id); $('#selInfo').textContent = state.selected.size ? t('selected_n', { n: state.selected.size }) : ''; };
    $('[data-act=edit]', tr).onclick = () => openEdit(state.proxies.find((p) => p.id === id));
    $('[data-act=test]', tr).onclick = (e) => testProxies([id], e.target);
  }
  for (const el of $$('#ptbody [data-copy]')) el.onclick = () => copyText(el.dataset.copy);
}

async function pollCommand(id) {
  for (let i = 0; i < 40; i++) {
    await new Promise((r) => setTimeout(r, 1500));
    const c = await api('GET', '/api/commands/' + id);
    if (c.done) return c;
  }
  return { done: true, ok: false, output: 'timeout' };
}

async function testProxies(ids, btn) {
  if (btn) { btn.disabled = true; btn.textContent = '…'; }
  try {
    const r = await api('POST', '/api/proxies/action', { ids, action: 'test' });
    const results = await Promise.all(r.commands.map((c) => pollCommand(c.id)));
    const ok = results.filter((x) => x.ok).length;
    if (ids.length === 1) toast(results[0].ok ? t('test_ok', { ip: results[0].output }) : t('test_fail', { err: results[0].output }), !results[0].ok);
    else toast(t('test_summary', { ok, total: ids.length }), ok !== ids.length);
    await loadProxies(); drawProxyRows();
  } catch (e) { toast(e.message, true); }
}

function chooseScope() {
  return new Promise((resolve) => {
    modal(`<h2>${t('act_set_scope')}</h2>
      ${SCOPES.map((v) => `<label class="check" style="align-items:flex-start;margin-bottom:10px"><input type="radio" name="sc" value="${v}" ${v === 'lan' ? 'checked' : ''}>
        <span><b>${t('scope_' + v)}</b><div class="hint">${t('scope_' + v + '_hint')}</div></span></label>`).join('')}
      <div class="actions"><button id="no">${t('cancel')}</button><button class="primary" id="yes">${t('ok')}</button></div>`, (m) => {
      $('#no', m).onclick = () => { closeModal(); resolve(''); };
      $('#yes', m).onclick = () => { const v = $('input[name=sc]:checked', m).value; closeModal(); resolve(v); };
    });
  });
}

async function bulkAction(action) {
  const ids = [...state.selected];
  if (!ids.length) { toast(t('select_first'), true); return; }
  if (action === 'test') return testProxies(ids);
  let value = '';
  if (action === 'set_tag') { value = prompt(t('act_set_tag')); if (value === null) return; }
  if (action === 'set_protocol') { value = prompt('both / http / socks5', 'both'); if (value === null) return; }
  if (action === 'set_scope') { value = await chooseScope(); if (!value) return; }
  if (action === 'delete' && !(await confirmBox(t('confirm_delete', { n: ids.length })))) return;
  if (action === 'regen_ip' && !(await confirmBox(t('confirm_regen_ip', { n: ids.length })))) return;
  if (action === 'regen_pass' && !(await confirmBox(t('confirm_regen_pass', { n: ids.length })))) return;
  try {
    const r = await api('POST', '/api/proxies/action', { ids, action, value });
    toast(t('done_n', { n: r.affected }));
    if (action === 'delete') state.selected.clear();
    await loadProxies(); drawProxyRows();
  } catch (e) { toast(e.message, true); }
}

function poolInfo(pool) {
  if (!pool.cidr) return t('pool_waiting');
  const bits = Number(pool.cidr.split('/')[1]);
  const n = bits >= 64 ? 1 : Math.pow(2, 64 - bits);
  return `${pool.cidr} · ${n.toLocaleString()} × /64 · ${pool.mode}`;
}

function openCreate() {
  if (!state.nodes.length) { toast(t('no_nodes'), true); return; }
  modal(`<h2>${t('create_proxies')}</h2>
    <form id="cf"><div class="form-grid">
      <label class="field">${t('node')}<select name="node_id">${state.nodes.map((n) => `<option value="${n.id}">${esc(n.name)}${n.online ? '' : ' (' + t('st_offline') + ')'}</option>`).join('')}</select></label>
      <label class="field">${t('pool')}<select name="pool_id"></select></label>
      <label class="field">${t('count')}<input name="count" type="number" min="1" max="5000" value="10" required></label>
      <label class="field">${t('start_port')}<input name="start_port" type="number" min="1024" max="65535" value="10001"></label>
      <label class="field">${t('protocol')}<select name="protocol"><option value="both">HTTP + SOCKS5</option><option value="http">HTTP</option><option value="socks5">SOCKS5</option></select></label>
      <label class="field">${t('auth')}<select name="auth"><option value="random">${t('auth_random')}</option><option value="none">${t('auth_none')}</option><option value="fixed">${t('auth_fixed')}</option></select></label>
      <label class="field fixed-auth" hidden>${t('username')}<input name="user"></label>
      <label class="field fixed-auth" hidden>${t('password')}<input name="pass"></label>
      <label class="field">${t('scope')}${scopeSelect('scope', 'lan')}</label>
      <label class="field">${t('label_prefix')}<input name="label_prefix" placeholder="FB, TikTok…"></label>
      <label class="field">${t('tag')}<input name="tag" placeholder="facebook, tiktok…"></label>
      <label class="field">${t('note')}<input name="note"></label>
      <label class="field">${t('expires_days')}<input name="expires_days" type="number" min="0" value="0"></label>
    </div>
    <p class="hint" id="poolHint"></p>
    <div id="spreadWrap"><label class="check"><input type="checkbox" name="spread" checked> ${t('spread')}</label>
    <p class="hint">${t('spread_hint')}</p></div>
    <label class="check"><input type="checkbox" name="ipv4_fallback"> ${t('ipv4_fallback')}</label>
    <p class="hint">${t('fallback_hint')}</p>
    <p class="hint" id="authHint">${t('auth_none_hint')}</p>
    <p class="hint" id="scopeHint"></p>
    <div id="cfErr"></div>
    <div class="actions"><button type="button" id="cancel">${t('cancel')}</button><button class="primary" type="submit">${t('create')}</button></div></form>`,
  (m) => {
    const form = $('#cf', m);
    const fillPools = () => {
      const node = state.nodes.find((n) => String(n.id) === form.node_id.value);
      form.pool_id.innerHTML = (node.pools || []).map((p) => `<option value="${p.id}">${esc(p.name)} — ${esc(p.cidr || t('pool_waiting'))}</option>`).join('');
      onPool();
    };
    const onPool = () => {
      const node = state.nodes.find((n) => String(n.id) === form.node_id.value);
      const pool = (node.pools || []).find((p) => String(p.id) === form.pool_id.value);
      $('#poolHint', m).textContent = pool ? poolInfo(pool) : '';
      const many = pool && pool.cidr && Number(pool.cidr.split('/')[1]) < 64;
      $('#spreadWrap', m).style.display = many ? '' : 'none';
      form.spread.checked = !!many;
    };
    const onAuth = () => {
      for (const el of $$('.fixed-auth', m)) el.hidden = form.auth.value !== 'fixed';
      $('#authHint', m).style.display = form.auth.value === 'none' ? '' : 'none';
      $('#scopeHint', m).innerHTML = esc(t('scope_' + form.scope.value + '_hint')) +
        (form.scope.value === 'any' && form.auth.value === 'none' ? `<div style="color:var(--err)">${esc(t('scope_any_needs_auth'))}</div>` : '');
    };
    if (state.filter.node) form.node_id.value = state.filter.node;
    form.node_id.onchange = fillPools; form.pool_id.onchange = onPool; form.auth.onchange = onAuth; form.scope.onchange = onAuth;
    fillPools(); onAuth();
    $('#cancel', m).onclick = closeModal;
    form.onsubmit = async (e) => {
      e.preventDefault();
      const body = {
        node_id: Number(form.node_id.value), pool_id: Number(form.pool_id.value), count: Number(form.count.value),
        start_port: Number(form.start_port.value), protocol: form.protocol.value, auth: form.auth.value,
        user: form.user.value, pass: form.pass.value, tag: form.tag.value, label_prefix: form.label_prefix.value, note: form.note.value,
        expires_days: Number(form.expires_days.value), spread: form.spread.checked, ipv4_fallback: form.ipv4_fallback.checked,
        scope: form.scope.value,
      };
      try {
        const r = await api('POST', '/api/proxies/bulk', body);
        closeModal(); toast(t('created_n', { n: r.created }));
        await loadProxies(); drawProxyRows();
      } catch (err) { $('#cfErr', m).innerHTML = `<div class="alert err">${esc(err.message)}</div>`; }
    };
  });
}

function openEdit(p) {
  const exp = p.expires_at ? new Date(p.expires_at * 1000).toISOString().slice(0, 10) : '';
  modal(`<h2>${t('edit')} — ${t('port')} ${p.port}</h2>
    <form id="ef"><div class="form-grid">
      <label class="field">${t('label')}<input name="label" value="${esc(p.label)}" placeholder="${t('label_ph')}"></label>
      <label class="field">${t('port')}<input name="port" type="number" min="1024" max="65535" value="${p.port}"></label>
      <label class="field">${t('protocol')}<select name="protocol"><option value="both">HTTP + SOCKS5</option><option value="http">HTTP</option><option value="socks5">SOCKS5</option></select></label>
      <label class="field">${t('username')}<input name="auth_user" value="${esc(p.auth_user)}"></label>
      <label class="field">${t('password')}<input name="auth_pass" value="${esc(p.auth_pass)}"></label>
      <label class="field">${t('scope')}${scopeSelect('scope', p.scope)}</label>
      <label class="field">${t('tag')}<input name="tag" value="${esc(p.tag)}"></label>
      <label class="field">${t('expires')}<input name="expires" type="date" value="${exp}"></label>
    </div>
    <label class="field" style="margin-top:12px">${t('ipv6_out')} <input name="ip" class="mono" value="${esc(p.ip)}"></label>
    <p class="hint">${t('ip_edit_hint')}</p>
    <label class="field">${t('note')}<textarea name="note" rows="2">${esc(p.note)}</textarea></label>
    <div class="row" style="margin-top:8px;gap:16px">
      <label class="check"><input type="checkbox" name="enabled" ${p.enabled ? 'checked' : ''}> ${t('enabled')}</label>
      <label class="check"><input type="checkbox" name="ipv4_fallback" ${p.ipv4_fallback ? 'checked' : ''}> ${t('ipv4_fallback')}</label>
    </div>
    <h3>${t('check_history')}</h3><div id="hist" class="small muted">…</div>
    <div id="efErr"></div>
    <div class="actions"><button type="button" class="danger" id="del">${t('act_delete')}</button><div class="spacer"></div>
      <button type="button" id="cancel">${t('cancel')}</button><button class="primary" type="submit">${t('save')}</button></div></form>`,
  (m) => {
    const form = $('#ef', m);
    form.protocol.value = p.protocol;
    api('GET', `/api/proxies/${p.id}/checks`).then((h) => {
      $('#hist', m).innerHTML = h.length ? `<div class="table-wrap"><table><tbody>${h.slice(0, 10).map((c) => `<tr><td>${fmtTime(c.at)}</td>
        <td class="wrap">${c.ok ? '<span style="color:var(--ok)">✓</span> ' + c.latency + ' ms' : '<span style="color:var(--err)">✗</span> ' + errInfo(c.code, c.error)}</td></tr>`).join('')}</tbody></table></div>` : t('never');
    }).catch(() => {});
    $('#cancel', m).onclick = closeModal;
    $('#del', m).onclick = async () => {
      if (!(await confirmBox(t('confirm_delete', { n: 1 })))) return;
      await api('POST', '/api/proxies/action', { ids: [p.id], action: 'delete' }); toast(t('done_n', { n: 1 }));
      await loadProxies(); drawProxyRows();
    };
    form.onsubmit = async (e) => {
      e.preventDefault();
      const body = {
        label: form.label.value, port: Number(form.port.value), protocol: form.protocol.value, auth_user: form.auth_user.value, auth_pass: form.auth_pass.value,
        tag: form.tag.value, note: form.note.value, enabled: form.enabled.checked, ipv4_fallback: form.ipv4_fallback.checked,
        scope: form.scope.value,
        expires_at: form.expires.value ? Math.floor(new Date(form.expires.value + 'T23:59:59').getTime() / 1000) : 0,
      };
      if (form.ip.value.trim() && form.ip.value.trim() !== p.ip) body.ip = form.ip.value.trim();
      try {
        await api('PUT', '/api/proxies/' + p.id, body);
        closeModal(); toast(t('saved')); await loadProxies(); drawProxyRows();
      } catch (err) { $('#efErr', m).innerHTML = `<div class="alert err">${esc(err.message)}</div>`; }
    };
  });
}

// Formats each anti-detect browser accepts when pasting a proxy list (from
// public guides; users are told to try a few proxies first).
const EXPORT_PRESETS = {
  custom: { name: '' },
  multilogin: { name: 'Multilogin', format: 'host:port:user:pass', scheme: 'http' },
  gologin: { name: 'GoLogin', format: 'scheme://host:port:user:pass', scheme: 'socks5' },
  dolphin: { name: 'Dolphin {Anty}', format: 'url', scheme: 'socks5' },
  ixbrowser: { name: 'ixBrowser', format: 'host:port:user:pass', scheme: 'socks5' },
  sphere: { name: 'Linken Sphere 2', format: 'host:port@user:pass', scheme: 'socks5' },
  adspower: { name: 'AdsPower', format: 'host:port:user:pass', scheme: 'socks5' },
};

function openExport() {
  const ids = state.selected.size ? [...state.selected] : filteredProxies().map((p) => p.id);
  if (!ids.length) { toast(t('no_proxies'), true); return; }
  modal(`<h2>${t('export')} (${ids.length})</h2>
    <div class="form-grid">
      <label class="field">${t('export_for')}<select id="xp">${Object.keys(EXPORT_PRESETS).map((k) => `<option value="${k}">${esc(EXPORT_PRESETS[k].name || t('custom'))}</option>`).join('')}</select></label>
      <label class="field">${t('format')}<select id="xf">
        <option value="host:port:user:pass">host:port:user:pass</option>
        <option value="user:pass@host:port">user:pass@host:port</option>
        <option value="url">scheme://user:pass@host:port</option>
        <option value="scheme://host:port:user:pass">scheme://host:port:user:pass</option>
        <option value="host:port@user:pass">host:port@user:pass</option>
        <option value="csv">CSV</option><option value="json">JSON</option></select></label>
      <label class="field">${t('scheme')}<select id="xs"><option value="http">http</option><option value="socks5">socks5</option></select></label>
      <label class="field">${t('host_override')}<input id="xh" placeholder="${t('auto')}"></label>
    </div>
    <p class="hint">${t('export_hint')}</p>
    <p class="hint" id="xnote"></p>
    <textarea id="xt" rows="12" readonly></textarea>
    <div class="actions"><button id="xd">${t('download')}</button><button class="primary" id="xc">${t('copy')}</button><button id="cancel">${t('close')}</button></div>`,
  (m) => {
    const applyPreset = () => {
      const pr = EXPORT_PRESETS[$('#xp', m).value];
      if (pr.format) { $('#xf', m).value = pr.format; $('#xs', m).value = pr.scheme; }
      $('#xf', m).disabled = $('#xs', m).disabled = !!pr.format;
      $('#xnote', m).textContent = pr.format ? t('preset_note') : '';
    };
    const saved = store('v6p_export_preset');
    if (saved && EXPORT_PRESETS[saved]) $('#xp', m).value = saved;
    const fmt = store('v6p_export_fmt'); if (fmt && !EXPORT_PRESETS[$('#xp', m).value].format) $('#xf', m).value = fmt;
    applyPreset();
    $('#xp', m).onchange = () => { applyPreset(); load(); };
    const load = async () => {
      store('v6p_export_preset', $('#xp', m).value);
      store('v6p_export_fmt', $('#xf', m).value);
      try {
        const r = await api('POST', '/api/proxies/export', { ids, format: $('#xf', m).value, scheme: $('#xs', m).value, host: $('#xh', m).value });
        $('#xt', m).value = r.text;
      } catch (e) { $('#xt', m).value = e.message; }
    };
    $('#xf', m).onchange = load; $('#xs', m).onchange = load; $('#xh', m).oninput = load;
    $('#xc', m).onclick = () => copyText($('#xt', m).value);
    $('#xd', m).onclick = () => {
      const f = $('#xf', m).value; const ext = f === 'csv' ? 'csv' : f === 'json' ? 'json' : 'txt';
      const a = document.createElement('a');
      a.href = URL.createObjectURL(new Blob([$('#xt', m).value], { type: 'text/plain' }));
      a.download = 'proxies.' + ext; a.click();
    };
    $('#cancel', m).onclick = closeModal;
    load();
  });
}

// ---------- nodes ----------
async function pageNodes() {
  const draw = async () => {
    const nodes = await api('GET', '/api/nodes');
    state.nodes = nodes;
    $('#page').innerHTML = `<div class="row" style="margin-bottom:12px"><h1 style="margin:0">${t('nav_nodes')}</h1><div class="spacer"></div>
      <button class="primary" id="btnAddNode">+ ${t('add_node')}</button></div>
      <p class="muted small">${t('nodes_hint')}</p>
      ${nodes.map(nodeCard).join('')}`;
    $('#btnAddNode').onclick = openAddNode;
    for (const card of $$('.node-card')) bindNodeCard(card, nodes.find((n) => n.id === Number(card.dataset.id)));
  };
  await draw();
  every(10000, () => { if (!document.activeElement || !document.activeElement.closest('.node-card')) draw().catch(() => {}); });
}

function dnsMode(v) { return !v || v === 'system' ? 'system' : (['cloudflare', 'google', 'quad9'].includes(v) ? v : 'custom'); }

function vpnSummary(n) {
  const v = (n.net.vpns || []).filter((x) => x.up);
  if (!v.length) return `<span class="muted">${t('vpn_none')}</span>`;
  const captured = n.net.egress_iface && n.net.primary_iface && n.net.egress_iface !== n.net.primary_iface;
  const names = v.map((x) => `${t('vpnkind_' + x.kind)} (${esc(x.iface)})`).join(', ');
  let state = '';
  if (captured) {
    state = n.net.bypass === 'ok' ? `<span class="badge ok">${t('vpn_bypass_ok')}</span>` :
      n.net.bypass === 'error' ? `<span class="badge error">${t('vpn_bypass_err')}</span>` : `<span class="badge error">${t('vpn_bypass_off')}</span>`;
  } else {
    state = `<span class="badge offline">${t('vpn_not_capturing')}</span>`;
  }
  return `${names} ${state}`;
}

function nodeCard(n) {
  const ifaces = (n.net.interfaces || []);
  const pools = (n.pools || []).map((p) => `<tr data-pool="${p.id}"><td>${esc(p.name)}${p.auto ? ` <span class="badge offline">auto</span>` : ''}</td>
    <td class="mono small">${esc(p.cidr || t('pool_waiting'))}</td><td class="small">${esc(p.mode)}</td>
    <td>${p.auto ? '' : `<button class="small danger" data-delpool="${p.id}">${t('act_delete')}</button>`}</td></tr>`).join('');
  return `<div class="card node-card" data-id="${n.id}">
    <div class="head"><h2 style="margin:0">${esc(n.name)}</h2>${n.online ? badge('ok').replace(t('st_ok'), t('online')) : badge('offline')}
      ${n.is_local ? `<span class="badge offline">${t('local')}</span>` : ''}
      <div class="spacer"></div>
      <button class="small" data-a="test">${t('test_v6')}</button>
      <button class="small" data-a="logs">${t('logs')}</button>
      <button class="small" data-a="reapply" title="${t('reapply_hint')}">${t('reapply')}</button>
      <button class="small" data-a="restart">${t('restart')}</button>
      ${n.outdated ? `<button class="small primary" data-a="update">${t('update_node', { v: esc(n.agent_version) })}</button>` : ''}
      ${n.is_local ? '' : `<button class="small" data-a="install">${t('install_cmd')}</button><button class="small" data-a="rotate">${t('rotate_token')}</button><button class="small danger" data-a="delete">${t('act_delete')}</button>`}
    </div>
    ${(n.errors || []).map((e) => `<div class="alert err">${esc(e)}</div>`).join('')}

    <div class="kv">
      <div>${t('hostname')}</div><div>${esc(n.hostname || '—')} <span class="muted small">${esc(n.os || '')} ${n.agent_version ? 'v' + esc(n.agent_version) : ''}</span></div>
      <div>${t('last_seen')}</div><div>${ago(n.last_seen)}</div>
      <div>${t('service')}</div><div>${n.online ? t('running_for', { d: fmtDuration(n.uptime) }) : t('st_offline')}</div>
      <div>${t('vpn')}</div><div>${vpnSummary(n)}</div>
      <div>${t('ipv6_iface')}</div><div>${esc(n.net.primary_iface || '—')}${n.net.gateway ? ` <span class="muted small">${t('router')}: ${esc(n.net.gateway)}</span>` : ''} ${ifaces.map((i) => `<div class="mono small">${esc(i.name)}: ${esc((i.prefixes || []).join(', ') || '—')}</div>`).join('')}</div>
      <div>${t('ipv4')}</div><div class="mono small">LAN: ${esc((n.net.lan_v4 || []).join(', ') || '—')} · WAN: ${esc(n.net.public_v4 || '—')}</div>
      <div>${t('proxies')}</div><div>${n.proxies}</div>
      <div>${t('system_checks')}</div><div class="checks">${(n.checks || []).map((c) => `<span class="badge ${c.ok ? 'ok' : (c.warn ? 'warn' : 'error')}" title="${esc(c.detail)}">${c.ok ? '✓' : (c.warn ? '!' : '✗')} ${esc(t('chk_' + c.id))}</span>`).join(' ') || '—'}
        ${(n.checks || []).filter((c) => !c.ok).map((c) => `<div class="hint">${esc(t('chk_' + c.id))}: ${esc(t('chkfix_' + c.id))}${c.detail ? ' (' + esc(c.detail) + ')' : ''}</div>`).join('')}</div>
    </div>
    <h3>${t('settings')}</h3>
    <form class="form-grid" data-form="node">
      <label class="field">${t('name')}<input name="name" value="${esc(n.name)}"></label>
      <label class="field">${t('entry_host')}<input name="entry_host" value="${esc(n.entry_host)}" placeholder="${esc(n.entry)}"></label>
      <label class="field">${t('iface')}<select name="iface"><option value="">${t('auto')} (${esc(n.net.primary_iface || '?')})</option>
        ${ifaces.map((i) => `<option ${i.name === n.iface ? 'selected' : ''}>${esc(i.name)}</option>`).join('')}</select></label>
      <label class="field">${t('dns')}<select name="dns_mode">
        ${['system', 'cloudflare', 'google', 'quad9', 'custom'].map((v) => `<option value="${v}" ${dnsMode(n.dns) === v ? 'selected' : ''}>${t('dns_' + v)}</option>`).join('')}</select></label>
      <label class="field" data-dns-custom ${dnsMode(n.dns) === 'custom' ? '' : 'hidden'}>${t('dns_servers')}<input name="dns_custom" value="${dnsMode(n.dns) === 'custom' ? esc(n.dns) : ''}" placeholder="2606:4700:4700::1111, 1.1.1.1"></label>
      <div style="grid-column:1/-1"><label class="check"><input type="checkbox" name="bypass" ${n.bypass ? 'checked' : ''}> ${t('bypass')}</label>
        <div class="hint">${t('bypass_hint')}</div><div class="hint">${t('dns_hint')}</div></div>
      <label class="field" style="grid-column:1/-1">${t('allow_cidrs')}<textarea name="allow_cidrs" rows="3">${esc(n.allow_cidrs)}</textarea>
        <span class="hint">${t('allow_hint')}</span></label>
      <div><button class="primary" type="submit">${t('save')}</button></div>
    </form>
    <h3>${t('pools')}</h3>
    <div class="table-wrap"><table><thead><tr><th>${t('name')}</th><th>CIDR</th><th>${t('mode')}</th><th></th></tr></thead><tbody>${pools}</tbody></table></div>
    <form class="row" data-form="pool" style="margin-top:8px">
      <input name="cidr" placeholder="2a01:4f8:abcd::/48" class="mono" required style="min-width:240px">
      <select name="mode"><option value="anyip">anyip (${t('routed')})</option><option value="addr">addr (/64 on-link)</option></select>
      <input name="name" placeholder="${t('name')}">
      <button type="submit">+ ${t('add_pool')}</button>
    </form>
    <p class="hint">${t('pool_hint')}</p>
  </div>`;
}

function bindNodeCard(card, n) {
  const act = (a) => $(`[data-a=${a}]`, card);
  act('test').onclick = async (e) => {
    e.target.disabled = true;
    try { const c = await api('POST', `/api/nodes/${n.id}/test`, {}); const r = await pollCommand(c.id); toast(r.ok ? t('v6_ok', { ip: r.output }) : t('test_fail', { err: r.output }), !r.ok); }
    catch (err) { toast(err.message, true); }
    e.target.disabled = false;
  };
  act('logs').onclick = () => openLogs(n);
  act('reapply').onclick = async () => {
    if (!(await confirmBox(t('confirm_reapply')))) return;
    try { await api('POST', `/api/nodes/${n.id}/reapply`, {}); toast(t('reapply_sent')); } catch (err) { toast(err.message, true); }
  };
  act('restart').onclick = async () => {
    if (!(await confirmBox(n.is_local ? t('confirm_restart_panel') : t('confirm_restart_node')))) return;
    try {
      const r = await api('POST', `/api/nodes/${n.id}/restart`, {});
      toast(t('restart_sent'));
      if (r.panel) setTimeout(() => location.reload(), 6000);
    } catch (err) { toast(err.message, true); }
  };
  const fdns = $('[data-form=node]', card);
  fdns.dns_mode.onchange = () => { $('[data-dns-custom]', card).hidden = fdns.dns_mode.value !== 'custom'; };
  if (act('update')) act('update').onclick = async (e) => {
    e.target.disabled = true;
    try { await api('POST', `/api/nodes/${n.id}/update`, {}); toast(t('update_sent')); } catch (err) { toast(err.message, true); }
  };
  if (act('install')) act('install').onclick = async () => showInstall(await api('GET', `/api/nodes/${n.id}/install`), n.name);
  if (act('rotate')) act('rotate').onclick = async () => { if (await confirmBox(t('confirm_rotate'))) showInstall(await api('POST', `/api/nodes/${n.id}/rotate-token`, {}), n.name); };
  if (act('delete')) act('delete').onclick = async () => {
    if (!(await confirmBox(t('confirm_delete_node', { name: n.name, n: n.proxies })))) return;
    try { await api('DELETE', '/api/nodes/' + n.id); renderPage(); } catch (e) { toast(e.message, true); }
  };
  $('[data-form=node]', card).onsubmit = async (e) => {
    e.preventDefault(); const f = e.target;
    const dns = f.dns_mode.value === 'custom' ? f.dns_custom.value : f.dns_mode.value;
    try {
      await api('PUT', '/api/nodes/' + n.id, { name: f.name.value, entry_host: f.entry_host.value, iface: f.iface.value, allow_cidrs: f.allow_cidrs.value,
        bypass: f.bypass.checked, dns });
      toast(t('saved')); renderPage();
    }
    catch (err) { toast(err.message, true); }
  };
  $('[data-form=pool]', card).onsubmit = async (e) => {
    e.preventDefault(); const f = e.target;
    try { await api('POST', '/api/pools', { node_id: n.id, cidr: f.cidr.value, mode: f.mode.value, name: f.name.value }); toast(t('saved')); renderPage(); }
    catch (err) { toast(err.message, true); }
  };
  for (const b of $$('[data-delpool]', card)) b.onclick = async () => {
    if (!(await confirmBox(t('confirm_delete_pool')))) return;
    try { await api('DELETE', '/api/pools/' + b.dataset.delpool); renderPage(); } catch (err) { toast(err.message, true); }
  };
}

function openLogs(n) {
  modal(`<div class="row"><h2 style="margin:0">${t('logs')} — ${esc(n.name)}</h2><div class="spacer"></div>
      <input id="lq" placeholder="${t('filter')}" style="width:160px"></div>
    <pre id="logbox" class="cmd" style="height:420px;overflow:auto;white-space:pre-wrap;margin-top:12px"></pre>
    <div class="actions"><label class="check small"><input type="checkbox" id="lauto" checked> ${t('auto_refresh')}</label><div class="spacer"></div>
      <button id="lcopy">${t('copy')}</button><button id="lclose">${t('close')}</button></div>`,
  (m) => {
    $('.modal', document).style.maxWidth = '960px';
    const box = $('#logbox', m);
    const load = async () => {
      if (!document.body.contains(box)) return;
      const r = await api('GET', `/api/nodes/${n.id}/logs?n=1000&q=${encodeURIComponent($('#lq', m).value)}`);
      const atBottom = box.scrollTop + box.clientHeight >= box.scrollHeight - 20;
      box.textContent = r.lines.join('\n') || t('no_logs');
      if (atBottom) box.scrollTop = box.scrollHeight;
    };
    load().then(() => { box.scrollTop = box.scrollHeight; });
    const timer = setInterval(() => { if (!document.body.contains(box)) clearInterval(timer); else if ($('#lauto', m).checked) load().catch(() => {}); }, 3000);
    $('#lq', m).oninput = () => load();
    $('#lcopy', m).onclick = () => copyText(box.textContent);
    $('#lclose', m).onclick = closeModal;
  });
}

function openAddNode() {
  modal(`<h2>${t('add_node')}</h2><form id="nf">
    <label class="field">${t('name')}<input name="name" required placeholder="VPS Hetzner, Mini PC…"></label>
    <label class="field" style="margin-top:8px">${t('entry_host')}<input name="entry_host" placeholder="${t('auto')}"></label>
    <p class="hint">${t('entry_hint')}</p>
    <div id="nfErr"></div>
    <div class="actions"><button type="button" id="cancel">${t('cancel')}</button><button class="primary" type="submit">${t('create')}</button></div></form>`,
  (m) => {
    $('#cancel', m).onclick = closeModal;
    $('#nf', m).onsubmit = async (e) => {
      e.preventDefault();
      try {
        const n = await api('POST', '/api/nodes', { name: e.target.name.value, entry_host: e.target.entry_host.value });
        showInstall(await api('GET', `/api/nodes/${n.id}/install`), n.name);
      } catch (err) { $('#nfErr', m).innerHTML = `<div class="alert err">${esc(err.message)}</div>`; }
    };
  });
}

function showInstall(r, name) {
  modal(`<h2>${t('install_on', { name: esc(name) })}</h2>
    <p class="small">${t('install_hint')}</p>
    <div class="cmd" id="cmd">${esc(r.command)}</div>
    <p class="hint">${t('install_hint2')}</p>
    <div class="actions"><button class="primary" id="cp">${t('copy')}</button><button id="cl">${t('close')}</button></div>`,
  (m) => {
    $('#cp', m).onclick = () => copyText(r.command);
    $('#cl', m).onclick = () => { closeModal(); if (state.page === 'nodes') renderPage(); };
  });
}

// ---------- events ----------
async function pageEvents() {
  const [ev, nodes] = await Promise.all([api('GET', '/api/events'), api('GET', '/api/nodes')]);
  $('#page').innerHTML = `<h1>${t('nav_events')}</h1><div class="card">${eventsTable(ev, nodes)}</div>`;
}

// ---------- settings ----------
async function pageSettings() {
  const [s, u] = await Promise.all([api('GET', '/api/settings'), api('GET', '/api/update').catch(() => ({}))]);
  const tog = (k) => `<label class="check"><input type="checkbox" name="${k}" ${s[k] === '1' ? 'checked' : ''}> ${t(k)}</label>`;
  $('#page').innerHTML = `<h1>${t('nav_settings')}</h1>
    <div class="grid" style="grid-template-columns:repeat(auto-fit,minmax(340px,1fr))">

    <div class="card" id="upd"><h2>${t('updates')}</h2>
      <div class="kv"><div>${t('current_version')}</div><div>v${esc(u.current || '')}</div>
        <div>${t('latest_version')}</div><div>${u.latest ? esc(u.latest) : (u.error ? `<span class="muted">${esc(u.error)}</span>` : '—')}</div></div>
      ${u.newer ? `<div class="alert info" style="margin-top:12px">${t('update_available', { v: esc(u.latest) })}</div>` : `<p class="small muted">${u.latest ? t('up_to_date') : ''}</p>`}
      ${u.newer && !u.managed ? `<p class="hint">${t('update_not_managed')}</p>` : ''}
      ${(u.outdated_nodes || []).length ? `<p class="small">${t('outdated_nodes', { list: esc(u.outdated_nodes.join(', ')) })}</p>` : ''}
      <div class="row" style="margin-top:8px">
        ${u.newer && u.managed ? `<button class="primary" id="btnUpd">${t('update_now', { v: esc(u.latest) })}</button>` : ''}
        ${(u.outdated_nodes || []).length ? `<button id="btnUpdNodes">${t('update_nodes')}</button>` : ''}
        <button id="btnUpdCheck">${t('check_updates')}</button>
        ${u.url ? `<a class="btn" href="${esc(u.url)}" target="_blank" rel="noopener">${t('release_notes')}</a>` : ''}
      </div></div>

    <form class="card" id="tgf"><h2>${t('telegram')}</h2>
      <p class="hint" style="margin-top:0">${t('telegram_hint')}</p>
      <label class="field">${t('telegram_token')}<input name="telegram_token" autocomplete="off" placeholder="${s.telegram_token_set ? t('telegram_token_saved') : '123456:ABC…'}"></label>
      <label class="field" style="margin-top:8px">${t('telegram_chat')}<input name="telegram_chat" value="${esc(s.telegram_chat)}" placeholder="123456789"></label>
      <h3>${t('notify_when')}</h3>
      <div style="display:grid;gap:6px">${tog('notify_node_offline')}${tog('notify_proxy_fail')}${tog('notify_prefix_change')}${tog('notify_update')}</div>
      <div class="row" style="margin-top:12px"><button class="primary" type="submit">${t('save')}</button><button type="button" id="tgTest">${t('send_test')}</button></div></form>

    <form class="card" id="opf"><h2>${t('operation')}</h2>
      <label class="field">${t('check_interval')}<select name="check_interval_min">
        ${[0, 5, 10, 15, 30, 60].map((v) => `<option value="${v}" ${String(v) === s.check_interval_min ? 'selected' : ''}>${v ? t('every_n_min', { n: v }) : t('off')}</option>`).join('')}</select></label>
      <p class="hint">${t('check_interval_hint')}</p>
      <label class="check" style="margin-top:8px"><input type="checkbox" name="auto_firewall" ${s.auto_firewall === '1' ? 'checked' : ''}> ${t('auto_firewall')}</label>
      <p class="hint">${t('auto_firewall_hint')}</p>
      <label class="field" style="margin-top:8px">${t('instance_name')}<input name="instance_name" value="${esc(s.instance_name)}" placeholder="${t('instance_name_ph')}"></label>
      <label class="field" style="margin-top:8px">${t('panel_url')}<input name="panel_url" value="${esc(s.panel_url)}" placeholder="${esc(s.detected_url)}"></label>
      <p class="hint">${t('panel_url_hint')}</p>
      <button class="primary" type="submit">${t('save')}</button></form>

    <div class="card"><h2>${t('backup')}</h2>
      <p class="small muted" style="margin-top:0">${t('backup_hint')}</p>
      <div class="row"><a class="btn primary" href="/api/backup" download>${t('download_backup')}</a>
        <label class="btn">${t('restore')}<input type="file" id="restoreFile" accept=".json,application/json" hidden></label></div></div>

    <form class="card" id="pf"><h2>${t('change_password')}</h2>
      <label class="field">${t('current_password')}<input name="current" type="password" autocomplete="current-password" required></label>
      <label class="field" style="margin-top:8px">${t('new_password')}<input name="new" type="password" minlength="8" autocomplete="new-password" required></label>
      <div style="margin-top:12px"><button class="primary" type="submit">${t('save')}</button></div></form>

    <div class="card"><h2>${t('language')}</h2>
      <div class="row"><button id="lvi" class="${LANG === 'vi' ? 'primary' : ''}">Tiếng Việt</button><button id="len" class="${LANG === 'en' ? 'primary' : ''}">English</button></div>
      <h3>${t('about')}</h3><p class="small muted">${t('about_text')}</p></div>
    </div>`;
  const save = async (body) => { try { await api('PUT', '/api/settings', body); toast(t('saved')); } catch (err) { toast(err.message, true); } };
  $('#tgf').onsubmit = (e) => {
    e.preventDefault(); const f = e.target;
    const body = { telegram_chat: f.telegram_chat.value };
    if (f.telegram_token.value.trim()) body.telegram_token = f.telegram_token.value.trim();
    for (const k of ['notify_node_offline', 'notify_proxy_fail', 'notify_prefix_change', 'notify_update']) body[k] = f[k].checked ? '1' : '0';
    save(body).then(() => renderPage());
  };
  $('#tgTest').onclick = async (e) => {
    e.target.disabled = true;
    try { await api('POST', '/api/settings/telegram-test', {}); toast(t('telegram_sent')); } catch (err) { toast(err.message, true); }
    e.target.disabled = false;
  };
  $('#opf').onsubmit = (e) => {
    e.preventDefault(); const f = e.target;
    save({ check_interval_min: f.check_interval_min.value, auto_firewall: f.auto_firewall.checked ? '1' : '0', instance_name: f.instance_name.value, panel_url: f.panel_url.value });
  };
  $('#btnUpdCheck').onclick = async () => { await api('GET', '/api/update?refresh=1').catch(() => {}); renderPage(); };
  if ($('#btnUpd')) $('#btnUpd').onclick = async (e) => {
    if (!(await confirmBox(t('confirm_update', { v: u.latest })))) return;
    e.target.disabled = true; e.target.textContent = t('updating');
    try {
      await api('POST', '/api/update/panel', {});
      toast(t('update_restarting'));
      // wait for the panel to come back with the new version
      for (let i = 0; i < 60; i++) {
        await new Promise((r) => setTimeout(r, 2000));
        try { const x = await (await fetch('/api/update')).json(); if (x.current && x.current !== u.current) { location.reload(); return; } } catch (err) { /* restarting */ }
      }
      location.reload();
    } catch (err) { toast(err.message, true); e.target.disabled = false; e.target.textContent = t('update_now', { v: u.latest }); }
  };
  if ($('#btnUpdNodes')) $('#btnUpdNodes').onclick = async () => {
    try { const r = await api('POST', '/api/update/nodes', {}); toast(t('update_sent_n', { n: r.commands ? r.commands.length : 0 })); } catch (err) { toast(err.message, true); }
  };
  $('#restoreFile').onchange = async (e) => {
    const f = e.target.files[0]; if (!f) return;
    if (!(await confirmBox(t('confirm_restore')))) return;
    try {
      const r = await fetch('/api/restore', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: await f.text() });
      const j = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error(j.error || r.statusText);
      toast(t('restored')); setTimeout(() => location.reload(), 1200);
    } catch (err) { toast(err.message, true); }
  };
  $('#pf').onsubmit = async (e) => {
    e.preventDefault();
    try { await api('POST', '/api/account/password', { current: e.target.current.value, new: e.target.new.value }); toast(t('saved')); renderLogin(false); }
    catch (err) { toast(err.message, true); }
  };
  const setLang = (l) => { LANG = l; store('v6p_lang', l); api('PUT', '/api/settings', { lang: l }).catch(() => {}); renderShell(); };
  $('#lvi').onclick = () => setLang('vi');
  $('#len').onclick = () => setLang('en');
}

// ---------- boot ----------
(async function boot() {
  applyTheme();
  try {
    const s = await (await fetch('/api/status')).json();
    if (s.setup_required) renderLogin(true);
    else if (!s.user) renderLogin(false);
    else { state.user = s.user; renderShell(); }
  } catch (e) {
    $('#app').innerHTML = `<div class="center"><div class="alert err">${esc(e.message)}</div></div>`;
  }
})();
