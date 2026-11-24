/** @file Run the collision benchmark program in Chromium and print one JSON report per run. */

import { writeFile } from "node:fs/promises";
import { join } from "node:path";
import { promisify } from "node:util";

import { chromium } from "playwright";

import { serveAssets } from "./playground-server.mjs";

const page = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Goje collision benchmark</title></head>
<body>
<script src="wasm_exec.js"></script>
<script>
  const go = new Go();
  WebAssembly.instantiateStreaming(fetch("collidebench.wasm"), go.importObject)
    .then(result => go.run(result.instance))
    .catch(error => console.error("GOJE_BENCH_ERROR " + error));
</script>
</body>
</html>
`;

const directory = process.argv[2];
const repetitions = Number(process.argv[3] ?? 3);
if (!directory || !Number.isSafeInteger(repetitions) || repetitions < 1 || process.argv.length > 4) {
  throw new Error("Usage: node scripts/benchmark-collide.mjs WEB_DIR [positive repetition count]");
}

await writeFile(join(directory, "index.html"), page);
const server = await serveAssets(0, directory, new Map([
  ["/", ["index.html", "text/html; charset=utf-8"]],
  ["/wasm_exec.js", ["wasm_exec.js", "text/javascript; charset=utf-8"]],
  ["/collidebench.wasm", ["collidebench.wasm", "application/wasm"]],
]));
try {
  const origin = `http://127.0.0.1:${server.address().port}/`;
  const browser = await chromium.launch({ headless: true, timeout: 15_000 });
  try {
    for (let repetition = 1; repetition <= repetitions; repetition++) {
      // A fresh, foreground page per run keeps one run from warming the next.
      const tab = await browser.newPage();
      try {
        await tab.bringToFront();
        const [message] = await Promise.all([
          tab.waitForEvent("console", {
            predicate: m => m.text().startsWith('{"target"') || m.type() === "error",
            timeout: 60_000,
          }),
          tab.goto(origin, { timeout: 15_000 }),
        ]);
        const line = message.text();
        if (!line.startsWith('{"target"')) throw new Error(line);
        console.log(JSON.stringify({ ...JSON.parse(line), repetition, chromium_version: browser.version() }));
      } finally {
        await tab.close();
      }
    }
  } finally {
    await browser.close();
  }
} finally {
  await promisify(server.close.bind(server))();
}
