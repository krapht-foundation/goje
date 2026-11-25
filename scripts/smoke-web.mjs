/** @file Verify the reference scenes with real Chromium input and rendered canvas pixels. */

import assert from "node:assert/strict";

import { chromium } from "playwright";
import { PNG } from "pngjs";

import { servePlayground } from "./playground-server.mjs";

/**
 * @typedef {object} SpriteCenter
 * @property {number} x - Horizontal position in screenshot pixels.
 * @property {number} y - Vertical position in screenshot pixels.
 */

/**
 * Find the bounds of one solid-color sprite in a rendered frame.
 * @param {PNG} frame - Decoded canvas screenshot.
 * @param {[number, number, number]} rgb - Target RGB color.
 * @returns {SpriteCenter | null} Sprite center, or null if the color is absent.
 */
function spriteCenter({ data, width }, [r, g, b]) {
  let left = width, right = 0, top = Infinity, bottom = 0, count = 0;
  for (let pixel = 0; pixel < data.length; pixel += 4) {
    if (Math.abs(data[pixel] - r) > 8 || Math.abs(data[pixel + 1] - g) > 8 ||
        Math.abs(data[pixel + 2] - b) > 8 || data[pixel + 3] !== 255) continue;
    const index = pixel / 4;
    const x = index % width, y = Math.floor(index / width);
    left = Math.min(left, x);
    right = Math.max(right, x);
    top = Math.min(top, y);
    bottom = Math.max(bottom, y);
    count++;
  }
  return count >= 64 ? { x: (left + right) / 2, y: (top + bottom) / 2 } : null;
}

/**
 * Read the player and token positions from one painted browser frame.
 * @param {import("playwright").Locator} canvas - The game canvas.
 * @returns {Promise<{ player: SpriteCenter | null, token: SpriteCenter | null }>} Visible sprites.
 */
async function readSprites(canvas) {
  const frame = PNG.sync.read(await canvas.screenshot({ timeout: 3_000 }));
  return { player: spriteCenter(frame, [248, 164, 76]), token: spriteCenter(frame, [92, 220, 210]) };
}

/**
 * Poll screenshots until both sprite positions meet a condition in the same frame.
 * @param {import("playwright").Locator} canvas - Locator for the rendered playground canvas.
 * @param {string} stage - Description for a timeout error.
 * @param {function(SpriteCenter | null, SpriteCenter | null): boolean} matches - Frame predicate.
 * @returns {Promise<void>} Resolves when the matching frame is rendered.
 * @throws {Error} The returned promise rejects if there is no matching frame before the deadline.
 */
async function waitForFrame(canvas, stage, matches) {
  const deadline = Date.now() + 8_000;
  while (Date.now() < deadline) {
    const { player, token } = await readSprites(canvas);
    if (matches(player, token)) return;
  }
  throw new Error(`No matching playground frame for ${stage}`);
}

/**
 * Check a rendered center against logical coordinates after viewport scaling.
 * @param {SpriteCenter | null} sprite - Detected center, if present.
 * @param {number} x - Expected horizontal position in logical game pixels.
 * @param {number} y - Expected vertical position in logical game pixels.
 * @param {number} scale - Screenshot pixels per logical game pixel.
 * @param {number} [left=0] - Horizontal start of the letterboxed viewport in screenshot pixels.
 * @returns {boolean} Whether the sprite is within two screenshot pixels of the target.
 */
function at(sprite, x, y, scale, left = 0) {
  return sprite !== null && Math.abs(sprite.x - (left + x * scale)) <= 2 &&
    Math.abs(sprite.y - y * scale) <= 2;
}

/**
 * Wait for a courtyard checkpoint using the rendered pixels at DPR 2.
 * @param {import("playwright").Locator} canvas - Goje canvas.
 * @param {string} stage - Checkpoint name for failures.
 * @param {function(PNG): boolean} matches - Whether the checkpoint is visible.
 * @returns {Promise<void>} Resolves after a matching frame is painted.
 */
async function waitCourtyard(canvas, stage, matches) {
  const deadline = Date.now() + 8_000;
  while (Date.now() < deadline) {
    if (matches(PNG.sync.read(await canvas.screenshot({ timeout: 3_000 })))) return;
  }
  throw new Error(`No matching courtyard frame for ${stage}`);
}

/**
 * Compare the rendered color at a canvas screenshot pixel.
 * @param {PNG} frame - Decoded screenshot.
 * @param {number} x - Horizontal device pixel.
 * @param {number} y - Vertical device pixel.
 * @param {[number, number, number]} color - Expected RGB.
 * @returns {boolean} Whether the pixel matches.
 */
function pixelIs(frame, x, y, color) {
  const offset = (Math.floor(y) * frame.width + Math.floor(x)) * 4;
  return frame.data[offset + 3] === 255 &&
    color.every((channel, i) => Math.abs(frame.data[offset + i] - channel) <= 2);
}

/**
 * Drive the M2 patrol with real Chromium keyboard and mouse events.
 * @param {import("playwright").Browser} browser - Running Chromium instance.
 * @param {string} url - Loopback playground URL.
 * @returns {Promise<void>} Resolves once both world and selection are restored.
 */
async function smokeCourtyard(browser, url) {
  const page = await browser.newPage({ viewport: { width: 640, height: 400 }, deviceScaleFactor: 2 });
  try {
    await page.goto(url, { timeout: 15_000 });
    const canvas = page.locator("#goje");
    const amber = [248, 164, 76], teal = [92, 220, 210], wall = [148, 122, 105];
    const selected = [255, 235, 87];
    await waitCourtyard(canvas, "spawn overlap", frame =>
      pixelIs(frame, 640, 352, teal) && pixelIs(frame, 640, 336, wall) && pixelIs(frame, 620, 336, amber));
    await page.mouse.click(320, 176);
    await waitCourtyard(canvas, "front unit selected", frame => pixelIs(frame, 640, 324, selected));
    await page.mouse.click(320, 168);
    await waitCourtyard(canvas, "wall blocks rear unit", frame => pixelIs(frame, 640, 324, wall));
    await page.mouse.click(310, 168);
    await waitCourtyard(canvas, "exposed rear unit selected", frame => pixelIs(frame, 640, 308, selected));

    await page.setViewportSize({ width: 880, height: 500 });
    // At DPR 2, the resized view begins 80 device pixels in and scales by 5.
    await waitCourtyard(canvas, "resized selection and letterbox", frame =>
      frame.width === 1760 && pixelIs(frame, 20, 500, [0, 0, 0]) && pixelIs(frame, 880, 385, selected));
    await page.mouse.click(10, 250);
    await page.keyboard.press("w");
    await waitCourtyard(canvas, "centered zoom preserves selection after a bar click", frame =>
      pixelIs(frame, 880, 360, selected) && pixelIs(frame, 904, 360, selected) &&
      !pixelIs(frame, 880, 385, selected));

    await page.keyboard.down("ArrowRight");
    try {
      await waitCourtyard(canvas, "camera pans right", frame => {
        const marker = spriteCenter(frame, selected);
        return marker !== null && marker.x < 850;
      });
    } finally {
      await page.keyboard.up("ArrowRight");
    }
    await page.keyboard.press("r");
    await waitCourtyard(canvas, "camera and selection reset", frame =>
      pixelIs(frame, 880, 440, teal) && pixelIs(frame, 880, 420, wall) && !pixelIs(frame, 880, 385, selected));
    await page.keyboard.press("Space");
    await waitCourtyard(canvas, "patrol moves the unit away from spawn", frame => {
      const unit = spriteCenter(frame, teal);
      return unit !== null && unit.x < 855;
    });
    await page.keyboard.press("r");
    await waitCourtyard(canvas, "patrol reset restores starting overlap", frame =>
      pixelIs(frame, 880, 440, teal) && pixelIs(frame, 880, 420, wall) && pixelIs(frame, 855, 420, amber));
  } finally {
    await page.close();
  }
}

/**
 * Walk the free-movement yard with real keyboard input while page time is paused,
 * so each step advances about one 60 Hz tick regardless of screenshot speed.
 * Logical coordinates are 320 × 200; the 640 × 400 CSS viewport at DPR 2 shows four
 * device pixels per logical pixel. The camera is centered on the 9 × 7 board, so a ground
 * point (x, y) in tiles appears at logical (16 (x − y) + 144, 8 (x + y) + 36).
 * @param {import("playwright").Browser} browser - Running Chromium instance.
 * @param {string} url - Loopback yard URL.
 * @returns {Promise<void>} Resolves after sliding, the doorway, the deck, reset, and focus loss.
 */
async function smokeYard(browser, url) {
  const page = await browser.newPage({ viewport: { width: 640, height: 400 }, deviceScaleFactor: 2 });
  try {
    const errors = [];
    page.on("pageerror", error => errors.push(error.message));
    await page.clock.install();
    await page.goto(url, { timeout: 15_000 });
    const canvas = page.locator("#goje");
    const footprint = [255, 0, 255], deck = [186, 142, 92], wall = [148, 122, 105];
    const lampDark = [74, 64, 86], lampLit = [255, 224, 102];
    const scale = 4;
    /** @returns {Promise<PNG>} The presented frame. */
    const shoot = async () => PNG.sync.read(await canvas.screenshot({ timeout: 3_000 }));
    /**
     * @param {PNG} frame - Decoded screenshot.
     * @param {number} x - Logical column.
     * @param {number} y - Logical row.
     * @param {number[]} color - Expected RGB.
     * @returns {boolean} Whether the logical pixel's center has the color.
     */
    const is = (frame, x, y, color) => pixelIs(frame, x * scale + scale / 2, y * scale + scale / 2, color);
    /**
     * Locate the footprint outline and its center marker as a ground position.
     * @param {PNG} frame - Decoded screenshot.
     * @returns {{ x: number, y: number } | null} Footprint center in tiles, or null if absent.
     */
    const scout = frame => {
      const center = spriteCenter(frame, footprint);
      if (center === null) return null;
      const screenX = center.x / scale, screenY = center.y / scale;
      const sum = (screenY - 36) / 8, difference = (screenX - 144) / 16;
      return { x: (sum + difference) / 2, y: (sum - difference) / 2 };
    };
    /** @param {PNG} frame - Decoded screenshot. @returns {boolean} Whether the scene shows its spawn. */
    const atSpawn = frame => {
      const at = scout(frame);
      return at !== null && Math.abs(at.x - 2.5) < 0.1 && Math.abs(at.y - 2.5) < 0.1 &&
        is(frame, 208, 44, lampDark) && is(frame, 192, 75, wall) && is(frame, 128, 88, deck);
    };
    /**
     * Advance one browser frame and read it.
     * @returns {Promise<PNG>} The presented frame.
     */
    const step = async () => {
      await page.clock.runFor(17);
      return shoot();
    };
    /**
     * Step until a frame satisfies a condition.
     * @param {string} description - Expected state for the failure message.
     * @param {(frame: PNG) => boolean} matches - Frame predicate.
     * @param {number} [limit=300] - Maximum steps.
     * @returns {Promise<PNG>} The matching frame.
     */
    const until = async (description, matches, limit = 300) => {
      for (let i = 0; i < limit; i++) {
        const frame = await step();
        if (matches(frame)) return frame;
      }
      return assert.fail(`The yard never showed ${description}`);
    };

    // Unpaused frames settle the first draw; then freeze time so steps are explicit.
    const deadline = Date.now() + 8_000;
    while (!atSpawn(await shoot())) assert.ok(Date.now() < deadline, "The yard never showed its spawn");
    await page.clock.pauseAt(await page.evaluate(() => Date.now() + 1_000));
    await canvas.focus();
    await until("the spawn after the paused clock started", atSpawn);

    // Slide: Down presses the scout into the wall, which stops its footprint flush at x = 4.
    // The 3/8-tile footprint then has its center at 4 − 3/16 = 3.8125 tiles. The scout slides south along it.
    const flushX = 3.8125;
    await page.keyboard.down("ArrowDown");
    let flush = await until("the scout flush against the wall", frame => {
      const at = scout(frame);
      return at !== null && Math.abs(at.x - flushX) < 0.06;
    });
    const before = scout(flush);
    // The wall ends 3/16 tile south of the flush point's center, so about seven ticks of sliding remain.
    for (let i = 0; i < 4; i++) flush = await step();
    const after = scout(flush);
    assert.ok(Math.abs(after.x - flushX) < 0.06 && after.y - before.y > 0.12,
      `The scout did not slide south along the wall: ${JSON.stringify({ before, after })}`);

    // Beneath the deck: it keeps walking through the deck's ground footprint,
    // which spans x 3–6 and y 5–6, while the deck stays drawn above it.
    const under = await until("the scout walking under the deck", frame => {
      const at = scout(frame);
      return at !== null && at.x > 4.4 && at.y > 5.1 && at.y < 5.9;
    });
    assert.ok(is(under, 128, 88, deck), "The deck was not drawn above the scout");
    await page.keyboard.up("ArrowDown");

    await page.keyboard.press("r");
    await until("the spawn after reset", atSpawn);

    // The doorway lamp follows the trigger: lit while overlapping, dark after.
    await page.keyboard.down("ArrowRight");
    await until("the doorway lamp lit", frame => is(frame, 208, 44, lampLit));
    await until("the doorway lamp dark again", frame => is(frame, 208, 44, lampDark));
    await page.keyboard.up("ArrowRight");
    await page.keyboard.press("r");
    await until("the spawn after the second reset", atSpawn);

    // Focus loss clears held movement: a key still down after refocus does not move the scout.
    await page.keyboard.down("ArrowDown");
    await until("movement before focus loss", frame => {
      const at = scout(frame);
      return at !== null && at.x > 2.8;
    });
    await canvas.blur();
    const blurred = scout(await step());
    await canvas.focus();
    for (let i = 0; i < 5; i++) {
      assert.deepEqual(scout(await step()), blurred, "The held key survived focus loss");
    }
    await page.keyboard.up("ArrowDown");
    assert.deepEqual(errors, []);
  } finally {
    await page.close();
  }
}

/**
 * Play the skirmish with real mouse and keyboard input while page time is paused, so each step
 * advances about one 60 Hz tick. Scout 1 is selected and ordered across the alarm line, the guards
 * wake and shoot, and the fight runs to its recorded end: both guards and scout 1 fall and scout 2
 * survives. Logical coordinates are 320 × 200; the camera is centered on the 10 × 8 board, so a
 * ground point (x, y) in tiles at a height appears at logical (16 (x − y) + 144, 8 (x + y) + 28 − height).
 * @param {import("playwright").Browser} browser - Running Chromium instance.
 * @param {string} url - Loopback skirmish URL.
 * @returns {Promise<void>} Resolves after selection, order, alarm, bolts, damage, defeat, and reset.
 */
async function smokeSkirmish(browser, url) {
  const page = await browser.newPage({ viewport: { width: 640, height: 400 }, deviceScaleFactor: 2 });
  try {
    const errors = [];
    page.on("pageerror", error => errors.push(error.message));
    await page.clock.install();
    await page.goto(url, { timeout: 15_000 });
    const canvas = page.locator("#goje");
    const scout = [80, 160, 255], guard = [232, 84, 84], bolt = [255, 255, 190];
    const healthy = [96, 220, 96], backing = [70, 24, 24];
    const quiet = [96, 74, 120], loud = [255, 96, 200];
    const selection = [255, 235, 87], destination = [116, 246, 144];
    const deck = [186, 142, 92], wall = [148, 122, 105];
    const scale = 4;
    /** @returns {Promise<PNG>} The presented frame. */
    const shoot = async () => PNG.sync.read(await canvas.screenshot({ timeout: 3_000 }));
    /**
     * @param {number} x - Ground column in tiles.
     * @param {number} y - Ground row in tiles.
     * @param {number} [height=0] - Height in projected pixels.
     * @returns {[number, number]} Logical pixel.
     */
    const screen = (x, y, height = 0) => [Math.floor(16 * (x - y) + 144), Math.floor(8 * (x + y) + 28 - height)];
    /**
     * @param {PNG} frame - Decoded screenshot.
     * @param {[number, number]} point - Logical pixel.
     * @param {number[]} color - Expected RGB.
     * @returns {boolean} Whether the logical pixel's center has the color.
     */
    const is = (frame, [x, y], color) => pixelIs(frame, x * scale + scale / 2, y * scale + scale / 2, color);
    /**
     * @param {PNG} frame - Decoded screenshot.
     * @param {number[]} color - RGB to count.
     * @returns {number} Device pixels within two levels of the color on every channel.
     */
    const count = ({ data }, color) => {
      let total = 0;
      for (let i = 0; i < data.length; i += 4) {
        if (data[i + 3] === 255 && Math.abs(data[i] - color[0]) <= 2 &&
            Math.abs(data[i + 1] - color[1]) <= 2 && Math.abs(data[i + 2] - color[2]) <= 2) total++;
      }
      return total;
    };
    // Feet of each unit at spawn; the body is drawn 14 pixels tall above, with a 12-pixel health bar.
    const units = [[1.5, 2.5, scout], [1.5, 5.5, scout], [8.5, 2.5, guard], [8.5, 5.5, guard]];
    const wallPoint = screen(3.5, 2.5);
    /** @param {PNG} frame - Decoded screenshot. @returns {boolean} Whether the frame shows the opening. */
    const atSpawn = frame => units.every(([x, y, color]) => {
      const [sx, sy] = screen(x, y);
      return is(frame, [sx, sy - 7], color) && is(frame, [sx - 5, sy - 17], healthy) &&
        is(frame, [sx + 5, sy - 17], healthy);
    }) && count(frame, quiet) > 0 && count(frame, loud) === 0 && count(frame, selection) === 0 &&
      is(frame, [wallPoint[0], wallPoint[1] - 9], wall) && is(frame, screen(6.5, 3.5, 28), deck);
    /**
     * Advance one browser frame and read it.
     * @returns {Promise<PNG>} The presented frame.
     */
    const step = async () => {
      await page.clock.runFor(17);
      return shoot();
    };
    /**
     * Step until a frame satisfies a condition.
     * @param {string} description - Expected state for the failure message.
     * @param {(frame: PNG) => boolean} matches - Frame predicate.
     * @param {number} [limit=300] - Maximum steps.
     * @returns {Promise<PNG>} The matching frame.
     */
    const until = async (description, matches, limit = 300) => {
      for (let i = 0; i < limit; i++) {
        const frame = await step();
        if (matches(frame)) return frame;
      }
      return assert.fail(`The skirmish never showed ${description}`);
    };

    // Unpaused frames settle the first draw; then freeze time so steps are explicit.
    const deadline = Date.now() + 8_000;
    while (!atSpawn(await shoot())) assert.ok(Date.now() < deadline, "The skirmish never showed its spawn");
    await page.clock.pauseAt(await page.evaluate(() => Date.now() + 1_000));
    await until("the spawn after the paused clock started", atSpawn);

    // The crossing: scout 1 walks one tile down the column and stops on the route that scout 2 is
    // ordered along next, so scout 2 waits touching it and then goes around on the far side.
    /** @param {number} x - Ground column. @param {number} y - Ground row. @returns {Promise<void>} Resolves after the click. */
    const select = async (x, y) => {
      const [sx, sy] = screen(x, y);
      await canvas.click({ position: { x: sx * 2, y: (sy - 7) * 2 } });
    };
    /** @param {number} x - Ground column. @param {number} y - Ground row. @returns {Promise<void>} Resolves after the click. */
    const order = async (x, y) => {
      const [sx, sy] = screen(x, y);
      await canvas.click({ position: { x: sx * 2, y: sy * 2 }, button: "right" });
    };
    /**
     * A scout's rectangle is 10 logical pixels wide; check that its left edge (dx -5) or right
     * edge (dx 4) falls exactly on the expected pixel, so the body is drawn at that spot.
     * @param {PNG} frame - Decoded screenshot.
     * @param {[number, number]} feet - Logical pixel of the scout's feet.
     * @param {number} dx - Edge offset from the feet.
     * @returns {boolean} Whether the last scout pixel on that side is at the offset.
     */
    const edge = (frame, [x, y], dx) => is(frame, [x + dx, y - 7], scout) &&
      !is(frame, [x + dx + Math.sign(dx), y - 7], scout);
    const rear = screen(1.5, 3.875), front = screen(1.5, 3.5), goal = screen(1.5, 2.5), around = screen(2.5, 3.5);
    const feet1 = screen(1.5, 2.5), feet2 = screen(1.5, 5.5);
    await select(1.5, 2.5);
    await until("the selection marker over scout 1", frame => is(frame, [feet1[0], feet1[1] - 21], selection));
    await order(1.5, 3.5);
    await until("the destination marker at 1, 3", frame => is(frame, front, destination));
    await select(1.5, 5.5);
    await until("the selection marker over scout 2", frame => is(frame, [feet2[0], feet2[1] - 21], selection));
    await order(1.5, 2.5);
    // Scout 1 is already walking down the column, so look at the marker's right side.
    await until("the destination marker at 1, 2", frame => is(frame, [goal[0] + 3, goal[1]], destination));
    // Scout 2 stops exactly one body length behind scout 1 and stays there: the two rectangles are
    // drawn offset by 6 pixels, so scout 2's left edge and scout 1's right edge show them apart.
    const held = frame => edge(frame, rear, -5) && edge(frame, front, 4);
    await until("scout 2 held behind scout 1", held);
    for (let i = 0; i < 12; i++) assert.ok(held(await step()), "Scout 2 moved while it was blocked");
    await until("scout 2 passing on the far side of scout 1", frame => is(frame, [around[0], around[1] - 7], scout));
    const settled = frame => edge(frame, front, 4) && edge(frame, goal, 4) && edge(frame, goal, -5) &&
      is(frame, [goal[0], goal[1] - 21], selection) && !is(frame, [around[0], around[1] - 7], scout);
    await until("both scouts standing at their cells", settled);
    for (let i = 0; i < 5; i++) assert.ok(settled(await step()), "A scout moved after the crossing ended");
    const crossed = await shoot();
    assert.equal(count(crossed, loud), 0, "The crossing rang the alarm");
    assert.equal(count(crossed, backing), 0, "The crossing hurt a unit");
    assert.equal(count(crossed, bolt), 0, "The crossing fired a bolt");
    await canvas.focus();
    await page.keyboard.press("r");
    await until("the spawn after the crossing", atSpawn);

    // Left click on scout 1 selects it; the marker appears over its health bar.
    const [feetX, feetY] = screen(1.5, 2.5);
    await canvas.click({ position: { x: feetX * 2, y: (feetY - 7) * 2 } });
    await until("the selection marker", frame => count(frame, selection) > 0);
    // Right click on the cell at 6, 2 orders it across the alarm line.
    const [goalX, goalY] = screen(6.5, 2.5);
    await canvas.click({ position: { x: goalX * 2, y: goalY * 2 }, button: "right" });
    await until("the destination marker", frame => is(frame, [goalX, goalY], destination));

    // Fight to the end, keeping what each frame showed: the alarm line lit, a bolt, a damaged bar.
    const seen = { alarm: false, bolt: false, damage: false };
    const final = await until("both guards defeated", frame => {
      seen.alarm ||= count(frame, loud) > 0;
      seen.bolt ||= count(frame, bolt) >= 64;
      seen.damage ||= count(frame, backing) >= 16;
      return count(frame, guard) === 0;
    }, 900);
    assert.deepEqual(seen, { alarm: true, bolt: true, damage: true }, "The fight did not show every effect");
    assert.ok(count(final, scout) > 0, "The surviving scout is missing");
    assert.equal(count(final, selection), 0, "The selection marker outlived its scout");
    assert.equal(count(final, quiet), 0, "The alarm line was still quiet after the alarm");

    // Reset restores the opening.
    await canvas.focus();
    await page.keyboard.press("r");
    await until("the spawn after reset", atSpawn);
    assert.deepEqual(errors, []);
  } finally {
    await page.close();
  }
}

/**
 * Play the outpost with real Chromium mouse and keyboard input: title menu, drag and shift selection,
 * control groups, orders, the pause menu, saving and loading through real localStorage, and a replay.
 * @param {import("playwright").Browser} browser - Running Chromium instance.
 * @param {string} url - Loopback playground URL.
 * @returns {Promise<void>} Resolves once the replay has ended and no page error was raised.
 */
async function smokeOutpost(browser, url) {
  const page = await browser.newPage({ viewport: { width: 640, height: 400 }, deviceScaleFactor: 2 });
  try {
    const errors = [];
    page.on("pageerror", error => errors.push(error.message));
    await page.clock.install();
    await page.goto(url, { timeout: 15_000 });
    // The page's control hint is white text over the canvas; hide it so every white pixel is the game's.
    await page.addStyleTag({ content: "#controls { display: none; }" });
    const canvas = page.locator("#goje");
    const scout = [80, 160, 255], guard = [232, 84, 84], bolt = [255, 255, 190];
    const healthy = [96, 220, 96], loud = [255, 96, 200];
    const selection = [255, 235, 87], destination = [116, 246, 144];
    const text = [255, 255, 255], recording = [255, 96, 96];
    const scale = 4;
    /** @returns {Promise<PNG>} The presented frame. */
    const shoot = async () => PNG.sync.read(await canvas.screenshot({ timeout: 3_000 }));
    /**
     * @param {number} x - Ground column in tiles.
     * @param {number} y - Ground row in tiles.
     * @param {number} [height=0] - Height in projected pixels.
     * @returns {[number, number]} Logical pixel; the 11 × 9 board is centered in the 320 × 200 viewport.
     */
    const screen = (x, y, height = 0) => [Math.floor(16 * (x - y) + 144), Math.floor(8 * (x + y) + 20 - height)];
    /**
     * @param {PNG} frame - Decoded screenshot.
     * @param {[number, number]} point - Logical pixel.
     * @param {number[]} color - Expected RGB.
     * @returns {boolean} Whether the logical pixel's center has the color.
     */
    const is = (frame, [x, y], color) => pixelIs(frame, x * scale + scale / 2, y * scale + scale / 2, color);
    /**
     * @param {PNG} frame - Decoded screenshot.
     * @param {number[]} color - RGB to count.
     * @returns {number} Device pixels within two levels of the color on every channel.
     */
    const count = ({ data }, color) => {
      let total = 0;
      for (let i = 0; i < data.length; i += 4) {
        if (data[i + 3] === 255 && Math.abs(data[i] - color[0]) <= 2 &&
            Math.abs(data[i + 1] - color[1]) <= 2 && Math.abs(data[i + 2] - color[2]) <= 2) total++;
      }
      return total;
    };
    const scouts = [[0.5, 1.5], [0.5, 3.5], [0.5, 5.5], [0.5, 7.5], [1.5, 2.5], [1.5, 6.5]];
    const guards = [[9.5, 1.5], [9.5, 3.5], [9.5, 5.5], [9.5, 7.5], [10.5, 2.5], [10.5, 6.5]];
    /** @param {PNG} frame - Decoded screenshot. @returns {boolean} Whether all twelve units are drawn at full health. */
    const atSpawn = frame => [[scouts, scout], [guards, guard]].every(([list, color]) => list.every(([x, y]) => {
      const [sx, sy] = screen(x, y);
      return is(frame, [sx, sy - 7], color) && is(frame, [sx - 5, sy - 17], healthy);
    })) && count(frame, loud) === 0;
    /** @param {PNG} frame - Decoded screenshot. @param {number[][]} list - Ground positions. @returns {number} Markers over them. */
    const marked = (frame, list) => list.filter(([x, y]) => {
      const [sx, sy] = screen(x, y);
      return is(frame, [sx, sy - 21], selection);
    }).length;
    /**
     * Advance one browser frame and read it.
     * @returns {Promise<PNG>} The presented frame.
     */
    const step = async () => {
      await page.clock.runFor(17);
      return shoot();
    };
    /**
     * Let the game see an input before the next one arrives. The host runs about 0.96 ticks per 16 ms
     * frame, so one frame can pass without a tick; two frames always include one. Inputs that share a
     * tick are read together, which is not what a player's separate actions do.
     * @returns {Promise<void>} Resolves after two frames.
     */
    const settle = () => page.clock.runFor(34);
    /**
     * Step until a frame satisfies a condition.
     * @param {string} description - Expected state for the failure message.
     * @param {(frame: PNG) => boolean} matches - Frame predicate.
     * @param {number} [limit=300] - Maximum steps.
     * @returns {Promise<PNG>} The matching frame.
     */
    const until = async (description, matches, limit = 300) => {
      for (let i = 0; i < limit; i++) {
        const frame = await step();
        if (matches(frame)) return frame;
      }
      return assert.fail(`The outpost never showed ${description}`);
    };
    /** @param {number} ticks - Frames to advance. @returns {Promise<PNG>} The last frame. */
    const run = async ticks => {
      let frame = await shoot();
      for (let i = 0; i < ticks; i++) frame = await step();
      return frame;
    };
    /**
     * @param {number} x - Logical x. @param {number} y - Logical y.
     * @returns {Promise<[number, number]>} Page position of a logical pixel.
     */
    const at = async (x, y) => {
      const box = await canvas.boundingBox();
      assert.ok(box !== null, "The canvas is not on the page");
      return [box.x + x * 2, box.y + y * 2];
    };
    /** @param {number} x - Logical x. @param {number} y - Logical y. @param {object} [options] - Mouse click options. @returns {Promise<void>} Resolves after the click. */
    const click = async (x, y, options = {}) => {
      const [px, py] = await at(x, y);
      await page.mouse.click(px, py, options);
      await settle();
    };
    /** @param {string} key - Key to press. @returns {Promise<void>} Resolves after the tick that saw the press. */
    const press = async key => {
      await page.keyboard.press(key);
      await settle();
    };
    /** @returns {Promise<void>} Resolves once the pointer rests over bare ground, away from every menu. */
    const park = async () => {
      const [px, py] = await at(3, 190);
      await page.mouse.move(px, py);
    };
    /** @returns {Promise<string[]>} The localStorage keys the game wrote. */
    const stored = () => page.evaluate(() => Object.keys(localStorage).filter(key => key.startsWith("goje/outpost/")).sort());

    // The title shows text; a real click on Start begins the match.
    const deadline = Date.now() + 8_000;
    while (count(await shoot(), text) < 100) assert.ok(Date.now() < deadline, "The outpost never showed its title");
    await page.clock.pauseAt(await page.evaluate(() => Date.now() + 1_000));
    await until("the title after the paused clock started", frame => count(frame, text) >= 100);
    assert.deepEqual(await stored(), [], "A fresh browser must have nothing saved");
    await click(160, 100);
    await until("the spawn after Start", frame => atSpawn(frame) && count(frame, selection) === 0);

    // A drag selects every scout in the box, and the box is drawn while the button is down.
    const [dragFromX, dragFromY] = await at(1, 13), [dragToX, dragToY] = await at(318, 178);
    await page.mouse.move(dragFromX, dragFromY);
    await page.mouse.down();
    await page.mouse.move(dragToX, dragToY, { steps: 4 });
    await until("the drag box", frame => is(frame, [160, 13], selection));
    await page.mouse.up();
    await until("all six scouts marked", frame => marked(frame, scouts) === 6 && count(frame, selection) > 0);
    assert.equal(marked(await shoot(), guards), 0, "A guard was selected");

    // Shift-click toggles one scout out, and a control group brings the set back.
    const [firstX, firstY] = screen(...scouts[0]);
    // Modifiers are held state, so the click settles before Shift goes up again.
    await page.keyboard.down("Shift");
    await click(firstX, firstY - 7);
    await page.keyboard.up("Shift");
    await until("scout 1 deselected", frame => marked(frame, scouts) === 5 && marked(frame, [scouts[0]]) === 0);
    await page.keyboard.down("Control");
    await page.keyboard.down("2");
    await settle();
    await page.keyboard.up("2");
    await page.keyboard.up("Control");
    await click(160, 100); // Bare ground clears the selection.
    await until("an empty selection", frame => marked(frame, scouts) === 0);
    await press("2");
    await until("group 2 recalled", frame => marked(frame, scouts) === 5 && marked(frame, [scouts[0]]) === 0);

    // A drag over the whole set and hold fire; then pause and save while every unit is still.
    await page.mouse.move(dragFromX, dragFromY);
    await page.mouse.down();
    await page.mouse.move(dragToX, dragToY, { steps: 4 });
    await page.mouse.up();
    await until("all six scouts marked again", frame => marked(frame, scouts) === 6);
    const plain = count(await shoot(), text);
    await press("h");
    const held = await until("the hold count in the selection text", frame => count(frame, text) > plain);
    const holding = count(held, text);
    await press("Escape");
    await park();
    const paused = await until("the pause menu", frame => count(frame, text) > holding);
    assert.deepEqual((await run(10)).data, paused.data, "The match moved while paused");
    await click(160, 75); // Save.
    await until("the saved notice", frame => count(frame, selection) > count(paused, selection));
    assert.deepEqual(await stored(), ["goje/outpost/save"], "Saving must write one key");
    await park();
    await run(200); // Let the notice expire.
    await press("Escape"); // Resume.
    await press("h");
    await until("the hold count gone", frame => count(frame, text) === plain);

    // Attack-move with A and a click, then focus fire with F every second, until bolts have flown.
    await press("a");
    await click(...screen(3.5, 4.5));
    await until("destination markers", frame => count(frame, destination) > 0);
    const seen = { alarm: false, bolt: false };
    for (let i = 0; i < 900 && !(seen.alarm && seen.bolt); i++) {
      if (i % 60 === 59) await page.keyboard.press("f");
      const frame = await step();
      seen.alarm ||= count(frame, loud) > 0;
      seen.bolt ||= count(frame, bolt) >= 16;
    }
    assert.deepEqual(seen, { alarm: true, bolt: true }, "The fight did not show the alarm and a bolt");

    // Loading from the pause menu brings back the saved opening: every unit at spawn, six scouts
    // marked, and the hold count in the text.
    await press("Escape");
    await click(160, 89); // Load.
    const loaded = await until("the saved opening", frame => atSpawn(frame) && marked(frame, scouts) === 6);
    assert.equal(count(loaded, text), holding, "The loaded match lost the hold order");

    // Play on from the load, quit to the title, and watch the recording of that run.
    await press("h");
    await press("a");
    await click(...screen(3.5, 4.5));
    const lastFrame = await run(100);
    assert.ok(!atSpawn(lastFrame), "The scouts did not move after the load");
    await press("Escape");
    await park();
    await until("the pause menu", frame => count(frame, text) > 0);
    await click(160, 131); // Quit to title.
    await until("the title again", frame => count(frame, text) >= 100 && count(frame, guard) === 0);
    assert.deepEqual(await stored(), ["goje/outpost/last", "goje/outpost/save"], "Quitting must keep the recording");
    await click(160, 132); // Watch last replay.
    await until("the replay label", frame => count(frame, recording) > 0);
    let previous = null, unchanged = 0, final = null;
    for (let i = 0; i < 600 && unchanged < 20; i++) {
      final = await step();
      unchanged = previous !== null && Buffer.compare(final.data, previous.data) === 0 ? unchanged + 1 : 0;
      previous = final;
    }
    assert.ok(unchanged >= 20 && final !== null, "The replay never ended");
    for (const color of [scout, guard, selection, bolt]) {
      assert.equal(count(final, color), count(lastFrame, color), `The replay ended with a different count of ${color}`);
    }
    assert.deepEqual(errors, []);
  } finally {
    await page.close();
  }
}

/**
 * Select the scout, command it across the authored courtyard, then reset.
 * @param {import("playwright").Browser} browser - Running Chromium instance.
 * @param {string} url - Loopback playground URL.
 * @param {boolean} shortcut - Whether the build embeds the Level Builder download and custom tile.
 * @param {boolean} [sprite=false] - Check only the edited move clip's timing and reset.
 * @returns {Promise<void>} Resolves when the authored spawn is restored.
 */
async function smokeAuthored(browser, url, shortcut, sprite = false) {
  const page = await browser.newPage({ viewport: { width: 880, height: 400 }, deviceScaleFactor: 2 });
  try {
    await page.clock.install();
    await page.goto(url, { timeout: 15_000 });
    const canvas = page.locator("#goje");
    const tomato = [215, 84, 73], ink = [36, 38, 46], eye = [244, 241, 235];
    const wall = [148, 122, 105], selected = [255, 235, 87];
    const destination = [116, 246, 144];
    const lookout = [92, 220, 210];
    // Match the highlight drawn through the Tile Maker in the round-trip smoke.
    const ground = shortcut ? [222, 122, 66] : [91, 133, 114];
    const background = [20, 28, 41], grid = [63, 84, 108];
    /**
     * Check a logical pixel with horizontal letterboxing at DPR 2.
     * @param {PNG} frame - Decoded canvas screenshot.
     * @param {number} x - Horizontal logical pixel.
     * @param {number} y - Vertical logical pixel.
     * @param {[number, number, number]} color - Expected RGB.
     * @returns {boolean} Whether the rendered pixel matches.
     */
    const matches = (frame, x, y, color) => pixelIs(frame, 240 + x * 4, y * 4, color);
    /**
     * Distinguish the original wall from the exported opening and relocated lookout.
     * @param {PNG} frame - Decoded canvas screenshot.
     * @returns {boolean} Whether the expected layout appears.
     */
    const layoutMatches = frame => shortcut
      ? !matches(frame, 176, 80, wall) && matches(frame, 176, 122, lookout) &&
        !matches(frame, 192, 114, lookout)
      : matches(frame, 176, 80, wall);
    await waitCourtyard(canvas, "authored spawn", frame =>
      matches(frame, 144, 71, tomato) && matches(frame, 139, 70, eye) && layoutMatches(frame) &&
      matches(frame, 141, 41, ground) && matches(frame, 130, 38, background) &&
      matches(frame, 152, 40, grid));
    // Step the browser clock ourselves so screenshot latency cannot skip a short pose.
    await page.clock.pauseAt(await page.evaluate(() => Date.now() + 1_000));
    /** @returns {Promise<PNG>} The frame presented after the next browser frame. */
    const step = async () => {
      await page.clock.runFor(16);
      return PNG.sync.read(await canvas.screenshot({ timeout: 3_000 }));
    };
    if (sprite) {
      // At most one 60 Hz simulation tick passes between these captures.
      /**
       * Step browser frames until one matches.
       * @param {string} stage - Checkpoint name for failures.
       * @param {function(PNG): boolean} expected - Frame predicate.
       */
      const stepUntil = async (stage, expected) => {
        for (let i = 0; i < 8; i++) if (expected(await step())) return;
        assert.fail(`No matching courtyard frame for ${stage}`);
      };
      await page.mouse.click(408, 128);
      await stepUntil("scout selected", frame => spriteCenter(frame, selected) !== null);
      // Command the straight +board-Y route from cell (2, 2) to (2, 5). Each
      // motion tick moves the ground anchor 1 px left and half a pixel down.
      await page.mouse.click(312, 200, { button: "right" });
      const cyan = [22, 216, 255];
      /**
       * At 0.75× the two painted rows span anchor Y−7.5 to Y−6.
       * @param {PNG} frame - Decoded canvas screenshot.
       * @param {number} x - Anchor column.
       * @param {number} y - Ground row.
       * @returns {boolean} Whether the hop shows the cyan stripe and clears the ground.
       */
      const hopping = (frame, x, y) => matches(frame, x, y - 7, cyan) &&
        matches(frame, x, y - 4, ink) && !matches(frame, x, y - 2, ink);
      // The edited move clip is hop (16 ticks), windup, standing. The original
      // landed on motion tick 13; the edit stays airborne through tick 16.
      /** @type {Map<number, [string, function(PNG, number, number): boolean]>} */
      const checkpoints = new Map([
        [13, ["airborne and painted where the original clip landed", hopping]],
        [16, ["airborne and painted on the last hop tick", hopping]],
        [17, ["planted on the unpainted windup pose", (frame, x, y) => !matches(frame, x, y - 7, cyan) &&
          matches(frame, x, y - 1, ink) && matches(frame, x, y - 4, tomato)]],
      ]);
      // The selection marker is centered on the anchor column, so its rendered x names the tick.
      for (let tick = 0, steps = 0; tick < 17; steps++) {
        assert.ok(steps < 40, `the scout stalled at motion tick ${tick}`);
        const frame = await step();
        const marker = spriteCenter(frame, selected);
        const next = marker === null ? NaN : 144 - (marker.x + 0.5 - 240) / 4;
        assert.ok(next === tick || next === tick + 1, `motion tick jumped from ${tick} to ${next}`);
        tick = next;
        const checkpoint = checkpoints.get(tick);
        if (checkpoint) {
          assert.ok(checkpoint[1](frame, 144 - tick, 76 + tick / 2), `motion tick ${tick}: scout not ${checkpoint[0]}`);
        }
      }
      await page.keyboard.press("r");
      await stepUntil("sprite reset restores the authored spawn", frame =>
        matches(frame, 144, 71, tomato) && matches(frame, 139, 70, eye) && layoutMatches(frame) &&
        matches(frame, 141, 41, ground) && spriteCenter(frame, selected) === null &&
        !matches(frame, 96, 100, destination) && !matches(frame, 127, 83.5, ink));
      return;
    }
    // Reset before timing idle so loading and the jump to paused time do not count.
    // The scout is open for 114 ticks and blinks for 6. Start sampling just before that window.
    await page.keyboard.press("r");
    await page.clock.runFor(1_600);
    assert.ok(matches(await step(), 139, 70, eye), "idle scout blinked before its first blink window");
    let blinked = false;
    for (let i = 0; i < 32 && !blinked; i++) {
      const shot = await step();
      blinked = matches(shot, 139, 70, ink) && matches(shot, 144, 71, tomato);
    }
    assert.ok(blinked, "No matching courtyard frame for idle scout blinks");
    // The topmost tile's asymmetric highlight follows the grid while its corners stay clear.
    await page.keyboard.press("w");
    await page.clock.runFor(32);
    await waitCourtyard(canvas, "ground art and planted scout pivot under zoom", frame =>
      matches(frame, 136, 26, ground) && matches(frame, 122, 22, background) &&
      matches(frame, 150, 25, grid) && matches(frame, 140, 64, tomato) &&
      matches(frame, 140, 69, ink) && !matches(frame, 140, 70, ink));
    await page.keyboard.press("r");
    await page.clock.runFor(32);
    await waitCourtyard(canvas, "authored camera reset", frame => matches(frame, 141, 41, ground));
    // Observe the real canvas event after its host listener, then separately
    // check that a contextmenu event outside the canvas is not canceled.
    /** @type {boolean[]} */
    const contextMenus = [];
    await page.exposeFunction("recordContextMenu", canceled => contextMenus.push(canceled));
    await page.evaluate(() => {
      document.addEventListener("contextmenu", event => {
        if (event.target === document.querySelector("#goje")) window.recordContextMenu(event.defaultPrevented);
      });
    });
    // A press while the other button is held must still reach the host.
    await page.mouse.move(408, 128);
    await page.mouse.down({ button: "left" });
    await page.mouse.click(536, 216, { button: "right" });
    await page.mouse.up({ button: "left" });
    await page.clock.runFor(32);
    await waitCourtyard(canvas, "authored command", frame =>
      spriteCenter(frame, selected) !== null && matches(frame, 208, 108, destination));
    await page.clock.runFor(103);
    await waitCourtyard(canvas, "moving scout hops above its ground anchor", frame => {
      for (let tick = 5; tick <= 12; tick++) {
        const x = 144 + tick, y = 76 + Math.floor(tick / 2);
        if (matches(frame, x, y - 7, tomato) && matches(frame, x, y - 4, ink) &&
            !matches(frame, x, y - 2, ink)) return true;
      }
      return false;
    });
    if (shortcut) {
      // The opening is two 16-tick tiles from spawn. Stop on the crossing.
      await page.clock.runFor(397);
      await waitCourtyard(canvas, "exported shortcut crossing", frame => {
        const scout = spriteCenter(frame, tomato);
        return scout !== null && matches(frame, 208, 108, destination) &&
          Math.abs(scout.x - (240 + 176 * 4)) <= 28 && Math.abs(scout.y - 85 * 4) <= 20;
      });
    } else {
      // At the end of the first edge the wall covers the lower part of the scout.
      await page.clock.runFor(132);
      await waitCourtyard(canvas, "wall occludes the moving scout", frame =>
        matches(frame, 159, 78, tomato) && matches(frame, 160, 81, wall));
      // The detour passes the third tile at tick 48. Sample every browser
      // frame around it so screenshot latency cannot skip the moving pose.
      await page.clock.runFor(300);
      let sawDetour = false;
      for (let frame = 0; frame < 24 && !sawDetour; frame++) {
        await page.clock.runFor(16);
        const shot = PNG.sync.read(await canvas.screenshot({ timeout: 3_000 }));
        sawDetour = matches(shot, 128, 94, tomato) && matches(shot, 208, 108, destination);
      }
      assert.ok(sawDetour, "No matching courtyard frame for authored wall detour");
    }
    await page.clock.resume();
    assert.deepEqual(contextMenus, [true], "the canvas did not suppress the real context menu");
    assert.ok(await page.evaluate(() => document.body.dispatchEvent(
      new MouseEvent("contextmenu", { bubbles: true, cancelable: true }),
    )), "the surrounding page suppressed its context menu");
    await waitCourtyard(canvas, "authored arrival", frame =>
      matches(frame, 208, 103, tomato) && !matches(frame, 208, 108, destination));
    await page.mouse.click(30, 216, { button: "right" });
    for (let i = 0; i < 3; i++) {
      const frame = PNG.sync.read(await canvas.screenshot({ timeout: 3_000 }));
      assert.ok(matches(frame, 208, 103, tomato) && spriteCenter(frame, destination) === null,
        "a secondary press in the bar should not issue an order");
    }
    await page.keyboard.press("ArrowRight", { delay: 100 });
    await page.keyboard.press("r", { delay: 100 });
    await waitCourtyard(canvas, "authored reset", frame =>
      matches(frame, 144, 71, tomato) && matches(frame, 139, 70, eye) && layoutMatches(frame) &&
      matches(frame, 141, 41, ground) &&
      !matches(frame, 144, 53, selected) && !matches(frame, 208, 108, destination));
  } finally {
    await page.close();
  }
}

const mode = ["--sprite", "--yard", "--outpost"].find(flag => flag === process.argv[2]);
const sprite = mode === "--sprite";
const editedDirectory = process.argv[mode ? 3 : 2];
if (process.argv.length > (mode ? 4 : 3) || (mode && editedDirectory === undefined)) {
  throw new Error("Usage: node scripts/smoke-web.mjs [EDITED_WEB_DIR | --sprite SPRITE_WEB_DIR | --yard YARD_WEB_DIR | --outpost OUTPOST_WEB_DIR]");
}
const server = await servePlayground(0, editedDirectory);
try {
  const browser = await chromium.launch({ timeout: 15_000 });
  try {
    if (mode === "--outpost") {
      await smokeOutpost(browser, `http://127.0.0.1:${server.address().port}/?fixture=outpost`);
      console.log("Browser smoke passed: the Level Builder's outpost level played with drag selection, groups, " +
        "orders, hold, pause, save, load, and replay");
    } else if (mode === "--yard") {
      await smokeYard(browser, `http://127.0.0.1:${server.address().port}/?fixture=yard`);
      console.log("Browser smoke passed: Level Builder zones drove the yard's slide along a wall, doorway lamp, " +
        "walk beneath a raised deck, reset, and focus loss");
    } else if (sprite) {
      await smokeAuthored(browser, `http://127.0.0.1:${server.address().port}/`, false, true);
      console.log("Browser smoke passed: authored scout hop airborne with cyan paint at motion ticks 13 and 16, " +
        "unpainted planted windup at tick 17, and reset");
    } else {
      const page = await browser.newPage({ viewport: { width: 640, height: 400 } });
      const errors = [];
      page.on("pageerror", error => errors.push(error.message));
      page.on("console", message => {
        if (message.type() === "error") errors.push(message.text());
      });
      await page.goto(`http://127.0.0.1:${server.address().port}/?fixture=pickup`, { timeout: 15_000 });
      const canvas = page.locator("#goje");
      await waitForFrame(canvas, "initial spawn", (player, token) =>
        at(player, 160, 92, 2) && at(token, 192, 108, 2));
      await canvas.click({ timeout: 3_000 });
      await page.keyboard.down("ArrowRight");
      try {
        await waitForFrame(canvas, "pickup", (player, token) =>
          player !== null && player.x > 410 && Math.abs(player.y - 184) <= 2 && token === null);
      } finally {
        await page.keyboard.up("ArrowRight");
      }
      await page.setViewportSize({ width: 880, height: 500 });
      await waitForFrame(canvas, "resized encounter", (player, token) =>
        player !== null && player.x > 550 && Math.abs(player.y - 230) <= 2 && token === null);
      await page.keyboard.press("r", { delay: 100 });
      await waitForFrame(canvas, "reset", (player, token) =>
        at(player, 160, 92, 2.5, 40) && at(token, 192, 108, 2.5, 40));
  
      await page.keyboard.down("ArrowLeft");
      try {
        await waitForFrame(canvas, "movement before focus loss", player =>
          player !== null && player.x < 425);
        await canvas.blur();
        const { player: blurred } = await readSprites(canvas);
        assert.ok(blurred !== null && blurred.x > 140, "player must have room to drift left");
  
        // Resume while the key is still held. A stale key must not move the
        // player on any of the following focused ticks.
        await canvas.focus();
        for (let i = 0; i < 4; i++) {
          const { player } = await readSprites(canvas);
          assert.deepEqual(player, blurred, "the held key survived focus loss");
        }
      } finally {
        await page.keyboard.up("ArrowLeft");
      }
      assert.deepEqual(errors, []);
      console.log("Browser smoke passed: movement, pickup, resize, reset, and focus loss");
      await smokeCourtyard(browser, `http://127.0.0.1:${server.address().port}/?fixture=courtyard`);
      console.log("Browser smoke passed: courtyard overlap, selection, pan, zoom, resize, patrol, and reset");
      await smokeAuthored(browser, `http://127.0.0.1:${server.address().port}/`, editedDirectory !== undefined);
      console.log(editedDirectory === undefined
        ? "Browser smoke passed: authored courtyard selection, wall detour, arrival, and reset"
        : "Browser smoke passed: custom ground art, exported lookout, shortcut crossing, arrival, and reset");
      if (editedDirectory === undefined) {
        await smokeYard(browser, `http://127.0.0.1:${server.address().port}/?fixture=yard`);
        console.log("Browser smoke passed: yard slide along a wall, doorway lamp, walk beneath a raised deck, " +
          "reset, and focus loss");
        await smokeSkirmish(browser, `http://127.0.0.1:${server.address().port}/?fixture=skirmish`);
        console.log("Browser smoke passed: skirmish selection, order, alarm, bolts, damage, defeat, and reset");
        await smokeOutpost(browser, `http://127.0.0.1:${server.address().port}/?fixture=outpost`);
        console.log("Browser smoke passed: outpost drag selection, groups, orders, hold, pause, save, load, and replay");
      }
    }
  } finally {
    await browser.close();
  }
} finally {
  await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
}
