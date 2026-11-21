// How M-x orders and narrows commands, the way the user's Doom does it
// (counsel-M-x with amx and orderless):
// - with nothing typed, the last 7 commands run come first, most recent
//   first, then the others by how often they were run, then shorter names,
//   then A to Z (amx);
// - typed words narrow it: each word matches as a regular expression, as
//   plain text or as an initialism ("mrt" for "Mark read toggle"), in any
//   order (orderless); the matches keep that order, shorter labels first
//   (ivy--shorter-matches-first).
// Its history (M-p, M-n) is what was typed before a pick, newest first.

export const RECENT = 7;
const INPUTS = 100;

/** @typedef {{recent: string[], counts: Record<string, number>, inputs: string[]}} History */

/** An empty history. */
export const fresh = () => /** @type {History} */ ({ recent: [], counts: {}, inputs: [] });

/** items in amx's order, by their names (items without one keep their place
 * after the named ones). */
export function rank(items, h) {
  const at = new Map(h.recent.slice(0, RECENT).map((n, i) => [n, i]));
  const score = (it) => (it.name ? at.get(it.name) ?? RECENT : RECENT + 1);
  return items
    .map((it, i) => ({ it, i }))
    .sort((a, b) => {
      const sa = score(a.it), sb = score(b.it);
      if (sa !== sb) return sa - sb;
      if (sa !== RECENT) return a.i - b.i; // recent ones by recency; unnamed ones as given
      const ca = h.counts[a.it.name] ?? 0, cb = h.counts[b.it.name] ?? 0;
      return cb - ca || a.it.label.length - b.it.label.length || a.it.label.localeCompare(b.it.label);
    })
    .map((x) => x.it);
}

const initials = (s) => s.toLowerCase().split(/[^a-z0-9]+/).filter(Boolean).map((w) => w[0]).join('');

/** One typed word as orderless reads it: a test on the text to match. */
function component(word) {
  const w = word.toLowerCase();
  let re = null;
  try {
    re = new RegExp(word, 'i');
  } catch {
    // Not a valid regular expression: plain text only.
  }
  return (hay, label) => hay.includes(w) || (re !== null && re.test(hay)) || initials(label).includes(w);
}

/** The items matching query, ordered: shorter labels first, ties as given. */
export function narrow(items, query) {
  const words = query.split(/\s+/).filter(Boolean).map(component);
  if (!words.length) return items;
  return items
    .filter((it) => {
      const hay = `${it.label} ${it.name ?? ''} ${it.detail ?? ''} ${it.key ?? ''}`.toLowerCase();
      return words.every((m) => m(hay, it.label));
    })
    .map((it, i) => ({ it, i }))
    .sort((a, b) => a.it.label.length - b.it.label.length || a.i - b.i)
    .map((x) => x.it);
}

/** The history after picking the command name, having typed input. */
export function record(h, name, input) {
  const out = { recent: [...h.recent], counts: { ...h.counts }, inputs: [...h.inputs] };
  if (name) {
    out.recent = [name, ...out.recent.filter((n) => n !== name)].slice(0, RECENT);
    out.counts[name] = (out.counts[name] ?? 0) + 1;
  }
  const t = input.trim();
  if (t) out.inputs = [t, ...out.inputs.filter((x) => x !== t)].slice(0, INPUTS);
  return out;
}

/** Stepping through the inputs: M-p goes back (+1), M-n forward (-1). at is
 * -1 for what is being typed now. Returns the new position and the text to
 * show (draft when back at -1). */
export function step(inputs, at, by, draft) {
  const to = Math.max(-1, Math.min(inputs.length - 1, at + by));
  return { at: to, text: to < 0 ? draft : inputs[to] };
}

const KEY = 'agent-desk.mx';

/** The saved history (an empty one when the browser keeps none). */
export function load(store = globalThis.localStorage) {
  try {
    const h = JSON.parse(store?.getItem(KEY) ?? 'null');
    if (h && Array.isArray(h.recent) && Array.isArray(h.inputs) && h.counts && typeof h.counts === 'object') return h;
  } catch {
    // A damaged entry: start again.
  }
  return fresh();
}

export function save(h, store = globalThis.localStorage) {
  try {
    store?.setItem(KEY, JSON.stringify(h));
  } catch {
    // Storage full or off: the order just does not learn.
  }
}
