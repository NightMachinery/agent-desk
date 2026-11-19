// Where the page goes once an item is dealt with (answered, approved, denied,
// dismissed) on a wide screen. Pinned items are never a destination: they
// stay on top by choice and are not work waiting in line. Preference: the
// next question after here, else the nearest question before it, else the
// next item of any kind, else the nearest one before; '' (the list) when
// only pinned items are left. Previous and Next still walk every item.

/**
 * @param {string[]} order the list's ids, in the order shown
 * @param {string} here the item just dealt with
 * @param {Set<string>} pinned ids of pinned items, in any section
 */
export function moveOnTarget(order, here, pinned) {
  const i = order.indexOf(here);
  const ok = (/** @type {string} */ id) => id !== here && !pinned.has(id);
  const after = order.slice(i + 1).filter(ok);
  const before = order.slice(0, Math.max(0, i)).filter(ok).reverse();
  const q = (/** @type {string} */ id) => id.startsWith('q-');
  return after.find(q) || before.find(q) || after[0] || before[0] || '';
}
