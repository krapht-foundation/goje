/** @file Browser UI for the Goje level editor. Go owns the level; this module owns display and input. */

/**
 * @typedef {object} Placement
 * @property {number} id - Stable placement ID.
 * @property {string} kind - Game-defined kind, treated as plain text.
 * @property {number} x - Zero-based column.
 * @property {number} y - Zero-based row.
 */

/**
 * @typedef {object} EditorState
 * @property {number} width - Board columns.
 * @property {number} height - Board rows.
 * @property {string} terrain - Row-major cells, "." for floor and "#" for wall.
 * @property {Placement[]} placements - Placements in file order.
 * @property {boolean} dirty - Whether edits have not been exported.
 * @property {boolean} canUndo - Whether undo has an action.
 * @property {boolean} canRedo - Whether redo has an action.
 */

/**
 * @typedef {object} Bridge
 * @property {number} maxBytes - Largest accepted level file.
 * @property {(bytes: Uint8Array) => string} open - Replace the document; returns a diagnostic or "".
 * @property {(tile: string) => string} begin - Start a terrain stroke.
 * @property {(x: number, y: number) => string} paint - Paint one stroke cell.
 * @property {(commit: boolean) => void} end - Commit or roll back the stroke.
 * @property {(fromX: number, fromY: number, toX: number, toY: number) => string} move - Move a placement.
 * @property {() => string} undo - Undo one action.
 * @property {() => string} redo - Redo one action.
 * @property {() => EditorState} state - Snapshot the document.
 * @property {() => Uint8Array | string} exportLevel - Encode the document or return a diagnostic.
 * @property {() => string} markExported - Mark the exported revision clean.
 */

const CELL = 24;
const WALL = 35; // "#"
const TILE_NAMES = { floor: ".", wall: "#" };

const openInput = /** @type {HTMLInputElement} */ (document.querySelector("#open"));
const board = /** @type {HTMLCanvasElement} */ (document.querySelector("#board"));
const scroller = /** @type {HTMLElement} */ (document.querySelector("#scroller"));
const statusLine = /** @type {HTMLElement} */ (document.querySelector("#status"));
const cursorLine = /** @type {HTMLElement} */ (document.querySelector("#cursor"));
const toolButtons = {
  floor: /** @type {HTMLButtonElement} */ (document.querySelector("#floor")),
  wall: /** @type {HTMLButtonElement} */ (document.querySelector("#wall")),
  placement: /** @type {HTMLButtonElement} */ (document.querySelector("#placement")),
};
const undoButton = /** @type {HTMLButtonElement} */ (document.querySelector("#undo"));
const redoButton = /** @type {HTMLButtonElement} */ (document.querySelector("#redo"));
const exportButton = /** @type {HTMLButtonElement} */ (document.querySelector("#export"));
const ctx = /** @type {CanvasRenderingContext2D} */ (board.getContext("2d"));

/** @type {Bridge} */
let bridge;
/**
 * Mirror of the Go document used for drawing; refreshed after each change.
 * @type {{width: number, height: number, terrain: Uint8Array, byIndex: Map<number, Placement>, byId: Map<number, Placement>, dirty: boolean, canUndo: boolean, canRedo: boolean} | null}
 */
let doc = null;
/** @type {"floor" | "wall" | "placement"} */
let tool = "floor";
const cursor = { x: 0, y: 0 };
/** @type {number | null} ID of the placement chosen for a move. */
let selectedId = null;
/**
 * Active terrain stroke. pointerId is null for keyboard strokes.
 * @type {{pointerId: number | null, tile: number, last: {x: number, y: number} | null, changed: number, rejected: number, error: string} | null}
 */
let stroke = null;
/** @type {{pointerId: number, x: number, y: number} | null} Pointer that selected a placement and may drag it. */
let drag = null;
let openSeq = 0;
let baseName = "level";
let scale = 1;

/**
 * Show a status message as text, marking errors visibly with a prefix and style.
 * @param {string} text - Message to show.
 * @param {boolean} [error] - Whether the message reports a failure.
 */
function setStatus(text, error = false) {
  statusLine.textContent = error ? `Error: ${text}` : text;
  statusLine.classList.toggle("error", error);
}

/**
 * Re-read Go state and redraw only cells whose content changed, or the whole board after open or resize.
 * @param {boolean} [replaced] - Whether a new document was opened.
 */
function refresh(replaced = false) {
  const s = bridge.state();
  const full = replaced || !doc || doc.width !== s.width || doc.height !== s.height;
  /** @type {Set<number>} */
  const changed = new Set();
  const terrain = full ? new Uint8Array(s.width * s.height) : doc.terrain;
  for (let i = 0; i < terrain.length; i++) {
    const code = s.terrain.charCodeAt(i);
    if (terrain[i] !== code) {
      terrain[i] = code;
      changed.add(i);
    }
  }
  const byIndex = new Map();
  const byId = new Map();
  for (const p of s.placements) {
    const i = p.y * s.width + p.x;
    byIndex.set(i, p);
    byId.set(p.id, p);
    const old = full ? undefined : doc.byId.get(p.id);
    if (old && (old.x !== p.x || old.y !== p.y)) {
      changed.add(old.y * s.width + old.x);
      changed.add(i);
    }
  }
  doc = { width: s.width, height: s.height, terrain, byIndex, byId, dirty: s.dirty, canUndo: s.canUndo, canRedo: s.canRedo };
  if (selectedId !== null && !byId.has(selectedId)) selectedId = null;
  if (full) {
    resizeBoard();
    drawAll();
  } else {
    for (const i of changed) drawCell(i % doc.width, Math.floor(i / doc.width));
  }
  updateControls();
  describeCursor();
}

/** Size the canvas to the board at a fixed CSS cell size without stretching. */
function resizeBoard() {
  scale = window.devicePixelRatio || 1;
  const w = doc.width * CELL;
  const h = doc.height * CELL;
  board.style.width = `${w}px`;
  board.style.height = `${h}px`;
  board.width = Math.round(w * scale);
  board.height = Math.round(h * scale);
  board.hidden = false;
}

/** Draw every board cell. */
function drawAll() {
  ctx.setTransform(scale, 0, 0, scale, 0, 0);
  for (let y = 0; y < doc.height; y++) {
    for (let x = 0; x < doc.width; x++) drawCell(x, y);
  }
}

/**
 * Draw one cell: terrain, placement, move selection, and keyboard cursor.
 * @param {number} x - Column.
 * @param {number} y - Row.
 */
function drawCell(x, y) {
  const px = x * CELL;
  const py = y * CELL;
  const i = y * doc.width + x;
  ctx.setTransform(scale, 0, 0, scale, 0, 0);
  if (doc.terrain[i] === WALL) {
    ctx.fillStyle = "#4b5568";
    ctx.fillRect(px, py, CELL, CELL);
    ctx.strokeStyle = "#2a3140";
    ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.moveTo(px, py + CELL);
    ctx.lineTo(px + CELL, py);
    ctx.moveTo(px, py + CELL / 2);
    ctx.lineTo(px + CELL / 2, py);
    ctx.moveTo(px + CELL / 2, py + CELL);
    ctx.lineTo(px + CELL, py + CELL / 2);
    ctx.stroke();
  } else {
    ctx.fillStyle = "#d8cfb4";
    ctx.fillRect(px, py, CELL, CELL);
  }
  ctx.strokeStyle = "#8c8572";
  ctx.lineWidth = 1;
  ctx.strokeRect(px + 0.5, py + 0.5, CELL - 1, CELL - 1);

  const p = doc.byIndex.get(i);
  if (p) {
    ctx.fillStyle = "#2563eb";
    ctx.beginPath();
    ctx.arc(px + CELL / 2, py + CELL / 2, CELL / 2 - 3, 0, Math.PI * 2);
    ctx.fill();
    ctx.fillStyle = "#fff";
    ctx.font = "bold 12px system-ui, sans-serif";
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    ctx.fillText((p.kind[0] || "?").toUpperCase(), px + CELL / 2, py + CELL / 2 + 1);
    if (p.id === selectedId) {
      ctx.strokeStyle = "#e11d48";
      ctx.lineWidth = 3;
      ctx.setLineDash([4, 2]);
      ctx.strokeRect(px + 1.5, py + 1.5, CELL - 3, CELL - 3);
      ctx.setLineDash([]);
    }
  }
  if (x === cursor.x && y === cursor.y) {
    ctx.lineWidth = 2;
    ctx.strokeStyle = "#000";
    ctx.strokeRect(px + 4, py + 4, CELL - 8, CELL - 8);
    ctx.strokeStyle = "#ffd166";
    ctx.strokeRect(px + 2, py + 2, CELL - 4, CELL - 4);
  }
}

/** Enable controls for the current document and reflect the selected tool. */
function updateControls() {
  const open = doc !== null;
  for (const [name, button] of Object.entries(toolButtons)) {
    button.disabled = !open;
    button.setAttribute("aria-pressed", String(name === tool));
  }
  undoButton.disabled = !open || !doc.canUndo;
  redoButton.disabled = !open || !doc.canRedo;
  exportButton.disabled = !open;
  document.title = open && doc.dirty ? "• Goje Level Editor" : "Goje Level Editor";
}

/** Describe the cursor cell, its contents, and any pending move in text. */
function describeCursor() {
  if (!doc) return;
  const i = cursor.y * doc.width + cursor.x;
  let text = `Cell ${cursor.x}, ${cursor.y}: ${doc.terrain[i] === WALL ? "wall" : "floor"}`;
  const p = doc.byIndex.get(i);
  if (p) text += `, ${p.kind} #${p.id}`;
  const chosen = selectedId === null ? undefined : doc.byId.get(selectedId);
  if (chosen) text += `. Moving ${chosen.kind} #${chosen.id} from ${chosen.x}, ${chosen.y}`;
  cursorLine.textContent = `${text}.`;
}

/**
 * Move the keyboard cursor, redrawing only the old and new cells and keeping it visible.
 * @param {number} x - Target column, clamped to the board.
 * @param {number} y - Target row, clamped to the board.
 */
function moveCursor(x, y) {
  const nx = Math.min(Math.max(x, 0), doc.width - 1);
  const ny = Math.min(Math.max(y, 0), doc.height - 1);
  const ox = cursor.x;
  const oy = cursor.y;
  cursor.x = nx;
  cursor.y = ny;
  if (ox !== nx || oy !== ny) {
    if (ox < doc.width && oy < doc.height) drawCell(ox, oy);
    drawCell(nx, ny);
  }
  scrollCellIntoView(nx, ny);
  describeCursor();
}

/**
 * Scroll the viewport the minimum amount needed to show a cell.
 * @param {number} x - Column.
 * @param {number} y - Row.
 */
function scrollCellIntoView(x, y) {
  const left = board.offsetLeft + x * CELL;
  const top = board.offsetTop + y * CELL;
  if (left < scroller.scrollLeft) scroller.scrollLeft = left;
  else if (left + CELL > scroller.scrollLeft + scroller.clientWidth) scroller.scrollLeft = left + CELL - scroller.clientWidth;
  if (top < scroller.scrollTop) scroller.scrollTop = top;
  else if (top + CELL > scroller.scrollTop + scroller.clientHeight) scroller.scrollTop = top + CELL - scroller.clientHeight;
}

/**
 * Convert a pointer position to an unclamped cell.
 * @param {PointerEvent} event - Pointer event over or captured by the board.
 * @returns {{x: number, y: number}} Cell coordinates, possibly outside the board.
 */
function cellAt(event) {
  const rect = board.getBoundingClientRect();
  return { x: Math.floor((event.clientX - rect.left) / CELL), y: Math.floor((event.clientY - rect.top) / CELL) };
}

/**
 * Report whether a cell lies on the board.
 * @param {number} x - Column.
 * @param {number} y - Row.
 * @returns {boolean} Whether the cell is in bounds.
 */
function inBounds(x, y) {
  return x >= 0 && y >= 0 && x < doc.width && y < doc.height;
}

/**
 * Start a terrain stroke with the current brush.
 * @param {number | null} pointerId - Pointer that owns the stroke, or null for keyboard.
 * @returns {boolean} Whether the stroke started.
 */
function beginStroke(pointerId) {
  const tile = TILE_NAMES[tool];
  const err = bridge.begin(tile);
  if (err) {
    setStatus(err, true);
    return false;
  }
  stroke = { pointerId, tile: tile.charCodeAt(0), last: null, changed: 0, rejected: 0, error: "" };
  return true;
}

/**
 * Paint one cell of the active stroke, updating only that cell's view.
 * @param {number} x - Column.
 * @param {number} y - Row.
 */
function paintCell(x, y) {
  stroke.last = { x, y };
  if (!inBounds(x, y)) return;
  const err = bridge.paint(x, y);
  if (err) {
    stroke.rejected++;
    stroke.error = err;
    return;
  }
  const i = y * doc.width + x;
  if (doc.terrain[i] !== stroke.tile) {
    doc.terrain[i] = stroke.tile;
    stroke.changed++;
    drawCell(x, y);
  }
}

/**
 * Paint every cell on the line from the stroke's last cell to a target, so fast drags skip nothing.
 * @param {number} x1 - Target column.
 * @param {number} y1 - Target row.
 */
function paintTo(x1, y1) {
  // Captured pointers can report coordinates far outside the canvas. Keep
  // interpolation bounded to the board and start a fresh segment on reentry.
  if (!inBounds(x1, y1)) {
    stroke.last = null;
    return;
  }
  if (!stroke.last) {
    paintCell(x1, y1);
    return;
  }
  let { x, y } = stroke.last;
  if (x === x1 && y === y1) return;
  const dx = Math.abs(x1 - x);
  const dy = -Math.abs(y1 - y);
  const sx = x < x1 ? 1 : -1;
  const sy = y < y1 ? 1 : -1;
  let e = dx + dy;
  while (x !== x1 || y !== y1) {
    const e2 = 2 * e;
    if (e2 >= dy) { e += dy; x += sx; }
    if (e2 <= dx) { e += dx; y += sy; }
    paintCell(x, y);
  }
}

/**
 * Commit or roll back the active stroke and report its outcome.
 * @param {boolean} commit - Whether to keep the stroke's changes.
 */
function finishStroke(commit) {
  const s = stroke;
  stroke = null;
  if (s.pointerId !== null && board.hasPointerCapture(s.pointerId)) board.releasePointerCapture(s.pointerId);
  bridge.end(commit);
  refresh();
  const name = s.tile === WALL ? "wall" : "floor";
  if (!commit) setStatus("Stroke canceled.");
  else if (s.rejected) setStatus(`Painted ${s.changed} ${name} cell(s); skipped ${s.rejected}: ${s.error}`, true);
  else setStatus(s.changed ? `Painted ${s.changed} ${name} cell(s).` : "No cells changed.");
}

/**
 * Handle a placement-tool choice: select a placement, cancel the selection, or move to the target.
 * @param {number} x - Column.
 * @param {number} y - Row.
 * @returns {boolean} Whether this choice selected a placement.
 */
function choosePlacementCell(x, y) {
  const p = doc.byIndex.get(y * doc.width + x);
  if (selectedId === null) {
    if (!p) {
      setStatus(`No placement at ${x}, ${y} to move.`, true);
      return false;
    }
    setSelection(p.id);
    setStatus(`Selected ${p.kind} #${p.id}. Choose a floor cell to move it to.`);
    return true;
  }
  const chosen = doc.byId.get(selectedId);
  if (chosen.x === x && chosen.y === y) {
    setSelection(null);
    setStatus("Move canceled.");
    return false;
  }
  movePlacement(chosen, x, y);
  return false;
}

/**
 * Move the selected placement and clear the selection on success.
 * @param {Placement} p - Placement to move.
 * @param {number} x - Target column.
 * @param {number} y - Target row.
 */
function movePlacement(p, x, y) {
  const err = bridge.move(p.x, p.y, x, y);
  if (err) {
    setStatus(err, true);
    return;
  }
  setSelection(null);
  refresh();
  setStatus(`Moved ${p.kind} #${p.id} to ${x}, ${y}.`);
}

/**
 * Change the placement chosen for a move and redraw the affected cells.
 * @param {number | null} id - Placement ID, or null to clear.
 */
function setSelection(id) {
  const old = selectedId === null ? undefined : doc.byId.get(selectedId);
  selectedId = id;
  if (old) drawCell(old.x, old.y);
  const next = id === null ? undefined : doc.byId.get(id);
  if (next) drawCell(next.x, next.y);
  describeCursor();
}

/** Cancel any in-progress gesture: a stroke rolls back, and a placement selection clears. */
function cancelGesture() {
  const pointerId = drag?.pointerId;
  drag = null;
  if (pointerId !== undefined && board.hasPointerCapture(pointerId)) board.releasePointerCapture(pointerId);
  if (stroke) finishStroke(false);
  else if (selectedId !== null) {
    setSelection(null);
    setStatus("Move canceled.");
  }
}

/**
 * Apply the current tool at the keyboard cursor as one action.
 */
function applyAtCursor() {
  if (tool === "placement") {
    choosePlacementCell(cursor.x, cursor.y);
    return;
  }
  if (!beginStroke(null)) return;
  paintCell(cursor.x, cursor.y);
  finishStroke(true);
}

/**
 * Run undo or redo and show the result.
 * @param {"undo" | "redo"} action - History direction.
 */
function history(action) {
  if (!doc || stroke) return;
  drag = null;
  const err = bridge[action]();
  if (err) {
    setStatus(err, true);
    return;
  }
  refresh();
  setStatus(action === "undo" ? "Undid last action." : "Redid action.");
}

/**
 * Select an editing tool, clearing any pending placement move.
 * @param {"floor" | "wall" | "placement"} name - Tool to select.
 */
function selectTool(name) {
  if (stroke) finishStroke(false);
  drag = null;
  tool = name;
  if (selectedId !== null) setSelection(null);
  updateControls();
  setStatus(name === "placement" ? "Move placement: choose a placement, then a floor cell." : `${name === "wall" ? "Wall" : "Floor"} brush selected.`);
}

/**
 * Open a chosen file after a size check; stale reads from superseded choices are ignored.
 * @param {File} file - File chosen in the picker.
 * @returns {Promise<void>} Resolves once the file is opened, rejected, or superseded.
 */
async function openFile(file) {
  const seq = ++openSeq;
  const name = file.name;
  if (file.size > bridge.maxBytes) {
    setStatus(`${name} is ${file.size} bytes; the limit is ${bridge.maxBytes} bytes.`, true);
    return;
  }
  setStatus(`Reading ${name}…`);
  let bytes;
  try {
    bytes = new Uint8Array(await file.arrayBuffer());
  } catch (error) {
    if (seq === openSeq) setStatus(`Could not read ${name}: ${error.message}`, true);
    return;
  }
  if (seq !== openSeq) return;
  if (doc && bridge.state().dirty && !window.confirm(`Replace the current level with ${name}? Unexported edits will be lost.`)) {
    setStatus(`Kept the current level; ${name} was not opened.`);
    return;
  }
  const err = bridge.open(bytes);
  if (err) {
    setStatus(`${name}: ${err}`, true);
    return;
  }
  if (stroke?.pointerId != null && board.hasPointerCapture(stroke.pointerId)) board.releasePointerCapture(stroke.pointerId);
  stroke = null;
  drag = null;
  selectedId = null;
  cursor.x = 0;
  cursor.y = 0;
  baseName = name.replace(/\.json$/i, "") || "level";
  refresh(true);
  scroller.scrollTo(0, 0);
  board.focus({ preventScroll: true });
  setStatus(`Opened ${name} (${doc.width} × ${doc.height}).`);
}

/** Encode the level and hand it to the browser as a download, marking it clean only after handoff. */
function exportLevel() {
  if (!doc) return;
  if (stroke) finishStroke(false);
  const data = bridge.exportLevel();
  if (typeof data === "string") {
    setStatus(`Export failed: ${data}`, true);
    return;
  }
  const url = URL.createObjectURL(new Blob([data], { type: "application/json" }));
  const fileName = `${baseName}-edited.json`;
  try {
    const link = document.createElement("a");
    link.href = url;
    link.download = fileName;
    link.click();
  } catch (error) {
    URL.revokeObjectURL(url);
    setStatus(`Export failed: ${error.message}`, true);
    return;
  }
  // Revoke after the click's navigation task has taken the URL.
  setTimeout(() => URL.revokeObjectURL(url), 0);
  const err = bridge.markExported();
  refresh();
  if (err) setStatus(err, true);
  else setStatus(`Exported ${fileName} (${data.length} bytes).`);
}

/**
 * Handle board keys: arrows move the cursor, Space/Enter apply, Escape cancels, Ctrl/⌘+Z undo and redo.
 * @param {KeyboardEvent} event - Key event on the board.
 */
function onKeyDown(event) {
  if (!doc) return;
  const steps = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] };
  const step = steps[event.key];
  if (step && !event.altKey && !event.ctrlKey && !event.metaKey) {
    event.preventDefault();
    moveCursor(cursor.x + step[0], cursor.y + step[1]);
  } else if (event.key === " " || event.key === "Enter") {
    event.preventDefault();
    if (!stroke) applyAtCursor();
  } else if (event.key === "Escape") {
    event.preventDefault();
    cancelGesture();
  } else if ((event.ctrlKey || event.metaKey) && !event.altKey && event.key.toLowerCase() === "z") {
    event.preventDefault();
    history(event.shiftKey ? "redo" : "undo");
  } else if (event.ctrlKey && !event.metaKey && !event.altKey && event.key.toLowerCase() === "y") {
    event.preventDefault();
    history("redo");
  }
}

/**
 * Start a brush stroke or placement choice with the primary pointer button.
 * @param {PointerEvent} event - Pointer down on the board.
 */
function onPointerDown(event) {
  if (!doc || event.button !== 0 || stroke || drag) return;
  event.preventDefault();
  board.focus({ preventScroll: true });
  const { x, y } = cellAt(event);
  if (!inBounds(x, y)) return;
  moveCursor(x, y);
  if (tool === "placement") {
    if (choosePlacementCell(x, y)) {
      drag = { pointerId: event.pointerId, x, y };
      board.setPointerCapture(event.pointerId);
    }
    return;
  }
  if (!beginStroke(event.pointerId)) return;
  board.setPointerCapture(event.pointerId);
  paintCell(x, y);
}

/**
 * Extend the active pointer stroke along the drag path.
 * @param {PointerEvent} event - Pointer move on the board.
 */
function onPointerMove(event) {
  if (!stroke || stroke.pointerId !== event.pointerId) return;
  const { x, y } = cellAt(event);
  paintTo(x, y);
}

/**
 * Commit the pointer stroke, or drop a dragged placement on a different cell.
 * @param {PointerEvent} event - Pointer up on the board.
 */
function onPointerUp(event) {
  if (stroke && stroke.pointerId === event.pointerId) {
    const { x, y } = cellAt(event);
    paintTo(x, y);
    finishStroke(true);
    return;
  }
  if (drag && drag.pointerId === event.pointerId) {
    const { x, y } = cellAt(event);
    const from = drag;
    drag = null;
    if (board.hasPointerCapture(event.pointerId)) board.releasePointerCapture(event.pointerId);
    const chosen = selectedId === null ? undefined : doc.byId.get(selectedId);
    if (chosen && (x !== from.x || y !== from.y)) {
      if (inBounds(x, y)) moveCursor(x, y);
      movePlacement(chosen, x, y);
    }
  }
}

/**
 * Cancel the pointer's gesture when the browser cancels the pointer or capture is lost.
 * @param {PointerEvent} event - Pointer cancel or lost capture.
 */
function onPointerCancel(event) {
  if (stroke && stroke.pointerId === event.pointerId) finishStroke(false);
  if (drag && drag.pointerId === event.pointerId) cancelGesture();
}

/** Cancel pointer and keyboard gestures when the board loses focus. */
function onBlur() {
  if (stroke || drag) cancelGesture();
}

/**
 * Load the Go editor core and wire up the UI.
 * @returns {Promise<void>} Resolves once the editor is ready.
 * @throws {Error} When the Wasm module cannot be fetched or does not start.
 */
async function start() {
  const go = new Go();
  const response = await fetch("editor.wasm");
  if (!response.ok) throw new Error(`Wasm download failed: HTTP ${response.status}`);
  const { instance } = await WebAssembly.instantiateStreaming(response, go.importObject);
  go.run(instance).then(
    () => setStatus("The editor core stopped. Reload the page.", true),
    error => setStatus(`The editor core failed: ${error.message}`, true),
  );
  bridge = globalThis.gojeLevelEditor;
  if (!bridge) throw new Error("The editor core did not start.");

  openInput.addEventListener("change", () => {
    const file = openInput.files?.[0];
    openInput.value = ""; // Release the selection; the same file can be chosen again.
    if (file) openFile(file).catch(error => setStatus(error.message, true));
  });
  for (const [name, button] of Object.entries(toolButtons)) {
    button.addEventListener("click", () => selectTool(/** @type {"floor" | "wall" | "placement"} */ (name)));
  }
  undoButton.addEventListener("click", () => history("undo"));
  redoButton.addEventListener("click", () => history("redo"));
  exportButton.addEventListener("click", exportLevel);
  board.addEventListener("keydown", onKeyDown);
  board.addEventListener("pointerdown", onPointerDown);
  board.addEventListener("pointermove", onPointerMove);
  board.addEventListener("pointerup", onPointerUp);
  board.addEventListener("pointercancel", onPointerCancel);
  board.addEventListener("lostpointercapture", onPointerCancel);
  board.addEventListener("blur", onBlur);
  openInput.disabled = false;
  setStatus("Choose a level file to open.");
}

start().catch(error => {
  setStatus(error.message, true);
  console.error(error);
});
