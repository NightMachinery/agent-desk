// The order of the items in the list, for previous and next. Opening an item
// marks it read, which drops it from the list on the next refresh; the order
// is therefore a snapshot, kept until the list changes while the open item
// is still in it (or nothing is open).

export const nav = $state({ order: /** @type {string[]} */ ([]) });

/** Records the list's order, unless the open item has dropped out of it. */
export function snapshot(ids, current) {
  const seen = new Set();
  const order = ids.filter((id) => !seen.has(id) && seen.add(id));
  if (!current || order.includes(current) || !nav.order.includes(current)) nav.order = order;
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
