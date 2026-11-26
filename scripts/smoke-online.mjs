/**
 * @file Play the online outpost between a native host and Chromium through a real relay.
 *
 * The script builds cmd/relay and the playground's smoke binary, starts the relay on a loopback
 * port, starts a headless native host with `--online-host`, and joins its room from the WebAssembly
 * playground in Chromium. Both players select their units, order them to the middle, and focus
 * fire. Each side logs the state hash it compares every 60 ticks; the script requires at least ten
 * checkpoints that appear on both sides with equal hashes, or, when a team wins sooner, every
 * checkpoint before the win. With `--fault` the host changes its hash from tick 100, and both sides
 * must end the match with a report of the divergence. With `--versus` the host picks Host versus and
 * commands the scouts while the guest commands the guards; unless a team wins first, the host leaves
 * through the menu at tick 720. Each side then watches the match log from its title menu, and the
 * replay must verify every tick that side recorded; the guest also switches the replay's fog with Tab.
 * With `--live`, join from a second browser page, cut each player transport, and verify that the
 * spectator's history still matches later checkpoints, accepts a fog switch, and can leave.
 * The WebAssembly build must exist in .build/web (make build-web). The protocol is in
 * specs/lockstep-m16.md.
 */

import assert from "node:assert/strict";
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import { createConnection, createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

import { chromium } from "playwright";

import { servePlayground } from "./playground-server.mjs";

const fault = process.argv.includes("--fault");
const versus = process.argv.includes("--versus");
const live = process.argv.includes("--live");
assert.ok(!live || versus, "--live requires --versus");
assert.ok(!(fault && versus), "--fault and --versus are separate runs");
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
 * Give one player its own loopback TCP route so the smoke can break its connection without
 * changing Outpost's state or affecting the other player or watcher.
 * @param {number} target - Relay port.
 * @returns {Promise<{address: string, cut: () => void, close: () => Promise<void>}>} The route.
 */
async function playerRoute(target) {
  const clients = new Set();
  const proxy = createServer(client => {
    const upstream = createConnection(target, "127.0.0.1");
    clients.add(client);
    client.on("error", () => client.destroy());
    upstream.on("error", () => upstream.destroy());
    client.on("close", () => { clients.delete(client); upstream.destroy(); });
    upstream.on("close", () => client.destroy());
    client.pipe(upstream).pipe(client);
  });
  await new Promise((resolve, reject) => {
    proxy.once("error", reject);
    proxy.listen(0, "127.0.0.1", resolve);
  });
  const cut = () => {
    assert.equal(clients.size, 1, "The player did not have one transport to cut");
    for (const client of clients) client.destroy();
  };
  return {
    address: `ws://127.0.0.1:${proxy.address().port}`,
    cut,
    close: () => new Promise(resolve => {
      for (const client of clients) client.destroy();
      proxy.close(resolve);
    }),
  };
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

/**
 * Require that a replay verified the whole log its player recorded: up to the winning tick, or
 * at least to the last checkpoint that player compared before the match was left.
 * @param {string} who - The player, for messages.
 * @param {number} ticks - The ticks the replay verified.
 * @param {Map<number, string>} compared - The player's checkpoints in the match.
 * @param {RegExpMatchArray | undefined} won - The player's completion line, if a team won.
 */
function checkReplay(who, ticks, compared, won) {
  if (won) {
    assert.equal(ticks, Number(won[2]), `The ${who}'s replay verified ${ticks} ticks of a match won at tick ${won[2]}`);
    return;
  }
  const last = Math.max(0, ...compared.keys());
  assert.ok(ticks >= last, `The ${who}'s replay verified ${ticks} ticks, short of its checkpoint at tick ${last}`);
}

const directory = mkdtempSync(join(tmpdir(), "goje-online-"));
/** @type {import("node:child_process").ChildProcess[]} */
const children = [];
let server, browser, hostLines = [], guestOutput = [];
const routes = [];
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

  if (live) {
    routes.push(await playerRoute(port));
    routes.push(await playerRoute(port));
  }
  const environment = { ...process.env, GOJE_RELAY: routes[0]?.address ?? relayAddress };
  if (fault) environment.GOJE_ONLINE_FAULT = "100";
  if (versus) environment.GOJE_ONLINE_VERSUS = "1";
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
  const base = `http://127.0.0.1:${server.address().port}/?fixture=outpost`;
  const url = `${base}&relay=${encodeURIComponent(routes[1]?.address ?? relayAddress)}&room=${code}`;
  await page.goto(url, { timeout: 15_000 });
  const canvas = page.locator("#goje");
  await canvas.waitFor();

  const sleep = ms => new Promise(r => setTimeout(r, ms));
  /**
   * Wait until the guest has logged a line.
   * @param {RegExp} pattern - Pattern to find.
   * @param {number} ms - Time to wait.
   * @returns {Promise<RegExpMatchArray>} The first matching line's match.
   */
  const guestLog = async (pattern, ms) => {
    const deadline = Date.now() + ms;
    for (;;) {
      const line = guestLines.find(text => pattern.test(text));
      if (line) return line.match(pattern);
      assert.ok(Date.now() < deadline, `The guest never logged ${pattern}; it logged:\n${guestLines.slice(-12).join("\n")}`);
      assert.deepEqual(errors, [], "The page raised an error");
      await sleep(50);
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
  /**
   * Press keys far enough apart that each lands on its own tick.
   * @param {...string} keys - Playwright key names.
   */
  const press = async (...keys) => {
    for (const key of keys) {
      await page.keyboard.press(key);
      await sleep(150);
    }
  };

  // With a relay the title lists Start, Load, Watch last replay, Guards, Watch last online match,
  // Host co-op, Host versus, and Join room in rows of 12 from y 76; the eighth joins the page's room.
  await sleep(1_500);
  const [joinX, joinY] = await at(160, 76 + 12 * 7 + 6);
  await page.mouse.click(joinX, joinY);
  const mode = versus ? 1 : 0;
  await guestLog(new RegExp(`Online match started player=1 mode=${mode}\\b`), 20_000);
  await hostLog.waitFor(new RegExp(`Online match started player=0 mode=${mode}\\b`), 10_000);
  if (live) {
    assert.equal(checkpoints(hostLines).size, 0, "Early cut missed the first checkpoint");
    routes[0].cut();
    await hostLog.waitFor(/Online match rejoined player=0 tick=0 epoch=2\b/, 25_000);
    await guestLog(/Online match rejoined player=1 tick=0 epoch=2\b/, 25_000);
  }

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
    await sleep(100);
    // Both boards put ground (x, y) at logical (16(x - y) + 144, 8(x + y) + 20).
    const [goalX, goalY] = versus ? [5.5, 4.5] : [3.5, 4.5];
    const [middleX, middleY] = await at(Math.floor(16 * (goalX - goalY) + 144), Math.floor(8 * (goalX + goalY) + 20));
    await page.mouse.click(middleX, middleY, { button: "right" });
    const won = /Online match completed result=(\w+) tick=(\d+)/, ended = /Online match ended reason=(.*)/;
    if (live) {
      const [, viewCode] = await hostLog.waitFor(/Online spectator invitation code=([A-Z2-9]{6})/, 2_000);
      assert.notEqual(viewCode, code);
      await guestLog(/Online sync tick=120 /, 20_000);
      const observer = await (await browser.newContext({ viewport: { width: 640, height: 400 } })).newPage();
      const viewed = new Map();
      const viewLines = [];
      observer.on("console", message => {
        const line = message.text();
        viewLines.push(line);
        const checkpoint = /Live view verified tick=(\d+) hash=([0-9a-f]+)/.exec(line);
        if (checkpoint) viewed.set(Number(checkpoint[1]), checkpoint[2]);
      });
      observer.on("pageerror", error => errors.push(error.message));
      await observer.goto(`${base}&relay=${encodeURIComponent(relayAddress)}&watch=${viewCode}`, { timeout: 15_000 });
      const viewCanvas = observer.locator("#goje");
      await viewCanvas.waitFor();
      const viewBox = await viewCanvas.boundingBox();
      await sleep(850);
      await observer.mouse.click(viewBox.x + viewBox.width / 2, viewBox.y + viewBox.height * (76 + 12 * 7 + 6) / 200);
      /** @param {() => boolean} ready - Whether the observer reached the expected checkpoint/state. */
      const waiting = async ready => {
        const deadline = Date.now() + 18_000;
        while (!ready()) {
          assert.ok(Date.now() < deadline, `Spectator did not make progress:\n${viewLines.slice(-12).join("\n")}`);
          assert.deepEqual(errors, [], "A player or the spectator raised an error");
          await sleep(50);
        }
      };
      await waiting(() => [...viewed.keys()].some(tick => tick >= 120));
      await observer.keyboard.press("Tab");
      await waiting(() => viewLines.some(line => /Live view fog team=/.test(line)));

      for (const [slot, route] of routes.entries()) {
        route.cut();
        const epoch = slot + 3;
        const [, hostTick] = await hostLog.waitFor(new RegExp(`Online match rejoined player=0 tick=(\\d+) epoch=${epoch}\\b`), 25_000);
        const [, guestTick] = await guestLog(new RegExp(`Online match rejoined player=1 tick=(\\d+) epoch=${epoch}\\b`), 25_000);
        assert.equal(hostTick, guestTick, "Players rebuilt different checkpoints");
        const next = Number(hostTick) + 120;
        const pattern = new RegExp(`Online sync tick=${next} hash=([0-9a-f]+)`);
        const [, hash] = await guestLog(pattern, 25_000);
        const [, hostHash] = await hostLog.waitFor(pattern, 25_000);
        assert.equal(hash, hostHash);
        await waiting(() => viewed.has(next));
        assert.equal(viewed.get(next), hash, `Spectator diverged after rejoin in epoch ${epoch}`);
      }
      await observer.keyboard.press("Escape");
      await observer.close();
      console.log("Live Outpost smoke passed: both players rejoined before the first checkpoint; a spectator " +
        "caught up, switched fog, and verified a later shared checkpoint after a cut to each player.");
    }
    const finished = Date.now() + 60_000;
    while (Date.now() < finished && !guestLines.some(line => won.test(line))) {
      if (versus ? guestLines.some(line => ended.test(line)) : checkpoints(guestLines).size >= 14) break;
      if (!versus) {
        assert.ok(!guestLines.some(line => ended.test(line)), `The match ended early:\n${guestLines.slice(-6).join("\n")}`);
      }
      await page.keyboard.press("f");
      await sleep(1_000);
    }

    // A team may win legitimately; otherwise co-op plays on and the versus host leaves at tick 720.
    if (versus) await guestLog(/Online match (completed|ended)/, 30_000);
    const guestWon = guestLines.find(line => won.test(line))?.match(won);
    const hostWon = guestWon && await hostLog.waitFor(won, 10_000);
    if (guestWon) {
      assert.equal(hostWon[2], guestWon[2], "The players' matches were won at different ticks");
      const expected = versus ? { victory: "defeat", defeat: "victory", draw: "draw" }[hostWon[1]] : hostWon[1];
      assert.equal(guestWon[1], expected, `The host's ${hostWon[1]} does not fit the guest's ${guestWon[1]}`);
    } else if (versus) {
      const [, reason] = guestLines.find(line => ended.test(line)).match(ended);
      assert.match(reason, /left/, "The guest's match should end because the host left");
    }

    let guestReplay;
    if (versus) {
      // Enter leaves the end screen; click the fifth title entry without depending
      // on the mouse hover or the menu's initially selected row.
      await press("Enter");
      await page.mouse.click(...await at(160, 76 + 12 * 4 + 6));
      guestReplay = Number((await guestLog(/Online replay verified ticks=(\d+)/, 60_000))[1]);
      // The replay has ended, so the top bar's team counts change only with the fog's team.
      const topBar = async () => {
        const box = await canvas.boundingBox();
        return page.screenshot({ clip: { x: box.x, y: box.y, width: box.width / 2, height: box.height * 12 / 200 } });
      };
      await sleep(300);
      const scouts = await topBar();
      await press("Tab");
      const guards = await topBar();
      await press("Tab");
      const back = await topBar();
      assert.ok(!scouts.equals(guards), "Tab did not switch the replay to the guards' fog");
      assert.ok(scouts.equals(back), "A second Tab did not switch the replay back to the scouts' fog");
    }

    const { code: exitCode } = await hostExit;
    assert.equal(exitCode, 0, `The host exited with ${exitCode}:\n${hostLog.lines.slice(-8).join("\n")}`);
    assert.deepEqual(errors, [], "The page raised an error");
    const mine = checkpoints(hostLog.lines), theirs = checkpoints(guestLines);
    const shared = [...mine.keys()].filter(tick => theirs.has(tick));
    const required = guestWon
      ? Math.max(1, Math.min(minimumCheckpoints, Math.floor(Number(guestWon[2]) / 60)))
      : minimumCheckpoints;
    assert.ok(shared.length >= required, `Only ${shared.length} checkpoints were logged by both players`);
    for (const tick of shared) assert.equal(theirs.get(tick), mine.get(tick), `The players' hashes differ at tick ${tick}`);
    const outcome = guestWon ? `the guest's ${guestWon[1]} at tick ${guestWon[2]}` : "no winner";
    if (versus) {
      const hostReplay = Number((await hostLog.waitFor(/Online replay verified ticks=(\d+)/, 1_000))[1]);
      checkReplay("host", hostReplay, mine, hostWon);
      checkReplay("guest", guestReplay, theirs, guestWon);
      console.log(`Online versus smoke passed: ${shared.length} state hashes matched, ${outcome}; ` +
        `the host's replay verified ${hostReplay} ticks and the guest's ${guestReplay}, and Tab switched the fog.`);
    } else {
      console.log(`Online smoke passed: ${shared.length} state hashes matched between a native host and a Chromium guest, ${outcome}.`);
    }
  }
} catch (error) {
  console.error(`Host log:\n${hostLines.slice(-25).join("\n")}\nGuest log:\n${guestOutput.slice(-25).join("\n")}`);
  throw error;
} finally {
  await browser?.close();
  await new Promise(resolve => server ? server.close(resolve) : resolve());
  await Promise.all(routes.map(route => route.close()));
  for (const child of children) child.kill();
  rmSync(directory, { recursive: true, force: true });
}
