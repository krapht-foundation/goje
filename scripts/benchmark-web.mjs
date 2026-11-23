/** @file Benchmark one playground Wasm artifact with diagnostics off and on in Chromium. */

import { promisify } from "node:util";

import { chromium } from "playwright";

import { servePlayground } from "./playground-server.mjs";

const reportPrefix = '{"benchmark":"goje",';
const errorPrefix = "GOJE_BENCH_ERROR ";
const viewport = { width: 640, height: 400 };

/**
 * Recognize the benchmark's JSON, Go failures, or any browser console error.
 * @param {import("playwright").ConsoleMessage} message - Chromium console message.
 * @returns {boolean} Whether the message is a report or an actionable error.
 */
function isBenchmarkResult(message) {
  const text = message.text();
  return text.startsWith(reportPrefix) || text.startsWith(errorPrefix) || message.type() === "error";
}

/**
 * Run one sample window in a fresh, foreground page without injecting input.
 * @param {import("playwright").Browser} browser - Chromium instance shared by the runs.
 * @param {string} origin - Base URL for the benchmark Wasm artifact.
 * @param {"off" | "on"} mode - Whether the Go host enables diagnostics.
 * @param {"pickup" | "courtyard"} fixture - Scene under test.
 * @param {0 | 2048} sprites - Zero for the small scene, 2048 for the dense variant.
 * @returns {Promise<object>} Parsed Go report for the requested configuration.
 * @throws {Error} If the report is missing, reports a startup failure, or does not match the run.
 */
async function runBenchmark(browser, origin, mode, fixture, sprites) {
  const page = await browser.newPage({ viewport, deviceScaleFactor: 1 });
  try {
    await page.bringToFront();
    const url = new URL(origin);
    url.searchParams.set("bench", mode);
    url.searchParams.set("fixture", fixture);
    url.searchParams.set("sprites", String(sprites));
    const [message, response] = await Promise.all([
      page.waitForEvent("console", { predicate: isBenchmarkResult, timeout: 75_000 }),
      page.goto(url.href, { timeout: 15_000 }),
    ]);
    if (!response?.ok()) throw new Error(`HTTP ${response?.status()} loading the playground`);
    const line = message.text();
    if (!line.startsWith(reportPrefix)) throw new Error(line);
    const report = JSON.parse(line);
    if (report.mode !== mode || report.fixture !== fixture || report.sprites !== sprites) {
      throw new Error(`unexpected benchmark configuration: ${line}`);
    }
    return report;
  } catch (error) {
    throw new Error(`browser benchmark: fixture=${fixture}, mode=${mode}, sprites=${sprites}: ${error.message}`, {
      cause: error,
    });
  } finally {
    await page.close();
  }
}

const directory = process.argv[2];
const repetitions = Number(process.argv[3] ?? 3);
const fixture = process.argv[4] ?? "pickup";
if (!directory || !Number.isSafeInteger(repetitions) || repetitions < 1 ||
    !["pickup", "courtyard"].includes(fixture) || process.argv.length > 5) {
  throw new Error("Usage: node scripts/benchmark-web.mjs WEB_DIR [positive repetition count] [pickup|courtyard]");
}

const server = await servePlayground(0, directory);
try {
  const origin = `http://127.0.0.1:${server.address().port}/`;
  const browser = await chromium.launch({ headless: true, timeout: 15_000 });
  try {
    const chromiumVersion = browser.version();
    for (const sprites of [0, 2048]) {
      for (let repetition = 1; repetition <= repetitions; repetition++) {
        // Alternate which mode runs first to reduce systematic order effects.
        const modes = repetition % 2 === 1 ? ["off", "on"] : ["on", "off"];
        for (const mode of modes) {
          const report = await runBenchmark(browser, origin, mode, fixture, sprites);
          console.log(JSON.stringify({ ...report, repetition, chromium_version: chromiumVersion }));
        }
      }
    }
  } finally {
    await browser.close();
  }
} finally {
  await promisify(server.close.bind(server))();
}
