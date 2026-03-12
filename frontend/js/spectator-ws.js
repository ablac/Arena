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
   */
  constructor(url, onState, onStatus) {
    this.url = url;
    this.onState = onState;
    this.onStatus = onStatus;
    /** @type {WebSocket|null} */
    this.ws = null;
    this.reconnectDelay = 1000;
    this.maxReconnectDelay = 30000;
    this.shouldConnect = false;
  }

  /** Start connecting to the spectator stream. */
  connect() {
    this.shouldConnect = true;
    this._doConnect();
  }

  /** Disconnect and stop reconnecting. */
  disconnect() {
    this.shouldConnect = false;
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
    };

    this.ws.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        this.onState(data);
      } catch (err) {
        console.error('[SpectatorWS] Parse error:', err);
      }
    };

    this.ws.onclose = (event) => {
      console.log('[SpectatorWS] Disconnected:', event.code);
      this.onStatus('disconnected');
      this._scheduleReconnect();
    };

    this.ws.onerror = (err) => {
      console.error('[SpectatorWS] Error:', err);
      this.onStatus('error');
    };
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
