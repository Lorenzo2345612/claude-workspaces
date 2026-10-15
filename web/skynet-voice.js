'use strict';
// Skynet's ears: the microphone of this window, a voice detector, and Whisper running here
// (transformers.js; the library and the models download once and stay in the browser's cache —
// the audio never leaves the machine). Saying the configured name wakes it: what follows is sent
// as an instruction, or the next phrase when the name came alone. Alt+J (or 🎙) skips the name.

(function () {
  // 3.8 fails on machines without a WebGPU adapter even when asked for WASM; 3.7.6 works.
  const LIB = 'https://cdn.jsdelivr.net/npm/@huggingface/transformers@3.7.6';
  // whisper-base: tiny garbles short names (it heard «Skynet» as «Escañad»).
  const MODEL = 'onnx-community/whisper-base';
  const RATE = 16000, FRAME = 480; // 30 ms frames
  const V = {
    on: false, mode: 'off', // off, cargando, escuchando, activo, oyendo, transcribiendo, error
    progress: '', err: '', heard: '',
    stream: null, ctx: null, node: null,
    noise: 0.004, speech: false, frames: [], pre: [], silence: 0, voiced: 0,
    awakeUntil: 0, push: false, busy: false, queue: [],
    asr: {},
  };

  const norm = s => s.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase().replace(/[^a-z0-9ñ ]+/g, ' ').replace(/\s+/g, ' ').trim();
  function lev(a, b) {
    const d = Array.from({ length: a.length + 1 }, (_, i) => [i, ...Array(b.length).fill(0)]);
    for (let j = 1; j <= b.length; j++) d[0][j] = j;
    for (let i = 1; i <= a.length; i++) for (let j = 1; j <= b.length; j++)
      d[i][j] = Math.min(d[i - 1][j] + 1, d[i][j - 1] + 1, d[i - 1][j - 1] + (a[i - 1] === b[j - 1] ? 0 : 1));
    return d[a.length][b.length];
  }
  // phon is a rough Spanish phonetic key: «Skynet», «Skainet» and «esquinet» sound alike.
  const phon = w => w.replace(/^e(?=s[^aeiou])/, '').replace(/h/g, '').replace(/(qu|k|c(?=[aou])|c$)/g, 'k').replace(/c(?=[ei])|z/g, 's')
    .replace(/v/g, 'b').replace(/(ai|ay|ei|ey|y)/g, 'i').replace(/(.)\1+/g, '$1');
  // wake finds the name in a transcription (words run together, small mistakes allowed) and
  // returns what comes after it, or null.
  function wake(text) {
    const name = phon(norm(skyName()).replace(/ /g, '')), words = norm(text).split(' ').filter(Boolean).map(phon);
    const tol = name.length >= 8 ? 2 : 1;
    for (let i = 0; i < words.length; i++) {
      let joined = '';
      for (let k = i; k < Math.min(words.length, i + 3); k++) {
        joined += words[k];
        if (lev(joined, name) <= tol) return text.split(/\s+/).filter(Boolean).slice(k + 1).join(' ').replace(/^[\s,.;:!¡¿?]+/, '');
      }
    }
    return null;
  }
  // Whisper invents these on noise or silence.
  const junk = /^(gracias( por ver( el video)?)?|subtitulos?.*|amara\.org.*|\.+|…|música|\[.*\]|\(.*\))$/i;

  async function model(id) {
    if (V.asr[id]) return V.asr[id];
    V.asr[id] = (async () => {
      const T = await import(LIB);
      T.env.allowLocalModels = false;
      // WebGPU when the machine really has an adapter (navigator.gpu alone is not enough), else WASM.
      const gpu = !!(navigator.gpu && await navigator.gpu.requestAdapter().catch(() => null));
      const files = {};
      const load = useGPU => T.pipeline('automatic-speech-recognition', id, {
        device: useGPU ? 'webgpu' : 'wasm',
        dtype: useGPU ? { encoder_model: 'fp32', decoder_model_merged: 'q4' } : 'q8',
        progress_callback: ev => {
          if (ev.status === 'progress' && ev.total) {
            files[ev.file] = [ev.loaded, ev.total];
            const [l, t] = Object.values(files).reduce((a, [x, y]) => [a[0] + x, a[1] + y], [0, 0]);
            V.progress = 'descargando el modelo de voz ' + Math.round(100 * l / t) + ' %'; draw();
          }
        },
      });
      const p = gpu ? await load(true).catch(() => load(false)) : await load(false);
      V.progress = ''; return p;
    })();
    V.asr[id].catch(() => { delete V.asr[id]; });
    return V.asr[id];
  }

  async function transcribe(audio, id) {
    const asr = await model(id);
    const out = await asr(audio, { language: 'spanish', task: 'transcribe', chunk_length_s: 30 });
    return (out.text || '').trim();
  }

  function setMode(m, err) { V.mode = m; V.err = err || ''; draw(); }

  async function start() {
    if (V.stream) return true;
    try {
      V.stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true, autoGainControl: true } });
    } catch (e) {
      setMode('error', 'Sin acceso al micrófono: ' + e.message); return false;
    }
    V.ctx = new AudioContext({ sampleRate: RATE });
    const src = V.ctx.createMediaStreamSource(V.stream);
    V.node = V.ctx.createScriptProcessor(4096, 1, 1);
    let rest = new Float32Array(0);
    V.node.onaudioprocess = ev => {
      const inb = ev.inputBuffer.getChannelData(0);
      const all = new Float32Array(rest.length + inb.length); all.set(rest); all.set(inb, rest.length);
      let i = 0;
      for (; i + FRAME <= all.length; i += FRAME) frame(all.slice(i, i + FRAME));
      rest = all.slice(i);
    };
    src.connect(V.node); V.node.connect(V.ctx.destination);
    return true;
  }

  function stop() {
    V.node?.disconnect(); V.ctx?.close().catch(() => {}); V.stream?.getTracks().forEach(t => t.stop());
    V.stream = V.ctx = V.node = null; V.speech = false; V.frames = []; V.pre = [];
  }

  // frame is the voice detector: an energy threshold over an adaptive noise floor, with some
  // audio kept from before the speech starts and a pause that ends the phrase.
  function frame(f) {
    let e = 0; for (const x of f) e += x * x; const rms = Math.sqrt(e / f.length);
    const loud = rms > Math.max(V.noise * 3.2, 0.012);
    if (!V.speech) {
      V.noise = loud ? V.noise : V.noise * 0.97 + rms * 0.03;
      V.pre.push(f); if (V.pre.length > 12) V.pre.shift();
      V.voiced = loud ? V.voiced + 1 : 0;
      if (V.voiced >= 4) { V.speech = true; V.frames = [...V.pre]; V.silence = 0; if (V.mode !== 'activo') setMode('oyendo'); }
      return;
    }
    V.frames.push(f);
    V.silence = loud ? 0 : V.silence + 1;
    const long = V.frames.length * FRAME / RATE;
    if (V.silence > 26 || long > 20) { // ~0.8 s of silence ends the phrase
      V.speech = false;
      const n = V.frames.length - V.silence;
      if (n * FRAME / RATE > 0.45) {
        const audio = new Float32Array(n * FRAME); for (let k = 0; k < n; k++) audio.set(V.frames[k], k * FRAME);
        V.queue.push(audio); work();
      } else if (V.mode === 'oyendo') setMode(V.on ? 'escuchando' : 'off');
      V.frames = []; V.pre = [];
    }
  }

  async function work() {
    if (V.busy) return;
    V.busy = true;
    while (V.queue.length) {
      const audio = V.queue.shift();
      const direct = V.push || Date.now() < V.awakeUntil;
      if (direct) setMode('transcribiendo');
      try {
        let text = '';
        text = await transcribe(audio, MODEL);
        if (!direct) {
          const after = wake(text);
          if (after === null) { V.heard = ''; continue; }
          text = after;
          if (!text.trim()) { V.awakeUntil = Date.now() + 8000; V.heard = '«' + skyName() + '»'; continue; }
        }
        text = text.trim();
        if (!text || junk.test(text)) continue;
        V.push = false; V.awakeUntil = 0; V.heard = text;
        skySend(text, 'voz');
      } catch (e) {
        setMode('error', 'No pude transcribir: ' + e.message);
      } finally {
        if (!V.on && !V.push && V.queue.length === 0) stop();
      }
    }
    V.busy = false;
    setMode(Date.now() < V.awakeUntil ? 'activo' : V.on ? 'escuchando' : 'off');
  }

  // Wakes it without its name: the next phrase is the instruction.
  async function push() {
    if (!skyOn()) return;
    if (!SKY.open) skyToggle(true);
    if (!(await start())) return;
    V.push = true; V.awakeUntil = Date.now() + 10000;
    setMode('activo');
    model(MODEL).catch(e => setMode('error', e.message));
    setTimeout(() => { if (V.push && !V.speech && !V.busy) { V.push = false; if (!V.on) { stop(); } setMode(V.on ? 'escuchando' : 'off'); } }, 10000);
  }

  // sync follows the setting: listening for the name all the time, or not.
  async function sync() {
    const want = skyOn() && !!(S.config && S.config.skynet_voice);
    if (want === V.on) return;
    V.on = want;
    if (!want) { if (!V.push) stop(); setMode('off'); return; }
    if (!(await start())) { V.on = false; return; }
    setMode('cargando');
    try { await model(MODEL); setMode('escuchando'); }
    catch (e) { setMode('error', 'No pude cargar el modelo de voz: ' + e.message); }
  }

  function draw() {
    if (typeof skyRender === 'function') skyRender();
    document.querySelector('.sky .mic')?.classList.toggle('rec', V.mode === 'activo' || V.mode === 'oyendo' || V.push);
  }

  const labels = { cargando: 'preparando la voz…', escuchando: 'escuchando «NAME»', activo: 'te escucho…', oyendo: 'oyendo…', transcribiendo: 'transcribiendo…' };
  window.skyVoice = {
    sync, push,
    // feed handles 16 kHz audio as if the microphone heard it (for tests without a microphone).
    feed(audio) { V.queue.push(audio); work(); },
    state: () => ({ on: V.on || V.push, label: V.mode === 'escuchando' ? '🎙' : (labels[V.mode] || '').replace('NAME', skyName()) }),
    renderBar(el) {
      if (!el) return;
      if (V.mode === 'off' && !V.progress) { el.replaceChildren(); el.className = 'skyvoice'; return; }
      el.className = 'skyvoice' + (V.mode === 'error' ? ' err' : V.mode === 'escuchando' || V.mode === 'cargando' ? ' idle' : '');
      const wave = h('span', { class: 'wave' }, h('i'), h('i'), h('i'), h('i'));
      const txt = V.progress || V.err || (labels[V.mode] || '').replace('NAME', skyName()) + (V.heard && V.mode !== 'escuchando' ? '  ' + V.heard : '');
      el.replaceChildren(V.mode === 'activo' || V.mode === 'oyendo' ? wave : '', txt);
    },
  };
})();
