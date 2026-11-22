/** @file Serve the built Goje Playground on loopback for local use and browser smoke tests. */

import { once } from "node:events";
import { readFile } from "node:fs/promises";
import { createServer } from "node:http";

const root = new URL("../.build/web/", import.meta.url);
/** @type {Map<string, [string, string]>} */
const assets = new Map([
  ["/", ["index.html", "text/html; charset=utf-8"]],
  ["/wasm_exec.js", ["wasm_exec.js", "text/javascript; charset=utf-8"]],
  ["/playground.wasm", ["playground.wasm", "application/wasm"]],
]);

/**
 * Load the built playground assets and start serving them on loopback.
 * @param {number} port - TCP port to bind; use 0 to request an available port.
 * @returns {Promise<import("node:http").Server>} The listening server. The caller owns shutdown.
 * @throws {Error} The returned promise rejects if an asset cannot be read or the bind fails.
 */
export async function servePlayground(port) {
  const files = new Map(await Promise.all(Array.from(assets, async ([path, [name, type]]) => {
    return [path, { type, bytes: await readFile(new URL(name, root)) }];
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
  const server = await servePlayground(Number(process.argv[2] ?? 8080));
  console.log(`Goje Playground: http://127.0.0.1:${server.address().port}/`);
}
