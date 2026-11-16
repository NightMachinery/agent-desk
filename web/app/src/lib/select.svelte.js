// Rows chosen for a bulk action. Ids are display ids; `order` is every row in
// the list, top to bottom, so shift-click can select a range.
import { SvelteSet } from 'svelte/reactivity';
import { post } from './api.js';
import { toast } from './toast.svelte.js';

export const sel = $state({ ids: new SvelteSet(), anchor: '', order: /** @type {string[]} */ ([]) });

/** Toggles one row; with shift, sets every row from the last one clicked. */
export function toggle(id, shift = false) {
  const { order } = sel;
  const a = order.indexOf(sel.anchor);
  const b = order.indexOf(id);
  if (shift && a >= 0 && b >= 0) {
    const on = !sel.ids.has(id) || sel.ids.has(sel.anchor);
    for (const x of order.slice(Math.min(a, b), Math.max(a, b) + 1)) on ? sel.ids.add(x) : sel.ids.delete(x);
  } else if (sel.ids.has(id)) {
    sel.ids.delete(id);
  } else {
    sel.ids.add(id);
  }
  sel.anchor = id;
}

/** Selects all of ids, or clears them when they are all selected already. */
export function toggleAll(ids) {
  if (ids.length && ids.every((x) => sel.ids.has(x))) ids.forEach((x) => sel.ids.delete(x));
  else ids.forEach((x) => sel.ids.add(x));
}

export function clear() {
  sel.ids.clear();
  sel.anchor = '';
}

/** Drops selected rows that are no longer in the list. */
export function prune(order) {
  sel.order = order;
  for (const x of [...sel.ids]) if (!order.includes(x)) sel.ids.delete(x);
}

/** Marks the selection read or unread; qs open questions among it get dismissed. */
export async function markSelected(read, onchange, qs = 0) {
  const targets = [...sel.ids];
  if (!targets.length) return;
  try {
    await post(read ? 'read' : 'unread', { targets });
    const n = (k, w) => `${k} ${k === 1 ? w : w + 's'}`;
    let msg = `${n(targets.length, 'item')} marked ${read ? 'read' : 'unread'}.`;
    if (read && qs) msg += ` ${n(qs, 'question')} dismissed: still open, answer from Show all.`;
    toast(msg, 'ok', qs ? 6000 : 3000);
    clear();
    onchange();
  } catch (err) {
    toast(err.message, 'error');
  }
}
