/** @file Generate launcher, store, and favicon artwork from the approved Goje wordmark lockup. */

import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { PNG } from "pngjs";

const brand = new URL("../docs/brand/", import.meta.url);
const ink = "#24262e";
const backdrop = "#def4f1";
const androidNS = "http://schemas.android.com/apk/res/android";
const android = "android/res";
const ios = "ios/Assets.xcassets/AppIcon.appiconset";

/**
 * Package several PNG sizes in one Windows icon without resampling them.
 * @param {{size: number, png: Buffer}[]} images - Square PNG icons, smallest first.
 * @returns {Buffer} An ICO file with a directory entry for each PNG.
 */
function faviconICO(images) {
  const directory = Buffer.alloc(6 + images.length * 16);
  directory.writeUInt16LE(1, 2);
  directory.writeUInt16LE(images.length, 4);
  let offset = directory.length;
  for (const [i, { size, png }] of images.entries()) {
    const entry = 6 + i * 16;
    directory[entry] = directory[entry + 1] = size;
    directory.writeUInt16LE(1, entry + 4);
    directory.writeUInt16LE(32, entry + 6);
    directory.writeUInt32LE(png.length, entry + 8);
    directory.writeUInt32LE(offset, entry + 12);
    offset += png.length;
  }
  return Buffer.concat([directory, ...images.map(({ png }) => png)], offset);
}

const svg = await readFile(new URL("goje.svg", brand), "utf8");
const browser = await chromium.launch();
const files = new Map();
try {
  const page = await browser.newPage();
  const paths = await page.evaluate((source) => {
    const document = new DOMParser().parseFromString(source, "image/svg+xml");
    return [...document.querySelectorAll("#mascot > path")].map((path) => ({
      fill: path.getAttribute("fill"),
      d: path.getAttribute("d"),
    }));
  }, svg);
  assert.equal(paths.length, 5, "The Android themed icon expects the outline and eye paths from the approved mascot.");
  const [outline, , , eyes] = paths;
  for (const path of paths) {
    if (path.fill === "currentColor") path.fill = ink;
  }
  const artwork = paths.map(({ fill, d }) => `<path fill="${fill}" d="${d}"/>`).join("\n  ");
  const favicon = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 -2 24 24" role="img" aria-label="Goje" shape-rendering="crispEdges">\n  ${artwork}\n</svg>\n`;
  files.set("web/favicon.svg", favicon);

  /**
   * Rasterize the approved mascot on a platform-specific canvas; strip alpha for store icons.
   * @param {number} size - Square output size in pixels.
   * @param {"transparent" | "rounded" | "opaque"} background - Icon canvas treatment.
   * @returns {Promise<Buffer>} Encoded PNG.
   */
  async function render(size, background) {
    const tile = background === "transparent" ? favicon :
      `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 36 36">
        <rect width="36" height="36" rx="${background === "rounded" ? 8 : 0}" fill="${backdrop}"/>
        <g shape-rendering="crispEdges" transform="translate(6 8)">${artwork}</g>
      </svg>`;
    await page.setViewportSize({ width: size, height: size });
    const url = `data:image/svg+xml;base64,${Buffer.from(tile).toString("base64")}`;
    await page.setContent(`<img width="${size}" height="${size}" style="position:fixed;inset:0" alt="Goje" src="${url}">`);
    const image = await page.locator("img").screenshot({ omitBackground: background !== "opaque" });
    const png = PNG.sync.read(image);
    assert.equal(png.width, size);
    assert.equal(png.height, size);
    return PNG.sync.write(png, { colorType: background === "opaque" ? 2 : 6 });
  }

  const webSizes = [];
  for (const size of [16, 32, 48]) {
    const png = await render(size, "transparent");
    files.set(`web/favicon-${size}.png`, png);
    webSizes.push({ size, png });
  }
  files.set("web/favicon.ico", faviconICO(webSizes));
  files.set("web/apple-touch-icon.png", await render(180, "opaque"));

  for (const [density, size] of [["mdpi", 48], ["hdpi", 72], ["xhdpi", 96], ["xxhdpi", 144], ["xxxhdpi", 192]]) {
    files.set(`${android}/mipmap-${density}/goje_launcher.png`, await render(size, "rounded"));
  }
  files.set("android/play-store-icon.png", await render(512, "opaque"));
  files.set(`${android}/values/goje_icon_colors.xml`, `<resources>\n  <color name="goje_icon_background">${backdrop}</color>\n</resources>\n`);

  /**
   * Center the 48 × 40 dp mascot inside the adaptive icon's 66 dp safe circle.
   * @param {string} content - Android vector paths.
   * @returns {string} Android VectorDrawable XML.
   */
  const vector = (content) => `<vector xmlns:android="${androidNS}"
    android:width="108dp" android:height="108dp"
    android:viewportWidth="108" android:viewportHeight="108">
  <group android:translateX="30" android:translateY="34" android:scaleX="2" android:scaleY="2">
${content}
  </group>
</vector>\n`;
  files.set(`${android}/drawable/goje_icon_foreground.xml`, vector(paths.map(({ fill, d }) =>
    `    <path android:fillColor="${fill}" android:pathData="${d}"/>`).join("\n")));
  files.set(`${android}/drawable/goje_icon_monochrome.xml`, vector(
    `    <path android:fillColor="#ffffff" android:fillType="evenOdd" android:pathData="${outline.d} ${eyes.d}"/>`));
  for (const api of [26, 33]) {
    const monochrome = api === 33 ? '  <monochrome android:drawable="@drawable/goje_icon_monochrome"/>\n' : "";
    files.set(`${android}/mipmap-anydpi-v${api}/goje_launcher.xml`, `<adaptive-icon xmlns:android="${androidNS}">
  <background android:drawable="@color/goje_icon_background"/>
  <foreground android:drawable="@drawable/goje_icon_foreground"/>
${monochrome}</adaptive-icon>\n`);
  }

  files.set(`${ios}/AppIcon.png`, await render(1024, "opaque"));
  const info = { images: [{ filename: "AppIcon.png", idiom: "universal", platform: "ios", size: "1024x1024" }],
    info: { author: "xcode", version: 1 } };
  files.set(`${ios}/Contents.json`, `${JSON.stringify(info, null, 2)}\n`);
} finally {
  await browser.close();
}

for (const [name, content] of files) {
  const destination = new URL(name, brand);
  await mkdir(dirname(fileURLToPath(destination)), { recursive: true });
  await writeFile(destination, content);
}
console.log(`Generated ${files.size} brand assets.`);
