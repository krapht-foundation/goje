/** @file Serve the standalone Font Editor and its Wasm runtime on loopback. */

import { fileURLToPath } from "node:url";

import { serveAssets } from "./playground-server.mjs";

const directory = fileURLToPath(new URL("../.build/fonteditor-web/", import.meta.url));
/** @type {Map<string, [string, string]>} */
const assets = new Map([
  ["/", ["index.html", "text/html; charset=utf-8"]],
  ["/editor.css", ["editor.css", "text/css; charset=utf-8"]],
  ["/editor.mjs", ["editor.mjs", "text/javascript; charset=utf-8"]],
  ["/fonteditor.wasm", ["fonteditor.wasm", "application/wasm"]],
  ["/wasm_exec.js", ["wasm_exec.js", "text/javascript; charset=utf-8"]],
]);

/**
 * Serve the built editor; it has no upload route.
 * @param {number} port - TCP port, or zero to bind an available port.
 * @returns {Promise<import("node:http").Server>} Listening server owned by the caller.
 */
export function serveFontEditor(port) {
  return serveAssets(port, directory, assets);
}

if (import.meta.main) {
  const server = await serveFontEditor(Number(process.argv[2] ?? 8086));
  console.log(`Goje Font Editor: http://127.0.0.1:${server.address().port}/`);
}
