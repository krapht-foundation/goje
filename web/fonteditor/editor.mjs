/** @file Bind browser input, drawing, and the PNG and JSON handoff to the Go Font Editor document. */

/**
 * Find a static element in the Font Editor page.
 * @template {Element} T
 * @param {string} id - Element ID.
 * @returns {T} The corresponding element.
 */
function element(id) {
  return /** @type {T} */ (document.getElementById(id));
}

const fontControls = /** @type {HTMLFieldSetElement} */ (element("font"));
const createControls = /** @type {HTMLFieldSetElement} */ (element("create"));
const toolControls = /** @type {HTMLFieldSetElement} */ (element("tools"));
const builtinButton = /** @type {HTMLButtonElement} */ (element("builtin"));
const openButton = /** @type {HTMLButtonElement} */ (element("open"));
const fileInput = /** @type {HTMLInputElement} */ (element("files"));
const exportPNG = /** @type {HTMLButtonElement} */ (element("export-png"));
const exportJSON = /** @type {HTMLButtonElement} */ (element("export-json"));
const newButton = /** @type {HTMLButtonElement} */ (element("new"));
const numberInputs = /** @type {Record<string, HTMLInputElement>} */ (Object.fromEntries(
  ["cell-width", "cell-height", "baseline", "first", "count"].map(id => [id, element(id)])));
const character = /** @type {HTMLInputElement} */ (element("character"));
const previous = /** @type {HTMLButtonElement} */ (element("previous"));
const next = /** @type {HTMLButtonElement} */ (element("next"));
const glyphOutput = element("glyph");
const undo = /** @type {HTMLButtonElement} */ (element("undo"));
const redo = /** @type {HTMLButtonElement} */ (element("redo"));
const pencil = element("pencil");
const eraser = element("eraser");
const status = element("status");
const coordinate = element("coordinate");
const dirtyOutput = element("dirty");
const grid = /** @type {HTMLElement} */ (element("grid"));
const lines = /** @type {HTMLElement} */ (element("lines"));
const baselineGuide = /** @type {HTMLElement} */ (element("baseline-guide"));
const cursor = /** @type {HTMLElement} */ (element("cursor"));
const art = /** @type {HTMLCanvasElement} */ (element("art"));
const sheetCanvas = /** @type {HTMLCanvasElement} */ (element("sheet"));
const preview = /** @type {HTMLElement} */ (element("preview"));
const artContext = art.getContext("2d");
const sheetContext = sheetCanvas.getContext("2d");

/**
 * Load Wasm and connect the page to its single document.
 * @returns {Promise<void>} Resolves when fonts can be created or opened.
 */
async function start() {
  if (!artContext || !sheetContext) throw new Error("Canvas is unavailable.");
  const go = new Go();
  const response = await fetch("fonteditor.wasm");
  if (!response.ok) throw new Error(`Wasm download failed: HTTP ${response.status}`);
  const { instance } = await WebAssembly.instantiateStreaming(response, go.importObject);
  go.run(instance).then(() => stop("The Font Editor core stopped. Reload the page."), error => stop(error.message));
  const core = globalThis.gojeFontEditor;
  if (!core) throw new Error("The Font Editor core did not start.");
  const { maxCell, maxPNGBytes, maxJSONBytes } = core;
  numberInputs["cell-width"].max = numberInputs["cell-height"].max = numberInputs.baseline.max = String(maxCell);
  /** @type {{cellWidth: number, cellHeight: number, baseline: number, first: number, count: number, columns: number, width: number, height: number}} */
  let font = { cellWidth: 0, cellHeight: 0, baseline: 0, first: 0, count: 0, columns: 1, width: 0, height: 0 };
  /** @type {ImageData | null} */
  let frame = null;
  let glyph = 0;
  let x = 0;
  let y = 0;
  let dirty = false;
  let erasing = false;
  /** @type {number | null} */
  let pointer = null;
  // Bumped by every document action so an older file read cannot replace newer work.
  let revision = 0;
  let pendingFrame = 0;
  let downloadName = "font";

  /** Copy the Go pixels into the sheet and glyph canvases and refresh document controls. */
  function render() {
    if (pendingFrame) cancelAnimationFrame(pendingFrame);
    pendingFrame = 0;
    const state = core.state();
    dirty = state.dirty;
    dirtyOutput.textContent = font.count === 0 ? "No font open" : dirty ? "Unsaved changes" : "Saved";
    undo.disabled = !state.canUndo;
    redo.disabled = !state.canRedo;
    builtinButton.disabled = openButton.disabled = newButton.disabled = pointer !== null;
    exportPNG.disabled = exportJSON.disabled = pointer !== null || font.count === 0;
    if (!frame) return;
    const error = core.pixels(frame.data);
    if (error) {
      message(error, true);
      return;
    }
    sheetContext.putImageData(frame, 0, 0);
    const [left, top] = origin(glyph);
    artContext.clearRect(0, 0, art.width, art.height);
    artContext.putImageData(frame, -left, -top, left, top, font.cellWidth, font.cellHeight);
  }

  /** Coalesce pointer-driven redraws into at most one per display frame. */
  function scheduleRender() {
    pendingFrame ||= requestAnimationFrame(render);
  }

  /**
   * Find a glyph cell on the sheet.
   * @param {number} index - Glyph index.
   * @returns {[number, number]} Sheet pixel of the cell's upper-left corner.
   */
  function origin(index) {
    return [index % font.columns * font.cellWidth, Math.floor(index / font.columns) * font.cellHeight];
  }

  /** Resize the view for the document that finishOpen just made live. */
  function load() {
    font = core.state();
    art.width = font.cellWidth;
    art.height = font.cellHeight;
    sheetCanvas.width = font.width;
    sheetCanvas.height = font.height;
    frame = new ImageData(font.width, font.height);
    // Show the sheet as large as fits a comfortable width, in whole pixels.
    const sheetZoom = Math.max(1, Math.floor(480 / Math.max(font.width, font.height)));
    sheetCanvas.style.width = `${font.width * sheetZoom}px`;
    sheetCanvas.style.height = `${font.height * sheetZoom}px`;
    const zoom = Math.max(1, Math.floor(384 / Math.max(font.cellWidth, font.cellHeight)));
    grid.style.setProperty("--z", String(zoom));
    grid.style.width = `${font.cellWidth * zoom}px`;
    grid.style.height = `${font.cellHeight * zoom}px`;
    baselineGuide.style.top = `${font.baseline * zoom}px`;
    lines.hidden = zoom < 4;
    grid.hidden = preview.hidden = false;
    grid.tabIndex = 0;
    select(0);
    move(0, 0);
  }

  /**
   * Show a glyph in the grid and name it in the glyph controls.
   * @param {number} index - Glyph index within the font.
   */
  function select(index) {
    glyph = index;
    const codePoint = font.first + index;
    const hex = codePoint.toString(16).toUpperCase().padStart(4, "0");
    // Control characters and lone surrogates have no visible form, so show only their code point.
    const printable = !/[\p{Cc}\p{Cs}]/u.test(String.fromCodePoint(codePoint));
    glyphOutput.textContent = printable ? `U+${hex} “${String.fromCodePoint(codePoint)}”` : `U+${hex}`;
    if (document.activeElement !== character) character.value = printable ? String.fromCodePoint(codePoint) : "";
    previous.disabled = index === 0;
    next.disabled = index === font.count - 1;
    render();
  }

  /**
   * Set the active pixel and the visible coordinate.
   * @param {number} column - In-cell pixel column.
   * @param {number} row - In-cell pixel row.
   */
  function move(column, row) {
    x = column;
    y = row;
    const zoom = Number(grid.style.getPropertyValue("--z"));
    cursor.style.transform = `translate(${x * zoom}px, ${y * zoom}px)`;
    coordinate.textContent = `(${x}, ${y})`;
  }

  /**
   * Project a pointer onto the glyph cell, including coordinates beyond its edges.
   * @param {PointerEvent} event - Pointer sample, which may be captured outside the grid.
   * @returns {[number, number]} Pixel column and row.
   */
  function pixelAt(event) {
    const rect = grid.getBoundingClientRect();
    return [
      Math.floor((event.clientX - rect.left) * font.cellWidth / rect.width),
      Math.floor((event.clientY - rect.top) * font.cellHeight / rect.height),
    ];
  }

  /**
   * Apply a pointer sample; Go breaks interpolation when it falls outside the cell.
   * @param {PointerEvent} event - Active pointer sample.
   */
  function extend(event) {
    const [column, row] = pixelAt(event);
    core.extend(column, row);
    if (column >= 0 && column < font.cellWidth && row >= 0 && row < font.cellHeight) move(column, row);
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
   * @param {string} prompt - Confirmation shown when the current font has unsaved changes.
   * @returns {boolean} Whether the candidate became the live document.
   */
  function replace(prompt) {
    const accepted = !dirty || window.confirm(prompt);
    core.finishOpen(accepted);
    if (accepted) load();
    else message("Kept the current font.");
    return accepted;
  }

  /**
   * Validate a chosen sheet and description before asking to discard edits.
   * @param {File[]} files - Files explicitly selected by the user.
   * @returns {Promise<void>} Resolves after the candidate is handled or superseded.
   */
  async function openFont(files) {
    const ticket = ++revision;
    const sheet = files.filter(file => /\.png$/i.test(file.name));
    const description = files.filter(file => /\.json$/i.test(file.name));
    if (files.length !== 2 || sheet.length !== 1 || description.length !== 1) {
      message("Choose one PNG sheet and one JSON description together.", true);
      return;
    }
    const [png, json] = [sheet[0], description[0]];
    for (const [file, limit] of /** @type {[File, number][]} */ ([[png, maxPNGBytes], [json, maxJSONBytes]])) {
      if (file.size > limit) {
        message(`${file.name} exceeds ${limit} bytes.`, true);
        return;
      }
    }
    let buffers;
    try {
      buffers = await Promise.all([png.arrayBuffer(), json.arrayBuffer()]);
    } catch {
      if (ticket === revision) message(`Could not read ${png.name} and ${json.name}.`, true);
      return;
    }
    // A newer selection or document action wins, regardless of file read order.
    if (ticket !== revision) return;
    const error = core.stageOpen(new Uint8Array(buffers[0]), new Uint8Array(buffers[1]));
    if (error) {
      message(`${png.name} and ${json.name}: ${error}`, true);
      return;
    }
    if (replace(`Discard unsaved changes and open ${png.name}?`)) {
      downloadName = `${png.name.replace(/\.png$/i, "")}-edited`;
      message(`Opened ${png.name} and ${json.name}.`);
    }
  }

  /**
   * Hand bytes to the browser as a download.
   * @param {BlobPart} bytes - File contents.
   * @param {string} name - Suggested file name.
   * @param {string} type - MIME type.
   */
  function save(bytes, name, type) {
    const url = URL.createObjectURL(new Blob([bytes], { type }));
    const link = document.createElement("a");
    link.href = url;
    link.download = name;
    link.click();
    // Let the browser consume the URL before releasing the blob.
    setTimeout(() => URL.revokeObjectURL(url), 0);
    message(`${name} handed off to the browser for download.`);
  }

  grid.addEventListener("pointerdown", event => {
    if (pointer !== null || event.button !== 0 || font.count === 0) return;
    const [column, row] = pixelAt(event);
    if (!core.begin(glyph, column, row, erasing)) return;
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
    // Process coalesced samples so a short excursion outside the cell still breaks the stroke.
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
      case "ArrowRight": move(Math.min(font.cellWidth - 1, x + 1), y); break;
      case "ArrowUp": move(x, Math.max(0, y - 1)); break;
      case "ArrowDown": move(x, Math.min(font.cellHeight - 1, y + 1)); break;
      case "Escape": finish(false); break;
      case "Enter":
      case " ":
        if (pointer === null && !event.repeat && core.begin(glyph, x, y, erasing)) {
          revision++;
          core.end(true);
          render();
        }
        break;
      default: return;
    }
    event.preventDefault();
  });

  character.addEventListener("input", () => {
    const codePoint = character.value.codePointAt(0);
    if (codePoint === undefined) return;
    const index = codePoint - font.first;
    if (index < 0 || index >= font.count) {
      message(`U+${codePoint.toString(16).toUpperCase().padStart(4, "0")} is not in this font.`, true);
      return;
    }
    select(index);
  });
  // Show the edited glyph's character again once the field loses focus.
  character.addEventListener("change", () => { if (font.count) select(glyph); });
  previous.addEventListener("click", () => select(Math.max(0, glyph - 1)));
  next.addEventListener("click", () => select(Math.min(font.count - 1, glyph + 1)));
  sheetCanvas.addEventListener("click", event => {
    const rect = sheetCanvas.getBoundingClientRect();
    const column = Math.floor((event.clientX - rect.left) * font.width / rect.width / font.cellWidth);
    const row = Math.floor((event.clientY - rect.top) * font.height / rect.height / font.cellHeight);
    const index = row * font.columns + column;
    if (column < font.columns && index < font.count) select(index);
  });
  pencil.addEventListener("click", () => selectTool(false));
  eraser.addEventListener("click", () => selectTool(true));
  undo.addEventListener("click", () => { if (core.undo()) { revision++; render(); } });
  redo.addEventListener("click", () => { if (core.redo()) { revision++; render(); } });
  builtinButton.addEventListener("click", () => {
    revision++;
    const error = core.stageBuiltin();
    if (error) {
      message(error, true);
      return;
    }
    if (replace("Discard unsaved changes and open the built-in font?")) {
      downloadName = "goje-font";
      message("Opened the built-in font.");
    }
  });
  newButton.addEventListener("click", () => {
    revision++;
    const [width, height, baseline, first, count] = Object.values(numberInputs).map(input => input.valueAsNumber);
    if (![width, height, baseline, first, count].every(Number.isInteger)) {
      message("Every new font field must be a whole number.", true);
      return;
    }
    const error = core.stageNew(width, height, baseline, first, count);
    if (error) {
      message(error, true);
      return;
    }
    if (replace(`Discard unsaved changes and create a blank ${width} × ${height} font?`)) {
      downloadName = "font";
      message(`Created a blank font of ${count} glyphs in ${width} × ${height} cells.`);
    }
  });
  openButton.addEventListener("click", () => { revision++; fileInput.click(); });
  fileInput.addEventListener("change", () => {
    const files = [...fileInput.files ?? []];
    fileInput.value = "";
    if (files.length) void openFont(files);
  });
  exportPNG.addEventListener("click", () => {
    const bytes = core.exportPNG();
    if (typeof bytes === "string") {
      message(bytes, true);
      return;
    }
    save(bytes, `${downloadName}.png`, "image/png");
    core.markExported();
    render();
  });
  exportJSON.addEventListener("click", () => save(core.exportJSON(), `${downloadName}.json`, "application/json"));
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

  render();
  fontControls.disabled = createControls.disabled = toolControls.disabled = false;
  message("Ready. Open the built-in font, open a PNG and JSON, or create a new font.");
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
  fontControls.disabled = createControls.disabled = toolControls.disabled = true;
  grid.removeAttribute("tabindex");
  message(text, true);
}

start().catch(error => stop(error.message));
