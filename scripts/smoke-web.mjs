/** @file Verify movement, pickup, resize, and reset using rendered browser canvas pixels. */

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
    const frame = PNG.sync.read(await canvas.screenshot({ timeout: 3_000 }));
    if (matches(spriteCenter(frame, [248, 164, 76]), spriteCenter(frame, [92, 220, 210]))) return;
  }
  throw new Error(`No matching playground frame for ${stage}`);
}

/**
 * Check a rendered center against logical coordinates after viewport scaling.
 * @param {SpriteCenter | null} sprite - Detected center, if present.
 * @param {number} x - Expected horizontal position in logical game pixels.
 * @param {number} y - Expected vertical position in logical game pixels.
 * @param {number} scale - Screenshot pixels per logical game pixel.
 * @returns {boolean} Whether the sprite is within two screenshot pixels of the target.
 */
function at(sprite, x, y, scale) {
  return sprite !== null && Math.abs(sprite.x - x * scale) <= 2 &&
    Math.abs(sprite.y - y * scale) <= 2;
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
    await page.goto(`http://127.0.0.1:${server.address().port}/`, { timeout: 15_000 });
    const canvas = page.locator("canvas");
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
    await page.setViewportSize({ width: 800, height: 500 });
    await waitForFrame(canvas, "resized encounter", (player, token) =>
      player !== null && player.x > 510 && Math.abs(player.y - 230) <= 2 && token === null);
    await page.keyboard.press("r", { delay: 100 });
    await waitForFrame(canvas, "reset", (player, token) =>
      at(player, 160, 92, 2.5) && at(token, 192, 108, 2.5));
    assert.deepEqual(errors, []);
    console.log("Browser smoke passed: real keys moved the player, collected the token, and reset both after resize");
  } finally {
    await browser.close();
  }
} finally {
  await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
}
