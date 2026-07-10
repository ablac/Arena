'use strict';

// Small dependency-free SSE frame parser shared by the browser dashboard and
// its Node regression test. Parser state lives across push() calls, so an
// arbitrary network chunk boundary cannot discard half of an event.
(function exposeSSEParser(root, factory) {
  const Parser = factory();
  if (typeof module === 'object' && module.exports) module.exports = Parser;
  root.ArenaSSEFrameParser = Parser;
})(typeof globalThis !== 'undefined' ? globalThis : window, function createSSEParser() {
  return class ArenaSSEFrameParser {
    constructor(onEvent) {
      if (typeof onEvent !== 'function') throw new TypeError('onEvent callback is required');
      this.onEvent = onEvent;
      this.buffer = '';
      this.eventType = '';
      this.dataLines = [];
      this.lastEventId = '';
    }

    push(chunk) {
      if (chunk == null || chunk === '') return;
      this.buffer += String(chunk);
      let newline;
      while ((newline = this.buffer.indexOf('\n')) !== -1) {
        let line = this.buffer.slice(0, newline);
        this.buffer = this.buffer.slice(newline + 1);
        if (line.endsWith('\r')) line = line.slice(0, -1);
        this.processLine(line);
      }
    }

    end() {
      if (this.buffer !== '') {
        let line = this.buffer;
        this.buffer = '';
        if (line.endsWith('\r')) line = line.slice(0, -1);
        this.processLine(line);
      }
    }

    processLine(line) {
      if (line === '') {
        this.dispatch();
        return;
      }
      if (line.startsWith(':')) return;

      const colon = line.indexOf(':');
      const field = colon === -1 ? line : line.slice(0, colon);
      let value = colon === -1 ? '' : line.slice(colon + 1);
      if (value.startsWith(' ')) value = value.slice(1);

      if (field === 'event') this.eventType = value;
      else if (field === 'data') this.dataLines.push(value);
      else if (field === 'id' && !value.includes('\0')) this.lastEventId = value;
    }

    dispatch() {
      if (this.dataLines.length > 0) {
        this.onEvent({
          type: this.eventType || 'message',
          data: this.dataLines.join('\n'),
          id: this.lastEventId,
        });
      }
      this.eventType = '';
      this.dataLines = [];
    }
  };
});
