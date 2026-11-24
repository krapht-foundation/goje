/** @file Exercise sprite editing, fixed-tick preview, and the exact downloaded sidecar in Chromium. */

import assert from "node:assert/strict";
import { readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { chromium } from "playwright";
import { PNG } from "pngjs";

import { serveSpriteEditor } from "./sprite-editor-server.mjs";

const pngPath = fileURLToPath(new URL("../sprite/testdata/tomato.png", import.meta.url));
const jsonPath = fileURLToPath(new URL("../sprite/testdata/tomato.json", import.meta.url));
const tomato = PNG.sync.read(await readFile(pngPath));
const original = JSON.parse(await readFile(jsonPath, "utf8"));

/**
 * Find visible tomato body bounds, excluding the outline, eyes, and preview guides.
 * @param {Uint8Array | number[]} rgba - Straight RGBA pixels.
 * @param {number} stride - Pixel width of the whole image.
 * @param {{x: number, y: number, width: number, height: number}} region - Source area to inspect.
 * @returns {{left: number, right: number, top: number, bottom: number}} Region-relative exclusive bounds.
 */
function bodyBounds(rgba, stride, { x, y, width, height }) {
  let left = width, right = 0, top = height, bottom = 0;
  for (let row = 0; row < height; row++) {
    for (let col = 0; col < width; col++) {
      const offset = ((y + row) * stride + x + col) * 4;
      if (rgba[offset] !== 215 || rgba[offset + 1] !== 84 || rgba[offset + 2] !== 73 || rgba[offset + 3] !== 255) continue;
      left = Math.min(col, left);
      right = Math.max(col + 1, right);
      top = Math.min(row, top);
      bottom = Math.max(row + 1, bottom);
    }
  }
  assert.ok(right > left && bottom > top, "tomato artwork is visible");
  return { left, right, top, bottom };
}

/**
 * Compare the visible pose with the source PNG relative to the same canvas center.
 * @param {import("playwright").Page} page - Editor page with playback paused.
 * @param {{x: number, y: number, width: number, height: number, pivotX: number, pivotY: number}} frame - Expected pose.
 * @returns {Promise<void>} Resolves after checking nearest-neighbor scale and the pivot position.
 */
async function anchoredPose(page, frame) {
  const { width, height, rgba } = await page.locator("#preview").evaluate(canvas => ({
    width: canvas.width,
    height: canvas.height,
    rgba: Array.from(canvas.getContext("2d").getImageData(0, 0, canvas.width, canvas.height).data),
  }));
  const observed = bodyBounds(rgba, width, { x: 0, y: 0, width, height });
  const source = bodyBounds(tomato.data, tomato.width, frame);
  const scale = (observed.right - observed.left) / (source.right - source.left);
  assert.ok(Number.isInteger(scale) && scale > 0, "small pixel art is magnified at integer scale");
  assert.deepEqual(observed, {
    left: width / 2 + (source.left - frame.pivotX) * scale,
    right: width / 2 + (source.right - frame.pivotX) * scale,
    top: height / 2 + (source.top - frame.pivotY) * scale,
    bottom: height / 2 + (source.bottom - frame.pivotY) * scale,
  });
}

/**
 * Test import, editing, preview, undo safety, and the downloaded asset.
 * @param {import("playwright").Page} page - Isolated Chromium page.
 * @param {string} url - Loopback address of the editor.
 * @returns {Promise<Buffer>} Exact browser download, for subsequent game smoke coverage.
 */
async function author(page, url) {
  await page.goto(url);
  const pngInput = page.locator("#png-file");
  const jsonInput = page.locator("#json-file");
  const open = page.locator("#open");
  const clip = page.locator("#clip");
  const frame = page.locator("#frame");
  const ticks = page.locator("#ticks");
  const dirty = page.locator("#dirty");
  const apply = page.locator("#apply-frame");
  const previewFrame = page.locator("#preview-frame");
  const status = page.locator("#status");
  await status.filter({ hasText: "Ready." }).waitFor();
  await pngInput.setInputFiles(pngPath);
  await jsonInput.setInputFiles(jsonPath);
  await open.click();
  await status.filter({ hasText: "Opened tomato.png and tomato.json" }).waitFor();

  await clip.selectOption("1");
  await frame.selectOption("1");
  // Commit the actual hop duration and reorder it using keyboard input only.
  assert.equal(await ticks.inputValue(), "8");
  for (let i = 0; i < 8; i++) await ticks.press("ArrowUp");
  await apply.press("Enter");
  await page.locator("#move-up").press("Enter");
  assert.equal(await dirty.textContent(), "Unsaved changes");
  assert.equal(await ticks.inputValue(), "16");
  await page.locator("#undo").click();
  await frame.selectOption("0");
  assert.equal(await ticks.inputValue(), "4", "undo restored the first frame");
  await page.locator("#redo").click();
  await frame.selectOption("0");
  assert.equal(await ticks.inputValue(), "16", "redo restored the reordered first frame");

  const [windup, hop, landing] = original.clips[1].frames;
  assert.match(await previewFrame.textContent(), /\bFrame 1\b/);
  await anchoredPose(page, hop);
  for (let i = 0; i < 15; i++) await page.locator("#step").press("Enter");
  assert.match(await previewFrame.textContent(), /\bFrame 1\b/, "the hop holds until the edited boundary");
  await page.locator("#step").press("Enter");
  assert.match(await previewFrame.textContent(), /\bFrame 2\b/, "one tick crosses the new boundary");
  await anchoredPose(page, windup);
  await page.clock.install();
  await page.clock.pauseAt(await page.evaluate(() => Date.now() + 1000));
  await page.clock.runFor(250);
  assert.match(await previewFrame.textContent(), /\bFrame 2\b/, "paused time does not advance playback");
  await page.locator("#play").press("Enter");
  await page.clock.runFor(100);
  await page.locator("#play").press("Enter");
  assert.match(await previewFrame.textContent(), /\bFrame 3\b/, "playing consumes fixed ticks");
  await anchoredPose(page, landing);

  // No discard prompt should appear for a sidecar rejected at the file boundary.
  await jsonInput.setInputFiles({ name: "oversized.json", mimeType: "application/json", buffer: Buffer.alloc(65_537) });
  await open.click();
  await status.filter({ hasText: /oversized\.json exceeds/ }).waitFor();
  assert.equal(await dirty.textContent(), "Unsaved changes");
  await jsonInput.setInputFiles(jsonPath);
  const replacement = page.waitForEvent("dialog").then(async dialog => {
    assert.match(dialog.message(), /unsaved|discard/i);
    await dialog.dismiss();
  });
  await Promise.all([replacement, open.click()]);
  assert.equal(await dirty.textContent(), "Unsaved changes");
  assert.equal(await ticks.inputValue(), "16");

  const downloaded = page.waitForEvent("download");
  await page.locator("#export").click();
  const download = await downloaded;
  assert.match(download.suggestedFilename(), /\.json$/);
  const data = await readFile(await download.path());
  const expected = { ...original, clips: [original.clips[0], { name: "move", frames: [{ ...hop, ticks: 16 }, windup, landing] }] };
  assert.deepEqual(JSON.parse(data.toString("utf8")), expected);
  assert.equal(await dirty.textContent(), "Saved");
  await jsonInput.setInputFiles({ name: "download.json", mimeType: "application/json", buffer: data });
  await open.click();
  await status.filter({ hasText: "Opened tomato.png and download.json" }).waitFor();
  await clip.selectOption("1");
  assert.equal(await ticks.inputValue(), "16");
  await anchoredPose(page, hop);

  // A PNG-only open can author a new clip from a dragged, inclusive pixel selection.
  await jsonInput.setInputFiles([]);
  await open.click();
  await status.filter({ hasText: "Opened tomato.png (98 × 24, 0 clips)" }).waitFor();
  await page.locator("#clip-name").fill("probe");
  const box = await page.locator("#sheet").boundingBox();
  const dx = box.width / tomato.width, dy = box.height / tomato.height;
  await page.mouse.move(box.x + 2.5 * dx, box.y + 1.5 * dy);
  await page.mouse.down();
  await page.mouse.move(box.x + 7.5 * dx, box.y + 5.5 * dy);
  await page.mouse.up();
  for (const [field, value] of [["x", "2"], ["y", "1"], ["width", "6"], ["height", "5"]]) {
    assert.equal(await page.locator(`#${field}`).inputValue(), value);
  }
  await page.locator("#add-clip").click();
  await clip.locator("option", { hasText: "probe" }).waitFor({ state: "attached" });
  assert.equal(await dirty.textContent(), "Unsaved changes");
  return data;
}

const output = process.argv[2];
if (process.argv.length > 3) throw new Error("Usage: node scripts/smoke-sprite-editor.mjs [EXPORT_PATH]");

const server = await serveSpriteEditor(0);
try {
  const browser = await chromium.launch({ timeout: 15_000 });
  try {
    const page = await browser.newPage({ acceptDownloads: true });
    page.setDefaultTimeout(8_000);
    const data = await author(page, `http://127.0.0.1:${server.address().port}/`);
    if (output) await writeFile(output, data, { flag: "wx" });
    console.log("Sprite Clip Editor smoke passed: keyboard and pointer edits, history, stable anchor, tick boundary, replacement safety, download, and reopen");
  } finally {
    await browser.close();
  }
} finally {
  await promisify(server.close.bind(server))();
}
