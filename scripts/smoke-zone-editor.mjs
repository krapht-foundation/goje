/**
 * @file Drive the Level Builder's zone tool in Chromium with real file choosers, pointer, and keyboard input.
 *
 * It opens the courtyard terrain, authors the yard's deck and doorway, exercises editing, deletion, undo,
 * redo, and rejected input, then exports a version 2 level and reopens it.
 *
 * Usage: node scripts/smoke-zone-editor.mjs [EXPORT_PATH]
 * EXPORT_PATH optionally keeps the download, so the playground can be built with it.
 */

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { chromium } from "playwright";
import { PNG } from "pngjs";

import { serveLevelEditor } from "./level-editor-server.mjs";

const cell = 24;
const courtyardPath = fileURLToPath(new URL("../cmd/playground/levels/courtyard.json", import.meta.url));
const modifier = "ControlOrMeta";

/**
 * @typedef {object} Zone
 * @property {number} id - Zone ID.
 * @property {string} kind - Game-defined kind.
 * @property {string} role - "solid" or "trigger".
 * @property {number} x - First column.
 * @property {number} y - First row.
 * @property {number} w - Width in cells.
 * @property {number} h - Height in cells.
 * @property {number} bottom - Lowest height.
 * @property {number} top - Height above the extent.
 */

/** The zones that rebuild the yard: a doorway trigger in the north gap and a deck over the south. */
const door = { id: 2, kind: "door", role: "trigger", x: 4, y: 0, w: 1, h: 1, bottom: 0, top: 32 };
const deck = { id: 1, kind: "deck", role: "solid", x: 3, y: 5, w: 3, h: 1, bottom: 24, top: 32 };

/**
 * Read the observable editor view. The script only reads state; all changes come from input.
 * @param {import("playwright").Page} page - Editor page.
 * @returns {Promise<{zones: Zone[], dirty: boolean, canUndo: boolean, status: string, error: boolean, cursorText: string}>} View.
 */
async function view(page) {
  return page.evaluate(() => {
    const status = document.querySelector("#status");
    const state = JSON.parse(JSON.stringify(globalThis.gojeLevelEditor.state()));
    return {
      zones: state.zones, dirty: state.dirty, canUndo: state.canUndo,
      status: status.textContent.trim(), error: status.classList.contains("error"),
      cursorText: document.querySelector("#cursor").textContent,
    };
  });
}

/**
 * Poll the view until it satisfies a condition.
 * @param {import("playwright").Page} page - Editor page.
 * @param {string} stage - Description for a timeout error.
 * @param {function(Awaited<ReturnType<typeof view>>): boolean} matches - View predicate.
 * @returns {Promise<Awaited<ReturnType<typeof view>>>} The first matching view.
 */
async function waitView(page, stage, matches) {
  const deadline = Date.now() + 8_000;
  let last;
  do {
    last = await view(page);
    if (matches(last)) return last;
    await page.waitForTimeout(10);
  } while (Date.now() < deadline);
  throw new Error(`Editor never reached ${stage}; last view: ${JSON.stringify(last)}`);
}

/**
 * Choose a file through the real Open level picker.
 * @param {import("playwright").Page} page - Editor page.
 * @param {string} path - Level file.
 * @returns {Promise<void>} Resolves once the browser accepted the selection.
 */
async function chooseLevel(page, path) {
  const [chooser] = await Promise.all([
    page.waitForEvent("filechooser", { timeout: 5_000 }),
    page.locator('label[for="open"]').click(),
  ]);
  await chooser.setFiles(path);
}

/**
 * Return the viewport position of a cell center.
 * @param {import("playwright").Page} page - Editor page.
 * @param {number} x - Column.
 * @param {number} y - Row.
 * @returns {Promise<{x: number, y: number}>} Viewport coordinates.
 */
async function cellPoint(page, x, y) {
  const box = await page.locator("#board").boundingBox();
  return { x: box.x + x * cell + cell / 2, y: box.y + y * cell + cell / 2 };
}

/**
 * Drag the primary button from one cell to another.
 * @param {import("playwright").Page} page - Editor page.
 * @param {[number, number]} from - Start cell.
 * @param {[number, number]} to - End cell.
 * @returns {Promise<void>} Resolves after release.
 */
async function drag(page, from, to) {
  const start = await cellPoint(page, ...from);
  await page.mouse.move(start.x, start.y);
  await page.mouse.down();
  const end = await cellPoint(page, ...to);
  await page.mouse.move(end.x, end.y);
  await page.mouse.up();
}

/**
 * Move the keyboard cursor from its announced cell to a target with arrow keys.
 * @param {import("playwright").Page} page - Editor page with the grid focused.
 * @param {number} x - Target column.
 * @param {number} y - Target row.
 * @returns {Promise<void>} Resolves when the cursor announces the target.
 */
async function moveCursor(page, x, y) {
  const { cursorText } = await view(page);
  const match = cursorText.match(/Cell (\d+), (\d+)/);
  assert.ok(match, `the cursor coordinates are not announced: ${cursorText}`);
  const [cx, cy] = [Number(match[1]), Number(match[2])];
  for (let i = cx; i < x; i++) await page.keyboard.press("ArrowRight");
  for (let i = cx; i > x; i--) await page.keyboard.press("ArrowLeft");
  for (let i = cy; i < y; i++) await page.keyboard.press("ArrowDown");
  for (let i = cy; i > y; i--) await page.keyboard.press("ArrowUp");
  await waitView(page, `cursor (${x}, ${y})`, v => v.cursorText.startsWith(`Cell ${x}, ${y}:`));
}

/**
 * Fill the zone form.
 * @param {import("playwright").Page} page - Editor page.
 * @param {Partial<Zone>} zone - Fields to set; omitted fields keep their value.
 * @returns {Promise<void>} Resolves when every field is set.
 */
async function fillForm(page, zone) {
  if (zone.kind !== undefined) await page.locator("#zone-kind").fill(zone.kind);
  if (zone.role !== undefined) await page.locator("#zone-role").selectOption(zone.role);
  for (const name of ["x", "y", "w", "h", "bottom", "top"]) {
    if (zone[name] !== undefined) await page.locator(`#zone-${name}`).fill(String(zone[name]));
  }
}

/**
 * Sample the 8x8 pixel center of cells from the rendered canvas.
 * @param {import("playwright").Page} page - Editor page rendered at DPR 1.
 * @param {[number, number][]} cells - Cells to sample.
 * @returns {Promise<string[]>} Hex digests of each sample, in request order.
 */
async function cellPixels(page, cells) {
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  const frame = PNG.sync.read(await page.locator("#board").screenshot({ timeout: 3_000 }));
  return cells.map(([x, y]) => {
    const sample = Buffer.alloc(8 * 8 * 4);
    for (let row = 0; row < 8; row++) {
      const offset = ((y * cell + 8 + row) * frame.width + x * cell + 8) * 4;
      frame.data.copy(sample, row * 32, offset, offset + 32);
    }
    return sample.toString("hex");
  });
}

/**
 * Author, edit, export, and reopen zones.
 * @param {import("playwright").Browser} browser - Chromium instance.
 * @param {string} url - Editor URL.
 * @param {string | undefined} output - Optional path for the download.
 * @returns {Promise<void>} Resolves when every check passes.
 */
async function smokeZones(browser, url, output) {
  const page = await browser.newPage({ viewport: { width: 1100, height: 800 }, deviceScaleFactor: 1 });
  const errors = [];
  const dialogs = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", message => message.type() === "error" && errors.push(message.text()));
  page.on("dialog", dialog => {
    dialogs.push(dialog.message());
    return dialog.dismiss();
  });
  try {
    await page.goto(url, { timeout: 15_000 });
    await chooseLevel(page, courtyardPath);
    let v = await waitView(page, "the opened courtyard", s => s.status.startsWith("Opened") && !s.error);
    assert.deepEqual(v.zones, [], "the version 1 courtyard has no zones");
    assert.equal(await page.locator("#zones").isDisabled(), false, "the zone panel stays disabled after opening");

    // Draw the deck with a pointer drag, using the form's kind, role, and heights.
    await page.locator("#zone").click();
    assert.equal(await page.locator("#zone").getAttribute("aria-pressed"), "true");
    await fillForm(page, { kind: "deck", role: "solid", bottom: 24, top: 32 });
    const [plain] = await cellPixels(page, [[3, 6]]);
    await drag(page, [3, 5], [5, 5]);
    v = await waitView(page, "the drawn deck", s => s.zones.length === 1);
    assert.deepEqual(v.zones, [deck]);
    assert.match(v.status, /Added solid zone #1 deck/);
    const [covered] = await cellPixels(page, [[4, 5]]);
    assert.notEqual(covered, plain, "a zone must tint the cells it covers");
    assert.equal(await page.locator("#zone-select option").count(), 1);
    assert.match(await page.locator("#zone-select option").first().textContent(), /#1 deck \(solid\) at 3, 5, 3 × 1/);

    // Add the doorway from the typed form.
    await fillForm(page, { kind: "door", role: "trigger", x: 4, y: 0, w: 1, h: 1, bottom: 0, top: 32 });
    await page.locator("#zone-add").click();
    v = await waitView(page, "the typed doorway", s => s.zones.length === 2);
    assert.deepEqual(v.zones, [deck, door]);

    // Rejected input names the problem and changes nothing.
    await fillForm(page, { w: 0 });
    await page.locator("#zone-add").click();
    v = await waitView(page, "the rejected zero width", s => s.error);
    assert.match(v.status, /not inside the/);
    await fillForm(page, { w: 5, x: 8 });
    await page.locator("#zone-add").click();
    v = await waitView(page, "the rejected overhang", s => s.error);
    await fillForm(page, { w: 1, x: 4, bottom: 40, top: 40 });
    await page.locator("#zone-add").click();
    v = await waitView(page, "the rejected empty height", s => s.error);
    assert.match(v.status, /vertical extent/);
    assert.deepEqual(v.zones, [deck, door], "a rejected zone changed the level");
    await fillForm(page, { bottom: 0, top: 32 });

    // Draw a third zone with the keyboard, then delete it and walk undo and redo over it.
    await page.locator("#zone").click();
    await page.locator("#board").focus();
    await moveCursor(page, 0, 6);
    await page.keyboard.press("Space");
    v = await waitView(page, "the pending corner", s => /Drawing a zone from 0, 6/.test(s.cursorText));
    await moveCursor(page, 1, 6);
    await page.keyboard.press("Space");
    v = await waitView(page, "the keyboard zone", s => s.zones.length === 3);
    assert.deepEqual(v.zones[2], { ...door, id: 3, x: 0, y: 6, w: 2, h: 1 });
    await page.locator("#zone-delete").click();
    v = await waitView(page, "the deleted zone", s => s.zones.length === 2);
    await page.locator("#board").focus();
    await page.keyboard.press(`${modifier}+z`);
    v = await waitView(page, "the undone deletion", s => s.zones.length === 3);
    await page.keyboard.press(`${modifier}+Shift+z`);
    v = await waitView(page, "the redone deletion", s => s.zones.length === 2);

    // A corner can be canceled.
    await moveCursor(page, 8, 6);
    await page.keyboard.press("Space");
    await waitView(page, "a pending corner", s => /Drawing a zone/.test(s.cursorText));
    await page.keyboard.press("Escape");
    v = await waitView(page, "the canceled corner", s => s.status === "Zone canceled.");
    assert.deepEqual(v.zones, [deck, door]);

    // Select the deck from the list, edit it, and put it back.
    await page.locator("#zone-select").selectOption("1");
    assert.equal(await page.locator("#zone-w").inputValue(), "3");
    assert.equal(await page.locator("#zone-role").inputValue(), "solid");
    await fillForm(page, { top: 30 });
    await page.locator("#zone-apply").click();
    v = await waitView(page, "the edited deck", s => s.zones[0].top === 30);
    await fillForm(page, { top: 32 });
    await page.locator("#zone-apply").click();
    v = await waitView(page, "the restored deck", s => s.zones[0].top === 32);
    assert.deepEqual(v.zones, [deck, door]);

    // Export, then reopen the download and check that every zone survived.
    const [download] = await Promise.all([page.waitForEvent("download"), page.locator("#export").click()]);
    const path = await download.path();
    if (output !== undefined) await download.saveAs(output);
    const text = await readFile(path, "utf8");
    assert.match(text, /"version": 2/);
    assert.deepEqual(JSON.parse(text).zones, [deck, door]);
    v = await waitView(page, "a clean export", s => !s.dirty);
    await chooseLevel(page, path);
    v = await waitView(page, "the reopened download", s => s.status.startsWith("Opened") && !s.error);
    assert.deepEqual(v.zones, [deck, door]);
    assert.equal(v.dirty, false);
    assert.deepEqual(dialogs, [], "unexpected browser dialogs");
    assert.deepEqual(errors, []);
  } finally {
    await page.close();
  }
}

const output = process.argv[2];
if (process.argv.length > 3) throw new Error("Usage: node scripts/smoke-zone-editor.mjs [EXPORT_PATH]");

const server = await serveLevelEditor(0);
try {
  const browser = await chromium.launch({ timeout: 15_000 });
  try {
    await smokeZones(browser, `http://127.0.0.1:${server.address().port}/`, output);
    console.log("Zone editor smoke passed: pointer and keyboard drawing, typed zones, rejected input, " +
      "edit, delete, undo, redo, export as version 2, and reopen");
  } finally {
    await browser.close();
  }
} finally {
  await promisify(server.close.bind(server))();
}
