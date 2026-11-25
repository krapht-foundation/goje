/**
 * @file Compare the Go mixer with the browser host's Web Audio graphs in Chromium.
 *
 * `go run ./cmd/mixerref` prints a fixed script of overlapping sound requests and
 * one of music requests, and what `audio.Mixer` renders for each. This script
 * renders the same requests offline in Chromium and requires each to agree within
 * 2% RMS. The graphs mirror `webAudio.start` in internal/platform/browser/audio_js.go
 * and `webMusic` in internal/platform/browser/music_js.go; keep them in step.
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
 * Render a music script with the browser host's scheduling: a looping buffer source →
 * gain → destination per track, linear gain ramps, and a replacement that starts when
 * the current track's fade out ends. Runs in the page.
 * @param {{sampleRate: number, music: {frames: number, tracks: number[][],
 *   requests: {frame: number, kind: string, track: number, gain: number, fadeMS: number}[]}}} script - Music script.
 * @returns {Promise<number[]>} Interleaved stereo frames, left first.
 */
async function renderMusic({ sampleRate, music: { frames, tracks, requests } }) {
  const context = new OfflineAudioContext({ numberOfChannels: 2, length: frames, sampleRate });
  const buffers = tracks.map(samples => {
    const buffer = new AudioBuffer({ length: samples.length, numberOfChannels: 1, sampleRate });
    buffer.copyToChannel(Float32Array.from(samples, sample => sample / 32768), 0);
    return buffer;
  });
  const none = () => ({ source: null, gain: null, track: -1, from: 0, to: 0, start: 0, stop: 0 });
  const schedule = (t, from, to, at, fade) => {
    t.gain.gain.cancelScheduledValues(at);
    if (fade > 0) {
      t.gain.gain.setValueAtTime(from, at);
      t.gain.gain.linearRampToValueAtTime(to, at + fade);
    } else {
      t.gain.gain.setValueAtTime(to, at);
    }
    Object.assign(t, { from, to, start: at, stop: at + fade });
  };
  const ramp = (t, target, now, fade) => {
    let from = t.to;
    if (now <= t.start) from = t.from;
    else if (now < t.stop) from = t.from + (t.to - t.from) * (now - t.start) / (t.stop - t.start);
    schedule(t, from, target, now, fade);
  };
  const newTrack = (track, gain, at, fade) => {
    const source = context.createBufferSource();
    source.buffer = buffers[track];
    source.loop = true;
    const t = { ...none(), source, gain: context.createGain(), track };
    schedule(t, 0, gain, at, fade);
    source.connect(t.gain);
    t.gain.connect(context.destination);
    source.start(at);
    return t;
  };
  // An offline context cannot stop a node in the past, so ended tracks stop at their switch time.
  const end = (t, at) => t.source?.stop(at);
  const m = { current: none(), next: none(), stopping: false, switchAt: 0 };
  const settle = now => {
    if (!(m.stopping || m.next.source) || now < m.switchAt) return;
    end(m.current, m.switchAt);
    Object.assign(m, { current: m.next, next: none(), stopping: false });
  };
  for (const { frame, kind, track, gain, fadeMS } of requests) {
    const now = frame / sampleRate;
    const fade = fadeMS / 1000;
    settle(now);
    if (kind === "play") {
      if (!m.current.source) {
        m.current = newTrack(track, gain, now, fade);
      } else if (m.current.track === track && !m.stopping) {
        end(m.next, now);
        m.next = none();
        ramp(m.current, gain, now, fade);
      } else {
        end(m.next, now);
        ramp(m.current, 0, now, fade);
        Object.assign(m, { stopping: false, switchAt: now + fade });
        m.next = newTrack(track, gain, m.switchAt, fade);
      }
    } else if (kind === "stop") {
      if (!m.current.source) continue;
      end(m.next, now);
      m.next = none();
      ramp(m.current, 0, now, fade);
      Object.assign(m, { stopping: true, switchAt: now + fade });
    } else if (m.current.source && !m.stopping) {
      if (m.next.source) schedule(m.next, 0, gain, m.next.start, m.next.stop - m.next.start);
      else ramp(m.current, gain, now, fade);
    }
  }
  settle(frames / sampleRate);
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
  cwd: root, encoding: "utf8", maxBuffer: 64 << 20,
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

  const music = reference.music;
  const played = await page.evaluate(renderMusic, reference);
  assert.equal(played.length, music.mixed.length);
  const musicError = relativeError(played, music.mixed);
  assert.ok(musicError < 0.02, `Web Audio music differs from the Go mixer by ${(100 * musicError).toFixed(3)}% RMS`);
  // The script ends in silence after the stop, and a shifted reference must fail.
  const tail = music.mixed.slice(-2 * 1000);
  assert.ok(tail.every(sample => sample === 0), "the Go music voice did not stop");
  assert.ok(played.slice(-2 * 1000).every(sample => Math.abs(sample) < 1e-6), "the Web Audio music did not stop");
  const late = music.mixed.map((_, i) => music.mixed[i - 2 * 4800] ?? 0);
  assert.ok(relativeError(played, late) >= 0.02, "the music comparison does not detect a late switch");
  console.log(`Mixer parity passed: ${reference.requests.length} overlapping voices within `
    + `${(100 * error).toExponential(1)}% RMS and ${music.requests.length} music requests within `
    + `${(100 * musicError).toExponential(1)}% RMS of the Go mixer in Chromium ${browser.version()}`);
} finally {
  await browser.close();
}
