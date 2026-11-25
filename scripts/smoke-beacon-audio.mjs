/** @file Exercise Beacon's real Web Audio output and silent fallback in Chromium. */

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { chromium } from "playwright";
import { PNG } from "pngjs";

import { servePlayground } from "./playground-server.mjs";

/**
 * Install a software tap on real sources without replacing their output.
 * @param {number | null} rate - Sample rate to force on the page's audio context, or null for the device's.
 */
function tapAudio(rate) {
  const tap = {
    context: null, analyser: null, recorder: null, starts: 0, stops: 0, requested: 0, rate: 0, samples: [], pending: [],
    music: [], musicGain: null, ramps: [],
  };
  Object.defineProperty(window, "beaconAudioProbe", { value: tap });
  const connect = AudioNode.prototype.connect;
  const start = AudioBufferSourceNode.prototype.start;
  const stop = AudioBufferSourceNode.prototype.stop;
  // The worklet runs on the audio thread and posts every block in order, so a busy page
  // cannot drop samples the way a main-thread script processor does on a slow runner.
  const processor = `registerProcessor("goje-tap", class extends AudioWorkletProcessor {
    process(inputs) {
      this.port.postMessage(inputs[0][0] ? inputs[0][0].slice() : new Float32Array(128));
      return true;
    }
  });`;
  const Context = window.AudioContext;
  window.AudioContext = class extends Context {
    /** @param {AudioContextOptions} [options] - Options the game asked for. */
    constructor(options) {
      // Forcing a rate different from the 48 kHz cue exercises the browser's resampler on every machine.
      super(rate === null ? options : { ...options, sampleRate: rate });
      tap.context = this;
      tap.rate = this.sampleRate;
      tap.analyser = this.createAnalyser();
      const module = URL.createObjectURL(new Blob([processor], { type: "text/javascript" }));
      this.audioWorklet.addModule(module).then(() => {
        // Copy what the graph renders, from the first audible sample, while the
        // host's own connection to the destination stays in place.
        const recorder = new AudioWorkletNode(this, "goje-tap");
        recorder.port.onmessage = event => {
          if (!tap.samples.length && !event.data.some(sample => sample !== 0)) return;
          if (tap.samples.length < tap.rate) for (const sample of event.data) tap.samples.push(sample);
        };
        connect.call(recorder, this.destination);
        tap.recorder = recorder;
        for (const source of tap.pending) connect.call(source, recorder);
      });
    }
  };
  // Beacon's music is the only looping source. Keep it out of the cue's analyser and
  // recorder and its counts, and remember its gain node so its fades can be read back.
  AudioNode.prototype.connect = function (target, ...rest) {
    if (this instanceof AudioBufferSourceNode && this.loop) tap.musicGain = target;
    return connect.call(this, target, ...rest);
  };
  const ramp = AudioParam.prototype.linearRampToValueAtTime;
  AudioParam.prototype.linearRampToValueAtTime = function (value, time) {
    tap.ramps.push({ param: this, value });
    return ramp.call(this, value, time);
  };
  AudioBufferSourceNode.prototype.start = function (...args) {
    if (this.loop) {
      start.apply(this, args);
      tap.music.push({ length: this.buffer.length, rate: this.buffer.sampleRate });
      return;
    }
    connect.call(this, tap.analyser);
    if (tap.recorder) connect.call(this, tap.recorder);
    else tap.pending.push(this);
    start.apply(this, args);
    tap.starts++;
  };
  AudioBufferSourceNode.prototype.stop = function (...args) {
    stop.apply(this, args);
    if (!this.loop) tap.stops++;
  };
  document.addEventListener("keydown", event => {
    if (event.isTrusted && event.code === "Space") tap.requested = performance.now();
  }, { capture: true });
}

/**
 * Wait for a specific rendered beacon state at its original arena placement.
 * @param {import("playwright").Locator} canvas - Beacon's real WebGL canvas.
 * @param {boolean} lit - Expected beacon state.
 */
async function beacon(canvas, lit) {
  const target = lit ? [248, 190, 66] : [91, 141, 209];
  for (let attempt = 0; attempt < 40; attempt++) {
    const png = PNG.sync.read(await canvas.screenshot({ timeout: 4000 }));
    const offset = 4 * (Math.floor(png.height * 32 / 128) * png.width + Math.floor(png.width * 32 / 160));
    if (target.every((channel, i) => png.data[offset + i] === channel)) return;
  }
  assert.fail(`Beacon never rendered ${lit ? "lit" : "unlit"}`);
}

/**
 * Read the samples of a mono 16-bit WAV file.
 * @param {Buffer} wav - Complete file.
 * @returns {Int16Array} The data chunk's samples.
 */
function wavSamples(wav) {
  assert.equal(wav.toString("latin1", 0, 4), "RIFF");
  assert.equal(wav.toString("latin1", 8, 12), "WAVE");
  for (let at = 12; at + 8 <= wav.length;) {
    const size = wav.readUInt32LE(at + 4);
    if (wav.toString("latin1", at, at + 4) === "data") {
      const data = wav.subarray(at + 8, at + 8 + size);
      return new Int16Array(data.buffer.slice(data.byteOffset, data.byteOffset + size));
    }
    at += 8 + size + (size & 1);
  }
  return assert.fail("WAV has no data chunk");
}

/**
 * Check what the Web Audio graph rendered against the file the game embeds.
 * @param {number[]} rendered - Samples captured from the first audible one.
 * @param {number} rate - Sample rate of the browser's audio context.
 * @param {Int16Array} expected - Samples the game was given.
 * @returns {string} How the comparison was made.
 */
function compareOutput(rendered, rate, expected) {
  const first = rendered.findIndex(sample => sample !== 0);
  const out = rendered.slice(first);
  const want = Array.from(expected.subarray(expected.findIndex(sample => sample !== 0)), sample => sample / 32768);
  if (rate === 48000) {
    // Same rate: the buffer plays untouched, so every float must match exactly.
    assert.deepEqual(out.slice(0, want.length), want, "rendered output equals the embedded samples");
    assert.ok(out.slice(want.length).every(sample => sample === 0), "silence after the cue");
    return "bit-exact at 48 kHz";
  }
  // The browser resampled the buffer with its own filter. That filter can start the signal a few
  // samples early or late (a tiny pre-ringing sample before the cue shifts "first audible"), and it
  // rings at sharp edges where linear interpolation does not, so a peak comparison would fail on a
  // square wave. Find the best alignment within a short window and compare the RMS error with the
  // cue's own RMS level.
  const count = Math.floor(want.length * rate / 48000) - 2;
  const reach = Math.ceil(rate / 48000 * 32);
  assert.ok(rendered.length >= first + count + reach, "the whole cue was rendered");
  const reference = Float64Array.from({ length: count }, (_, i) => {
    const at = i * 48000 / rate;
    const lo = Math.floor(at);
    return want[lo] + (want[Math.min(lo + 1, want.length - 1)] - want[lo]) * (at - lo);
  });
  const level = Math.sqrt(reference.reduce((sum, sample) => sum + sample * sample, 0) / count);
  let best = { error: Infinity, lag: 0 };
  for (let lag = -reach; lag <= reach; lag++) {
    let sum = 0;
    for (let i = 0; i < count; i++) sum += ((rendered[first + lag + i] ?? 0) - reference[i]) ** 2;
    const error = Math.sqrt(sum / count) / level;
    if (error < best.error) best = { error, lag };
  }
  assert.ok(best.error < 0.1,
    `resampled output differs by ${best.error} of the cue's RMS at best offset ${best.lag} (${rate} Hz)`);
  return `within ${(100 * best.error).toFixed(1)}% RMS after resampling ${rate} Hz, offset ${best.lag} samples`;
}

/**
 * Exercise output, the trust boundary for unlock, and lifecycle stops.
 * @param {import("playwright").Browser} browser - Managed headless Chromium.
 * @param {string} url - Served Beacon build.
 * @param {Int16Array} expected - Samples of the cue the game embeds.
 * @param {number | null} forcedRate - Sample rate to force on the audio context, or null for the device's.
 */
async function playback(browser, url, expected, forcedRate) {
  const page = await browser.newPage();
  page.setDefaultTimeout(5_000);
  try {
    await page.addInitScript(tapAudio, forcedRate);
    await page.goto(url);
    const canvas = page.locator("#goje");
    await beacon(canvas, false);
    // A synthetic keyboard event reaches gameplay but must not unlock audio.
    await canvas.dispatchEvent("keydown", { code: "Space", bubbles: true });
    await canvas.dispatchEvent("keyup", { code: "Space", bubbles: true });
    await beacon(canvas, true);
    assert.equal(await page.evaluate(() => window.beaconAudioProbe.starts), 0);
    await canvas.press("r");
    await beacon(canvas, false);
    assert.equal(await page.evaluate(() => window.beaconAudioProbe.starts), 0, "unlock did not replay the locked cue");
    // The capture worklet loads asynchronously after the context starts. Wait for it so
    // the cue's first samples are recorded.
    await page.waitForFunction(() => window.beaconAudioProbe.recorder);

    // Wait from before the real key press so a short buffer cannot go unnoticed.
    const signal = page.waitForFunction(() => {
      const { analyser, requested } = window.beaconAudioProbe;
      if (!analyser || !requested) return false;
      const samples = new Float32Array(analyser.fftSize);
      analyser.getFloatTimeDomainData(samples);
      return samples.some(sample => Math.abs(sample) > 0.01) && performance.now() - requested;
    });
    await canvas.press("Space");
    const milliseconds = await (await signal).jsonValue();
    assert.equal(await page.evaluate(() => window.beaconAudioProbe.starts), 1);
    const rate = await page.evaluate(() => window.beaconAudioProbe.rate);
    const needed = Math.ceil(expected.length * rate / 48000) + 512;
    await page.waitForFunction(count => window.beaconAudioProbe.samples.length >= count, needed);
    const how = compareOutput(await page.evaluate(() => window.beaconAudioProbe.samples), rate, expected);

    await canvas.evaluate(element => element.blur());
    await page.waitForFunction(() => window.beaconAudioProbe.stops === 1);
    await canvas.focus();
    await canvas.press("r");
    await beacon(canvas, false);
    assert.equal(await page.evaluate(() => window.beaconAudioProbe.starts), 1, "focus or reset replayed the cue");

    // The music started once, after unlock, as a looping 48 kHz buffer that faded in to the play gain.
    /** @returns {Promise<{ starts: { length: number, rate: number }[], targets: number[] }>} Music starts and fades. */
    const music = () => page.evaluate(() => {
      const { music: starts, musicGain, ramps } = window.beaconAudioProbe;
      return { starts, targets: ramps.filter(ramp => ramp.param === musicGain?.gain).map(ramp => ramp.value) };
    });
    /** @param {number} gain - Expected final target of the music's latest fade. */
    const fadeTo = gain => page.waitForFunction(want => {
      const { musicGain, ramps } = window.beaconAudioProbe;
      const targets = ramps.filter(ramp => ramp.param === musicGain?.gain);
      return Math.abs(targets.at(-1)?.value - want) < 1e-6;
    }, gain);
    let track = await music();
    assert.equal(track.starts.length, 1, "the music started once");
    assert.ok(track.starts[0].rate === 48000 && track.starts[0].length > 48000, "the music is the 48 kHz loop");
    assert.ok(Math.abs(track.targets[0] - 0.5) < 1e-6, "the music faded in to the play gain");
    // Space lights column 0 and a click lights beacon #2 at (2, 0); the win fades the music down.
    await canvas.press("Space");
    const box = await canvas.boundingBox();
    assert.ok(box);
    await page.mouse.click(box.x + box.width * 96 / 160, box.y + box.height * 32 / 128);
    await fadeTo(0.2);
    await canvas.press("r");
    await fadeTo(0.5);
    track = await music();
    assert.equal(track.starts.length, 1, "restart kept the looping track instead of starting it again");
    console.log(`Beacon Web Audio smoke passed; rendered cue ${how}; key-to-software-tap ${milliseconds.toFixed(1)} ms; ` +
      `one ${(track.starts[0].length / 48000).toFixed(1)} s looping music track with fades ` +
      track.targets.map(target => target.toFixed(2)).join(" → "));
  } finally {
    await page.close();
  }
}

/**
 * Verify device absence does not change the outcome of a game command.
 * @param {import("playwright").Browser} browser - Managed headless Chromium.
 * @param {string} url - Served Beacon build.
 * @param {boolean} failPlayback - Simulate a playback exception instead of a missing API.
 */
async function withoutOutput(browser, url, failPlayback) {
  const page = await browser.newPage();
  try {
    await page.addInitScript(fails => {
      if (!fails) {
        Object.defineProperty(window, "AudioContext", { value: undefined });
        return;
      }
      window.AudioContext = class extends AudioContext {
        /** Capture the real context so the test can wait for its running state. */
        constructor(options) {
          super(options);
          window.beaconFaultContext = this;
        }
      };
      window.beaconOutputFailures = 0;
      AudioBufferSourceNode.prototype.start = function () {
        window.beaconOutputFailures++;
        throw new DOMException("Output failed", "InvalidStateError");
      };
    }, failPlayback);
    await page.goto(url);
    const canvas = page.locator("#goje");
    await beacon(canvas, false);
    if (failPlayback) {
      await canvas.press("q");
      await page.waitForFunction(() => window.beaconFaultContext?.state === "running");
    }
    await canvas.press("Space");
    await beacon(canvas, true);
    await canvas.press("r");
    await beacon(canvas, false);
    if (failPlayback) {
      assert.equal(await page.evaluate(() => window.beaconOutputFailures), 1);
    }
    console.log(`Beacon played normally ${failPlayback ? "after a Web Audio playback error" : "without a Web Audio device"}`);
  } finally {
    await page.close();
  }
}

const [directory, wavPath, ...extra] = process.argv.slice(2);
if (!directory || extra.length) throw new Error("Usage: node scripts/smoke-beacon-audio.mjs WEB_DIR [WAV]");
// Without a file, check the cue Beacon ships with.
const expected = wavSamples(await readFile(wavPath ?? fileURLToPath(new URL("../examples/beacon/assets/toggle.wav", import.meta.url))));
const server = await servePlayground(0, directory);
try {
  const browser = await chromium.launch({ timeout: 15_000 });
  try {
    const url = `http://127.0.0.1:${server.address().port}/`;
    await playback(browser, url, expected, null);
    // A rate other than the cue's 48 kHz makes the browser resample, as some hosts' devices do.
    await playback(browser, url, expected, 44100);
    await withoutOutput(browser, url, false);
    await withoutOutput(browser, url, true);
  } finally {
    await browser.close();
  }
} finally {
  await promisify(server.close.bind(server))();
}
