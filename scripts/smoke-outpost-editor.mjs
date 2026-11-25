/**
 * @file Author the outpost's placements and zones in the Level Builder with real file choosers and form input.
 *
 * It strips the shipped outpost level down to its terrain, opens that in Chromium, adds every placement and
 * zone of the shipped level through the editor's forms, exports the result, and checks that the export has the
 * same content as the shipped level, so the editor can rebuild what the playground ships.
 *
 * Usage: node scripts/smoke-outpost-editor.mjs EXPORT_PATH
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

const outpostPath = fileURLToPath(new URL("../cmd/playground/levels/outpost.json", import.meta.url));

/**
 * Read the observable editor view. The script only reads state; all changes come from input.
 * @param {import("playwright").Page} page - Editor page.
 * @returns {Promise<{placements: object[], zones: object[], dirty: boolean, status: string, error: boolean}>} View.
 */
async function view(page) {
  return page.evaluate(() => {
    const status = document.querySelector("#status");
    const state = JSON.parse(JSON.stringify(globalThis.gojeLevelEditor.state()));
    return {
      placements: state.placements.map(p => ({ id: p.id, kind: p.kind, x: p.x, y: p.y })).sort((a, b) => a.id - b.id),
      zones: state.zones, dirty: state.dirty,
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
 * Author the shipped level's placements and zones on its bare terrain.
 * @param {import("playwright").Browser} browser - Chromium instance.
 * @param {string} url - Editor URL.
 * @param {string} output - Path for the download.
 * @returns {Promise<void>} Resolves when every check passes.
 */
async function smokeOutpost(browser, url, output) {
  const shipped = JSON.parse(await readFile(outpostPath, "utf8"));
  const directory = await mkdtemp(join(tmpdir(), "goje-outpost-"));
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
    const terrain = join(directory, "terrain.json");
    await writeFile(terrain, JSON.stringify({ version: 1, rows: shipped.rows, placements: [] }));
    await page.goto(url, { timeout: 15_000 });
    await chooseLevel(page, terrain);
    let v = await waitView(page, "the opened terrain", s => s.status.startsWith("Opened") && !s.error);
    assert.deepEqual([v.placements, v.zones], [[], []], "the terrain must open without placements or zones");

    // Every placement goes in through the form with its authored ID.
    for (const [i, p] of shipped.placements.entries()) {
      await page.locator("#placement-kind").fill(p.kind);
      await page.locator("#placement-id").fill(String(p.id));
      await page.locator("#placement-x").fill(String(p.x));
      await page.locator("#placement-y").fill(String(p.y));
      await page.locator("#placement-add").click();
      v = await waitView(page, `placement ${p.id}`, s => s.placements.length === i + 1 && !s.error);
    }

    // Every zone goes in through the zone form.
    for (const [i, z] of shipped.zones.entries()) {
      await page.locator("#zone-kind").fill(z.kind);
      await page.locator("#zone-role").selectOption(z.role);
      for (const name of ["x", "y", "w", "h", "bottom", "top"]) await page.locator(`#zone-${name}`).fill(String(z[name]));
      await page.locator("#zone-add").click();
      v = await waitView(page, `zone ${z.kind}`, s => s.zones.length === i + 1 && !s.error);
    }

    // The export has the shipped level's content; the editor may order placements differently.
    const [download] = await Promise.all([page.waitForEvent("download"), page.locator("#export").click()]);
    await download.saveAs(output);
    const exported = JSON.parse(await readFile(output, "utf8"));
    const byID = list => [...list].sort((a, b) => a.id - b.id);
    assert.equal(exported.version, shipped.version);
    assert.deepEqual(exported.rows, shipped.rows);
    assert.deepEqual(byID(exported.placements), byID(shipped.placements));
    assert.deepEqual(byID(exported.zones), byID(shipped.zones));
    await waitView(page, "a clean export", s => !s.dirty);
    await chooseLevel(page, output);
    v = await waitView(page, "the reopened download", s => s.status.startsWith("Opened") && !s.error);
    assert.deepEqual(v.placements, byID(shipped.placements));
    assert.deepEqual(byID(v.zones), byID(shipped.zones));
    assert.deepEqual(dialogs, [], "unexpected browser dialogs");
    assert.deepEqual(errors, []);
  } finally {
    await page.close();
    await rm(directory, { recursive: true, force: true });
  }
}

const output = process.argv[2];
if (output === undefined || process.argv.length > 3) throw new Error("Usage: node scripts/smoke-outpost-editor.mjs EXPORT_PATH");

const server = await serveLevelEditor(0);
try {
  const browser = await chromium.launch({ timeout: 15_000 });
  try {
    await smokeOutpost(browser, `http://127.0.0.1:${server.address().port}/`, output);
    console.log("Outpost editor smoke passed: placements and zones authored through the forms, exported, and reopened");
  } finally {
    await browser.close();
  }
} finally {
  await promisify(server.close.bind(server))();
}
