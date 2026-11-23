/**
 * @file Drive the Level Builder in Chromium with real file choosers, pointer, and keyboard input.
 *
 * Usage: node scripts/smoke-level-editor.mjs [EXPORT_PATH]
 * EXPORT_PATH optionally keeps the edited courtyard download for the author-to-play check.
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
 * @property {number} id - Stable placement ID.
 * @property {string} kind - Placement kind identifier.
 * @property {number} x - Column.
 * @property {number} y - Row.
 */

/**
 * @typedef {object} EditorState
 * @property {number} width - Board columns.
 * @property {number} height - Board rows.
 * @property {string} terrain - Row-major terrain, one '.' or '#' per cell.
 * @property {Placement[]} placements - Placements in file order.
 * @property {boolean} dirty - Whether edits have not been exported.
 * @property {boolean} canUndo - Whether undo is available.
 * @property {boolean} canRedo - Whether redo is available.
 */

/**
 * @typedef {object} View
 * @property {EditorState} state - Go document snapshot.
 * @property {string} status - Visible status text.
 * @property {boolean} error - Whether the status is styled as an error.
 * @property {[number, number] | null} cursor - Announced cursor cell.
 * @property {boolean} focused - Whether the grid canvas has focus.
 */

/**
 * Encode a level exactly as the v1 Go encoder does.
 * @param {string[]} rows - Terrain rows.
 * @param {Placement[]} placements - Placements in file order.
 * @returns {string} Canonical JSON text.
 */
function encodeLevel(rows, placements) {
  const body = placements.map(p => `\n    {"id": ${p.id}, "kind": "${p.kind}", "x": ${p.x}, "y": ${p.y}}`);
  return `{\n  "version": 1,\n  "rows": [${rows.map(row => `\n    "${row}"`).join(",")}\n  ],\n` +
    `  "placements": [${body.join(",")}${placements.length ? "\n  " : ""}]\n}\n`;
}

/**
 * Split flat terrain into rows.
 * @param {EditorState} state - Editor snapshot.
 * @returns {string[]} Terrain rows.
 */
function rowsOf(state) {
  return Array.from({ length: state.height }, (_, y) => state.terrain.slice(y * state.width, (y + 1) * state.width));
}

/**
 * Read the terrain at one cell.
 * @param {EditorState} state - Editor snapshot.
 * @param {number} x - Column.
 * @param {number} y - Row.
 * @returns {string} '.' or '#'.
 */
function tile(state, x, y) {
  return state.terrain[y * state.width + x];
}

/**
 * Track browser dialogs and answer them according to the current expectation.
 * Unexpected dialogs are dismissed and recorded so the smoke can fail on them.
 */
class Dialogs {
  /** @type {"accept" | "dismiss" | null} */
  answer = null;
  /** @type {string[]} */
  seen = [];
  /** @type {string[]} */
  unexpected = [];

  /**
   * Attach to a page.
   * @param {import("playwright").Page} page - Editor page.
   */
  constructor(page) {
    page.on("dialog", dialog => {
      if (this.answer === null) {
        this.unexpected.push(dialog.message());
        return dialog.dismiss();
      }
      this.seen.push(dialog.message());
      return this.answer === "accept" ? dialog.accept() : dialog.dismiss();
    });
  }
}

/**
 * Read the observable editor view. The script only reads state; all changes come from input.
 * @param {import("playwright").Page} page - Editor page.
 * @returns {Promise<View>} Current view.
 */
async function view(page) {
  return page.evaluate(() => {
    const status = document.querySelector("#status");
    const match = document.querySelector("#cursor").textContent.match(/(\d+)\D+(\d+)/);
    return {
      // Copy through JSON so Go-owned values cross as plain data.
      state: JSON.parse(JSON.stringify(globalThis.gojeLevelEditor.state())),
      status: status.textContent.trim(),
      error: status.classList.contains("error"),
      cursor: match ? [Number(match[1]), Number(match[2])] : null,
      focused: document.activeElement === document.querySelector("#board"),
    };
  });
}

/**
 * Poll the editor view until it satisfies a condition.
 * @param {import("playwright").Page} page - Editor page.
 * @param {string} stage - Description for a timeout error.
 * @param {function(View): boolean} matches - View predicate.
 * @param {number} [timeout=8000] - Deadline in milliseconds.
 * @returns {Promise<View>} The first matching view.
 * @throws {Error} The returned promise rejects with the last view if the deadline passes.
 */
async function waitView(page, stage, matches, timeout = 8_000) {
  const deadline = Date.now() + timeout;
  let last;
  do {
    last = await view(page);
    if (matches(last)) return last;
    await page.waitForTimeout(10);
  } while (Date.now() < deadline);
  throw new Error(`Editor never reached ${stage}; last view: ${JSON.stringify({ ...last, state: { ...last.state, terrain: undefined } })}`);
}

/**
 * Wait until the browser has painted two more frames.
 * @param {import("playwright").Page} page - Editor page.
 * @returns {Promise<void>} Resolves after the frames.
 */
async function frames(page) {
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
}

/**
 * Choose files through the real Open level picker.
 * @param {import("playwright").Page} page - Editor page.
 * @param {Parameters<import("playwright").FileChooser["setFiles"]>[0]} files - Path or in-memory payload.
 * @returns {Promise<void>} Resolves once the browser accepted the selection.
 */
async function chooseLevel(page, files) {
  const [chooser] = await Promise.all([
    page.waitForEvent("filechooser", { timeout: 5_000 }),
    page.locator('label[for="open"]').click(),
  ]);
  assert.ok(!chooser.isMultiple(), "the level picker must select one file");
  await chooser.setFiles(files);
}

/**
 * Open a valid level and wait for the editor to show it from the first cell.
 * @param {import("playwright").Page} page - Editor page.
 * @param {Parameters<import("playwright").FileChooser["setFiles"]>[0]} files - Level file.
 * @param {string} rows - Expected flat terrain.
 * @returns {Promise<View>} View after opening.
 */
async function openLevel(page, files, rows) {
  await chooseLevel(page, files);
  return waitView(page, "the opened level", v =>
    v.state.terrain === rows && !v.state.dirty && !v.state.canUndo && !v.state.canRedo &&
    v.focused && v.cursor?.[0] === 0 && v.cursor[1] === 0 && !v.error);
}

/**
 * Choose a file that must not replace the active document.
 * @param {import("playwright").Page} page - Editor page.
 * @param {Parameters<import("playwright").FileChooser["setFiles"]>[0]} files - Rejected file.
 * @param {string} stage - Failure description.
 * @returns {Promise<View>} Error view.
 */
async function openRejected(page, files, stage) {
  const before = await view(page);
  await chooseLevel(page, files);
  const after = await waitView(page, stage, v => v.error && v.status !== before.status && v.status !== "");
  assert.deepEqual(after.state, before.state, `${stage} changed the document or history`);
  return after;
}

/**
 * Move the keyboard cursor with arrow keys.
 * @param {import("playwright").Page} page - Editor page with the grid focused.
 * @param {number} x - Target column.
 * @param {number} y - Target row.
 * @returns {Promise<void>} Resolves when the cursor announces the target.
 */
async function moveCursor(page, x, y) {
  const { cursor } = await view(page);
  assert.ok(cursor, "the cursor coordinates are not announced");
  const [cx, cy] = cursor;
  for (let i = cx; i < x; i++) await page.keyboard.press("ArrowRight");
  for (let i = cx; i > x; i--) await page.keyboard.press("ArrowLeft");
  for (let i = cy; i < y; i++) await page.keyboard.press("ArrowDown");
  for (let i = cy; i > y; i--) await page.keyboard.press("ArrowUp");
  await waitView(page, `cursor (${x}, ${y})`, v => v.cursor?.[0] === x && v.cursor[1] === y);
}

/**
 * Select a tool by its labeled button, then return keyboard focus to the grid.
 * @param {import("playwright").Page} page - Editor page.
 * @param {"floor" | "wall" | "placement"} tool - Tool button ID.
 * @returns {Promise<void>} Resolves when the tool is pressed and the grid is focused.
 */
async function selectTool(page, tool) {
  await page.locator(`#${tool}`).click();
  for (const other of ["floor", "wall", "placement"]) {
    assert.equal(await page.locator(`#${other}`).getAttribute("aria-pressed"), String(other === tool));
  }
  await page.locator("#board").focus();
}

/**
 * Return the screen position of a cell center.
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
 * Drag a primary-button stroke across cells, jumping straight to each waypoint.
 * @param {import("playwright").Page} page - Editor page.
 * @param {[number, number][]} cells - Waypoints; skipped cells between them must be interpolated.
 * @param {boolean} [cancel=false] - Press Escape before releasing.
 * @returns {Promise<void>} Resolves after release.
 */
async function drag(page, cells, cancel = false) {
  const [first, ...rest] = cells;
  const start = await cellPoint(page, ...first);
  await page.mouse.move(start.x, start.y);
  await page.mouse.down();
  for (const waypoint of rest) {
    const point = await cellPoint(page, ...waypoint);
    await page.mouse.move(point.x, point.y);
  }
  if (cancel) await page.keyboard.press("Escape");
  await page.mouse.up();
}

/**
 * Capture the 8x8 pixel center of each requested cell from the rendered canvas.
 * @param {import("playwright").Page} page - Editor page rendered at DPR 1.
 * @param {[number, number][]} cells - Cells to sample.
 * @returns {Promise<Buffer[]>} RGBA samples in request order.
 */
async function cellPixels(page, cells) {
  await frames(page);
  const frame = PNG.sync.read(await page.locator("#board").screenshot({ timeout: 3_000 }));
  return cells.map(([x, y]) => {
    const sample = Buffer.alloc(8 * 8 * 4);
    for (let row = 0; row < 8; row++) {
      const offset = ((y * cell + 8 + row) * frame.width + x * cell + 8) * 4;
      frame.data.copy(sample, row * 32, offset, offset + 32);
    }
    return sample;
  });
}

/**
 * Check that the canvas is laid out at 24 CSS pixels per cell inside a scrolling viewport.
 * @param {import("playwright").Page} page - Editor page.
 * @param {number} width - Board columns.
 * @param {number} height - Board rows.
 * @returns {Promise<{clientWidth: number, clientHeight: number, scrollWidth: number, scrollHeight: number}>} Scroller metrics.
 */
async function checkLayout(page, width, height) {
  const layout = await page.evaluate(() => {
    const board = document.querySelector("#board"), scroller = board.parentElement;
    const rect = board.getBoundingClientRect();
    const style = getComputedStyle(scroller);
    return {
      cssWidth: rect.width, cssHeight: rect.height, pixelWidth: board.width, pixelHeight: board.height,
      dpr: devicePixelRatio, overflow: [style.overflowX, style.overflowY],
      clientWidth: scroller.clientWidth, clientHeight: scroller.clientHeight,
      scrollWidth: scroller.scrollWidth, scrollHeight: scroller.scrollHeight,
    };
  });
  assert.deepEqual([layout.cssWidth, layout.cssHeight], [width * cell, height * cell], "the grid is stretched");
  assert.deepEqual([layout.pixelWidth, layout.pixelHeight],
    [Math.round(width * cell * layout.dpr), Math.round(height * cell * layout.dpr)], "canvas backing store is scaled");
  assert.ok(layout.overflow.every(value => value === "auto" || value === "scroll"), "the grid viewport cannot scroll");
  return layout;
}

/**
 * Open the authored courtyard, edit it through keyboard and pointer, and export it.
 * @param {import("playwright").Browser} browser - Chromium instance.
 * @param {string} url - Editor URL.
 * @param {string | undefined} output - Optional path for the edited download.
 * @returns {Promise<void>} Resolves when the baseline passes.
 */
async function smokeCourtyard(browser, url, output) {
  const page = await browser.newPage({ viewport: { width: 900, height: 700 }, deviceScaleFactor: 1 });
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", message => message.type() === "error" && errors.push(message.text()));
  const dialogs = new Dialogs(page);
  try {
    await page.goto(url, { timeout: 15_000 });
    const authored = await readFile(courtyardPath, "utf8");
    const original = JSON.parse(authored);
    const originalTerrain = original.rows.join("");
    let v = await openLevel(page, courtyardPath, originalTerrain);
    assert.deepEqual(v.state.placements, original.placements);
    await checkLayout(page, 9, 7);
    assert.equal(await page.locator("#floor").getAttribute("aria-pressed"), "true", "floor is not the default tool");

    // Pixel references: (4,1) is wall, (0,4) is floor, (2,2) holds the scout.
    await moveCursor(page, 0, 6);
    let [wallRef, floorRef, gap, lookoutFrom, lookoutTo, scout] = await cellPixels(page,
      [[4, 1], [0, 4], [4, 2], [7, 4], [7, 5], [2, 2]]);
    assert.ok(gap.equals(wallRef) && lookoutTo.equals(floorRef), "authored terrain is not drawn");
    assert.ok(!wallRef.equals(floorRef) && !lookoutFrom.equals(floorRef) && !scout.equals(floorRef),
      "walls and placements are not distinguishable from floor");

    // The default floor brush opens the wall with one keyboard action.
    await moveCursor(page, 4, 2);
    await page.keyboard.press("Space");
    v = await waitView(page, "keyboard opening", v => tile(v.state, 4, 2) === "." && v.state.canUndo);
    assert.ok(v.state.dirty && !v.state.canRedo);
    const opened = v.state;

    // An Escape-canceled pointer stroke rolls back and leaves history untouched.
    await selectTool(page, "wall");
    await drag(page, [[0, 6], [3, 6]], true);
    await frames(page);
    assert.deepEqual((await view(page)).state, opened, "a canceled stroke changed the document");

    // A pointer stroke interpolates skipped cells, applies valid cells, and refuses to cover the scout.
    const beforeStroke = (await view(page)).status;
    await drag(page, [[0, 2], [3, 2]]);
    v = await waitView(page, "wall stroke", v => tile(v.state, 3, 2) === "#");
    assert.equal(rowsOf(v.state)[2], "##.#.....", "the stroke skipped cells or covered the scout");
    assert.deepEqual(v.state.placements, original.placements);
    assert.ok(v.error && v.status !== beforeStroke, "the blocked wall cell had no visible feedback");

    // The button undoes the whole stroke at once.
    await page.locator("#undo").click();
    v = await waitView(page, "button undo", v => v.state.canRedo);
    assert.deepEqual({ ...v.state, canRedo: false }, opened, "undo did not restore the whole stroke");

    // A rejected move stays put. A valid move is a new edit and discards redo.
    await selectTool(page, "placement");
    await moveCursor(page, 7, 4);
    await page.keyboard.press("Enter");
    await moveCursor(page, 4, 3);
    const beforeMove = (await view(page)).status;
    await page.keyboard.press("Enter");
    v = await waitView(page, "rejected move", v => v.error && v.status !== beforeMove);
    assert.deepEqual({ ...v.state, canRedo: false }, opened, "a placement moved onto a wall");
    await page.keyboard.press("Escape");
    await moveCursor(page, 7, 4);
    await page.keyboard.press("Enter");
    await moveCursor(page, 7, 5);
    await page.keyboard.press("Enter");
    v = await waitView(page, "lookout move", v => v.state.placements[1].y === 5);
    const expectedPlacements = [original.placements[0], { ...original.placements[1], y: 5 }];
    assert.deepEqual(v.state.placements, expectedPlacements, "the move changed IDs, kinds, or order");
    assert.ok(!v.state.canRedo, "a new edit kept the redo branch");
    const edited = v.state;

    await page.keyboard.press(`${modifier}+z`);
    v = await waitView(page, "keyboard undo", v => v.state.placements[1].y === 4);
    assert.deepEqual({ ...v.state, canRedo: false }, opened);
    await page.keyboard.press(`${modifier}+Shift+z`);
    v = await waitView(page, "keyboard redo", v => v.state.placements[1].y === 5);
    assert.deepEqual(v.state, edited);

    await moveCursor(page, 0, 6);
    [wallRef, floorRef, gap, lookoutFrom, lookoutTo] = await cellPixels(page, [[4, 1], [0, 4], [4, 2], [7, 4], [7, 5]]);
    assert.ok(gap.equals(floorRef) && lookoutFrom.equals(floorRef) && !lookoutTo.equals(floorRef),
      "the canvas does not show the opening and moved lookout");

    // Unexported edits survive a dismissed replacement prompt.
    dialogs.answer = "dismiss";
    await chooseLevel(page, courtyardPath);
    await waitView(page, "replacement prompt", () => dialogs.seen.length === 1);
    await frames(page);
    assert.deepEqual((await view(page)).state, edited, "a dismissed prompt replaced the document");

    // Invalid and oversized files leave the edits and history intact.
    dialogs.answer = "accept";
    await openRejected(page, { name: "broken.json", mimeType: "application/json", buffer: Buffer.from("{") },
      "malformed level");
    const maxBytes = await page.evaluate(() => globalThis.gojeLevelEditor.maxBytes);
    await openRejected(page, { name: "huge.json", mimeType: "application/json", buffer: Buffer.alloc(maxBytes + 1, 32) },
      "oversized level");
    assert.equal(dialogs.seen.length, 2, "oversized files must be rejected before the replace prompt");
    dialogs.answer = null;

    // Export downloads canonical JSON and marks the document clean.
    const [download] = await Promise.all([page.waitForEvent("download"), page.locator("#export").click()]);
    assert.match(download.suggestedFilename(), /\.json$/);
    const exported = await readFile(await download.path(), "utf8");
    const expectedRows = original.rows.map((row, y) => y === 2 ? "........." : row);
    assert.equal(exported, encodeLevel(expectedRows, expectedPlacements), "export is not the canonical edited level");
    await waitView(page, "clean export", v => !v.state.dirty);
    if (output) await download.saveAs(output);

    // A clean document reopens the download without prompting.
    v = await openLevel(page, await download.path(), expectedRows.join(""));
    assert.deepEqual(v.state.placements, expectedPlacements);
    assert.equal(dialogs.seen.length, 2, "reopening after export should not prompt");

    // One more edit makes it dirty again; confirming replaces it with the authored level.
    await selectTool(page, "wall");
    await moveCursor(page, 0, 6);
    await page.keyboard.press("Enter");
    await waitView(page, "edit before replacement", v => v.state.dirty);
    dialogs.answer = "accept";
    await openLevel(page, courtyardPath, originalTerrain);
    assert.equal(dialogs.seen.length, 3, "replacing unexported edits did not ask first");
    dialogs.answer = null;

    assert.deepEqual(dialogs.unexpected, [], "unexpected browser dialogs");
    assert.deepEqual(errors, []);
  } finally {
    await page.close();
  }
}

/**
 * Build a full-size level with a wall spine and the maximum number of placements.
 * @returns {{rows: string[], placements: Placement[]}} Level content.
 */
function stressLevel() {
  const rows = Array.from({ length: 128 }, (_, y) =>
    Array.from({ length: 128 }, (_, x) => x === 64 && y % 16 !== 8 ? "#" : ".").join(""));
  const placements = Array.from({ length: 1024 }, (_, i) => {
    const column = i % 127;
    return { id: i + 1, kind: i % 2 ? "marker" : "crate", x: column < 64 ? column : column + 1,
      y: 100 + Math.floor(i / 127) };
  });
  return { rows, placements };
}

/**
 * Measure elapsed wall-clock time around an async step.
 * @param {function(): Promise<unknown>} step - Step to time.
 * @returns {Promise<number>} Milliseconds, rounded to 0.1.
 */
async function timed(step) {
  const start = performance.now();
  await step();
  return Math.round((performance.now() - start) * 10) / 10;
}

/**
 * Exercise a 128x128 board through the UI and report observed costs.
 * @param {import("playwright").Browser} browser - Chromium instance.
 * @param {string} url - Editor URL.
 * @returns {Promise<Record<string, number>>} Observed durations in milliseconds.
 */
async function stress(browser, url) {
  const page = await browser.newPage({ viewport: { width: 1024, height: 768 }, deviceScaleFactor: 1 });
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", message => message.type() === "error" && errors.push(message.text()));
  const dialogs = new Dialogs(page);
  try {
    await page.goto(url, { timeout: 15_000 });
    const { rows, placements } = stressLevel();
    const source = encodeLevel(rows, placements);
    const file = { name: "stress.json", mimeType: "application/json", buffer: Buffer.from(source) };
    const timings = {};
    timings.open_ms = await timed(async () => {
      await openLevel(page, file, rows.join(""));
      await frames(page);
    });
    const layout = await checkLayout(page, 128, 128);
    assert.ok(layout.scrollWidth > layout.clientWidth && layout.scrollHeight > layout.clientHeight,
      "a full board does not scroll");

    // A long wall stroke across every visible cell of one row, in one pointer jump.
    const last = Math.min(127, Math.floor(layout.clientWidth / cell) - 1);
    await selectTool(page, "wall");
    // Each timing covers Playwright input dispatch, browser handling, and two painted frames.
    timings.edit_ms = await timed(async () => {
      await drag(page, [[0, 10], [last, 10]]);
      await frames(page);
    });
    const edited = rows.map((row, y) => y === 10 ? "#".repeat(last + 1) + row.slice(last + 1) : row);
    let v = await view(page);
    assert.ok(v.state.terrain === edited.join("") && v.state.canUndo, "the stress stroke was not applied");
    timings.undo_ms = await timed(async () => {
      await page.keyboard.press(`${modifier}+z`);
      await frames(page);
    });
    v = await view(page);
    assert.ok(v.state.terrain === rows.join("") && v.state.canRedo, "the stress stroke was not undone");
    timings.redo_ms = await timed(async () => {
      await page.keyboard.press(`${modifier}+Shift+z`);
      await frames(page);
    });
    assert.equal((await view(page)).state.terrain, edited.join(""), "the stress stroke was not redone");

    // The keyboard cursor reaches the far corner and keeps it in view.
    await page.locator("#board").focus();
    timings.traverse_ms = await timed(() => moveCursor(page, 127, 127));
    const scrolled = await page.evaluate(() => {
      const scroller = document.querySelector("#board").parentElement;
      return [scroller.scrollLeft, scroller.scrollTop];
    });
    assert.ok(scrolled[0] > 0 && scrolled[1] > 0, "the cursor left the scroll viewport");

    let download;
    timings.export_ms = await timed(async () => {
      [download] = await Promise.all([page.waitForEvent("download"), page.locator("#export").click()]);
    });
    assert.equal(await readFile(await download.path(), "utf8"), encodeLevel(edited, placements));
    await waitView(page, "stress clean export", v => !v.state.dirty);
    assert.deepEqual(dialogs.unexpected, [], "unexpected browser dialogs");
    assert.deepEqual(errors, []);
    return { stroke_cells: last + 1, ...timings };
  } finally {
    await page.close();
  }
}

const output = process.argv[2];
if (process.argv.length > 3) throw new Error("Usage: node scripts/smoke-level-editor.mjs [EXPORT_PATH]");

const server = await serveLevelEditor(0);
try {
  const url = `http://127.0.0.1:${server.address().port}/`;
  const browser = await chromium.launch({ timeout: 15_000 });
  try {
    await smokeCourtyard(browser, url, output);
    console.log("Level editor smoke passed: open, keyboard and pointer edits, undo/redo, rejected files, prompts, export, and reopen");
    const report = await stress(browser, url);
    console.log(JSON.stringify({ benchmark: "goje-level-editor", board: "128x128", placements: 1024,
      chromium_version: browser.version(), ...report }));
  } finally {
    await browser.close();
  }
} finally {
  await promisify(server.close.bind(server))();
}
