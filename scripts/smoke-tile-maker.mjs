/** @file Verify the Tile Maker's visible editing and PNG download in Chromium. */

import assert from "node:assert/strict";
import { readFile, writeFile } from "node:fs/promises";
import { promisify } from "node:util";

import { chromium } from "playwright";
import { PNG } from "pngjs";

import { serveTileMaker } from "./tile-maker-server.mjs";

const width = 32;
const height = 16;
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
 * Select a PNG with the Open button's file chooser.
 * @param {import("playwright").Page} page - Running editor.
 * @param {string | {name: string, mimeType: string, buffer: Buffer}} source - File path or in-memory fixture.
 * @returns {Promise<void>} Resolves when the chooser accepts the file.
 */
async function openPNG(page, source) {
  const chooser = page.waitForEvent("filechooser");
  await page.getByRole("button", { name: "Open PNG" }).click();
  await (await chooser).setFiles(source);
}

/**
 * Answer a replacement prompt and check the action the prompt describes.
 * @param {import("playwright").Page} page - Running editor.
 * @param {RegExp} expected - Expected prompt text.
 * @param {boolean} accept - Whether to confirm discarding edits.
 * @returns {Promise<void>} Resolves when the dialog has been answered.
 */
async function replacement(page, expected, accept) {
  const dialog = await page.waitForEvent("dialog");
  const text = dialog.message();
  if (accept) await dialog.accept();
  else await dialog.dismiss();
  assert.match(text, expected);
}

/**
 * Interact with the editor and verify the PNG read from the browser's download.
 * @param {import("playwright").Page} page - Isolated test page with downloads enabled.
 * @param {string} url - Loopback Tile Maker address.
 * @param {string | undefined} output - Optional destination for the exact download bytes.
 * @returns {Promise<{stroke_ms: number, export_ms: number}>} Browser workflow timings.
 */
async function author(page, url, output) {
  await page.goto(url);
  await page.getByText("Ready. Draw a tile or open a PNG.", { exact: true }).waitFor();
  const button = /** @param {string} name - Accessible button name. */ name => page.getByRole("button", { name, exact: true });
  const grid = page.getByRole("group", { name: "Tile editing grid" });
  const erased = button("Eraser");
  const pencil = button("Pencil");
  const undo = button("Undo");
  const redo = button("Redo");
  const status = page.locator("#status");
  const dirty = page.locator("#dirty");
  await button("New tile").click();
  assert.equal(await dirty.textContent(), "Saved");
  assert.deepEqual(await pixel(page, "#tile", 1, 3), clear);

  await grid.focus();
  // Keep the keyboard mark away from the playground's transparent-corner probes.
  await page.keyboard.press("ArrowRight");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("Enter");
  assert.deepEqual(await pixel(page, "#tile", 1, 3), blue);
  assert.equal(await page.locator("#coordinate").textContent(), "(1, 3)");

  const box = await grid.boundingBox();
  assert.ok(box);
  /**
   * Move to a cell center or to a coordinate outside the board.
   * @param {number} x - Pixel column.
   * @param {number} y - Pixel row.
   */
  const move = (x, y) => page.mouse.move(box.x + box.width * (x + 0.5) / width, box.y + box.height * (y + 0.5) / height);
  await page.getByLabel("Color", { exact: true }).fill("#de7a42");
  await move(6, 4);
  const strokeStart = performance.now();
  await page.mouse.down();
  // Put an off-center highlight inside the diamond for the playground's pixel probes.
  await move(15, 4);
  await move(15, 3);
  await move(11, 3);
  await move(11, 5);
  await move(15, 5);
  await move(15, 6);
  await move(11, 6);
  await move(11, -1);
  await move(20, -1);
  await move(20, 4);
  await page.mouse.up();
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  const stroke_ms = performance.now() - strokeStart;
  assert.deepEqual(await pixel(page, "#tile", 10, 4), accent, "stroke interpolates inside the grid");
  assert.deepEqual(await pixel(page, "#tile", 17, 4), clear, "stroke does not cross the outside gap");
  assert.deepEqual(await pixel(page, "#tile", 20, 4), accent);
  assert.deepEqual(await pixel(page, "#native", 1, 3), blue);
  for (const [x, y] of [[17, 3], [33, 11], [1, 11], [17, 19]]) {
    assert.deepEqual(await pixel(page, "#repeat", x, y), blue, "repeat uses courtyard offsets");
  }

  await erased.click();
  await grid.focus();
  await page.keyboard.press("Space");
  assert.deepEqual(await pixel(page, "#tile", 20, 4), clear);
  await undo.click();
  assert.deepEqual(await pixel(page, "#tile", 20, 4), accent);
  await redo.click();
  assert.deepEqual(await pixel(page, "#tile", 20, 4), clear);
  await pencil.click();
  await move(25, 7);
  await page.mouse.down();
  await page.keyboard.press("Tab"); // Leaving grid focus interrupts the live stroke.
  await page.mouse.up();
  assert.deepEqual(await pixel(page, "#tile", 25, 7), clear);
  await page.getByLabel("Diamond guide").uncheck();
  assert.equal(await page.locator("#guide").isVisible(), false);
  await page.getByLabel("Diamond guide").check();
  assert.equal(await page.locator("#guide").isVisible(), true);

  const newPrompt = replacement(page, /create a blank tile/, false);
  await Promise.all([newPrompt, button("New tile").click()]);
  const imported = new PNG({ width, height });
  imported.data.set([255, 0, 0, 127], (11 * width + 2) * 4);
  const fixture = { name: "source.png", mimeType: "image/png", buffer: PNG.sync.write(imported) };
  await Promise.all([replacement(page, /open this PNG/, false), openPNG(page, fixture)]);
  await openPNG(page, { ...fixture, buffer: Buffer.from("not a PNG") });
  await page.locator("#status[data-error]").waitFor();
  assert.match(await status.textContent(), /PNG/);
  await openPNG(page, { ...fixture, buffer: Buffer.alloc(64 * 1024 + 1) });
  await page.getByText("PNG exceeds 65536 bytes.", { exact: true }).waitFor();
  assert.deepEqual(await pixel(page, "#tile", 1, 3), blue);
  assert.equal(await dirty.textContent(), "Unsaved changes");
  // Undo still restores the erased pixel after canceled and invalid replacements.
  await undo.click();
  assert.deepEqual(await pixel(page, "#tile", 20, 4), accent);
  await redo.click();

  const exportStart = performance.now();
  const [download] = await Promise.all([page.waitForEvent("download"), button("Export PNG").click()]);
  const export_ms = performance.now() - exportStart;
  const path = await download.path();
  assert.equal(download.suggestedFilename(), "tile.png");
  const bytes = await readFile(path);
  const result = PNG.sync.read(bytes);
  const expected = new PNG({ width, height });
  expected.data.set(blue, (3 * width + 1) * 4);
  for (let y = 3; y <= 6; y++) {
    for (let x = y === 4 ? 6 : 11; x <= 15; x++) expected.data.set(accent, (y * width + x) * 4);
  }
  assert.equal(result.width, width);
  assert.equal(result.height, height);
  assert.deepEqual(result.data, expected.data, "export contains only authored pixels, with no overlays");
  assert.equal(await dirty.textContent(), "Saved");

  await openPNG(page, { name: download.suggestedFilename(), mimeType: "image/png", buffer: bytes });
  await page.getByText("Opened tile.png.", { exact: true }).waitFor();
  assert.deepEqual(await pixel(page, "#tile", 10, 4), accent);
  assert.equal(await undo.isDisabled(), true);
  await grid.focus();
  await page.keyboard.press("Enter");
  assert.equal(await dirty.textContent(), "Unsaved changes");
  await undo.click();
  assert.equal(await dirty.textContent(), "Saved");
  await openPNG(page, fixture);
  await page.getByText("Opened source.png.", { exact: true }).waitFor();
  assert.deepEqual(await pixel(page, "#tile", 2, 11), [255, 0, 0, 127]);
  await grid.focus();
  await page.keyboard.press("Enter");
  await Promise.all([replacement(page, /create a blank tile/, true), button("New tile").click()]);
  assert.equal(await dirty.textContent(), "Saved");
  assert.deepEqual(await pixel(page, "#tile", 2, 11), clear);
  if (output) await writeFile(output, bytes, { flag: "wx" });
  return { stroke_ms: Math.round(stroke_ms), export_ms: Math.round(export_ms) };
}

const output = process.argv[2];
if (process.argv.length > 3) throw new Error("Usage: node scripts/smoke-tile-maker.mjs [EXPORT_PATH]");

const server = await serveTileMaker(0);
try {
  const browser = await chromium.launch({ timeout: 15_000 });
  try {
    const page = await browser.newPage({ acceptDownloads: true });
    page.setDefaultTimeout(8_000);
    const timings = await author(page, `http://127.0.0.1:${server.address().port}/`, output);
    console.log("Tile Maker smoke passed: keyboard and pointer edits, previews, history, replacement safety, download, and reopen");
    console.log(JSON.stringify({ benchmark: "goje-tile-maker", tile: "32x16", chromium_version: browser.version(), ...timings }));
  } finally {
    await browser.close();
  }
} finally {
  await promisify(server.close.bind(server))();
}
