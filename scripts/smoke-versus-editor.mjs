/**
 * @file Author the versus level's vision in the Level Builder with real file choosers and form input.
 *
 * It strips the shipped versus level down to its terrain, opens that in Chromium, sets the sight radius in the
 * Vision panel, undoes and redoes it, adds every placement through the Placements form and every sight blocker
 * through the Sight block preset, exports the result, and checks that the download is byte for byte the shipped
 * version 3 file, so the editor can rebuild the level that the playground ships.
 *
 * Usage: node scripts/smoke-versus-editor.mjs EXPORT_PATH
 * EXPORT_PATH receives the exported level, so the playground can be built with it.
 */

import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { chromium } from "playwright";

import { serveLevelEditor } from "./level-editor-server.mjs";

const versusPath = fileURLToPath(new URL("../cmd/playground/levels/versus.json", import.meta.url));

/**
 * Read the observable editor view. The script only reads state; all changes come from input.
 * @param {import("playwright").Page} page - Editor page.
 * @returns {Promise<{placements: object[], zones: object[], sight: number, field: string, canUndo: boolean, dirty: boolean, status: string, error: boolean}>} View.
 */
async function view(page) {
  return page.evaluate(() => {
    const status = document.querySelector("#status");
    const state = JSON.parse(JSON.stringify(globalThis.gojeLevelEditor.state()));
    return {
      placements: state.placements, zones: state.zones, sight: state.sight,
      field: /** @type {HTMLInputElement} */ (document.querySelector("#sight-radius")).value,
      canUndo: state.canUndo, dirty: state.dirty,
      status: status.textContent.trim(), error: status.classList.contains("error"),
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
 * Author the shipped level's vision, placements, and blockers on its bare terrain.
 * @param {import("playwright").Browser} browser - Chromium instance.
 * @param {string} url - Editor URL.
 * @param {string} output - Path for the download.
 * @returns {Promise<void>} Resolves when every check passes.
 */
async function smokeVersus(browser, url, output) {
  const shippedText = await readFile(versusPath, "utf8");
  const shipped = JSON.parse(shippedText);
  assert.equal(shipped.version, 3, "the versus level must be a version 3 level");
  const radius = shipped.sight.radius;
  const directory = await mkdtemp(join(tmpdir(), "goje-versus-"));
  const page = await browser.newPage({ viewport: { width: 1100, height: 1000 }, deviceScaleFactor: 1 });
  const errors = [];
  const dialogs = [];
  page.on("pageerror", error => errors.push(error.message));
  page.on("console", message => message.type() === "error" && errors.push(message.text()));
  page.on("dialog", dialog => {
    dialogs.push(dialog.message());
    return dialog.dismiss();
  });
  try {
    const terrain = join(directory, "terrain.json");
    await writeFile(terrain, JSON.stringify({ version: 1, rows: shipped.rows, placements: [] }));
    await page.goto(url, { timeout: 15_000 });
    await chooseLevel(page, terrain);
    let v = await waitView(page, "the opened terrain", s => s.status.startsWith("Opened") && !s.error);
    assert.deepEqual([v.placements, v.zones, v.sight, v.field], [[], [], 0, "0"], "the terrain must open without vision");

    // A radius outside the vision range is refused without an action.
    await page.locator("#sight-radius").fill("182");
    await page.locator("#sight-apply").click();
    v = await waitView(page, "a rejected radius", s => s.error);
    assert.equal(v.sight, 0);
    assert.equal(v.canUndo, false, "a rejected radius must not enter the history");

    // The radius is one undoable action, and undo and redo update the field.
    await page.locator("#sight-radius").fill(String(radius));
    await page.locator("#sight-apply").click();
    await waitView(page, `radius ${radius}`, s => s.sight === radius && !s.error);
    await page.locator("#undo").click();
    await waitView(page, "the undone radius", s => s.sight === 0 && s.field === "0");
    await page.locator("#redo").click();
    await waitView(page, "the redone radius", s => s.sight === radius && s.field === String(radius));

    // Every placement goes in through the form with its authored ID, in file order.
    for (const [i, p] of shipped.placements.entries()) {
      await page.locator("#placement-kind").fill(p.kind);
      await page.locator("#placement-id").fill(String(p.id));
      await page.locator("#placement-x").fill(String(p.x));
      await page.locator("#placement-y").fill(String(p.y));
      await page.locator("#placement-add").click();
      await waitView(page, `placement ${p.id}`, s => s.placements.length === i + 1 && !s.error);
    }

    // Every zone is a sight blocker, and the preset alone supplies its kind, role, and heights.
    for (const [i, z] of shipped.zones.entries()) {
      assert.equal(z.kind, "sight_block", "the versus level's zones must all block sight");
      await page.locator("#zone-kind").fill("deck");
      await page.locator("#zone-role").selectOption("solid");
      await page.locator("#zone-sight-preset").click();
      for (const name of ["x", "y", "w", "h"]) await page.locator(`#zone-${name}`).fill(String(z[name]));
      await page.locator("#zone-add").click();
      v = await waitView(page, `blocker ${z.id}`, s => s.zones.length === i + 1 && !s.error);
    }
    assert.deepEqual(v.zones, shipped.zones);

    // The download is the shipped file, byte for byte.
    const [download] = await Promise.all([page.waitForEvent("download"), page.locator("#export").click()]);
    await download.saveAs(output);
    assert.equal(await readFile(output, "utf8"), shippedText, "the export must equal the shipped versus level");
    await waitView(page, "a clean export", s => !s.dirty);

    await chooseLevel(page, output);
    v = await waitView(page, "the reopened download", s => s.status.startsWith("Opened") && !s.error);
    assert.equal(v.sight, radius);
    assert.equal(v.field, String(radius));
    assert.deepEqual(v.zones, shipped.zones);
    assert.deepEqual(dialogs, [], "unexpected browser dialogs");
    assert.deepEqual(errors, []);
  } finally {
    await page.close();
    await rm(directory, { recursive: true, force: true });
  }
}

const output = process.argv[2];
if (output === undefined || process.argv.length > 3) throw new Error("Usage: node scripts/smoke-versus-editor.mjs EXPORT_PATH");

const server = await serveLevelEditor(0);
try {
  const browser = await chromium.launch({ timeout: 15_000 });
  try {
    await smokeVersus(browser, `http://127.0.0.1:${server.address().port}/`, output);
    console.log("Versus editor smoke passed: sight radius and blockers authored through the forms, exported byte for byte, and reopened");
  } finally {
    await browser.close();
  }
} finally {
  await promisify(server.close.bind(server))();
}
