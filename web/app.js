'use strict';

(() => {
  // ============================================================
  // Утилиты
  // ============================================================

  const $ = (sel, root = document) => root.querySelector(sel);
  const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];

  function el(tag, attrs, ...kids) {
    const node = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) {
      if (v == null || v === false) continue;
      if (k === 'class') node.className = v;
      else if (k === 'text') node.textContent = v;
      else if (k === 'value') node.value = v;
      else if (k.startsWith('on') && typeof v === 'function') node.addEventListener(k.slice(2).toLowerCase(), v);
      else if (v === true) node.setAttribute(k, '');
      else node.setAttribute(k, v);
    }
    for (const kid of kids.flat(3)) {
      if (kid == null || kid === false) continue;
      node.append(kid.nodeType ? kid : document.createTextNode(String(kid)));
    }
    return node;
  }

  const SVG_NS = 'http://www.w3.org/2000/svg';
  function icon(name) {
    const svg = document.createElementNS(SVG_NS, 'svg');
    svg.setAttribute('class', 'icon');
    svg.setAttribute('aria-hidden', 'true');
    const use = document.createElementNS(SVG_NS, 'use');
    use.setAttribute('href', '#i-' + name);
    svg.append(use);
    return svg;
  }

  const REACTIONS = ['👍', '❤️', '😂', '🎉', '👀', '✅'];
  const COLORS = ['#E8A33D', '#7FB08A', '#7FA7C9', '#C98FA8', '#B9A26B', '#8E9BD6', '#D08A6A', '#6FB3AE', '#A98BC4', '#C4A35A'];

  const timeFmt = new Intl.DateTimeFormat('ru-RU', { hour: '2-digit', minute: '2-digit' });
  const dateFmt = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long' });
  const fullFmt = new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', year: 'numeric', hour: '2-digit', minute: '2-digit' });

  function initials(name) {
    const parts = String(name || '?').trim().split(/\s+/).slice(0, 2);
    return parts.map(p => p[0] ? p[0].toUpperCase() : '').join('') || '?';
  }

  function fmtTime(iso) {
    const d = new Date(iso);
    return isNaN(d) ? '' : timeFmt.format(d);
  }

  function fmtSize(n) {
    if (!n && n !== 0) return '';
    if (n < 1024) return n + ' Б';
    if (n < 1024 * 1024) return (n / 1024).toFixed(n < 10 * 1024 ? 1 : 0) + ' КБ';
    return (n / (1024 * 1024)).toFixed(1) + ' МБ';
  }

  function dayLabel(iso) {
    const d = new Date(iso);
    const today = new Date();
    today.setHours(0, 0, 0, 0);
    const yesterday = new Date(today);
    yesterday.setDate(today.getDate() - 1);
    const dd = new Date(d);
    dd.setHours(0, 0, 0, 0);
    if (+dd === +today) return 'Сегодня';
    if (+dd === +yesterday) return 'Вчера';
    return dateFmt.format(d);
  }

  function sameDay(a, b) {
    const x = new Date(a), y = new Date(b);
    return x.getFullYear() === y.getFullYear() && x.getMonth() === y.getMonth() && x.getDate() === y.getDate();
  }

  function presenceText(user) {
    if (!user) return '';
    if (state.online.has(user.id)) return 'в сети';
    if (!user.lastSeen) return 'не в сети';
    const d = new Date(user.lastSeen);
    const diff = Date.now() - d.getTime();
    if (diff < 90 * 1000) return 'был(а) только что';
    if (diff < 60 * 60 * 1000) {
      const m = Math.max(1, Math.floor(diff / 60000));
      return `был(а) ${m} мин назад`;
    }
    if (sameDay(d, new Date())) return `был(а) в ${timeFmt.format(d)}`;
    return `был(а) ${dateFmt.format(d)}`;
  }

  function linkify(text) {
    const frag = document.createDocumentFragment();
    const re = /(https?:\/\/[^\s<>"']+)/g;
    let last = 0, m;
    while ((m = re.exec(text))) {
      if (m.index > last) frag.append(text.slice(last, m.index));
      let href = m[1];
      let tail = '';
      while (/[.,;:!?)]$/.test(href)) {
        tail = href.slice(-1) + tail;
        href = href.slice(0, -1);
      }
      frag.append(el('a', { href, target: '_blank', rel: 'noopener noreferrer' }, href));
      if (tail) frag.append(tail);
      last = m.index + m[1].length;
    }
    if (last < text.length) frag.append(text.slice(last));
    return frag;
  }

  function escapeReg(s) { return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'); }

  // ---------- Упоминания ----------

  const MENTION_RE = /(^|[\s([{«"'“])@([a-z0-9._-]{2,24})\b/gi;

  function userByLogin(login) {
    const q = String(login || '').toLowerCase();
    for (const u of state.users.values()) {
      if (u.login.toLowerCase() === q) return u;
    }
    return null;
  }

  function mentionIds(text) {
    const ids = [];
    if (!text) return ids;
    const re = new RegExp(MENTION_RE.source, 'gi');
    let m;
    while ((m = re.exec(text))) {
      const u = userByLogin(m[2]);
      if (u) ids.push(u.id);
      else if (m[2].toLowerCase() === 'all') ids.push('all');
    }
    return ids;
  }

  function mentionsMe(text) {
    if (!state.me) return false;
    return mentionIds(text).some(id => id === 'all' || id === state.me.id);
  }

  // renderText рисует текст сообщения: ссылки и @упоминания.
  function renderText(text) {
    const frag = document.createDocumentFragment();
    const re = new RegExp(MENTION_RE.source, 'gi');
    let last = 0, m;
    while ((m = re.exec(text))) {
      const at = m.index + m[1].length;
      if (at > last) frag.append(linkify(text.slice(last, at)));
      const login = m[2].toLowerCase();
      const u = userByLogin(login);
      const isAll = login === 'all' && !u;
      if (u || isAll) {
        const mine = state.me && (isAll || u.id === state.me.id);
        frag.append(el('span', {
          class: 'mention' + (mine ? ' is-me' : ''),
          title: isAll ? 'Упоминание всех участников' : '@' + u.login,
        }, '@' + login));
      } else {
        frag.append('@' + m[2]);
      }
      last = at + 1 + m[2].length;
    }
    if (last < text.length) frag.append(linkify(text.slice(last)));
    return frag;
  }

  function highlight(text, q) {
    const frag = document.createDocumentFragment();
    if (!q) { frag.append(text); return frag; }
    const re = new RegExp(escapeReg(q), 'gi');
    let last = 0, m;
    while ((m = re.exec(text))) {
      if (m.index > last) frag.append(text.slice(last, m.index));
      frag.append(el('mark', {}, m[0]));
      last = m.index + m[0].length;
      if (m[0].length === 0) break;
    }
    if (last < text.length) frag.append(text.slice(last));
    return frag;
  }

  function debounce(fn, ms) {
    let t;
    return (...args) => {
      clearTimeout(t);
      t = setTimeout(() => fn(...args), ms);
    };
  }

  async function copyText(text) {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch (_) { /* http без https */ }
    const ta = el('textarea', { style: 'position:fixed;opacity:0' });
    ta.value = text;
    document.body.append(ta);
    ta.select();
    let ok = false;
    try { ok = document.execCommand('copy'); } catch (_) {}
    ta.remove();
    return ok;
  }

  // ============================================================
  // Состояние
  // ============================================================

  const state = {
    me: null,
    users: new Map(),
    chats: new Map(),
    messages: new Map(),
    hasMore: new Map(),
    pins: new Map(),
    online: new Set(),
    unread: new Map(),
    typing: new Map(),
    activeChat: null,
    replyTo: null,
    editing: null,
    pendingFiles: [],
    drafts: {},
    ws: null,
    wsRetry: 0,
    wsOpened: false,
    sound: localStorage.getItem('oc-sound') !== '0',
    theme: localStorage.getItem('oc-theme') || 'dark',
    filter: '',
    searchQuery: '',
    version: '',
    loadingOlder: false,
    lastRead: { id: null, t: 0 },
    hasUsers: false,
    notify: localStorage.getItem('oc-notify') === '1',
    mentions: new Set(),
  };

  class ApiError extends Error {
    constructor(message, status) { super(message); this.status = status; }
  }

  async function api(path, { method = 'GET', body, form } = {}) {
    const opts = { method, credentials: 'same-origin', headers: {} };
    if (form) opts.body = form;
    else if (body !== undefined) {
      opts.headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(body);
    }
    let res;
    try {
      res = await fetch(path, opts);
    } catch (_) {
      throw new ApiError('Нет связи с сервером', 0);
    }
    if (res.status === 401) {
      showAuth();
      throw new ApiError('Нужно войти в чат', 401);
    }
    let data = {};
    try { data = await res.json(); } catch (_) {}
    if (!res.ok) throw new ApiError(data.error || `Ошибка ${res.status}`, res.status);
    return data;
  }

  // ============================================================
  // Звук
  // ============================================================

  let audioCtx = null;
  function initAudio() {
    if (!audioCtx) {
      const AC = window.AudioContext || window.webkitAudioContext;
      if (AC) audioCtx = new AC();
    }
    if (audioCtx && audioCtx.state === 'suspended') audioCtx.resume().catch(() => {});
  }

  function playPing() {
    if (!state.sound) return;
    initAudio();
    if (!audioCtx) return;
    const t = audioCtx.currentTime;
    const gain = audioCtx.createGain();
    gain.gain.setValueAtTime(0.0001, t);
    gain.gain.exponentialRampToValueAtTime(0.09, t + 0.012);
    gain.gain.exponentialRampToValueAtTime(0.0001, t + 0.26);
    gain.connect(audioCtx.destination);

    const o1 = audioCtx.createOscillator();
    o1.type = 'sine';
    o1.frequency.setValueAtTime(784, t);
    o1.frequency.setValueAtTime(988, t + 0.09);
    o1.connect(gain);
    o1.start(t);
    o1.stop(t + 0.28);
  }

  // playMention — короткая трель, когда упомянули лично.
  function playMention() {
    if (!state.sound) return;
    initAudio();
    if (!audioCtx) return;
    const t = audioCtx.currentTime;
    [880, 1174.66, 1567.98].forEach((freq, i) => {
      const start = t + i * 0.1;
      const gain = audioCtx.createGain();
      gain.gain.setValueAtTime(0.0001, start);
      gain.gain.exponentialRampToValueAtTime(0.08, start + 0.01);
      gain.gain.exponentialRampToValueAtTime(0.0001, start + 0.2);
      gain.connect(audioCtx.destination);
      const o = audioCtx.createOscillator();
      o.type = 'sine';
      o.frequency.setValueAtTime(freq, start);
      o.connect(gain);
      o.start(start);
      o.stop(start + 0.22);
    });
  }

  function notifyMessage(m, chat, mention) {
    if (!state.notify || !('Notification' in window)) return;
    if (Notification.permission !== 'granted') return;
    if (document.visibilityState === 'visible' && document.hasFocus()) return;
    const author = state.users.get(m.userId);
    const title = chat.isDm
      ? (author ? author.name : 'Личные сообщения')
      : `${chatTitle(chat)}${author ? ' · ' + author.name : ''}`;
    let body = m.text || (m.files && m.files.length ? '📎 ' + m.files[0].name : 'Новое сообщение');
    if (body.length > 180) body = body.slice(0, 177) + '…';
    try {
      const n = new Notification(title, {
        body,
        tag: 'oc-' + m.chatId,
        icon: '/favicon.svg',
        silent: state.sound,
      });
      n.onclick = () => {
        window.focus();
        openChat(m.chatId);
        n.close();
      };
    } catch (_) {}
  }

  // ============================================================
  // Тосты и модальные окна
  // ============================================================

  function toast(text, kind = 'ok', ms = 3600) {
    const box = $('#toasts');
    const node = el('div', { class: 'toast' + (kind === 'error' ? ' is-error' : '') },
      icon(kind === 'error' ? 'x' : 'check'),
      el('span', {}, text));
    box.append(node);
    setTimeout(() => {
      node.style.opacity = '0';
      node.style.transform = 'translateY(6px)';
      node.style.transition = 'opacity .18s, transform .18s';
      setTimeout(() => node.remove(), 220);
    }, ms);
  }

  const modalStack = [];

  function openModal({ title, body, footer, wide = false, onClose }) {
    const overlay = el('div', { class: 'modal-overlay' });
    const modal = el('div', { class: 'modal' + (wide ? ' is-wide' : ''), role: 'dialog', 'aria-modal': 'true' });
    const close = () => {
      overlay.remove();
      const i = modalStack.indexOf(close);
      if (i >= 0) modalStack.splice(i, 1);
      if (onClose) onClose();
    };
    const head = el('div', { class: 'modal-head' },
      el('h2', {}, title),
      el('button', { class: 'icon-btn', 'aria-label': 'Закрыть', onclick: close }, icon('x')));
    const bodyEl = el('div', { class: 'modal-body' }, body);
    modal.append(head, bodyEl);
    if (footer) modal.append(el('div', { class: 'modal-foot' }, footer));
    overlay.append(modal);
    overlay.addEventListener('mousedown', (e) => { if (e.target === overlay) close(); });
    $('#modalRoot').append(overlay);
    modalStack.push(close);
    setTimeout(() => {
      const focusable = modal.querySelector('input, textarea, button.btn-primary, button');
      if (focusable) focusable.focus();
    }, 30);
    return { close, modal, body: bodyEl };
  }

  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && modalStack.length) {
      e.preventDefault();
      modalStack[modalStack.length - 1]();
    }
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
      e.preventDefault();
      toggleSearch(true);
    }
  });

  function confirmDialog({ title, text, confirm = 'Подтвердить', danger = false }) {
    return new Promise((resolve) => {
      let done = false;
      const finish = (v) => { if (!done) { done = true; resolve(v); } };
      const m = openModal({
        title,
        body: el('p', { class: 'modal-text' }, text),
        footer: [
          el('button', { class: 'btn btn-ghost', onclick: () => { finish(false); m.close(); } }, 'Отмена'),
          el('button', { class: 'btn ' + (danger ? 'btn-danger' : 'btn-primary'), onclick: () => { finish(true); m.close(); } }, confirm),
        ],
        onClose: () => finish(false),
      });
    });
  }

  // ============================================================
  // Вход и регистрация
  // ============================================================

  function showAuth() {
    state.me = null;
    state.activeChat = null;
    if (state.ws) { try { state.ws.close(); } catch (_) {} state.ws = null; }
    $('#app').hidden = true;
    $('#auth').hidden = false;
    document.title = 'Офис-чат';
    setTimeout(() => $('#loginName').focus(), 50);
  }

  function switchTab(login) {
    $('#tabLogin').classList.toggle('is-on', login);
    $('#tabRegister').classList.toggle('is-on', !login);
    $('#tabLogin').setAttribute('aria-selected', String(login));
    $('#tabRegister').setAttribute('aria-selected', String(!login));
    $('#loginForm').hidden = !login;
    $('#registerForm').hidden = login;
    $('#loginError').textContent = '';
    $('#registerError').textContent = '';
    setTimeout(() => (login ? $('#loginName') : $('#regName')).focus(), 30);
  }

  async function submitLogin(e) {
    e.preventDefault();
    const btn = $('#loginBtn');
    const err = $('#loginError');
    err.textContent = '';
    btn.disabled = true;
    try {
      await api('/api/login', {
        method: 'POST',
        body: { login: $('#loginName').value.trim(), password: $('#loginPass').value },
      });
      $('#loginPass').value = '';
      await startApp();
    } catch (ex) {
      err.textContent = ex.message;
    } finally {
      btn.disabled = false;
    }
  }

  async function submitRegister(e) {
    e.preventDefault();
    const btn = $('#registerBtn');
    const err = $('#registerError');
    err.textContent = '';
    btn.disabled = true;
    try {
      await api('/api/register', {
        method: 'POST',
        body: {
          name: $('#regName').value.trim(),
          login: $('#regLogin').value.trim().toLowerCase(),
          password: $('#regPass').value,
        },
      });
      $('#regPass').value = '';
      await startApp();
    } catch (ex) {
      err.textContent = ex.message;
    } finally {
      btn.disabled = false;
    }
  }

  // ============================================================
  // Запуск приложения
  // ============================================================

  async function boot() {
    applyTheme(state.theme);
    try {
      const st = await fetch('/api/state', { credentials: 'same-origin' }).then(r => r.json());
      state.hasUsers = !!st.hasUsers;
      state.version = st.version || '';
      $('#authVersion').textContent = st.version ? `версия ${st.version}` : '';
    } catch (_) {}

    const meRes = await fetch('/api/me', { credentials: 'same-origin' }).catch(() => null);
    if (meRes && meRes.ok) {
      await startApp();
    } else {
      showAuth();
      if (!state.hasUsers) {
        switchTab(false);
        $('#firstUserHint').hidden = false;
      }
    }
  }

  async function startApp() {
    $('#auth').hidden = true;
    $('#app').hidden = false;
    try {
      await bootstrap();
      connectWS();
    } catch (ex) {
      if (ex.status !== 401) {
        toast(ex.message, 'error');
        showAuth();
      }
    }
  }

  async function bootstrap() {
    const data = await api('/api/bootstrap');
    state.me = data.me;
    state.users = new Map(data.users.map(u => [u.id, u]));
    const fresh = new Map(data.chats.map(c => [c.id, c]));
    for (const [id] of state.chats) {
      if (!fresh.has(id)) {
        state.chats.delete(id);
        state.messages.delete(id);
      }
    }
    for (const [id, c] of fresh) state.chats.set(id, c);
    state.mentions.clear();
    for (const c of data.chats) {
      state.unread.set(c.id, c.unread || 0);
      if (c.unread && c.lastMessage && c.lastMessage.userId !== state.me.id && mentionsMe(c.lastMessage.text)) {
        state.mentions.add(c.id);
      }
    }
    state.online = new Set(data.online);
    state.version = data.version || state.version;

    renderMe();
    renderChats();
    renderMembers();

    const saved = localStorage.getItem('oc-chat');
    const target = (saved && state.chats.has(saved)) ? saved
      : (state.chats.has('general') ? 'general' : [...state.chats.keys()][0]);
    if (target) await openChat(target);
    else renderHeader();
  }

  function connectWS() {
    if (state.ws) return;
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
    let ws;
    try {
      ws = new WebSocket(`${proto}//${location.host}/api/ws`);
    } catch (_) {
      setTimeout(connectWS, 3000);
      return;
    }
    state.ws = ws;
    ws.onopen = () => {
      state.wsRetry = 0;
      if (state.wsOpened) resync();
      state.wsOpened = true;
    };
    ws.onmessage = (e) => {
      let ev;
      try { ev = JSON.parse(e.data); } catch (_) { return; }
      handleEvent(ev);
    };
    ws.onclose = () => {
      state.ws = null;
      const delay = Math.min(20000, 1000 * Math.pow(2, state.wsRetry++));
      setTimeout(connectWS, delay);
    };
    ws.onerror = () => { try { ws.close(); } catch (_) {} };
  }

  async function resync() {
    try {
      await bootstrap();
      const id = state.activeChat;
      if (id) {
        const data = await api(`/api/messages?chat=${encodeURIComponent(id)}&limit=50`);
        if (isAtBottom()) {
          state.messages.set(id, data.messages);
          state.hasMore.set(id, data.hasMore);
          renderFeed({ bottom: true });
        }
      }
    } catch (_) {}
  }

  // ============================================================
  // События WebSocket
  // ============================================================

  function handleEvent(ev) {
    switch (ev.type) {
      case 'hello': {
        state.online = new Set(ev.data.online || []);
        renderMembers();
        renderHeader();
        break;
      }
      case 'message': onIncoming(ev.data); break;
      case 'message.update': onUpdated(ev.data); break;
      case 'presence': {
        if (ev.data.online) state.online.add(ev.data.userId);
        else state.online.delete(ev.data.userId);
        const u = state.users.get(ev.data.userId);
        if (u && !ev.data.online) u.lastSeen = new Date().toISOString();
        renderMembers();
        renderHeader();
        break;
      }
      case 'typing': {
        const t = state.typing.get(ev.data.chatId) || new Map();
        t.set(ev.data.userId, { name: ev.data.name, until: Date.now() + 4500 });
        state.typing.set(ev.data.chatId, t);
        renderTyping();
        break;
      }
      case 'chat.new': {
        const c = ev.data;
        if (!c || !c.id) break;
        if (c.isDm && !(c.members || []).includes(state.me.id)) break;
        state.chats.set(c.id, c);
        renderChats();
        break;
      }
      case 'chat.update': {
        const c = ev.data;
        const old = state.chats.get(c.id);
        if (old) state.chats.set(c.id, { ...old, ...c });
        else state.chats.set(c.id, c);
        renderChats();
        if (state.activeChat === c.id) renderHeader();
        break;
      }
      case 'chat.deleted': {
        state.chats.delete(ev.data.chatId);
        state.messages.delete(ev.data.chatId);
        renderChats();
        if (state.activeChat === ev.data.chatId) {
          const next = state.chats.has('general') ? 'general' : [...state.chats.keys()][0];
          if (next) openChat(next);
        }
        break;
      }
      case 'user.new':
      case 'user.update': {
        const u = ev.data;
        state.users.set(u.id, { ...(state.users.get(u.id) || {}), ...u });
        if (state.me && u.id === state.me.id) {
          state.me = { ...state.me, ...u };
          renderMe();
        }
        renderMembers();
        renderChats();
        break;
      }
      case 'read': {
        if (ev.data.userId === state.me.id) {
          if (ev.data.chatId) {
            state.unread.set(ev.data.chatId, 0);
            state.mentions.delete(ev.data.chatId);
          } else {
            for (const id of state.unread.keys()) state.unread.set(id, 0);
            state.mentions.clear();
          }
          renderChats();
        }
        break;
      }
      default: break;
    }
  }

  function onIncoming(m) {
    const list = state.messages.get(m.chatId);
    if (list && !list.some(x => x.id === m.id)) list.push(m);

    let chat = state.chats.get(m.chatId);
    if (!chat) {
      bootstrap().catch(() => {});
      return;
    }
    chat.lastMessage = m;

    const mine = m.userId === state.me.id;
    const active = state.activeChat === m.chatId && document.visibilityState === 'visible';
    const mention = !mine && mentionsMe(m.text);

    if (active) {
      const atBottom = isAtBottom();
      renderFeed({ bottom: atBottom, preserve: !atBottom });
      markRead();
    } else if (!mine) {
      state.unread.set(m.chatId, (state.unread.get(m.chatId) || 0) + 1);
      if (mention) {
        state.mentions.add(m.chatId);
        playMention();
      } else {
        playPing();
      }
    }
    if (!mine) notifyMessage(m, chat, mention);
    renderChats();
  }

  function onUpdated(m) {
    const list = state.messages.get(m.chatId);
    let prev = null;
    if (list) {
      const i = list.findIndex(x => x.id === m.id);
      if (i >= 0) {
        prev = list[i];
        list[i] = m;
      }
    }
    const chat = state.chats.get(m.chatId);
    if (chat && chat.lastMessage && chat.lastMessage.id === m.id) chat.lastMessage = m;
    if (state.activeChat === m.chatId) {
      renderFeed({ preserve: true });
      if (prev && prev.pinned !== m.pinned) {
        const arr = state.pins.get(m.chatId) || [];
        const filtered = arr.filter(x => x.id !== m.id);
        if (m.pinned) filtered.push(m);
        state.pins.set(m.chatId, filtered);
        renderPins();
      }
    }
  }

  // ============================================================
  // Список чатов
  // ============================================================

  function chatTitle(chat) {
    if (!chat) return '';
    if (chat.isDm) {
      const other = dmPartner(chat) || chat.dmUser;
      return other ? other.name : 'Личный чат';
    }
    return chat.name || 'Без названия';
  }

  function dmPartner(chat) {
    if (!chat || !chat.isDm) return null;
    for (const id of chat.members || []) {
      if (id !== state.me.id) return state.users.get(id) || null;
    }
    return chat.dmUser || null;
  }

  function chatSubtitle(chat) {
    if (chat.isDm) {
      const other = chat.dmUser || dmPartner(chat);
      if (!other) return '';
      return state.online.has(other.id) ? 'в сети' : presenceText(other);
    }
    return chat.topic || '';
  }

  function lastPreview(chat) {
    const m = chat.lastMessage;
    if (!m) return 'Нет сообщений';
    const author = m.userId === state.me.id ? 'Вы' : (state.users.get(m.userId)?.name || 'Кто-то');
    let text = m.text || (m.files && m.files.length ? '📎 ' + m.files[0].name : '');
    if (!text) text = 'сообщение';
    return `${author}: ${text}`;
  }

  function lastTime(chat) {
    const m = chat.lastMessage;
    return m ? new Date(m.createdAt).getTime() : new Date(chat.createdAt).getTime();
  }

  function renderChats() {
    const box = $('#chatList');
    box.textContent = '';
    const q = state.filter.trim().toLowerCase();
    const all = [...state.chats.values()];
    const match = (c) => !q || chatTitle(c).toLowerCase().includes(q);
    const channels = all.filter(c => !c.isDm && match(c)).sort((a, b) => lastTime(b) - lastTime(a));
    const dms = all.filter(c => c.isDm && match(c)).sort((a, b) => lastTime(b) - lastTime(a));

    box.append(chatGroup('Каналы', channels, 'Создать канал', openChannelModal));
    box.append(chatGroup('Личные', dms, 'Написать сотруднику', openNewDmModal));

    let total = 0;
    for (const n of state.unread.values()) total += n || 0;
    document.title = total > 0 ? `(${total > 99 ? '99+' : total}) Офис-чат` : 'Офис-чат';
  }

  function chatGroup(title, chats, actionTitle, action) {
    const g = el('div', { class: 'chat-group' });
    g.append(el('div', { class: 'group-head' },
      el('h2', {}, title),
      el('button', { class: 'icon-btn', title: actionTitle, 'aria-label': actionTitle, onclick: action }, icon('plus'))));
    if (!chats.length) {
      g.append(el('div', { class: 'side-empty' }, state.filter ? 'Ничего не найдено' : 'Пока пусто'));
    }
    for (const c of chats) g.append(chatItem(c));
    return g;
  }

  function chatItem(chat) {
    const unread = state.unread.get(chat.id) || 0;
    const active = state.activeChat === chat.id;
    const node = el('button', {
      class: 'chat-item' + (active ? ' is-active' : '') + (unread ? ' has-unread' : ''),
      onclick: () => openChat(chat.id),
      title: chatTitle(chat),
    });

    if (chat.isDm) {
      const other = chat.dmUser || dmPartner(chat);
      node.append(avatarEl(other || { name: '?', color: '#555' }, other && state.online.has(other.id), 28));
    } else {
      node.append(el('span', { class: 'glyph' }, icon('hash')));
    }

    node.append(el('span', { class: 'chat-meta' },
      el('strong', {}, chatTitle(chat)),
      el('small', {}, lastPreview(chat))));

    if (unread) {
      const mentioned = state.mentions.has(chat.id);
      node.append(el('span', {
        class: 'badge' + (mentioned ? ' is-mention' : ''),
        title: mentioned ? 'Вас упомянули' : 'Непрочитанные сообщения',
      }, mentioned ? '@' : (unread > 99 ? '99+' : String(unread))));
    }
    return node;
  }

  // ============================================================
  // Шапка, участники, закреплённые
  // ============================================================

  function renderHeader() {
    const chat = state.chats.get(state.activeChat);
    if (!chat) {
      $('#chatName').textContent = 'Офис-чат';
      $('#chatTopic').textContent = 'Выберите чат слева';
      return;
    }
    $('#chatName').textContent = chatTitle(chat);
    $('#chatTopic').textContent = chatSubtitle(chat);
    const glyph = $('#chatGlyph');
    glyph.textContent = '';
    glyph.append(icon(chat.isDm ? 'at' : 'hash'));
    renderTyping();
  }

  function renderTyping() {
    const chatId = state.activeChat;
    const map = state.typing.get(chatId);
    const node = $('#typing');
    if (!map || !map.size) { node.textContent = ''; return; }
    const now = Date.now();
    const names = [];
    for (const [uid, v] of map) {
      if (v.until < now) { map.delete(uid); continue; }
      names.push(v.name);
    }
    if (!names.length) { node.textContent = ''; return; }
    if (names.length === 1) node.textContent = `${names[0]} печатает…`;
    else if (names.length === 2) node.textContent = `${names[0]} и ${names[1]} печатают…`;
    else node.textContent = `${names[0]} и ещё ${names.length - 1} печатают…`;
  }

  setInterval(() => { if (state.activeChat) renderTyping(); }, 1200);

  function renderMembers() {
    const box = $('#members');
    const chat = state.chats.get(state.activeChat);
    box.textContent = '';
    let users = [...state.users.values()].filter(u => !u.blocked);
    if (chat && chat.isDm) {
      users = users.filter(u => (chat.members || []).includes(u.id));
    }
    users.sort((a, b) => {
      const oa = state.online.has(a.id) ? 1 : 0;
      const ob = state.online.has(b.id) ? 1 : 0;
      if (oa !== ob) return ob - oa;
      return a.name.localeCompare(b.name, 'ru');
    });
    $('#membersTotal').textContent = String(users.length);
    if (!users.length) {
      box.append(el('div', { class: 'side-empty' }, 'Пока никого нет'));
      return;
    }
    for (const u of users) {
      const online = state.online.has(u.id);
      const node = el('button', {
        class: 'member' + (online ? '' : ' is-offline'),
        title: u.id === state.me.id ? 'Это вы' : 'Написать в личные сообщения',
        onclick: () => { if (u.id !== state.me.id) openDM(u.id); },
      },
        avatarEl(u, online, 27),
        el('span', { class: 'm-text' },
          el('strong', {}, u.name + (u.id === state.me.id ? ' (вы)' : '')),
          el('small', {}, online ? 'в сети' : presenceText(u))));
      box.append(node);
    }
  }

  function renderPins() {
    const chatId = state.activeChat;
    const arr = state.pins.get(chatId) || [];
    const box = $('#pins');
    box.textContent = '';
    $('#pinsTotal').textContent = String(arr.length);
    const count = $('#pinsCount');
    count.hidden = arr.length === 0;
    count.textContent = arr.length > 9 ? '9+' : String(arr.length);
    if (!arr.length) {
      box.append(el('div', { class: 'side-empty' }, 'Закреплённых сообщений нет'));
      return;
    }
    for (const m of arr.slice().reverse()) {
      const author = state.users.get(m.userId);
      box.append(el('button', {
        class: 'pin-item',
        onclick: () => { scrollToMessage(m.id); if (window.innerWidth <= 1040) $('#side').classList.remove('is-open'); },
      },
        el('strong', {}, author ? author.name : 'Сообщение'),
        el('p', {}, m.text || (m.files && m.files.length ? '📎 ' + m.files[0].name : '—'))));
    }
  }

  async function loadPins(chatId) {
    try {
      const data = await api(`/api/pins?chat=${encodeURIComponent(chatId)}`);
      state.pins.set(chatId, data.messages || []);
      if (state.activeChat === chatId) renderPins();
    } catch (_) {}
  }

  // ============================================================
  // Открытие чата и лента
  // ============================================================

  async function openChat(id, opts = {}) {
    if (!state.chats.has(id)) return;
    if (state.activeChat && state.activeChat !== id) saveDraft();
    state.activeChat = id;
    localStorage.setItem('oc-chat', id);
    state.replyTo = null;
    state.editing = null;
    renderReplyNote();
    renderEditNote();
    renderChats();
    renderHeader();
    renderMembers();
    renderPins();

    const input = $('#input');
    input.value = state.drafts[id] || '';
    hideMentionPop();
    autoGrow();
    updateSendState();

    if (!state.messages.has(id) || opts.before) {
      try {
        await loadMessages(id, opts);
      } catch (ex) {
        toast(ex.message, 'error');
        return;
      }
    }
    renderFeed({ bottom: !opts.jumpTo });
    if (opts.jumpTo) setTimeout(() => scrollToMessage(opts.jumpTo), 60);
    loadPins(id);
    markRead(true);
    input.focus();
  }

  async function loadMessages(id, opts = {}) {
    const params = new URLSearchParams({ chat: id, limit: '50' });
    if (opts.before) params.set('before', opts.before);
    const data = await api('/api/messages?' + params.toString());
    state.messages.set(id, data.messages || []);
    state.hasMore.set(id, !!data.hasMore);
  }

  function saveDraft() {
    const id = state.activeChat;
    if (!id) return;
    const v = $('#input').value;
    if (v.trim()) state.drafts[id] = v;
    else delete state.drafts[id];
  }

  function isAtBottom() {
    const feed = $('#feed');
    return feed.scrollHeight - feed.scrollTop - feed.clientHeight < 90;
  }

  function renderFeed({ bottom = false, preserve = false } = {}) {
    const feed = $('#feed');
    const chatId = state.activeChat;
    const list = state.messages.get(chatId) || [];
    const prevTop = feed.scrollTop;
    const prevHeight = feed.scrollHeight;
    const wasBottom = bottom || isAtBottom();

    feed.textContent = '';
    $('#emptyChat').hidden = list.length > 0;

    let prevMsg = null;
    for (const m of list) {
      if (!prevMsg || !sameDay(prevMsg.createdAt, m.createdAt)) {
        feed.append(el('div', { class: 'day' }, dayLabel(m.createdAt)));
        prevMsg = null;
      }
      const grouped = prevMsg && prevMsg.userId === m.userId &&
        (new Date(m.createdAt) - new Date(prevMsg.createdAt) < 5 * 60 * 1000) &&
        !m.replyTo && !m.pinned;
      feed.append(renderMessage(m, grouped));
      prevMsg = m;
    }

    if (wasBottom) {
      feed.scrollTop = feed.scrollHeight;
      updateJump();
    } else if (preserve) {
      feed.scrollTop = prevTop + (feed.scrollHeight - prevHeight);
    }
  }

  function renderMessage(m, grouped) {
    const author = state.users.get(m.userId) || { name: 'Неизвестный', color: '#555' };
    const mine = m.userId === state.me.id;
    const node = el('div', {
      class: 'msg' + (grouped ? ' is-grouped' : '') + (m.deleted ? ' is-deleted' : ''),
      'data-id': m.id,
    });

    node.append(el('div', { class: 'msg-avatar' },
      avatarEl(author, state.online.has(author.id), 34)));

    const body = el('div', { class: 'msg-body' });
    const head = el('div', { class: 'msg-head' },
      el('strong', {}, author.name),
      el('time', { datetime: m.createdAt, title: fullFmt.format(new Date(m.createdAt)) }, fmtTime(m.createdAt)));
    if (m.editedAt) head.append(el('span', { class: 'edited' }, 'изменено'));
    if (m.pinned) head.append(el('span', { class: 'msg-pin-mark' }, icon('pin'), 'закреплено'));
    body.append(head);

    if (m.replyTo) {
      const parent = findMessage(m.chatId, m.replyTo);
      const pAuthor = parent ? state.users.get(parent.userId) : null;
      const quote = el('div', {
        class: 'reply-quote',
        onclick: () => { if (parent) scrollToMessage(parent.id); },
      },
        el('strong', {}, pAuthor ? pAuthor.name : 'Сообщение'),
        el('span', {}, parent ? (parent.deleted ? 'сообщение удалено' : (parent.text || '📎 файл')) : 'сообщение не найдено'));
      body.append(quote);
    }

    if (m.deleted) {
      body.append(el('div', { class: 'msg-text' }, 'Сообщение удалено'));
    } else {
      if (m.text) {
        const text = el('div', { class: 'msg-text' });
        text.append(renderText(m.text));
        body.append(text);
      }
      if (m.files && m.files.length) body.append(renderFiles(m.files));
      if (m.reactions && Object.keys(m.reactions).length) body.append(renderReactions(m));
    }

    node.append(body);

    if (!m.deleted) node.append(renderActions(m, mine));
    return node;
  }

  function renderFiles(files) {
    const box = el('div', { class: 'msg-files' });
    for (const f of files) {
      const href = `/files/${encodeURIComponent(f.id)}`;
      if (f.mime && f.mime.startsWith('image/')) {
        const link = el('a', { class: 'file-card is-image', href, target: '_blank', rel: 'noopener', title: f.name });
        const img = el('img', { src: href, alt: f.name, loading: 'lazy' });
        link.append(img);
        box.append(link);
      } else {
        box.append(el('a', { class: 'file-card', href, target: '_blank', rel: 'noopener', download: f.name },
          icon(f.mime && f.mime.startsWith('image/') ? 'image' : 'file'),
          el('span', { class: 'file-meta' },
            el('strong', {}, f.name),
            el('small', {}, fmtSize(f.size)))));
      }
    }
    return box;
  }

  function renderReactions(m) {
    const box = el('div', { class: 'reactions' });
    for (const [emoji, users] of Object.entries(m.reactions)) {
      if (!users.length) continue;
      const mine = users.includes(state.me.id);
      box.append(el('button', {
        class: 'reaction' + (mine ? ' is-mine' : ''),
        title: users.map(id => state.users.get(id)?.name || '?').join(', '),
        onclick: () => toggleReaction(m.id, emoji),
      }, el('span', {}, emoji), el('b', {}, String(users.length))));
    }
    return box;
  }

  function renderActions(m, mine) {
    const box = el('div', { class: 'msg-actions' });
    box.append(el('button', { class: 'icon-btn', title: 'Ответить', 'aria-label': 'Ответить', onclick: () => setReply(m) }, icon('reply')));
    box.append(el('button', {
      class: 'icon-btn', title: 'Реакция', 'aria-label': 'Реакция',
      onclick: (e) => { e.stopPropagation(); showReactionPicker(m); },
    }, icon('smile')));
    box.append(el('button', {
      class: 'icon-btn', title: m.pinned ? 'Открепить' : 'Закрепить', 'aria-label': 'Закрепить',
      onclick: () => togglePin(m),
    }, icon('pin')));
    if (mine || (state.me && state.me.isAdmin)) {
      box.append(el('button', { class: 'icon-btn', title: 'Изменить', 'aria-label': 'Изменить', onclick: () => startEdit(m) }, icon('edit')));
      box.append(el('button', { class: 'icon-btn', title: 'Удалить', 'aria-label': 'Удалить', onclick: () => deleteMessage(m) }, icon('trash')));
    }
    return box;
  }

  function showReactionPicker(m) {
    $$('.reaction-picker').forEach(n => n.remove());
    const target = document.querySelector(`.msg[data-id="${CSS.escape(m.id)}"]`);
    if (!target) return;
    const picker = el('div', { class: 'reaction-picker' });
    for (const emoji of REACTIONS) {
      picker.append(el('button', {
        title: emoji,
        onclick: (e) => { e.stopPropagation(); toggleReaction(m.id, emoji); picker.remove(); },
      }, emoji));
    }
    target.append(picker);
    setTimeout(() => {
      const close = (e) => { if (!picker.contains(e.target)) { picker.remove(); document.removeEventListener('click', close); } };
      document.addEventListener('click', close);
    }, 0);
  }

  function findMessage(chatId, id) {
    const list = state.messages.get(chatId) || [];
    return list.find(m => m.id === id) || null;
  }

  function scrollToMessage(id) {
    const node = document.querySelector(`.msg[data-id="${CSS.escape(id)}"]`);
    if (!node) { toast('Это сообщение в более старой части истории'); return; }
    node.scrollIntoView({ block: 'center', behavior: 'smooth' });
    node.classList.add('is-highlight');
    setTimeout(() => node.classList.remove('is-highlight'), 2200);
  }

  function updateJump() {
    const feed = $('#feed');
    const hidden = feed.scrollHeight - feed.scrollTop - feed.clientHeight;
    const btn = $('#jumpBtn');
    btn.hidden = hidden < 200;
  }

  $('#feed').addEventListener('scroll', () => {
    updateJump();
    if ($('#feed').scrollTop < 120) loadOlder();
  });

  async function loadOlder() {
    const id = state.activeChat;
    if (!id || state.loadingOlder || !state.hasMore.get(id)) return;
    const list = state.messages.get(id) || [];
    if (!list.length) return;
    state.loadingOlder = true;
    const feed = $('#feed');
    const prevHeight = feed.scrollHeight;
    const prevTop = feed.scrollTop;
    try {
      const data = await api(`/api/messages?chat=${encodeURIComponent(id)}&limit=50&before=${encodeURIComponent(list[0].createdAt)}`);
      const older = data.messages || [];
      state.messages.set(id, [...older, ...list]);
      state.hasMore.set(id, !!data.hasMore);
      renderFeed({ preserve: true });
      feed.scrollTop = prevTop + (feed.scrollHeight - prevHeight);
    } catch (_) {
      // тихо
    } finally {
      state.loadingOlder = false;
    }
  }

  $('#jumpBtn').addEventListener('click', () => {
    const feed = $('#feed');
    feed.scrollTop = feed.scrollHeight;
    updateJump();
  });

  // ============================================================
  // Действия с сообщениями
  // ============================================================

  function setReply(m) {
    state.replyTo = m;
    state.editing = null;
    renderEditNote();
    renderReplyNote();
    $('#input').focus();
  }

  function renderReplyNote() {
    const note = $('#replyNote');
    if (!state.replyTo) { note.hidden = true; return; }
    const author = state.users.get(state.replyTo.userId);
    $('#replyWho').textContent = author ? author.name : 'Сообщение';
    $('#replyText').textContent = state.replyTo.text || '📎 файл';
    note.hidden = false;
  }

  function clearReply() {
    state.replyTo = null;
    renderReplyNote();
  }

  $('#replyCancel').addEventListener('click', clearReply);

  function startEdit(m) {
    state.editing = m;
    state.replyTo = null;
    renderReplyNote();
    const input = $('#input');
    input.value = m.text || '';
    autoGrow();
    $('#editText').textContent = cut(m.text || '', 90);
    $('#editNote').hidden = false;
    input.focus();
    input.setSelectionRange(input.value.length, input.value.length);
    updateSendState();
  }

  function renderEditNote() {
    const note = $('#editNote');
    if (!state.editing) { note.hidden = true; return; }
    note.hidden = false;
  }

  function cancelEdit() {
    if (!state.editing) return;
    state.editing = null;
    $('#editNote').hidden = true;
    $('#input').value = state.drafts[state.activeChat] || '';
    autoGrow();
    updateSendState();
  }

  $('#editCancel').addEventListener('click', cancelEdit);

  function cut(s, n) { return s.length > n ? s.slice(0, n) + '…' : s; }

  async function toggleReaction(id, emoji) {
    try {
      const data = await api(`/api/messages/${encodeURIComponent(id)}/react`, { method: 'POST', body: { emoji } });
      onUpdated(data.message);
    } catch (ex) {
      toast(ex.message, 'error');
    }
  }

  async function togglePin(m) {
    try {
      const data = await api(`/api/messages/${encodeURIComponent(m.id)}/pin`, { method: 'POST', body: { pinned: !m.pinned } });
      onUpdated(data.message);
      toast(data.message.pinned ? 'Сообщение закреплено' : 'Сообщение откреплено');
    } catch (ex) {
      toast(ex.message, 'error');
    }
  }

  async function deleteMessage(m) {
    const ok = await confirmDialog({
      title: 'Удалить сообщение?',
      text: 'Сообщение исчезнет из чата у всех. Отменить это действие нельзя.',
      confirm: 'Удалить',
      danger: true,
    });
    if (!ok) return;
    try {
      const data = await api(`/api/messages/${encodeURIComponent(m.id)}`, { method: 'DELETE' });
      onUpdated(data.message);
      toast('Сообщение удалено');
    } catch (ex) {
      toast(ex.message, 'error');
    }
  }

  async function saveEdit() {
    const m = state.editing;
    const text = $('#input').value.trim();
    if (!text) { toast('Текст не может быть пустым', 'error'); return; }
    try {
      const data = await api(`/api/messages/${encodeURIComponent(m.id)}`, { method: 'PATCH', body: { text } });
      onUpdated(data.message);
      state.editing = null;
      $('#editNote').hidden = true;
      $('#input').value = '';
      autoGrow();
      updateSendState();
      toast('Изменено');
    } catch (ex) {
      toast(ex.message, 'error');
    }
  }

  // ============================================================
  // Композер
  // ============================================================

  const input = $('#input');
  const mentionPop = $('#mentionPop');
  let mentionItems = [];
  let mentionSel = 0;

  function currentMentionToken() {
    const pos = input.selectionStart;
    if (pos == null) return null;
    const before = input.value.slice(0, pos);
    const m = before.match(/(^|[\s(])@([a-z0-9._-]{0,24})$/i);
    if (!m) return null;
    return { start: pos - m[2].length - 1, end: pos, query: m[2].toLowerCase() };
  }

  function mentionCandidates(q) {
    const chat = state.chats.get(state.activeChat);
    const out = [];
    if (chat && !chat.isDm && 'all'.startsWith(q)) {
      out.push({ id: 'all', login: 'all', name: 'Все участники', color: '', all: true });
    }
    const users = [...state.users.values()]
      .filter(u => u.id !== (state.me && state.me.id) && !u.blocked)
      .filter(u => u.login.toLowerCase().includes(q) || u.name.toLowerCase().includes(q));
    users.sort((a, b) => {
      const ap = a.login.toLowerCase().startsWith(q) ? 0 : 1;
      const bp = b.login.toLowerCase().startsWith(q) ? 0 : 1;
      if (ap !== bp) return ap - bp;
      const ao = state.online.has(a.id) ? 0 : 1;
      const bo = state.online.has(b.id) ? 0 : 1;
      if (ao !== bo) return ao - bo;
      return a.name.localeCompare(b.name, 'ru');
    });
    return out.concat(users).slice(0, 6);
  }

  function hideMentionPop() {
    mentionPop.hidden = true;
    mentionPop.textContent = '';
    mentionItems = [];
    mentionSel = 0;
  }

  function renderMentionPop() {
    const tok = state.editing ? null : currentMentionToken();
    if (!tok) { hideMentionPop(); return; }
    const items = mentionCandidates(tok.query);
    if (!items.length) { hideMentionPop(); return; }
    mentionItems = items;
    if (mentionSel >= items.length) mentionSel = 0;
    mentionPop.textContent = '';
    items.forEach((u, i) => {
      mentionPop.append(el('button', {
        class: 'mention-item' + (i === mentionSel ? ' is-on' : ''),
        type: 'button',
        onmousedown: (e) => e.preventDefault(),
        onclick: () => pickMention(u),
      },
        u.all ? el('span', { class: 'mention-avatar is-all' }, '@') : avatarEl(u, state.online.has(u.id), 24),
        el('span', { class: 'mention-meta' },
          el('strong', {}, u.all ? 'Все участники' : u.name),
          el('small', {}, '@' + u.login))));
    });
    mentionPop.hidden = false;
  }

  function pickMention(u) {
    const tok = currentMentionToken();
    if (!tok) { hideMentionPop(); return; }
    const insert = '@' + u.login + ' ';
    input.value = input.value.slice(0, tok.start) + insert + input.value.slice(tok.end);
    const caret = tok.start + insert.length;
    input.setSelectionRange(caret, caret);
    hideMentionPop();
    autoGrow();
    updateSendState();
    input.focus();
    if (state.activeChat) state.drafts[state.activeChat] = input.value;
  }

  function autoGrow() {
    input.style.height = 'auto';
    input.style.height = Math.min(180, input.scrollHeight) + 'px';
  }

  function updateSendState() {
    const hasText = input.value.trim().length > 0;
    const uploading = state.pendingFiles.some(f => f.status === 'uploading');
    const hasFiles = state.pendingFiles.some(f => f.status === 'done');
    $('#sendBtn').disabled = (!hasText && !hasFiles) || uploading;
  }

  input.addEventListener('input', () => {
    autoGrow();
    updateSendState();
    renderMentionPop();
    if (state.editing) return;
    const chatId = state.activeChat;
    if (chatId) {
      if (input.value.trim()) state.drafts[chatId] = input.value;
      else delete state.drafts[chatId];
    }
    if (chatId && input.value.trim() && Date.now() - lastTypingSent > 2000) {
      lastTypingSent = Date.now();
      api('/api/typing', { method: 'POST', body: { chatId } }).catch(() => {});
    }
  });

  let lastTypingSent = 0;

  input.addEventListener('keydown', (e) => {
    if (!mentionPop.hidden && mentionItems.length) {
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        mentionSel = (mentionSel + 1) % mentionItems.length;
        renderMentionPop();
        return;
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault();
        mentionSel = (mentionSel - 1 + mentionItems.length) % mentionItems.length;
        renderMentionPop();
        return;
      }
      if (e.key === 'Enter' || e.key === 'Tab') {
        e.preventDefault();
        pickMention(mentionItems[mentionSel]);
        return;
      }
      if (e.key === 'Escape') {
        e.preventDefault();
        hideMentionPop();
        return;
      }
    }
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      sendMessage();
    }
    if (e.key === 'Escape') {
      if (state.editing) { cancelEdit(); e.preventDefault(); }
      else if (state.replyTo) { clearReply(); e.preventDefault(); }
    }
  });

  $('#sendBtn').addEventListener('click', sendMessage);

  input.addEventListener('blur', () => setTimeout(hideMentionPop, 120));
  input.addEventListener('click', renderMentionPop);

  async function sendMessage() {
    if (!state.activeChat) return;
    if (state.editing) { await saveEdit(); return; }
    if (state.pendingFiles.some(f => f.status === 'uploading')) {
      toast('Дождитесь окончания загрузки файлов', 'error');
      return;
    }
    const text = input.value.trim();
    const files = state.pendingFiles.filter(f => f.status === 'done').map(f => f.id);
    if (!text && !files.length) return;

    const chatId = state.activeChat;
    const replyTo = state.replyTo ? state.replyTo.id : '';
    input.value = '';
    autoGrow();
    delete state.drafts[chatId];
    updateSendState();
    clearReply();

    try {
      const data = await api('/api/messages', { method: 'POST', body: { chatId, text, files, replyTo } });
      clearPendingFiles();
      onIncoming(data.message);
      $('#feed').scrollTop = $('#feed').scrollHeight;
    } catch (ex) {
      toast(ex.message, 'error');
      input.value = text;
      autoGrow();
      updateSendState();
    }
  }

  // ============================================================
  // Файлы
  // ============================================================

  $('#attachBtn').addEventListener('click', () => $('#fileInput').click());
  $('#fileInput').addEventListener('change', (e) => {
    addFiles(e.target.files);
    e.target.value = '';
  });

  function addFiles(fileList) {
    const files = [...fileList];
    const free = 10 - state.pendingFiles.length;
    if (free <= 0) { toast('Не больше 10 файлов в одном сообщении', 'error'); return; }
    for (const file of files.slice(0, free)) {
      if (file.size > 50 * 1024 * 1024) {
        toast(`«${file.name}» больше 50 МБ`, 'error');
        continue;
      }
      const item = {
        localId: Math.random().toString(36).slice(2),
        id: null,
        name: file.name,
        size: file.size,
        mime: file.type,
        status: 'uploading',
        progress: 0,
        xhr: null,
      };
      state.pendingFiles.push(item);
      uploadFile(file, item);
    }
    renderPendingFiles();
    updateSendState();
  }

  function uploadFile(file, item) {
    const xhr = new XMLHttpRequest();
    item.xhr = xhr;
    xhr.open('POST', '/api/upload');
    xhr.withCredentials = true;
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) {
        item.progress = e.loaded / e.total;
        renderPendingFiles();
      }
    };
    xhr.onload = () => {
      if (xhr.status === 200) {
        try {
          const a = JSON.parse(xhr.responseText);
          item.id = a.id;
          item.name = a.name;
          item.mime = a.mime;
          item.status = 'done';
        } catch (_) {
          item.status = 'error';
        }
      } else if (xhr.status === 401) {
        showAuth();
        item.status = 'error';
      } else {
        let msg = 'Ошибка загрузки';
        try { msg = JSON.parse(xhr.responseText).error || msg; } catch (_) {}
        item.status = 'error';
        item.error = msg;
      }
      renderPendingFiles();
      updateSendState();
    };
    xhr.onerror = () => {
      item.status = 'error';
      item.error = 'Нет связи';
      renderPendingFiles();
      updateSendState();
    };
    const fd = new FormData();
    fd.append('file', file);
    xhr.send(fd);
  }

  function renderPendingFiles() {
    const box = $('#pendingFiles');
    box.textContent = '';
    box.hidden = state.pendingFiles.length === 0;
    for (const item of state.pendingFiles) {
      const node = el('div', { class: 'pending' + (item.status === 'uploading' ? ' is-uploading' : '') + (item.status === 'error' ? ' is-error' : '') });
      node.append(icon(item.mime && item.mime.startsWith('image/') ? 'image' : 'file'));
      const meta = el('span', { class: 'pending-meta' },
        el('span', { class: 'pending-name', title: item.name }, item.name),
        el('span', { class: 'pending-size' }, item.status === 'uploading'
          ? `загрузка ${Math.round(item.progress * 100)}%`
          : item.status === 'error' ? (item.error || 'ошибка') : fmtSize(item.size)));
      node.append(meta);
      node.append(el('button', {
        class: 'icon-btn', 'aria-label': 'Убрать файл',
        onclick: () => {
          if (item.xhr) { try { item.xhr.abort(); } catch (_) {} }
          state.pendingFiles = state.pendingFiles.filter(f => f !== item);
          renderPendingFiles();
          updateSendState();
        },
      }, icon('x')));
      box.append(node);
    }
  }

  function clearPendingFiles() {
    state.pendingFiles = [];
    renderPendingFiles();
    updateSendState();
  }

  let dragDepth = 0;
  document.addEventListener('dragenter', (e) => {
    if (!e.dataTransfer || ![...e.dataTransfer.types].includes('Files')) return;
    dragDepth++;
    $('#dropHint').hidden = false;
  });
  document.addEventListener('dragleave', () => {
    dragDepth = Math.max(0, dragDepth - 1);
    if (!dragDepth) $('#dropHint').hidden = true;
  });
  document.addEventListener('dragover', (e) => { e.preventDefault(); });
  document.addEventListener('drop', (e) => {
    e.preventDefault();
    dragDepth = 0;
    $('#dropHint').hidden = true;
    if (state.me && e.dataTransfer && e.dataTransfer.files.length) addFiles(e.dataTransfer.files);
  });

  input.addEventListener('paste', (e) => {
    const items = e.clipboardData && e.clipboardData.files;
    if (items && items.length) {
      e.preventDefault();
      addFiles(items);
    }
  });

  // ============================================================
  // Непрочитанные
  // ============================================================

  function markRead(force = false) {
    const id = state.activeChat;
    if (!id) return;
    const list = state.messages.get(id) || [];
    const last = list[list.length - 1];
    const at = last ? last.createdAt : new Date().toISOString();
    if ((state.unread.get(id) || 0) > 0) {
      state.unread.set(id, 0);
      state.mentions.delete(id);
      renderChats();
    }
    const now = Date.now();
    if (!force && state.lastRead.id === id && now - state.lastRead.t < 2000) return;
    state.lastRead = { id, t: now };
    api('/api/read', { method: 'POST', body: { chatId: id, at } }).catch(() => {});
  }

  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible' && state.activeChat) markRead(true);
  });

  window.addEventListener('focus', () => { if (state.activeChat) markRead(true); });

  // ============================================================
  // Профиль
  // ============================================================

  $('#meBox').addEventListener('click', openProfile);

  function avatarEl(user, online, size) {
    const node = el('span', { class: 'avatar' + (online ? ' is-online' : ''), title: user.name || '' });
    node.style.background = user.color || '#6b7280';
    node.style.width = size + 'px';
    node.style.height = size + 'px';
    node.style.fontSize = Math.max(9, Math.round(size * 0.36)) + 'px';
    node.textContent = initials(user.name);
    return node;
  }

  function renderMe() {
    if (!state.me) return;
    const av = $('#meAvatar');
    av.style.background = state.me.color || '#6b7280';
    av.textContent = initials(state.me.name);
    $('#meName').textContent = state.me.name;
    $('#meStatus').textContent = state.me.isAdmin ? 'администратор' : 'в сети';
  }

  function openProfile() {
    const me = state.me;
    const nameInput = el('input', { value: me.name, maxlength: '60' });
    const colorBox = el('div', { class: 'swatches' });
    let chosen = me.color || COLORS[0];
    const renderSwatches = () => {
      colorBox.textContent = '';
      for (const c of COLORS) {
        colorBox.append(el('button', {
          class: 'swatch' + (c === chosen ? ' is-on' : ''),
          style: `background:${c}`,
          'aria-label': 'Цвет ' + c,
          onclick: () => { chosen = c; renderSwatches(); },
        }));
      }
    };
    renderSwatches();

    const oldPass = el('input', { type: 'password', autocomplete: 'current-password', placeholder: 'Текущий пароль' });
    const newPass = el('input', { type: 'password', autocomplete: 'new-password', placeholder: 'Новый пароль (от 6 символов)' });

    const soundSwitch = el('button', {
      class: 'switch' + (state.sound ? ' is-on' : ''),
      'aria-label': 'Звук уведомлений',
      onclick: (e) => {
        state.sound = !state.sound;
        localStorage.setItem('oc-sound', state.sound ? '1' : '0');
        e.currentTarget.classList.toggle('is-on', state.sound);
        if (state.sound) playPing();
      },
    });

    const themeSwitch = el('button', {
      class: 'switch' + (state.theme === 'light' ? ' is-on' : ''),
      'aria-label': 'Светлая тема',
      onclick: (e) => {
        state.theme = state.theme === 'light' ? 'dark' : 'light';
        localStorage.setItem('oc-theme', state.theme);
        applyTheme(state.theme);
        e.currentTarget.classList.toggle('is-on', state.theme === 'light');
      },
    });

    const notifySwitch = el('button', {
      class: 'switch' + (state.notify ? ' is-on' : ''),
      'aria-label': 'Уведомления на рабочем столе',
      onclick: async (e) => {
        if (state.notify) {
          state.notify = false;
          localStorage.setItem('oc-notify', '0');
          e.currentTarget.classList.remove('is-on');
          return;
        }
        if (!('Notification' in window)) {
          toast('Браузер не поддерживает уведомления', 'error');
          return;
        }
        let perm = Notification.permission;
        if (perm === 'default') perm = await Notification.requestPermission();
        if (perm !== 'granted') {
          toast('Браузер запретил уведомления — разрешите их в настройках сайта', 'error');
          return;
        }
        state.notify = true;
        localStorage.setItem('oc-notify', '1');
        e.currentTarget.classList.add('is-on');
        toast('Уведомления включены');
      },
    });

    const body = [
      el('div', { class: 'profile-top' },
        avatarEl(me, true, 54),
        el('div', {},
          el('h3', {}, me.name),
          el('p', {}, '@' + me.login + (me.isAdmin ? ' · администратор' : '')))),
      el('div', { class: 'section-label' }, 'Имя и цвет'),
      el('label', { class: 'field' }, el('span', {}, 'Отображаемое имя'), nameInput),
      colorBox,
      el('div', { class: 'section-label' }, 'Пароль'),
      oldPass,
      newPass,
      el('button', {
        class: 'btn', onclick: async () => {
          try {
            await api('/api/me/password', { method: 'POST', body: { old: oldPass.value, new: newPass.value } });
            oldPass.value = ''; newPass.value = '';
            toast('Пароль изменён');
          } catch (ex) { toast(ex.message, 'error'); }
        },
      }, 'Изменить пароль'),
      el('div', { class: 'section-label' }, 'Настройки'),
      el('div', { class: 'toggle-row' }, el('span', {}, 'Звук новых сообщений'), soundSwitch),
      el('div', { class: 'toggle-row' }, el('span', {}, 'Светлая тема'), themeSwitch),
      el('div', { class: 'toggle-row' }, el('span', {}, 'Уведомления на рабочем столе'), notifySwitch),
      el('p', { class: 'form-hint' }, 'Уведомления показываются, когда вкладка свёрнута или окно не в фокусе.'),
    ];

    if (me.isAdmin) {
      body.push(el('button', { class: 'btn', onclick: () => { modal.close(); openAdmin(); } },
        icon('users'), 'Сотрудники'));
    }

    const footer = [
      el('button', { class: 'btn btn-ghost', onclick: async () => {
        try { await api('/api/logout', { method: 'POST', body: {} }); } catch (_) {}
        modal.close();
        showAuth();
      } }, icon('logout'), 'Выйти'),
      el('button', { class: 'btn btn-primary', onclick: async () => {
        try {
          const data = await api('/api/me', { method: 'PATCH', body: { name: nameInput.value.trim(), color: chosen } });
          state.me = { ...state.me, ...data.user };
          state.users.set(state.me.id, { ...(state.users.get(state.me.id) || {}), ...data.user });
          renderMe();
          renderChats();
          renderMembers();
          toast('Профиль сохранён');
          modal.close();
        } catch (ex) { toast(ex.message, 'error'); }
      } }, 'Сохранить'),
    ];

    const modal = openModal({ title: 'Профиль', body, footer });
  }

  function openAdmin() {
    const list = el('div', { class: 'admin-list' });
    const users = [...state.users.values()].sort((a, b) => a.name.localeCompare(b.name, 'ru'));

    const render = () => {
      list.textContent = '';
      for (const u of users) {
        const online = state.online.has(u.id);
        const actions = el('div', { class: 'a-actions' });
        if (u.id !== state.me.id) {
          actions.append(el('button', {
            class: 'icon-btn', title: 'Сбросить пароль', 'aria-label': 'Сбросить пароль',
            onclick: async () => {
              const ok = await confirmDialog({
                title: 'Сбросить пароль?',
                text: `Для «${u.name}» будет создан новый пароль. Старый перестанет работать, все сеансы завершатся.`,
                confirm: 'Сбросить',
              });
              if (!ok) return;
              try {
                const data = await api(`/api/admin/users/${encodeURIComponent(u.id)}/reset`, { method: 'POST', body: {} });
                showTempPassword(u, data.password);
              } catch (ex) { toast(ex.message, 'error'); }
            },
          }, icon('lock')));
          actions.append(el('button', {
            class: 'icon-btn', title: u.blocked ? 'Разблокировать' : 'Заблокировать', 'aria-label': 'Блокировка',
            onclick: async () => {
              try {
                await api(`/api/admin/users/${encodeURIComponent(u.id)}/block`, { method: 'POST', body: { blocked: !u.blocked } });
                u.blocked = !u.blocked;
                render();
                renderMembers();
                toast(u.blocked ? 'Доступ закрыт' : 'Доступ восстановлен');
              } catch (ex) { toast(ex.message, 'error'); }
            },
          }, icon('logout')));
        }
        list.append(el('div', { class: 'admin-row' },
          avatarEl(u, online, 30),
          el('span', { class: 'a-text' },
            el('strong', {}, u.name + (u.id === state.me.id ? ' (вы)' : '')),
            el('small', {}, '@' + u.login + (u.blocked ? ' · заблокирован' : online ? ' · в сети' : ''))),
          actions));
      }
    };
    render();

    openModal({
      title: 'Сотрудники',
      wide: true,
      body: [
        el('p', { class: 'modal-text' }, 'Новый сотрудник регистрируется сам на странице входа. Здесь можно сбросить пароль или закрыть доступ.'),
        list,
      ],
      footer: [el('button', { class: 'btn btn-primary', onclick: () => modalStack[modalStack.length - 1]() }, 'Закрыть')],
    });
  }

  function showTempPassword(user, password) {
    const code = el('code', {}, password);
    const modal = openModal({
      title: 'Новый пароль',
      body: [
        el('p', { class: 'modal-text' }, `Передайте этот пароль сотруднику «${user.name}». После входа он сможет сменить его в профиле.`),
        el('div', { class: 'temp-pass' }, code),
      ],
      footer: [
        el('button', {
          class: 'btn', onclick: async () => {
            const ok = await copyText(password);
            toast(ok ? 'Пароль скопирован' : 'Не удалось скопировать — выделите вручную', ok ? 'ok' : 'error');
          },
        }, 'Скопировать'),
        el('button', { class: 'btn btn-primary', onclick: () => modal.close() }, 'Готово'),
      ],
    });
  }

  // ============================================================
  // Каналы и личные чаты
  // ============================================================

  function openChannelModal() {
    const name = el('input', { placeholder: 'Например: Склад', maxlength: '40' });
    const topic = el('input', { placeholder: 'Коротко о чём канал (необязательно)', maxlength: '160' });
    const modal = openModal({
      title: 'Новый канал',
      body: [
        el('label', { class: 'field' }, el('span', {}, 'Название'), name),
        el('label', { class: 'field' }, el('span', {}, 'Описание'), topic),
        el('p', { class: 'form-hint' }, 'Канал увидят все сотрудники. Писать в нём сможет любой.'),
      ],
      footer: [
        el('button', { class: 'btn btn-ghost', onclick: () => modal.close() }, 'Отмена'),
        el('button', { class: 'btn btn-primary', onclick: async () => {
          try {
            const data = await api('/api/chats', { method: 'POST', body: { name: name.value.trim(), topic: topic.value.trim() } });
            const chat = { ...data.chat, unread: 0, lastMessage: null };
            state.chats.set(chat.id, chat);
            renderChats();
            modal.close();
            await openChat(chat.id);
            toast('Канал создан');
          } catch (ex) { toast(ex.message, 'error'); }
        } }, 'Создать'),
      ],
    });
  }

  function openNewDmModal() {
    const search = el('input', { placeholder: 'Найти сотрудника', autocomplete: 'off' });
    const list = el('div', { class: 'admin-list' });
    const users = [...state.users.values()].filter(u => u.id !== state.me.id && !u.blocked)
      .sort((a, b) => a.name.localeCompare(b.name, 'ru'));

    const render = () => {
      const q = search.value.trim().toLowerCase();
      list.textContent = '';
      const filtered = users.filter(u => !q || u.name.toLowerCase().includes(q) || u.login.includes(q));
      if (!filtered.length) {
        list.append(el('div', { class: 'side-empty' }, 'Никого не нашли'));
        return;
      }
      for (const u of filtered) {
        list.append(el('button', {
          class: 'member', onclick: () => { modal.close(); openDM(u.id); },
        },
          avatarEl(u, state.online.has(u.id), 30),
          el('span', { class: 'm-text' },
            el('strong', {}, u.name),
            el('small', {}, state.online.has(u.id) ? 'в сети' : presenceText(u)))));
      }
    };
    search.addEventListener('input', render);
    render();

    const modal = openModal({
      title: 'Новый разговор',
      body: [search, list],
    });
    setTimeout(() => search.focus(), 40);
  }

  async function openDM(userId) {
    try {
      const data = await api('/api/dm', { method: 'POST', body: { userId } });
      const chat = { ...data.chat, unread: 0, lastMessage: null };
      if (!state.chats.has(chat.id)) state.chats.set(chat.id, chat);
      renderChats();
      await openChat(chat.id);
    } catch (ex) {
      toast(ex.message, 'error');
    }
  }

  // ============================================================
  // Поиск
  // ============================================================

  function toggleSearch(force) {
    const panel = $('#searchPanel');
    const open = force !== undefined ? force : panel.hidden;
    panel.hidden = !open;
    $('#searchResults').hidden = !open || !$('#searchResults').children.length;
    if (open) {
      setTimeout(() => $('#searchInput').focus(), 30);
    } else {
      $('#searchInput').value = '';
      $('#searchResults').textContent = '';
      $('#searchResults').hidden = true;
    }
  }

  $('#searchToggle').addEventListener('click', () => toggleSearch());
  $('#searchClose').addEventListener('click', () => toggleSearch(false));

  $('#readAllBtn').addEventListener('click', async () => {
    try {
      await api('/api/read/all', { method: 'POST', body: {} });
      for (const id of state.unread.keys()) state.unread.set(id, 0);
      state.mentions.clear();
      renderChats();
      toast('Все чаты отмечены прочитанными');
    } catch (ex) {
      toast(ex.message, 'error');
    }
  });

  $('#searchInput').addEventListener('input', debounce(async (e) => {
    const q = e.target.value.trim();
    const box = $('#searchResults');
    if (q.length < 2) {
      box.textContent = '';
      box.hidden = true;
      return;
    }
    try {
      const data = await api(`/api/search?q=${encodeURIComponent(q)}`);
      box.textContent = '';
      const msgs = data.messages || [];
      if (!msgs.length) {
        box.append(el('div', { class: 'sr-empty' }, 'Ничего не найдено'));
      } else {
        for (const m of msgs.slice(0, 30)) {
          const chat = state.chats.get(m.chatId);
          const author = state.users.get(m.userId);
          const title = chat ? (chat.isDm ? 'Личные' : chatTitle(chat)) : 'Чат';
          box.append(el('button', {
            class: 'sr-item',
            onclick: () => jumpToSearchResult(m),
          },
            el('span', { class: 'sr-top' },
              el('span', {}, `${title} · ${author ? author.name : '?'}`),
              el('span', {}, fullFmt.format(new Date(m.createdAt)))),
            el('span', { class: 'sr-text' }, highlight(m.text || '📎 файл', q))));
        }
      }
      box.hidden = false;
    } catch (ex) {
      toast(ex.message, 'error');
    }
  }, 300));

  async function jumpToSearchResult(m) {
    toggleSearch(false);
    if (!state.chats.has(m.chatId)) return;
    const before = new Date(new Date(m.createdAt).getTime() + 1).toISOString();
    await openChat(m.chatId, { before, jumpTo: m.id });
  }

  // ============================================================
  // Разное
  // ============================================================

  $('#chatFilter').addEventListener('input', (e) => {
    state.filter = e.target.value;
    renderChats();
  });

  $('#sideToggle').addEventListener('click', () => {
    const side = $('#side');
    if (window.innerWidth <= 1040) side.classList.toggle('is-open');
    else side.scrollIntoView({ behavior: 'smooth', block: 'start' });
  });

  $('#pinsToggle').addEventListener('click', () => {
    const side = $('#side');
    if (window.innerWidth <= 1040) side.classList.add('is-open');
    else side.scrollTop = 0;
  });

  function applyTheme(theme) {
    document.documentElement.dataset.theme = theme;
    const meta = document.querySelector('meta[name="theme-color"]');
    if (meta) meta.setAttribute('content', theme === 'light' ? '#efece3' : '#101417');
  }

  document.addEventListener('click', initAudio, { once: true });

  // ============================================================
  // Старт
  // ============================================================

  $('#tabLogin').addEventListener('click', () => switchTab(true));
  $('#tabRegister').addEventListener('click', () => switchTab(false));
  $('#loginForm').addEventListener('submit', submitLogin);
  $('#registerForm').addEventListener('submit', submitRegister);

  window.addEventListener('beforeunload', saveDraft);

  boot();
})();
