/** @file Verify keyboard movement and resize in the browser using rendered canvas pixels. */

import assert from "node:assert/strict";

import { chromium } from "playwright";
import { PNG } from "pngjs";

import { servePlayground } from "./playground-server.mjs";

/**
 * @typedef {object} MarkerCenter
 * @property {number} x - Horizontal position in screenshot pixels.
 * @property {number} y - Vertical position in screenshot pixels.
 */

/**
 * Poll canvas screenshots for a rendered marker whose center meets the expected condition.
 * @param {import("playwright").Locator} canvas - Locator for the rendered playground canvas.
 * @param {function(MarkerCenter): boolean} matches - Accepts a center at the expected position.
 * @returns {Promise<MarkerCenter>} The center of the marker's detected pixel bounds.
 * @throws {Error} The returned promise rejects if no matching marker is found before the deadline.
 */
async function findMarker(canvas, matches) {
  const deadline = Date.now() + 8_000;
  while (Date.now() < deadline) {
    const { data, width } = PNG.sync.read(await canvas.screenshot({ timeout: 3_000 }));
    let left = width, right = 0, top = Infinity, bottom = 0, count = 0;
    for (let pixel = 0; pixel < data.length; pixel += 4) {
      if (Math.abs(data[pixel] - 248) > 8 || Math.abs(data[pixel + 1] - 164) > 8 ||
          Math.abs(data[pixel + 2] - 76) > 8 || data[pixel + 3] !== 255) continue;
      const index = pixel / 4;
      const x = index % width, y = Math.floor(index / width);
      left = Math.min(left, x);
      right = Math.max(right, x);
      top = Math.min(top, y);
      bottom = Math.max(bottom, y);
      count++;
    }
    const center = { x: (left + right) / 2, y: (top + bottom) / 2 };
    if (count >= 64 && matches(center)) return center;
  }
  throw new Error("No matching orange marker in the rendered playground canvas");
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
    const initial = await findMarker(canvas, ({ x }) => x > 300 && x < 340);
    await canvas.click({ timeout: 3_000 });
    await page.keyboard.down("ArrowRight");
    try {
      await findMarker(canvas, ({ x, y }) => x > initial.x + 30 && Math.abs(y - initial.y) <= 2);
    } finally {
      await page.keyboard.up("ArrowRight");
    }
    await page.setViewportSize({ width: 800, height: 500 });
    await findMarker(canvas, ({ x }) => x > 420);
    assert.deepEqual(errors, []);
    console.log("Browser smoke passed: real key input moved the rendered marker, which remained visible after resize");
  } finally {
    await browser.close();
  }
} finally {
  await new Promise((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
}
