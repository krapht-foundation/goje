/** @file Bind browser files, controls, and preview playback to the Go sprite clip document. */

/**
 * @typedef {object} Frame
 * @property {number} x - Source left edge in sheet pixels.
 * @property {number} y - Source top edge in sheet pixels.
 * @property {number} width - Source width; the right edge is exclusive.
 * @property {number} height - Source height; the bottom edge is exclusive.
 * @property {number} pivotX - Pivot X relative to the frame's left edge.
 * @property {number} pivotY - Pivot Y relative to the frame's top edge.
 * @property {number} ticks - Duration in 60 Hz simulation ticks.
 */

/**
 * @typedef {object} Clip
 * @property {string} name - Unique clip name.
 * @property {Frame[]} frames - Ordered frames.
 */

/**
 * @typedef {object} Snapshot
 * @property {number} width - Sheet width, or 0 before an open.
 * @property {number} height - Sheet height, or 0 before an open.
 * @property {boolean} dirty - Whether metadata differs from the last open or download.
 * @property {boolean} canUndo - Whether undo is available.
 * @property {boolean} canRedo - Whether redo is available.
 * @property {Clip[]} clips - Ordered committed clips.
 */

/**
 * @typedef {object} Composition
 * @property {number} index - Zero-based frame index in the clip.
 * @property {number} x - Source left edge.
 * @property {number} y - Source top edge.
 * @property {number} width - Source width.
 * @property {number} height - Source height.
 * @property {number} dx - Destination left edge on the preview canvas.
 * @property {number} dy - Destination top edge on the preview canvas.
 * @property {number} dw - Destination width.
 * @property {number} dh - Destination height.
 */

/**
 * @typedef {object} Core
 * @property {number} maxPNGBytes - Encoded PNG limit.
 * @property {number} maxJSONBytes - Encoded sidecar limit.
 * @property {(png: Uint8Array, sidecar: Uint8Array | null) => string} stageOpen - Validate a candidate pair.
 * @property {(accept: boolean) => void} finishOpen - Replace the document with, or discard, the staged pair.
 * @property {() => Snapshot} snapshot - Read committed state.
 * @property {(target: Uint8ClampedArray) => string} pixels - Copy straight RGBA sheet pixels; returns an error or "".
 * @property {(action: string, clip: number, frame: number, value?: unknown) => string} edit - Apply one undoable edit.
 * @property {() => boolean} undo - Undo one edit.
 * @property {() => boolean} redo - Redo one edit.
 * @property {() => Uint8Array | string} exportJSON - Encode the sidecar or describe why it cannot be.
 * @property {() => void} markSaved - Mark the current metadata as handed off.
 * @property {(clip: number, ticks: number, anchorX: number, anchorY: number, scale: number) => Composition | null} preview - Advance and compose the preview.
 */

/**
 * Find a static element in the editor page.
 * @template {HTMLElement} T
 * @param {string} id - Element ID.
 * @returns {T} The corresponding element.
 */
function element(id) {
  return /** @type {T} */ (document.getElementById(id));
}

const TICK_MS = 1000 / 60;
// Ticks a single animation frame may consume; slower frames drop time instead of replaying it.
const MAX_CATCH_UP = 4;

const status = element("status");
const pngInput = /** @type {HTMLInputElement} */ (element("png-file"));
const jsonInput = /** @type {HTMLInputElement} */ (element("json-file"));
const clipSelect = /** @type {HTMLSelectElement} */ (element("clip"));
const frameSelect = /** @type {HTMLSelectElement} */ (element("frame"));
const clipName = /** @type {HTMLInputElement} */ (element("clip-name"));
const zoomSelect = /** @type {HTMLSelectElement} */ (element("zoom"));
const overlay = /** @type {HTMLInputElement} */ (element("overlay"));
const sheet = /** @type {HTMLCanvasElement} */ (element("sheet"));
const previewCanvas = /** @type {HTMLCanvasElement} */ (element("preview"));
const play = /** @type {HTMLButtonElement} */ (element("play"));
const step = /** @type {HTMLButtonElement} */ (element("step"));
const fieldsState = element("fields-state");
const keys = /** @type {const} */ (["x", "y", "width", "height", "pivotX", "pivotY", "ticks"]);
const fields = ["x", "y", "width", "height", "pivot-x", "pivot-y", "ticks"].map(id => /** @type {HTMLInputElement} */ (element(id)));
const sheetContext = sheet.getContext("2d");
const previewContext = previewCanvas.getContext("2d");
/** Holds the decoded sheet once per open so redraws never touch Go or the PNG. */
const bitmap = document.createElement("canvas");
const bitmapContext = bitmap.getContext("2d");

/** @type {Core} */
let core;
/** @type {Snapshot} */
let state = { width: 0, height: 0, dirty: false, canUndo: false, canRedo: false, clips: [] };
let clipIndex = -1;
let frameIndex = -1;
let revision = 0;
let downloadName = "sprite.json";
/** @type {{ id: number, x: number, y: number, before: string[] } | null} */
let drag = null;
let playing = false;
let raf = 0;
/** @type {number | null} */
let last = null;
let elapsed = 0;
let shown = -1;
let previewScale = 1;

/**
 * Show a message without treating filenames or diagnostics as markup.
 * @param {string} text - Visible diagnostic or status.
 * @param {boolean} [error] - Whether the message reports a failure.
 */
function message(text, error = false) {
  status.textContent = text;
  status.toggleAttribute("data-error", error);
}

/** @returns {number} The current integer view scale. */
function zoom() {
  return Number(zoomSelect.value);
}

/** @returns {Frame | undefined} The committed selected frame. */
function selectedFrame() {
  return state.clips[clipIndex]?.frames[frameIndex];
}

/**
 * Read the frame fields without committing them.
 * @returns {Frame | HTMLInputElement} The field values, or the first field that is not a whole number.
 */
function readFields() {
  const frame = /** @type {Frame} */ ({});
  for (let i = 0; i < keys.length; i++) {
    const value = fields[i].valueAsNumber;
    if (!Number.isInteger(value)) return fields[i];
    frame[keys[i]] = value;
  }
  return frame;
}

/**
 * Read the frame fields for a commit, reporting and focusing the first malformed one.
 * @returns {Frame | null} Field values, or null after reporting an error.
 */
function fieldsForCommit() {
  const frame = readFields();
  if (!(frame instanceof HTMLInputElement)) return frame;
  message(`${frame.labels?.[0]?.textContent ?? "Field"} must be a whole number.`, true);
  frame.focus();
  return null;
}

/** @param {Frame} frame - Values to show in the uncommitted fields. */
function writeFields(frame) {
  keys.forEach((key, i) => { fields[i].value = String(frame[key]); });
}

/** Describe whether the fields differ from committed metadata, and refresh dependent views. */
function fieldsChanged() {
  const ticks = fields[6].valueAsNumber;
  element("ms").textContent = Number.isFinite(ticks) ? `${(ticks * TICK_MS).toFixed(1)} ms` : "— ms";
  const frame = selectedFrame();
  const pending = !frame || keys.some((key, i) => fields[i].value !== String(frame[key]));
  fieldsState.toggleAttribute("data-pending", pending && Boolean(frame));
  fieldsState.textContent = !frame
    ? "These fields are not part of any clip yet. Add clip commits them as a new clip's first frame."
    : pending
      ? `Fields differ from frame ${frameIndex + 1}. Press Apply frame to commit them, or Add frame to append them.`
      : `Fields match frame ${frameIndex + 1}.`;
  drawSheet();
}

/**
 * Refresh controls from Go's committed state.
 * @param {boolean} sync - Whether to replace the frame fields and clip name with the selection.
 */
function render(sync) {
  state = core.snapshot();
  const open = state.width > 0;
  const clips = state.clips;
  clipIndex = clips.length ? Math.min(Math.max(clipIndex, 0), clips.length - 1) : -1;
  const frames = clips[clipIndex]?.frames ?? [];
  frameIndex = frames.length ? Math.min(Math.max(frameIndex, 0), frames.length - 1) : -1;

  clipSelect.replaceChildren(...clips.map((clip, i) => new Option(`${clip.name} (${clip.frames.length} frames)`, String(i))));
  clipSelect.selectedIndex = clipIndex;
  frameSelect.replaceChildren(...frames.map((f, i) =>
    new Option(`Frame ${i + 1}: ${f.width}×${f.height} at ${f.x}, ${f.y}; ${f.ticks} ticks`, String(i))));
  frameSelect.selectedIndex = frameIndex;

  element("dirty").textContent = state.dirty ? "Unsaved changes" : "Saved";
  /** @type {HTMLFieldSetElement} */ (element("view")).disabled = !open;
  /** @type {HTMLFieldSetElement} */ (element("clips")).disabled = !open;
  /** @type {HTMLFieldSetElement} */ (element("frames")).disabled = !open;
  const noClip = clipIndex < 0;
  for (const id of ["clip", "rename-clip", "delete-clip", "frame", "apply-frame", "add-frame", "duplicate-frame"]) {
    /** @type {HTMLButtonElement} */ (element(id)).disabled = noClip;
  }
  /** @type {HTMLButtonElement} */ (element("delete-frame")).disabled = frames.length < 2;
  /** @type {HTMLButtonElement} */ (element("move-up")).disabled = frameIndex < 1;
  /** @type {HTMLButtonElement} */ (element("move-down")).disabled = noClip || frameIndex >= frames.length - 1;
  /** @type {HTMLButtonElement} */ (element("export")).disabled = !clips.length;
  /** @type {HTMLButtonElement} */ (element("undo")).disabled = !state.canUndo;
  /** @type {HTMLButtonElement} */ (element("redo")).disabled = !state.canRedo;
  if (noClip && playing) setPlaying(false);
  play.disabled = noClip;
  step.disabled = noClip || playing;

  const frame = selectedFrame();
  if (sync && frame) {
    writeFields(frame);
    clipName.value = clips[clipIndex].name;
  }
  fieldsChanged();
  fitPreview();
  // Go resets playback when the clip or its frames changed; zero ticks only composes.
  advance(0, true);
}

/** Draw the sheet, committed frame outlines, and the uncommitted field rectangle. */
function drawSheet() {
  if (!sheetContext) return;
  const z = zoom();
  const w = Math.max(1, state.width * z);
  const h = Math.max(1, state.height * z);
  if (sheet.width !== w || sheet.height !== h) {
    sheet.width = w;
    sheet.height = h;
  }
  sheetContext.imageSmoothingEnabled = false;
  sheetContext.clearRect(0, 0, w, h);
  if (!state.width) return;
  sheetContext.drawImage(bitmap, 0, 0, w, h);

  /**
   * Outline a sheet rectangle with a dark halo so it shows on any pixels.
   * @param {Frame} f - Rectangle in sheet pixels.
   * @param {string} color - Stroke color.
   * @param {number[]} dash - Line dash pattern.
   */
  function outline(f, color, dash) {
    if (!sheetContext) return;
    sheetContext.setLineDash(dash);
    for (const [style, width] of [["#000", 3], [color, 1]]) {
      sheetContext.strokeStyle = style;
      sheetContext.lineWidth = /** @type {number} */ (width);
      sheetContext.strokeRect(f.x * z + .5, f.y * z + .5, f.width * z - 1, f.height * z - 1);
    }
  }

  const frames = state.clips[clipIndex]?.frames ?? [];
  if (overlay.checked) frames.forEach((f, i) => { if (i !== frameIndex) outline(f, "#7fd0ff", []); });
  const frame = selectedFrame();
  if (frame) outline(frame, "#ffd400", []);
  const draft = readFields();
  if (draft instanceof HTMLInputElement) return;
  outline(draft, "#ff4fd8", [4, 3]);
  const px = (draft.x + draft.pivotX) * z;
  const py = (draft.y + draft.pivotY) * z;
  sheetContext.setLineDash([]);
  sheetContext.fillStyle = "#ff4fd8";
  sheetContext.fillRect(px - 4, py, 9, 1);
  sheetContext.fillRect(px, py - 4, 1, 9);
}

/** Keep every pose in the selected clip visible at one scale around the fixed anchor. */
function fitPreview() {
  let x = 0, y = 0;
  for (const frame of state.clips[clipIndex]?.frames ?? []) {
    x = Math.max(x, frame.pivotX, frame.width - frame.pivotX);
    y = Math.max(y, frame.pivotY, frame.height - frame.pivotY);
  }
  const fit = Math.min((previewCanvas.width / 2 - 8) / x, (previewCanvas.height / 2 - 8) / y);
  previewScale = Math.min(zoom(), fit < 1 ? fit : Math.floor(fit));
}

/**
 * Advance the preview and redraw it only when the composed frame changes.
 * @param {number} ticks - Simulation ticks to consume.
 * @param {boolean} force - Whether to redraw even if the frame index is unchanged.
 */
function advance(ticks, force) {
  if (!previewContext || !core) return;
  const ax = previewCanvas.width / 2;
  const ay = previewCanvas.height / 2;
  const pose = core.preview(clipIndex, ticks, ax, ay, previewScale);
  const index = pose?.index ?? -1;
  if (!force && index === shown) return;
  shown = index;
  previewContext.imageSmoothingEnabled = false;
  previewContext.clearRect(0, 0, previewCanvas.width, previewCanvas.height);
  if (pose) previewContext.drawImage(bitmap, pose.x, pose.y, pose.width, pose.height, pose.dx, pose.dy, pose.dw, pose.dh);
  previewContext.fillStyle = "#c22";
  previewContext.fillRect(0, ay, previewCanvas.width, 1);
  previewContext.fillRect(ax, ay - 8, 1, 17);
  element("preview-frame").textContent = pose ? `Frame ${index + 1}` : "No clip";
}

/**
 * Consume elapsed time in whole ticks while playing.
 * @param {number} now - Animation frame timestamp in milliseconds.
 */
function loop(now) {
  raf = requestAnimationFrame(loop);
  if (last !== null) elapsed += now - last;
  last = now;
  let ticks = Math.floor(elapsed / TICK_MS);
  if (ticks > MAX_CATCH_UP) {
    ticks = MAX_CATCH_UP;
    elapsed = 0;
  } else {
    elapsed -= ticks * TICK_MS;
  }
  if (ticks) advance(ticks, false);
}

/** @param {boolean} on - Whether the preview should play. */
function setPlaying(on) {
  playing = on;
  play.textContent = on ? "Pause" : "Play";
  play.setAttribute("aria-pressed", String(on));
  step.disabled = on || clipIndex < 0;
  restartClock();
}

/** Schedule only visible playback and don't let hidden or paused time accumulate. */
function restartClock() {
  cancelAnimationFrame(raf);
  last = null;
  elapsed = 0;
  if (playing && !document.hidden) raf = requestAnimationFrame(loop);
}

/**
 * Apply one edit, keeping the fields untouched when Go rejects it.
 * @param {string} action - Edit action name.
 * @param {unknown} [value] - Action payload.
 * @returns {boolean} Whether the edit committed.
 */
function edit(action, value) {
  const error = value === undefined
    ? core.edit(action, clipIndex, frameIndex)
    : core.edit(action, clipIndex, frameIndex, value);
  if (error) message(error, true);
  return !error;
}

/**
 * Select a clip and frame, then show the committed state.
 * @param {number} clip - Clip index, clamped by render.
 * @param {number} frame - Frame index, clamped by render.
 * @param {string} text - Status describing what happened.
 */
function select(clip, frame, text) {
  clipIndex = clip;
  frameIndex = frame;
  render(true);
  message(text);
}

/**
 * Read, validate, and stage the selected pair, confirming before discarding unsaved edits.
 * @returns {Promise<void>} Resolves after the pair is handled or superseded.
 */
async function openFiles() {
  const ticket = ++revision;
  const png = pngInput.files?.[0];
  const json = jsonInput.files?.[0];
  if (!png) {
    message("Choose a sprite sheet PNG first.", true);
    return;
  }
  if (png.size > core.maxPNGBytes) {
    message(`${png.name} exceeds ${core.maxPNGBytes} bytes.`, true);
    return;
  }
  if (json && json.size > core.maxJSONBytes) {
    message(`${json.name} exceeds ${core.maxJSONBytes} bytes.`, true);
    return;
  }
  message(`Reading ${png.name}…`);
  let buffers;
  try {
    buffers = await Promise.all([png.arrayBuffer(), json?.arrayBuffer()]);
  } catch {
    if (ticket === revision) message("Could not read the selected files.", true);
    return;
  }
  // A newer file choice or Open wins, regardless of read order.
  if (ticket !== revision) return;
  const names = json ? `${png.name} and ${json.name}` : png.name;
  const error = core.stageOpen(new Uint8Array(buffers[0]), buffers[1] ? new Uint8Array(buffers[1]) : null);
  if (error) {
    message(`Could not open ${names}: ${error}`, true);
    return;
  }
  const accept = !core.snapshot().dirty || window.confirm(`Discard unsaved changes and open ${names}?`);
  if (accept) finishDrag(false);
  core.finishOpen(accept);
  if (!accept) {
    message("Open canceled; the current document is unchanged.");
    return;
  }
  const opened = core.snapshot();
  bitmap.width = opened.width;
  bitmap.height = opened.height;
  if (bitmapContext) {
    const image = bitmapContext.createImageData(opened.width, opened.height);
    const error = core.pixels(image.data);
    if (error) message(`Opened ${names}, but the sheet could not be shown: ${error}`, true);
    bitmapContext.putImageData(image, 0, 0);
    if (error) return render(true);
  }
  downloadName = json?.name ?? `${png.name.replace(/\.png$/i, "") || "sprite"}.json`;
  const fit = Math.max(1, Math.floor(512 / Math.max(opened.width, opened.height)));
  zoomSelect.value = String([...zoomSelect.options].map(o => Number(o.value)).filter(z => z <= fit).pop() ?? 1);
  if (!opened.clips.length) {
    // Start the uncommitted fields on the whole sheet, pivoted at its bottom center.
    writeFields({ x: 0, y: 0, width: opened.width, height: opened.height, pivotX: opened.width >> 1, pivotY: opened.height, ticks: 6 });
    clipName.value = "idle";
  }
  select(0, 0, `Opened ${names} (${opened.width} × ${opened.height}, ${opened.clips.length} clips).`);
}

/** Encode the sidecar and hand it to the browser, marking it clean only after handoff. */
function exportJSON() {
  const data = core.exportJSON();
  if (typeof data === "string") {
    message(`Download failed: ${data}`, true);
    return;
  }
  const url = URL.createObjectURL(new Blob([data], { type: "application/json" }));
  try {
    const link = document.createElement("a");
    link.href = url;
    link.download = downloadName;
    link.click();
  } catch (error) {
    URL.revokeObjectURL(url);
    message(`Download failed: ${error instanceof Error ? error.message : error}`, true);
    return;
  }
  // Revoke after the click's navigation task has taken the URL.
  setTimeout(() => URL.revokeObjectURL(url), 0);
  core.markSaved();
  render(false);
  message(`Handed ${downloadName} (${data.length} bytes) to the browser for download.`);
}

/**
 * Project a pointer onto sheet pixels, clamped to the sheet.
 * @param {PointerEvent} event - Pointer sample, possibly captured outside the sheet.
 * @returns {[number, number]} Pixel column and row.
 */
function pixelAt(event) {
  const rect = sheet.getBoundingClientRect();
  const x = Math.floor((event.clientX - rect.left) * state.width / rect.width);
  const y = Math.floor((event.clientY - rect.top) * state.height / rect.height);
  return [Math.min(Math.max(x, 0), state.width - 1), Math.min(Math.max(y, 0), state.height - 1)];
}

/** @param {PointerEvent} event - Sample extending the active drag rectangle. */
function extendDrag(event) {
  if (!drag) return;
  const [x, y] = pixelAt(event);
  const width = Math.abs(x - drag.x) + 1;
  const height = Math.abs(y - drag.y) + 1;
  fields[0].value = String(Math.min(x, drag.x));
  fields[1].value = String(Math.min(y, drag.y));
  fields[2].value = String(width);
  fields[3].value = String(height);
  // Keep a typed pivot when it still fits; otherwise use the bottom center.
  const pivotX = drag.before[4] === "" ? NaN : Number(drag.before[4]);
  const pivotY = drag.before[5] === "" ? NaN : Number(drag.before[5]);
  fields[4].value = String(Number.isInteger(pivotX) && pivotX >= 0 && pivotX <= width ? pivotX : width >> 1);
  fields[5].value = String(Number.isInteger(pivotY) && pivotY >= 0 && pivotY <= height ? pivotY : height);
  fieldsChanged();
}

/**
 * Settle the active drag before releasing capture, which itself dispatches an event.
 * @param {boolean} keep - Whether to keep the dragged rectangle in the fields.
 */
function finishDrag(keep) {
  if (!drag) return;
  const { id, before } = drag;
  drag = null;
  if (!keep) {
    fields.forEach((field, i) => { field.value = before[i]; });
    fieldsChanged();
  }
  if (sheet.hasPointerCapture(id)) sheet.releasePointerCapture(id);
  if (keep) message("Rectangle copied to the frame fields. Commit it with Apply frame, Add frame, or Add clip.");
}

sheet.addEventListener("pointerdown", event => {
  if (drag || event.button !== 0 || !state.width) return;
  event.preventDefault();
  const [x, y] = pixelAt(event);
  drag = { id: event.pointerId, x, y, before: fields.map(field => field.value) };
  sheet.setPointerCapture(event.pointerId);
  extendDrag(event);
});
sheet.addEventListener("pointermove", event => { if (event.pointerId === drag?.id) extendDrag(event); });
sheet.addEventListener("pointerup", event => {
  if (event.pointerId !== drag?.id) return;
  extendDrag(event);
  finishDrag(true);
});
for (const name of ["pointercancel", "lostpointercapture"]) {
  sheet.addEventListener(name, event => { if (/** @type {PointerEvent} */ (event).pointerId === drag?.id) finishDrag(false); });
}

for (const field of fields) field.addEventListener("input", fieldsChanged);
pngInput.addEventListener("change", () => { revision++; });
jsonInput.addEventListener("change", () => { revision++; });
element("open").addEventListener("click", () => void openFiles());
element("export").addEventListener("click", exportJSON);
element("undo").addEventListener("click", () => { if (core.undo()) select(clipIndex, frameIndex, "Undid the last edit."); });
element("redo").addEventListener("click", () => { if (core.redo()) select(clipIndex, frameIndex, "Redid the edit."); });
clipSelect.addEventListener("change", () => select(clipSelect.selectedIndex, 0, `Selected clip ${state.clips[clipSelect.selectedIndex]?.name}.`));
frameSelect.addEventListener("change", () => select(clipIndex, frameSelect.selectedIndex, `Selected frame ${frameSelect.selectedIndex + 1}.`));
zoomSelect.addEventListener("change", () => { drawSheet(); fitPreview(); advance(0, true); });
overlay.addEventListener("change", drawSheet);

element("add-clip").addEventListener("click", () => {
  const frame = fieldsForCommit();
  const name = clipName.value;
  if (frame && edit("addClip", { name, frame })) select(state.clips.length, 0, `Added clip ${name}.`);
});
element("rename-clip").addEventListener("click", () => {
  const name = clipName.value;
  if (edit("renameClip", name)) select(clipIndex, frameIndex, `Renamed the clip to ${name}.`);
});
element("delete-clip").addEventListener("click", () => {
  const name = state.clips[clipIndex].name;
  if (edit("deleteClip")) select(clipIndex, 0, `Deleted clip ${name}.`);
});
element("apply-frame").addEventListener("click", () => {
  const frame = fieldsForCommit();
  if (frame && edit("setFrame", frame)) select(clipIndex, frameIndex, `Applied frame ${frameIndex + 1}.`);
});
element("add-frame").addEventListener("click", () => {
  const frame = fieldsForCommit();
  const index = state.clips[clipIndex].frames.length;
  if (frame && edit("addFrame", frame)) select(clipIndex, index, `Added frame ${index + 1}.`);
});
element("duplicate-frame").addEventListener("click", () => {
  if (edit("duplicateFrame")) select(clipIndex, frameIndex + 1, `Duplicated frame ${frameIndex + 1}.`);
});
element("delete-frame").addEventListener("click", () => {
  const index = frameIndex;
  if (edit("deleteFrame")) select(clipIndex, index, `Deleted frame ${index + 1}.`);
});
element("move-up").addEventListener("click", () => {
  if (edit("moveFrame", frameIndex - 1)) select(clipIndex, frameIndex - 1, `Moved the frame up to position ${frameIndex}.`);
});
element("move-down").addEventListener("click", () => {
  if (edit("moveFrame", frameIndex + 1)) select(clipIndex, frameIndex + 1, `Moved the frame down to position ${frameIndex + 2}.`);
});
play.addEventListener("click", () => setPlaying(!playing));
step.addEventListener("click", () => advance(1, false));
document.addEventListener("visibilitychange", restartClock);
window.addEventListener("beforeunload", event => {
  if (!state.dirty) return;
  event.preventDefault();
  event.returnValue = "";
});

/**
 * Load Wasm and connect the page to its single document.
 * @returns {Promise<void>} Resolves when files can be opened.
 */
async function start() {
  if (!sheetContext || !previewContext || !bitmapContext) throw new Error("Canvas is unavailable.");
  const go = new Go();
  const response = await fetch("spriteeditor.wasm");
  if (!response.ok) throw new Error(`Wasm download failed: HTTP ${response.status}`);
  const { instance } = await WebAssembly.instantiateStreaming(response, go.importObject);
  go.run(instance).then(() => stop("The editor core stopped. Reload the page."), error => stop(error.message));
  core = globalThis.gojeSpriteEditor;
  if (!core) throw new Error("The editor core did not start.");
  render(true);
  /** @type {HTMLFieldSetElement} */ (element("files")).disabled = false;
  message("Ready. Choose a PNG, optionally its JSON sidecar, then press Open.");
}

/** @param {string} text - Diagnostic describing why editing stopped. */
function stop(text) {
  setPlaying(false);
  for (const id of ["files", "view", "clips", "frames"]) /** @type {HTMLFieldSetElement} */ (element(id)).disabled = true;
  play.disabled = step.disabled = true;
  message(text, true);
}

start().catch(error => stop(error.message));
