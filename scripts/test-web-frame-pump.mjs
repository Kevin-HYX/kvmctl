import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

class FakeClassList {
  add() {}
  remove() {}
  toggle() {}
}

class FakeElement {
  constructor() {
    this.classList = new FakeClassList();
    this.dataset = {};
    this.listeners = new Map();
    this.textContent = '';
  }
  addEventListener(type, listener) { this.listeners.set(type, listener); }
  getContext() { return { drawImage: bitmap => drawn.push(bitmap.id) }; }
  getBoundingClientRect() { return { left: 0, top: 0, width: 1920, height: 1080 }; }
  focus() {}
  requestFullscreen() { this.fullscreenRequested = true; return Promise.resolve(); }
  requestPointerLock() {}
  setAttribute() {}
}

const drawn = [];
const elements = new Map([
  ['#screen', new FakeElement()],
  ['#control-surface', new FakeElement()],
  ['#stage', new FakeElement()],
  ['#viewport', new FakeElement()],
  ['#message', new FakeElement()],
  ['#connection', new FakeElement()],
  ['#video', new FakeElement()],
  ['#special-keys-toggle', new FakeElement()],
  ['#special-keys', new FakeElement()],
  ['#pointer-mode', new FakeElement()],
  ['#fullscreen', new FakeElement()],
  ['#release-all', new FakeElement()],
  ['#transport-direct', new FakeElement()],
  ['#transport-relayed', new FakeElement()],
  ['#connection-rtt', new FakeElement()],
  ['#connection-fps', new FakeElement()],
  ['#connection-kind', new FakeElement()],
  ['#connection-protocol', new FakeElement()],
]);
const documentListeners = new Map();
const windowListeners = new Map();
const sockets = [];
const decodes = [];

class FakeWebSocket {
  static OPEN = 1;
  constructor() {
    this.readyState = FakeWebSocket.OPEN;
    this.listeners = new Map();
    this.sent = [];
    sockets.push(this);
  }
  addEventListener(type, listener) { this.listeners.set(type, listener); }
  send(message) { this.sent.push(JSON.parse(message)); }
}

const context = vm.createContext({
  Blob,
  JSON,
  Math,
  Map,
  Promise,
  Set,
  WebSocket: FakeWebSocket,
  clearTimeout,
  clearInterval,
  console,
  location: { protocol: 'https:', host: 'example.test', origin: 'https://example.test' },
  setTimeout,
  setInterval,
  fetch: async () => ({ ok: true, json: async () => ({ enabled: false, stun_urls: [], connect_timeout_ms: 10000 }) }),
  createImageBitmap(blob) {
    return new Promise(resolve => decodes.push({ id: blob.id, resolve }));
  },
  document: {
    hidden: false,
    pointerLockElement: null,
    querySelector(selector) { return elements.get(selector); },
    querySelectorAll() { return []; },
    addEventListener(type, listener) { documentListeners.set(type, listener); },
    exitPointerLock() {},
  },
  window: {
    addEventListener(type, listener) { windowListeners.set(type, listener); },
  },
});

const source = fs.readFileSync(new URL('../internal/webcontrol/web/app.js', import.meta.url), 'utf8');
vm.runInContext(source, context);
await new Promise(resolve => setTimeout(resolve, 0));
const socket = sockets[0];
assert.ok(socket, '控制页面应创建 WebSocket');

for (const id of [1, 2, 3]) {
  const blob = new Blob([String(id)]);
  blob.id = id;
  socket.listeners.get('message')({ data: blob });
}

assert.deepEqual(decodes.map(item => item.id), [1], '同一时间只能解码一帧');
decodes[0].resolve({ id: 1, close() {} });
await new Promise(resolve => setTimeout(resolve, 0));
assert.deepEqual(decodes.map(item => item.id), [1, 3], '解码完成后必须跳过旧帧并处理最新帧');
decodes[1].resolve({ id: 3, close() {} });
await new Promise(resolve => setTimeout(resolve, 0));
assert.deepEqual(drawn, [1, 3]);
assert.equal(socket.sent.filter(message => message.type === 'frame_ack').length, 2, '每个完成处理的帧都必须确认');

console.log('web frame pump: latest-only decode and acknowledgements verified');
process.exit(0);
