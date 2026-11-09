'use strict';
/* openv6 dashboard — vanilla JS single page app (no build step). */

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

const state = {
  user: null, page: 'dashboard', nodes: [], proxies: [], selected: new Set(), hidden: new Set(),
  filter: { node: '', tag: '', status: '', q: '' }, timer: null, summary: null,
};

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

// toast(msg) · toast(msg, true) for errors · toast(msg, {action: {label, fn}, ms})
function toast(msg, opt) {
  if (opt === true) opt = { err: true };
  opt = opt || {};
  let box = $('#toasts');
  if (!box) { box = document.createElement('div'); box.id = 'toasts'; box.className = 'toasts'; document.body.appendChild(box); }
  const el = document.createElement('div');
  el.className = 'toast' + (opt.err ? ' err' : '');
  el.innerHTML = `${icon(opt.err ? 'alert' : 'check', 'sm')}<span></span>`;
  $('span', el).textContent = msg;
  if (opt.action) {
    const b = document.createElement('button');
    b.className = 'small'; b.innerHTML = `${icon('undo', 'sm')} ${esc(opt.action.label)}`;
    b.onclick = () => { el.remove(); opt.action.fn(); };
    el.appendChild(b);
  }
  box.appendChild(el);
  setTimeout(() => el.remove(), opt.ms || (opt.err ? 6000 : 2400));
  return el;
}

async function copyText(text, what) {
  try { await navigator.clipboard.writeText(text); }
  catch (e) {
    const ta = document.createElement('textarea'); ta.value = text; document.body.appendChild(ta); ta.select();
    document.execCommand('copy'); ta.remove();
  }
  toast(what ? t('copied_what', { what }) : t('copied'));
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
function flag(cc) {
  if (!cc || cc.length !== 2) return '';
  return String.fromCodePoint(...[...cc.toUpperCase()].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65));
}
const DOT = { ok: 'ok', error: 'error', expired: 'error', pending: 'pending', warn: 'warn' };
const badge = (st) => `<span class="badge ${esc(st)}">${esc(t('st_' + st))}</span>`;
const statusDot = (st) => `<span class="status"><span class="dot ${DOT[st] || ''}"></span>${esc(t('st_' + st))}</span>`;

// theme: system | light | dark
function themeMode() { return store('v6p_theme') || 'system'; }
function accentMode() { return store('v6p_accent') || 'panel'; }
function applyTheme() {
  const ac = accentMode();
  if (ac === 'panel') document.documentElement.removeAttribute('data-accent'); else document.documentElement.setAttribute('data-accent', ac);
  const th = themeMode();
  if (th === 'light' || th === 'dark') document.documentElement.setAttribute('data-theme', th);
  else document.documentElement.removeAttribute('data-theme');
}

// ---------- modal & drawer ----------
function modal(html, onMount, wide) {
  closeModal();
  const bg = document.createElement('div');
  bg.className = 'modal-bg'; bg.id = 'modal';
  bg.innerHTML = `<div class="modal" role="dialog" aria-modal="true" ${wide ? 'style="max-width:880px"' : ''}>${html}</div>`;
  bg.addEventListener('mousedown', (e) => { if (e.target === bg) closeModal(); });
  document.body.appendChild(bg);
  if (onMount) onMount($('.modal', bg));
  const first = $('input:not([type=checkbox]):not([type=radio]),select,textarea', bg); if (first) first.focus();
}
function closeModal() { const m = $('#modal'); if (m) m.remove(); }

function drawer(html, onMount) {
  closeDrawer(true);
  const bg = document.createElement('div'); bg.className = 'drawer-bg'; bg.id = 'drawerBg';
  const d = document.createElement('aside'); d.className = 'drawer'; d.id = 'drawer'; d.setAttribute('role', 'dialog');
  d.innerHTML = html;
  bg.onclick = () => closeDrawer();
  document.body.append(bg, d);
  requestAnimationFrame(() => { bg.classList.add('show'); d.classList.add('show'); });
  if (onMount) onMount(d);
  return d;
}
function closeDrawer(now) {
  const d = $('#drawer'), bg = $('#drawerBg');
  if (!d) return;
  if (now) { d.remove(); bg.remove(); return; }
  d.classList.remove('show'); bg.classList.remove('show');
  setTimeout(() => { d.remove(); bg.remove(); }, 250);
}

document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') { if ($('#modal')) closeModal(); else closeDrawer(); return; }
  const typing = /INPUT|TEXTAREA|SELECT/.test((document.activeElement || {}).tagName || '');
  if (typing || e.ctrlKey || e.metaKey || e.altKey || $('#modal')) return;
  if (e.key === '/' && state.page === 'proxies') { e.preventDefault(); const q = $('#fQ'); if (q) q.focus(); }
  if (e.key === 'n' && state.page === 'proxies') { e.preventDefault(); openCreate(); }
  if (e.key === 'g') state.gPending = Date.now();
  else if (state.gPending && Date.now() - state.gPending < 800) {
    const map = { d: 'dashboard', p: 'proxies', n: 'nodes', s: 'settings', e: 'events' };
    if (map[e.key]) location.hash = map[e.key];
    state.gPending = 0;
  }
});

function confirmBox(msg, opt = {}) {
  return new Promise((resolve) => {
    modal(`<div class="row" style="gap:12px;align-items:flex-start">
        <div class="stat"><div class="ico ${opt.danger ? 'warn' : ''}">${icon(opt.danger ? 'alert' : 'info')}</div></div>
        <div><h2>${esc(opt.title || t('confirm'))}</h2><p class="muted" style="margin:0">${esc(msg)}</p></div></div>
      <div class="actions"><button data-a="no">${t('cancel')}</button>
        <button class="${opt.danger ? 'danger solid' : 'primary'}" data-a="yes">${esc(opt.ok || t('ok'))}</button></div>`,
    (m) => {
      $('[data-a=no]', m).onclick = () => { closeModal(); resolve(false); };
      $('[data-a=yes]', m).onclick = () => { closeModal(); resolve(true); };
      $('[data-a=yes]', m).focus();
    });
  });
}

// ---------- login / setup ----------
function setHTML(el, html) {
  if (!el || el._h === html) return false;
  el._h = html; el.innerHTML = html; return true;
}

function brand() { return `<div class="brand"><span class="logo"><i></i></span>openv6</div>`; }

function langSeg(id) {
  return `<div class="seg" id="${id}"><button data-lang="vi" class="${LANG === 'vi' ? 'on' : ''}">VI</button><button data-lang="en" class="${LANG === 'en' ? 'on' : ''}">EN</button></div>`;
}
function bindLangSeg(root, after) {
  for (const b of $$('[data-lang]', root)) b.onclick = () => {
    LANG = b.dataset.lang; store('v6p_lang', LANG);
    if (state.user) api('PUT', '/api/settings', { lang: LANG }).catch(() => {});
    after();
  };
}

function renderLogin(setup) {
  stopTimer();
  document.documentElement.lang = LANG;
  $('#app').innerHTML = `<div class="center"><form class="card login" id="loginForm">
    <div class="row">${brand()}<div class="spacer"></div>${langSeg('loginLang')}</div>
    <div><h2 style="margin-bottom:4px">${setup ? t('setup_title') : t('login_title')}</h2>
      <p class="muted small" style="margin:0">${setup ? t('setup_hint') : t('login_sub')}</p></div>
    <label class="field">${t('username')}<input name="username" autocomplete="username" required value="${setup ? 'admin' : ''}"></label>
    <label class="field">${t('password')}<input name="password" type="password" autocomplete="${setup ? 'new-password' : 'current-password'}" required minlength="${setup ? 8 : 1}"></label>
    <div id="loginErr"></div>
    <button class="primary" type="submit" style="justify-content:center">${setup ? t('create_account') : t('login')}</button>
  </form></div>`;
  bindLangSeg($('#loginLang'), () => renderLogin(setup));
  $('#loginForm').onsubmit = async (e) => {
    e.preventDefault();
    const f = new FormData(e.target);
    try {
      const r = await api('POST', setup ? '/api/setup' : '/api/login', { username: f.get('username'), password: f.get('password') });
      state.user = r.user; renderShell();
    } catch (err) { $('#loginErr').innerHTML = `<div class="alert err">${icon('alert')}<div>${esc(err.message)}</div></div>`; }
  };
}

// ---------- shell ----------
const PAGES = ['dashboard', 'proxies', 'nodes', 'events', 'settings'];
const PAGE_ICON = { dashboard: 'dashboard', proxies: 'proxies', nodes: 'nodes', events: 'events', settings: 'settings' };

function renderShell() {
  const page = (location.hash.slice(1) || 'dashboard');
  state.page = PAGES.includes(page) ? page : 'dashboard';
  document.documentElement.lang = LANG;
  const th = themeMode();
  $('#app').innerHTML = `<div class="layout">
    <aside class="sidebar">
      ${brand()}
      <nav class="nav">${PAGES.map((p) => `<a href="#${p}" data-nav="${p}" class="${p === state.page ? 'active' : ''}">${icon(PAGE_ICON[p])}<span class="lbl">${t('nav_' + p)}</span><span class="lbl-s">${t('navs_' + p)}</span>${p === 'proxies' || p === 'nodes' ? `<span class="count" data-count="${p}"></span>` : ''}</a>`).join('')}</nav>
    </aside>
    <div class="content">
      <header class="topbar">
        <div class="mbrand">${brand()}</div>
        <div class="title" id="pageTitle">${t('nav_' + state.page)}</div>
        <div class="spacer"></div>
        <button class="pill" id="statusPill" title="${t('nav_dashboard')}"><span class="dot pending"></span><span class="pill-txt">…</span></button>
        <div class="acct">
          <button class="avatar" id="acctBtn" aria-haspopup="true" aria-expanded="false" title="${esc(state.user ? state.user.username : '')}">${esc((state.user ? state.user.username : '?').slice(0, 1).toUpperCase())}</button>
          <div class="menu" id="acctMenu" hidden>
            <div class="menu-head"><b>${esc(state.user ? state.user.username : '')}</b><span class="muted small" id="verFoot">openv6</span></div>
            <div class="menu-row"><span>${t('language')}</span>${langSeg('topLang')}</div>
            <div class="menu-row"><span>${t('appearance')}</span><div class="seg" id="themeSeg">
              <button data-th="light" class="${th === 'light' ? 'on' : ''}" title="${t('theme_light')}">${icon('sun', 'sm')}</button>
              <button data-th="system" class="${th === 'system' ? 'on' : ''}" title="${t('theme_system')}">${icon('monitor', 'sm')}</button>
              <button data-th="dark" class="${th === 'dark' ? 'on' : ''}" title="${t('theme_dark')}">${icon('moon', 'sm')}</button></div></div>
            <div class="menu-sep"></div>
            <a class="menu-item" href="#settings">${icon('settings', 'sm')} ${t('nav_settings')}</a>
            <button class="menu-item" id="logoutBtn">${icon('logout', 'sm')} ${t('logout')}</button>
          </div>
        </div>
      </header>
      <main class="main" id="page"></main>
    </div>
  </div>`;
  bindLangSeg($('#topLang'), renderShell);
  for (const b of $$('[data-th]')) b.onclick = () => {
    store('v6p_theme', b.dataset.th); applyTheme();
    for (const x of $$('[data-th]')) x.classList.toggle('on', x === b);
  };
  $('#statusPill').onclick = () => { location.hash = 'dashboard'; };
  const menu = $('#acctMenu'), ab = $('#acctBtn');
  const setMenu = (open) => { menu.hidden = !open; ab.setAttribute('aria-expanded', String(open)); };
  ab.onclick = (e) => { e.stopPropagation(); setMenu(menu.hidden); };
  menu.onclick = (e) => { e.stopPropagation(); if (e.target.closest('a.menu-item')) setMenu(false); };
  document.onclick = () => setMenu(false);
  $('#logoutBtn').onclick = async () => { await api('POST', '/api/logout', {}).catch(() => {}); state.user = null; renderLogin(false); };
  refreshSummary();
  renderPage();
}

// summary drives the status pill and nav counters (every page)
async function refreshSummary() {
  try {
    const d = await api('GET', '/api/dashboard');
    state.summary = d;
    const st = d.status || {};
    const pill = $('#statusPill'); if (!pill) return;
    let cls = 'ok', txt = t('pill_ok', { ok: st.ok || 0, total: d.proxies });
    if (!d.nodes_online) { cls = 'error'; txt = t('pill_offline'); }
    else if (d.proxies === 0) { cls = ''; txt = t('pill_empty'); }
    else if (d.failing || st.error) { cls = 'warn'; txt = t('pill_warn', { ok: st.ok || 0, total: d.proxies }); }
    else if (st.paused && st.paused === d.proxies) { cls = ''; txt = t('pill_paused'); }
    else if ((st.ok || 0) < d.proxies) { cls = 'pending'; txt = t('pill_applying', { ok: st.ok || 0, total: d.proxies }); }
    setHTML(pill, `<span class="dot ${cls}"></span><span class="pill-txt">${esc(txt)}</span>`);
    const c1 = $('[data-count=proxies]'); if (c1) c1.textContent = d.proxies || '';
    const c2 = $('[data-count=nodes]'); if (c2) c2.textContent = d.nodes || '';
    const v = $('#verFoot'); if (v) v.textContent = 'openv6 v' + d.version;
  } catch (e) { /* ignore */ }
}
setInterval(() => { if (state.user && !document.hidden) refreshSummary(); }, 10000);

window.addEventListener('hashchange', () => { if (state.user) { closeModal(); closeDrawer(true); navigate(); } });

function navigate() {
  if (!$('#page')) { renderShell(); return; }
  const page = location.hash.slice(1) || 'dashboard';
  state.page = PAGES.includes(page) ? page : 'dashboard';
  for (const a of $$('[data-nav]')) a.classList.toggle('active', a.dataset.nav === state.page);
  $('#pageTitle').textContent = t('nav_' + state.page);
  const m = $('#acctMenu'); if (m) m.hidden = true;
  const pg = $('#page');
  pg.classList.remove('enter'); void pg.offsetWidth; pg.classList.add('enter');
  window.scrollTo(0, 0);
  renderPage();
}

function stopTimer() { if (state.timer) { clearInterval(state.timer); state.timer = null; } }
function every(ms, fn) { stopTimer(); state.timer = setInterval(() => { if (!$('#modal') && !$('#drawer') && !document.hidden) fn(); }, ms); }

function skeleton(rows = 4) {
  return `<div class="card">${Array.from({ length: rows }, (_, i) => `<div class="skel" style="width:${90 - i * 12}%;margin:10px 0"></div>`).join('')}</div>`;
}

function renderPage() {
  stopTimer();
  $('#page')._h = null;
  $('#page').innerHTML = skeleton();
  const fn = { dashboard: pageDashboard, proxies: pageProxies, nodes: pageNodes, events: pageEvents, settings: pageSettings }[state.page];
  fn().catch((e) => { $('#page').innerHTML = `<div class="alert err">${icon('alert')}<div>${esc(e.message)}</div></div>`; });
}

// tipBox is an info hint the user can dismiss for good
function tipBox(key, html) {
  return `<div class="alert info" data-tip="${key}">${icon('info')}<div>${html}</div><button class="ghost icon-btn tip-x" title="${t('close')}">${icon('x', 'sm')}</button></div>`;
}

function alertBox(kind, html) {
  const ic = { err: 'alert', warn: 'alert', info: 'info' }[kind] || 'info';
  return `<div class="alert ${kind}">${icon(ic)}<div>${html}</div></div>`;
}

// ---------- dashboard ----------
async function pageDashboard() {
  const draw = async () => {
    const [d, nodes, u] = await Promise.all([api('GET', '/api/dashboard'), api('GET', '/api/nodes'), api('GET', '/api/update').catch(() => ({}))]);
    state.nodes = nodes;
    const st = d.status || {};
    const warnings = [];
    if (u.newer) warnings.push(alertBox('info', `${t('update_available', { v: esc(u.latest) })} <a href="#settings">${t('nav_settings')} →</a>`));
    for (const n of nodes) {
      if (!n.online) warnings.push(alertBox('warn', esc(t('warn_node_offline', { name: n.name }))));
      for (const e of n.errors || []) warnings.push(alertBox('err', `<b>${esc(n.name)}:</b> ${esc(e)}`));
      if (n.online) for (const c of (n.checks || []).filter((c) => !c.ok && !c.warn)) {
        warnings.push(alertBox('err', `<b>${esc(n.name)} — ${esc(t('chk_' + c.id))}:</b> ${esc(t('chkfix_' + c.id))} <a href="#nodes">${t('nav_nodes')} →</a>`));
      }
    }
    if (d.failing) warnings.push(alertBox('err', `${t('warn_failing', { n: d.failing })} <a href="#proxies">${t('nav_proxies')} →</a>`));
    if (d.proxies > 1 && d.subnets === 1 && !store('v6p_tip_same64')) warnings.push(tipBox('v6p_tip_same64', t('warn_same_64')));

    // hero: one sentence about the whole system
    let hero = { cls: 'ok', ic: 'check', title: t('hero_ok_title', { n: st.ok || 0 }), sub: t('hero_ok_sub') };
    if (!d.nodes_online) hero = { cls: 'error', ic: 'alert', title: t('hero_offline_title'), sub: t('hero_offline_sub') };
    else if (d.proxies === 0) hero = { cls: 'paused', ic: 'sparkle', title: t('hero_empty_title'), sub: t('hero_empty_sub') };
    else if (warnings.some((w) => w.includes('alert err'))) hero = { cls: 'error', ic: 'alert', title: t('hero_problem_title'), sub: t('hero_problem_sub') };
    else if (st.paused) hero = { cls: 'paused', ic: 'pause', title: t('hero_paused_title'), sub: t('hero_paused_sub') };
    else if ((st.ok || 0) < d.proxies) hero = { cls: 'warn', ic: 'pulse', title: t('hero_partial_title', { ok: st.ok || 0, total: d.proxies }), sub: t('hero_partial_sub') };

    const prefixes = nodes.flatMap((n) => (n.pools || []).filter((p) => p.cidr).map((p) => ({ n, p })));
    if (!setHTML($('#page'), `
      <div class="hero ${hero.cls}"><div class="big">${icon(hero.ic)}</div>
        <div style="flex:1"><h2>${esc(hero.title)}</h2><p>${esc(hero.sub)}</p></div>
        ${d.proxies === 0 ? `<button class="primary" id="startWizard">${icon('sparkle', 'sm')} ${t('wizard_start')}</button>` :
          `<div class="row hide-sm"><button id="dashCheck">${icon('pulse', 'sm')} ${t('check_all')}</button><a class="btn primary" href="#proxies">${icon('proxies', 'sm')} ${t('nav_proxies')}</a></div>`}
      </div>
      ${warnings.join('')}
      <div class="grid cols-4" style="margin-bottom:14px">
        ${statCard('proxies', '', t('proxies'), `${st.ok || 0}<span class="muted" style="font-size:16px"> / ${d.proxies}</span>`, t('running'))}
        ${statCard('nodes', d.nodes_online === d.nodes ? 'ok' : 'warn', t('nodes'), `${d.nodes_online}<span class="muted" style="font-size:16px"> / ${d.nodes}</span>`, t('online'))}
        ${statCard('globe', '', t('distinct_64'), d.subnets, t('distinct_64_sub'))}
        ${statCard('bolt', '', t('traffic'), fmtBytes(d.bytes_up + d.bytes_down), `↑ ${fmtBytes(d.bytes_up)} · ↓ ${fmtBytes(d.bytes_down)} · ${d.active} ${t('active_short')}`)}
      </div>
      <div class="grid cols-2">
        <div class="card"><div class="card-head">${icon('globe')}<h2>${t('ipv6_ranges')}</h2></div>
          ${prefixes.length ? `<div class="timeline">${prefixes.map(({ n, p }) => `<div class="ev"><div class="when">${esc(n.name)}</div>
            <div><code>${esc(p.cidr)}</code> <span class="badge neutral">${esc(p.mode)}</span>
            ${n.online ? '' : ` <span class="badge offline">${t('st_offline')}</span>`}</div></div>`).join('')}</div>`
          : `<p class="muted">${t('pool_waiting')}</p>`}
        </div>
        <div class="card"><div class="card-head">${icon('events')}<h2>${t('recent_events')}</h2><div class="spacer"></div><a href="#events" class="small">${t('view_all')} →</a></div>
          ${eventsList(d.events.slice(0, 6), nodes)}</div>
      </div>`)) return;
    for (const x of $$('[data-tip] .tip-x')) x.onclick = () => { const a = x.closest('[data-tip]'); store(a.dataset.tip, '1'); a.hidden = true; };
    const w = $('#startWizard'); if (w) w.onclick = () => openWizard();
    const dc = $('#dashCheck'); if (dc) dc.onclick = async () => { const r = await api('POST', '/api/checks/run', {}); toast(t('check_queued', { n: r.queued })); };
    if (d.proxies === 0 && !store('v6p_wizard_seen') && !$('#modal')) { store('v6p_wizard_seen', '1'); openWizard(); }
  };
  await draw();
  every(6000, () => draw().catch(() => {}));
}

function statCard(ic, cls, label, value, sub) {
  return `<div class="card stat"><div class="ico ${cls}">${icon(ic)}</div>
    <div><div class="label">${esc(label)}</div><div class="value">${value}</div><div class="sub">${esc(sub)}</div></div></div>`;
}

function eventsList(events, nodes) {
  if (!events || !events.length) return `<p class="muted">${t('no_events')}</p>`;
  const nm = Object.fromEntries((nodes || []).map((n) => [n.id, n.name]));
  return `<div class="timeline">${events.map((e) => `<div class="ev">
    <div class="when">${ago(e.at)}<div class="small">${esc(nm[e.node_id] || '')}</div></div>
    <div><span class="dot ${e.level === 'info' ? 'ok' : 'warn'}" style="display:inline-block;margin-right:8px"></span>${esc(e.message)}</div></div>`).join('')}</div>`;
}
const eventsTable = eventsList;

// ---------- first-run wizard ----------
async function openWizard() {
  const nodes = await api('GET', '/api/nodes');
  state.nodes = nodes;
  const local = nodes.find((n) => n.is_local) || nodes[0];
  if (!local) { toast(t('no_nodes'), true); return; }
  const w = { step: 1, scope: 'lan', count: 10, auth: 'random', label: 'Profile', tag: '' };
  const pool = () => (local.pools || []).find((p) => p.auto) || (local.pools || [])[0];
  const render = async (m) => {
    const steps = `<div class="steps">${[1, 2, 3, 4].map((i) => `<span class="${i <= w.step ? 'on' : ''}"></span>`).join('')}</div>`;
    let body = '';
    if (w.step === 1) {
      const n = (await api('GET', '/api/nodes')).find((x) => x.id === local.id) || local;
      const bad = (n.checks || []).filter((c) => !c.ok && !c.warn);
      const p = pool();
      body = `<h2>${t('wiz1_title')}</h2><p class="muted">${t('wiz1_sub')}</p>
        <div class="big-check"><div class="stat"><div class="ico ${p && p.cidr ? 'ok' : 'warn'}">${icon(p && p.cidr ? 'check' : 'alert')}</div></div>
          <div><b>${p && p.cidr ? esc(p.cidr) : t('pool_waiting')}</b><div class="small muted">${esc(n.net.primary_iface || '')} ${n.net.gateway ? '· router ' + esc(n.net.gateway) : ''}</div></div></div>
        ${bad.map((c) => alertBox('err', `<b>${esc(t('chk_' + c.id))}:</b> ${esc(t('chkfix_' + c.id))}`)).join('')}
        <button class="small" id="wizTest">${icon('pulse', 'sm')} ${t('test_v6')}</button> <span id="wizTestOut" class="small muted"></span>`;
    } else if (w.step === 2) {
      body = `<h2>${t('wiz2_title')}</h2><p class="muted">${t('wiz2_sub')}</p>
        <div class="choice">${['lan', 'local', 'any'].map((s) => `<label><input type="radio" name="scope" value="${s}" ${w.scope === s ? 'checked' : ''}>
          <span><b>${t('scope_' + s)}</b><span class="small muted">${t('scope_' + s + '_hint')}</span></span></label>`).join('')}</div>
        <p class="hint">${t('wiz2_host', { host: esc(local.entry || '') })}</p>`;
    } else if (w.step === 3) {
      body = `<h2>${t('wiz3_title')}</h2><p class="muted">${t('wiz3_sub')}</p>
        <div class="form-grid">
          <label class="field">${t('count')}<input id="wCount" type="number" min="1" max="500" value="${w.count}"></label>
          <label class="field">${t('label_prefix')}<input id="wLabel" value="${esc(w.label)}" placeholder="FB, TikTok…"></label>
          <label class="field">${t('tag')}<input id="wTag" value="${esc(w.tag)}" placeholder="facebook"></label>
        </div>
        <div class="choice" style="margin-top:14px">
          <label><input type="radio" name="auth" value="random" ${w.auth === 'random' ? 'checked' : ''}><span><b>${t('auth_random')}</b><span class="small muted">${t('wiz_auth_random_hint')}</span></span></label>
          <label ${w.scope === 'any' ? 'hidden' : ''}><input type="radio" name="auth" value="none" ${w.auth === 'none' ? 'checked' : ''}><span><b>${t('auth_none')}</b><span class="small muted">${t('auth_none_hint')}</span></span></label>
        </div>`;
    } else {
      body = `<h2>${t('wiz4_title')}</h2><p class="muted">${t('wiz4_sub')}</p>
        <label class="field">${t('export_for')}<select id="wPreset">${Object.keys(EXPORT_PRESETS).filter((k) => k !== 'custom').map((k) => `<option value="${k}">${esc(EXPORT_PRESETS[k].name)}</option>`).join('')}</select></label>
        <textarea id="wText" rows="8" readonly style="margin-top:10px"></textarea>
        <p class="hint">${t('preset_note')}</p>`;
    }
    m.innerHTML = `${steps}${body}<div class="actions">
      ${w.step > 1 && w.step < 4 ? `<button id="wBack">${t('back')}</button>` : `<button id="wSkip" class="ghost">${t(w.step === 4 ? 'close' : 'wiz_skip')}</button>`}
      <div class="spacer"></div>
      ${w.step < 3 ? `<button class="primary" id="wNext">${t('next')} ${icon('chevron', 'sm')}</button>` : ''}
      ${w.step === 3 ? `<button class="primary" id="wCreate">${icon('plus', 'sm')} ${t('wiz_create')}</button>` : ''}
      ${w.step === 4 ? `<button id="wCopy">${icon('copy', 'sm')} ${t('copy')}</button><a class="btn primary" href="#proxies" id="wDone">${t('wiz_done')}</a>` : ''}
    </div>`;
    const go = (s) => { w.step = s; render(m); };
    if ($('#wSkip', m)) $('#wSkip', m).onclick = closeModal;
    if ($('#wBack', m)) $('#wBack', m).onclick = () => go(w.step - 1);
    if ($('#wNext', m)) $('#wNext', m).onclick = () => {
      if (w.step === 2) { w.scope = $('input[name=scope]:checked', m).value; if (w.scope === 'any') w.auth = 'random'; }
      go(w.step + 1);
    };
    if ($('#wTest', m)) $('#wTest', m).onclick = async (e) => {
      e.target.disabled = true; $('#wizTestOut', m).textContent = '…';
      try { const c = await api('POST', `/api/nodes/${local.id}/test`, {}); const r = await pollCommand(c.id);
        $('#wizTestOut', m).textContent = r.ok ? t('v6_ok', { ip: r.output }) : t('test_fail', { err: r.output }); }
      catch (err) { $('#wizTestOut', m).textContent = err.message; }
      e.target.disabled = false;
    };
    if ($('#wCreate', m)) $('#wCreate', m).onclick = async (e) => {
      w.count = Number($('#wCount', m).value) || 1; w.label = $('#wLabel', m).value; w.tag = $('#wTag', m).value;
      w.auth = ($('input[name=auth]:checked', m) || {}).value || 'random';
      const p = pool();
      if (!p) { toast(t('pool_waiting'), true); return; }
      e.target.disabled = true;
      try {
        const r = await api('POST', '/api/proxies/bulk', { node_id: local.id, pool_id: p.id, count: w.count, auth: w.auth, scope: w.scope,
          label_prefix: w.label, tag: w.tag, protocol: 'both', spread: Number((p.cidr || '/64').split('/')[1]) < 64 });
        w.created = true; toast(t('created_n', { n: r.created })); go(4);
      } catch (err) { toast(err.message, true); e.target.disabled = false; }
    };
    if (w.step === 4) {
      await loadProxies();
      const ids = state.proxies.map((x) => x.id);
      const load = async () => {
        const pr = EXPORT_PRESETS[$('#wPreset', m).value];
        const r = await api('POST', '/api/proxies/export', { ids, format: pr.format, scheme: pr.scheme });
        $('#wText', m).value = r.text;
      };
      const saved = store('v6p_export_preset'); if (saved && saved !== 'custom' && EXPORT_PRESETS[saved]) $('#wPreset', m).value = saved;
      $('#wPreset', m).onchange = () => { store('v6p_export_preset', $('#wPreset', m).value); load(); };
      $('#wCopy', m).onclick = () => copyText($('#wText', m).value, t('proxies'));
      $('#wDone', m).onclick = () => closeModal();
      load();
    }
  };
  modal('', (m) => render(m));
}

// ---------- proxy data ----------
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

// ---------- proxies ----------
const STATUS_FILTERS = ['', 'ok', 'error', 'pending', 'offline', 'paused', 'disabled', 'expired'];

async function pageProxies() {
  await loadProxies();
  $('#page').innerHTML = `<div class="page-head">
      <div><h1>${t('nav_proxies')}</h1><p id="pCount"></p></div><div class="spacer"></div>
      <button id="btnCheckAll" title="${t('check_all_hint')}">${icon('pulse', 'sm')} <span class="hide-sm">${t('check_all')}</span></button>
      <button id="btnExport">${icon('download', 'sm')} <span class="hide-sm">${t('export')}</span></button>
      <button class="primary" id="btnCreate" title="N">${icon('plus', 'sm')} ${t('create_proxies')}</button>
    </div>
    <div class="row" style="margin-bottom:12px">
      <div class="search">${icon('search', 'sm')}<input id="fQ" placeholder="${t('search')}"></div>
      <select id="fNode"></select><select id="fTag"></select>
      <span class="muted small hide-sm"><span class="kbd">/</span> ${t('kbd_search')} · <span class="kbd">N</span> ${t('kbd_new')}</span>
    </div>
    <div class="row" id="chips" style="margin-bottom:14px"></div>
    <div id="ptable"></div>
    <div class="bulkbar" id="bulkbar"></div>`;
  const f = state.filter;
  $('#fQ').value = f.q;
  $('#fNode').onchange = (e) => { f.node = e.target.value; drawProxyRows(); };
  $('#fTag').onchange = (e) => { f.tag = e.target.value; drawProxyRows(); };
  $('#fQ').oninput = (e) => { f.q = e.target.value; drawProxyRows(); };
  $('#btnCreate').onclick = () => openCreate();
  $('#btnExport').onclick = () => openExport();
  $('#btnCheckAll').onclick = async (e) => {
    e.currentTarget.disabled = true;
    try { const r = await api('POST', '/api/checks/run', {}); toast(t('check_queued', { n: r.queued })); } catch (err) { toast(err.message, true); }
    const b = e.currentTarget; setTimeout(() => { b.disabled = false; }, 5000);
  };
  drawProxyRows();
  every(5000, async () => { try { await loadProxies(); drawProxyRows(); } catch (e) { /* ignore */ } });
}

function errInfo(code, raw) {
  if (!code) return esc(raw || '');
  const title = I18N[LANG]['err_' + code] || I18N.en['err_' + code];
  if (!title) return esc(raw || code);
  const fix = I18N[LANG]['errfix_' + code] || I18N.en['errfix_' + code] || '';
  return `<b>${esc(title)}</b>${fix ? `<div>${esc(fix)}</div>` : ''}${raw ? `<details><summary>${t('details')}</summary><code>${esc(raw)}</code></details>` : ''}`;
}

const SCOPES = ['lan', 'local', 'any'];
function relayNodes(exceptId) { return (state.nodes || []).filter((n) => n.relay_port > 0 && n.id !== exceptId); }
function relaySelect(name, value, exceptId) {
  const ns = relayNodes(exceptId);
  return `<select name="${name}"><option value="0">${t('relay_off')}</option>${ns.map((n) => `<option value="${n.id}" ${n.id === value ? 'selected' : ''}>${esc(n.name)} (${esc(n.entry_host || n.net.public_v4 || n.entry)})</option>`).join('')}</select>`;
}
function fmtRate(kbps) { return kbps >= 1024 ? (kbps / 1024).toFixed(kbps % 1024 ? 1 : 0) + ' MB/s' : kbps + ' KB/s'; }
function limitsText(p) {
  const a = [];
  if (p.max_conns) a.push(t('max_conns_n', { n: p.max_conns }));
  if (p.rate_kbps) a.push(fmtRate(p.rate_kbps));
  return a.join(' · ');
}
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
    return `<div class="health" title="${esc(p.check_org)} · ${fmtTime(p.check_at)}"><span><span class="flag">${flag(p.check_country)}</span> ${p.check_latency} ms</span>
      <span class="muted">${p.check_org ? esc(shortOrg(p.check_org)) + ' · ' : ''}${ago(p.check_at)}</span></div>`;
  }
  if (p.check_ok === 0) {
    const title = I18N[LANG]['err_' + p.check_code] || t('check_failed_n', { n: p.fail_streak });
    return `<div class="health"><span style="color:var(--err)">${icon('alert', 'sm')} ${esc(title)}</span><span class="muted">${ago(p.check_at)}</span></div>`;
  }
  return '<span class="muted">—</span>';
}

function hostPort(p) { return (p.host.includes(':') ? `[${p.host}]` : p.host) + ':' + p.port; }

// the proxy line in the user's preferred anti-detect format
function proxyLine(p, presetKey) {
  const pr = EXPORT_PRESETS[presetKey || store('v6p_export_preset') || 'multilogin'] || EXPORT_PRESETS.multilogin;
  const hp = hostPort(p), auth = p.auth_user ? `${p.auth_user}:${p.auth_pass}` : '';
  switch (pr.format || 'host:port:user:pass') {
    case 'user:pass@host:port': return auth ? `${auth}@${hp}` : hp;
    case 'url': return `${pr.scheme}://${auth ? auth + '@' : ''}${hp}`;
    case 'scheme://host:port:user:pass': return `${pr.scheme}://${hp}${auth ? ':' + auth : ''}`;
    case 'host:port@user:pass': return auth ? `${hp}@${auth}` : hp;
    default: return auth ? `${hp}:${auth}` : hp;
  }
}

function drawProxyRows() {
  const f = state.filter;
  const nodeSel = $('#fNode'); if (!nodeSel) return;
  setHTML(nodeSel, `<option value="">${t('all_nodes')}</option>` + state.nodes.map((n) => `<option value="${n.id}">${esc(n.name)}</option>`).join(''));
  nodeSel.value = f.node;
  nodeSel.hidden = state.nodes.length < 2;
  const tags = [...new Set(state.proxies.map((p) => p.tag).filter(Boolean))].sort();
  setHTML($('#fTag'), `<option value="">${t('all_tags')}</option>` + tags.map((x) => `<option>${esc(x)}</option>`).join(''));
  $('#fTag').value = f.tag;
  $('#fTag').hidden = !tags.length;

  const visible = state.proxies.filter((p) => !state.hidden.has(p.id));
  const counts = {};
  for (const p of visible) counts[p.status] = (counts[p.status] || 0) + 1;
  setHTML($('#chips'), STATUS_FILTERS.filter((s) => !s || counts[s]).map((s) => `<button class="chip ${f.status === s ? 'on' : ''}" data-st="${s}">
    ${s ? `<span class="dot ${DOT[s] || ''}"></span>${t('st_' + s)}` : t('all_status')} <span class="n">${s ? counts[s] : visible.length}</span></button>`).join(''));
  for (const c of $$('[data-st]')) c.onclick = () => { f.status = c.dataset.st; drawProxyRows(); };

  const list = filteredProxies().filter((p) => !state.hidden.has(p.id));
  const pc = t('proxy_count', { n: visible.length }); if ($('#pCount').textContent !== pc) $('#pCount').textContent = pc;
  drawBulkBar();
  if (!state.proxies.length) {
    $('#ptable')._h = null; $('#ptable').innerHTML = `<div class="card empty"><div class="art">${icon('proxies')}</div><h2>${t('empty_proxies_title')}</h2>
      <p>${t('empty_proxies_sub')}</p><div class="row" style="justify-content:center;margin-top:14px">
      <button class="primary" id="emptyWizard">${icon('sparkle', 'sm')} ${t('wizard_start')}</button><button id="emptyCreate">${icon('plus', 'sm')} ${t('create_proxies')}</button></div></div>`;
    $('#emptyWizard').onclick = () => openWizard();
    $('#emptyCreate').onclick = () => openCreate();
    return;
  }
  if (!list.length) { $('#ptable')._h = null; $('#ptable').innerHTML = `<div class="card empty"><div class="art">${icon('filter')}</div><h2>${t('no_match')}</h2></div>`; return; }
  const allSel = list.every((p) => state.selected.has(p.id));
  if (!setHTML($('#ptable'), `<div class="table-wrap"><table><thead><tr>
      <th style="width:36px"><input type="checkbox" id="selAll" ${allSel ? 'checked' : ''} aria-label="${t('select_all')}"></th>
      <th>${t('name_port')}</th><th>${t('connect_to')}</th><th class="hide-sm hide-lg">${t('ipv6_out')}</th><th>${t('status')}</th>
      <th class="hide-sm">${t('health')}</th><th class="hide-sm hide-md">${t('traffic')}</th><th></th></tr></thead>
    <tbody>${list.map((p) => `<tr class="clickable ${state.selected.has(p.id) ? 'selected' : ''}" data-id="${p.id}">
      <td class="ck"><input type="checkbox" class="sel" ${state.selected.has(p.id) ? 'checked' : ''}></td>
      <td class="nm"><div class="name">${esc(p.label || ':' + p.port)}</div><div class="sub">${p.label ? ':' + p.port + ' · ' : ''}${esc(p.node_name)}${p.tag ? ` · <span class="badge neutral">${esc(p.tag)}</span>` : ''}${p.relay_name ? ` · <span class="badge ${p.relay_up ? 'accent' : 'warn'}" title="${esc(t('relay_via', { n: p.relay_name }))}">${icon('globe', 'sm')} ${esc(p.relay_host)}:${p.relay_port}</span>` : ''}</div></td>
      <td class="cn"><span class="copyable mono" data-copy="${esc(proxyLine(p))}" title="${t('click_copy_line')}">${esc(hostPort(p))}${icon('copy', 'sm')}</span>
        <div class="sub">${p.auth_user ? `${icon('key', 'sm')} ${esc(p.auth_user)}` : t('no_auth')} · ${esc(t('scope_short_' + p.scope))}</div></td>
      <td class="hide-sm hide-lg mono small"><span class="copyable" data-copy="${esc(p.ip)}">${esc(p.ip || '—')}${p.ip ? icon('copy', 'sm') : ''}</span></td>
      <td class="st">${statusDot(p.status)}${p.error ? `<div class="errbox" title="${esc(p.error)}">${esc(I18N[LANG]['err_' + p.code] || p.error)}</div>` : ''}</td>
      <td class="hide-sm">${checkCell(p)}</td>
      <td class="hide-sm hide-md small"><div>↑ ${fmtBytes(p.bytes_up)}</div><div class="muted">↓ ${fmtBytes(p.bytes_down)} · ${p.active}/${p.conns_total}</div></td>
      <td class="ac"><button class="ghost icon-btn" data-act="test" title="${t('test')}">${icon('pulse', 'sm')}</button></td>
    </tr>`).join('')}</tbody></table></div>`)) return;
  $('#selAll').onchange = (e) => { for (const p of list) e.target.checked ? state.selected.add(p.id) : state.selected.delete(p.id); drawProxyRows(); };
  for (const tr of $$('#ptable tr[data-id]')) {
    const id = Number(tr.dataset.id);
    tr.onclick = (e) => {
      if (e.target.closest('input,button,.copyable,details,a')) return;
      openProxyDrawer(id);
    };
    $('.sel', tr).onchange = (e) => { e.target.checked ? state.selected.add(id) : state.selected.delete(id); tr.classList.toggle('selected', e.target.checked); drawBulkBar(); };
    $('[data-act=test]', tr).onclick = (e) => testProxies([id], e.currentTarget);
  }
  for (const el of $$('#ptable [data-copy]')) el.onclick = () => copyText(el.dataset.copy);
}

function drawBulkBar() {
  const bar = $('#bulkbar'); if (!bar) return;
  const n = state.selected.size;
  bar.classList.toggle('show', n > 0);
  if (!n) return;
  bar.innerHTML = `<span class="n">${t('selected_n', { n })}</span>
    <button data-b="copy">${icon('copy', 'sm')} ${t('copy')}</button>
    <button data-b="test">${icon('pulse', 'sm')} ${t('act_test')}</button>
    <button data-b="enable">${icon('play', 'sm')} ${t('act_enable')}</button>
    <button data-b="disable">${icon('pause', 'sm')} ${t('act_disable')}</button>
    <button data-b="regen_ip">${icon('refresh', 'sm')} ${t('act_regen_ip')}</button>
    <button data-b="more">${icon('more', 'sm')} ${t('more')}</button>
    <button data-b="delete">${icon('trash', 'sm')} ${t('act_delete')}</button>
    <button data-b="clear" title="${t('clear_selection')}">${icon('x', 'sm')}</button>`;
  for (const b of $$('[data-b]', bar)) b.onclick = () => {
    const a = b.dataset.b;
    if (a === 'clear') { state.selected.clear(); drawProxyRows(); return; }
    if (a === 'copy') {
      const lines = state.proxies.filter((p) => state.selected.has(p.id)).map((p) => proxyLine(p)).join('\n');
      copyText(lines, t('selected_n', { n })); return;
    }
    if (a === 'more') { openMoreActions(); return; }
    bulkAction(a);
  };
}

function openMoreActions() {
  const acts = ['regen_pass', 'remove_auth', 'set_scope', 'set_limits', 'set_relay', 'set_tag', 'set_protocol', 'fallback_on', 'fallback_off', 'reset_stats'];
  modal(`<h2>${t('more_actions', { n: state.selected.size })}</h2>
    <div class="choice">${acts.map((a) => `<label data-act="${a}" style="align-items:center"><span><b>${t('act_' + a)}</b></span></label>`).join('')}</div>
    <div class="actions"><button id="cl">${t('close')}</button></div>`, (m) => {
    for (const l of $$('[data-act]', m)) l.onclick = () => { closeModal(); bulkAction(l.dataset.act); };
    $('#cl', m).onclick = closeModal;
  });
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
  let old;
  if (btn) { btn.disabled = true; old = btn.innerHTML; btn.innerHTML = '<span class="dot pending"></span>'; }
  try {
    const r = await api('POST', '/api/proxies/action', { ids, action: 'test' });
    const results = await Promise.all(r.commands.map((c) => pollCommand(c.id)));
    const ok = results.filter((x) => x.ok).length;
    if (ids.length === 1) {
      const x = results[0];
      toast(x.ok ? t('test_ok_full', { ip: x.output, ms: x.latency || '?', isp: shortOrg(x.org) || '' }) : (I18N[LANG]['err_' + x.code] || t('test_fail', { err: x.output })), !x.ok);
    } else toast(t('test_summary', { ok, total: ids.length }), ok !== ids.length);
    await loadProxies(); drawProxyRows();
  } catch (e) { toast(e.message, true); }
  if (btn) { btn.disabled = false; btn.innerHTML = old; }
}

function chooseScope() {
  return new Promise((resolve) => {
    modal(`<h2>${t('act_set_scope')}</h2>
      <div class="choice">${SCOPES.map((v) => `<label><input type="radio" name="sc" value="${v}" ${v === 'lan' ? 'checked' : ''}>
        <span><b>${t('scope_' + v)}</b><span class="small muted">${t('scope_' + v + '_hint')}</span></span></label>`).join('')}</div>
      <div class="actions"><button id="no">${t('cancel')}</button><button class="primary" id="yes">${t('ok')}</button></div>`, (m) => {
      $('#no', m).onclick = () => { closeModal(); resolve(''); };
      $('#yes', m).onclick = () => { const v = $('input[name=sc]:checked', m).value; closeModal(); resolve(v); };
    });
  });
}

function promptBox(title, label, value = '') {
  return new Promise((resolve) => {
    modal(`<h2>${esc(title)}</h2><label class="field">${esc(label)}<input id="pv" value="${esc(value)}"></label>
      <div class="actions"><button id="no">${t('cancel')}</button><button class="primary" id="yes">${t('ok')}</button></div>`, (m) => {
      $('#no', m).onclick = () => { closeModal(); resolve(null); };
      const ok = () => { const v = $('#pv', m).value; closeModal(); resolve(v); };
      $('#yes', m).onclick = ok;
      $('#pv', m).onkeydown = (e) => { if (e.key === 'Enter') ok(); };
    });
  });
}

// deleteWithUndo hides proxies at once and deletes them after a grace period
function deleteWithUndo(ids) {
  for (const id of ids) { state.hidden.add(id); state.selected.delete(id); }
  closeDrawer();
  drawProxyRows();
  let undone = false;
  toast(t('deleted_n', { n: ids.length }), { ms: 6000, action: { label: t('undo'), fn: () => { undone = true; for (const id of ids) state.hidden.delete(id); drawProxyRows(); } } });
  setTimeout(async () => {
    if (undone) return;
    try { await api('POST', '/api/proxies/action', { ids, action: 'delete' }); } catch (e) { toast(e.message, true); }
    for (const id of ids) state.hidden.delete(id);
    await loadProxies(); drawProxyRows(); refreshSummary();
  }, 6200);
}

async function bulkAction(action, idsIn) {
  const ids = idsIn || [...state.selected];
  if (!ids.length) { toast(t('select_first'), true); return; }
  if (action === 'test') return testProxies(ids);
  if (action === 'delete') return deleteWithUndo(ids);
  let value = '';
  if (action === 'set_tag') { value = await promptBox(t('act_set_tag'), t('tag')); if (value === null) return; }
  if (action === 'set_protocol') { value = await chooseProtocol(); if (!value) return; }
  if (action === 'set_scope') { value = await chooseScope(); if (!value) return; }
  if (action === 'set_limits') { value = await chooseLimits(); if (value === null) return; }
  if (action === 'set_relay') { value = await chooseRelay(); if (value === null) return; }
  if (action === 'regen_ip' && !(await confirmBox(t('confirm_regen_ip', { n: ids.length }), { ok: t('act_regen_ip') }))) return;
  if (action === 'regen_pass' && !(await confirmBox(t('confirm_regen_pass', { n: ids.length }), { ok: t('act_regen_pass') }))) return;
  try {
    const r = await api('POST', '/api/proxies/action', { ids, action, value });
    toast(t('done_n', { n: r.affected }));
    await loadProxies(); drawProxyRows();
  } catch (e) { toast(e.message, true); }
}

function chooseLimits() {
  return new Promise((resolve) => {
    modal(`<h2>${t('act_set_limits')}</h2>
      <div class="form-grid"><label class="field">${t('max_conns')}<input id="lc" type="number" min="0" value="0"></label>
      <label class="field">${t('rate_kbps')}<input id="lr" type="number" min="0" value="0"></label></div>
      <p class="hint">${t('limits_hint')}</p>
      <div class="actions"><button id="no">${t('cancel')}</button><button class="primary" id="yes">${t('ok')}</button></div>`, (m) => {
      $('#no', m).onclick = () => { closeModal(); resolve(null); };
      $('#yes', m).onclick = () => { const v = `${Number($('#lc', m).value) || 0},${Number($('#lr', m).value) || 0}`; closeModal(); resolve(v); };
    });
  });
}

function chooseRelay() {
  return new Promise((resolve) => {
    const ns = relayNodes(0);
    modal(`<h2>${t('act_set_relay')}</h2>
      ${ns.length ? '' : alertBox('warn', esc(t('relay_none_yet')))}
      <div class="choice"><label><input type="radio" name="rl" value="0" checked><span><b>${t('relay_off')}</b><span class="small muted">${t('relay_off_hint')}</span></span></label>
      ${ns.map((n) => `<label><input type="radio" name="rl" value="${n.id}"><span><b>${esc(n.name)}</b><span class="small muted">${esc(n.entry_host || n.net.public_v4 || n.entry)} · ${t('relay_port')} ${n.relay_port}</span></span></label>`).join('')}</div>
      <p class="hint">${t('relay_need_auth')}</p>
      <div class="actions"><button id="no">${t('cancel')}</button><button class="primary" id="yes">${t('ok')}</button></div>`, (m) => {
      $('#no', m).onclick = () => { closeModal(); resolve(null); };
      $('#yes', m).onclick = () => { const v = $('input[name=rl]:checked', m).value; closeModal(); resolve(v); };
    });
  });
}

function chooseProtocol() {
  return new Promise((resolve) => {
    modal(`<h2>${t('act_set_protocol')}</h2><div class="choice">
      ${[['both', 'HTTP + SOCKS5'], ['http', 'HTTP'], ['socks5', 'SOCKS5']].map(([v, l], i) => `<label><input type="radio" name="pr" value="${v}" ${i ? '' : 'checked'}><span><b>${l}</b></span></label>`).join('')}</div>
      <div class="actions"><button id="no">${t('cancel')}</button><button class="primary" id="yes">${t('ok')}</button></div>`, (m) => {
      $('#no', m).onclick = () => { closeModal(); resolve(''); };
      $('#yes', m).onclick = () => { const v = $('input[name=pr]:checked', m).value; closeModal(); resolve(v); };
    });
  });
}

// ---------- proxy detail drawer ----------
async function openProxyDrawer(id, tab = 'info') {
  const p = state.proxies.find((x) => x.id === id);
  if (!p) return;
  const lines = [
    ['host:port:user:pass', 'multilogin'], ['http://user:pass@host:port', 'http_url'], ['socks5://user:pass@host:port', 'dolphin'],
  ];
  const lineFor = (k) => k === 'http_url' ? `http://${p.auth_user ? p.auth_user + ':' + p.auth_pass + '@' : ''}${hostPort(p)}` : proxyLine(p, k);
  drawer(`<div class="drawer-head"><div style="flex:1;min-width:0"><h2>${esc(p.label || ':' + p.port)}</h2>
      <div class="row small muted">${statusDot(p.status)} · ${esc(p.node_name)} · :${p.port}</div></div>
      <button class="ghost icon-btn" id="dClose" title="Esc">${icon('x')}</button></div>
    <div class="drawer-body">
      <div class="tabs"><button data-tab="info">${t('tab_overview')}</button><button data-tab="health">${t('tab_health')}</button><button data-tab="edit">${t('edit')}</button></div>
      <div data-pane="info">
        ${p.error ? alertBox('err', errInfo(p.code, p.error)) : ''}
        <h3 style="margin-top:0">${t('copy_for_antidetect')}</h3>
        ${lines.map(([lbl, k]) => `<div class="copy-line"><span class="lbl">${esc(lbl.split('://')[0] === lbl ? lbl : lbl.split('://')[0].toUpperCase())}</span><code>${esc(lineFor(k))}</code>
          <button class="small" data-copyline="${esc(lineFor(k))}">${icon('copy', 'sm')}</button></div>`).join('')}
        ${otherEndpoints(p)}
        <h3>${t('details')}</h3>
        <div class="kv">
          <div>${t('ipv6_out')}</div><div class="mono small">${esc(p.ip || '—')}</div>
          <div>/64</div><div class="mono small">${esc(p.subnet64 || '—')}</div>
          <div>${t('auth')}</div><div class="mono small">${p.auth_user ? esc(p.auth_user) + ' / ' + esc(p.auth_pass) : t('no_auth')}</div>
          <div>${t('scope')}</div><div>${esc(t('scope_' + p.scope))}</div>
          <div>${t('protocol')}</div><div>${p.protocol === 'both' ? 'HTTP + SOCKS5' : esc(p.protocol.toUpperCase())}</div>
          <div>${t('limits')}</div><div>${esc(limitsText(p)) || `<span class="muted">${t('unlimited')}</span>`}</div>
          <div>${t('traffic')}</div><div>↑ ${fmtBytes(p.bytes_up)} · ↓ ${fmtBytes(p.bytes_down)} · ${p.active}/${p.conns_total} ${t('conns_short')}</div>
          <div>${t('tag_note')}</div><div>${p.tag ? `<span class="badge neutral">${esc(p.tag)}</span> ` : ''}${esc(p.note || '—')}</div>
          ${p.expires_at ? `<div>${t('expires')}</div><div>${fmtTime(p.expires_at)}</div>` : ''}
          <div>${t('created')}</div><div>${fmtTime(p.created_at)}</div>
        </div>
      </div>
      <div data-pane="health" hidden>
        <div class="big-check"><div class="stat"><div class="ico ${p.check_ok === 1 ? 'ok' : p.check_ok === 0 ? 'warn' : ''}">${icon(p.check_ok === 0 ? 'alert' : 'pulse')}</div></div>
          <div style="flex:1">${checkCell(p)}</div><button class="small" id="dTest">${icon('pulse', 'sm')} ${t('test')}</button></div>
        <div class="bars" id="dBars"></div>
        <div id="dHist" class="timeline small" style="margin-top:12px"><div class="skel"></div></div>
      </div>
      <div data-pane="edit" hidden><form id="ef" class="stack">
        <div class="form-grid">
          <label class="field">${t('label')}<input name="label" value="${esc(p.label)}" placeholder="${t('label_ph')}"></label>
          <label class="field">${t('port')}<input name="port" type="number" min="1024" max="65535" value="${p.port}"></label>
          <label class="field">${t('username')}<input name="auth_user" value="${esc(p.auth_user)}"></label>
          <label class="field">${t('password')}<input name="auth_pass" value="${esc(p.auth_pass)}"></label>
          <label class="field">${t('protocol')}<select name="protocol"><option value="both">HTTP + SOCKS5</option><option value="http">HTTP</option><option value="socks5">SOCKS5</option></select></label>
          <label class="field">${t('scope')}${scopeSelect('scope', p.scope)}</label>
          <label class="field">${t('tag')}<input name="tag" value="${esc(p.tag)}"></label>
          <label class="field">${t('expires')}<input name="expires" type="date" value="${p.expires_at ? new Date(p.expires_at * 1000).toISOString().slice(0, 10) : ''}"></label>
          <label class="field">${t('max_conns')}<input name="max_conns" type="number" min="0" value="${p.max_conns || 0}"></label>
          <label class="field">${t('rate_kbps')}<input name="rate_kbps" type="number" min="0" value="${p.rate_kbps || 0}"></label>
          <label class="field">${t('relay')}${relaySelect('relay_node_id', p.relay_node_id, p.node_id)}</label>
          <label class="field">${t('relay_public_port')}<input name="relay_port" type="number" min="0" max="65535" value="${p.relay_port || 0}"></label>
        </div>
        <p class="hint">${t('limits_hint')} ${t('relay_edit_hint')}</p>
        <label class="field">${t('ipv6_out')}<input name="ip" class="mono" value="${esc(p.ip)}"><span class="hint">${t('ip_edit_hint')}</span></label>
        <label class="field">${t('note')}<textarea name="note" rows="2">${esc(p.note)}</textarea></label>
        <label class="check"><input type="checkbox" name="enabled" ${p.enabled ? 'checked' : ''}> ${t('enabled')}</label>
        <label class="check"><input type="checkbox" name="ipv4_fallback" ${p.ipv4_fallback ? 'checked' : ''}><span>${t('ipv4_fallback')}<span class="hint" style="display:block">${t('fallback_hint')}</span></span></label>
        <div id="efErr"></div>
        <div><button class="primary" type="submit">${icon('check', 'sm')} ${t('save')}</button></div>
      </form></div>
    </div>
    <div class="drawer-foot">
      <button id="dTest2">${icon('pulse', 'sm')} ${t('test')}</button>
      <button id="dRegen">${icon('refresh', 'sm')} ${t('act_regen_ip')}</button>
      <button id="dToggle">${icon(p.enabled ? 'pause' : 'play', 'sm')} ${p.enabled ? t('act_disable') : t('act_enable')}</button>
      <div class="spacer"></div>
      <button class="danger" id="dDel">${icon('trash', 'sm')}</button>
    </div>`, (d) => {
    const show = (name) => {
      for (const b of $$('[data-tab]', d)) b.classList.toggle('on', b.dataset.tab === name);
      for (const x of $$('[data-pane]', d)) x.hidden = x.dataset.pane !== name;
      if (name === 'health') loadHistory();
    };
    for (const b of $$('[data-tab]', d)) b.onclick = () => show(b.dataset.tab);
    $('#dClose', d).onclick = () => closeDrawer();
    for (const b of $$('[data-copyline]', d)) b.onclick = () => copyText(b.dataset.copyline);
    const loadHistory = async () => {
      const h = await api('GET', `/api/proxies/${p.id}/checks`).catch(() => []);
      const recent = h.slice(0, 40).reverse();
      const max = Math.max(1, ...recent.map((c) => c.latency || 0));
      $('#dBars', d).innerHTML = recent.map((c) => `<span class="${c.ok ? '' : 'bad'}" style="height:${c.ok ? Math.max(8, c.latency / max * 100) : 100}%" title="${fmtTime(c.at)} · ${c.ok ? c.latency + ' ms' : esc(c.error)}"></span>`).join('');
      $('#dHist', d).innerHTML = h.length ? h.slice(0, 15).map((c) => `<div class="ev"><div class="when">${fmtTime(c.at)}</div>
        <div class="wrap">${c.ok ? `<span style="color:var(--ok)">${icon('check', 'sm')}</span> ${c.latency} ms · ${esc(c.ip)}` : `<span style="color:var(--err)">${icon('x', 'sm')}</span> ${errInfo(c.code, c.error)}`}</div></div>`).join('') : `<p class="muted">${t('never')}</p>`;
    };
    const test = (b) => testProxies([p.id], b).then(() => openProxyDrawer(p.id, 'health'));
    $('#dTest', d).onclick = (e) => test(e.currentTarget);
    $('#dTest2', d).onclick = (e) => test(e.currentTarget);
    $('#dRegen', d).onclick = () => bulkAction('regen_ip', [p.id]).then(() => openProxyDrawer(p.id));
    $('#dToggle', d).onclick = () => bulkAction(p.enabled ? 'disable' : 'enable', [p.id]).then(() => openProxyDrawer(p.id));
    $('#dDel', d).onclick = () => deleteWithUndo([p.id]);
    const form = $('#ef', d);
    form.protocol.value = p.protocol;
    form.onsubmit = async (e) => {
      e.preventDefault();
      const body = {
        label: form.label.value, port: Number(form.port.value), protocol: form.protocol.value, auth_user: form.auth_user.value, auth_pass: form.auth_pass.value,
        tag: form.tag.value, note: form.note.value, enabled: form.enabled.checked, ipv4_fallback: form.ipv4_fallback.checked, scope: form.scope.value,
        max_conns: Number(form.max_conns.value) || 0, rate_kbps: Number(form.rate_kbps.value) || 0,
        relay_node_id: Number(form.relay_node_id.value) || 0, relay_port: Number(form.relay_port.value) || 0,
        expires_at: form.expires.value ? Math.floor(new Date(form.expires.value + 'T23:59:59').getTime() / 1000) : 0,
      };
      if (form.ip.value.trim() && form.ip.value.trim() !== p.ip) body.ip = form.ip.value.trim();
      try {
        await api('PUT', '/api/proxies/' + p.id, body);
        toast(t('saved')); await loadProxies(); drawProxyRows(); openProxyDrawer(p.id);
      } catch (err) { $('#efErr', d).innerHTML = alertBox('err', esc(err.message)); }
    };
    show(tab);
  });
}

function openEdit(p) { openProxyDrawer(p.id, 'edit'); }

// otherEndpoints lists the ways to reach a proxy from outside the LAN.
function otherEndpoints(p) {
  const rows = [];
  const auth = p.auth_user ? ':' + p.auth_user + ':' + p.auth_pass : '';
  if (p.relay_host) rows.push([t('via_relay') + (p.relay_up ? '' : ' ⚠'), `${p.relay_host}:${p.relay_port}${auth}`, p.relay_up ? '' : t('relay_down_hint')]);
  if (p.wan_host) rows.push([t('via_wan'), `${p.wan_host}:${p.port}${auth}`, t('via_wan_hint')]);
  if (p.ts_host) rows.push([t('via_tailscale'), `${p.ts_host}:${p.port}${auth}`, t('via_tailscale_hint')]);
  if (!rows.length) return '';
  return `<h3>${t('other_endpoints')}</h3>${rows.map(([l, v, h]) => `<div class="copy-line"><span class="lbl">${esc(l)}</span><code>${esc(v)}</code>
    <button class="small" data-copyline="${esc(v)}">${icon('copy', 'sm')}</button></div>${h ? `<p class="hint" style="margin:-4px 0 8px">${esc(h)}</p>` : ''}`).join('')}`;
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
      <label class="field">${t('max_conns')}<input name="max_conns" type="number" min="0" value="0"></label>
      <label class="field">${t('rate_kbps')}<input name="rate_kbps" type="number" min="0" value="0"></label>
      <label class="field" id="relayWrap">${t('relay')}<span data-relay></span></label>
    </div>
    <p class="hint">${t('limits_hint')}</p>
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
      $('[data-relay]', m).innerHTML = relaySelect('relay_node_id', 0, node.id);
      $('#relayWrap', m).hidden = !relayNodes(node.id).length;
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
        scope: form.scope.value, max_conns: Number(form.max_conns.value) || 0, rate_kbps: Number(form.rate_kbps.value) || 0,
        relay_node_id: form.relay_node_id ? Number(form.relay_node_id.value) || 0 : 0,
      };
      try {
        const r = await api('POST', '/api/proxies/bulk', body);
        closeModal(); toast(t('created_n', { n: r.created }));
        await loadProxies(); drawProxyRows();
      } catch (err) { $('#cfErr', m).innerHTML = `<div class="alert err">${esc(err.message)}</div>`; }
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
      <label class="field">${t('connect_via')}<select id="xv">
        <option value="">${t('via_lan')}</option><option value="relay">${t('via_relay')}</option>
        <option value="wan">${t('via_wan')}</option><option value="tailscale">${t('via_tailscale')}</option></select></label>
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
        const r = await api('POST', '/api/proxies/export', { ids, format: $('#xf', m).value, scheme: $('#xs', m).value, host: $('#xh', m).value, via: $('#xv', m).value });
        $('#xt', m).value = r.text;
        $('#xnote', m).textContent = (EXPORT_PRESETS[$('#xp', m).value].format ? t('preset_note') + ' ' : '') + (r.skipped ? t('via_skipped', { n: r.skipped }) : '');
      } catch (e) { $('#xt', m).value = e.message; }
    };
    $('#xf', m).onchange = load; $('#xs', m).onchange = load; $('#xh', m).oninput = load; $('#xv', m).onchange = load;
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
    const open = new Set($$('details[open][data-keep]').map((d) => d.dataset.keep));
    if (!setHTML($('#page'), `<div class="page-head"><div><h1>${t('nav_nodes')}</h1><p>${t('nodes_hint')}</p></div><div class="spacer"></div>
      <button class="primary" id="btnAddNode">${icon('plus', 'sm')} ${t('add_node')}</button></div>
      ${nodes.map(nodeCard).join('')}`)) return;
    for (const d of $$('details[data-keep]')) if (open.has(d.dataset.keep)) d.open = true;
    $('#btnAddNode').onclick = openAddNode;
    for (const card of $$('.node-card')) bindNodeCard(card, nodes.find((n) => n.id === Number(card.dataset.id)));
  };
  await draw();
  every(10000, () => { if (!document.activeElement || !document.activeElement.closest('.node-card')) draw().catch(() => {}); });
}

function osName(os) {
  const p = String(os).split('/')[0];
  return { linux: 'Linux', windows: 'Windows', darwin: 'macOS' }[p] || p;
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

function sharingSummary(n) {
  const out = [];
  const u = n.upnp_state;
  if (n.upnp) {
    const cls = !u || u.state === 'pending' ? 'neutral' : u.state === 'ok' ? 'ok' : u.state === 'cgnat' ? 'warn' : 'error';
    const txt = !u || u.state === 'pending' ? t('upnp_pending') : u.state === 'ok' ? t('upnp_ok', { n: u.mapped, ip: u.external_ip || '?' }) : t('upnp_' + u.state);
    out.push(`<span class="badge ${cls}" title="${esc((u && u.error) || '')}">UPnP: ${esc(txt)}</span>`);
  }
  if (n.relay_port) {
    const c = (n.relays || []).filter((r) => r.role === 'server' && r.connected).length;
    out.push(`<span class="badge accent">${t('relay_server_on', { p: n.relay_port, n: c })}</span>`);
  }
  for (const r of (n.relays || []).filter((x) => x.role === 'client')) {
    const rn = (state.nodes || []).find((x) => x.id === r.peer_node_id);
    out.push(`<span class="badge ${r.connected ? 'ok' : 'error'}" title="${esc(r.error || '')}">${t('relay_to', { n: esc(rn ? rn.name : r.peer) })}: ${r.connected ? t('connected') : t('disconnected')}</span>`);
  }
  if (n.net.tailscale_ip) out.push(`<span class="badge neutral">Tailscale ${esc(n.net.tailscale_ip)}</span>`);
  return out.join(' ') || `<span class="muted">${t('sharing_lan_only')}</span>`;
}

function nodeCard(n) {
  const ifaces = (n.net.interfaces || []);
  const pools = (n.pools || []).map((p) => `<tr data-pool="${p.id}"><td>${esc(p.name)}${p.auto ? ` <span class="badge offline">auto</span>` : ''}</td>
    <td class="mono small">${esc(p.cidr || t('pool_waiting'))}</td><td class="small">${esc(p.mode)}</td>
    <td>${p.auto ? '' : `<button class="small danger" data-delpool="${p.id}">${t('act_delete')}</button>`}</td></tr>`).join('');
  const checks = n.checks || [];
  const bad = checks.filter((c) => !c.ok);
  return `<div class="card node-card" data-id="${n.id}">
    <div class="card-head" style="flex-wrap:wrap">
      <div class="stat"><div class="ico ${n.online ? (bad.some((c) => !c.warn) ? 'warn' : 'ok') : ''}">${icon('nodes')}</div></div>
      <div style="min-width:0"><h2>${esc(n.name)}</h2>
        <div class="row small" style="gap:6px;margin-top:4px">${n.online ? `<span class="status"><span class="dot ok"></span>${t('online')}</span>` : `<span class="status"><span class="dot"></span>${t('st_offline')}</span>`}
        ${n.paused ? badge('paused') : ''} ${n.os ? `<span class="badge neutral">${esc(osName(n.os))}</span>` : ''}
        ${n.is_local ? `<span class="badge accent">${t('local')}</span>` : ''}
        ${n.outdated ? `<span class="badge warn">v${esc(n.agent_version)}</span>` : ''}</div></div>
      <div class="spacer"></div>
      <div class="row">
        ${n.outdated ? `<button class="small primary" data-a="update">${icon('download', 'sm')} ${t('update_node', { v: esc(n.agent_version) })}</button>` : ''}
        <button class="small" data-a="test">${icon('pulse', 'sm')} ${t('test_v6')}</button>
        <button class="small" data-a="logs">${icon('terminal', 'sm')} ${t('logs')}</button>
        <button class="small ${n.paused ? 'primary' : ''}" data-a="pause">${icon(n.paused ? 'play' : 'pause', 'sm')} ${n.paused ? t('resume_all') : t('pause_all')}</button>
        <button class="small ghost icon-btn" data-a="more" title="${t('more')}">${icon('more', 'sm')}</button>
      </div>
    </div>
    ${(n.errors || []).map((e) => alertBox('err', esc(e))).join('')}
    <div class="grid cols-2" style="gap:18px">
      <div class="kv">
        <div>${t('service')}</div><div>${n.online ? t('running_for', { d: fmtDuration(n.uptime) }) : t('st_offline') + ' · ' + ago(n.last_seen)}</div>
        <div>${t('hostname')}</div><div>${esc(n.hostname || '—')} <span class="muted small">${esc(n.os || '')} ${n.agent_version ? 'v' + esc(n.agent_version) : ''}</span></div>
        <div>${t('ipv6_iface')}</div><div>${esc(n.net.primary_iface || '—')}${n.net.gateway ? ` <span class="muted small">${t('router')} ${esc(n.net.gateway)}</span>` : ''} ${ifaces.map((i) => `<div class="mono small">${esc(i.name)}: ${esc((i.prefixes || []).join(', ') || '—')}</div>`).join('')}</div>
        <div>${t('ipv4')}</div><div class="mono small">LAN ${esc((n.net.lan_v4 || []).join(', ') || '—')} · WAN ${esc(n.net.public_v4 || '—')}</div>
        <div>${t('vpn')}</div><div>${vpnSummary(n)}</div>
        <div>${t('sharing')}</div><div>${sharingSummary(n)}</div>
        <div>${t('proxies')}</div><div>${n.proxies}</div>
      </div>
      <div>
        <div class="small muted" style="margin-bottom:8px;font-weight:550">${t('system_checks')}</div>
        <div class="checks">${checks.map((c) => `<span class="badge ${c.ok ? 'ok' : (c.warn ? 'warn' : 'error')}" title="${esc(c.detail)}">${icon(c.ok ? 'check' : 'alert', 'sm')} ${esc(t('chk_' + c.id))}</span>`).join('') || '—'}</div>
        ${bad.map((c) => alertBox(c.warn ? 'warn' : 'err', `<b>${esc(t('chk_' + c.id))}:</b> ${esc(t('chkfix_' + c.id))}${c.detail ? ` <span class="muted">(${esc(c.detail)})</span>` : ''}`)).join('').replace(/alert /g, 'alert small ')}
      </div>
    </div>
    <details class="section" data-keep="set-${n.id}"><summary><b>${icon('settings', 'sm')} ${t('settings')}</b> <span class="muted small">— ${t('node_settings_sub')}</span></summary>
    <div style="margin-top:14px">
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
      <div><button class="primary" type="submit">${icon('check', 'sm')} ${t('save')}</button></div>
    </form></div></details>
    <details class="section" data-keep="share-${n.id}"><summary><b>${icon('globe', 'sm')} ${t('sharing_internet')}</b> <span class="muted small">— ${t('sharing_sub')}</span></summary>
    <div style="margin-top:14px">
    <form class="stack" data-form="share">
      <div class="choice">
        <label><input type="checkbox" name="upnp" ${n.upnp ? 'checked' : ''}><span><b>${t('upnp')}</b><span class="small muted">${t('upnp_hint')}</span></span></label>
        <label><input type="checkbox" name="relay_on" ${n.relay_port ? 'checked' : ''}><span><b>${t('relay_server')}</b><span class="small muted">${t('relay_server_hint')}</span></span></label>
      </div>
      <label class="field" data-relayport ${n.relay_port ? '' : 'hidden'}>${t('relay_port')}<input name="relay_port" type="number" min="1024" max="65535" value="${n.relay_port || 7443}">
        <span class="hint">${t('relay_port_hint')}</span></label>
      ${n.relay_fp ? `<div class="small muted">${t('relay_fp')}: <span class="mono">${esc(n.relay_fp.slice(0, 16))}…</span></div>` : ''}
      <div class="hint">${t('sharing_howto')}</div>
      <div><button class="primary" type="submit">${icon('check', 'sm')} ${t('save')}</button></div>
    </form></div></details>
    <details class="section" data-keep="pool-${n.id}"><summary><b>${icon('globe', 'sm')} ${t('pools')}</b> <span class="muted small">— ${(n.pools || []).map((p) => esc(p.cidr || t('pool_waiting'))).join(', ')}</span></summary>
    <div style="margin-top:14px">
    <div class="table-wrap"><table><thead><tr><th>${t('name')}</th><th>CIDR</th><th>${t('mode')}</th><th></th></tr></thead><tbody>${pools}</tbody></table></div>
    <form class="row" data-form="pool" style="margin-top:8px">
      <input name="cidr" placeholder="2a01:4f8:abcd::/48" class="mono" required style="min-width:240px">
      <select name="mode"><option value="anyip">anyip (${t('routed')})</option><option value="addr">addr (/64 on-link)</option></select>
      <input name="name" placeholder="${t('name')}">
      <button type="submit">+ ${t('add_pool')}</button>
    </form>
    <p class="hint">${t('pool_hint')}</p></div></details>
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
  act('pause').onclick = async () => {
    try { await api('POST', `/api/nodes/${n.id}/pause`, { paused: !n.paused }); toast(n.paused ? t('resumed') : t('paused_msg')); renderPage(); }
    catch (err) { toast(err.message, true); }
  };
  act('more').onclick = () => openNodeMenu(n);
  const reapply = async () => {
    if (!(await confirmBox(t('confirm_reapply')))) return;
    try { await api('POST', `/api/nodes/${n.id}/reapply`, {}); toast(t('reapply_sent')); } catch (err) { toast(err.message, true); }
  };
  const restart = async () => {
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
  n._actions = {
    reapply, restart,
    install: async () => showInstall(await api('GET', `/api/nodes/${n.id}/install`), n.name),
    rotate: async () => { if (await confirmBox(t('confirm_rotate'), { danger: true })) showInstall(await api('POST', `/api/nodes/${n.id}/rotate-token`, {}), n.name); },
    delete: async () => {
      if (!(await confirmBox(t('confirm_delete_node', { name: n.name, n: n.proxies }), { danger: true, ok: t('act_delete') }))) return;
      try { await api('DELETE', '/api/nodes/' + n.id); renderPage(); } catch (e) { toast(e.message, true); }
    },
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
  const fs = $('[data-form=share]', card);
  fs.relay_on.onchange = () => { $('[data-relayport]', card).hidden = !fs.relay_on.checked; };
  fs.onsubmit = async (e) => {
    e.preventDefault();
    try {
      await api('PUT', '/api/nodes/' + n.id, { upnp: fs.upnp.checked, relay_port: fs.relay_on.checked ? Number(fs.relay_port.value) || 7443 : 0 });
      toast(t('saved')); renderPage();
    } catch (err) { toast(err.message, true); }
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

function openNodeMenu(n) {
  const items = [['reapply', 'refresh', t('reapply'), t('reapply_hint')], ['restart', 'refresh', t('restart'), t('restart_hint')]];
  if (!n.is_local) items.push(['install', 'terminal', t('install_cmd'), t('install_cmd_hint')], ['rotate', 'key', t('rotate_token'), t('rotate_hint')], ['delete', 'trash', t('act_delete'), t('delete_node_hint')]);
  modal(`<h2>${esc(n.name)}</h2><div class="choice">${items.map(([k, ic, l, h]) => `<label data-k="${k}" style="align-items:center">
      <span class="stat"><span class="ico ${k === 'delete' ? 'warn' : ''}">${icon(ic)}</span></span><span><b>${esc(l)}</b><span class="small muted">${esc(h)}</span></span></label>`).join('')}</div>
    <div class="actions"><button id="cl">${t('close')}</button></div>`, (m) => {
    for (const l of $$('[data-k]', m)) l.onclick = () => { closeModal(); n._actions[l.dataset.k](); };
    $('#cl', m).onclick = closeModal;
  });
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
  const tabs = [['linux', 'Linux', r.command, 'install_hint'], ['windows', 'Windows', r.command_windows, 'install_hint_win'], ['macos', 'macOS', r.command_macos, 'install_hint_mac']];
  modal(`<h2>${t('install_on', { name: esc(name) })}</h2>
    <div class="tabs">${tabs.map(([k, l], i) => `<button class="small ${i ? '' : 'primary'}" data-tab="${k}">${l}</button>`).join('')}</div>
    ${tabs.map(([k, , cmd, hint], i) => `<div data-pane="${k}" ${i ? 'hidden' : ''}><p class="small">${t(hint)}</p><div class="cmd">${esc(cmd || '')}</div></div>`).join('')}
    <p class="hint">${t('install_hint2')}</p>
    <div class="actions"><button class="primary" id="cp">${t('copy')}</button><button id="cl">${t('close')}</button></div>`,
  (m) => {
    let cur = 0;
    for (const b of $$('[data-tab]', m)) b.onclick = () => {
      cur = tabs.findIndex((x) => x[0] === b.dataset.tab);
      for (const x of $$('[data-tab]', m)) x.classList.toggle('primary', x === b);
      for (const p of $$('[data-pane]', m)) p.hidden = p.dataset.pane !== b.dataset.tab;
    };
    $('#cp', m).onclick = () => copyText(tabs[cur][2] || '');
    $('#cl', m).onclick = () => { closeModal(); if (state.page === 'nodes') renderPage(); };
  });
}

// ---------- events ----------
async function pageEvents() {
  const [ev, nodes] = await Promise.all([api('GET', '/api/events'), api('GET', '/api/nodes')]);
  $('#page').innerHTML = `<div class="page-head"><div><h1>${t('nav_events')}</h1><p>${t('events_sub')}</p></div></div>
    <div class="card">${eventsList(ev, nodes)}</div>`;
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

    <div class="card"><h2>${t('api_tokens')}</h2>
      <p class="small muted" style="margin-top:0">${t('api_tokens_hint')}</p>
      <div id="tokList"><div class="skel"></div></div>
      <form class="row" id="tokf" style="margin-top:10px"><input name="name" placeholder="${t('token_name_ph')}" style="flex:1;min-width:160px">
        <button type="submit">${icon('plus', 'sm')} ${t('create_token')}</button></form></div>

    <form class="card" id="pf"><h2>${t('change_password')}</h2>
      <label class="field">${t('current_password')}<input name="current" type="password" autocomplete="current-password" required></label>
      <label class="field" style="margin-top:8px">${t('new_password')}<input name="new" type="password" minlength="8" autocomplete="new-password" required></label>
      <div style="margin-top:12px"><button class="primary" type="submit">${t('save')}</button></div></form>

    <div class="card"><h2>${t('language')}</h2>
      <div class="row"><button id="lvi" class="${LANG === 'vi' ? 'primary' : ''}">Tiếng Việt</button><button id="len" class="${LANG === 'en' ? 'primary' : ''}">English</button></div>
      <h3>${t('theme')}</h3>
      <div class="seg" id="setTheme">${['light', 'system', 'dark'].map((m) => `<button data-th2="${m}" class="${themeMode() === m ? 'on' : ''}">${icon({ light: 'sun', system: 'monitor', dark: 'moon' }[m], 'sm')} ${t('theme_' + m)}</button>`).join('')}</div>
      <h3>${t('accent')}</h3>
      <div class="seg" id="setAccent">${[['panel', '#171717'], ['moon', '#6366f1'], ['grass', '#10b981']].map(([m, c]) => `<button data-ac="${m}" class="${accentMode() === m ? 'on' : ''}"><span class="swatch" style="background:${c}"></span> ${t('accent_' + m)}</button>`).join('')}</div>
      <h3>${t('shortcuts')}</h3><p class="small muted">${t('shortcuts_text')}</p>
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
  const loadTokens = async () => {
    const ts = await api('GET', '/api/tokens').catch(() => []);
    $('#tokList').innerHTML = ts.length ? ts.map((x) => `<div class="row" style="padding:8px 0;border-bottom:1px solid var(--border);flex-wrap:nowrap">
      <div style="flex:1;min-width:0"><b>${esc(x.name)}</b><div class="small muted"><span class="mono">${esc(x.prefix)}…</span> · ${t('token_used')} ${x.last_used ? ago(x.last_used) : t('never')}</div></div>
      <button class="small danger icon-btn" data-deltok="${x.id}" title="${t('act_delete')}">${icon('trash', 'sm')}</button></div>`).join('') : `<p class="small muted">${t('no_tokens')}</p>`;
    for (const b of $$('[data-deltok]')) b.onclick = async () => {
      if (!(await confirmBox(t('confirm_delete_token'), { danger: true, ok: t('act_delete') }))) return;
      try { await api('DELETE', '/api/tokens/' + b.dataset.deltok); loadTokens(); } catch (err) { toast(err.message, true); }
    };
  };
  loadTokens();
  $('#tokf').onsubmit = async (e) => {
    e.preventDefault();
    try {
      const r = await api('POST', '/api/tokens', { name: e.target.name.value });
      e.target.reset(); loadTokens();
      modal(`<h2>${t('token_created')}</h2><p class="small">${t('token_once')}</p>
        <div class="copy-line"><code>${esc(r.token)}</code><button class="small" id="tc">${icon('copy', 'sm')}</button></div>
        <p class="hint">${t('token_usage')}</p><pre class="mono small" style="white-space:pre-wrap">curl -H "Authorization: Bearer ${esc(r.token)}" ${esc(location.origin)}/api/proxies</pre>
        <div class="actions"><button class="primary" id="cl">${t('close')}</button></div>`, (m) => {
        $('#tc', m).onclick = () => copyText(r.token);
        $('#cl', m).onclick = closeModal;
      });
    } catch (err) { toast(err.message, true); }
  };
  $('#pf').onsubmit = async (e) => {
    e.preventDefault();
    try { await api('POST', '/api/account/password', { current: e.target.current.value, new: e.target.new.value }); toast(t('saved')); renderLogin(false); }
    catch (err) { toast(err.message, true); }
  };
  const setLang = (l) => { LANG = l; store('v6p_lang', l); api('PUT', '/api/settings', { lang: l }).catch(() => {}); renderShell(); };
  for (const b of $$('[data-ac]')) b.onclick = () => { store('v6p_accent', b.dataset.ac); applyTheme(); renderShell(); };
  for (const b of $$('[data-th2]')) b.onclick = () => { store('v6p_theme', b.dataset.th2); applyTheme(); renderShell(); };
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
