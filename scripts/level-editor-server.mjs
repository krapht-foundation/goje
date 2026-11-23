/** @file Serve the standalone Level Builder and its Wasm runtime on loopback. */

import { fileURLToPath } from "node:url";

import { serveAssets } from "./playground-server.mjs";

const directory = fileURLToPath(new URL("../.build/leveleditor-web/", import.meta.url));
/** @type {Map<string, [string, string]>} */
const assets = new Map([
  ["/", ["index.html", "text/html; charset=utf-8"]],
  ["/editor.css", ["editor.css", "text/css; charset=utf-8"]],
  ["/editor.mjs", ["editor.mjs", "text/javascript; charset=utf-8"]],
  ["/editor.wasm", ["editor.wasm", "application/wasm"]],
  ["/wasm_exec.js", ["wasm_exec.js", "text/javascript; charset=utf-8"]],
]);

/**
 * Serve the built editor on loopback. The server does not receive user levels.
 * @param {number} port - TCP port, or zero to bind an available port.
 * @returns {Promise<import("node:http").Server>} Listening server owned by the caller.
 */
export function serveLevelEditor(port) {
  return serveAssets(port, directory, assets);
}

if (import.meta.main) {
  const server = await serveLevelEditor(Number(process.argv[2] ?? 8081));
  console.log(`Goje Level Builder: http://127.0.0.1:${server.address().port}/`);
}
