/**
 * @file Play Beacon's challenge by touch in a running Android emulator or iOS simulator: find
 * the board in a screenshot, light the three beacons, check the win banner, and restart.
 * Usage: node scripts/smoke-mobile.mjs android SERIAL | ios UDID AXE. The app must be in front.
 */

import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { setTimeout as sleep } from "node:timers/promises";

import { PNG } from "pngjs";

const [platform, device, axe] = process.argv.slice(2);
if (!(platform === "android" && process.argv.length === 4) && !(platform === "ios" && process.argv.length === 5)) {
  throw new Error("Usage: node scripts/smoke-mobile.mjs android SERIAL | ios UDID AXE");
}

// The challenge's logical 160 × 128 frame and colors; see examples/beacon.
const beacons = [[32, 32], [96, 32], [32, 64]];
const unlit = [91, 141, 209];
const lit = [248, 190, 66];
const timerFull = [244, 241, 235];
const background = [20, 28, 41];
const banner = [17, 3];
const timer = [142, 7];

const scratch = mkdtempSync(join(tmpdir(), "goje-mobile-"));
/** @type {number} iOS points per screenshot pixel; Android taps use pixels. */
let pointsPerPixel = 1;
if (platform === "ios") {
  const scale = /@(\d)x/.exec(execFileSync(axe, ["displays", "--udid", device], { encoding: "utf8" }));
  assert.ok(scale, "axe displays reported no scale");
  pointsPerPixel = 1 / Number(scale[1]);
}

/** @returns {PNG} The current screen. */
function screenshot() {
  if (platform === "android") {
    return PNG.sync.read(execFileSync("adb", ["-s", device, "exec-out", "screencap", "-p"], { maxBuffer: 1 << 28 }));
  }
  const file = join(scratch, "screen.png");
  execFileSync(axe, ["screenshot", "--udid", device, "--output", file]);
  return PNG.sync.read(readFileSync(file));
}

/**
 * Touch the screen once.
 * @param {number} x - Screenshot pixel column.
 * @param {number} y - Screenshot pixel row.
 */
function tap(x, y) {
  const px = Math.round(x * pointsPerPixel), py = Math.round(y * pointsPerPixel);
  if (platform === "android") execFileSync("adb", ["-s", device, "shell", "input", "tap", `${px}`, `${py}`]);
  // A raw touch needs no accessibility hit test, which fails for a game canvas on a slow runner.
  else execFileSync(axe, ["touch", "-x", `${px}`, "-y", `${py}`, "--down", "--up", "--udid", device]);
}

/**
 * Find the centroids of the largest regions of one exact color.
 * @param {PNG} image - Screenshot.
 * @param {number[]} rgb - Color to find.
 * @returns {{x: number, y: number, n: number}[]} Regions by decreasing size.
 */
function regions(image, rgb) {
  const { width, height, data } = image;
  const seen = new Uint8Array(width * height);
  const found = [];
  const matches = i => data[i * 4] === rgb[0] && data[i * 4 + 1] === rgb[1] && data[i * 4 + 2] === rgb[2];
  for (let start = 0; start < width * height; start++) {
    if (seen[start] || !matches(start)) continue;
    let sx = 0, sy = 0, n = 0;
    const stack = [start];
    seen[start] = 1;
    while (stack.length) {
      const i = stack.pop(), x = i % width, y = (i - x) / width;
      sx += x; sy += y; n++;
      for (const j of [x > 0 ? i - 1 : -1, x < width - 1 ? i + 1 : -1, i - width, i + width]) {
        if (j >= 0 && j < width * height && !seen[j] && matches(j)) { seen[j] = 1; stack.push(j); }
      }
    }
    // Pixel i covers [i, i + 1), so the region's continuous center is half a pixel further.
    found.push({ x: sx / n + 0.5, y: sy / n + 0.5, n });
  }
  return found.sort((a, b) => b.n - a.n);
}

/**
 * Wait for the ready board and derive the logical-to-screen mapping from the three beacons.
 * @returns {Promise<(x: number, y: number) => [number, number]>} Maps a logical point to pixels.
 */
async function findBoard() {
  for (let attempt = 0; attempt < 90; attempt++) {
    const blobs = regions(screenshot(), unlit).slice(0, 3);
    if (blobs.length === 3 && blobs[2].n >= 4) {
      blobs.sort((a, b) => a.y - b.y || a.x - b.x);
      const [first, second, third] = blobs;
      const scale = (second.x - first.x) / (beacons[1][0] - beacons[0][0]);
      const toScreen = (x, y) => [first.x + (x - beacons[0][0]) * scale, first.y + (y - beacons[0][1]) * scale];
      const [ex, ey] = toScreen(...beacons[2]);
      // The third beacon checks the mapping that the first two define.
      if (scale > 0 && Math.abs(third.x - ex) < scale * 2 && Math.abs(third.y - ey) < scale * 2) return toScreen;
    }
    await sleep(1000);
  }
  throw new Error("Beacon's ready board did not appear");
}

/**
 * Read the color at the middle of a logical pixel.
 * @param {PNG} image - Screenshot.
 * @param {(x: number, y: number) => [number, number]} toScreen - Mapping from findBoard.
 * @param {number[]} point - Logical point.
 * @returns {number[]} RGB.
 */
function colorAt(image, toScreen, [x, y]) {
  const [px, py] = toScreen(x + 0.5, y + 0.5).map(Math.floor);
  const i = (py * image.width + px) * 4;
  return [image.data[i], image.data[i + 1], image.data[i + 2]];
}

/**
 * Poll until every logical point shows its color.
 * @param {(x: number, y: number) => [number, number]} toScreen - Mapping from findBoard.
 * @param {[number[], number[]][]} expected - Logical points and their colors.
 * @param {string} state - What the colors mean, for the error.
 */
async function expectColors(toScreen, expected, state) {
  let seen;
  for (let attempt = 0; attempt < 10; attempt++) {
    const image = screenshot();
    seen = expected.map(([point]) => colorAt(image, toScreen, point));
    if (expected.every(([, rgb], i) => rgb.every((c, k) => c === seen[i][k]))) return;
    await sleep(300);
  }
  assert.fail(`${state}: expected ${JSON.stringify(expected.map(([, rgb]) => rgb))}, saw ${JSON.stringify(seen)}`);
}

try {
  const toScreen = await findBoard();
  const ready = [[timer, timerFull], [banner, background], ...beacons.map(b => [b, unlit])];
  await expectColors(toScreen, ready, "ready");
  // The host buffers one press per tick, so wait for each tap to reach the game.
  for (const [i, beacon] of beacons.entries()) {
    tap(...toScreen(...beacon));
    await expectColors(toScreen, [[beacon, lit]], `beacon ${i + 1} lit by touch`);
  }
  await expectColors(toScreen, [[banner, lit], ...beacons.map(b => [b, lit])], "win");
  tap(...toScreen(80, 100));
  await expectColors(toScreen, ready, "restart");
  console.log(`passed: Beacon on ${platform} ${device}: ready board, win by touch, and restart by touch`);
} finally {
  rmSync(scratch, { recursive: true, force: true });
}
