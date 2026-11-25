/**
 * @file Run a Goje benchmark program in Chromium and print its JSON reports, one or two per run.
 *
 * Usage: node scripts/benchmark-collide.mjs WEB_DIR [repetitions] [collidebench | skirmishbench]
 * The program's WebAssembly build must be in WEB_DIR as PROGRAM.wasm next to wasm_exec.js.
 */

import { writeFile } from "node:fs/promises";
import { join } from "node:path";
import { promisify } from "node:util";

import { chromium } from "playwright";

import { serveAssets } from "./playground-server.mjs";

const pageFor = program => `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Goje benchmark: ${program}</title></head>
<body>
<script src="wasm_exec.js"></script>
<script>
  const go = new Go();
  WebAssembly.instantiateStreaming(fetch("${program}.wasm"), go.importObject)
    .then(result => go.run(result.instance))
    .catch(error => console.error("GOJE_BENCH_ERROR " + error));
</script>
</body>
</html>
`;

const directory = process.argv[2];
const repetitions = Number(process.argv[3] ?? 3);
const program = process.argv[4] ?? "collidebench";
if (!directory || !Number.isSafeInteger(repetitions) || repetitions < 1 || process.argv.length > 5 ||
    !["collidebench", "skirmishbench"].includes(program)) {
  throw new Error("Usage: node scripts/benchmark-collide.mjs WEB_DIR [positive repetition count] [collidebench | skirmishbench]");
}

await writeFile(join(directory, "index.html"), pageFor(program));
const server = await serveAssets(0, directory, new Map([
  ["/", ["index.html", "text/html; charset=utf-8"]],
  ["/wasm_exec.js", ["wasm_exec.js", "text/javascript; charset=utf-8"]],
  [`/${program}.wasm`, [`${program}.wasm`, "application/wasm"]],
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
        // The skirmish program prints two reports, the battle and then the same battle with group orders.
        const expected = program === "skirmishbench" ? 2 : 1;
        const lines = [];
        const reports = new Promise((resolve, reject) => {
          tab.on("console", message => {
            if (message.text().startsWith('{"target"')) {
              lines.push(message.text());
              if (lines.length === expected) resolve();
            } else if (message.type() === "error") {
              reject(new Error(message.text()));
            }
          });
          setTimeout(() => reject(new Error(`Only ${lines.length} of ${expected} reports arrived`)), 60_000).unref();
        });
        await Promise.all([reports, tab.goto(origin, { timeout: 15_000 })]);
        for (const line of lines) {
          console.log(JSON.stringify({ ...JSON.parse(line), repetition, chromium_version: browser.version() }));
        }
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
