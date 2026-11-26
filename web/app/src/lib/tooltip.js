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
// - A control with data-cmd gets its command's key added (withKey) when the
//   title names none, and a card naming the key when it has no title.

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

/** The index of the first ": ", "; " or sentence end outside brackets, or -1. */
function firstBreak(t) {
  let depth = 0;
  for (let i = 0; i < t.length - 1; i++) {
    const c = t[i];
    if (c === '(' || c === '[') depth++;
    else if ((c === ')' || c === ']') && depth) depth--;
    else if (depth) continue;
    else if ((c === ':' || c === ';') && /\s/.test(t[i + 1])) return i;
    else if ('.!?'.includes(c) && /^\s+[A-Z(]/.test(t.slice(i + 1))) return i;
  }
  return -1;
}

/** "SPC i v" → ["SPC", "i", "v"]; ", z" → [",", "z"]. */
function chord(s) {
  return s.split(/\s+/).filter(Boolean);
}

/** Capitalizes a body, but not one that starts with a URL. */
function capital(s) {
  return s && !/^[a-z][a-z0-9+.-]*:\/\//.test(s) ? s[0].toUpperCase() + s.slice(1) : s;
}

/**
 * The text with key appended as tip() would, unless it names a key already:
 * a control a command clicks (data-cmd) teaches its key even when its title
 * was written without tip(). With no text, label stands in as the heading.
 * @param {string} text
 * @param {string} key
 * @param {string} label
 */
export function withKey(text, key, label) {
  if (!key || parseTip(text).keys.length) return text;
  return `${(text ?? '').trim() || label}  (${key})`;
}

/** A heading for a control with no title: its aria-label, its text if short, else its command's name. */
export function ownLabel(/** @type {string} */ aria, /** @type {string} */ content, /** @type {string} */ cmd) {
  const t = (content ?? '').replace(/\s+/g, ' ').trim();
  return (aria ?? '').trim() || (t && t.length <= 48 ? t : capital(cmd.replace(/-/g, ' ')));
}

/** The tooltip text of el: its title, or the one Tooltip.svelte moved aside. */
export function tipText(el) {
  return el?.getAttribute?.('title') || el?.dataset?.tip || '';
}
