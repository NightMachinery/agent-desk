// Changes the user made that the desk may not show yet, laid over every view
// and item the page gets: the page shows a change at once, and keeps showing
// it while answers that predate it arrive (a reload the live stream started
// before the change reached the desk), so nothing jumps back and forth. A
// change goes once an answer asked for after the desk took it is shown, at
// once if it failed, and after KEEP_MS whatever happens.
//
// Tickets tell which answers are new enough: every request for a view or an
// item takes one before it goes (ticket()) and reports it once its answer is
// shown (shown(kind, t)). A change the desk took when the last ticket out was
// n shows in every answer whose ticket is above n.
//
// A change names the items it touches (display ids), the fields it sets on
// them (unread, draft, answered, dismissed, pinned, deferred), and the
// sections it takes them out of (needs, new, responses, deliverables,
// pinned). Pinning puts an item into Pinned (ListPane does that, from the
// rows it has). The laying over itself is overlay.js.
import { leaves, live, patchGroups, patchItem, settled } from './overlay.js';

const KEEP_MS = 120_000;

/** The sections a read item leaves when the list shows only the unread
 * (Pinned keeps it: a read item stays pinned). */
export const READ_LEAVES = ['needs', 'new', 'responses', 'deliverables'];

let tickets = 0;
/** A ticket for a request about to go. */
export const ticket = () => ++tickets;

let next = 0;
/** changes: see overlay.js. shown: the newest ticket shown, per kind of
 * answer. */
export const pending = $state({ changes: /** @type {any[]} */ ([]), shown: { view: 0, item: 0 } });

// Run when the desk has taken a change: the app asks for a fresh view, so
// the change stops being laid over even if nothing else reloads.
let reload = () => {};
export function onTaken(f) {
  reload = f;
}

/**
 * Shows patch on the items ids at once, and keeps them out of the sections
 * in leave. Returns the change's handle: done() once the desk has taken it,
 * failed() to drop it (the page shows what the desk says again).
 */
export function change(ids, patch = {}, leave = []) {
  const n = ++next;
  pending.changes.push({ n, ids: [...ids], patch, leave, at: Date.now(), done: 0 });
  setTimeout(() => drop(n), KEEP_MS);
  return {
    done: () => {
      const c = pending.changes.find((x) => x.n === n);
      if (!c || c.done) return;
      c.done = tickets;
      reload();
    },
    failed: () => drop(n),
  };
}

/**
 * Runs request (a function returning a promise) as a change: shown at once,
 * kept until the desk's answers show it, dropped if the request fails (the
 * error goes on to the caller). Returns what the request returned.
 */
export async function act(ids, patch, leave, request) {
  const c = change(ids, patch, leave);
  try {
    const out = await request();
    c.done();
    return out;
  } catch (err) {
    c.failed();
    throw err;
  }
}

function drop(n) {
  const i = pending.changes.findIndex((x) => x.n === n);
  if (i >= 0) pending.changes.splice(i, 1);
}

/** An answer of kind ('view' or 'item') asked for with ticket t is shown:
 * the changes it shows stop being laid over. Call it before showing it. */
export function shown(kind, t) {
  if (t <= pending.shown[kind]) return;
  pending.shown[kind] = t;
  for (const c of pending.changes.filter((x) => settled(x, pending.shown))) drop(c.n);
}

const now = (kind) => live(pending.changes, pending.shown, kind);

/** item (a list row, or an item page's item) with the changes on it. */
export const patched = (item, kind = 'view') => patchItem(item, now(kind));

/** Whether a change takes id out of section. */
export const left = (id, section) => leaves(now('view'), id, section);

/** A section's groups with the changes laid over. */
export const patchedGroups = (groups, section) => patchGroups(groups, section, now('view'));

/** An item page's answer (api/item) with the changes on its item laid over;
 * answered is the page's own field. */
export function patchedPage(page) {
  if (!page?.item) return page;
  const it = patched(page.item, 'item');
  if (it === page.item) return page;
  return { ...page, item: it, ...('answered' in it && { answered: it.answered }) };
}
