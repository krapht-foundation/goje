/** @file Serve the standalone Sound Maker and its Wasm runtime on loopback. */

import { fileURLToPath } from "node:url";

import { serveAssets } from "./playground-server.mjs";

const directory = fileURLToPath(new URL("../.build/soundmaker-web/", import.meta.url));
/** @type {Map<string, [string, string]>} */
const assets = new Map([
  ["/", ["index.html", "text/html; charset=utf-8"]],
  ["/editor.css", ["editor.css", "text/css; charset=utf-8"]],
  ["/editor.mjs", ["editor.mjs", "text/javascript; charset=utf-8"]],
  ["/soundmaker.wasm", ["soundmaker.wasm", "application/wasm"]],
  ["/wasm_exec.js", ["wasm_exec.js", "text/javascript; charset=utf-8"]],
]);

/**
 * Serve the built editor; it has no upload route.
 * @param {number} port - TCP port, or zero to bind an available port.
 * @returns {Promise<import("node:http").Server>} Listening server owned by the caller.
 */
export function serveSoundMaker(port) {
  return serveAssets(port, directory, assets);
}

if (import.meta.main) {
  const server = await serveSoundMaker(Number(process.argv[2] ?? 8085));
  console.log(`Goje Sound Maker: http://127.0.0.1:${server.address().port}/`);
}
