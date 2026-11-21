// Keyboard driving, in the style of Doom Emacs's evil mode.
//
// Modes: normal (keys are commands), insert (a text field has focus; Escape
// or C-g leaves it) and visual (moving extends the selection). A key
// sequence is a list of tokens such as "SPC", "g", "C-w" or "G"; a sequence
// that is the start of longer bindings waits for the next key and, after a
// moment, shows the which-key popup listing what can follow.
//
// Every action is a named command (command()); a key sequence is bound to a
// command name (bind()). Dispatch, which-key, M-x, SPC SPC, the binding list
// and describe-key/describe-command all read these two tables, so a command
// with no key is still reachable by name. Bindings belong to a pane (side,
// list, detail, or dialog while a sheet is open) or to all of them. The keymap
// itself is in keymap.js.

import { route, pageOpen } from './route.svelte.js';

export const keys = $state({
  mode: 'normal', // normal, insert, visual
  pane: 'list', // the focused pane on a wide screen: side, list or detail
  pending: /** @type {string[]} */ ([]),
  count: '',
  which: false, // the which-key popup
  help: /** @type {false | 'sheet' | 'all' | 'describe'} */ (false), // the cheatsheet, every binding, or the describe sheet
  palette: /** @type {null | {title: string, items: any[], query: string, index: number}} */ (null),
  capture: false, // describe-key: the next sequence is described, not run
  describe: /** @type {null | {keys?: string, cmd?: string}} */ (null), // what the describe sheet shows
  hints: /** @type {null | {typed: string, marks: {label: string, el: HTMLElement, x: number, y: number}[], act?: (el: HTMLElement) => any}} */ (null),
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

/** @typedef {{name: string, desc: string, run: (n: number) => any, pane?: string, when?: () => any, need?: string, palette?: boolean, anywhere?: boolean}} Command */
/** @typedef {{keys: string, seq: string[], cmd: string, desc: string, run: (n: number) => any, pane?: string, when?: () => any, anywhere?: boolean}} Binding */
/** @type {Map<string, Command>} */
export const commands = new Map();
/** @type {Binding[]} */
export const bindings = [];
/** @type {Record<string, string>} prefix -> group name */
export const groups = {};

/** Defines a command. opts: pane (it acts on that pane only), when (it
 * applies only while this is true), need (what it needs, said when it does
 * not apply), palette (false: not offered by name), anywhere (also inside a
 * sheet). */
export function command(name, desc, run, opts = {}) {
  if (commands.has(name)) throw new Error(`command ${name} is defined twice`);
  commands.set(name, { name, desc, run, ...opts });
}

/** Binds a key sequence to a command. opts.pane limits this key to a pane
 * (the command's own pane otherwise). */
export function bind(seq, name, opts = {}) {
  const c = commands.get(name);
  if (!c) throw new Error(`bind ${seq}: no command ${name}`);
  bindings.push({ keys: seq, seq: seq.split(' '), cmd: name, desc: c.desc, run: c.run, when: c.when, pane: opts.pane ?? c.pane, anywhere: opts.anywhere ?? c.anywhere });
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
  if (narrow()) return route.item || pageOpen() ? 'detail' : 'list';
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

/** Every binding that applies now, for the binding list. */
export const activeBindings = () => bindings.filter(active);

/** Whether a command can run here now. */
export const applies = (c) => active(c);

/** The keys that run command name here, or with all = true anywhere. */
export const keysOf = (name, all = false) => bindings.filter((b) => b.cmd === name && (all || active(b))).map((b) => b.keys);

/** The commands offered by name: those that apply here, A to Z, each with
 * the keys that run it. */
export function commandItems() {
  return [...commands.values()]
    .filter((c) => c.palette !== false && applies(c))
    .map((c) => ({ label: c.desc, key: keysOf(c.name).slice(0, 2).join(', '), run: () => c.run(1) }))
    .sort((a, b) => a.label.localeCompare(b.label));
}

/** Runs a command by name, as a key would (for the palette). */
export function runCommand(name, n = 1) {
  const c = commands.get(name);
  if (!c) return say(`No command ${name}`);
  if (!applies(c)) return say(c.need ? `Needs ${c.need}` : `${c.desc}: not here`);
  run(c, n);
}

// --- describe-key and describe-command ----------------------------------------

/** SPC h k: the next key sequence is described instead of run. */
export function describeKey() {
  reset();
  keys.capture = true;
  say('Describe key: press a key sequence (C-g cancels)', 60000);
}

function captureKey(e, tok) {
  e.preventDefault();
  if (tok === 'C-g' && !keys.pending.length) {
    keys.capture = false;
    return say('Cancelled');
  }
  const seq = [...keys.pending, tok];
  if (isPrefix(seq) || (!exact(seq) && bindings.some((b) => b.seq.length > seq.length && starts(b.seq, seq)))) {
    keys.pending = seq;
    return;
  }
  keys.capture = false;
  reset();
  say('');
  keys.describe = { keys: seq.join(' ') };
  keys.help = 'describe';
}

/** Opens the describe sheet on a command. */
export function describeCommand(name) {
  keys.describe = { cmd: name };
  keys.help = 'describe';
}

/** What the describe sheet shows: the command a sequence runs here (or
 * where else it is bound), or a command with its keys and whether it
 * applies here. */
export function description(d) {
  if (!d) return null;
  if (d.keys) {
    const seq = d.keys.split(' ');
    const here = exact(seq);
    const elsewhere = bindings.filter((b) => b.keys === d.keys && b !== here);
    return { keys: d.keys, cmd: here ? commands.get(here.cmd) : null, elsewhere: elsewhere.map((b) => ({ desc: b.desc, cmd: b.cmd, pane: b.pane })) };
  }
  const c = commands.get(d.cmd ?? '');
  if (!c) return null;
  return {
    cmd: c,
    here: keysOf(c.name),
    all: bindings.filter((b) => b.cmd === c.name).map((b) => ({ keys: b.keys, pane: b.pane })),
    applies: applies(c),
  };
}

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
  if (keys.capture) return captureKey(e, tok);
  if (keys.hints) return hintKey(e, tok);
  if (keys.help) {
    if (['ESC', 'C-g', 'q', '?'].includes(tok)) keys.help = false;
    else if (keys.help === 'describe') {
      // Only the keys above close it.
    }
    else if (tok === 'TAB') keys.help = keys.help === 'all' ? 'sheet' : 'all';
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

/** Insert mode: the field keeps its keys, except those that leave it.
 * Sending is the composer's own ⌘/Ctrl+Enter. */
function insertKey(e, tok, el) {
  if (tok === 'ESC' || tok === 'C-g' || tok === 'C-[') {
    e.preventDefault();
    el.blur();
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

/** Labels everything clickable on screen (or only what matches the CSS
 * selector only); typing a label activates it, or runs act on it when given
 * (none: what to say when nothing matches). Returns how many it labelled. */
export function startHints(only = '', { act = null, none = 'Nothing to click here' } = {}) {
  const root = dialog() ?? document.body;
  const els = /** @type {HTMLElement[]} */ ([...root.querySelectorAll(only || focusable + ', [role="button"], [role="checkbox"]')]).filter((el) => {
    if (!visible(el) || el.closest('[inert], .keys-overlay')) return false;
    const r = el.getBoundingClientRect();
    if (!(r.width > 0 && r.height > 0 && r.bottom > 0 && r.right > 0 && r.top < innerHeight && r.left < innerWidth)) return false;
    // Only what shows: not clipped by a scrolling box, nor under a sheet.
    const x = (Math.max(0, r.left) + Math.min(innerWidth, r.right)) / 2;
    const y = (Math.max(0, r.top) + Math.min(innerHeight, r.bottom)) / 2;
    const top = document.elementFromPoint(x, y);
    return !!top && (top === el || el.contains(top) || top.contains(el));
  });
  if (!els.length) {
    say(none);
    return 0;
  }
  if (only && els.length === 1) {
    if (act) act(els[0]);
    else els[0].click();
    return 1;
  }
  const ls = labels(els.length);
  keys.hints = {
    act,
    typed: '',
    marks: els.map((el, i) => {
      const r = el.getBoundingClientRect();
      return { label: ls[i], el, x: Math.max(0, r.left), y: Math.max(0, r.top) };
    }),
  };
  return els.length;
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
  if (h.act) return void h.act(el);
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
