/** @file Edit a glyph in the Font Editor in Chromium, then check the download with fontcheck and a scratch game through font.Decode. */

import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { chromium } from "playwright";
import { PNG } from "pngjs";

import { serveFontEditor } from "./font-editor-server.mjs";

const root = fileURLToPath(new URL("..", import.meta.url));
const go = process.env.GO ?? "go";

// The built-in 'A' in its 6 × 8 cell, before and after the edits below.
const builtinA = [".###..", "#...#.", "#...#.", "#####.", "#...#.", "#...#.", "#...#.", "......"];
const editedA = ["..##.#", "#...##", "#...##", "######", "#...#.", "#...##", "#.#.#.", "......"];

// A scratch game draws text with the exported pair through font.Decode into a
// software canvas and prints the alpha it covered, one row per line.
const scratchGame = `package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"strings"

	"github.com/krapht-foundation/goje/engine"
	"github.com/krapht-foundation/goje/font"
)

type canvas struct {
	engine.Canvas
	sheet  image.Image
	pixels *image.NRGBA
}

func (c *canvas) ImageTinted(source image.Rectangle, x, y, w, h float32, tint color.NRGBA) {
	for dy := range int(h) {
		for dx := range int(w) {
			at := source.Min.Add(image.Pt(dx*source.Dx()/int(w), dy*source.Dy()/int(h)))
			texel := color.NRGBAModel.Convert(c.sheet.At(at.X, at.Y)).(color.NRGBA)
			texel.A = uint8(int(texel.A) * int(tint.A) / 255)
			c.pixels.SetNRGBA(int(x)+dx, int(y)+dy, texel)
		}
	}
}

func main() {
	sheet, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	description, err := os.Open(os.Args[2])
	if err != nil {
		panic(err)
	}
	face, err := font.Decode(sheet, description)
	if err != nil {
		panic(err)
	}
	text := os.Args[3]
	width, height := face.Measure(text, 1)
	c := &canvas{sheet: face.Image(), pixels: image.NewNRGBA(image.Rect(0, 0, width, height))}
	face.Draw(c, text, 0, 0, 1, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	for y := range height {
		var row strings.Builder
		for x := range width {
			if c.pixels.NRGBAAt(x, y).A == 0 {
				row.WriteByte('.')
			} else {
				row.WriteByte('#')
			}
		}
		fmt.Println(row.String())
	}
}
`;

/**
 * Read a whole canvas backing store without writing app state.
 * @param {import("playwright").Page} page - Running editor.
 * @param {string} selector - Canvas selector.
 * @returns {Promise<{width: number, height: number, data: number[]}>} Size and packed RGBA rows.
 */
function canvasData(page, selector) {
  return page.locator(selector).evaluate(canvas => ({
    width: canvas.width,
    height: canvas.height,
    data: Array.from(canvas.getContext("2d").getImageData(0, 0, canvas.width, canvas.height).data),
  }));
}

/**
 * Describe pixels as rows of '#' for opaque white and '.' for transparent.
 * @param {{width: number, height: number, data: ArrayLike<number>}} image - RGBA pixels.
 * @param {number} [left] - First column.
 * @param {number} [top] - First row.
 * @param {number} [width] - Columns to describe.
 * @param {number} [height] - Rows to describe.
 * @returns {string[]} One string per row; any other color fails.
 */
function rows(image, left = 0, top = 0, width = image.width, height = image.height) {
  return Array.from({ length: height }, (_, y) => Array.from({ length: width }, (_, x) => {
    const at = ((top + y) * image.width + left + x) * 4;
    const rgba = [0, 1, 2, 3].map(i => image.data[at + i]).join();
    if (rgba === "255,255,255,255") return "#";
    assert.equal(rgba.split(",")[3], "0", `pixel (${left + x}, ${top + y}) is neither white nor transparent`);
    return ".";
  }).join(""));
}

/**
 * Wait for the status line to change after an action.
 * @param {import("playwright").Page} page - Running editor.
 * @param {() => Promise<unknown>} action - Action expected to report a new status.
 * @returns {Promise<string>} New status text.
 */
async function statusAfter(page, action) {
  const before = await page.locator("#status").textContent();
  await action();
  await page.waitForFunction(previous => document.querySelector("#status").textContent !== previous, before);
  return /** @type {string} */ (await page.locator("#status").textContent());
}

/**
 * Click a download button and return the file the browser received.
 * @param {import("playwright").Page} page - Running editor with downloads enabled.
 * @param {string} name - Button name.
 * @returns {Promise<{name: string, bytes: Buffer}>} Suggested name and exact bytes.
 */
async function download(page, name) {
  const [event] = await Promise.all([page.waitForEvent("download"), page.getByRole("button", { name, exact: true }).click()]);
  return { name: event.suggestedFilename(), bytes: await readFile(await event.path()) };
}

/**
 * Choose files through the editor's file input.
 * @param {import("playwright").Page} page - Running editor.
 * @param {{name: string, bytes: Buffer}[]} files - Selected files.
 * @returns {Promise<void>} Resolves when the input accepts the files.
 */
function choose(page, files) {
  return page.locator("#files").setInputFiles(files.map(({ name, bytes }) => ({
    name, buffer: bytes, mimeType: name.endsWith(".png") ? "image/png" : "application/json",
  })));
}

/**
 * Edit the built-in 'A' by keyboard and pointer, download the pair, and reopen it.
 * @param {import("playwright").Page} page - Isolated test page with downloads enabled.
 * @param {string[]} unexpected - Messages of dialogs the editor should not have shown.
 * @returns {Promise<{png: {name: string, bytes: Buffer}, json: {name: string, bytes: Buffer}}>} The downloaded pair.
 */
async function editBuiltin(page, unexpected) {
  const button = /** @param {string} name - Accessible name. */ name => page.getByRole("button", { name, exact: true });
  const grid = page.locator("#grid");
  const dirty = page.locator("#dirty");
  assert.equal(await statusAfter(page, () => button("Built-in font").click()), "Opened the built-in font.");
  assert.equal(await dirty.textContent(), "Saved");
  const sheet = await canvasData(page, "#sheet");
  assert.deepEqual([sheet.width, sheet.height], [96, 48]);

  await page.getByLabel("Character").fill("A");
  assert.match(await page.locator("#glyph").textContent(), /U\+0041/);
  assert.deepEqual(rows(await canvasData(page, "#art")), builtinA, "grid shows the built-in A");

  // Keyboard: paint (2, 6), then erase (1, 0).
  await grid.focus();
  for (const key of ["ArrowRight", "ArrowRight", ...Array(6).fill("ArrowDown")]) await page.keyboard.press(key);
  assert.equal(await page.locator("#coordinate").textContent(), "(2, 6)");
  await page.keyboard.press("Enter");
  assert.equal(await dirty.textContent(), "Unsaved changes");
  await button("Eraser").click();
  await grid.focus();
  for (const key of ["ArrowLeft", ...Array(6).fill("ArrowUp")]) await page.keyboard.press(key);
  await page.keyboard.press("Enter");

  // Pointer: one stroke down column 5 that leaves the cell between rows 3 and 5.
  await button("Pencil").click();
  const box = await grid.boundingBox();
  assert.ok(box);
  const move = /** @param {number} x - Column. @param {number} y - Row. */ (x, y) =>
    page.mouse.move(box.x + box.width * (x + 0.5) / 6, box.y + box.height * (y + 0.5) / 8);
  await move(5, 0);
  await page.mouse.down();
  await move(5, 3);
  await move(8, 3);
  await move(5, 5);
  await page.mouse.up();
  // Escape cancels a stroke along the empty bottom row.
  await move(0, 7);
  await page.mouse.down();
  await move(4, 7);
  await page.keyboard.press("Escape");
  await page.mouse.up();
  // An erase stroke, undone, redone, and undone again.
  await button("Eraser").click();
  await move(5, 0);
  await page.mouse.down();
  await move(5, 1);
  await page.mouse.up();
  assert.equal(rows(await canvasData(page, "#art"))[0], "..##..");
  await button("Undo").click();
  await button("Redo").click();
  assert.equal(rows(await canvasData(page, "#art"))[1], "#...#.");
  await button("Undo").click();
  assert.deepEqual(rows(await canvasData(page, "#art")), editedA, "keyboard, pointer, and history edits");

  // The neighboring glyphs are untouched.
  const edited = await canvasData(page, "#sheet");
  assert.deepEqual(rows(edited, 0, 16, 6, 8), rows(sheet, 0, 16, 6, 8), "'@' unchanged");
  assert.deepEqual(rows(edited, 12, 16, 6, 8), rows(sheet, 12, 16, 6, 8), "'B' unchanged");
  assert.deepEqual(rows(edited, 6, 16, 6, 8), editedA, "sheet preview shows the edit");

  const png = await download(page, "Download PNG");
  const json = await download(page, "Download JSON");
  assert.deepEqual([png.name, json.name], ["goje-font.png", "goje-font.json"]);
  assert.equal(json.bytes.toString(), '{"version":1,"cellWidth":6,"cellHeight":8,"baseline":7,"first":32,"count":95}\n');
  const decoded = PNG.sync.read(png.bytes);
  assert.deepEqual(Array.from(decoded.data), edited.data, "the PNG holds exactly the sheet the editor shows");
  assert.equal(await dirty.textContent(), "Saved");

  // Reopening the exact pair needs no prompt and shows the same pixels.
  assert.equal(await statusAfter(page, () => choose(page, [png, json])), "Opened goje-font.png and goje-font.json.");
  assert.deepEqual(unexpected, []);
  assert.deepEqual((await canvasData(page, "#sheet")).data, edited.data);
  assert.equal(await dirty.textContent(), "Saved");
  assert.equal(await page.locator("#undo").isDisabled(), true);
  return { png, json };
}

/**
 * Check that rejected inputs leave the open font alone, and create a blank font.
 * @param {import("playwright").Page} page - Running editor with a saved font open.
 * @param {{png: {name: string, bytes: Buffer}}} pair - A valid sheet to reuse.
 * @param {string[]} unexpected - Messages of dialogs the editor should not have shown.
 * @param {{accept: boolean, resolve: (message: string) => void}[]} answers - Pending dialog answers.
 */
async function rejectsAndCreates(page, pair, unexpected, answers) {
  const before = await canvasData(page, "#sheet");
  const proportional = {
    name: "narrow.json",
    bytes: Buffer.from('{"version":1,"cellWidth":6,"cellHeight":8,"baseline":7,"first":32,"count":2,"advances":[3,6]}'),
  };
  assert.match(await statusAfter(page, () => choose(page, [pair.png, proportional])), /monospaced/);
  assert.match(await statusAfter(page, () => choose(page, [pair.png])), /one PNG sheet and one JSON/);
  assert.deepEqual(unexpected, [], "rejected imports do not prompt");
  assert.deepEqual(await canvasData(page, "#sheet"), before, "rejected imports keep the font");

  // A dismissed replacement keeps unsaved work.
  await page.getByRole("button", { name: "Pencil", exact: true }).click();
  await page.locator("#grid").focus();
  await page.keyboard.press("Enter");
  const prompt = new Promise(resolve => answers.push({ accept: false, resolve }));
  await page.getByRole("button", { name: "Built-in font", exact: true }).click();
  assert.match(String(await prompt), /Discard unsaved changes/);
  assert.equal(await page.locator("#dirty").textContent(), "Unsaved changes");

  for (const [label, value] of [["Cell width", "8"], ["Cell height", "10"], ["Baseline", "8"], ["First code point", "65"], ["Glyphs", "3"]]) {
    await page.getByLabel(label, { exact: true }).fill(value);
  }
  const accept = new Promise(resolve => answers.push({ accept: true, resolve }));
  const status = await statusAfter(page, () => page.getByRole("button", { name: "New font", exact: true }).click());
  assert.match(String(await accept), /Discard unsaved changes/);
  assert.equal(status, "Created a blank font of 3 glyphs in 8 × 10 cells.");
  const blank = await canvasData(page, "#sheet");
  assert.deepEqual([blank.width, blank.height], [24, 10]);
  assert.ok(blank.data.every(value => value === 0), "a new font is transparent");
  await page.getByRole("button", { name: "Next", exact: true }).click();
  assert.match(await page.locator("#glyph").textContent(), /U\+0042/);
  const json = await download(page, "Download JSON");
  assert.equal(json.bytes.toString(), '{"version":1,"cellWidth":8,"cellHeight":10,"baseline":8,"first":65,"count":3}\n');
}

/**
 * Validate the downloaded pair with fontcheck and draw it in a scratch game.
 * @param {{png: {bytes: Buffer}, json: {bytes: Buffer}}} pair - Downloaded font.
 */
async function runtime(pair) {
  await mkdir(join(root, ".build"), { recursive: true });
  const scratch = await mkdtemp(join(root, ".build", "font-editor-smoke."));
  try {
    const png = join(scratch, "font.png");
    const json = join(scratch, "font.json");
    await writeFile(png, pair.png.bytes);
    await writeFile(json, pair.json.bytes);
    const check = execFileSync(go, ["run", "./cmd/fontcheck", png, json], { cwd: root, encoding: "utf8" });
    assert.match(check, /valid font sheet \(96 × 48 pixels, line height 8\)/);
    await writeFile(join(scratch, "main.go"), scratchGame);
    const drawn = execFileSync(go, ["run", join(scratch, "main.go"), png, json, "AB"], { cwd: root, encoding: "utf8" });
    const sheet = PNG.sync.read(pair.png.bytes);
    const want = rows(sheet, 6, 16, 12, 8);
    assert.deepEqual(drawn.trimEnd().split("\n"), want, "font.Decode draws the exported glyphs");
    assert.deepEqual(want.map(row => row.slice(0, 6)), editedA);
  } finally {
    await rm(scratch, { recursive: true, force: true });
  }
}

if (process.argv.length > 2) throw new Error("Usage: node scripts/smoke-font-editor.mjs");

const server = await serveFontEditor(0);
try {
  const browser = await chromium.launch({ timeout: 15_000 });
  try {
    const page = await browser.newPage({ acceptDownloads: true, viewport: { width: 1280, height: 1100 } });
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
    assert.equal(await page.locator("#dirty").textContent(), "No font open");
    const pair = await editBuiltin(page, unexpected);
    await rejectsAndCreates(page, pair, unexpected, answers);
    assert.deepEqual(unexpected, []);
    await runtime(pair);
    console.log("Font Editor smoke passed: keyboard and pointer glyph edits, history, download, reopen, rejection, new font, fontcheck, and font.Decode drawing");
  } finally {
    await browser.close();
  }
} finally {
  await promisify(server.close.bind(server))();
}
