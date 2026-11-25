/**
 * @file Check that the relay's WebSocket subset interoperates with Chromium's WebSocket.
 *
 * The script builds cmd/relay and the probe cmd/relay/internal/wsprobe, natively
 * and as WebAssembly, starts the relay on a loopback port, and drives Chromium's
 * own WebSocket from a page served on another loopback port. A browser socket
 * hosts each room. It checks the Welcome bytes, pairing, and ordered forwarding of 200
 * messages each way up to the 2048-byte limit with three guests: the native
 * netlink client, the WebAssembly probe running netlink's browser
 * implementation in the page, and a second browser socket. It then checks that
 * an oversized message closes the sender with status 1009 and that the host
 * receives PeerLeft. The message pattern matches `message` in the probe; keep
 * the two in step. The wire contract is in specs/relay-m16.md.
 */

import assert from "node:assert/strict";
import { execFile, execFileSync, spawn } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync } from "node:fs";
import { createServer } from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

import { chromium } from "playwright";

const count = 200;
const maxMessage = 2048;

/**
 * Install WebSocket helpers on the page. Runs in the page.
 * @param {{count: number, maxMessage: number}} limits - Message count and size limit.
 * @returns {void}
 */
function install({ count, maxMessage }) {
  /**
   * The i-th of n messages from the player in slot, as in the Go probe.
   * @param {number} slot - Sender's slot.
   * @param {number} i - Message index.
   * @param {number} n - Number of messages.
   * @returns {Uint8Array} The message.
   */
  function message(slot, i, n) {
    const size = i === n - 1 ? maxMessage : 1 + (i * 37) % maxMessage;
    return Uint8Array.from({ length: size }, (_, j) => (i * 7 + j + slot * 13) & 0xff);
  }

  /**
   * Open a socket whose messages can be awaited in order.
   * @param {string} url - Relay URL.
   * @returns {{ws: WebSocket, next: () => Promise<Uint8Array>, closed: Promise<CloseEvent>}} The socket.
   */
  function open(url) {
    const ws = new WebSocket(url);
    ws.binaryType = "arraybuffer";
    const queue = [], waiters = [];
    let closeEvent = null;
    const closed = new Promise(resolve => {
      ws.onclose = event => {
        closeEvent = event;
        for (const waiter of waiters.splice(0)) waiter.reject(new Error(`socket closed with status ${event.code}`));
        resolve(event);
      };
    });
    ws.onmessage = event => {
      const msg = new Uint8Array(event.data);
      if (waiters.length) waiters.shift().resolve(msg);
      else queue.push(msg);
    };
    const next = () => {
      if (queue.length) return Promise.resolve(queue.shift());
      if (closeEvent) return Promise.reject(new Error(`socket closed with status ${closeEvent.code}`));
      return new Promise((resolve, reject) => waiters.push({ resolve, reject }));
    };
    return { ws, next, closed };
  }

  /**
   * Await a Welcome and return the room code.
   * @param {ReturnType<typeof open>} socket - The socket.
   * @param {number} slot - Expected slot.
   * @returns {Promise<string>} The room code.
   */
  async function welcome(socket, slot) {
    const msg = await socket.next();
    if (msg.length !== 8 || msg[0] !== 1 || msg[1] !== slot) throw new Error(`bad welcome ${msg}`);
    const code = String.fromCharCode(...msg.subarray(2));
    if (!/^[ABCDEFGHJKMNPQRSTUVWXYZ23456789]{6}$/.test(code)) throw new Error(`bad room code ${code}`);
    return code;
  }

  /**
   * Await one relay message of exactly the given bytes.
   * @param {ReturnType<typeof open>} socket - The socket.
   * @param {number[]} bytes - Expected message.
   * @param {string} what - Name for the failure message.
   * @returns {Promise<void>} Resolves when it arrived.
   */
  async function expect(socket, bytes, what) {
    const msg = await socket.next();
    if (msg.length !== bytes.length || msg.some((b, i) => b !== bytes[i])) throw new Error(`expected ${what}, got ${msg.subarray(0, 8)}`);
  }

  /**
   * Send this side's messages and check the peer's, which arrive with the data type byte.
   * @param {ReturnType<typeof open>} socket - The socket.
   * @param {number} slot - This side's slot.
   * @returns {Promise<void>} Resolves when every message arrived in order.
   */
  async function exchange(socket, slot) {
    for (let i = 0; i < count; i++) socket.ws.send(message(slot, i, count));
    for (let i = 0; i < count; i++) {
      const msg = await socket.next();
      const want = message(1 - slot, i, count);
      if (msg[0] !== 4 || msg.length !== want.length + 1 || want.some((b, j) => b !== msg[j + 1])) {
        throw new Error(`slot ${slot}: message ${i} arrived as ${msg.length} bytes starting ${msg.subarray(0, 4)}`);
      }
    }
  }

  window.smoke = { open, welcome, expect, exchange, maxMessage };
}

const root = fileURLToPath(new URL("..", import.meta.url));
const go = process.env.GO ?? "go";
const bin = mkdtempSync(join(tmpdir(), "goje-smoke-ws-"));
let relay, server;
const browser = await chromium.launch({ timeout: 15_000 });
try {
  const relayPath = join(bin, "relay"), probePath = join(bin, "wsprobe"), wasmPath = join(bin, "wsprobe.wasm");
  execFileSync(go, ["build", "-o", relayPath, "./cmd/relay"], { cwd: root, stdio: "inherit" });
  execFileSync(go, ["build", "-o", probePath, "./cmd/relay/internal/wsprobe"], { cwd: root, stdio: "inherit" });
  execFileSync(go, ["build", "-o", wasmPath, "./cmd/relay/internal/wsprobe"], {
    cwd: root, stdio: "inherit", env: { ...process.env, GOOS: "js", GOARCH: "wasm" },
  });
  const goroot = execFileSync(go, ["env", "GOROOT"], { encoding: "utf8" }).trim();
  relay = spawn(relayPath, ["-addr", "127.0.0.1:0"], { stdio: ["ignore", "inherit", "pipe"] });
  const addr = await new Promise((resolve, reject) => {
    let log = "";
    relay.stderr.on("data", chunk => {
      log += chunk;
      const match = /addr=(\S+)/.exec(log);
      if (match) resolve(match[1]);
    });
    relay.on("exit", code => reject(new Error(`relay exited with ${code}: ${log}`)));
  });
  const url = `ws://${addr}`;

  // A loopback server delivers the page and the WebAssembly build of the probe.
  // Chromium's local network access checks would block loopback sockets from a
  // page Playwright fulfils itself, which has no address.
  const files = {
    "/": ["<!doctype html><title>smoke-ws</title>", "text/html"],
    "/wasm_exec.js": [readFileSync(join(goroot, "lib", "wasm", "wasm_exec.js")), "text/javascript"],
    "/wsprobe.wasm": [readFileSync(wasmPath), "application/wasm"],
  };
  server = createServer((req, res) => {
    const [body, contentType] = files[req.url] ?? ["not found", "text/plain"];
    res.writeHead(files[req.url] ? 200 : 404, { "content-type": contentType }).end(body);
  });
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  const page = await browser.newPage();
  await page.goto(`http://127.0.0.1:${server.address().port}/`);
  await page.addScriptTag({ url: "/wasm_exec.js" });
  await page.evaluate(install, { count, maxMessage });

  // Browser host, native guest.
  const code = await page.evaluate(async url => {
    window.host = smoke.open(`${url}/new`);
    return smoke.welcome(window.host, 0);
  }, url);
  const probe = promisify(execFile)(probePath, ["-url", `${url}/join?room=${code}`, "-n", String(count)]);
  await page.evaluate(async () => {
    await smoke.expect(window.host, [2], "PeerJoined");
    await smoke.exchange(window.host, 0);
    window.host.ws.close(1000);
    await window.host.closed;
  });
  const summary = JSON.parse((await probe).stdout);
  assert.deepEqual(summary, { slot: 1, code, sent: count, received: count });

  // Browser host, guest running netlink's browser implementation as WebAssembly.
  const wasmCode = await page.evaluate(async url => {
    window.host = smoke.open(`${url}/new`);
    return smoke.welcome(window.host, 0);
  }, url);
  const wasmRun = await page.evaluate(async ({ url, code, count }) => {
    const go = new Go();
    go.argv = ["wsprobe", "-url", `${url}/join?room=${code}`, "-n", String(count)];
    let exitCode = 0;
    go.exit = status => { exitCode = status; };
    // The Go runtime writes standard output and error through console.log.
    const lines = [], log = console.log;
    console.log = line => lines.push(String(line));
    try {
      const { instance } = await WebAssembly.instantiateStreaming(fetch("/wsprobe.wasm"), go.importObject);
      const run = go.run(instance);
      await smoke.expect(window.host, [2], "PeerJoined");
      await smoke.exchange(window.host, 0);
      window.host.ws.close(1000);
      await window.host.closed;
      await run;
    } finally {
      console.log = log;
    }
    return { exitCode, lines };
  }, { url, code: wasmCode, count });
  assert.equal(wasmRun.exitCode, 0, `the WebAssembly probe failed: ${wasmRun.lines.join("\n")}`);
  assert.deepEqual(JSON.parse(wasmRun.lines.at(-1)), { slot: 1, code: wasmCode, sent: count, received: count });

  // Two browser sockets, then an oversized message from the guest.
  const closeCode = await page.evaluate(async url => {
    const host = smoke.open(`${url}/new`);
    const room = await smoke.welcome(host, 0);
    const guest = smoke.open(`${url}/join?room=${room}`);
    if (await smoke.welcome(guest, 1) !== room) throw new Error("guest joined another room");
    await smoke.expect(host, [2], "PeerJoined");
    await Promise.all([smoke.exchange(host, 0), smoke.exchange(guest, 1)]);
    guest.ws.send(new Uint8Array(smoke.maxMessage + 1));
    const closed = await guest.closed;
    await smoke.expect(host, [3], "PeerLeft");
    await host.closed;
    return closed.code;
  }, url);
  assert.equal(closeCode, 1009, "an oversized message did not close the sender with status 1009");

  console.log(`passed: Chromium ${browser.version()}, the native client, and the WebAssembly client `
    + `exchanged ${count} messages each way through the relay, up to ${maxMessage} bytes, and an `
    + "oversized message closed the sender");
} finally {
  await browser.close();
  relay?.kill("SIGTERM");
  server?.close();
  rmSync(bin, { recursive: true, force: true });
}
