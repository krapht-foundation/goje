/** @file Bind the effect form, waveform view, Web Audio preview, and WAV handoff to the Go Sound Maker document. */

/**
 * Find a static element in the Sound Maker page.
 * @template {Element} T
 * @param {string} id - Element ID.
 * @returns {T} The corresponding element.
 */
function element(id) {
  return /** @type {T} */ (document.getElementById(id));
}

const SAMPLE_RATE = 48000;
const form = /** @type {HTMLFormElement} */ (element("controls"));
const fields = /** @type {HTMLFieldSetElement} */ (form.querySelector("fieldset"));
const waveform = /** @type {HTMLSelectElement} */ (element("waveform"));
const startHz = /** @type {HTMLInputElement} */ (element("start-hz"));
const endHz = /** @type {HTMLInputElement} */ (element("end-hz"));
const duration = /** @type {HTMLInputElement} */ (element("duration"));
const attack = /** @type {HTMLInputElement} */ (element("attack"));
const release = /** @type {HTMLInputElement} */ (element("release"));
const play = /** @type {HTMLButtonElement} */ (element("play"));
const download = /** @type {HTMLButtonElement} */ (element("download"));
const status = element("status");
const summary = element("summary");
const wave = /** @type {HTMLCanvasElement} */ (element("wave"));
const waveContext = wave.getContext("2d");

/**
 * Load Wasm and connect the page to its single effect document.
 * @returns {Promise<void>} Resolves when the effect can be edited, previewed, and downloaded.
 * @throws {Error} When the canvas, Wasm download, or Go core is unavailable.
 */
async function start() {
  if (!waveContext) throw new Error("Canvas is unavailable.");
  const go = new Go();
  const response = await fetch("soundmaker.wasm");
  if (!response.ok) throw new Error(`Wasm download failed: HTTP ${response.status}`);
  const { instance } = await WebAssembly.instantiateStreaming(response, go.importObject);
  go.run(instance).then(() => stop("The Sound Maker core stopped. Reload the page."), error => stop(error.message));
  const core = globalThis.gojeSoundMaker;
  if (!core) throw new Error("The Sound Maker core did not start.");
  const { minHz, maxHz, minDurationMS, maxDurationMS } = core.limits;
  for (const input of [startHz, endHz]) [input.min, input.max] = [String(minHz), String(maxHz)];
  for (const input of [duration, attack, release]) input.max = String(maxDurationMS);
  duration.min = String(minDurationMS);
  attack.min = release.min = "0";
  /** @type {AudioContext | null} */
  let audio = null;

  /** Copy the accepted settings into the draft fields, summary, and waveform view. */
  function show() {
    const s = core.settings();
    waveform.value = s.waveform;
    startHz.value = String(s.startHz);
    endHz.value = String(s.endHz);
    duration.value = String(s.durationMS);
    attack.value = String(s.attackMS);
    release.value = String(s.releaseMS);
    summary.textContent = `${s.waveform}, ${s.startHz} to ${s.endHz} Hz, ${s.durationMS} ms, attack ${s.attackMS} ms, release ${s.releaseMS} ms`;
    draw(samples(core.pcm()));
  }

  form.addEventListener("submit", event => {
    event.preventDefault();
    // Empty or fractional drafts reach Go as NaN or fractions so it reports one consistent diagnostic.
    const error = core.apply(waveform.value, startHz.valueAsNumber, endHz.valueAsNumber,
      duration.valueAsNumber, attack.valueAsNumber, release.valueAsNumber);
    if (error) {
      message(`Not applied: ${error}`, true);
      return;
    }
    show();
    message(`Applied: ${core.pcm().length / 2} samples, ${core.settings().durationMS} ms.`);
  });

  play.addEventListener("click", () => {
    try {
      void audio?.close();
      audio = null;
      const context = new AudioContext({ sampleRate: SAMPLE_RATE });
      audio = context;
      const data = samples(core.pcm());
      const buffer = context.createBuffer(1, data.length, SAMPLE_RATE);
      buffer.copyToChannel(data, 0);
      const source = context.createBufferSource();
      source.buffer = buffer;
      source.connect(context.destination);
      source.addEventListener("ended", () => {
        // A newer preview already closed this context and owns the status line.
        if (audio !== context) return;
        audio = null;
        void context.close();
        message("Preview finished.");
      });
      source.start();
      message("Playing preview.");
    } catch (error) {
      message(`Preview unavailable: ${error instanceof Error ? error.message : String(error)}`, true);
    }
  });

  download.addEventListener("click", () => {
    const bytes = core.exportWAV();
    if (typeof bytes === "string") {
      message(bytes, true);
      return;
    }
    const url = URL.createObjectURL(new Blob([bytes], { type: "audio/wav" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = "sound.wav";
    link.click();
    // Let the browser consume the URL before releasing the blob.
    setTimeout(() => URL.revokeObjectURL(url), 0);
    message(`Downloaded sound.wav, ${bytes.length} bytes.`);
  });

  show();
  fields.disabled = play.disabled = download.disabled = false;
  message("Ready.");
}

/**
 * Convert packed little-endian signed 16-bit PCM into Web Audio floats.
 * @param {Uint8Array} pcm - Bytes from the Go core, two per sample.
 * @returns {Float32Array} Samples in [-1, 1), each int16 / 32768.
 */
function samples(pcm) {
  const view = new DataView(pcm.buffer, pcm.byteOffset, pcm.byteLength);
  const out = new Float32Array(pcm.byteLength >> 1);
  for (let i = 0; i < out.length; i++) out[i] = view.getInt16(i * 2, true) / 32768;
  return out;
}

/**
 * Draw the sound as a min/max envelope per pixel column around a zero baseline.
 * @param {Float32Array} data - Samples in [-1, 1).
 */
function draw(data) {
  const ctx = /** @type {CanvasRenderingContext2D} */ (waveContext);
  const { width, height } = wave;
  const mid = height / 2;
  ctx.clearRect(0, 0, width, height);
  ctx.fillStyle = getComputedStyle(wave).color;
  ctx.globalAlpha = 0.4;
  ctx.fillRect(0, Math.floor(mid), width, 1);
  ctx.globalAlpha = 1;
  for (let x = 0; x < width; x++) {
    const from = Math.floor((x * data.length) / width);
    const to = Math.max(from + 1, Math.floor(((x + 1) * data.length) / width));
    if (from >= data.length) break;
    let low = 0;
    let high = 0;
    for (let i = from; i < Math.min(to, data.length); i++) {
      low = Math.min(low, data[i]);
      high = Math.max(high, data[i]);
    }
    const top = Math.floor(mid - high * mid);
    ctx.fillRect(x, top, 1, Math.max(1, Math.ceil(mid - low * mid) - top));
  }
}

/**
 * Show a message without treating diagnostics as markup.
 * @param {string} text - Visible diagnostic or status.
 * @param {boolean} [error] - Whether the message reports a failure.
 */
function message(text, error = false) {
  status.textContent = text;
  status.toggleAttribute("data-error", error);
}

/** @param {string} text - Diagnostic describing why editing stopped. */
function stop(text) {
  fields.disabled = play.disabled = download.disabled = true;
  message(text, true);
}

start().catch(error => stop(error.message));
