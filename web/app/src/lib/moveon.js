// Auto-navigation: where Next open item (n) goes, and where a wide screen
// moves on to once an item is dealt with (answered, approved, denied,
// dismissed, a message sent). Only open items are destinations (an open
// question, an unread item): a read one still listed (lingering, or under
// Show: All) is not waiting. Pinned items never are: they stay on top by
// choice. Previous and Next still walk every item in list order.
//
// The order (the "nav" setting, shared by every device):
// 1. Fewer views first (views): how often the dashboard has opened the item,
//    on any device. Most items are read on opening and leave the open set,
//    but an open question stays, so this sends n to the questions not yet
//    looked at, and never back and forth between two open questions.
// 2. Kind (kinds): blocked questions, questions with a default,
//    deliverables, responses, notices, unless reordered.
// 3. Age (age): oldest first, or newest first.

export const navKinds = ['blocked', 'default', 'deliverable', 'response', 'notice'];

export const navKindNames = {
  blocked: 'Blocked questions',
  default: 'Questions with a default',
  deliverable: 'Deliverables',
  response: 'Responses to you',
  notice: 'Notices',
};

/** @typedef {{ views: boolean, kinds: string[], age: 'old' | 'new' }} NavPrefs */
/** @typedef {{ kind?: string, ts?: number, views?: number }} NavInfo */

/** @type {NavPrefs} */
export const defaultNav = { views: true, kinds: navKinds, age: 'old' };

/** prefs made whole: unknown kinds dropped, missing ones appended in the
 * default order, anything else back to its default.
 * @returns {NavPrefs} */
export function normNav(prefs) {
  const p = prefs && typeof prefs === 'object' ? prefs : {};
  const kinds = (Array.isArray(p.kinds) ? p.kinds : []).filter((k, i, all) => navKinds.includes(k) && all.indexOf(k) === i);
  return {
    views: typeof p.views === 'boolean' ? p.views : defaultNav.views,
    kinds: [...kinds, ...navKinds.filter((k) => !kinds.includes(k))],
    age: p.age === 'new' ? 'new' : 'old',
  };
}

/** Whether prefs are the default ones (saved as null). */
export function isDefaultNav(prefs) {
  const p = normNav(prefs);
  return p.views === defaultNav.views && p.age === defaultNav.age && p.kinds.join() === navKinds.join();
}

/**
 * The open, unpinned item auto-navigation goes to from here, or '' when no
 * other is open.
 * @param {string[]} order the list's ids, in the order shown (the last tie-break)
 * @param {string} here the item shown, never the answer
 * @param {Set<string>} pinned ids of pinned items, in any section
 * @param {Set<string>} open ids of open items
 * @param {(id: string) => NavInfo | undefined} info each item's kind, time (ms) and views
 * @param {NavPrefs} prefs
 */
export function navTarget(order, here, pinned, open, info = () => undefined, prefs = defaultNav) {
  const rank = new Map(prefs.kinds.map((k, i) => [k, i]));
  const sign = prefs.age === 'new' ? -1 : 1;
  let best = '';
  /** @type {number[] | null} */
  let bestKey = null;
  order.forEach((id, i) => {
    if (id === here || pinned.has(id) || !open.has(id)) return;
    const x = info(id) ?? {};
    const key = [prefs.views ? (x.views ?? 0) : 0, rank.get(x.kind ?? '') ?? rank.size, x.ts == null ? Infinity : sign * x.ts, i];
    if (!bestKey || before(key, bestKey)) [best, bestKey] = [id, key];
  });
  return best;
}

/** @param {number[]} a @param {number[]} b */
function before(a, b) {
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return a[i] < b[i];
  return false;
}

// When nothing open is left (n finds nothing, or the last open item was just
// dealt with), the toast says so, once: ALL_CLEAR_KEY keeps a repeat from
// stacking a second copy.
export const ALL_CLEAR = '🎉 All caught up';
export const ALL_CLEAR_KEY = 'all-clear';
