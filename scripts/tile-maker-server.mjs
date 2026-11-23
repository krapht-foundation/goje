/** @file Serve the standalone Tile Maker and its Wasm runtime on loopback. */

import { fileURLToPath } from "node:url";

import { serveAssets } from "./playground-server.mjs";

const directory = fileURLToPath(new URL("../.build/tilemaker-web/", import.meta.url));
/** @type {Map<string, [string, string]>} */
const assets = new Map([
  ["/", ["index.html", "text/html; charset=utf-8"]],
  ["/tilemaker.css", ["tilemaker.css", "text/css; charset=utf-8"]],
  ["/tilemaker.mjs", ["tilemaker.mjs", "text/javascript; charset=utf-8"]],
  ["/tilemaker.wasm", ["tilemaker.wasm", "application/wasm"]],
  ["/wasm_exec.js", ["wasm_exec.js", "text/javascript; charset=utf-8"]],
]);

/**
 * Serve the built editor; it has no upload route.
 * @param {number} port - TCP port, or zero to bind an available port.
 * @returns {Promise<import("node:http").Server>} Listening server owned by the caller.
 */
export function serveTileMaker(port) {
  return serveAssets(port, directory, assets);
}

if (import.meta.main) {
  const server = await serveTileMaker(Number(process.argv[2] ?? 8082));
  console.log(`Goje Tile Maker: http://127.0.0.1:${server.address().port}/`);
}
