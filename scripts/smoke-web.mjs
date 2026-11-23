/** @file Verify both reference scenes with real Chromium input and rendered canvas pixels. */

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
 * Drive the default courtyard with real Chromium keyboard and mouse events.
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

const server = await servePlayground(0);
try {
  const browser = await chromium.launch({ timeout: 15_000 });
  try {
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
    await smokeCourtyard(browser, `http://127.0.0.1:${server.address().port}/`);
    console.log("Browser smoke passed: courtyard overlap, selection, pan, zoom, resize, patrol, and reset");
  } finally {
    await browser.close();
  }
} finally {
  await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
}
