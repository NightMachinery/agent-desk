// Keyboard driving, in the style of Doom Emacs's evil mode.
//
// Modes: normal (keys are commands), insert (a text field has focus; Escape
// or C-g leaves it) and visual (moving extends the selection). A key
// sequence is a list of tokens such as "SPC", "g", "C-w" or "G"; a sequence
// that is the start of longer bindings waits for the next key and, after a
// moment, shows the which-key popup listing what can follow.
//
// Bindings belong to a pane (side, list, detail, or dialog while a sheet is
// open) or to all of them. The keymap itself is in keymap.js.

import { route } from './route.svelte.js';

export const keys = $state({
  mode: 'normal', // normal, insert, visual
  pane: 'list', // the focused pane on a wide screen: side, list or detail
  pending: /** @type {string[]} */ ([]),
  count: '',
  which: false, // the which-key popup
  help: false, // the full list of bindings
  palette: /** @type {null | {title: string, items: any[], query: string, index: number}} */ (null),
  hints: /** @type {null | {typed: string, marks: {label: string, el: HTMLElement, x: number, y: number}[]}} */ (null),
  anchor: '', // visual mode's first row
  used: false, // the keyboard has driven the page: show which pane has focus
  message: '',
  tick: 0, // bumped by every key, so derived labels recompute
});

/** What the keymap needs from the app. App.svelte fills it in. */
export const ctx = {
  refresh() {},
  /** @returns {any} */ item: () => null,
  /** @returns {any} */ view: () => null,
  /** @returns {any[]} */ runs: () => [],
};

/** @typedef {{keys: string, seq: string[], desc: string, run: (n: number) => any, pane?: string, when?: () => any, palette?: boolean, anywhere?: boolean}} Binding */
/** @type {Binding[]} */
export const bindings = [];
/** @type {Record<string, string>} prefix -> group name */
export const groups = {};

/** Adds a binding. opts: pane, when, palette (false: not in M-x),
 * anywhere (also inside a sheet). */
export function bind(seq, desc, run, opts = {}) {
  bindings.push({ keys: seq, seq: seq.split(' '), desc, run, ...opts });
}
export function group(prefix, name) {
  groups[prefix] = name;
}

// --- tokens ------------------------------------------------------------------

const named = {
  ' ': 'SPC',
  Enter: 'RET',
  Escape: 'ESC',
  Tab: 'TAB',
  Backspace: 'DEL',
  ArrowDown: '<down>',
  ArrowUp: '<up>',
  ArrowLeft: '<left>',
  ArrowRight: '<right>',
  PageDown: '<next>',
  PageUp: '<prior>',
  Home: '<home>',
  End: '<end>',
};

/** The token for a keydown, or '' for a lone modifier. Command (s-) combos
 * are left to the browser. */
export function token(e) {
  let k = e.key;
  if (['Shift', 'Control', 'Alt', 'Meta', 'CapsLock', 'Dead', 'Unidentified'].includes(k)) return '';
  // On a Mac, Option changes the character; read the key itself.
  if (e.altKey && /^Key[A-Z]$/.test(e.code)) k = e.shiftKey ? e.code.slice(3) : e.code.slice(3).toLowerCase();
  if (e.ctrlKey && k.length === 1) k = k.toLowerCase();
  const isNamed = k in named;
  k = named[k] ?? k;
  let mods = '';
  if (e.ctrlKey) mods += 'C-';
  if (e.altKey) mods += 'M-';
  if (e.metaKey) mods += 's-';
  if (e.shiftKey && isNamed && k !== 'SPC') mods += 'S-';
  return mods + k;
}

// --- context -----------------------------------------------------------------

const narrow = () => matchMedia('(max-width: 900px)').matches;

/** The open sheet, if any. */
export const dialog = () => /** @type {HTMLElement | null} */ (document.querySelector('[role="dialog"]:not(.keys-ui)'));

/** The pane keys act on now. */
export function pane() {
  if (dialog()) return 'dialog';
  if (narrow()) return route.item || route.settings ? 'detail' : 'list';
  return keys.pane;
}

export function paneEl(p = pane()) {
  if (p === 'dialog') return dialog();
  return /** @type {HTMLElement | null} */ (document.querySelector(`[data-pane="${p}"]`));
}

/** Whether el takes typed text, which puts the page in insert mode. */
export function typing(el) {
  return (
    el instanceof HTMLElement &&
    (el.matches('textarea, select, [contenteditable]') ||
      (el.matches('input') && !['checkbox', 'radio', 'button', 'submit'].includes(/** @type {HTMLInputElement} */ (el).type)))
  );
}

const visible = (el) => el.getClientRects().length > 0 && getComputedStyle(el).visibility !== 'hidden';

/** The visible element tagged data-cmd=name, preferring the current pane. */
export function cmdEl(name) {
  const all = [...document.querySelectorAll(`[data-cmd="${name}"]`)].filter(visible);
  const inPane = paneEl();
  return /** @type {HTMLElement | undefined} */ (all.find((el) => inPane?.contains(el)) ?? all[0]);
}

export function say(text, ms = 2200) {
  keys.message = text;
  clearTimeout(sayTimer);
  sayTimer = setTimeout(() => (keys.message = ''), ms);
}
let sayTimer;

// In a sheet only its own bindings apply, and the few marked anywhere.
const active = (b) => (b.pane ? b.pane === pane() : pane() !== 'dialog' || b.anywhere) && (!b.when || b.when());
const starts = (seq, pre) => pre.every((t, i) => seq[i] === t);

/** The binding for seq exactly, a pane's own before the global one. */
function exact(seq) {
  const p = pane();
  const hit = bindings.filter((b) => b.seq.length === seq.length && starts(b.seq, seq) && active(b));
  return hit.find((b) => b.pane === p) ?? hit[0];
}
const isPrefix = (seq) => bindings.some((b) => b.seq.length > seq.length && starts(b.seq, seq) && active(b));

/** What may follow prefix, for which-key: [{key, desc, group}]. */
export function continuations(prefix) {
  const out = new Map();
  for (const b of bindings) {
    if (b.seq.length <= prefix.length || !starts(b.seq, prefix) || !active(b)) continue;
    const k = b.seq[prefix.length];
    if (b.seq.length === prefix.length + 1) {
      if (!out.has(k) || out.get(k).group) out.set(k, { key: k, desc: b.desc, group: false });
    } else if (!out.has(k)) {
      out.set(k, { key: k, desc: groups[[...prefix, k].join(' ')] ?? '+prefix', group: true });
    }
  }
  return [...out.values()].sort((a, b) => sortKey(a.key).localeCompare(sortKey(b.key)));
}
// Letters before punctuation, lower case before upper, as which-key does.
const sortKey = (k) => (/^[a-z]$/.test(k) ? '0' + k : /^[A-Z]$/.test(k) ? '1' + k.toLowerCase() + k : '2' + k);

/** Every binding that applies now, for help and M-x. */
export const activeBindings = () => bindings.filter(active);

// --- dispatch ----------------------------------------------------------------

let whichTimer;
function reset() {
  keys.pending = [];
  keys.count = '';
  keys.which = false;
  clearTimeout(whichTimer);
}

function run(b, n) {
  reset();
  try {
    const r = b.run(n);
    if (r instanceof Promise) r.catch((err) => say(String(err?.message ?? err)));
  } catch (err) {
    say(String(err?.message ?? err));
  }
}

/** The window's keydown handler. */
export function onkeydown(e) {
  if (e.isComposing || e.defaultPrevented) return;
  const tok = token(e);
  if (!tok || e.metaKey) return;
  syncMode();
  queueMicrotask(() => {
    keys.tick++;
    syncMode();
  });
  if (keys.palette) return; // the palette's input handles its own keys
  if (keys.hints) return hintKey(e, tok);
  if (keys.help) {
    if (['ESC', 'C-g', 'q', '?'].includes(tok)) keys.help = false;
    else if (tok === 'j' || tok === 'C-d') document.querySelector('.keyhelp')?.scrollBy(0, tok === 'j' ? 60 : 300);
    else if (tok === 'k' || tok === 'C-u') document.querySelector('.keyhelp')?.scrollBy(0, tok === 'k' ? -60 : -300);
    e.preventDefault();
    return;
  }
  const el = document.activeElement;
  if (typing(el)) return insertKey(e, tok, /** @type {HTMLElement} */ (el));

  // A focused button or link keeps Enter, unless it is a list row.
  if (tok === 'RET' && !keys.pending.length && el instanceof HTMLElement && el.matches('a[href], button, summary') && !el.matches('[data-item]'))
    return;
  if (!keys.pending.length && /^[0-9]$/.test(tok) && (tok !== '0' || keys.count)) {
    keys.count += tok;
    e.preventDefault();
    return;
  }
  const seq = [...keys.pending, tok];
  const b = exact(seq);
  const more = isPrefix(seq);
  if (b && !more) {
    e.preventDefault();
    keys.used = true;
    run(b, Math.max(1, parseInt(keys.count || '1', 10)));
    return;
  }
  if (more) {
    e.preventDefault();
    keys.used = true;
    keys.pending = seq;
    clearTimeout(whichTimer);
    whichTimer = setTimeout(() => (keys.which = true), 300);
    return;
  }
  if (keys.pending.length || keys.count) {
    e.preventDefault();
    say(`${seq.join(' ')} is undefined`);
  }
  reset();
}

/** Insert mode: the field keeps its keys, except leaving it and the C-c
 * pair (C-c C-c sends, C-c C-k discards). */
function insertKey(e, tok, el) {
  if (keys.pending[0] === 'C-c') {
    reset();
    e.preventDefault();
    const box = el.closest('.composer');
    if (tok === 'C-c') {
      const send = box?.querySelector('[data-cmd="send"]');
      if (send instanceof HTMLButtonElement && !send.disabled) send.click();
      else say(box ? 'Nothing to send' : 'C-c C-c sends from a message box');
    } else if (tok === 'C-k') {
      if (el instanceof HTMLTextAreaElement || el instanceof HTMLInputElement) {
        el.value = '';
        el.dispatchEvent(new Event('input', { bubbles: true }));
      }
      el.blur();
      say('Discarded');
    } else say(`C-c ${tok} is undefined`);
    return;
  }
  if (tok === 'ESC' || tok === 'C-g' || tok === 'C-[') {
    e.preventDefault();
    el.blur();
    return;
  }
  if (tok === 'C-c' && el.closest('.composer') && !(el instanceof HTMLTextAreaElement && el.selectionStart !== el.selectionEnd)) {
    e.preventDefault();
    keys.pending = ['C-c'];
    whichTimer = setTimeout(() => (keys.which = true), 300);
  }
}

// Mode follows focus.
export function syncMode() {
  keys.mode = typing(document.activeElement) ? 'insert' : keys.anchor ? 'visual' : 'normal';
}
document.addEventListener('focusin', syncMode);
document.addEventListener('focusout', () => queueMicrotask(syncMode));

/** Starts or ends visual mode. */
export function setVisual(anchor) {
  keys.anchor = anchor;
  syncMode();
}

// A click in a pane focuses that pane.
document.addEventListener(
  'pointerdown',
  (e) => {
    const p = e.target instanceof Element && e.target.closest('[data-pane]')?.getAttribute('data-pane');
    if (p) keys.pane = p;
  },
  true,
);

// --- focus inside a pane or a sheet -----------------------------------------

const focusable = 'a[href], button:not([disabled]), select, input:not([disabled]), textarea:not([disabled]), summary';

export function focusables(root) {
  return root ? /** @type {HTMLElement[]} */ ([...root.querySelectorAll(focusable)]).filter(visible) : [];
}

/** Moves focus n places within root; to the first or last with n = ±Infinity. */
export function moveFocus(root, n) {
  const els = focusables(root);
  if (!els.length) return;
  const i = els.indexOf(/** @type {HTMLElement} */ (document.activeElement));
  let j = n === Infinity ? els.length - 1 : n === -Infinity ? 0 : i < 0 ? (n > 0 ? 0 : els.length - 1) : i + n;
  j = Math.max(0, Math.min(els.length - 1, j));
  els[j].focus();
  els[j].scrollIntoView({ block: 'nearest' });
}

// --- link hints (avy) --------------------------------------------------------

const hintChars = 'asdfghjkl';

function labels(n) {
  if (n <= hintChars.length) return [...hintChars].slice(0, n);
  const out = [];
  for (const a of hintChars) for (const b of hintChars) out.push(a + b);
  return out.slice(0, n);
}

/** Labels everything clickable on screen; typing a label activates it. */
export function startHints() {
  const root = dialog() ?? document.body;
  const els = /** @type {HTMLElement[]} */ ([...root.querySelectorAll(focusable + ', [role="button"], [role="checkbox"]')]).filter((el) => {
    if (!visible(el) || el.closest('[inert], .keys-overlay')) return false;
    const r = el.getBoundingClientRect();
    if (!(r.width > 0 && r.height > 0 && r.bottom > 0 && r.right > 0 && r.top < innerHeight && r.left < innerWidth)) return false;
    // Only what shows: not clipped by a scrolling box, nor under a sheet.
    const x = (Math.max(0, r.left) + Math.min(innerWidth, r.right)) / 2;
    const y = (Math.max(0, r.top) + Math.min(innerHeight, r.bottom)) / 2;
    const top = document.elementFromPoint(x, y);
    return !!top && (top === el || el.contains(top) || top.contains(el));
  });
  if (!els.length) return say('Nothing to click here');
  const ls = labels(els.length);
  keys.hints = {
    typed: '',
    marks: els.map((el, i) => {
      const r = el.getBoundingClientRect();
      return { label: ls[i], el, x: Math.max(0, r.left), y: Math.max(0, r.top) };
    }),
  };
}

function hintKey(e, tok) {
  e.preventDefault();
  const h = keys.hints;
  if (!h) return;
  if (tok === 'ESC' || tok === 'C-g') return void (keys.hints = null);
  if (tok === 'DEL') return void (h.typed = h.typed.slice(0, -1));
  if (tok.length !== 1) return;
  const typed = h.typed + tok.toLowerCase();
  const left = h.marks.filter((m) => m.label.startsWith(typed));
  if (!left.length) return say(`No hint ${typed}`);
  const hit = left.find((m) => m.label === typed);
  if (!hit) return void (h.typed = typed);
  keys.hints = null;
  const el = hit.el;
  if (typing(el)) el.focus();
  else {
    el.focus({ preventScroll: true });
    el.click();
  }
}

// --- the palette (M-x, jumps) ------------------------------------------------

/** Opens the palette on items [{label, detail?, key?, run}]. */
export function openPalette(title, items) {
  reset();
  keys.palette = { title, items, query: '', index: 0 };
}

/** Orderless matching: every word of the query appears, in any order. */
export function matches(items, query) {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  return items.filter((it) => {
    const hay = `${it.label} ${it.detail ?? ''} ${it.key ?? ''}`.toLowerCase();
    return words.every((w) => hay.includes(w));
  });
}
