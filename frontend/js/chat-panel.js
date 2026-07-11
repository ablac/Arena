'use strict';

/**
 * Developer lobby chat panel.
 * Connects to /ws/chat, renders the shared lobby for bot developers, and
 * lets signed-in customers post. Self-initializing: hides itself when the
 * server reports chat disabled.
 * @module chat-panel
 */

import { apiPath, wsURL } from './paths.js?v=20260710a';

const MAX_RENDERED_MESSAGES = 200;
const OVERLAY_ID = 'chat-overlay';

/** Mirror of SpectatorSocket's reconnect discipline for the chat stream. */
class ChatSocket {
  constructor(url, onMessage, onStatus) {
    this.url = url;
    this.onMessage = onMessage;
    this.onStatus = onStatus;
    this.ws = null;
    this.reconnectDelay = 1000;
    this.maxReconnectDelay = 30000;
    this.shouldConnect = false;
    this._pingInterval = null;
    this._staleTimer = null;
    this._staleTimeout = 45000;
    this._reconnectTimer = null;
    this._backoffResetTimer = null;
  }

  connect() {
    this.shouldConnect = true;
    this._doConnect();
  }

  disconnect() {
    this.shouldConnect = false;
    this._stopPing();
    this._clearTimer('_staleTimer');
    this._clearTimer('_reconnectTimer');
    this._clearTimer('_backoffResetTimer');
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
  }

  _clearTimer(name) {
    if (this[name] !== null) {
      clearTimeout(this[name]);
      this[name] = null;
    }
  }

  _doConnect() {
    if (!this.shouldConnect) return;
    this._clearTimer('_reconnectTimer');
    try {
      this.ws = new WebSocket(this.url);
    } catch (err) {
      this._scheduleReconnect();
      return;
    }

    this.ws.onopen = () => {
      this.onStatus('connected');
      this._startPing();
      this._resetStaleTimer();
      // Reset backoff only after the connection survives a beat, so an
      // accept-then-drop server does not loop at 1s.
      this._clearTimer('_backoffResetTimer');
      this._backoffResetTimer = setTimeout(() => {
        this.reconnectDelay = 1000;
      }, 5000);
    };

    this.ws.onmessage = (event) => {
      this._resetStaleTimer();
      let msg;
      try {
        msg = JSON.parse(event.data);
      } catch (err) {
        return;
      }
      if (msg.type === 'heartbeat') return;
      this.onMessage(msg);
    };

    this.ws.onclose = () => {
      this._stopPing();
      this._clearTimer('_staleTimer');
      this._clearTimer('_backoffResetTimer');
      this.ws = null;
      if (this.shouldConnect) {
        this.onStatus('reconnecting');
        this._scheduleReconnect();
      }
    };

    this.ws.onerror = () => {
      this.onStatus('error');
    };
  }

  _scheduleReconnect() {
    if (!this.shouldConnect || this._reconnectTimer !== null) return;
    const delay = this.reconnectDelay / 2 + Math.random() * (this.reconnectDelay / 2);
    this.reconnectDelay = Math.min(this.reconnectDelay * 2, this.maxReconnectDelay);
    this._reconnectTimer = setTimeout(() => {
      this._reconnectTimer = null;
      this._doConnect();
    }, delay);
  }

  _startPing() {
    this._stopPing();
    this._pingInterval = setInterval(() => {
      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        this.ws.send('ping');
      }
    }, 15000);
  }

  _stopPing() {
    if (this._pingInterval !== null) {
      clearInterval(this._pingInterval);
      this._pingInterval = null;
    }
  }

  _resetStaleTimer() {
    this._clearTimer('_staleTimer');
    this._staleTimer = setTimeout(() => {
      if (this.ws) this.ws.close();
    }, this._staleTimeout);
  }

  send(obj) {
    if (this.ws && this.ws.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(obj));
      return true;
    }
    return false;
  }
}

function formatTime(tsMillis) {
  const d = new Date(tsMillis);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

async function fetchChatConfig() {
  try {
    const resp = await fetch(apiPath('/chat/config'), { cache: 'no-store' });
    if (!resp.ok) return null;
    return await resp.json();
  } catch (err) {
    return null;
  }
}

async function fetchAccountSession() {
  try {
    const resp = await fetch(apiPath('/account/session'), {
      credentials: 'same-origin',
      cache: 'no-store',
      headers: { Accept: 'application/json' },
    });
    if (!resp.ok) return null;
    return await resp.json();
  } catch (err) {
    return null;
  }
}

function initChatPanel(cfg) {
  const overlay = document.getElementById(OVERLAY_ID);
  const listEl = document.getElementById('chat-messages');
  const formEl = document.getElementById('chat-form');
  const inputEl = document.getElementById('chat-input');
  const sendBtn = document.getElementById('chat-send');
  const statusEl = document.getElementById('chat-status-line');
  const signinEl = document.getElementById('chat-signin');
  if (!overlay || !listEl || !formEl || !inputEl || !sendBtn || !statusEl || !signinEl) return;

  document.body.classList.add('chat-enabled');
  const bodyLimit = cfg.max_body_len > 0 ? cfg.max_body_len : 280;
  // maxlength counts UTF-16 code units, but the server counts code points, so
  // an emoji-heavy message could be truncated below the real limit. Use a
  // loose 2x backstop here and enforce the true code-point limit on submit.
  inputEl.maxLength = bodyLimit * 2;

  let canPost = false;
  let connected = false;
  let started = false;
  const messageIndex = new Map();

  function setStatus(text, tone) {
    statusEl.textContent = text;
    statusEl.dataset.tone = tone || 'info';
  }

  function updateComposer() {
    const ready = connected && canPost;
    inputEl.disabled = !ready;
    sendBtn.disabled = !ready;
  }

  function nearBottom() {
    return listEl.scrollHeight - listEl.scrollTop - listEl.clientHeight < 60;
  }

  // Server strings only ever land in the DOM through textContent.
  function appendMessage(msg) {
    if (messageIndex.has(msg.id)) return;
    const stick = nearBottom();

    const row = document.createElement('div');
    row.className = 'chat-row';

    const meta = document.createElement('div');
    meta.className = 'chat-row-meta';
    const handleEl = document.createElement('span');
    handleEl.className = 'chat-handle';
    handleEl.textContent = msg.handle || 'dev';
    const timeEl = document.createElement('span');
    timeEl.className = 'chat-time';
    timeEl.textContent = formatTime(msg.ts);
    meta.appendChild(handleEl);
    meta.appendChild(timeEl);

    const bodyEl = document.createElement('div');
    bodyEl.className = 'chat-body';
    bodyEl.textContent = msg.body || '';

    row.appendChild(meta);
    row.appendChild(bodyEl);
    listEl.appendChild(row);
    messageIndex.set(msg.id, row);

    while (listEl.children.length > MAX_RENDERED_MESSAGES) {
      const evicted = listEl.firstElementChild;
      for (const [id, el] of messageIndex) {
        if (el === evicted) {
          messageIndex.delete(id);
          break;
        }
      }
      evicted.remove();
    }
    if (stick) listEl.scrollTop = listEl.scrollHeight;
  }

  function appendNotice(text) {
    const row = document.createElement('div');
    row.className = 'chat-row chat-row-notice';
    row.textContent = text;
    listEl.appendChild(row);
    if (nearBottom()) listEl.scrollTop = listEl.scrollHeight;
  }

  function removeMessage(id) {
    const el = messageIndex.get(id);
    if (el) {
      el.remove();
      messageIndex.delete(id);
    }
  }

  const socket = new ChatSocket(
    wsURL('/chat'),
    (msg) => {
      switch (msg.type) {
        case 'chat_status':
          canPost = !!msg.can_post;
          if (canPost) {
            setStatus('Chatting as ' + (msg.handle || 'dev'), 'ok');
            signinEl.hidden = true;
          } else if (msg.reason === 'sign_in_required') {
            setStatus('Read-only: sign in to post', 'info');
            signinEl.hidden = false;
          } else {
            setStatus('Read-only', 'info');
          }
          updateComposer();
          break;
        case 'chat_history':
          listEl.textContent = '';
          messageIndex.clear();
          (msg.messages || []).forEach(appendMessage);
          listEl.scrollTop = listEl.scrollHeight;
          break;
        case 'chat_message':
          if (msg.message) appendMessage(msg.message);
          break;
        case 'chat_message_hidden':
          removeMessage(msg.id);
          break;
        case 'chat_error':
          if (msg.code === 'BOT_ALIVE_LOCK') {
            appendNotice('Chat is locked while your bot is alive in the round.');
          } else {
            appendNotice(msg.message || 'Message rejected.');
          }
          break;
        default:
          break;
      }
    },
    (state) => {
      connected = state === 'connected';
      if (!connected) {
        setStatus(state === 'reconnecting' ? 'Reconnecting...' : 'Connecting...', 'warn');
      }
      updateComposer();
    },
  );

  formEl.addEventListener('submit', (event) => {
    event.preventDefault();
    const body = inputEl.value.trim();
    if (!body) return;
    // Count code points, matching the server's rune limit.
    if ([...body].length > bodyLimit) {
      appendNotice('Message is too long (max ' + bodyLimit + ' characters).');
      return;
    }
    if (socket.send({ type: 'chat_post', body })) {
      inputEl.value = '';
    }
  });

  // Escape with a draft clears the draft; Escape on an empty input falls
  // through to the shell's close-overlay handler.
  inputEl.addEventListener('keydown', (event) => {
    if (event.key === 'Escape' && inputEl.value !== '') {
      inputEl.value = '';
      event.stopPropagation();
    }
  });

  // Connect lazily on first open; afterwards the socket stays up so the
  // unread state survives the drawer closing.
  const observer = new MutationObserver(() => {
    if (!started && overlay.classList.contains('open')) {
      started = true;
      observer.disconnect();
      setStatus('Connecting...', 'warn');
      socket.connect();
    }
  });
  observer.observe(overlay, { attributes: true, attributeFilter: ['class'] });

  fetchAccountSession().then((session) => {
    const loginURL = (session && session.login_url) || apiPath('/dashboard/login');
    const link = signinEl.querySelector('a');
    if (link) link.href = loginURL;
  });
}

document.addEventListener('DOMContentLoaded', async () => {
  const cfg = await fetchChatConfig();
  if (!cfg || !cfg.enabled) return;
  initChatPanel(cfg);
});
