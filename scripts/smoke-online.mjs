/**
 * @file Play the online outpost between a native host and Chromium through a real relay.
 *
 * The script builds cmd/relay and the playground's smoke binary, starts the relay on a loopback
 * port, starts a headless native host with `--online-host`, and joins its room from the WebAssembly
 * playground in Chromium. Both players select their scouts, order them to the middle, and focus
 * fire. Each side logs the state hash it compares every 60 ticks; the script requires at least ten
 * checkpoints that appear on both sides with equal hashes. With `--fault` the host changes its hash
 * from tick 100, and both sides must end the match with a report of the divergence. The WebAssembly
 * build must exist in .build/web (make build-web). The protocol is in specs/lockstep-m16.md.
 */

import assert from "node:assert/strict";
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import { chromium } from "playwright";

import { servePlayground } from "./playground-server.mjs";

const fault = process.argv.includes("--fault");
const minimumCheckpoints = 10;
const root = fileURLToPath(new URL("..", import.meta.url));
const go = process.env.GO ?? "go";

/**
 * Ask the operating system for a free loopback port.
 * @returns {Promise<number>} The port, free at the moment of the call.
 */
function freePort() {
  return new Promise((resolve, reject) => {
    const probe = createServer();
    probe.once("error", reject);
    probe.listen(0, "127.0.0.1", () => {
      const { port } = probe.address();
      probe.close(() => resolve(port));
    });
  });
}

/**
 * Collect a child process's output lines and let callers wait for one.
 * @param {import("node:child_process").ChildProcess} child - The process.
 * @returns {{lines: string[], waitFor: (pattern: RegExp, ms: number) => Promise<RegExpMatchArray>}} The lines so far and a waiter.
 */
function watch(child) {
  const lines = [];
  let pending = "";
  const take = chunk => {
    pending += chunk;
    const parts = pending.split("\n");
    pending = parts.pop();
    lines.push(...parts);
  };
  child.stdout.on("data", take);
  child.stderr.on("data", take);
  const waitFor = async (pattern, ms) => {
    const deadline = Date.now() + ms;
    for (let seen = 0; Date.now() < deadline; await new Promise(r => setTimeout(r, 50))) {
      for (; seen < lines.length; seen++) {
        const match = lines[seen].match(pattern);
        if (match) return match;
      }
    }
    return assert.fail(`No line matching ${pattern} in ${ms} ms; the last lines were:\n${lines.slice(-12).join("\n")}`);
  };
  return { lines, waitFor };
}

/**
 * Parse the checkpoints in a list of log lines.
 * @param {string[]} lines - Log lines.
 * @returns {Map<number, string>} State hash by tick.
 */
function checkpoints(lines) {
  const found = new Map();
  for (const line of lines) {
    const match = line.match(/Online sync tick=(\d+) hash=([0-9a-f]+)/);
    if (match) found.set(Number(match[1]), match[2]);
  }
  return found;
}

const directory = mkdtempSync(join(tmpdir(), "goje-online-"));
/** @type {import("node:child_process").ChildProcess[]} */
const children = [];
let server, browser, hostLines = [], guestOutput = [];
try {
  const relayBinary = join(directory, "relay"), hostBinary = join(directory, "host");
  execFileSync(go, ["build", "-o", relayBinary, "./cmd/relay"], { cwd: root, stdio: "inherit" });
  execFileSync(go, ["build", "-tags", "goje_smoke", "-o", hostBinary, "./cmd/playground"], {
    cwd: root, stdio: "inherit",
  });

  const port = await freePort();
  const relayAddress = `ws://127.0.0.1:${port}`;
  const relay = spawn(relayBinary, ["-addr", `127.0.0.1:${port}`], { stdio: ["ignore", "pipe", "pipe"] });
  children.push(relay);
  await watch(relay).waitFor(/relay listening/, 10_000);

  const environment = { ...process.env, GOJE_RELAY: relayAddress };
  if (fault) environment.GOJE_ONLINE_FAULT = "100";
  const host = spawn(hostBinary, ["--online-host"], { env: environment, stdio: ["ignore", "pipe", "pipe"] });
  children.push(host);
  const hostLog = watch(host);
  hostLines = hostLog.lines;
  const hostExit = new Promise(resolve => host.once("exit", (code, signal) => resolve({ code, signal })));
  const [, code] = await hostLog.waitFor(/Online room opened code=([A-Z2-9]{6})/, 30_000);

  server = await servePlayground(0);
  browser = await chromium.launch({ timeout: 15_000 });
  const page = await (await browser.newContext({ viewport: { width: 640, height: 400 } })).newPage();
  const guestLines = guestOutput;
  const errors = [];
  page.on("console", message => guestLines.push(message.text()));
  page.on("pageerror", error => errors.push(error.message));
  const url = `http://127.0.0.1:${server.address().port}/?fixture=outpost&relay=${encodeURIComponent(relayAddress)}&room=${code}`;
  await page.goto(url, { timeout: 15_000 });
  const canvas = page.locator("#goje");
  await canvas.waitFor();

  /**
   * Wait until the guest has logged a line.
   * @param {RegExp} pattern - Pattern to find.
   * @param {number} ms - Time to wait.
   * @returns {Promise<void>} Resolves when the line appears.
   */
  const guestLog = async (pattern, ms) => {
    const deadline = Date.now() + ms;
    while (!guestLines.some(line => pattern.test(line))) {
      assert.ok(Date.now() < deadline, `The guest never logged ${pattern}; it logged:\n${guestLines.slice(-12).join("\n")}`);
      assert.deepEqual(errors, [], "The page raised an error");
      await new Promise(r => setTimeout(r, 50));
    }
  };
  /**
   * @param {number} x - Logical x on the 320 × 200 screen.
   * @param {number} y - Logical y.
   * @returns {Promise<[number, number]>} The page position of the logical pixel.
   */
  const at = async (x, y) => {
    const box = await canvas.boundingBox();
    return [box.x + (x + 0.5) * box.width / 320, box.y + (y + 0.5) * box.height / 200];
  };

  // The fifth title entry joins the room named by the page's parameters.
  await new Promise(r => setTimeout(r, 1_500));
  const [joinX, joinY] = await at(160, 92 + 16 * 4 + 8);
  await page.mouse.click(joinX, joinY);
  await guestLog(/Online match started/, 20_000);
  await hostLog.waitFor(/Online match started/, 10_000);

  if (fault) {
    await guestLog(/Online match ended/, 30_000);
    await hostLog.waitFor(/Online match ended/, 30_000);
    const { code: exitCode } = await hostExit;
    assert.equal(exitCode, 0, `The host exited with ${exitCode}`);
    assert.ok(guestLines.some(line => /no longer agree|desync/i.test(line)), "The guest did not report a divergence");
    console.log("Online smoke passed: an injected divergence stopped both players.");
  } else {
    // Select everything, send it to the middle, then focus fire once a second.
    const [fromX, fromY] = await at(1, 13), [toX, toY] = await at(318, 178);
    await page.mouse.move(fromX, fromY);
    await page.mouse.down();
    await page.mouse.move(toX, toY, { steps: 4 });
    await page.mouse.up();
    await new Promise(r => setTimeout(r, 100));
    const [middleX, middleY] = await at(Math.floor(16 * (3.5 - 4.5) + 144), Math.floor(8 * (3.5 + 4.5) + 20));
    await page.mouse.click(middleX, middleY, { button: "right" });
    const finished = Date.now() + 60_000;
    while (checkpoints(guestLines).size < 14 && Date.now() < finished) {
      await page.keyboard.press("f");
      await new Promise(r => setTimeout(r, 1_000));
      assert.ok(!guestLines.some(line => /Online match ended/.test(line)), `The match ended early:\n${guestLines.slice(-6).join("\n")}`);
    }
    const { code: exitCode } = await hostExit;
    assert.equal(exitCode, 0, `The host exited with ${exitCode}:\n${hostLog.lines.slice(-8).join("\n")}`);
    assert.deepEqual(errors, [], "The page raised an error");
    const mine = checkpoints(hostLog.lines), theirs = checkpoints(guestLines);
    const shared = [...mine.keys()].filter(tick => theirs.has(tick));
    assert.ok(shared.length >= minimumCheckpoints, `Only ${shared.length} checkpoints were logged by both players`);
    for (const tick of shared) assert.equal(theirs.get(tick), mine.get(tick), `The players' hashes differ at tick ${tick}`);
    console.log(`Online smoke passed: ${shared.length} state hashes matched between a native host and a Chromium guest.`);
  }
} catch (error) {
  console.error(`Host log:\n${hostLines.slice(-25).join("\n")}\nGuest log:\n${guestOutput.slice(-25).join("\n")}`);
  throw error;
} finally {
  await browser?.close();
  await new Promise(resolve => server ? server.close(resolve) : resolve());
  for (const child of children) child.kill();
  rmSync(directory, { recursive: true, force: true });
}
