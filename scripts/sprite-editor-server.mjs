/** @file Serve the standalone Sprite Clip Editor and its Wasm runtime on loopback. */

import { fileURLToPath } from "node:url";

import { serveAssets } from "./playground-server.mjs";

const directory = fileURLToPath(new URL("../.build/spriteeditor-web/", import.meta.url));
/** @type {Map<string, [string, string]>} */
const assets = new Map([
  ["/", ["index.html", "text/html; charset=utf-8"]],
  ["/editor.css", ["editor.css", "text/css; charset=utf-8"]],
  ["/editor.mjs", ["editor.mjs", "text/javascript; charset=utf-8"]],
  ["/spriteeditor.wasm", ["spriteeditor.wasm", "application/wasm"]],
  ["/wasm_exec.js", ["wasm_exec.js", "text/javascript; charset=utf-8"]],
]);

/**
 * Serve the editor's static assets. Selected artwork is never sent to this server.
 * @param {number} port - TCP port, or zero to bind an available port.
 * @returns {Promise<import("node:http").Server>} Listening server owned by the caller.
 */
export function serveSpriteEditor(port) {
  return serveAssets(port, directory, assets);
}

if (import.meta.main) {
  const server = await serveSpriteEditor(Number(process.argv[2] ?? 8083));
  console.log(`Goje Sprite Clip Editor: http://127.0.0.1:${server.address().port}/`);
}
