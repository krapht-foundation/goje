/**
 * @file Drive the Level Builder's placement editing in Chromium with real file choosers, pointer, and keyboard input.
 *
 * It opens the courtyard, adds scouts and guards by pointer, keyboard, and typed cell, rejects invalid
 * additions, changes a kind, deletes a placement, walks undo and redo, then exports a level and reopens it.
 *
 * Usage: node scripts/smoke-placement-editor.mjs [EXPORT_PATH]
 * EXPORT_PATH optionally keeps the download.
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
 * @typedef {object} Placement
 * @property {number} id - Placement ID.
 * @property {string} kind - Game-defined kind.
 * @property {number} x - Column.
 * @property {number} y - Row.
 */

const scout = { id: 1, kind: "scout", x: 2, y: 2 };
const lookout = { id: 2, kind: "lookout", x: 7, y: 4 };

/**
 * Read the observable editor view. The script only reads state; all changes come from input.
 * @param {import("playwright").Page} page - Editor page.
 * @returns {Promise<{placements: Placement[], dirty: boolean, status: string, error: boolean, cursorText: string}>} View,
 * with placements sorted by ID.
 */
async function view(page) {
  return page.evaluate(() => {
    const status = document.querySelector("#status");
    const state = JSON.parse(JSON.stringify(globalThis.gojeLevelEditor.state()));
    return {
      placements: state.placements.map(p => ({ id: p.id, kind: p.kind, x: p.x, y: p.y })).sort((a, b) => a.id - b.id),
      dirty: state.dirty,
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
 * Click the center of a cell with the primary button.
 * @param {import("playwright").Page} page - Editor page.
 * @param {number} x - Column.
 * @param {number} y - Row.
 * @returns {Promise<void>} Resolves after release.
 */
async function clickCell(page, x, y) {
  const box = await page.locator("#board").boundingBox();
  await page.mouse.click(box.x + x * cell + cell / 2, box.y + y * cell + cell / 2);
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
 * Sample the 8x8 pixel center of a cell from the rendered canvas.
 * @param {import("playwright").Page} page - Editor page rendered at DPR 1.
 * @param {number} x - Column.
 * @param {number} y - Row.
 * @returns {Promise<string>} Hex digest of the sample.
 */
async function cellPixels(page, x, y) {
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
  const frame = PNG.sync.read(await page.locator("#board").screenshot({ timeout: 3_000 }));
  const sample = Buffer.alloc(8 * 8 * 4);
  for (let row = 0; row < 8; row++) {
    const offset = ((y * cell + 8 + row) * frame.width + x * cell + 8) * 4;
    frame.data.copy(sample, row * 32, offset, offset + 32);
  }
  return sample.toString("hex");
}

/**
 * Add, reject, re-kind, delete, undo, redo, export, and reopen placements.
 * @param {import("playwright").Browser} browser - Chromium instance.
 * @param {string} url - Editor URL.
 * @param {string | undefined} output - Optional path for the download.
 * @returns {Promise<void>} Resolves when every check passes.
 */
async function smokePlacements(browser, url, output) {
  const page = await browser.newPage({ viewport: { width: 1100, height: 900 }, deviceScaleFactor: 1 });
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
    assert.deepEqual(v.placements, [scout, lookout]);
    assert.equal(await page.locator("#placements").isDisabled(), false, "the placement panel stays disabled after opening");
    assert.equal(await page.locator("#placement-select option").count(), 2);

    // Add a scout and a guard with the pointer; IDs follow the largest.
    await page.locator("#add-placement").click();
    assert.equal(await page.locator("#add-placement").getAttribute("aria-pressed"), "true");
    const empty = await cellPixels(page, 1, 5);
    await page.locator("#placement-kind").fill("scout");
    await clickCell(page, 1, 5);
    v = await waitView(page, "the clicked scout", s => s.placements.length === 3);
    assert.deepEqual(v.placements[2], { id: 3, kind: "scout", x: 1, y: 5 });
    assert.match(v.status, /Added scout #3 at 1, 5/);
    assert.notEqual(await cellPixels(page, 1, 5), empty, "a new placement must be drawn");
    await page.locator("#placement-kind").fill("guard");
    await clickCell(page, 6, 1);
    v = await waitView(page, "the clicked guard", s => s.placements.length === 4);
    assert.deepEqual(v.placements[3], { id: 4, kind: "guard", x: 6, y: 1 });

    // Add a guard with the keyboard and a scout with a typed cell and authored ID.
    await page.locator("#board").focus();
    await moveCursor(page, 8, 6);
    await page.keyboard.press("Space");
    v = await waitView(page, "the keyboard guard", s => s.placements.length === 5);
    assert.deepEqual(v.placements[4], { id: 5, kind: "guard", x: 8, y: 6 });
    await page.locator("#placement-kind").fill("scout");
    await page.locator("#placement-id").fill("20");
    await page.locator("#placement-x").fill("0");
    await page.locator("#placement-y").fill("0");
    await page.locator("#placement-add").click();
    v = await waitView(page, "the typed scout", s => s.placements.length === 6);
    assert.deepEqual(v.placements[5], { id: 20, kind: "scout", x: 0, y: 0 });
    assert.equal(await page.locator("#placement-id").inputValue(), "", "an authored ID must be used only once");
    const authored = v.placements;

    // Rejected additions name the problem and change nothing.
    await clickCell(page, 2, 2);
    v = await waitView(page, "the rejected occupied cell", s => s.error);
    assert.match(v.status, /already has a placement/);
    await clickCell(page, 4, 2);
    v = await waitView(page, "the rejected wall", s => s.error && /wall/.test(s.status));
    await page.locator("#placement-kind").fill("Guard");
    await clickCell(page, 0, 6);
    v = await waitView(page, "the rejected kind", s => s.error && /invalid kind/.test(s.status));
    await page.locator("#placement-kind").fill("guard");
    await page.locator("#placement-id").fill("20");
    await clickCell(page, 0, 6);
    v = await waitView(page, "the rejected duplicate ID", s => s.error && /already in use/.test(s.status));
    await page.locator("#placement-id").fill("");
    assert.deepEqual(v.placements, authored, "a rejected addition changed the level");

    // Change the clicked scout into a guard from the list.
    await page.locator("#placement-select").selectOption("3");
    assert.equal(await page.locator("#placement-kind").inputValue(), "scout");
    await page.locator("#placement-kind").fill("guard");
    await page.locator("#placement-apply").click();
    v = await waitView(page, "the re-kinded placement", s => s.placements[2].kind === "guard");
    assert.deepEqual(v.placements[2], { id: 3, kind: "guard", x: 1, y: 5 });

    // Delete the first guard with the Delete key in the list.
    await page.locator("#placement-select").selectOption("4");
    await page.locator("#placement-select").press("Delete");
    v = await waitView(page, "the deleted guard", s => s.placements.length === 5);
    assert.ok(!v.placements.some(p => p.id === 4));
    assert.match(v.status, /Deleted placement #4/);
    assert.equal(await page.locator("#placement-select option").count(), 5);

    // Undo the deletion and the kind change, then redo both.
    await page.locator("#board").focus();
    await page.keyboard.press(`${modifier}+z`);
    v = await waitView(page, "the undone deletion", s => s.placements.length === 6);
    assert.deepEqual(v.placements[3], { id: 4, kind: "guard", x: 6, y: 1 });
    await page.keyboard.press(`${modifier}+z`);
    v = await waitView(page, "the undone kind change", s => s.placements[2].kind === "scout");
    await page.keyboard.press(`${modifier}+Shift+z`);
    await page.keyboard.press(`${modifier}+Shift+z`);
    v = await waitView(page, "both redone", s => s.placements.length === 5 && s.placements[2].kind === "guard");
    const want = [scout, lookout, { id: 3, kind: "guard", x: 1, y: 5 }, { id: 5, kind: "guard", x: 8, y: 6 },
      { id: 20, kind: "scout", x: 0, y: 0 }];
    assert.deepEqual(v.placements, want);

    // Export, check the file, and reopen it.
    const [download] = await Promise.all([page.waitForEvent("download"), page.locator("#export").click()]);
    const path = await download.path();
    if (output !== undefined) await download.saveAs(output);
    const level = JSON.parse(await readFile(path, "utf8"));
    assert.equal(level.version, 1, "a level without zones exports as version 1");
    assert.deepEqual(Object.keys(level), ["version", "rows", "placements"]);
    assert.equal(level.rows.length, 7);
    assert.deepEqual(level.placements, want, "export lost a placement or the document order");
    v = await waitView(page, "a clean export", s => !s.dirty);
    await chooseLevel(page, path);
    v = await waitView(page, "the reopened download", s => s.status.startsWith("Opened") && !s.error);
    assert.deepEqual(v.placements, want);
    assert.equal(v.dirty, false);
    assert.deepEqual(dialogs, [], "unexpected browser dialogs");
    assert.deepEqual(errors, []);
  } finally {
    await page.close();
  }
}

const output = process.argv[2];
if (process.argv.length > 3) throw new Error("Usage: node scripts/smoke-placement-editor.mjs [EXPORT_PATH]");

const server = await serveLevelEditor(0);
try {
  const browser = await chromium.launch({ timeout: 15_000 });
  try {
    await smokePlacements(browser, `http://127.0.0.1:${server.address().port}/`, output);
    console.log("Placement editor smoke passed: pointer, keyboard, and typed additions, rejected input, " +
      "kind change, deletion, undo, redo, export, and reopen");
  } finally {
    await browser.close();
  }
} finally {
  await promisify(server.close.bind(server))();
}
