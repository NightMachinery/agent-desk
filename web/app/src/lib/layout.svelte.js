// The panes of a wide screen, kept per device (localStorage): how wide the
// sidebar is, the list's share of the room beside it, whether the sidebar or
// the list is hidden, and fullscreen, which shows only the focused pane
// (keys.pane), so the pane keys switch between them. The detail pane is
// never hidden: fullscreen on the list gives the list the whole width. A
// narrow screen shows one pane at a time anyway and ignores all of this.
//
// App.svelte lays the panes out from it (columns()); keymap.js has the
// commands, and the grips, rails and mode line buttons call the same
// functions.
import { keys } from './keys.svelte.js';

const KEY = 'agent-desk.layout';
export const SIDE_MIN = 160;
export const SIDE_MAX = 560;
export const SIDE_DEFAULT = 248;
export const LIST_MIN = 240;
export const DETAIL_MIN = 320;
/** The list's share of the room beside the sidebar by default: list to detail as 1 to 1.15. */
export const SPLIT_DEFAULT = 1 / 2.15;
/** A hidden pane leaves a rail this wide, to bring it back with a click. */
export const RAIL = 22;
/** A grip dragged closer than this to the pane's left edge hides the pane. */
export const HIDE_AT = { side: SIDE_MIN / 2, list: LIST_MIN / 2 };
export const PANES = ['side', 'list', 'detail'];
export const paneLabel = { side: 'Sidebar', list: 'List', detail: 'Detail' };

const clamp = (x, lo, hi) => Math.min(hi, Math.max(lo, x));
const num = (x, lo, hi, d) => (Number.isFinite(x) ? clamp(x, lo, hi) : d);

function load() {
  try {
    return JSON.parse(localStorage.getItem(KEY) ?? '{}') ?? {};
  } catch {
    return {};
  }
}
const saved = load();

export const layout = $state({
  side: num(saved.side, SIDE_MIN, SIDE_MAX, SIDE_DEFAULT),
  split: num(saved.split, 0.05, 0.95, SPLIT_DEFAULT),
  hidden: { side: saved.hidden?.side === true, list: saved.hidden?.list === true },
  full: saved.full === true,
});

export function saveLayout() {
  try {
    localStorage.setItem(KEY, JSON.stringify($state.snapshot(layout)));
  } catch {
    /* private mode: the layout lasts until reload */
  }
}

const wideQuery = matchMedia('(min-width: 901px)');
/** Whether the screen is wide enough for panes side by side. */
export const screen = $state({ wide: wideQuery.matches });
wideQuery.addEventListener('change', (e) => (screen.wide = e.matches));

/** Whether pane p shows on a wide screen. */
export const shows = (p) => (layout.full ? keys.pane === p : !layout.hidden[p]);

/** The panes the pane keys move between, left to right: in fullscreen all
 * three (each shows as it gets focus), else those not hidden. */
export const reachable = () => (layout.full ? PANES : PANES.filter((p) => !layout.hidden[p]));

// The focused pane must be one that shows: the first shown to the right of
// p, else the detail.
function leave(p) {
  if (keys.pane !== p) return;
  keys.pane = PANES.slice(PANES.indexOf(p) + 1).find((x) => !layout.hidden[x]) ?? 'detail';
}

if (!layout.full && layout.hidden[keys.pane]) leave(keys.pane);

/** Hides or shows pane p (the sidebar or the list; the detail stays). Focus
 * moves right off a pane as it hides. Returns false for the detail. */
export function setHidden(p, hide) {
  if (!(p in layout.hidden)) return false;
  layout.hidden[p] = hide;
  if (hide) {
    leave(p);
    // Focus left inside would take the keys to a pane no one sees.
    const el = /** @type {HTMLElement | null} */ (document.activeElement);
    if (el && document.querySelector(`[data-pane="${p}"]`)?.contains(el)) el.blur();
  }
  saveLayout();
  return true;
}

/** Starts or ends fullscreen. Leaving it keeps the pane you were on in
 * view, showing it again if it was hidden. */
export function setFull(on) {
  layout.full = on;
  if (!on && layout.hidden[keys.pane]) layout.hidden[keys.pane] = false;
  saveLayout();
}

/** The sidebar px wide, within its limits. Saved by the caller. */
export const setSide = (px) => (layout.side = Math.round(clamp(px, SIDE_MIN, SIDE_MAX)));

/** The list listPx wide out of total px beside the sidebar, leaving each
 * pane its minimum. Saved by the caller. */
export function setListWidth(listPx, total) {
  const w = clamp(listPx, LIST_MIN, Math.max(LIST_MIN, total - DETAIL_MIN));
  layout.split = clamp(w / total, 0.05, 0.95);
}

/** Every pane back at its default width, out of fullscreen. */
export function resetLayout() {
  Object.assign(layout, { side: SIDE_DEFAULT, split: SPLIT_DEFAULT, hidden: { side: false, list: false }, full: false });
  saveLayout();
}

/** The wide grid's columns: the sidebar (or its rail), a grip, the list (or
 * its rail), a grip, the detail. The sidebar gives way before the others go
 * under their minimum in a small window. */
export function columns() {
  if (layout.full) return 'minmax(0, 1fr)';
  const rest = layout.hidden.list ? RAIL + DETAIL_MIN : LIST_MIN + DETAIL_MIN;
  const side = layout.hidden.side ? `${RAIL}px` : `max(${SIDE_MIN}px, min(${layout.side}px, calc(100vw - ${rest}px)))`;
  const list = layout.hidden.list ? `${RAIL}px` : `minmax(${LIST_MIN}px, ${layout.split}fr)`;
  const detail = layout.hidden.list ? 'minmax(0, 1fr)' : `minmax(${DETAIL_MIN}px, ${1 - layout.split}fr)`;
  return `${side} 0 ${list} 0 ${detail}`;
}
