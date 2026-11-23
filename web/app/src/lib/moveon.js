// Where the page goes once an item is dealt with (answered, approved, denied,
// dismissed) on a wide screen. Pinned items are never a destination: they
// stay on top by choice and are not work waiting in line. Preference: the
// next question after here, else the nearest question before it, else the
// next item of any kind, else the nearest one before; '' (the list) when
// only pinned items are left. Only open items count (an open question, an
// unread item): a read one still listed (lingering, or under Show: All) is
// not waiting. Previous and Next still walk every item; Next open item (n)
// goes where this would.

/**
 * @param {string[]} order the list's ids, in the order shown
 * @param {string} here the item just dealt with
 * @param {Set<string>} pinned ids of pinned items, in any section
 * @param {Set<string> | null} open ids of open items; null counts every item
 */
export function moveOnTarget(order, here, pinned, open = null) {
  const i = order.indexOf(here);
  const ok = (/** @type {string} */ id) => id !== here && !pinned.has(id) && (!open || open.has(id));
  const after = order.slice(i + 1).filter(ok);
  const before = order.slice(0, Math.max(0, i)).filter(ok).reverse();
  const q = (/** @type {string} */ id) => id.startsWith('q-');
  return after.find(q) || before.find(q) || after[0] || before[0] || '';
}

// When nothing open is left (n finds nothing, or the last open item was just
// dealt with), the toast says so, once: ALL_CLEAR_KEY keeps a repeat from
// stacking a second copy.
export const ALL_CLEAR = '🎉 All caught up';
export const ALL_CLEAR_KEY = 'all-clear';
