/** @file Serve the built Goje Playground on loopback for local use and browser tooling. */

import { once } from "node:events";
import { readFile } from "node:fs/promises";
import { createServer } from "node:http";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const defaultDirectory = fileURLToPath(new URL("../.build/web/", import.meta.url));
/** @type {Map<string, [string, string]>} */
const assets = new Map([
  ["/", ["index.html", "text/html; charset=utf-8"]],
  ["/wasm_exec.js", ["wasm_exec.js", "text/javascript; charset=utf-8"]],
  ["/playground.wasm", ["playground.wasm", "application/wasm"]],
]);

/**
 * Load the built playground assets and start serving them on loopback.
 * @param {number} port - TCP port to bind; use 0 to request an available port.
 * @param {string} [directory] - Build output directory; defaults to the ordinary playground build.
 * @returns {Promise<import("node:http").Server>} The listening server. The caller owns shutdown.
 * @throws {Error} The returned promise rejects if an asset cannot be read or the bind fails.
 */
export async function servePlayground(port, directory = defaultDirectory) {
  return serveAssets(port, directory, assets);
}

/**
 * Serve a fixed set of built assets without resolving request paths on disk.
 * @param {number} port - Loopback TCP port, or zero for an available port.
 * @param {string} directory - Build output directory.
 * @param {Map<string, [string, string]>} routes - URL paths mapped to filenames and MIME types.
 * @returns {Promise<import("node:http").Server>} The listening server.
 */
export async function serveAssets(port, directory, routes) {
  const files = new Map(await Promise.all(Array.from(routes, async ([path, [name, type]]) => {
    return [path, { type, bytes: await readFile(join(directory, name)) }];
  })));
  const server = createServer((request, response) => {
    const file = files.get(request.url?.split("?", 1)[0]);
    if (request.method !== "GET" && request.method !== "HEAD") {
      response.writeHead(405, { Allow: "GET, HEAD" }).end();
    } else if (!file) {
      response.writeHead(404).end();
    } else {
      response.writeHead(200, { "Content-Type": file.type, "Content-Length": file.bytes.length });
      response.end(request.method === "HEAD" ? undefined : file.bytes);
    }
  });
  server.listen(port, "127.0.0.1");
  await once(server, "listening");
  return server;
}

if (import.meta.main) {
  if (process.argv.length > 4) throw new Error("Usage: node scripts/playground-server.mjs [PORT] [WEB_DIR]");
  const server = await servePlayground(Number(process.argv[2] ?? 8080), process.argv[3]);
  console.log(`Goje Playground: http://127.0.0.1:${server.address().port}/`);
}
