// Tooltips are our own cards, not the browser's: components keep writing
// title="..." (or tip() and off()), and components/Tooltip.svelte moves the
// title aside when the pointer or keyboard reaches the element and shows the
// text as a small structured card. This file is the pure part: how a title
// splits into the card's parts.
//
// The conventions it reads:
// - tip(label, cmd) ends the text with two spaces and the key in brackets:
//   "Defer it  (, z)", alternatives joined by " · ". Keys become key caps.
// - The first clause is the heading, up to ": ", "; " or ". ", when it is
//   short; the rest is the body. A short text with no such break is all
//   heading, a long one all body.
// - A trailing "(...)" after the heading is a note: body text.
// - off() keeps a disabled control's usual title in data-tip-what, so the
//   card says what the control does and, below, why it is unavailable now.

const KEY = /\s{2}\(([^()]+)\)\s*$/;
const HEAD_MAX = 64;

/**
 * Splits a title into the card's parts.
 * @param {string} text
 * @returns {{head: string, body: string, keys: string[][]}}
 */
export function parseTip(text) {
  let t = (text ?? '').trim();
  /** @type {string[][]} */
  let keys = [];
  const k = t.match(KEY);
  if (k) {
    t = t.slice(0, k.index).trim();
    keys = k[1].split(/\s+·\s+/).map(chord).filter((c) => c.length); // alternatives: tip() joins them with " · "
  }
  if (!t) return { head: '', body: '', keys };
  const at = firstBreak(t);
  if (at > 0 && at <= HEAD_MAX) {
    return { head: t.slice(0, at).trim(), body: capital(t.slice(at + 1).trim()), keys };
  }
  const note = t.match(/^(.{1,64}?)\s+\(([^()]+)\)$/);
  if (note) return { head: note[1], body: capital(note[2]), keys };
  return t.length <= HEAD_MAX ? { head: t, body: '', keys } : { head: '', body: t, keys };
}

/** The index of the first ": ", "; " or sentence end, or -1. */
function firstBreak(t) {
  const m = t.match(/[:;](?=\s)|[.!?](?=\s+[A-Z(])/);
  return m?.index ?? -1;
}

/** "SPC i v" → ["SPC", "i", "v"]; ", z" → [",", "z"]. */
function chord(s) {
  return s.split(/\s+/).filter(Boolean);
}

function capital(s) {
  return s ? s[0].toUpperCase() + s.slice(1) : s;
}

/** The tooltip text of el: its title, or the one Tooltip.svelte moved aside. */
export function tipText(el) {
  return el?.getAttribute?.('title') || el?.dataset?.tip || '';
}
