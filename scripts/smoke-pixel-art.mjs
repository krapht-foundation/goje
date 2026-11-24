/** @file Verify the Pixel Art Maker's visible editing and PNG download in Chromium. */

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { promisify } from "node:util";

import { chromium } from "playwright";
import { PNG } from "pngjs";

import { servePixelArt } from "./pixel-art-server.mjs";

const blue = [56, 107, 176, 255];
const accent = [222, 122, 66, 255];
const clear = [0, 0, 0, 0];

/**
 * Inspect rendered canvas pixels without writing app state.
 * @param {import("playwright").Page} page - Running editor.
 * @param {string} selector - Canvas selector.
 * @param {number} x - Pixel column.
 * @param {number} y - Pixel row.
 * @returns {Promise<number[]>} RGBA channels.
 */
function pixel(page, selector, x, y) {
  return page.locator(selector).evaluate((canvas, [column, row]) =>
    Array.from(canvas.getContext("2d").getImageData(column, row, 1, 1).data), [x, y]);
}

/**
 * Read a whole canvas backing store without writing app state.
 * @param {import("playwright").Page} page - Running editor.
 * @param {string} selector - Canvas selector.
 * @returns {Promise<Buffer>} Packed RGBA rows.
 */
async function canvasData(page, selector) {
  return Buffer.from(await page.locator(selector).evaluate(canvas =>
    Array.from(canvas.getContext("2d").getImageData(0, 0, canvas.width, canvas.height).data)));
}

/**
 * Measure from an edit action until the 1× preview shows the edited pixel.
 * @param {import("playwright").Page} page - Running editor.
 * @param {() => Promise<void>} action - Real input that edits one pixel.
 * @param {number} x - Edited pixel column.
 * @param {number} y - Edited pixel row.
 * @param {number[]} rgba - Expected preview channels.
 * @returns {Promise<number>} Elapsed milliseconds, including input dispatch.
 */
async function previewed(page, action, x, y, rgba) {
  const start = performance.now();
  await action();
  await page.waitForFunction(([column, row, want]) => {
    const data = document.querySelector("#native").getContext("2d").getImageData(column, row, 1, 1).data;
    return want.every((value, i) => data[i] === value);
  }, [x, y, rgba], { polling: "raf" });
  return Math.round((performance.now() - start) * 10) / 10;
}

/**
 * Click Download PNG and measure until the browser receives the download.
 * @param {import("playwright").Page} page - Running editor with downloads enabled.
 * @returns {Promise<{name: string, bytes: Buffer, ms: number}>} Download name, exact bytes, and handoff time.
 */
async function download(page) {
  const start = performance.now();
  const [event] = await Promise.all([page.waitForEvent("download"), page.getByRole("button", { name: "Download PNG", exact: true }).click()]);
  const ms = Math.round((performance.now() - start) * 10) / 10;
  return { name: event.suggestedFilename(), bytes: await readFile(await event.path()), ms };
}

/**
 * Select a file through the editor's file input.
 * @param {import("playwright").Page} page - Running editor.
 * @param {string} name - Displayed file name.
 * @param {Buffer} buffer - File bytes.
 * @returns {Promise<void>} Resolves when the input accepts the file.
 */
function choose(page, name, buffer) {
  return page.locator("#file").setInputFiles({ name, mimeType: "image/png", buffer });
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
 * Create a blank sheet through the labeled size inputs and New sheet button.
 * @param {import("playwright").Page} page - Running editor without unsaved changes.
 * @param {number} width - Sheet width.
 * @param {number} height - Sheet height.
 * @returns {Promise<void>} Resolves when the editor reports the new sheet.
 */
async function newSheet(page, width, height) {
  await page.locator("#width").fill(String(width));
  await page.locator("#height").fill(String(height));
  const status = await statusAfter(page, () => page.getByRole("button", { name: "New sheet", exact: true }).click());
  assert.match(status, /Created/);
  assert.match(status, new RegExp(`${width}\\D+${height}`));
  assert.equal(await page.locator("#dirty").textContent(), "Saved");
  for (const selector of ["#art", "#native"]) {
    assert.deepEqual(await page.locator(selector).evaluate(canvas => [canvas.width, canvas.height]), [width, height]);
  }
  assert.equal(await page.locator("#undo").isDisabled(), true);
  assert.equal(await page.locator("#redo").isDisabled(), true);
}

/**
 * Return a mouse mover for cell centers, or coordinates outside the sheet, on the grid.
 * @param {import("playwright").Page} page - Running editor.
 * @param {number} width - Sheet width.
 * @param {number} height - Sheet height.
 * @returns {Promise<(x: number, y: number) => Promise<void>>} Single-sample mouse move.
 */
async function pointer(page, width, height) {
  const box = await page.locator("#grid").boundingBox();
  assert.ok(box);
  return (x, y) => page.mouse.move(box.x + box.width * (x + 0.5) / width, box.y + box.height * (y + 0.5) / height);
}

/**
 * Author a small non-square sheet and check history, protection, download, and reopen.
 * @param {import("playwright").Page} page - Isolated test page with downloads enabled.
 * @param {string[]} unexpected - Messages of dialogs the editor should not have shown.
 * @param {{accept: boolean, resolve: (message: string) => void}[]} answers - Pending dialog answers.
 * @returns {Promise<{sheet: string, edit_to_preview_ms: number, download_ms: number}>} Workflow timings.
 */
async function small(page, unexpected, answers) {
  const width = 24;
  const height = 12;
  const button = /** @param {string} id - Button element id. */ id => page.locator(`#${id}`);
  const grid = page.locator("#grid");
  const dirty = page.locator("#dirty");
  /**
   * Answer the next replacement prompt.
   * @param {boolean} accept - Whether to discard unsaved changes.
   * @returns {Promise<string>} Prompt text.
   */
  const prompt = accept => new Promise(resolve => answers.push({ accept, resolve }));

  await newSheet(page, width, height);
  assert.ok((await canvasData(page, "#art")).every(value => value === 0), "new sheet is transparent");
  const gridBox = await grid.boundingBox();
  assert.ok(gridBox);
  assert.ok(Math.abs(gridBox.width / gridBox.height - width / height) < 0.05, "grid keeps the sheet aspect");
  assert.equal(await page.locator("#native").isVisible(), true);
  assert.deepEqual(await page.locator("#native").evaluate(canvas => [canvas.clientWidth, canvas.clientHeight]), [width, height]);

  // Keyboard: paint (2, 1) and (3, 1), then erase (2, 1) to transparent.
  await button("pencil").click();
  await page.locator("#color").fill("#386bb0");
  await grid.focus();
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("ArrowDown");
  assert.match(await page.locator("#coordinate").textContent(), /2\D+1/);
  const edit_to_preview_ms = await previewed(page, () => page.keyboard.press("Enter"), 2, 1, blue);
  assert.deepEqual(await pixel(page, "#art", 2, 1), blue);
  assert.equal(await dirty.textContent(), "Unsaved changes");
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("Enter");
  await button("eraser").click();
  await grid.focus();
  await page.keyboard.press("ArrowLeft");
  await page.keyboard.press("Enter");
  assert.deepEqual(await pixel(page, "#art", 2, 1), clear);
  assert.deepEqual(await pixel(page, "#art", 3, 1), blue);

  // Pointer: each move is one sample, so painted gaps prove interpolation.
  await button("pencil").click();
  await page.locator("#color").fill("#de7a42");
  const move = await pointer(page, width, height);
  await move(3, 5);
  await page.mouse.down();
  await move(20, 5);
  await move(20, 9);
  await move(30, 9);
  await move(30, 2);
  await move(22, 2);
  await page.mouse.up();
  assert.deepEqual(await pixel(page, "#art", 12, 5), accent, "stroke interpolates inside the grid");
  assert.deepEqual(await pixel(page, "#art", 20, 7), accent);
  assert.deepEqual(await pixel(page, "#art", 23, 9), clear, "leaving the grid breaks interpolation");
  assert.deepEqual(await pixel(page, "#art", 22, 3), clear, "re-entering does not join the outside path");
  assert.deepEqual(await pixel(page, "#art", 22, 2), accent);
  // Escape must roll back the in-flight stroke instead of adding undo history.
  await move(2, 10);
  await page.mouse.down();
  await move(8, 10);
  await page.keyboard.press("Escape");
  await page.mouse.up();
  assert.deepEqual(await pixel(page, "#art", 5, 10), clear, "cancel rolls back the whole stroke");
  await button("eraser").click();
  await move(10, 5);
  await page.mouse.down();
  await move(14, 5);
  await page.mouse.up();
  assert.deepEqual(await pixel(page, "#art", 12, 5), clear, "pointer eraser clears to transparent");

  await button("undo").click();
  assert.deepEqual(await pixel(page, "#art", 12, 5), accent, "undo restores the whole erase stroke");
  assert.deepEqual(await pixel(page, "#art", 10, 5), accent);
  await button("redo").click();
  assert.deepEqual(await pixel(page, "#art", 12, 5), clear);
  assert.deepEqual(await canvasData(page, "#native"), await canvasData(page, "#art"), "1× preview matches the art");

  const expected = new PNG({ width, height });
  expected.data.set(blue, (1 * width + 3) * 4);
  for (let x = 3; x <= 20; x++) if (x < 10 || x > 14) expected.data.set(accent, (5 * width + x) * 4);
  for (let y = 6; y <= 9; y++) expected.data.set(accent, (y * width + 20) * 4);
  expected.data.set(accent, (2 * width + 22) * 4);
  assert.deepEqual(await canvasData(page, "#art"), expected.data);

  // Rejected imports neither prompt nor replace the dirty sheet.
  const tooWide = PNG.sync.write(new PNG({ width: 513, height: 1 }));
  assert.doesNotMatch(await statusAfter(page, () => choose(page, "bad.png", Buffer.from("not a PNG"))), /Opened/);
  assert.match(await statusAfter(page, () => choose(page, "huge.png", Buffer.alloc(1_048_577))), /bytes/);
  assert.doesNotMatch(await statusAfter(page, () => choose(page, "wide.png", tooWide)), /Opened/);
  assert.deepEqual(unexpected, [], "rejected imports do not prompt");
  assert.deepEqual(await canvasData(page, "#art"), expected.data, "rejected imports keep the artwork");
  assert.equal(await dirty.textContent(), "Unsaved changes");

  // Dismissed valid replacements keep artwork and history.
  const source = new PNG({ width: 5, height: 3 });
  source.data.set([255, 0, 0, 127], (2 * 5 + 1) * 4);
  const fixture = PNG.sync.write(source);
  const [openPrompt] = await Promise.all([prompt(false), choose(page, "source.png", fixture)]);
  assert.match(openPrompt, /Discard unsaved changes/);
  const [newPrompt] = await Promise.all([prompt(false), button("new").click()]);
  assert.match(newPrompt, /Discard unsaved changes/);
  assert.deepEqual(await canvasData(page, "#art"), expected.data, "dismissed replacements keep the artwork");
  assert.equal(await dirty.textContent(), "Unsaved changes");
  await button("undo").click();
  assert.deepEqual(await pixel(page, "#art", 12, 5), accent, "history survives dismissed replacements");
  await button("redo").click();

  const saved = await download(page);
  assert.match(saved.name, /\.png$/);
  const result = PNG.sync.read(saved.bytes);
  assert.equal(result.width, width);
  assert.equal(result.height, height);
  assert.deepEqual(result.data, expected.data, "export contains only authored pixels");
  assert.equal(await dirty.textContent(), "Saved");

  // Reopen the exact download; a saved sheet opens without a prompt.
  assert.ok((await statusAfter(page, () => choose(page, saved.name, saved.bytes))).includes(`Opened ${saved.name}`));
  assert.deepEqual(unexpected, []);
  assert.deepEqual(await canvasData(page, "#art"), expected.data, "reopened download has no guide or preview pixels");
  assert.equal(await dirty.textContent(), "Saved");
  assert.equal(await button("undo").isDisabled(), true);

  // An accepted replacement swaps in the imported pixels, alpha included.
  await button("pencil").click();
  await grid.focus();
  await page.keyboard.press("Enter");
  assert.equal(await dirty.textContent(), "Unsaved changes");
  const [acceptPrompt] = await Promise.all([prompt(true), choose(page, "source.png", fixture)]);
  assert.match(acceptPrompt, /Discard unsaved changes/);
  await page.locator("#status", { hasText: "Opened source.png" }).waitFor();
  assert.deepEqual(await page.locator("#art").evaluate(canvas => [canvas.width, canvas.height]), [5, 3]);
  assert.deepEqual(await pixel(page, "#art", 1, 2), [255, 0, 0, 127]);
  assert.equal(await dirty.textContent(), "Saved");
  return { sheet: `${width}x${height}`, edit_to_preview_ms, download_ms: saved.ms };
}

/**
 * Edit and download the largest sheet.
 * @param {import("playwright").Page} page - Isolated test page with downloads enabled.
 * @returns {Promise<{sheet: string, edit_to_preview_ms: number, download_ms: number}>} Workflow timings.
 */
async function large(page) {
  const size = 512;
  await newSheet(page, size, size);
  await page.locator("#pencil").click();
  await page.locator("#color").fill("#de7a42");
  await page.locator("#grid").focus();
  const edit_to_preview_ms = await previewed(page, () => page.keyboard.press("Enter"), 0, 0, accent);
  // Keep the stroke in the grid's top-left corner, which is in view at any zoom.
  await page.locator("#grid").evaluate(grid => grid.scrollIntoView({ block: "start", inline: "start" }));
  const move = await pointer(page, size, size);
  await move(1, 3);
  await page.mouse.down();
  await move(8, 3);
  await page.mouse.up();
  assert.deepEqual(await pixel(page, "#native", 5, 3), accent);

  const saved = await download(page);
  const result = PNG.sync.read(saved.bytes);
  assert.equal(result.width, size);
  assert.equal(result.height, size);
  const at = /** @param {number} x - Column. @param {number} y - Row. */ (x, y) => Array.from(result.data.subarray((y * size + x) * 4, (y * size + x) * 4 + 4));
  assert.deepEqual(at(0, 0), accent);
  assert.deepEqual(at(5, 3), accent);
  assert.deepEqual(at(size - 1, size - 1), clear, "untouched pixels stay transparent");
  return { sheet: `${size}x${size}`, edit_to_preview_ms, download_ms: saved.ms };
}

if (process.argv.length > 2) throw new Error("Usage: node scripts/smoke-pixel-art.mjs");

const server = await servePixelArt(0);
try {
  const browser = await chromium.launch({ timeout: 15_000 });
  try {
    const page = await browser.newPage({ acceptDownloads: true });
    page.setDefaultTimeout(8_000);
    /** @type {string[]} */
    const unexpected = [];
    /** @type {{accept: boolean, resolve: (message: string) => void}[]} */
    const answers = [];
    page.on("dialog", dialog => {
      const answer = answers.shift();
      if (!answer) {
        unexpected.push(dialog.message());
        return dialog.dismiss();
      }
      answer.resolve(dialog.message());
      return answer.accept ? dialog.accept() : dialog.dismiss();
    });
    await page.goto(`http://127.0.0.1:${server.address().port}/`);
    await page.locator("#status", { hasText: "Ready." }).waitFor();
    assert.equal(await page.locator("#dirty").textContent(), "No sheet open");
    const timings = [await small(page, unexpected, answers), await large(page)];
    assert.deepEqual(unexpected, []);
    console.log("Pixel Art Maker smoke passed: keyboard and pointer edits, erase, history, preview, replacement safety, download, and reopen");
    console.log(JSON.stringify({ benchmark: "goje-pixel-art", chromium_version: browser.version(), timings }));
  } finally {
    await browser.close();
  }
} finally {
  await promisify(server.close.bind(server))();
}
