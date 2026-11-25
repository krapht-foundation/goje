/**
 * @file Compare the Go mixer with the browser host's Web Audio voice graph in Chromium.
 *
 * `go run ./cmd/mixerref` prints a fixed script of overlapping requests and
 * what `audio.Mixer` renders for it. This script renders the same requests offline in
 * Chromium and requires the two to agree within 2% RMS. The graph mirrors
 * `webAudio.start` in internal/platform/browser/audio_js.go; keep the two in step.
 */

import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

import { chromium } from "playwright";

/**
 * Render a request script with one buffer source → gain → stereo panner per voice.
 * Runs in the page.
 * @param {{sampleRate: number, frames: number, clips: number[][],
 *   requests: {frame: number, cue: number, gain: number, pan: number}[]}} script - Requests and clips.
 * @returns {Promise<number[]>} Interleaved stereo frames, left first.
 */
async function renderWebAudio({ sampleRate, frames, clips, requests }) {
  const context = new OfflineAudioContext({ numberOfChannels: 2, length: frames, sampleRate });
  // The host converts 16-bit PCM the same way before creating its buffers.
  const buffers = clips.map(samples => {
    const buffer = new AudioBuffer({ length: samples.length, numberOfChannels: 1, sampleRate });
    buffer.copyToChannel(Float32Array.from(samples, sample => sample / 32768), 0);
    return buffer;
  });
  for (const { frame, cue, gain: level, pan } of requests) {
    const source = context.createBufferSource();
    source.buffer = buffers[cue];
    const gain = context.createGain();
    gain.gain.value = level;
    const panner = context.createStereoPanner();
    panner.pan.value = pan;
    source.connect(gain);
    gain.connect(panner);
    panner.connect(context.destination);
    source.start(frame / sampleRate);
  }
  const rendered = await context.startRendering();
  const left = rendered.getChannelData(0);
  const right = rendered.getChannelData(1);
  return Array.from({ length: 2 * frames }, (_, i) => (i & 1 ? right : left)[i >> 1]);
}

/**
 * Relative RMS error of rendered output against a reference.
 * @param {number[]} actual - Interleaved frames to check.
 * @param {number[]} expected - Interleaved reference frames.
 * @returns {number} RMS of the difference divided by the RMS of the reference.
 */
function relativeError(actual, expected) {
  let error = 0;
  let level = 0;
  for (let i = 0; i < expected.length; i++) {
    error += (actual[i] - expected[i]) ** 2;
    level += expected[i] ** 2;
  }
  return Math.sqrt(error / level);
}

const root = fileURLToPath(new URL("..", import.meta.url));
const reference = JSON.parse(execFileSync(process.env.GO ?? "go", ["run", "./cmd/mixerref"], {
  cwd: root, encoding: "utf8", maxBuffer: 16 << 20,
}));
const browser = await chromium.launch({ timeout: 15_000 });
try {
  const page = await browser.newPage();
  const rendered = await page.evaluate(renderWebAudio, reference);
  assert.equal(rendered.length, reference.mixed.length);
  const error = relativeError(rendered, reference.mixed);
  assert.ok(error < 0.02, `Web Audio differs from the Go mixer by ${(100 * error).toFixed(3)}% RMS`);
  // Swapping the channels must fail, so the check cannot pass on symmetric output.
  const swapped = reference.mixed.map((_, i) => reference.mixed[i ^ 1]);
  assert.ok(relativeError(rendered, swapped) >= 0.02, "the comparison does not distinguish left from right");
  console.log(`Mixer parity passed: ${reference.requests.length} overlapping voices within `
    + `${(100 * error).toExponential(1)}% RMS of the Go mixer in Chromium ${browser.version()}`);
} finally {
  await browser.close();
}
