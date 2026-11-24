/** @file Export Beacon assets in the editors and verify the separate game's web build. */

import assert from "node:assert/strict";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { chromium } from "playwright";
import { PNG } from "pngjs";

import { serveLevelEditor } from "./level-editor-server.mjs";
import { servePlayground } from "./playground-server.mjs";
import { serveTileMaker } from "./tile-maker-server.mjs";

const sourceLevel = fileURLToPath(new URL("../examples/beacon/assets/arena.json", import.meta.url));

/**
 * Run a test page against a loopback server and release both afterward.
 * @param {(port: number) => Promise<import("node:http").Server>} serve - Starts a server on the given port.
 * @param {(page: import("playwright").Page) => Promise<void>} check - Page workflow.
 */
async function withPage(serve, check) {
  const server = await serve(0);
  try {
    const browser = await chromium.launch({ timeout: 15_000 });
    try {
      const page = await browser.newPage({ viewport: { width: 800, height: 600 } });
      page.setDefaultTimeout(5_000);
      await page.goto(`http://127.0.0.1:${server.address().port}/`, { timeout: 15_000 });
      await check(page);
    } finally {
      await browser.close();
    }
  } finally {
    await promisify(server.close.bind(server))();
  }
}

/**
 * Move the custom placement in Level Builder and paint a tile from blank.
 * @param {string} output - Scratch asset directory for the exact downloads.
 */
async function author(output) {
  await withPage(serveLevelEditor, async page => {
    await page.waitForFunction(() => !document.querySelector("#open").disabled);
    const [chooser] = await Promise.all([
      page.waitForEvent("filechooser"), page.locator('label[for="open"]').click(),
    ]);
    await chooser.setFiles(sourceLevel);
    const board = page.locator("#board");
    await board.waitFor();
    await page.getByRole("button", { name: "Move placement" }).click();
    await board.press("Enter");
    await board.press("ArrowRight");
    await board.press("Enter");
    const [download] = await Promise.all([
      page.waitForEvent("download"), page.getByRole("button", { name: "Export", exact: true }).click(),
    ]);
    await download.saveAs(join(output, "arena.json"));
  });
  await withPage(serveTileMaker, async page => {
    await page.getByText("Ready. Draw a tile or open a PNG.").waitFor();
    await page.getByLabel("Color", { exact: true }).fill("#44ae91");
    const grid = page.locator("#grid");
    const rect = await grid.boundingBox();
    assert.ok(rect);
    await page.mouse.move(rect.x + rect.width * 4.5 / 32, rect.y + rect.height * 4.5 / 16);
    await page.mouse.down();
    await page.mouse.move(rect.x + rect.width * 20.5 / 32, rect.y + rect.height * 4.5 / 16);
    await page.mouse.up();
    const [download] = await Promise.all([
      page.waitForEvent("download"), page.getByRole("button", { name: "Export PNG" }).click(),
    ]);
    await download.saveAs(join(output, "floor.png"));
  });
}

/**
 * Exercise rendered content and real input in the browser.
 * @param {string} directory - Static bundle directory.
 */
async function play(directory) {
  await withPage(port => servePlayground(port, directory), async page => {
    const canvas = page.locator("#goje");
    const inactive = [91, 141, 209, 255];
    const active = [248, 190, 66, 255];
    /** @param {number[]} beacon - Expected color at the authored placement. */
    async function frame(beacon) {
      const deadline = Date.now() + 5_000;
      do {
        const png = PNG.sync.read(await canvas.screenshot({ timeout: 2_000 }));
        /**
         * Sample a pixel in the logical 160 × 96 viewport.
         * @param {number} x - Logical column.
         * @param {number} y - Logical row.
         * @param {number[]} color - Expected RGBA.
         */
        const pixelIs = (x, y, color) => {
          const offset = (Math.floor(y * png.height / 96) * png.width + Math.floor(x * png.width / 160)) * 4;
          return color.every((channel, i) => png.data[offset + i] === channel);
        };
        if (pixelIs(64, 32, beacon) && pixelIs(32, 32, [52, 69, 87, 255]) &&
            pixelIs(24, 25, [68, 174, 145, 255]) && pixelIs(64, 64, [99, 112, 129, 255])) return;
      } while (Date.now() < deadline);
      assert.fail(`Beacon frame did not show the editor assets and beacon color ${beacon.join(",")}`);
    }
    await frame(inactive);
    const bounds = await canvas.boundingBox();
    assert.ok(bounds);
    await page.mouse.click(bounds.x + bounds.width * 64 / 160, bounds.y + bounds.height * 32 / 96);
    await frame(active);
    await page.keyboard.press("r");
    await frame(inactive);
    await page.keyboard.press("Space");
    await frame(active);
  });
  console.log("Browser Beacon smoke passed: authored art and placement, pointer and keyboard input, and reset");
}

const [mode, directory] = process.argv.slice(2);
if (!directory || process.argv.length !== 4 || !["author", "play"].includes(mode)) {
  throw new Error("Usage: node scripts/smoke-beacon.mjs author|play DIRECTORY");
}
if (mode === "author") await author(directory);
else await play(directory);
