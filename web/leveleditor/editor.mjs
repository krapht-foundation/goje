/** @file Browser UI for the Goje level editor. Go owns the level; this module owns display and input. */

/**
 * @typedef {object} Placement
 * @property {number} id - Stable placement ID.
 * @property {string} kind - Game-defined kind, treated as plain text.
 * @property {number} x - Zero-based column.
 * @property {number} y - Zero-based row.
 */

/**
 * @typedef {object} Zone
 * @property {number} id - Stable zone ID.
 * @property {string} kind - Game-defined kind, treated as plain text.
 * @property {"solid" | "trigger"} role - Whether the zone blocks movement or only reports overlaps.
 * @property {number} x - First column.
 * @property {number} y - First row.
 * @property {number} w - Width in cells.
 * @property {number} h - Height in cells.
 * @property {number} bottom - Lowest projected pixel of the vertical extent.
 * @property {number} top - Projected pixel just above the vertical extent.
 */

/**
 * @typedef {object} EditorState
 * @property {number} width - Board columns.
 * @property {number} height - Board rows.
 * @property {string} terrain - Row-major cells, "." for floor and "#" for wall.
 * @property {Placement[]} placements - Placements in row-major cell order.
 * @property {Zone[]} zones - Zones in file order.
 * @property {number} sight - Sight radius in cells, or 0 when the level sets none.
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
 * @property {(kind: string, role: string, x: number, y: number, w: number, h: number, bottom: number, top: number) => string} addZone - Append a zone.
 * @property {(id: number, kind: string, role: string, x: number, y: number, w: number, h: number, bottom: number, top: number) => string} editZone - Replace a zone.
 * @property {(id: number) => string} deleteZone - Remove a zone.
 * @property {(kind: string, x: number, y: number, id: number) => string} addPlacement - Add a placement; ID 0 means the next free ID.
 * @property {(id: number, kind: string) => string} setPlacementKind - Change a placement's kind.
 * @property {(id: number) => string} deletePlacement - Remove a placement.
 * @property {() => string} undo - Undo one action.
 * @property {() => string} redo - Redo one action.
 * @property {() => EditorState} state - Snapshot the document.
 * @property {() => Uint8Array | string} exportLevel - Encode the document or return a diagnostic.
 * @property {() => string} markExported - Mark the exported revision clean.
 * @property {(radius: number) => string} setSight - Set the sight radius; 0 removes it.
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
  add: /** @type {HTMLButtonElement} */ (document.querySelector("#add-placement")),
  zone: /** @type {HTMLButtonElement} */ (document.querySelector("#zone")),
};
const undoButton = /** @type {HTMLButtonElement} */ (document.querySelector("#undo"));
const redoButton = /** @type {HTMLButtonElement} */ (document.querySelector("#redo"));
const exportButton = /** @type {HTMLButtonElement} */ (document.querySelector("#export"));
const zonePanel = /** @type {HTMLFieldSetElement} */ (document.querySelector("#zones"));
const zoneSelect = /** @type {HTMLSelectElement} */ (document.querySelector("#zone-select"));
const zoneKind = /** @type {HTMLInputElement} */ (document.querySelector("#zone-kind"));
const zoneRole = /** @type {HTMLSelectElement} */ (document.querySelector("#zone-role"));
const zoneNumbers = ["x", "y", "w", "h", "bottom", "top"].map(
  name => /** @type {HTMLInputElement} */ (document.querySelector(`#zone-${name}`)),
);
const zoneAdd = /** @type {HTMLButtonElement} */ (document.querySelector("#zone-add"));
const zoneApply = /** @type {HTMLButtonElement} */ (document.querySelector("#zone-apply"));
const zoneDelete = /** @type {HTMLButtonElement} */ (document.querySelector("#zone-delete"));
const zoneSightPreset = /** @type {HTMLButtonElement} */ (document.querySelector("#zone-sight-preset"));
const visionPanel = /** @type {HTMLFieldSetElement} */ (document.querySelector("#vision"));
const sightRadius = /** @type {HTMLInputElement} */ (document.querySelector("#sight-radius"));
const sightApply = /** @type {HTMLButtonElement} */ (document.querySelector("#sight-apply"));
const placementPanel = /** @type {HTMLFieldSetElement} */ (document.querySelector("#placements"));
const placementSelect = /** @type {HTMLSelectElement} */ (document.querySelector("#placement-select"));
const placementKind = /** @type {HTMLInputElement} */ (document.querySelector("#placement-kind"));
const placementId = /** @type {HTMLInputElement} */ (document.querySelector("#placement-id"));
const placementX = /** @type {HTMLInputElement} */ (document.querySelector("#placement-x"));
const placementY = /** @type {HTMLInputElement} */ (document.querySelector("#placement-y"));
const placementAdd = /** @type {HTMLButtonElement} */ (document.querySelector("#placement-add"));
const placementApply = /** @type {HTMLButtonElement} */ (document.querySelector("#placement-apply"));
const placementDelete = /** @type {HTMLButtonElement} */ (document.querySelector("#placement-delete"));
const ctx = /** @type {CanvasRenderingContext2D} */ (board.getContext("2d"));

/** @type {Bridge} */
let bridge;
/**
 * Mirror of the Go document used for drawing; refreshed after each change.
 * @type {{width: number, height: number, terrain: Uint8Array, placements: Placement[], byIndex: Map<number, Placement>, byId: Map<number, Placement>, zones: Zone[], sight: number, dirty: boolean, canUndo: boolean, canRedo: boolean} | null}
 */
let doc = null;
/** @type {"floor" | "wall" | "placement" | "add" | "zone"} */
let tool = "floor";
const cursor = { x: 0, y: 0 };
/** @type {number | null} ID of the selected placement: the one chosen for a move, or in the Placements panel. */
let selectedId = null;
/**
 * Active terrain stroke. pointerId is null for keyboard strokes.
 * @type {{pointerId: number | null, tile: number, last: {x: number, y: number} | null, changed: number, rejected: number, error: string} | null}
 */
let stroke = null;
/** @type {{pointerId: number, x: number, y: number} | null} Pointer that selected a placement and may drag it. */
let drag = null;
/** @type {number | null} ID of the zone shown in the form and outlined on the board. */
let selectedZoneId = null;
/** @type {{x: number, y: number} | null} First corner of the zone being drawn. */
let zoneAnchor = null;
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
    if (!old || old.x !== p.x || old.y !== p.y || old.kind !== p.kind) {
      if (old) changed.add(old.y * s.width + old.x);
      changed.add(i);
    }
  }
  if (!full) {
    for (const old of doc.placements) if (!byId.has(old.id)) changed.add(old.y * s.width + old.x);
  }
  const placementsChanged = full || s.placements.length !== doc.placements.length ||
    s.placements.some((p, n) => { const q = doc.placements[n]; return p.id !== q.id || p.kind !== q.kind || p.x !== q.x || p.y !== q.y; });
  const zonesChanged = full || JSON.stringify(s.zones) !== JSON.stringify(doc.zones);
  // Only a change from Go overwrites the field, so a refresh never discards typing.
  if (full || s.sight !== doc.sight) sightRadius.value = String(s.sight);
  doc = { width: s.width, height: s.height, terrain, placements: s.placements, byIndex, byId, zones: s.zones, sight: s.sight, dirty: s.dirty, canUndo: s.canUndo, canRedo: s.canRedo };
  if (selectedId !== null && !byId.has(selectedId)) selectedId = null;
  if (selectedZoneId !== null && !s.zones.some(z => z.id === selectedZoneId)) selectedZoneId = null;
  if (full) resizeBoard();
  if (zonesChanged) {
    // A zone covers many cells, so redraw the whole board and the list together.
    drawAll();
    updateZoneList();
  } else {
    for (const i of changed) drawCell(i % doc.width, Math.floor(i / doc.width));
  }
  if (placementsChanged) updatePlacementList();
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
  drawZones(x, y, px, py);

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
  zonePanel.disabled = !open;
  visionPanel.disabled = !open;
  placementPanel.disabled = !open;
  placementApply.disabled = !open || selectedId === null;
  placementDelete.disabled = !open || selectedId === null;
  zoneApply.disabled = !open || selectedZoneId === null;
  zoneDelete.disabled = !open || selectedZoneId === null;
  document.title = open && doc.dirty ? "• Goje Level Editor" : "Goje Level Editor";
}

/** Describe the cursor cell, its contents, and any pending move in text. */
function describeCursor() {
  if (!doc) return;
  const i = cursor.y * doc.width + cursor.x;
  let text = `Cell ${cursor.x}, ${cursor.y}: ${doc.terrain[i] === WALL ? "wall" : "floor"}`;
  const p = doc.byIndex.get(i);
  if (p) text += `, ${p.kind} #${p.id}`;
  const zones = doc.zones.filter(z => zoneCovers(z, cursor.x, cursor.y));
  if (zones.length) text += `, inside ${zones.map(z => `${z.role} zone #${z.id} ${z.kind}`).join(" and ")}`;
  const chosen = selectedId === null ? undefined : doc.byId.get(selectedId);
  if (chosen && tool === "placement") text += `. Moving ${chosen.kind} #${chosen.id} from ${chosen.x}, ${chosen.y}`;
  else if (chosen) text += `. Selected ${chosen.kind} #${chosen.id} at ${chosen.x}, ${chosen.y}`;
  if (zoneAnchor) text += `. Drawing a zone from ${zoneAnchor.x}, ${zoneAnchor.y}`;
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
  updatePlacementList();
  updateControls();
  describeCursor();
}

/** Rebuild the placement list from the mirror, as text, keeping the selection. */
function updatePlacementList() {
  placementSelect.replaceChildren(...doc.placements.map(p => {
    const option = document.createElement("option");
    option.value = String(p.id);
    option.textContent = `#${p.id} ${p.kind} at ${p.x}, ${p.y}`;
    option.selected = p.id === selectedId;
    return option;
  }));
}

/**
 * Add a placement of the panel's kind and ID at a cell as one action, and select it.
 * @param {number} x - Column.
 * @param {number} y - Row.
 */
function addPlacementAt(x, y) {
  const typed = placementId.value.trim();
  const id = typed === "" ? 0 : Number(typed);
  if (!Number.isInteger(id) || id < 0 || id > 4294967295 || (typed !== "" && id === 0)) {
    setStatus("Placement ID must be a whole number from 1 to 4294967295, or blank for the next free ID.", true);
    return;
  }
  const err = bridge.addPlacement(placementKind.value.trim(), x, y, id);
  if (err) {
    setStatus(err, true);
    return;
  }
  // An authored ID can be used only once, so the next addition defaults to the next free ID.
  placementId.value = "";
  refresh();
  const added = doc.byIndex.get(y * doc.width + x);
  setSelection(added.id);
  setStatus(`Added ${added.kind} #${added.id} at ${x}, ${y}.`);
}

/** Add a placement at the panel's typed cell. */
function addPlacementFromForm() {
  if (!doc) return;
  const x = placementX.valueAsNumber;
  const y = placementY.valueAsNumber;
  if (!Number.isInteger(x) || !Number.isInteger(y)) {
    setStatus("Placement X and Y must be whole numbers.", true);
    return;
  }
  addPlacementAt(x, y);
}

/** Change the selected placement's kind to the panel's kind as one action. */
function applyPlacementKind() {
  if (!doc || selectedId === null) return;
  const id = selectedId;
  const err = bridge.setPlacementKind(id, placementKind.value.trim());
  if (err) {
    setStatus(err, true);
    return;
  }
  refresh();
  const p = doc.byId.get(id);
  setStatus(`Placement #${id} is now ${p.kind}.`);
}

/** Delete the selected placement as one action. */
function deleteSelectedPlacement() {
  if (!doc || selectedId === null || stroke) return;
  const id = selectedId;
  const err = bridge.deletePlacement(id);
  if (err) {
    setStatus(err, true);
    return;
  }
  selectedId = null;
  refresh();
  setStatus(`Deleted placement #${id}.`);
}

/**
 * Report whether a zone covers a cell.
 * @param {Zone} z - Zone to test.
 * @param {number} x - Column.
 * @param {number} y - Row.
 * @returns {boolean} Whether the cell is inside the zone's rectangle.
 */
function zoneCovers(z, x, y) {
  return x >= z.x && y >= z.y && x < z.x + z.w && y < z.y + z.h;
}

/**
 * Draw zone overlays on one cell. Solids are hatched and triggers have a dotted border, with an
 * S or T label and the ID at the zone's first cell, so the role is never shown by color alone.
 * @param {number} x - Column.
 * @param {number} y - Row.
 * @param {number} px - Cell left edge in CSS pixels.
 * @param {number} py - Cell top edge in CSS pixels.
 */
function drawZones(x, y, px, py) {
  for (const z of doc.zones) {
    if (!zoneCovers(z, x, y)) continue;
    const solid = z.role === "solid";
    ctx.fillStyle = solid ? "rgba(234, 138, 40, 0.38)" : "rgba(56, 189, 248, 0.32)";
    ctx.fillRect(px, py, CELL, CELL);
    ctx.strokeStyle = solid ? "#b45309" : "#0369a1";
    ctx.lineWidth = 2;
    if (solid) {
      ctx.beginPath();
      ctx.moveTo(px, py + CELL);
      ctx.lineTo(px + CELL, py);
      ctx.moveTo(px, py + CELL / 2);
      ctx.lineTo(px + CELL / 2, py);
      ctx.moveTo(px + CELL / 2, py + CELL);
      ctx.lineTo(px + CELL, py + CELL / 2);
      ctx.stroke();
    } else {
      ctx.setLineDash([3, 3]);
      ctx.strokeRect(px + 2, py + 2, CELL - 4, CELL - 4);
      ctx.setLineDash([]);
    }
    if (x === z.x && y === z.y) {
      ctx.fillStyle = "#111";
      ctx.font = "bold 10px system-ui, sans-serif";
      ctx.textAlign = "left";
      ctx.textBaseline = "top";
      ctx.fillText(`${solid ? "S" : "T"}${z.id}`, px + 3, py + 3);
    }
    if (z.id === selectedZoneId) {
      ctx.strokeStyle = "#e11d48";
      ctx.lineWidth = 4;
      ctx.beginPath();
      if (x === z.x) {
        ctx.moveTo(px + 2, py);
        ctx.lineTo(px + 2, py + CELL);
      }
      if (x === z.x + z.w - 1) {
        ctx.moveTo(px + CELL - 2, py);
        ctx.lineTo(px + CELL - 2, py + CELL);
      }
      if (y === z.y) {
        ctx.moveTo(px, py + 2);
        ctx.lineTo(px + CELL, py + 2);
      }
      if (y === z.y + z.h - 1) {
        ctx.moveTo(px, py + CELL - 2);
        ctx.lineTo(px + CELL, py + CELL - 2);
      }
      ctx.stroke();
    }
  }
  if (zoneAnchor && zoneAnchor.x === x && zoneAnchor.y === y) {
    ctx.strokeStyle = "#c026d3";
    ctx.lineWidth = 3;
    ctx.setLineDash([4, 2]);
    ctx.strokeRect(px + 3, py + 3, CELL - 6, CELL - 6);
    ctx.setLineDash([]);
  }
}

/** Rebuild the zone list from the mirror, as text, keeping the selection. */
function updateZoneList() {
  zoneSelect.replaceChildren(...doc.zones.map(z => {
    const option = document.createElement("option");
    option.value = String(z.id);
    option.textContent = `#${z.id} ${z.kind} (${z.role}) at ${z.x}, ${z.y}, ${z.w} × ${z.h} cells, height ${z.bottom} to ${z.top}`;
    option.selected = z.id === selectedZoneId;
    return option;
  }));
}

/**
 * Read the zone form as bridge arguments, or report why it cannot be used.
 * @returns {[string, string, number, number, number, number, number, number] | null} Kind, role, x, y, w, h, bottom, top.
 */
function readZoneForm() {
  const numbers = zoneNumbers.map(input => input.valueAsNumber);
  if (!numbers.every(Number.isInteger)) {
    setStatus("Zone position, size, and heights must be whole numbers.", true);
    return null;
  }
  return [zoneKind.value.trim(), zoneRole.value, numbers[0], numbers[1], numbers[2], numbers[3], numbers[4], numbers[5]];
}

/**
 * Show a zone's values in the form.
 * @param {Zone} z - Zone to show.
 */
function fillZoneForm(z) {
  zoneKind.value = z.kind;
  zoneRole.value = z.role;
  [z.x, z.y, z.w, z.h, z.bottom, z.top].forEach((value, i) => {
    zoneNumbers[i].value = String(value);
  });
}

/**
 * Choose the zone shown in the form and outlined on the board.
 * @param {number | null} id - Zone ID, or null to clear.
 */
function selectZone(id) {
  selectedZoneId = id;
  const z = id === null ? undefined : doc.zones.find(candidate => candidate.id === id);
  if (z) fillZoneForm(z);
  drawAll();
  updateZoneList();
  updateControls();
  describeCursor();
}

/** Add a zone from the form as one action and select it. */
function addZoneFromForm() {
  const values = readZoneForm();
  if (!doc || !values) return;
  const err = bridge.addZone(...values);
  if (err) {
    setStatus(err, true);
    return;
  }
  refresh();
  const added = doc.zones[doc.zones.length - 1];
  selectZone(added.id);
  setStatus(`Added ${added.role} zone #${added.id} ${added.kind}.`);
}

/** Replace the selected zone with the form's values as one action. */
function applyZoneForm() {
  const values = readZoneForm();
  if (!doc || selectedZoneId === null || !values) return;
  const err = bridge.editZone(selectedZoneId, ...values);
  if (err) {
    setStatus(err, true);
    return;
  }
  refresh();
  setStatus(`Updated zone #${selectedZoneId}.`);
}

/** Set the level's sight radius from the Vision panel as one action. */
function applySight() {
  const radius = sightRadius.valueAsNumber;
  if (!doc || !Number.isInteger(radius)) {
    setStatus("The sight radius must be a whole number.", true);
    return;
  }
  const err = bridge.setSight(radius);
  if (err) {
    setStatus(err, true);
    return;
  }
  refresh();
  setStatus(radius === 0 ? "The level sets no sight radius." : `Sight radius is ${radius} cells.`);
}

/** Fill the zone form with a sight blocker, which blocks sight but not movement, keeping its cells. */
function applySightBlockPreset() {
  zoneKind.value = "sight_block";
  zoneRole.value = "trigger";
  zoneNumbers[4].value = "0";
  zoneNumbers[5].value = "32";
  setStatus("Zone form set to a sight block. Draw it, or type its cells and choose Add zone.");
}

/** Delete the selected zone as one action. */
function deleteSelectedZone() {
  if (!doc || selectedZoneId === null) return;
  const id = selectedZoneId;
  const err = bridge.deleteZone(id);
  if (err) {
    setStatus(err, true);
    return;
  }
  selectedZoneId = null;
  refresh();
  setStatus(`Deleted zone #${id}.`);
}

/**
 * Handle a zone-tool choice: the first cell is one corner, and the second is the opposite corner.
 * @param {number} x - Column.
 * @param {number} y - Row.
 * @returns {boolean} Whether this choice started a zone, so a drag can finish it.
 */
function chooseZoneCell(x, y) {
  if (!zoneAnchor) {
    zoneAnchor = { x, y };
    drawCell(x, y);
    describeCursor();
    setStatus(`Zone corner at ${x}, ${y}. Choose the opposite corner, or press Escape to cancel.`);
    return true;
  }
  const anchor = zoneAnchor;
  zoneAnchor = null;
  drawCell(anchor.x, anchor.y);
  const values = [Math.min(anchor.x, x), Math.min(anchor.y, y), Math.abs(anchor.x - x) + 1, Math.abs(anchor.y - y) + 1];
  values.forEach((value, i) => {
    zoneNumbers[i].value = String(value);
  });
  addZoneFromForm();
  return false;
}

/** Cancel any in-progress gesture: a stroke rolls back, and a placement selection clears. */
function cancelGesture() {
  const pointerId = drag?.pointerId;
  drag = null;
  if (pointerId !== undefined && board.hasPointerCapture(pointerId)) board.releasePointerCapture(pointerId);
  if (stroke) finishStroke(false);
  else if (zoneAnchor) {
    const anchor = zoneAnchor;
    zoneAnchor = null;
    drawCell(anchor.x, anchor.y);
    describeCursor();
    setStatus("Zone canceled.");
  } else if (selectedId !== null) {
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
  if (tool === "add") {
    addPlacementAt(cursor.x, cursor.y);
    return;
  }
  if (tool === "zone") {
    chooseZoneCell(cursor.x, cursor.y);
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
 * Select an editing tool, clearing any pending placement move or zone corner.
 * @param {"floor" | "wall" | "placement" | "add" | "zone"} name - Tool to select.
 */
function selectTool(name) {
  if (stroke) finishStroke(false);
  drag = null;
  if (zoneAnchor) {
    const anchor = zoneAnchor;
    zoneAnchor = null;
    drawCell(anchor.x, anchor.y);
  }
  tool = name;
  if (selectedId !== null) setSelection(null);
  updateControls();
  const messages = {
    placement: "Move placement: choose a placement, then a floor cell.",
    add: "Add placement: choose a free floor cell. Kind and ID come from the Placements panel.",
    zone: "Draw zone: choose one corner, then the opposite corner. Kind, role, and heights come from the Zones panel.",
    floor: "Floor brush selected.",
    wall: "Wall brush selected.",
  };
  setStatus(messages[name]);
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
  selectedZoneId = null;
  zoneAnchor = null;
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
  } else if (event.key === "Delete" && !stroke) {
    event.preventDefault();
    deleteSelectedPlacement();
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
  if (tool === "add") {
    addPlacementAt(x, y);
    return;
  }
  if (tool === "placement") {
    if (choosePlacementCell(x, y)) {
      drag = { pointerId: event.pointerId, x, y };
      board.setPointerCapture(event.pointerId);
    }
    return;
  }
  if (tool === "zone") {
    if (chooseZoneCell(x, y)) {
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
    if (tool === "zone") {
      // Releasing on another cell finishes the rectangle; releasing in place keeps the corner for a second click.
      if ((x !== from.x || y !== from.y) && inBounds(x, y)) {
        moveCursor(x, y);
        chooseZoneCell(x, y);
      }
      return;
    }
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
    button.addEventListener("click", () => selectTool(/** @type {"floor" | "wall" | "placement" | "add" | "zone"} */ (name)));
  }
  undoButton.addEventListener("click", () => history("undo"));
  redoButton.addEventListener("click", () => history("redo"));
  exportButton.addEventListener("click", exportLevel);
  zoneAdd.addEventListener("click", addZoneFromForm);
  zoneApply.addEventListener("click", applyZoneForm);
  zoneDelete.addEventListener("click", deleteSelectedZone);
  zoneSightPreset.addEventListener("click", applySightBlockPreset);
  sightApply.addEventListener("click", applySight);
  zoneSelect.addEventListener("change", () => {
    const id = Number(zoneSelect.value);
    const z = doc?.zones.find(candidate => candidate.id === id);
    if (!z) return;
    selectZone(id);
    moveCursor(z.x, z.y);
  });
  placementAdd.addEventListener("click", addPlacementFromForm);
  placementApply.addEventListener("click", applyPlacementKind);
  placementDelete.addEventListener("click", deleteSelectedPlacement);
  placementSelect.addEventListener("change", () => {
    const p = doc?.byId.get(Number(placementSelect.value));
    if (!p) return;
    placementKind.value = p.kind;
    setSelection(p.id);
    moveCursor(p.x, p.y);
  });
  placementSelect.addEventListener("keydown", event => {
    if (event.key !== "Delete") return;
    event.preventDefault();
    deleteSelectedPlacement();
  });
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
