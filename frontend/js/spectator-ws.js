'use strict';

/**
 * WebSocket client for spectator stream.
 * Connects to the arena and feeds state updates to the renderer.
 * @module spectator-ws
 */

export class SpectatorSocket {
  /**
   * @param {string} url - WebSocket URL (ws:// or wss://)
   * @param {Function} onState - Callback receiving arena state objects
   * @param {Function} onStatus - Callback receiving connection status string
   * @param {Function} [onControl] - Callback receiving non-render control data
   */
  constructor(url, onState, onStatus, onControl = () => {}) {
    this.url = url;
    this.onState = onState;
    this.onStatus = onStatus;
    this.onControl = onControl;
    /** @type {WebSocket|null} */
    this.ws = null;
    this.reconnectDelay = 1000;
    this.maxReconnectDelay = 30000;
    this.shouldConnect = false;
    /** @type {number|null} */
    this._pingInterval = null;
    /** @type {number|null} */
    this._staleTimer = null;
    /** Milliseconds of application-message silence before forcing reconnect. */
    this._staleTimeout = 45000;
  }

  /** Start connecting to the spectator stream. */
  connect() {
    this.shouldConnect = true;
    this._doConnect();
  }

  /** Disconnect and stop reconnecting. */
  disconnect() {
    this.shouldConnect = false;
    this._stopPing();
    this._clearStaleTimer();
    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
  }

  /** @private */
  _doConnect() {
    if (!this.shouldConnect) return;
    this.onStatus('connecting');
    try {
      this.ws = new WebSocket(this.url);
    } catch (err) {
      console.error('[SpectatorWS] Connection error:', err);
      this._scheduleReconnect();
      return;
    }

    this.ws.onopen = () => {
      console.log('[SpectatorWS] Connected');
      this.onStatus('connected');
      this.reconnectDelay = 1000;
      this._startPing();
      this._resetStaleTimer();
    };

    this.ws.onmessage = (event) => {
      this._resetStaleTimer();
      try {
        const data = JSON.parse(event.data);
        // WebSocket ping frames are not exposed to browser JavaScript. The
        // server therefore sends text heartbeats while gameplay is paused so
        // a healthy quiet stream does not trigger this client's stale timer.
        if (data.type === 'heartbeat') return;
        if (data.type === 'service_status') {
          this.onControl(data);
          return;
        }
        this.onState(data);
      } catch (err) {
        console.error('[SpectatorWS] Parse error:', err);
      }
    };

    this.ws.onclose = (event) => {
      console.log('[SpectatorWS] Disconnected:', event.code);
      this._stopPing();
      this._clearStaleTimer();
      this.onStatus('disconnected');
      this._scheduleReconnect();
    };

    this.ws.onerror = (err) => {
      console.error('[SpectatorWS] Error:', err);
      this.onStatus('error');
    };
  }

  /** @private Reset the stale-connection timer. Forces reconnect if no messages arrive. */
  _resetStaleTimer() {
    this._clearStaleTimer();
    this._staleTimer = setTimeout(() => {
      console.warn('[SpectatorWS] No messages received, forcing reconnect');
      if (this.ws) {
        this.ws.close();
        this.ws = null;
      }
    }, this._staleTimeout);
  }

  /** @private */
  _clearStaleTimer() {
    if (this._staleTimer) {
      clearTimeout(this._staleTimer);
      this._staleTimer = null;
    }
  }

  /** @private Send periodic pings to keep the connection alive. */
  _startPing() {
    this._stopPing();
    this._pingInterval = setInterval(() => {
      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        try { this.ws.send('ping'); } catch { /* reconnect will handle it */ }
      }
    }, 15000);
  }

  /** @private */
  _stopPing() {
    if (this._pingInterval) {
      clearInterval(this._pingInterval);
      this._pingInterval = null;
    }
  }

  /** @private Exponential backoff reconnect. */
  _scheduleReconnect() {
    if (!this.shouldConnect) return;
    const delay = this.reconnectDelay;
    this.reconnectDelay = Math.min(this.reconnectDelay * 2, this.maxReconnectDelay);
    console.log(`[SpectatorWS] Reconnecting in ${delay}ms`);
    this.onStatus('reconnecting');
    setTimeout(() => this._doConnect(), delay);
  }
}
