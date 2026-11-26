/** @file Install a `goje build` web directory as an offline web app in Chromium, replay it from the cache, then publish a newer build. */

import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { join } from "node:path";

import { chromium } from "playwright";
import { PNG } from "pngjs";

import { serveAssets } from "./playground-server.mjs";

const [directory, nextDirectory] = process.argv.slice(2);
if (!directory || !nextDirectory || process.argv.length !== 4) {
  throw new Error("Usage: node scripts/smoke-pwa.mjs WEB_DIR NEXT_WEB_DIR");
}

/** @type {Map<string, [string, string]>} Every file of a `goje build` web directory. */
const routes = new Map([
  ["/", ["index.html", "text/html; charset=utf-8"]],
  ["/index.html", ["index.html", "text/html; charset=utf-8"]],
  ["/wasm_exec.js", ["wasm_exec.js", "text/javascript; charset=utf-8"]],
  ["/game.wasm", ["game.wasm", "application/wasm"]],
  ["/manifest.webmanifest", ["manifest.webmanifest", "application/manifest+json"]],
  ["/icon-512.png", ["icon-512.png", "image/png"]],
  ["/sw.js", ["sw.js", "text/javascript; charset=utf-8"]],
]);

/**
 * @param {string} dir - A `goje build` web directory.
 * @returns {Promise<{hash: string, title: string}>} The content hash of its service worker cache and its page title.
 */
async function describe(dir) {
  const hash = /const cache = prefix \+ "([0-9a-f]{64})";/.exec(await readFile(join(dir, "sw.js"), "utf8"))?.[1];
  assert.ok(hash, `${dir}/sw.js names no content-hashed cache`);
  const title = /<title>(.*)<\/title>/.exec(await readFile(join(dir, "index.html"), "utf8"))?.[1];
  assert.ok(title, `${dir}/index.html has no title`);
  return { hash, title };
}
const { hash, title } = await describe(directory);
const next = await describe(nextDirectory);
assert.notEqual(next.hash, hash, "the next bundle has the same content hash");
assert.notEqual(next.title, title, "the next bundle cannot be told apart by its title");

let server = await serveAssets(0, directory, routes);
const port = server.address().port;
const origin = `http://127.0.0.1:${port}/`;
const browser = await chromium.launch({ timeout: 15_000 });
try {
  const context = await browser.newContext({ viewport: { width: 800, height: 640 } });
  const page = await context.newPage();
  page.setDefaultTimeout(10_000);
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));

  // An older cache of this scope must go; another app's cache on the origin must stay.
  await page.goto(`${origin}manifest.webmanifest`);
  await page.evaluate(async scope => {
    await caches.open(`goje ${scope} stale`);
    await caches.open("another app");
  }, origin);

  await page.goto(origin);
  await page.locator("#status").waitFor({ state: "hidden" });
  await page.evaluate(async () => {
    const worker = (await navigator.serviceWorker.ready).active;
    if (worker.state === "activated") return;
    await new Promise(resolve => worker.addEventListener("statechange", () => {
      if (worker.state === "activated") resolve();
    }));
  });
  const keys = await page.evaluate(() => caches.keys());
  assert.deepEqual(keys.toSorted(), ["another app", `goje ${origin} ${hash}`]);
  const manifest = await page.evaluate(async () => (await fetch("manifest.webmanifest")).json());
  assert.equal(manifest.scope, "./");
  assert.equal(manifest.start_url, "./");
  assert.ok(manifest.icons.some(icon => icon.purpose === "maskable" && icon.sizes === "512x512"),
    "the manifest has no 512 × 512 maskable icon");

  // Stop the server as well as the network, so only the cache can answer.
  server.closeAllConnections();
  await new Promise(resolve => server.close(resolve));
  await context.setOffline(true);
  await page.reload();
  assert.ok(await page.evaluate(() => navigator.serviceWorker.controller !== null),
    "the reloaded page is not controlled by the service worker");
  await page.locator("#status").waitFor({ state: "hidden" });
  const deadline = Date.now() + 5_000;
  let colors = 0;
  do {
    const png = PNG.sync.read(await page.locator("#goje").screenshot());
    const seen = new Set();
    for (let i = 0; i < png.data.length; i += 4) seen.add(png.data.readUInt32BE(i));
    colors = seen.size;
  } while (colors < 3 && Date.now() < deadline);
  assert.ok(colors >= 3, `the offline game drew ${colors} colors`);
  assert.deepEqual(errors, []);

  // Publish another bundle at the same origin, as an upload to the web host would. The controlled page must keep
  // its version, even across a reload, until every page of the scope has closed; only then may the new worker
  // take over and delete the old cache, leaving the cache of another app alone.
  server = await serveAssets(port, nextDirectory, routes);
  await context.setOffline(false);
  const installed = await page.evaluate(async () => {
    const registration = await navigator.serviceWorker.getRegistration();
    await registration.update();
    const worker = registration.installing ?? registration.waiting;
    if (worker.state !== "installed") {
      await new Promise(resolve => worker.addEventListener("statechange", () => {
        if (worker.state === "installed") resolve();
      }));
    }
    return { waiting: registration.waiting?.state, keys: (await caches.keys()).toSorted() };
  });
  assert.equal(installed.waiting, "installed", "the new worker did not wait for the old pages to close");
  assert.deepEqual(installed.keys, ["another app", `goje ${origin} ${hash}`, `goje ${origin} ${next.hash}`].toSorted());
  await page.reload();
  assert.equal(await page.title(), title, "a reload switched a controlled page to the new version");
  await page.close();

  // Each attempt is a page of the scope, which the old worker would keep alive, so close it before the next try.
  let seen = "";
  let finalKeys = [];
  for (const deadline = Date.now() + 10_000; seen !== next.title && Date.now() < deadline;) {
    const fresh = await context.newPage();
    await fresh.goto(origin);
    seen = await fresh.title();
    finalKeys = await fresh.evaluate(() => caches.keys());
    await fresh.close();
    if (seen !== next.title) await new Promise(resolve => setTimeout(resolve, 250));
  }
  assert.equal(seen, next.title, "the new version was not served after every page closed");
  assert.deepEqual(finalKeys.toSorted(), ["another app", `goje ${origin} ${next.hash}`]);
} finally {
  await browser.close();
  if (server.listening) {
    server.closeAllConnections();
    server.close();
  }
}
console.log("Offline web app smoke passed: the service worker precached the bundle under its content hash, " +
  "removed the stale cache of its scope only, served the game with the server and network down, and held a " +
  "controlled page on its version until every page closed before serving the next one");
