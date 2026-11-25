// The order of the items in the list, for previous and next. Opening an item
// marks it read, which drops it from the list on the next refresh; the order
// is therefore a snapshot, kept until the list changes while the open item
// is still in it (or nothing is open).

import { untrack } from 'svelte';
import { post } from './api.js';
import { navTarget } from './moveon.js';
import { ui } from './ui.svelte.js';

export const nav = $state({
  order: /** @type {string[]} */ ([]),
  pinned: /** @type {Set<string>} */ (new Set()),
  open: /** @type {Set<string>} */ (new Set()),
  // Each listed item's kind, time (ms) and views, for auto-navigation.
  info: /** @type {Map<string, import('./moveon.js').NavInfo>} */ (new Map()),
});

/** Records the list's order, unless the open item has dropped out of it,
 * which items are pinned (moving on after an answer skips them), which
 * are open (an open question or an unread item: where moving on may go),
 * and what auto-navigation ranks them by (info). */
export function snapshot(ids, current, pinned = new Set(), open = new Set(), info = new Map()) {
  const seen = new Set();
  const order = ids.filter((id) => !seen.has(id) && seen.add(id));
  // Untracked: the caller's effect writes nav.order, so depending on it would
  // loop forever whenever the open item is in neither order.
  const was = untrack(() => nav.order);
  if (!current || order.includes(current) || !was.includes(current)) nav.order = order;
  nav.pinned = pinned;
  nav.open = open;
  nav.info = info;
}

// Opens counted on this page, by display id. The desk's count reaches the
// list with the next reload; until then n must already see this one, or
// it could go straight back to the item it just left.
const seenHere = new Map();

/** An item's kind, time and views, the views counted here included. */
function navInfo(id) {
  const x = nav.info.get(id);
  const views = Math.max(x?.views ?? 0, seenHere.get(id) ?? 0);
  return x ? { ...x, views } : { views };
}

/** Counts one open of the item on the desk (api/seen), for every device's
 * auto-navigation. */
export function countOpen(eventId, displayId) {
  seenHere.set(displayId, (navInfo(displayId).views ?? 0) + 1);
  post('seen', { id: eventId }).catch(() => {
    // offline or an older desk: this page still counts it
  });
}

/** Where auto-navigation goes from here (moveon.js), or ''. */
export function navNext(here) {
  return navTarget(nav.order, here, nav.pinned, nav.open, navInfo, ui.nav);
}

/** The item dir (+1 or -1) steps away from current, or ''. */
export function step(dir, current) {
  const o = nav.order;
  const i = o.indexOf(current);
  if (i < 0) return (dir > 0 ? o[0] : o[o.length - 1]) ?? '';
  return o[i + dir] ?? '';
}

/** Where current stands: { at, of }, at 0 when it is not in the order. */
export function position(current) {
  return { at: nav.order.indexOf(current) + 1, of: nav.order.length };
}

/** Items this page has marked read on opening; never again, so that "mark
 * unread" sticks while the item stays open. */
export const autoRead = new Set();
