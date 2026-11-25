/** @file Verify the Sound Maker's keyboard editing, preview, WAV download, and Music panel in Chromium. */

import assert from "node:assert/strict";
import { readFile, writeFile } from "node:fs/promises";
import { promisify } from "node:util";

import { chromium } from "playwright";

import { serveSoundMaker } from "./sound-maker-server.mjs";

const peak = 26214; // 0.8 of full scale, rounded as the synthesizer does.

/** Record each preview buffer, source start, and loop flag without replacing real output. */
function tapAudio() {
  const tap = { previews: [], starts: 0, loops: [] };
  Object.defineProperty(window, "soundMakerProbe", { value: tap });
  const copy = AudioBuffer.prototype.copyToChannel;
  const start = AudioBufferSourceNode.prototype.start;
  AudioBuffer.prototype.copyToChannel = function (source, ...rest) {
    tap.previews.push(Array.from(source));
    return copy.call(this, source, ...rest);
  };
  AudioBufferSourceNode.prototype.start = function (...args) {
    start.apply(this, args);
    tap.starts++;
    tap.loops.push(this.loop);
  };
}

/**
 * Wait for the status line to change after an action.
 * @param {import("playwright").Page} page - Running editor.
 * @param {() => Promise<void>} action - Action expected to report a new status.
 * @returns {Promise<string>} New status text.
 */
async function statusAfter(page, action) {
  const before = await page.locator("#status").textContent();
  await action();
  await page.waitForFunction(previous => document.querySelector("#status").textContent !== previous, before);
  return page.locator("#status").textContent();
}

/**
 * Type a draft into the labelled controls and submit it with Enter.
 * @param {import("playwright").Page} page - Running editor.
 * @param {{waveform: string, startHz: number, endHz: number, durationMS: number, attackMS: number, releaseMS: number}} draft - Control values.
 * @returns {Promise<string>} Status reported for the submission.
 */
async function apply(page, draft) {
  // A select's accessible label includes its option text, so address it by id.
  await page.locator("#waveform").selectOption(draft.waveform);
  await page.locator("#start-hz").fill(String(draft.startHz));
  await page.locator("#end-hz").fill(String(draft.endHz));
  await page.locator("#duration").fill(String(draft.durationMS));
  await page.locator("#attack").fill(String(draft.attackMS));
  await page.locator("#release").fill(String(draft.releaseMS));
  return statusAfter(page, () => page.locator("#release").press("Enter"));
}

/**
 * Press Download WAV from the keyboard and read the file the browser receives.
 * @param {import("playwright").Page} page - Running editor with downloads enabled.
 * @returns {Promise<{name: string, bytes: Buffer, ms: number}>} Download name, exact bytes, and handoff time.
 */
async function download(page) {
  const start = performance.now();
  const [event] = await Promise.all([page.waitForEvent("download"), page.locator("#download").press("Enter")]);
  const ms = Math.round((performance.now() - start) * 10) / 10;
  const bytes = await readFile(await event.path());
  await page.locator("#status", { hasText: `Downloaded sound.wav, ${bytes.length} bytes.` }).waitFor();
  return { name: event.suggestedFilename(), bytes, ms };
}

/**
 * Check a canonical mono 48 kHz PCM16 WAV and return its samples.
 * @param {Buffer} wav - Downloaded file.
 * @returns {Int16Array} Signed samples from the data chunk.
 */
function decode(wav) {
  assert.equal(wav.toString("ascii", 0, 4), "RIFF");
  assert.equal(wav.readUInt32LE(4), wav.length - 8);
  assert.equal(wav.toString("ascii", 8, 16), "WAVEfmt ");
  assert.equal(wav.readUInt32LE(16), 16);
  assert.equal(wav.readUInt16LE(20), 1, "PCM");
  assert.equal(wav.readUInt16LE(22), 1, "mono");
  assert.equal(wav.readUInt32LE(24), 48000);
  assert.equal(wav.readUInt32LE(28), 96000);
  assert.equal(wav.readUInt16LE(32), 2);
  assert.equal(wav.readUInt16LE(34), 16);
  assert.equal(wav.toString("ascii", 36, 40), "data");
  assert.equal(wav.readUInt32LE(40), wav.length - 44);
  const samples = new Int16Array(wav.readUInt32LE(40) / 2);
  for (let i = 0; i < samples.length; i++) samples[i] = wav.readInt16LE(44 + 2 * i);
  return samples;
}

/**
 * Edit, preview, reject, and export effects on a page with Web Audio.
 * @param {import("playwright").Browser} browser - Managed headless Chromium.
 * @param {string} url - Served Sound Maker.
 * @returns {Promise<{default_wav: Buffer, square_wav: Buffer, apply_ms: number, download_ms: number, play_ms: number}>} Exports and timings.
 */
async function edit(browser, url) {
  const page = await browser.newPage({ acceptDownloads: true });
  page.setDefaultTimeout(8_000);
  try {
    await page.addInitScript(tapAudio);
    await page.goto(url);
    await page.locator("#status", { hasText: "Ready." }).waitFor();
    const status = page.locator("#status");
    const summary = page.locator("#summary");
    assert.equal(await summary.textContent(), "sine, 880 to 440 Hz, 150 ms, attack 5 ms, release 60 ms");

    // A constant 480 Hz square has exactly 100 samples per cycle at 48 kHz.
    const square = { waveform: "square", startHz: 480, endHz: 480, durationMS: 100, attackMS: 20, releaseMS: 30 };
    let start = performance.now();
    assert.equal(await apply(page, square), "Applied: 4800 samples, 100 ms.");
    const apply_ms = Math.round((performance.now() - start) * 10) / 10;
    assert.equal(await status.getAttribute("data-error"), null);
    const squareSummary = "square, 480 to 480 Hz, 100 ms, attack 20 ms, release 30 ms";
    assert.equal(await summary.textContent(), squareSummary);

    const first = await download(page);
    const second = await download(page);
    assert.equal(first.name, "sound.wav");
    assert.equal(second.name, "sound.wav");
    assert.ok(first.bytes.equals(second.bytes), "repeated downloads are byte-identical");
    const samples = decode(first.bytes);
    assert.equal(samples.length, 4800);
    assert.equal(first.bytes.readUInt32LE(40), 9600);
    assert.equal(samples[0], 0);
    assert.equal(samples.at(-1), 0);
    // Sustain runs from the end of the 960-sample attack to the 1440-sample release.
    const sustain = samples.subarray(960, 4800 - 1440);
    assert.ok(sustain.every(sample => Math.abs(sample) === peak), "square sustain holds full amplitude");
    assert.equal(samples[1010], peak);
    assert.equal(samples[1060], -peak);
    assert.equal(Math.abs(samples[480]), 13107, "attack is linear");
    assert.ok(samples.every(sample => Math.abs(sample) <= peak));

    start = performance.now();
    await page.locator("#play").press("Space");
    await status.filter({ hasText: "Preview finished." }).waitFor();
    const play_ms = Math.round((performance.now() - start) * 10) / 10;
    const probe = await page.evaluate(() => window.soundMakerProbe);
    assert.equal(probe.starts, 1);
    assert.equal(probe.previews.length, 1);
    assert.deepEqual(probe.previews[0], Array.from(samples, sample => sample / 32768), "preview plays the exported samples");

    // Rejected drafts keep the typed values but leave the sound and export alone.
    const misfit = { ...square, attackMS: 100, releaseMS: 51 };
    assert.match(await apply(page, misfit), /^Not applied: \S/);
    assert.equal(await status.getAttribute("data-error"), "");
    assert.equal(await summary.textContent(), squareSummary);
    assert.equal(await page.getByLabel("Attack (ms)", { exact: true }).inputValue(), "100");
    assert.equal(await page.getByLabel("Release (ms)", { exact: true }).inputValue(), "51");
    assert.match(await apply(page, { ...square, startHz: 5 }), /^Not applied: \S/);
    assert.equal(await status.getAttribute("data-error"), "");
    assert.equal(await summary.textContent(), squareSummary);
    assert.equal(await page.getByLabel("Start pitch (Hz)", { exact: true }).inputValue(), "5");
    assert.ok((await download(page)).bytes.equals(first.bytes), "rejected drafts keep the exported sound");

    const glide = { waveform: "sine", startHz: 880, endHz: 440, durationMS: 150, attackMS: 5, releaseMS: 60 };
    assert.equal(await apply(page, glide), "Applied: 7200 samples, 150 ms.");
    assert.equal(await summary.textContent(), "sine, 880 to 440 Hz, 150 ms, attack 5 ms, release 60 ms");
    const sine = await download(page);
    assert.ok(!sine.bytes.equals(first.bytes));
    const wave = decode(sine.bytes);
    assert.equal(wave.length, 7200);
    assert.equal(wave[0], 0);
    assert.equal(wave.at(-1), 0);
    // A mean pitch of 660 Hz over 0.15 s is 99 cycles, so about 198 sign changes.
    const signs = Array.from(wave, Math.sign).filter(Boolean);
    const crossings = signs.filter((sign, i) => i > 0 && sign !== signs[i - 1]).length;
    assert.ok(crossings >= 185 && crossings <= 210, `${crossings} zero crossings`);
    return { default_wav: sine.bytes, square_wav: first.bytes, apply_ms, download_ms: first.ms, play_ms };
  } finally {
    await page.close();
  }
}

/**
 * Verify a missing Web Audio API only disables the preview.
 * @param {import("playwright").Browser} browser - Managed headless Chromium.
 * @param {string} url - Served Sound Maker.
 * @param {Buffer} expected - Export of the default effect.
 * @returns {Promise<void>} Resolves when the fallback checks pass.
 */
async function withoutAudio(browser, url, expected) {
  const page = await browser.newPage({ acceptDownloads: true });
  page.setDefaultTimeout(8_000);
  try {
    await page.addInitScript(() => Object.defineProperty(window, "AudioContext", { value: undefined }));
    await page.goto(url);
    await page.locator("#status", { hasText: "Ready." }).waitFor();
    assert.match(await statusAfter(page, () => page.locator("#play").press("Enter")), /^Preview unavailable: \S/);
    assert.equal(await page.locator("#status").getAttribute("data-error"), "");
    assert.ok((await download(page)).bytes.equals(expected), "default export is deterministic across pages");
  } finally {
    await page.close();
  }
}

/**
 * Edit, reject, play, and export a score in the Music panel.
 * @param {import("playwright").Browser} browser - Managed headless Chromium.
 * @param {string} url - Served Sound Maker.
 * @returns {Promise<void>} Resolves when the Music panel checks pass.
 */
async function music(browser, url) {
  const page = await browser.newPage({ acceptDownloads: true });
  page.setDefaultTimeout(8_000);
  try {
    await page.addInitScript(tapAudio);
    await page.goto(url);
    await page.locator("#status", { hasText: "Ready." }).waitFor();
    const summary = page.locator("#score-summary");
    assert.equal(await summary.textContent(), "120 BPM, 64 steps, 2 channels, 32 notes, 8.00 s loop");
    const editor = page.getByLabel("Score (JSON)");
    const original = await editor.inputValue();

    // At 150 BPM a step is 4800 frames, so 64 steps loop every 307,200 samples.
    await editor.fill(original.replace('"tempo": 120', '"tempo": 150'));
    assert.equal(await statusAfter(page, () => page.locator("#apply-score").press("Enter")), "Score applied: 307200 samples per loop.");
    const applied = await editor.inputValue();
    assert.equal(applied, original.replace('"tempo": 120', '"tempo": 150'), "the editor shows the canonical score");
    assert.equal(await summary.textContent(), "150 BPM, 64 steps, 2 channels, 32 notes, 6.40 s loop");

    await editor.fill(applied.replace('"pitch": 45', '"pitch": 200'));
    assert.match(await statusAfter(page, () => page.locator("#apply-score").press("Enter")), /^Score not applied: .*pitch/);
    assert.equal(await page.locator("#status").getAttribute("data-error"), "");
    assert.equal(await summary.textContent(), "150 BPM, 64 steps, 2 channels, 32 notes, 6.40 s loop");

    const [event] = await Promise.all([page.waitForEvent("download"), page.locator("#download-score").press("Enter")]);
    assert.equal(event.suggestedFilename(), "score.json");
    const exported = await readFile(await event.path(), "utf8");
    assert.equal(exported, applied, "a rejected score keeps the exported one");
    assert.equal(JSON.parse(exported).tempo, 150);

    assert.equal(await statusAfter(page, () => page.locator("#play-music").press("Enter")), "Playing score loop.");
    const probe = await page.evaluate(() => {
      const { previews, loops } = window.soundMakerProbe;
      const pcm = globalThis.gojeSoundMaker.scorePCM();
      const view = new DataView(pcm.buffer);
      const rendered = Array.from({ length: pcm.length / 2 }, (_, i) => view.getInt16(2 * i, true) / 32768);
      const preview = previews.at(-1);
      return { loops, same: preview.length === rendered.length && preview.every((v, i) => v === rendered[i]) };
    });
    assert.deepEqual(probe.loops, [true]);
    assert.ok(probe.same, "the loop plays the rendered score");
    assert.equal(await statusAfter(page, () => page.locator("#stop-music").press("Enter")), "Score loop stopped.");
  } finally {
    await page.close();
  }
}

const [output, ...extra] = process.argv.slice(2);
if (extra.length) throw new Error("Usage: node scripts/smoke-sound-maker.mjs [EXPORT_PATH]");

const server = await serveSoundMaker(0);
try {
  const browser = await chromium.launch({ timeout: 15_000 });
  try {
    const url = `http://127.0.0.1:${server.address().port}/`;
    const { default_wav, square_wav, ...timings } = await edit(browser, url);
    // The caller owns the path; refuse to replace a file that is already there.
    if (output) await writeFile(output, square_wav, { flag: "wx" });
    await withoutAudio(browser, url, default_wav);
    await music(browser, url);
    console.log(`Sound Maker smoke passed: keyboard apply ${timings.apply_ms} ms, download ${timings.download_ms} ms, preview ${timings.play_ms} ms; rejection, determinism, no-Web-Audio fallback, and a score edited, rejected, exported, and looped`);
    console.log(JSON.stringify({ benchmark: "goje-sound-maker", chromium_version: browser.version(), timings }));
  } finally {
    await browser.close();
  }
} finally {
  await promisify(server.close.bind(server))();
}
