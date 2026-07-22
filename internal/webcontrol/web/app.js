(() => {
  'use strict';
  const canvas = document.querySelector('#screen');
  const context = canvas.getContext('2d', { alpha: false });
  const controlSurface = document.querySelector('#control-surface');
  const message = document.querySelector('#message');
  const connectionBadge = document.querySelector('#connection');
  const videoBadge = document.querySelector('#video');
  const rttBadge = document.querySelector('#connection-rtt');
  const fpsBadge = document.querySelector('#connection-fps');
  const connectionKind = document.querySelector('#connection-kind');
  const connectionProtocol = document.querySelector('#connection-protocol');
  const directButton = document.querySelector('#transport-direct');
  const relayedButton = document.querySelector('#transport-relayed');
  const specialKeysButton = document.querySelector('#special-keys-toggle');
  const specialKeys = document.querySelector('#special-keys');
  const pointerModeButton = document.querySelector('#pointer-mode');
  const pressedKeys = new Set();
  const pointerButtons = new Set();
  const latchedModifiers = new Set();
  const heldInputRefreshMs = 4000;
  const idleReleaseMs = 4900;
  const reconnectWindowMs = 60000;
  const frameChunkHeaderSize = 16;
  const frameChunkMessageSize = 16 * 1024;
  const frameChunkPayloadSize = frameChunkMessageSize - frameChunkHeaderSize;
  const maxFrameSize = 16 * 1024 * 1024;
  let configuration = { enabled: false, stun_urls: [], connect_timeout_ms: 10000 };
  let selectedTransport = 'direct';
  let pointerMode = 'absolute';
  let lastAbsolutePosition = { x: 500, y: 500 };
  let pendingFrame;
  let decodingFrame = false;
  let socket;
  let peer;
  let framesChannel;
  let controlChannel;
  let connectionGeneration = 0;
  let reconnectDeadline = 0;
  let reconnectTimer;
  let connectTimer;
  let statsTimer;
  let idleTimer;
  let framesSinceSample = 0;
  let lastStatsAt = Date.now();
  let newestFrameID;
  let latestCompletedFrameID;
  const frameAssemblies = new Map();

  const keyMap = new Map([
    ['Escape','escape'], ['Tab','tab'], ['Enter','enter'], ['Backspace','backspace'], ['Space','space'],
    ['Delete','delete'], ['Insert','insert'], ['Home','home'], ['End','end'], ['PageUp','pageup'], ['PageDown','pagedown'],
    ['ArrowUp','up'], ['ArrowDown','down'], ['ArrowLeft','left'], ['ArrowRight','right'],
    ['Minus','minus'], ['Equal','equal'], ['BracketLeft','leftbracket'], ['BracketRight','rightbracket'],
    ['Backslash','backslash'], ['Semicolon','semicolon'], ['Quote','apostrophe'], ['Backquote','grave'],
    ['Comma','comma'], ['Period','period'], ['Slash','slash'], ['CapsLock','capslock'],
    ['PrintScreen','printscreen'], ['ScrollLock','scrolllock'], ['Pause','pause'], ['NumLock','numlock'],
    ['ControlLeft','ctrl'], ['ControlRight','ctrl'], ['ShiftLeft','shift'], ['ShiftRight','shift'],
    ['AltLeft','alt'], ['AltRight','alt'], ['MetaLeft','meta'], ['MetaRight','meta']
  ]);
  for (let i = 1; i <= 12; i += 1) keyMap.set(`F${i}`, `f${i}`);
  for (let i = 0; i <= 9; i += 1) keyMap.set(`Digit${i}`, `${i}`);
  for (let code = 65; code <= 90; code += 1) keyMap.set(`Key${String.fromCharCode(code)}`, String.fromCharCode(code + 32));

  function setBadge(element, text, state) {
    element.textContent = text;
    element.className = `badge ${state}`;
  }

  function activeSender() {
    if (selectedTransport === 'direct' && controlChannel?.readyState === 'open') return controlChannel;
    if (selectedTransport === 'relayed' && socket?.readyState === WebSocket.OPEN) return socket;
    return undefined;
  }

  function send(payload, countsAsInput = true) {
    const sender = activeSender();
    if (!sender) return;
    sender.send(JSON.stringify(payload));
    if (countsAsInput) resetIdleTimer();
  }

  function resetIdleTimer() {
    clearTimeout(idleTimer);
    idleTimer = setTimeout(refreshHeldInputOrRelease, hasHeldInput() ? heldInputRefreshMs : idleReleaseMs);
  }

  function hasHeldInput() { return pressedKeys.size > 0 || pointerButtons.size > 0; }

  function refreshHeldInputOrRelease() {
    if (!hasHeldInput()) { releaseAll(); return; }
    if (pressedKeys.size > 0) send({ type: 'keyboard', keys: [...new Set([...latchedModifiers, ...pressedKeys])] }, false);
    if (pointerButtons.size > 0) {
      if (pointerMode === 'relative') send({ type: 'pointer_relative', dx: 0, dy: 0, buttons: [...pointerButtons] }, false);
      else send({ type: 'pointer_absolute', ...lastAbsolutePosition, buttons: [...pointerButtons] }, false);
    }
    idleTimer = setTimeout(refreshHeldInputOrRelease, heldInputRefreshMs);
  }

  function releaseAll() {
    clearTimeout(idleTimer);
    send({ type: 'release_all' }, false);
    pressedKeys.clear();
    pointerButtons.clear();
    latchedModifiers.clear();
    document.querySelectorAll('[data-modifier].active').forEach(button => button.classList.remove('active'));
  }

  function sendKeyboard() { send({ type: 'keyboard', keys: [...new Set([...latchedModifiers, ...pressedKeys])] }); }
  function buttonName(button) { return button === 0 ? 'left' : button === 1 ? 'middle' : button === 2 ? 'right' : null; }
  function absolutePosition(event) {
    const rect = canvas.getBoundingClientRect();
    const scale = Math.min(rect.width / canvas.width, rect.height / canvas.height);
    const contentWidth = canvas.width * scale;
    const contentHeight = canvas.height * scale;
    const contentLeft = rect.left + (rect.width - contentWidth) / 2;
    const contentTop = rect.top + (rect.height - contentHeight) / 2;
    return {
      x: Math.max(0, Math.min(1000, Math.round((event.clientX - contentLeft) * 1000 / contentWidth))),
      y: Math.max(0, Math.min(1000, Math.round((event.clientY - contentTop) * 1000 / contentHeight)))
    };
  }
  function sendPointer(event, wheel = 0) {
    if (pointerMode === 'relative') {
      send({ type: 'pointer_relative', dx: Math.round(event.movementX || 0), dy: Math.round(event.movementY || 0), buttons: [...pointerButtons], wheel });
      return;
    }
    const position = absolutePosition(event);
    lastAbsolutePosition = position;
    send({ type: 'pointer_absolute', ...position, buttons: [...pointerButtons], wheel });
  }

  async function drawFrame(value) {
    try {
      const blob = value instanceof Blob ? value : new Blob([value], { type: 'image/jpeg' });
      const bitmap = await createImageBitmap(blob);
      context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
      bitmap.close();
      framesSinceSample += 1;
      message.classList.add('hidden');
      videoBadge.textContent = '1080p · 实时';
    } catch (_) {
      videoBadge.textContent = '画面解码失败';
    } finally {
      if (selectedTransport === 'relayed') send({ type: 'frame_ack' }, false);
    }
  }
  async function drawLatestFrames() {
    if (decodingFrame) return;
    decodingFrame = true;
    try {
      while (pendingFrame) {
        const frame = pendingFrame;
        pendingFrame = undefined;
        await drawFrame(frame);
      }
    } finally {
      decodingFrame = false;
      if (pendingFrame) drawLatestFrames();
    }
  }
  function offerLatestFrame(frame) { pendingFrame = frame; drawLatestFrames(); }

  function resetFrameAssembly() {
    newestFrameID = undefined;
    latestCompletedFrameID = undefined;
    frameAssemblies.clear();
  }

  function isNewerFrameID(candidate, current) {
    if (current === undefined) return true;
    const distance = (candidate - current) >>> 0;
    return distance !== 0 && distance < 0x80000000;
  }

  function acceptFrameChunk(value) {
    const packet = value instanceof ArrayBuffer
      ? new Uint8Array(value)
      : ArrayBuffer.isView(value)
        ? new Uint8Array(value.buffer, value.byteOffset, value.byteLength)
        : undefined;
    if (!packet || packet.byteLength < frameChunkHeaderSize || packet.byteLength > frameChunkMessageSize) return;
    if (packet[0] !== 0x4b || packet[1] !== 0x56 || packet[2] !== 0x4d || packet[3] !== 0x31) return;

    const view = new DataView(packet.buffer, packet.byteOffset, packet.byteLength);
    const frameID = view.getUint32(4);
    const chunkIndex = view.getUint16(8);
    const chunkCount = view.getUint16(10);
    const frameSize = view.getUint32(12);
    if (frameSize === 0 || frameSize > maxFrameSize || chunkCount === 0 || chunkIndex >= chunkCount) return;
    if (chunkCount !== Math.ceil(frameSize / frameChunkPayloadSize)) return;
    const expectedPayloadSize = Math.min(frameChunkPayloadSize, frameSize - chunkIndex * frameChunkPayloadSize);
    if (packet.byteLength - frameChunkHeaderSize !== expectedPayloadSize) return;

    if (latestCompletedFrameID !== undefined && !isNewerFrameID(frameID, latestCompletedFrameID)) return;

    let frame = frameAssemblies.get(frameID);
    if (!frame && isNewerFrameID(frameID, newestFrameID)) {
      newestFrameID = frameID;
      frame = { id: frameID, size: frameSize, chunks: new Array(chunkCount), received: 0, receivedBytes: 0 };
      frameAssemblies.set(frameID, frame);
      while (frameAssemblies.size > 2) frameAssemblies.delete(frameAssemblies.keys().next().value);
    } else if (!frame) {
      return;
    }
    if (frame.size !== frameSize || frame.chunks.length !== chunkCount || frame.chunks[chunkIndex]) return;

    const payload = packet.slice(frameChunkHeaderSize);
    frame.chunks[chunkIndex] = payload;
    frame.received += 1;
    frame.receivedBytes += payload.byteLength;
    if (frame.received !== chunkCount) return;
    frameAssemblies.delete(frameID);
    if (frame.receivedBytes !== frame.size) return;

    const jpeg = new Uint8Array(frame.size);
    let offset = 0;
    for (const chunk of frame.chunks) {
      if (!chunk) return;
      jpeg.set(chunk, offset);
      offset += chunk.byteLength;
    }
    latestCompletedFrameID = frameID;
    for (const pendingFrameID of frameAssemblies.keys()) {
      if (!isNewerFrameID(pendingFrameID, frameID)) frameAssemblies.delete(pendingFrameID);
    }
    offerLatestFrame(jpeg.buffer);
  }

  function handleServerUpdate(data) {
    let update;
    try { update = JSON.parse(data); } catch (_) { return; }
    if (update.type === 'video' && update.state === 'ready') videoBadge.textContent = '画面已就绪';
    if (update.type === 'error') {
      message.textContent = `${update.code}: ${update.message}`;
      message.classList.remove('hidden');
    }
  }

  function updateTransportControls() {
    const direct = selectedTransport === 'direct';
    directButton.classList.toggle('active', direct);
    relayedButton.classList.toggle('active', !direct);
    directButton.setAttribute('aria-pressed', String(direct));
    relayedButton.setAttribute('aria-pressed', String(!direct));
    connectionKind.textContent = direct ? '直连' : '转发';
    connectionProtocol.textContent = direct ? 'UDP' : 'WebSocket';
  }

  function closeCurrentTransport() {
    clearTimeout(connectTimer);
    clearTimeout(reconnectTimer);
    clearInterval(statsTimer);
    const oldPeer = peer;
    const oldSocket = socket;
    peer = undefined; framesChannel = undefined; controlChannel = undefined; socket = undefined;
    resetFrameAssembly();
    oldPeer?.close();
    oldSocket?.close();
  }

  function showConnecting() {
    setBadge(connectionBadge, selectedTransport === 'direct' ? '正在直连' : '正在转发', 'waiting');
    message.textContent = selectedTransport === 'direct' ? '正在建立 UDP 直连通道……' : '正在建立 WebSocket 转发通道……';
    message.classList.remove('hidden');
    rttBadge.textContent = '— ms';
    fpsBadge.textContent = '— FPS';
  }

  function scheduleReconnect(generation, reason) {
    if (generation !== connectionGeneration) return;
    clearTimeout(connectTimer);
    clearTimeout(reconnectTimer);
    if (reconnectDeadline === 0) reconnectDeadline = Date.now() + reconnectWindowMs;
    releaseAll();
    setBadge(connectionBadge, '连接失败', 'error');
    message.textContent = reason;
    message.classList.remove('hidden');
    if (Date.now() >= reconnectDeadline) return;
    reconnectTimer = setTimeout(() => {
      if (generation !== connectionGeneration) return;
      if (selectedTransport === 'direct') connectDirect(generation);
      else connectRelayed(generation);
    }, 2000);
  }

  function markConnected(generation) {
    if (generation !== connectionGeneration) return;
    clearTimeout(connectTimer);
    clearTimeout(reconnectTimer);
    reconnectTimer = undefined;
    reconnectDeadline = 0;
    setBadge(connectionBadge, '已连接', 'ready');
    message.classList.add('hidden');
    lastStatsAt = Date.now(); framesSinceSample = 0;
    clearInterval(statsTimer);
    statsTimer = setInterval(() => updateStats(generation), 1000);
  }

  async function waitForICEGathering(target, timeoutMs) {
    if (target.iceGatheringState === 'complete') return;
    await new Promise((resolve, reject) => {
      const timeout = setTimeout(() => reject(new Error('ICE 候选收集超时')), timeoutMs);
      target.addEventListener('icegatheringstatechange', () => {
        if (target.iceGatheringState === 'complete') { clearTimeout(timeout); resolve(); }
      });
    });
  }

  async function connectDirect(generation) {
    if (generation !== connectionGeneration) return;
    closeCurrentTransport();
    showConnecting();
    let framesOpen = false, controlOpen = false;
    const currentPeer = new RTCPeerConnection({ iceServers: configuration.stun_urls.map(url => ({ urls: url })) });
    peer = currentPeer;
    framesChannel = currentPeer.createDataChannel('frames', { ordered: false, maxRetransmits: 0 });
    controlChannel = currentPeer.createDataChannel('control');
    framesChannel.binaryType = 'arraybuffer';
    const ready = () => { if (framesOpen && controlOpen) markConnected(generation); };
    framesChannel.addEventListener('open', () => { framesOpen = true; ready(); });
    framesChannel.addEventListener('message', event => acceptFrameChunk(event.data));
    framesChannel.addEventListener('close', () => scheduleReconnect(generation, 'UDP 画面通道已中断，正在重试。'));
    framesChannel.addEventListener('error', () => scheduleReconnect(generation, 'UDP 画面通道发生错误，正在重试。'));
    controlChannel.addEventListener('open', () => { controlOpen = true; ready(); });
    controlChannel.addEventListener('message', event => handleServerUpdate(event.data));
    controlChannel.addEventListener('close', () => scheduleReconnect(generation, 'UDP 控制通道已中断，正在重试。'));
    controlChannel.addEventListener('error', () => scheduleReconnect(generation, 'UDP 控制通道发生错误，正在重试。'));
    currentPeer.addEventListener('connectionstatechange', () => {
      if (generation !== connectionGeneration || peer !== currentPeer) return;
      if (currentPeer.connectionState === 'disconnected' || currentPeer.connectionState === 'failed' || currentPeer.connectionState === 'closed') scheduleReconnect(generation, 'UDP 直连已中断，正在重试。');
    });
    connectTimer = setTimeout(() => scheduleReconnect(generation, '十秒内未能建立 UDP 直连，正在重试；你也可以手动选择转发。'), configuration.connect_timeout_ms);
    try {
      await currentPeer.setLocalDescription(await currentPeer.createOffer());
      await waitForICEGathering(currentPeer, configuration.connect_timeout_ms);
      const response = await fetch('/control/webrtc/offer', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(currentPeer.localDescription)
      });
      if (!response.ok) throw new Error(`信令返回 HTTP ${response.status}`);
      await currentPeer.setRemoteDescription(await response.json());
    } catch (error) {
      scheduleReconnect(generation, `UDP 直连失败：${error.message}`);
    }
  }

  function connectRelayed(generation) {
    if (generation !== connectionGeneration) return;
    closeCurrentTransport();
    showConnecting();
    const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const currentSocket = new WebSocket(`${scheme}//${location.host}/control/ws`);
    socket = currentSocket;
    currentSocket.binaryType = 'blob';
    currentSocket.addEventListener('open', () => markConnected(generation));
    currentSocket.addEventListener('message', event => {
      if (event.data instanceof Blob) offerLatestFrame(event.data); else handleServerUpdate(event.data);
    });
    currentSocket.addEventListener('close', () => {
      if (generation === connectionGeneration && socket === currentSocket) scheduleReconnect(generation, '转发连接已中断，正在重试。');
    });
  }

  async function updateStats(generation) {
    if (generation !== connectionGeneration) return;
    const now = Date.now();
    const elapsed = Math.max(1, now - lastStatsAt);
    fpsBadge.textContent = `${Math.round(framesSinceSample * 1000 / elapsed)} FPS`;
    framesSinceSample = 0; lastStatsAt = now;
    if (selectedTransport !== 'direct' || !peer) { rttBadge.textContent = '转发'; return; }
    try {
      const stats = await peer.getStats();
      for (const report of stats.values()) {
        if (report.type === 'candidate-pair' && report.state === 'succeeded' && report.nominated) {
          rttBadge.textContent = `${Math.round((report.currentRoundTripTime || 0) * 1000)} ms`;
          break;
        }
      }
    } catch (_) {}
  }

  function selectTransport(next) {
    if (next === selectedTransport && (peer || socket)) return;
    releaseAll();
    connectionGeneration += 1;
    selectedTransport = next;
    reconnectDeadline = 0;
    closeCurrentTransport();
    updateTransportControls();
    if (next === 'direct') connectDirect(connectionGeneration); else connectRelayed(connectionGeneration);
  }

  window.addEventListener('keydown', event => {
    const key = keyMap.get(event.code); if (!key) return;
    event.preventDefault(); if (!event.repeat) { pressedKeys.add(key); sendKeyboard(); }
  }, { capture: true });
  window.addEventListener('keyup', event => {
    const key = keyMap.get(event.code); if (!key) return;
    event.preventDefault(); pressedKeys.delete(key); sendKeyboard();
  }, { capture: true });
  window.addEventListener('blur', releaseAll);
  document.addEventListener('visibilitychange', () => { if (document.hidden) releaseAll(); });
  canvas.addEventListener('mousemove', event => sendPointer(event));
  canvas.addEventListener('mousedown', event => {
    const name = buttonName(event.button); if (name) pointerButtons.add(name);
    canvas.focus(); if (pointerMode === 'relative' && document.pointerLockElement !== canvas) canvas.requestPointerLock(); sendPointer(event);
  });
  window.addEventListener('mouseup', event => { const name = buttonName(event.button); if (name) pointerButtons.delete(name); sendPointer(event); });
  canvas.addEventListener('wheel', event => { event.preventDefault(); sendPointer(event, -Math.sign(event.deltaY) * 3); }, { passive: false });
  canvas.addEventListener('contextmenu', event => event.preventDefault());
  pointerModeButton.addEventListener('click', () => {
    releaseAll(); pointerMode = pointerMode === 'absolute' ? 'relative' : 'absolute';
    pointerModeButton.textContent = pointerMode === 'absolute' ? '绝对鼠标' : '相对鼠标';
    pointerModeButton.classList.toggle('active', pointerMode === 'relative');
    if (pointerMode === 'absolute' && document.pointerLockElement) document.exitPointerLock();
  });
  document.addEventListener('pointerlockchange', () => { if (pointerMode === 'relative' && document.pointerLockElement !== canvas) releaseAll(); });
  document.addEventListener('pointerlockerror', releaseAll);
  specialKeysButton.addEventListener('click', () => {
    specialKeys.hidden = !specialKeys.hidden;
    specialKeysButton.classList.toggle('active', !specialKeys.hidden);
    specialKeysButton.setAttribute('aria-expanded', String(!specialKeys.hidden));
  });
  document.querySelector('#fullscreen').addEventListener('click', () => controlSurface.requestFullscreen());
  document.querySelector('#release-all').addEventListener('click', releaseAll);
  directButton.addEventListener('click', () => selectTransport('direct'));
  relayedButton.addEventListener('click', () => selectTransport('relayed'));
  document.querySelectorAll('[data-modifier]').forEach(button => button.addEventListener('click', () => {
    const modifier = button.dataset.modifier;
    if (latchedModifiers.has(modifier)) latchedModifiers.delete(modifier); else latchedModifiers.add(modifier);
    button.classList.toggle('active', latchedModifiers.has(modifier)); sendKeyboard();
  }));
  document.querySelectorAll('[data-key]').forEach(button => button.addEventListener('click', () => {
    pressedKeys.add(button.dataset.key); sendKeyboard(); setTimeout(() => { pressedKeys.delete(button.dataset.key); sendKeyboard(); }, 80);
  }));
  document.querySelectorAll('[data-chord]').forEach(button => button.addEventListener('click', () => {
    releaseAll(); button.dataset.chord.split(',').forEach(key => pressedKeys.add(key)); sendKeyboard(); setTimeout(releaseAll, 100);
  }));

  (async () => {
    try {
      const response = await fetch('/control/webrtc/config');
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      configuration = await response.json();
      selectedTransport = configuration.enabled ? 'direct' : 'relayed';
      connectionGeneration += 1;
      reconnectDeadline = 0;
      updateTransportControls();
      if (selectedTransport === 'direct') connectDirect(connectionGeneration); else connectRelayed(connectionGeneration);
    } catch (error) {
      setBadge(connectionBadge, '配置失败', 'error');
      message.textContent = `无法读取连接配置：${error.message}`;
    }
  })();
})();
