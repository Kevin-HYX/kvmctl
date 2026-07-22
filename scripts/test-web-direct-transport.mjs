import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';

class FakeClassList { add() {} remove() {} toggle() {} }
class FakeElement {
  constructor() {
    this.classList = new FakeClassList();
    this.dataset = {};
    this.listeners = new Map();
    this.textContent = '';
    this.width = 1920;
    this.height = 1080;
    this.rect = { left: 0, top: 0, width: 1920, height: 1080 };
  }
  addEventListener(type, listener) { this.listeners.set(type, listener); }
  getContext() { return { drawImage: bitmap => drawn.push(bitmap.id) }; }
  getBoundingClientRect() { return this.rect; }
  focus() {}
  requestFullscreen() { this.fullscreenRequested = true; return Promise.resolve(); }
  requestPointerLock() {}
  setAttribute() {}
}
class FakeDataChannel {
  constructor(label) { this.label = label; this.readyState = 'connecting'; this.listeners = new Map(); this.sent = []; this.binaryType = ''; }
  addEventListener(type, listener) { this.listeners.set(type, listener); }
  send(value) { this.sent.push(typeof value === 'string' ? JSON.parse(value) : value); }
  open() { this.readyState = 'open'; this.listeners.get('open')?.(); }
  close() { this.readyState = 'closed'; this.listeners.get('close')?.(); }
}

const peers = [];
class FakeRTCPeerConnection {
  constructor(configuration) { this.configuration = configuration; this.channels = []; this.listeners = new Map(); this.iceGatheringState = 'complete'; this.connectionState = 'new'; peers.push(this); }
  createDataChannel(label, options) { const channel = new FakeDataChannel(label); channel.options = options; this.channels.push(channel); return channel; }
  async createOffer() { return { type: 'offer', sdp: 'test-offer' }; }
  async setLocalDescription(description) { this.localDescription = description; }
  async setRemoteDescription(description) { this.remoteDescription = description; }
  addEventListener(type, listener) { this.listeners.set(type, listener); }
  getStats() { return Promise.resolve(new Map()); }
  close() { this.connectionState = 'closed'; }
}

const drawn = [];
const decodes = [];
const frameChunkMessageSize = 16 * 1024;
const frameChunkHeaderSize = 16;
const frameChunkPayloadSize = frameChunkMessageSize - frameChunkHeaderSize;
function jpegBytes(marker, size = 5) {
  const bytes = new Uint8Array(size);
  bytes[0] = 0xff; bytes[1] = 0xd8; bytes[2] = marker;
  bytes[size - 2] = 0xff; bytes[size - 1] = 0xd9;
  return bytes;
}
function framePackets(frameID, bytes) {
  const count = Math.ceil(bytes.byteLength / frameChunkPayloadSize);
  return Array.from({ length: count }, (_, index) => {
    const start = index * frameChunkPayloadSize;
    const end = Math.min(start + frameChunkPayloadSize, bytes.byteLength);
    const packet = new Uint8Array(frameChunkHeaderSize + end - start);
    packet.set([0x4b, 0x56, 0x4d, 0x31]);
    const view = new DataView(packet.buffer);
    view.setUint32(4, frameID);
    view.setUint16(8, index);
    view.setUint16(10, count);
    view.setUint32(12, bytes.byteLength);
    packet.set(bytes.subarray(start, end), frameChunkHeaderSize);
    return packet.buffer;
  });
}
async function decodedMarker(index) {
  const bytes = new Uint8Array(await decodes[index].blob.arrayBuffer());
  return bytes[2];
}
const elements = new Map();
for (const selector of ['#screen','#control-surface','#stage','#viewport','#message','#connection','#video','#special-keys-toggle','#special-keys','#pointer-mode','#fullscreen','#release-all','#transport-direct','#transport-relayed','#connection-rtt','#connection-fps','#connection-kind','#connection-protocol']) {
  elements.set(selector, new FakeElement());
}
elements.get('#special-keys').hidden = true;
const documentListeners = new Map();
const windowListeners = new Map();
const sockets = [];
class FakeWebSocket { static OPEN = 1; constructor() { this.readyState = 1; this.listeners = new Map(); this.sent = []; sockets.push(this); } addEventListener(t, l) { this.listeners.set(t, l); } send(v) { this.sent.push(JSON.parse(v)); } close() {} }

const context = vm.createContext({
  Blob, JSON, Math, Map, Promise, Set, ArrayBuffer,
  RTCPeerConnection: FakeRTCPeerConnection,
  WebSocket: FakeWebSocket,
  clearTimeout, clearInterval, setTimeout, setInterval,
  console,
  location: { protocol: 'https:', host: 'example.test', origin: 'https://example.test' },
  fetch: async url => url.endsWith('/config')
    ? { ok: true, json: async () => ({ enabled: true, stun_urls: [], connect_timeout_ms: 10000 }) }
    : { ok: true, json: async () => ({ type: 'answer', sdp: 'test-answer' }) },
  createImageBitmap(blob) { return new Promise(resolve => decodes.push({ blob, resolve })); },
  document: {
    hidden: false, pointerLockElement: null,
    querySelector(selector) { return elements.get(selector); },
    querySelectorAll() { return []; },
    addEventListener(type, listener) { documentListeners.set(type, listener); },
    exitPointerLock() {},
  },
  window: { addEventListener(type, listener) { windowListeners.set(type, listener); } },
});

const source = fs.readFileSync(new URL('../internal/webcontrol/web/app.js', import.meta.url), 'utf8');
vm.runInContext(source, context);
await new Promise(resolve => setTimeout(resolve, 0));

assert.equal(peers.length, 1, '页面默认必须建立 WebRTC 直连');
assert.equal(sockets.length, 0, '页面不得同时建立 WebSocket 转发');
const peer = peers[0];
const frames = peer.channels.find(channel => channel.label === 'frames');
const control = peer.channels.find(channel => channel.label === 'control');
assert.equal(frames.options.ordered, false);
assert.equal(frames.options.maxRetransmits, 0);
assert.ok(control, '必须创建可靠控制 DataChannel');
frames.open(); control.open();

const largeFrame = jpegBytes(1, 300_000);
for (const packet of framePackets(1, largeFrame).reverse()) frames.listeners.get('message')({ data: packet });
await new Promise(resolve => setTimeout(resolve, 0));
assert.equal(decodes.length, 1, '超过 DataChannel 单消息上限的帧必须在重组完成后只解码一次');
assert.equal(decodes[0].blob.size, largeFrame.byteLength, '大帧重组后必须保持完整长度');
assert.equal(await decodedMarker(0), 1, '乱序分片必须按序重组');

for (const id of [2, 3]) {
  for (const packet of framePackets(id, jpegBytes(id))) frames.listeners.get('message')({ data: packet });
}
assert.equal(decodes.length, 1, '同一时间只能解码一帧');
decodes[0].resolve({ id: 1, close() {} });
await new Promise(resolve => setTimeout(resolve, 0));
assert.equal(await decodedMarker(1), 3, '直连画面必须跳过已经完整重组的旧帧并处理最新帧');

const older = framePackets(4, jpegBytes(4, 40_000));
const newer = framePackets(5, jpegBytes(5, 40_000));
for (const packet of older.slice(0, -1)) frames.listeners.get('message')({ data: packet });
frames.listeners.get('message')({ data: newer[0] });
frames.listeners.get('message')({ data: older.at(-1) });
decodes[1].resolve({ id: 3, close() {} });
await new Promise(resolve => setTimeout(resolve, 0));
assert.equal(await decodedMarker(2), 4, '新帧首片先到时，旧帧尾片仍必须能够完成旧帧');
for (const packet of newer.slice(1)) frames.listeners.get('message')({ data: packet });
decodes[2].resolve({ id: 4, close() {} });
await new Promise(resolve => setTimeout(resolve, 0));
assert.equal(await decodedMarker(3), 5, '更新的完整帧必须在旧帧之后继续显示');
frames.listeners.get('message')({ data: older.at(-1) });
await new Promise(resolve => setTimeout(resolve, 0));
assert.equal(decodes.length, 4, '已经完成的旧帧迟到分片不得重新触发解码');

decodes[3].resolve({ id: 5, close() {} });
await new Promise(resolve => setTimeout(resolve, 0));
const sixth = framePackets(6, jpegBytes(6, 40_000));
const seventh = framePackets(7, jpegBytes(7, 40_000));
const eighth = framePackets(8, jpegBytes(8, 40_000));
frames.listeners.get('message')({ data: sixth[0] });
frames.listeners.get('message')({ data: sixth[0] });
frames.listeners.get('message')({ data: seventh[0] });
frames.listeners.get('message')({ data: seventh[0] });
frames.listeners.get('message')({ data: eighth[0] });
for (const packet of sixth.slice(1)) frames.listeners.get('message')({ data: packet });
for (const packet of seventh.slice(1)) frames.listeners.get('message')({ data: packet });
await new Promise(resolve => setTimeout(resolve, 0));
assert.equal(await decodedMarker(4), 7, '第三帧开始后必须淘汰最旧的缺片帧，并允许次新帧完成');
for (const packet of eighth.slice(1)) frames.listeners.get('message')({ data: packet });
decodes[4].resolve({ id: 7, close() {} });
await new Promise(resolve => setTimeout(resolve, 0));
assert.equal(await decodedMarker(5), 8, '重复分片不得阻止最新帧完成');
assert.equal(decodes.length, 6, '被淘汰的第六帧不得触发解码');
decodes[5].resolve({ id: 8, close() {} });
await new Promise(resolve => setTimeout(resolve, 0));

frames.close();
assert.deepEqual(control.sent.at(-1), { type: 'release_all' }, '任一 DataChannel 关闭都必须立即释放输入');
frames.readyState = 'open';

windowListeners.get('keydown')({ code: 'KeyA', preventDefault() {}, repeat: false });
assert.deepEqual(control.sent.at(-1), { type: 'keyboard', keys: ['a'] }, '键盘输入必须走可靠控制 DataChannel');

const screen = elements.get('#screen');
screen.rect = { left: 0, top: 0, width: 2560, height: 1080 };
screen.listeners.get('mousemove')({ clientX: 320, clientY: 540, movementX: 0, movementY: 0 });
assert.deepEqual(control.sent.at(-1), { type: 'pointer_absolute', x: 0, y: 500, buttons: [], wheel: 0 }, '绝对坐标必须排除宽屏模式下的左侧黑边');
screen.listeners.get('mousemove')({ clientX: 2240, clientY: 540, movementX: 0, movementY: 0 });
assert.deepEqual(control.sent.at(-1), { type: 'pointer_absolute', x: 1000, y: 500, buttons: [], wheel: 0 }, '绝对坐标必须排除宽屏模式下的右侧黑边');
screen.rect = { left: 0, top: 0, width: 1920, height: 1200 };
screen.listeners.get('mousemove')({ clientX: 960, clientY: 60, movementX: 0, movementY: 0 });
assert.deepEqual(control.sent.at(-1), { type: 'pointer_absolute', x: 500, y: 0, buttons: [], wheel: 0 }, '绝对坐标必须排除顶部黑边');
screen.listeners.get('mousemove')({ clientX: 960, clientY: 1140, movementX: 0, movementY: 0 });
assert.deepEqual(control.sent.at(-1), { type: 'pointer_absolute', x: 500, y: 1000, buttons: [], wheel: 0 }, '绝对坐标必须排除底部黑边');
elements.get('#fullscreen').listeners.get('click')();
assert.equal(elements.get('#control-surface').fullscreenRequested, true, '全屏必须包含传输、鼠标和特殊按键工具箱');
elements.get('#special-keys-toggle').listeners.get('click')();
assert.equal(elements.get('#special-keys').hidden, false, '特殊按键按钮必须呼出工具栏');

elements.get('#transport-relayed').listeners.get('click')();
assert.deepEqual(control.sent.at(-1), { type: 'release_all' }, '人工切换前必须通过旧控制通道释放输入');
assert.equal(peer.connectionState, 'closed', '人工切换必须关闭旧 WebRTC');
assert.equal(sockets.length, 1, '人工切换后必须只建立 WebSocket 转发通道');

elements.get('#transport-direct').listeners.get('click')();
await new Promise(resolve => setTimeout(resolve, 0));
const wrapPeer = peers[1];
const wrapFrames = wrapPeer.channels.find(channel => channel.label === 'frames');
const wrapControl = wrapPeer.channels.find(channel => channel.label === 'control');
wrapFrames.open(); wrapControl.open();
for (const packet of framePackets(0xffffffff, jpegBytes(9))) wrapFrames.listeners.get('message')({ data: packet });
await new Promise(resolve => setTimeout(resolve, 0));
assert.equal(await decodedMarker(6), 9, '帧号回绕前的最后一帧必须可以完成');
decodes[6].resolve({ id: 9, close() {} });
for (const packet of framePackets(0, jpegBytes(10))) wrapFrames.listeners.get('message')({ data: packet });
await new Promise(resolve => setTimeout(resolve, 0));
assert.equal(await decodedMarker(7), 10, '32 位帧号回绕后必须继续接收新帧');

console.log('web direct transport: WebRTC default, latest frame, reliable control, explicit switch verified');
process.exit(0);
