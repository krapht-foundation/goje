/** @file Drive the input probe fixture with emulated touch and a fake gamepad API, and check canvas pixels. */

import assert from "node:assert/strict";
import { promisify } from "node:util";

import { chromium } from "playwright";
import { PNG } from "pngjs";

import { servePlayground } from "./playground-server.mjs";

// The page is 640 × 400 CSS pixels, so each logical pixel of the 320 × 200
// viewport is 2 × 2 and there are no letterbox bars.
const scale = 2;
const background = [20, 28, 41], white = [255, 255, 255];
const touchColors = [[255, 64, 64], [64, 255, 64], [64, 128, 255], [255, 255, 64], [255, 64, 255]];
const stick = [255, 160, 0], buttonUp = [40, 40, 60], buttonDown = [0, 255, 255];

/**
 * Wait for a presented frame that satisfies a predicate on logical pixels.
 * @param {import("playwright").Locator} canvas - The game canvas.
 * @param {string} description - Expected state for the failure message.
 * @param {(is: (x: number, y: number, rgb: number[]) => boolean) => boolean} expected - Frame predicate.
 * @returns {Promise<void>} Resolves at the first matching frame.
 * @throws {Error} The returned promise rejects if no frame matches within five seconds.
 */
async function frame(canvas, description, expected) {
  const deadline = Date.now() + 5_000;
  let last = "";
  do {
    const png = PNG.sync.read(await canvas.screenshot({ timeout: 2_000 }));
    const is = (x, y, rgb) => {
      const offset = (Math.floor((y + 0.5) * scale) * png.width + Math.floor((x + 0.5) * scale)) * 4;
      last = `(${x}, ${y}) = ${Array.from(png.data.subarray(offset, offset + 3))}`;
      return rgb.every((channel, i) => Math.abs(png.data[offset + i] - channel) <= 8);
    };
    if (expected(is)) return;
  } while (Date.now() < deadline);
  throw new Error(`No frame showed ${description}; last sample ${last}`);
}

/**
 * Install a fake Gamepad API before the page's scripts run. The Go host reads
 * it through navigator.getGamepads like a real one.
 */
function fakeGamepads() {
  /** @type {(object | null)[]} */
  window.gojePads = [null, null, null, null];
  /**
   * @param {number[]} pressed - Standard button indexes that are down.
   * @param {number[]} axes - Four standard stick axes.
   * @param {string} [mapping="standard"] - The pad's mapping.
   * @returns {object} A Gamepad-shaped object.
   */
  window.gojePad = (pressed, axes, mapping = "standard") => ({
    id: "Goje smoke pad", index: 0, connected: true, mapping, timestamp: 0, axes,
    buttons: Array.from({ length: 17 }, (_, i) => {
      const down = pressed.includes(i);
      return { pressed: down, touched: down, value: down ? 1 : 0 };
    }),
  });
  Object.defineProperty(Navigator.prototype, "getGamepads", {
    configurable: true,
    value: () => window.gojePads,
  });
}

const directory = process.argv[2];
if (process.argv.length > 3) throw new Error("Usage: node scripts/smoke-input.mjs [WEB_DIR]");
const server = await servePlayground(0, directory);
try {
  const browser = await chromium.launch({ timeout: 15_000 });
  try {
    const context = await browser.newContext({ viewport: { width: 640, height: 400 }, hasTouch: true });
    await context.addInitScript(fakeGamepads);
    const page = await context.newPage();
    page.setDefaultTimeout(5_000);
    const errors = [];
    page.on("pageerror", error => errors.push(error));
    await page.goto(`http://127.0.0.1:${server.address().port}/?fixture=input`, { timeout: 15_000 });
    await page.waitForFunction(() => document.querySelector("#status").hidden, null, { timeout: 15_000 });
    const canvas = page.locator("#goje");
    assert.equal(await canvas.evaluate(element => element.style.touchAction), "none");
    await frame(canvas, "the empty probe", is => is(160, 100, background));

    // A tap is a click: the pointer stays where the finger lifted, and no touch remains.
    await page.touchscreen.tap(40 * scale, 40 * scale);
    await frame(canvas, "the pointer after a tap", is => is(40, 40, white) && is(45, 40, white));

    const cdp = await context.newCDPSession(page);
    /**
     * Send one CDP touch event. Start and move list every finger that is down;
     * an end lists the fingers that lift, and an empty end or cancel lifts all.
     * @param {string} type - touchStart, touchMove, touchEnd, or touchCancel.
     * @param {[number, number, number][]} points - Logical x, y, and CDP ID of each finger.
     */
    const touch = (type, points) => cdp.send("Input.dispatchTouchEvent", {
      type, touchPoints: points.map(([x, y, id]) => ({ x: x * scale, y: y * scale, id })),
    });

    // A drag moves the first touch and the pointer with it.
    await touch("touchStart", [[60, 50, 0]]);
    await touch("touchMove", [[80, 60, 0]]);
    await frame(canvas, "the first finger dragging the pointer", is =>
      is(80, 60, touchColors[0]) && is(85, 60, white) && is(65, 50, background));

    // Five fingers are listed in landing order, and a sixth is ignored.
    const fingers = [[80, 60, 0], [150, 60, 1], [200, 60, 2], [240, 60, 3], [280, 60, 4]];
    for (let n = 2; n <= 5; n++) await touch("touchStart", fingers.slice(0, n));
    await touch("touchStart", [...fingers, [300, 30, 5]]);
    await frame(canvas, "five touch markers and no sixth", is =>
      fingers.every(([x, y], i) => is(x, y, touchColors[i])) && is(300, 30, background));

    // The first finger lifts: its release keeps the pointer, the rest move up
    // in order, and they no longer drive the pointer.
    await touch("touchEnd", [fingers[0]]);
    await touch("touchMove", [[150, 90, 1], ...fingers.slice(2), [300, 30, 5]]);
    await frame(canvas, "the remaining fingers after the first lifts", is =>
      is(150, 90, touchColors[0]) && is(200, 60, touchColors[1]) && is(280, 60, touchColors[3]) &&
      is(85, 60, white) && is(155, 90, background) && is(300, 30, background));
    await touch("touchEnd", []);
    await frame(canvas, "no touches after every finger lifts", is =>
      is(150, 90, background) && is(200, 60, background) && is(85, 60, white));

    // After all have lifted, the next finger drives the pointer again; a
    // cancel removes it.
    await touch("touchStart", [[100, 100, 7]]);
    await frame(canvas, "a new first finger", is => is(100, 100, touchColors[0]) && is(105, 100, white));
    await touch("touchCancel", []);
    await frame(canvas, "no touch after a cancel", is => is(100, 100, white) && is(105, 100, white));

    // Gamepads: slot 0 holds up on the left stick with south and D-pad up,
    // slot 1 has no standard mapping, and slot 3 leans right.
    await page.evaluate(() => {
      window.gojePads = [
        window.gojePad([0, 12], [0, -1, 0, 0]),
        window.gojePad([1], [0, 0, 0, 0], ""),
        null,
        window.gojePad([], [1, 0, 0, 0]),
      ];
    });
    await frame(canvas, "the standard pads", is =>
      is(24, 115, stick) && is(5, 161, buttonDown) && is(37, 169, buttonDown) && is(13, 161, buttonUp) &&
      is(104, 132, background) && is(281, 132, stick) && is(245, 161, buttonUp));
    await page.evaluate(() => { window.gojePads = [null, null, null, null]; });
    await frame(canvas, "no pads after they disconnect", is => is(24, 132, background) && is(264, 132, background));

    assert.deepEqual(errors, []);
  } finally {
    await browser.close();
  }
} finally {
  await promisify(server.close.bind(server))();
}
console.log("Input smoke passed: touch, multi-touch, and gamepads reached the Go host.");
