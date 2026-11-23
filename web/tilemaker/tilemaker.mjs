/** @file Bind browser input and PNG handoff to the Go Tile Maker document. */

/**
 * Find a static element in the Tile Maker page.
 * @template {Element} T
 * @param {string} id - Element ID.
 * @returns {T} The corresponding element.
 */
function element(id) {
  return /** @type {T} */ (document.getElementById(id));
}

const grid = /** @type {HTMLElement} */ (element("grid"));
const fileInput = /** @type {HTMLInputElement} */ (element("file"));
const color = /** @type {HTMLInputElement} */ (element("color"));
const guideToggle = /** @type {HTMLInputElement} */ (element("guide-toggle"));
const undo = /** @type {HTMLButtonElement} */ (element("undo"));
const redo = /** @type {HTMLButtonElement} */ (element("redo"));
const exportButton = /** @type {HTMLButtonElement} */ (element("export"));
const newButton = /** @type {HTMLButtonElement} */ (element("new"));
const openButton = /** @type {HTMLButtonElement} */ (element("open"));
const pencil = element("pencil");
const eraser = element("eraser");
const status = element("status");
const controls = /** @type {HTMLFieldSetElement} */ (element("controls"));
const cursor = /** @type {HTMLElement} */ (element("cursor"));
const tile = /** @type {HTMLCanvasElement} */ (element("tile"));
const editContext = tile.getContext("2d");
const nativeContext = /** @type {HTMLCanvasElement} */ (element("native")).getContext("2d");
const repeatContext = /** @type {HTMLCanvasElement} */ (element("repeat")).getContext("2d");

/**
 * Load Wasm and connect the page to its single document.
 * @returns {Promise<void>} Resolves when editing is enabled.
 */
async function start() {
  if (!editContext || !nativeContext || !repeatContext) throw new Error("Canvas is unavailable.");
  const go = new Go();
  const response = await fetch("tilemaker.wasm");
  if (!response.ok) throw new Error(`Wasm download failed: HTTP ${response.status}`);
  const { instance } = await WebAssembly.instantiateStreaming(response, go.importObject);
  go.run(instance).then(() => stop("The Tile Maker core stopped. Reload the page."), error => stop(error.message));
  const core = globalThis.gojeTileMaker;
  if (!core) throw new Error("The Tile Maker core did not start.");
  const { width, height, maxBytes } = core;
  const frame = editContext.createImageData(width, height);
  const halfWidth = width / 2;
  const halfHeight = height / 2;
  let x = 0;
  let y = 0;
  let dirty = false;
  let erasing = false;
  /** @type {number | null} */
  let pointer = null;
  let revision = 0;

  /** Draw the current pixels; grid lines, cursor, and guide are separate DOM overlays. */
  function render() {
    const state = core.snapshot(frame.data);
    dirty = state.dirty;
    element("dirty").textContent = dirty ? "Unsaved changes" : "Saved";
    undo.disabled = !state.canUndo;
    redo.disabled = !state.canRedo;
    newButton.disabled = openButton.disabled = exportButton.disabled = pointer !== null;
    editContext.putImageData(frame, 0, 0);
    nativeContext.putImageData(frame, 0, 0);
    repeatContext.clearRect(0, 0, 2 * width, 2 * height);
    // Match the courtyard axes: +x goes down-right, +y down-left. Draw back to front.
    repeatContext.drawImage(tile, halfWidth, 0);
    repeatContext.drawImage(tile, width, halfHeight);
    repeatContext.drawImage(tile, 0, halfHeight);
    repeatContext.drawImage(tile, halfWidth, height);
  }

  /**
   * Set the keyboard cursor and the visible active coordinate.
   * @param {number} column - In-bounds pixel column.
   * @param {number} row - In-bounds pixel row.
   */
  function move(column, row) {
    x = column;
    y = row;
    cursor.style.transform = `translate(${100 * x}%, ${100 * y}%)`;
    element("coordinate").textContent = `(${x}, ${y})`;
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
   * Apply a pointer sample, breaking interpolation when it falls outside the grid.
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
  }

  /**
   * Validate a chosen file before asking to discard edits.
   * @param {File} file - File explicitly selected by the user.
   * @returns {Promise<void>} Resolves after the candidate is handled or superseded.
   */
  async function openPNG(file) {
    const ticket = ++revision;
    if (file.size > maxBytes) {
      message(`PNG exceeds ${maxBytes} bytes.`, true);
      return;
    }
    let buffer;
    try {
      buffer = await file.arrayBuffer();
    } catch {
      if (ticket === revision) message("Could not read the selected PNG.", true);
      return;
    }
    // A newer selection or document action wins, regardless of file read order.
    if (ticket !== revision) return;
    const error = core.stageOpen(new Uint8Array(buffer));
    if (error) {
      message(error, true);
      return;
    }
    const replace = !dirty || window.confirm("Discard unsaved changes and open this PNG?");
    core.finishOpen(replace);
    if (replace) {
      move(0, 0);
      render();
      message(`Opened ${file.name}.`);
    }
  }

  grid.addEventListener("pointerdown", event => {
    if (pointer !== null || event.button !== 0) return;
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
    render();
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
  });

  pencil.addEventListener("click", () => selectTool(false));
  eraser.addEventListener("click", () => selectTool(true));
  guideToggle.addEventListener("change", () => element("guide").toggleAttribute("hidden", !guideToggle.checked));
  undo.addEventListener("click", () => { if (core.undo()) { revision++; render(); } });
  redo.addEventListener("click", () => { if (core.redo()) { revision++; render(); } });
  newButton.addEventListener("click", () => {
    revision++;
    if (dirty && !window.confirm("Discard unsaved changes and create a blank tile?")) return;
    core.newTile();
    move(0, 0);
    render();
    message("Created a blank tile.");
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
    link.download = "tile.png";
    link.click();
    // Let the browser consume the URL before releasing the blob.
    setTimeout(() => URL.revokeObjectURL(url), 0);
    core.markExported();
    render();
    message("PNG handed off to the browser for download.");
  });

  /** @param {boolean} erase - Whether the next gesture will erase pixels. */
  function selectTool(erase) {
    erasing = erase;
    pencil.setAttribute("aria-pressed", String(!erase));
    eraser.setAttribute("aria-pressed", String(erase));
  }

  render();
  controls.disabled = false;
  grid.tabIndex = 0;
  message("Ready. Draw a tile or open a PNG.");
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
  controls.disabled = true;
  grid.removeAttribute("tabindex");
  message(text, true);
}

start().catch(error => stop(error.message));
