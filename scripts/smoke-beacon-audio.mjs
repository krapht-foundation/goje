/** @file Exercise Beacon's real Web Audio output and silent fallback in Chromium. */

import assert from "node:assert/strict";
import { promisify } from "node:util";

import { chromium } from "playwright";
import { PNG } from "pngjs";

import { servePlayground } from "./playground-server.mjs";

/** Install a software tap on real sources without replacing their output. */
function tapAudio() {
  const tap = { context: null, analyser: null, starts: 0, stops: 0, requested: 0 };
  Object.defineProperty(window, "beaconAudioProbe", { value: tap });
  const create = BaseAudioContext.prototype.createBufferSource;
  const connect = AudioNode.prototype.connect;
  const start = AudioBufferSourceNode.prototype.start;
  const stop = AudioBufferSourceNode.prototype.stop;
  BaseAudioContext.prototype.createBufferSource = function () {
    const source = create.call(this);
    if (tap.context !== this) {
      tap.context = this;
      tap.analyser = this.createAnalyser();
    }
    connect.call(source, tap.analyser);
    return source;
  };
  AudioBufferSourceNode.prototype.start = function (...args) {
    start.apply(this, args);
    tap.starts++;
  };
  AudioBufferSourceNode.prototype.stop = function (...args) {
    stop.apply(this, args);
    tap.stops++;
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
 * Exercise output, the trust boundary for unlock, and lifecycle stops.
 * @param {import("playwright").Browser} browser - Managed headless Chromium.
 * @param {string} url - Served Beacon build.
 */
async function playback(browser, url) {
  const page = await browser.newPage();
  page.setDefaultTimeout(5_000);
  try {
    await page.addInitScript(tapAudio);
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

    await canvas.evaluate(element => element.blur());
    await page.waitForFunction(() => window.beaconAudioProbe.stops === 1);
    await canvas.focus();
    await canvas.press("r");
    await beacon(canvas, false);
    assert.equal(await page.evaluate(() => window.beaconAudioProbe.starts), 1, "focus or reset replayed the cue");
    console.log(`Beacon Web Audio smoke passed; key-to-software-tap ${milliseconds.toFixed(1)} ms`);
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

const [directory] = process.argv.slice(2);
if (!directory || process.argv.length !== 3) throw new Error("Usage: node scripts/smoke-beacon-audio.mjs WEB_DIR");
const server = await servePlayground(0, directory);
try {
  const browser = await chromium.launch({ timeout: 15_000 });
  try {
    const url = `http://127.0.0.1:${server.address().port}/`;
    await playback(browser, url);
    await withoutOutput(browser, url, false);
    await withoutOutput(browser, url, true);
  } finally {
    await browser.close();
  }
} finally {
  await promisify(server.close.bind(server))();
}
