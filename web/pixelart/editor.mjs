/** @file Bind browser input, drawing, and PNG handoff to the Go Pixel Art Maker document. */

/**
 * Find a static element in the Pixel Art Maker page.
 * @template {Element} T
 * @param {string} id - Element ID.
 * @returns {T} The corresponding element.
 */
function element(id) {
  return /** @type {T} */ (document.getElementById(id));
}

const sheetControls = /** @type {HTMLFieldSetElement} */ (element("sheet"));
const toolControls = /** @type {HTMLFieldSetElement} */ (element("tools"));
const widthInput = /** @type {HTMLInputElement} */ (element("width"));
const heightInput = /** @type {HTMLInputElement} */ (element("height"));
const newButton = /** @type {HTMLButtonElement} */ (element("new"));
const openButton = /** @type {HTMLButtonElement} */ (element("open"));
const fileInput = /** @type {HTMLInputElement} */ (element("file"));
const exportButton = /** @type {HTMLButtonElement} */ (element("export"));
const undo = /** @type {HTMLButtonElement} */ (element("undo"));
const redo = /** @type {HTMLButtonElement} */ (element("redo"));
const pencil = element("pencil");
const eraser = element("eraser");
const color = /** @type {HTMLInputElement} */ (element("color"));
const swatches = /** @type {HTMLButtonElement[]} */ ([...element("palette").querySelectorAll("button")]);
const zoomSelect = /** @type {HTMLSelectElement} */ (element("zoom"));
const status = element("status");
const coordinate = element("coordinate");
const dirtyOutput = element("dirty");
const grid = /** @type {HTMLElement} */ (element("grid"));
const lines = /** @type {HTMLElement} */ (element("lines"));
const cursor = /** @type {HTMLElement} */ (element("cursor"));
const art = /** @type {HTMLCanvasElement} */ (element("art"));
const nativeCanvas = /** @type {HTMLCanvasElement} */ (element("native"));
const preview = /** @type {HTMLElement} */ (element("preview"));
const artContext = art.getContext("2d");
const nativeContext = nativeCanvas.getContext("2d");

/**
 * Load Wasm and connect the page to its single document.
 * @returns {Promise<void>} Resolves when sheets can be created or opened.
 */
async function start() {
  if (!artContext || !nativeContext) throw new Error("Canvas is unavailable.");
  const go = new Go();
  const response = await fetch("pixelart.wasm");
  if (!response.ok) throw new Error(`Wasm download failed: HTTP ${response.status}`);
  const { instance } = await WebAssembly.instantiateStreaming(response, go.importObject);
  go.run(instance).then(() => stop("The Pixel Art Maker core stopped. Reload the page."), error => stop(error.message));
  const core = globalThis.gojePixelArt;
  if (!core) throw new Error("The Pixel Art Maker core did not start.");
  const { maxAxis, maxPNGBytes } = core;
  widthInput.max = heightInput.max = String(maxAxis);
  let width = 0;
  let height = 0;
  /** @type {ImageData | null} */
  let frame = null;
  let x = 0;
  let y = 0;
  let dirty = false;
  let erasing = false;
  /** @type {number | null} */
  let pointer = null;
  // Bumped by every document action so an older file read cannot replace newer work.
  let revision = 0;
  let pendingFrame = 0;
  let downloadName = "sprite-sheet.png";

  /** Copy the Go pixels into both canvases once and refresh document controls. */
  function render() {
    if (pendingFrame) cancelAnimationFrame(pendingFrame);
    pendingFrame = 0;
    const state = core.state();
    dirty = state.dirty;
    dirtyOutput.textContent = width === 0 ? "No sheet open" : dirty ? "Unsaved changes" : "Saved";
    undo.disabled = !state.canUndo;
    redo.disabled = !state.canRedo;
    newButton.disabled = openButton.disabled = pointer !== null;
    exportButton.disabled = pointer !== null || width === 0;
    if (!frame) return;
    const error = core.pixels(frame.data);
    if (error) {
      message(error, true);
      return;
    }
    artContext.putImageData(frame, 0, 0);
    nativeContext.putImageData(frame, 0, 0);
  }

  /** Coalesce pointer-driven redraws into at most one per display frame. */
  function scheduleRender() {
    pendingFrame ||= requestAnimationFrame(render);
  }

  /** Resize the view for the document that finishOpen just made live. */
  function load() {
    ({ width, height } = core.state());
    widthInput.value = String(width);
    heightInput.value = String(height);
    art.width = nativeCanvas.width = width;
    art.height = nativeCanvas.height = height;
    frame = new ImageData(width, height);
    const longest = Math.max(width, height);
    // Pick the largest zoom that keeps the sheet within a comfortable on-screen size.
    zoomSelect.value = [...zoomSelect.options].map(o => o.value).filter(z => longest * Number(z) <= 640).pop() ?? "1";
    grid.hidden = preview.hidden = false;
    grid.tabIndex = 0;
    layout();
    move(0, 0);
    render();
  }

  /** Apply the CSS zoom; the canvas keeps native backing pixels. */
  function layout() {
    const zoom = Number(zoomSelect.value);
    grid.style.setProperty("--z", String(zoom));
    grid.style.width = `${width * zoom}px`;
    grid.style.height = `${height * zoom}px`;
    lines.hidden = zoom < 4;
    move(x, y);
  }

  /**
   * Set the active pixel and the visible coordinate.
   * @param {number} column - In-bounds pixel column.
   * @param {number} row - In-bounds pixel row.
   */
  function move(column, row) {
    x = column;
    y = row;
    const zoom = Number(zoomSelect.value);
    cursor.style.transform = `translate(${x * zoom}px, ${y * zoom}px)`;
    coordinate.textContent = `(${x}, ${y})`;
  }

  /**
   * Project a pointer onto the grid, including coordinates beyond its edges.
   * @param {PointerEvent} event - Pointer sample, which may be captured outside the grid.
   * @returns {[number, number]} Pixel column and row.
   */
  function pixelAt(event) {
    const rect = grid.getBoundingClientRect();
    return [Math.floor((event.clientX - rect.left) * width / rect.width), Math.floor((event.clientY - rect.top) * height / rect.height)];
  }

  /** @returns {number} The RGB pencil color, or -1 for the eraser. */
  function ink() {
    return erasing ? -1 : Number.parseInt(color.value.slice(1), 16);
  }

  /**
   * Apply a pointer sample; Go breaks interpolation when it falls outside the grid.
   * @param {PointerEvent} event - Active pointer sample.
   */
  function extend(event) {
    const [column, row] = pixelAt(event);
    core.extend(column, row);
    if (column >= 0 && column < width && row >= 0 && row < height) move(column, row);
  }

  /**
   * Settle the active stroke before releasing capture, which itself dispatches an event.
   * @param {boolean} commit - Whether to keep the stroke.
   */
  function finish(commit) {
    if (pointer === null) return;
    const id = pointer;
    pointer = null;
    core.end(commit);
    if (grid.hasPointerCapture(id)) grid.releasePointerCapture(id);
    render();
    if (!commit) message("Canceled the stroke.");
  }

  /**
   * Ask before a staged candidate replaces unsaved work, then swap or discard it.
   * @param {string} prompt - Confirmation shown when the current sheet has unsaved changes.
   * @returns {boolean} Whether the candidate became the live document.
   */
  function replace(prompt) {
    const accepted = !dirty || window.confirm(prompt);
    core.finishOpen(accepted);
    if (accepted) load();
    else message("Kept the current sheet.");
    return accepted;
  }

  /**
   * Validate a chosen file before asking to discard edits.
   * @param {File} file - File explicitly selected by the user.
   * @returns {Promise<void>} Resolves after the candidate is handled or superseded.
   */
  async function openPNG(file) {
    const ticket = ++revision;
    if (file.size > maxPNGBytes) {
      message(`${file.name} exceeds ${maxPNGBytes} bytes.`, true);
      return;
    }
    let buffer;
    try {
      buffer = await file.arrayBuffer();
    } catch {
      if (ticket === revision) message(`Could not read ${file.name}.`, true);
      return;
    }
    // A newer selection or document action wins, regardless of file read order.
    if (ticket !== revision) return;
    const error = core.stageOpen(new Uint8Array(buffer));
    if (error) {
      message(`${file.name}: ${error}`, true);
      return;
    }
    if (replace(`Discard unsaved changes and open ${file.name}?`)) {
      downloadName = `${file.name.replace(/\.png$/i, "")}-edited.png`;
      message(`Opened ${file.name}.`);
    }
  }

  grid.addEventListener("pointerdown", event => {
    if (pointer !== null || event.button !== 0 || width === 0) return;
    const [column, row] = pixelAt(event);
    if (!core.begin(column, row, ink())) return;
    event.preventDefault();
    revision++;
    pointer = event.pointerId;
    grid.focus({ preventScroll: true });
    grid.setPointerCapture(pointer);
    move(column, row);
    render();
  });
  grid.addEventListener("pointermove", event => {
    if (event.pointerId !== pointer) return;
    // Process coalesced samples so a short excursion outside the grid still breaks the stroke.
    for (const sample of event.getCoalescedEvents?.() ?? []) extend(sample);
    extend(event);
    scheduleRender();
  });
  grid.addEventListener("pointerup", event => {
    if (event.pointerId !== pointer) return;
    extend(event);
    finish(true);
  });
  for (const name of ["pointercancel", "lostpointercapture"]) {
    grid.addEventListener(name, event => {
      if (event.pointerId === pointer) finish(false);
    });
  }
  grid.addEventListener("blur", () => finish(false));
  grid.addEventListener("keydown", event => {
    if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
    switch (event.key) {
      case "ArrowLeft": move(Math.max(0, x - 1), y); break;
      case "ArrowRight": move(Math.min(width - 1, x + 1), y); break;
      case "ArrowUp": move(x, Math.max(0, y - 1)); break;
      case "ArrowDown": move(x, Math.min(height - 1, y + 1)); break;
      case "Escape": finish(false); break;
      case "Enter":
      case " ":
        if (pointer === null && !event.repeat && core.begin(x, y, ink())) {
          revision++;
          core.end(true);
          render();
        }
        break;
      default: return;
    }
    event.preventDefault();
    if (event.key.startsWith("Arrow")) cursor.scrollIntoView({ block: "nearest", inline: "nearest" });
  });

  pencil.addEventListener("click", () => selectTool(false));
  eraser.addEventListener("click", () => selectTool(true));
  color.addEventListener("input", () => { selectTool(false); markSwatch(); });
  for (const swatch of swatches) {
    swatch.addEventListener("click", () => {
      color.value = /** @type {string} */ (swatch.dataset.color);
      selectTool(false);
      markSwatch();
    });
  }
  zoomSelect.addEventListener("change", () => {
    layout();
    cursor.scrollIntoView({ block: "nearest", inline: "nearest" });
  });
  undo.addEventListener("click", () => { if (core.undo()) { revision++; render(); } });
  redo.addEventListener("click", () => { if (core.redo()) { revision++; render(); } });
  newButton.addEventListener("click", () => {
    revision++;
    const w = widthInput.valueAsNumber;
    const h = heightInput.valueAsNumber;
    if (!Number.isInteger(w) || w < 1 || w > maxAxis || !Number.isInteger(h) || h < 1 || h > maxAxis) {
      message(`Width and height must be whole numbers from 1 to ${maxAxis}.`, true);
      return;
    }
    const error = core.stageNew(w, h);
    if (error) {
      message(error, true);
      return;
    }
    if (replace(`Discard unsaved changes and create a blank ${w} × ${h} sheet?`)) {
      downloadName = "sprite-sheet.png";
      message(`Created a blank ${w} × ${h} sheet.`);
    }
  });
  openButton.addEventListener("click", () => { revision++; fileInput.click(); });
  fileInput.addEventListener("change", () => {
    const file = fileInput.files?.[0];
    fileInput.value = "";
    if (file) void openPNG(file);
  });
  exportButton.addEventListener("click", () => {
    const bytes = core.exportPNG();
    if (typeof bytes === "string") {
      message(bytes, true);
      return;
    }
    const url = URL.createObjectURL(new Blob([bytes], { type: "image/png" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = downloadName;
    link.click();
    // Let the browser consume the URL before releasing the blob.
    setTimeout(() => URL.revokeObjectURL(url), 0);
    core.markExported();
    render();
    message(`${downloadName} handed off to the browser for download.`);
  });
  window.addEventListener("beforeunload", event => {
    if (!dirty) return;
    event.preventDefault();
    event.returnValue = "";
  });

  /** @param {boolean} erase - Whether the next gesture will erase pixels. */
  function selectTool(erase) {
    erasing = erase;
    pencil.setAttribute("aria-pressed", String(!erase));
    eraser.setAttribute("aria-pressed", String(erase));
  }

  /** Mark the palette swatch matching the current color, if any. */
  function markSwatch() {
    for (const swatch of swatches) swatch.setAttribute("aria-pressed", String(swatch.dataset.color === color.value));
  }

  render();
  sheetControls.disabled = toolControls.disabled = false;
  message("Ready. Create a sheet or open a PNG.");
}

/**
 * Show a message without treating filenames or decoder diagnostics as markup.
 * @param {string} text - Visible diagnostic or status.
 * @param {boolean} [error] - Whether the message reports a failure.
 */
function message(text, error = false) {
  status.textContent = text;
  status.toggleAttribute("data-error", error);
}

/** @param {string} text - Diagnostic describing why editing stopped. */
function stop(text) {
  sheetControls.disabled = toolControls.disabled = true;
  grid.removeAttribute("tabindex");
  message(text, true);
}

start().catch(error => stop(error.message));
