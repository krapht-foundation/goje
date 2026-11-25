/** @file Export Beacon assets in the editors and verify the separate game's web build. */

import assert from "node:assert/strict";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { chromium } from "playwright";
import { PNG } from "pngjs";

import { serveLevelEditor } from "./level-editor-server.mjs";
import { serveAssets, servePlayground } from "./playground-server.mjs";
import { serveTileMaker } from "./tile-maker-server.mjs";

const sourceLevel = fileURLToPath(new URL("../examples/beacon/assets/arena.json", import.meta.url));

/**
 * Run a test page against a loopback server and release both afterward.
 * @param {(port: number) => Promise<import("node:http").Server>} serve - Starts a server on the given port.
 * @param {(page: import("playwright").Page) => Promise<void>} check - Page workflow.
 * @param {{ height?: number, clock?: boolean }} [options] - Viewport height and whether to fake page time.
 */
async function withPage(serve, check, { height = 600, clock = false } = {}) {
  const server = await serve(0);
  try {
    const browser = await chromium.launch({ timeout: 15_000 });
    try {
      const page = await browser.newPage({ viewport: { width: 800, height } });
      page.setDefaultTimeout(5_000);
      if (clock) await page.clock.install();
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

/** @type {Map<string, [string, string]>} Files of a `goje build` web directory. */
const buildRoutes = new Map([
  ["/", ["index.html", "text/html; charset=utf-8"]],
  ["/wasm_exec.js", ["wasm_exec.js", "text/javascript; charset=utf-8"]],
  ["/game.wasm", ["game.wasm", "application/wasm"]],
  ["/manifest.webmanifest", ["manifest.webmanifest", "application/manifest+json"]],
  ["/icon-512.png", ["icon-512.png", "image/png"]],
]);

/**
 * Exercise rendered content and real input in the browser.
 * @param {string} directory - Static bundle directory.
 * @param {boolean} [sprite=false] - Check only the edited move clip's timing and reset.
 * @param {boolean} [build=false] - Serve a `goje build` web directory instead of the Beacon page.
 */
async function play(directory, sprite = false, build = false) {
  // The goje shell's canvas fills the page, so a 5:4 page keeps the 160 × 128 game unletterboxed.
  const serve = build
    ? port => serveAssets(port, directory, buildRoutes)
    : port => servePlayground(port, directory);
  await withPage(serve, async page => {
    const canvas = page.locator("#goje");
    const inactive = [91, 141, 209, 255], active = [248, 190, 66, 255];
    const background = [20, 28, 41, 255], eye = [244, 241, 235, 255];
    const lid = [215, 84, 73, 255], outline = [36, 38, 46, 255];
    // The tomato's anchor (bottom center) starts at the lane's left end on this ground line.
    const ground = 112, start = 32, end = 128;
    let paused = false;

    /**
     * Decoded canvas screenshot sampled in the logical 160 × 128 viewport.
     * @typedef {{
     *   is: (x: number, y: number, color: number[]) => boolean,
     *   eyes: () => ({ x: number, y: number } | null),
     * }} Shot
     */

    /** @returns {Promise<Shot>} The presented frame. */
    async function shoot() {
      const png = PNG.sync.read(await canvas.screenshot({ timeout: 2_000 }));
      /**
       * @param {number} x - Logical column.
       * @param {number} y - Logical row.
       * @param {number[]} color - Expected RGBA.
       * @returns {boolean} Whether the center of the logical pixel matches.
       */
      const is = (x, y, color) => {
        const offset = (Math.floor((y + 0.5) * png.height / 128) * png.width +
          Math.floor((x + 0.5) * png.width / 160)) * 4;
        return color.every((channel, i) => png.data[offset + i] === channel);
      };
      /** @returns {{ x: number, y: number } | null} Upper-left corner of the eye-white bounds in the lane. */
      const eyes = () => {
        let left = Infinity, top = Infinity;
        for (let y = 80; y < 128; y++) {
          for (let x = 16; x < 144; x++) {
            if (is(x, y, eye)) [left, top] = [Math.min(left, x), Math.min(top, y)];
          }
        }
        return left === Infinity ? null : { x: left, y: top };
      };
      return { is, eyes };
    }

    /**
     * Poll presented frames. While page time is paused, advance it two ticks
     * before each capture, so animation follows game time rather than capture speed.
     * @param {string} description - Expected state for the failure message.
     * @param {(shot: Shot) => boolean} expected - Frame predicate.
     */
    async function frame(description, expected) {
      const deadline = Date.now() + 5_000;
      do {
        if (paused) await page.clock.runFor(32);
        if (expected(await shoot())) return;
      } while (Date.now() < deadline);
      assert.fail(`Beacon frame did not show ${description}`);
    }

    /**
     * @param {Shot} shot - Presented frame.
     * @param {number[]} beacon - Expected color at the authored placement.
     * @returns {boolean} Whether the editor-authored tile, wall, and moved beacon appear.
     */
    const board = (shot, beacon) => shot.is(64, 32, beacon) && shot.is(32, 32, [52, 69, 87, 255]) &&
      shot.is(24, 25, [68, 174, 145, 255]) && shot.is(64, 64, [99, 112, 129, 255]);
    // Every tomato frame puts its eye-white bounds 7 px left of the anchor.
    // Their top is 11 px above the ground when standing and 15 px when hopping.
    /**
     * @param {Shot} shot - Presented frame.
     * @param {number} x - Anchor column.
     * @returns {boolean} Whether the open-eyed tomato stands on the ground line at x.
     */
    const standing = (shot, x) => {
      const at = shot.eyes();
      return at?.x === x - 7 && at.y === ground - 11 && shot.is(x, ground - 1, outline);
    };
    /**
     * @param {Shot} shot - Presented frame.
     * @returns {boolean} Whether the tomato hops right of the start with its anchor column clear.
     */
    const hopping = shot => {
      const at = shot.eyes();
      return at !== null && at.x + 7 > start && at.y === ground - 15 &&
        [1, 2, 3, 4].every(lift => shot.is(at.x + 7, ground - lift, background));
    };
    const cyan = [22, 216, 255, 255];
    /**
     * The painted stripe crosses the hop center 10 pixels above the ground at native scale.
     * @param {Shot} shot - Presented frame.
     * @param {number} x - Anchor column.
     * @returns {boolean} Whether the stripe reaches its expected center.
     */
    const painted = (shot, x) => shot.is(x, ground - 10, cyan);

    if (sprite) {
      await frame("the tomato at the lane start", shot => standing(shot, start));
      await page.clock.pauseAt(await page.evaluate(() => Date.now() + 1_000));
      paused = true;
      await canvas.focus();
      // Drain the time skipped by the pause before motion starts.
      await frame("the tomato still at the lane start", shot => standing(shot, start));
      // The edited move clip is hop (16 ticks), windup, standing. The original
      // landed on motion tick 13; the edit stays airborne through tick 16.
      /** @type {Map<number, [string, (shot: Shot, x: number) => boolean]>} */
      const checkpoints = new Map([
        [13, ["airborne and painted where the original clip landed", (shot, x) => hopping(shot) && painted(shot, x)]],
        [16, ["airborne and painted on the last hop tick", (shot, x) => hopping(shot) && painted(shot, x)]],
        [17, ["planted on the unpainted windup pose", (shot, x) => shot.eyes()?.y === ground - 10 &&
          shot.is(x, ground - 1, outline) && shot.is(x, ground - 2, outline) &&
          !painted(shot, x)]],
      ]);
      await page.keyboard.down("ArrowRight");
      try {
        // One 16 ms browser frame runs at most one 60 Hz tick. The anchor
        // moves 1 px per motion tick, so its rendered column names the tick.
        for (let tick = 0, steps = 0; tick < 17; steps++) {
          assert.ok(steps < 40, `the tomato stalled at motion tick ${tick}`);
          await page.clock.runFor(16);
          const shot = await shoot();
          const x = (shot.eyes()?.x ?? NaN) + 7;
          assert.ok(x - start === tick || x - start === tick + 1,
            `motion tick jumped from ${tick} to ${x - start}`);
          tick = x - start;
          const checkpoint = checkpoints.get(tick);
          if (checkpoint) assert.ok(checkpoint[1](shot, x), `motion tick ${tick}: tomato not ${checkpoint[0]}`);
        }
      } finally {
        await page.keyboard.up("ArrowRight");
      }
      await page.keyboard.press("r");
      await frame("the tomato restored to the lane start by reset", shot => standing(shot, start) &&
        shot.is(start + 17, ground - 1, background) && shot.is(start + 17, ground - 2, background));
      return;
    }

    await frame("the editor assets, an unlit beacon, and the tomato at the lane start",
      shot => board(shot, inactive) && standing(shot, start));
    // Freeze page time; frames now advance only when the smoke steps the clock.
    await page.clock.pauseAt(await page.evaluate(() => Date.now() + 1_000));
    paused = true;
    await canvas.focus();
    const bounds = await canvas.boundingBox();
    assert.ok(bounds);
    await page.mouse.click(bounds.x + bounds.width * 64 / 160, bounds.y + bounds.height * 32 / 128);
    await frame("a beacon lit by a click", shot => board(shot, active) && standing(shot, start));
    await page.keyboard.press("r");
    await frame("an unlit beacon after reset", shot => board(shot, inactive) && standing(shot, start));
    await page.keyboard.press("Space");
    await frame("a beacon lit by Space", shot => board(shot, active));

    await page.keyboard.down("ArrowRight");
    try {
      await frame("the tomato hopping right on its ground line", hopping);
      // The lane is 96 px at one pixel per tick; at its end the tomato idles.
      await page.clock.runFor(1_700);
      await frame("the tomato idle at the lane end", shot => board(shot, active) && standing(shot, end));
    } finally {
      await page.keyboard.up("ArrowRight");
    }

    await page.keyboard.press("r");
    await frame("the beacon and tomato restored by reset",
      shot => board(shot, inactive) && standing(shot, start) && !shot.is(end, ground - 1, outline));
    // Idle shows open eyes for 114 ticks, then blinks for 6. Stay inside the
    // open-eyed span, then step two ticks at a time so the blink cannot be skipped.
    await page.clock.runFor(1_400);
    assert.ok(standing(await shoot(), start), "the tomato blinked early or moved while idle");
    await frame("the tomato blinking in place", shot => shot.eyes() === null &&
      shot.is(start - 7, ground - 10, lid) && shot.is(start, ground - 1, outline));
    await frame("the tomato reopening its eyes", shot => standing(shot, start));
  }, { height: build ? 640 : 720, clock: true });
  console.log(sprite
    ? "Browser Beacon sprite smoke passed: edited hop airborne with cyan paint at motion ticks 13 and 16, " +
      "unpainted planted windup at tick 17, and reset"
    : "Browser Beacon smoke passed: authored art and placement, pointer and keyboard input, " +
      "tomato blink and hop on a stable anchor, and reset");
}

const [mode, directory] = process.argv.slice(2);
if (!directory || process.argv.length !== 4 || !["author", "play", "sprite", "build"].includes(mode)) {
  throw new Error("Usage: node scripts/smoke-beacon.mjs author|play|sprite|build DIRECTORY");
}
if (mode === "author") await author(directory);
else await play(directory, mode === "sprite", mode === "build");
