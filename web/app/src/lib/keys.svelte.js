// Keyboard driving, in the style of Doom Emacs's evil mode.
//
// Modes: normal (keys are commands), insert (a text field has focus; Escape
// or C-g leaves it) and visual (moving extends the selection). A key
// sequence is a list of tokens such as "SPC", "g", "C-w" or "G"; a sequence
// that is the start of longer bindings waits for the next key and, after a
// moment, shows the which-key popup listing what can follow.
//
// Every action is a named command (command(), in keymap.js); keys are bound
// to command names in keymap files: keys.edn, built in, and the user's file
// over it (keyfile.js reads and checks them; docs/keyboard.md). Dispatch,
// which-key, M-x, SPC SPC, the binding list, tooltips and
// describe-key/describe-command all read the same effective map, so a
// command with no key is still reachable by name.

import { route, pageOpen } from './route.svelte.js';
import { rank, narrow as narrowItems, load as loadHistory, save as saveHistory, record, step } from './mx.js';
import { readKeyFile, buildKeymap, effective as effectiveOf, lookup, allBindings, primaryKey, KeymapError } from './keyfile.js';
import { get } from './api.js';
import BASE from './keys.edn?raw';

export const keys = $state({
  mode: 'normal', // normal, insert, visual
  pane: 'list', // the focused pane on a wide screen: side, list or detail
  pending: /** @type {string[]} */ ([]),
  count: '',
  which: false, // the which-key popup
  help: /** @type {false | 'sheet' | 'all' | 'describe'} */ (false), // the cheatsheet, every binding, or the describe sheet
  // history: M-x's (ranked, and M-p/M-n step through what was typed); at,
  // draft: where M-p/M-n stand; back: the field M-x was opened from.
  palette: /** @type {null | {title: string, items: any[], query: string, index: number, history: boolean, at: number, draft: string, back: HTMLElement | null}} */ (null),
  capture: false, // describe-key: the next sequence is described, not run
  describe: /** @type {null | {keys?: string, cmd?: string}} */ (null), // what the describe sheet shows
  hints: /** @type {null | {typed: string, marks: {label: string, el: HTMLElement, x: number, y: number}[], act?: (el: HTMLElement) => any}} */ (null),
  anchor: '', // visual mode's first row
  used: false, // the keyboard has driven the page: show which pane has focus
  message: '',
  tick: 0, // bumped by every key, so derived labels recompute
});

/** Whether key UI is up (hints, the palette, the guide, describe-key, a
 * sequence half typed). It takes Escape before the sheet under it does, so
 * Escape closes it and leaves the sheet open. */
export const keyUiOpen = () => !!(keys.hints || keys.palette || keys.help || keys.capture || keys.pending.length);

/** What the keymap needs from the app. App.svelte fills it in. */
export const ctx = {
  refresh() {},
  /** @returns {any} */ item: () => null,
  /** @returns {any} */ view: () => null,
  /** @returns {any[]} */ runs: () => [],
  /** The context map for what is open (task, settings, ...), or ''. */
  context: () => '',
};

/** @typedef {{name: string, desc: string, run: (n: number) => any, pane?: string, when?: () => any, need?: string, palette?: boolean, anywhere?: boolean}} Command */
/** @typedef {import('./keyfile.js').Binding} Binding */
/** @type {Map<string, Command>} */
export const commands = new Map();

/** Defines a command. opts: pane (it acts on that pane only), when (it
 * applies only while this is true), need (what it needs, said when it does
 * not apply), palette (false: not offered by name), anywhere (also inside a
 * sheet). */
export function command(name, desc, run, opts = {}) {
  if (commands.has(name)) throw new Error(`command ${name} is defined twice`);
  commands.set(name, { name, desc, run, ...opts });
}

// --- the keymap ----------------------------------------------------------------

/** How the keymap stands: version (bumped on every rebuild, so what shows
 * keys recomputes), the user's file (path, whether it is applied) and why
 * it was not (error). */
export const keymap = $state({ version: 0, path: '', applied: false, error: '', dismissed: '' });
/** @type {import('./keyfile.js').Keymap} */
let km = null;
/** @type {Binding[]} */
let all = [];
let effs = new Map();
let primary = new Map();

/** Builds the keymap from keys.edn and the user's file (text null: none).
 * A user file with any error is left out whole, and keymap.error names the
 * entry and its line. Run once the commands are defined. */
export function setKeymap(text = null, path = '', failed = '') {
  const base = readKeyFile(BASE); // a broken built-in file is a bug: it throws
  let built = null;
  let error = failed;
  if (text != null && !error) {
    try {
      built = buildKeymap(base, readKeyFile(text), commands);
    } catch (err) {
      if (!(err instanceof KeymapError)) throw err;
      error = err.entry ? `${err.entry}${lineOf(text, err.entry)}: ${err.reason}` : err.message;
    }
  }
  km = built ?? buildKeymap(base, null, commands);
  all = allBindings(km);
  effs = new Map();
  primary = new Map();
  reset();
  Object.assign(keymap, { version: km.version, path, applied: !!built, error });
}

/** " (line N)" for the first line naming the entry's chord, else "". */
function lineOf(text, entry) {
  const chord = /"(?:[^"\\]|\\.)*"$/.exec(entry ?? '')?.[0];
  const map = /^:[a-z-]+/.exec(entry ?? '')?.[0];
  const needle = chord ?? map;
  if (!needle) return '';
  const i = text.split('\n').findIndex((l) => l.includes(needle));
  return i < 0 ? '' : ` (line ${i + 1})`;
}

/** Reads the user's keys file from the server and applies it. */
export async function loadUserKeys(announce = false) {
  let r;
  try {
    r = await get('keys');
  } catch {
    return; // offline: keep what is in use
  }
  const before = keymap.applied;
  if (!r.exists) setKeymap(null, r.path);
  else setKeymap(r.text ?? '', r.path, r.error ? `${r.error}` : '');
  if (announce && keymap.applied) say('Your keys file is applied');
  else if (announce && before && !r.exists) say('Your keys file is gone: the built-in keys are in use');
}

/** The state the effective map depends on. */
function now(insert = false) {
  const p = pane();
  return { insert, dialog: p === 'dialog', pane: p, context: p === 'dialog' || insert ? '' : ctx.context() };
}
/** The effective map for state (keyfile.js), cached per state. */
function eff(state = now()) {
  const k = `${state.insert}|${state.dialog}|${state.pane}|${state.context}`;
  let e = effs.get(k);
  if (!e) effs.set(k, (e = effectiveOf(km, state)));
  return e;
}
// The effective map a sequence started with: a focus change halfway through
// a chord does not change what it means.
let frozen = null;
const current = () => frozen ?? eff();

/** The key a tooltip names for a command ('' for none). */
export function keyFor(name) {
  keymap.version;
  if (!km) return '';
  if (!primary.has(name)) primary.set(name, primaryKey(km, name, all));
  return primary.get(name);
}
/** keyFor within one map; prefer: a chord to name instead while it is still
 * bound to the command there. */
export function keyIn(name, map = '', prefer = '') {
  keymap.version;
  const bs = all.filter((b) => b.cmd === name && (!map || b.map === map));
  if (prefer && bs.some((b) => b.keys === prefer)) return prefer;
  return bs.sort((a, b) => a.seq.length - b.seq.length)[0]?.keys ?? '';
}
/** The keys that start the context maps (:localleader). */
export const localleaders = () => (keymap.version, km?.localleader ?? []);
/** Whether a command is left without a key on purpose (:keyless). */
export const keyless = (name) => (keymap.version, !!km?.keyless.has(name));

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

// The characters of the keys Option changes, unshifted and shifted (US layout).
const ALT_CODES = {
  Quote: ["'", '"'], Semicolon: [';', ':'], Comma: [',', '<'], Period: ['.', '>'], Slash: ['/', '?'],
  Backslash: ['\\', '|'], BracketLeft: ['[', '{'], BracketRight: [']', '}'], Minus: ['-', '_'],
  Equal: ['=', '+'], Backquote: ['`', '~'],
  Digit1: ['1', '!'], Digit2: ['2', '@'], Digit3: ['3', '#'], Digit4: ['4', '$'], Digit5: ['5', '%'],
  Digit6: ['6', '^'], Digit7: ['7', '&'], Digit8: ['8', '*'], Digit9: ['9', '('], Digit0: ['0', ')'],
};
/** @param {KeyboardEvent} e */
function altChar(e) {
  if (/^Key[A-Z]$/.test(e.code)) return e.shiftKey ? e.code.slice(3) : e.code.slice(3).toLowerCase();
  return ALT_CODES[e.code]?.[e.shiftKey ? 1 : 0];
}

/** The token for a keydown, or '' for a lone modifier. Command (s-) combos
 * are left to the browser. */
export function token(e) {
  let k = e.key;
  if (['Shift', 'Control', 'Alt', 'Meta', 'CapsLock'].includes(k)) return '';
  // On a Mac, Option changes the character or makes it a dead key (M-e,
  // M-u); read the key itself, as on a US layout.
  if (e.altKey) k = altChar(e) ?? k;
  if (k === 'Dead' || k === 'Unidentified') return '';
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

/** The visible element tagged data-cmd=name, preferring the focused card,
 * then the current pane. */
export function cmdEl(name) {
  const all = [...document.querySelectorAll(`[data-cmd="${name}"]`)].filter(visible);
  const inPane = paneEl();
  // A card tagged data-scope (an out-of-quota agent) holding focus: its own button first.
  const scope = document.activeElement?.closest?.('[data-scope]');
  return /** @type {HTMLElement | undefined} */ (all.find((el) => scope?.contains(el)) ?? all.find((el) => inPane?.contains(el)) ?? all[0]);
}

export function say(text, ms = 2200) {
  keys.message = text;
  clearTimeout(sayTimer);
  sayTimer = setTimeout(() => (keys.message = ''), ms);
}
let sayTimer;

// A command applies in a pane (its own, if it names one; in a sheet only if
// it works anywhere) while its condition holds.
const active = (c) => (c.pane ? c.pane === pane() : pane() !== 'dialog' || c.anywhere) && (!c.when || c.when());
const starts = (seq, pre) => pre.length < seq.length && pre.every((t, i) => seq[i] === t);
const cmdOf = (b) => commands.get(b.cmd);

/** What may follow prefix, for which-key: [{key, desc, group}], leaving out
 * commands that do not apply here. */
export function continuations(prefix) {
  const e = current();
  const out = new Map();
  for (const b of e.binds) {
    if (!starts(b.seq, prefix) || !active(cmdOf(b))) continue;
    const k = b.seq[prefix.length];
    if (b.seq.length === prefix.length + 1) {
      if (!out.has(k) || out.get(k).group) out.set(k, { key: k, desc: cmdOf(b).desc, group: false });
    } else if (!out.has(k)) {
      out.set(k, { key: k, desc: '+' + (e.labels.get([...prefix, k].join(' ')) ?? 'prefix'), group: true });
    }
  }
  return [...out.values()].sort((a, b) => sortKey(a.key).localeCompare(sortKey(b.key)));
}

/** A prefix's label in the key guide ('' for none). */
export const labelOf = (prefix) => (keymap.version, current().labels.get(prefix) ?? '');
// Letters before punctuation, lower case before upper, as which-key does.
const sortKey = (k) => (/^[a-z]$/.test(k) ? '0' + k : /^[A-Z]$/.test(k) ? '1' + k.toLowerCase() + k : '2' + k);

/** Every binding that applies now, for the binding list: {keys, seq, cmd,
 * map, desc}. */
export const activeBindings = () => eff().binds.filter((b) => active(cmdOf(b))).map((b) => ({ ...b, desc: cmdOf(b).desc }));

/** Whether a command can run here now. */
export const applies = (c) => active(c);

/** The keys that run command name here, or with anywhere = true in any map. */
export const keysOf = (name, anywhere = false) => (anywhere ? all : eff().binds).filter((b) => b.cmd === name).map((b) => b.keys);

/** The commands offered by name: those that apply here, each with the keys
 * that run it (M-x ranks them; see mx.js). */
export function commandItems() {
  return [...commands.values()]
    .filter((c) => c.palette !== false && applies(c))
    .map((c) => {
      const ks = keysOf(c.name).slice(0, 2);
      return { name: c.name, label: c.desc, key: ks.join(' '), keys: ks, run: () => c.run(1) };
    });
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
  const ef = current();
  const { hit, more } = lookup(ef, seq);
  // Keep reading while the keys start a binding here, or one elsewhere.
  if (more || (!hit && all.some((b) => starts(b.seq, seq)))) {
    frozen = ef;
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
  keymap.version;
  if (d.keys) {
    const here = lookup(eff(), d.keys.split(' ')).hit;
    const elsewhere = all.filter((b) => b.keys === d.keys && !(here && b.map === here.map));
    return { keys: d.keys, cmd: here ? cmdOf(here) : null, elsewhere: elsewhere.map((b) => ({ desc: cmdOf(b).desc, cmd: b.cmd, map: b.map })) };
  }
  const c = commands.get(d.cmd ?? '');
  if (!c) return null;
  return {
    cmd: c,
    here: keysOf(c.name),
    all: all.filter((b) => b.cmd === c.name).map((b) => ({ keys: b.keys, map: b.map })),
    applies: applies(c),
    keyless: km?.keyless.has(c.name) ?? false,
  };
}

// --- dispatch ----------------------------------------------------------------

let whichTimer;
function reset() {
  frozen = null;
  keys.pending = [];
  keys.count = '';
  keys.which = false;
  clearTimeout(whichTimer);
}

/** Runs the command a binding names, or says why it does not apply here. */
function fire(b, n) {
  const c = cmdOf(b);
  if (!active(c)) {
    reset();
    return say(c.need ? `Needs ${c.need}` : `${c.desc}: not here`);
  }
  run(c, n);
}

function run(c, n) {
  reset();
  try {
    const r = c.run(n);
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
  // A focused radio keeps its arrows: they pick the next option of its group
  // (in a sheet, <up> and <down> would only move the focus).
  if (!keys.pending.length && el instanceof HTMLInputElement && el.type === 'radio' && /^<(up|down|left|right)>$/.test(tok)) return;
  if (!keys.pending.length && /^[0-9]$/.test(tok) && (tok !== '0' || keys.count)) {
    keys.count += tok;
    e.preventDefault();
    return;
  }
  const seq = [...keys.pending, tok];
  const ef = current();
  const { hit, more } = lookup(ef, seq);
  if (hit && !more) {
    e.preventDefault();
    keys.used = true;
    fire(hit, Math.max(1, parseInt(keys.count || '1', 10)));
    return;
  }
  // The localleader always starts a sequence, even with nothing open that has
  // keys of its own: its next key must not fall through to :normal (, t
  // would take a question's recommendation).
  const local = localleaders().some((l) => {
    const ks = l.split(' ');
    return seq.length <= ks.length && seq.every((t, i) => ks[i] === t);
  });
  if (more || local) {
    e.preventDefault();
    keys.used = true;
    frozen = ef;
    keys.pending = seq;
    // Say at once when nothing it leads to can run here.
    if (!continuations(seq).length) say(`${seq.join(' ')}: ${more ? 'none of its keys apply here now' : 'nothing open here has keys of its own'}`, 4000);
    clearTimeout(whichTimer);
    whichTimer = setTimeout(() => (keys.which = true), 300);
    return;
  }
  if (keys.pending.length || keys.count) {
    e.preventDefault();
    const ll = localleaders().find((l) => starts(seq, l.split(' ')));
    say(ll && !ctx.context() ? `${seq.join(' ')}: nothing open here has keys of its own` : `${seq.join(' ')} is undefined`);
  }
  reset();
}

/** Insert mode: the field keeps its keys, except those that leave it and
 * the :insert map's chords (M-x: cancelling it comes back to this field).
 * Sending is the composer's own ⌘/Ctrl+Enter. */
function insertKey(e, tok, el) {
  if (tok === 'ESC' || tok === 'C-g' || tok === 'C-[') {
    e.preventDefault();
    reset();
    return leaveField(el);
  }
  const seq = [...keys.pending, tok];
  const ef = frozen ?? eff(now(true));
  const { hit, more } = lookup(ef, seq);
  if (more) {
    e.preventDefault();
    frozen = ef;
    keys.pending = seq;
    clearTimeout(whichTimer);
    whichTimer = setTimeout(() => (keys.which = true), 300);
  } else if (hit) {
    e.preventDefault();
    fire(hit, 1);
  } else if (keys.pending.length) {
    e.preventDefault();
    say(`${seq.join(' ')} is undefined`);
    reset();
  }
}

// The selected field: the one Escape left. It stays outlined in normal mode,
// and `i` goes back into it, until another field takes focus, Escape backs
// out of it, or it leaves the page.
let field = /** @type {HTMLElement | null} */ (null);

/** Marks el as the selected field (null: none). */
export function selectField(el) {
  if (field === el) return;
  field?.classList.remove('kb-field');
  field = el;
  el?.classList.add('kb-field');
}

/** The selected field, while it is on screen and enabled; null otherwise. */
export function selectedField() {
  if (field && !field.isConnected) selectField(null);
  return field && visible(field) && !field.matches(':disabled') ? field : null;
}

/** Leaves insert mode and keeps el selected. to: what takes focus instead
 * (by default nothing does). */
export function leaveField(el, to = /** @type {HTMLElement | null} */ (null)) {
  if (to) to.focus();
  else el.blur();
  selectField(el);
  syncMode();
}

// Mode follows focus.
export function syncMode() {
  keys.mode = typing(document.activeElement) ? 'insert' : keys.anchor ? 'visual' : 'normal';
}
// A focus change cancels a sequence halfway through.
document.addEventListener('focusin', () => {
  if (typing(document.activeElement)) selectField(null);
  if (keys.pending.length) reset();
  syncMode();
});
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

/** Opens the palette on items [{label, name?, detail?, key?, run}].
 * opts.history: M-x's ordering and input history (mx.js). The field typing
 * was in, if any, gets focus back when the palette is cancelled. */
export function openPalette(title, items, opts = {}) {
  reset();
  const el = document.activeElement;
  const back = el instanceof HTMLElement && typing(el) ? el : null;
  const history = !!opts.history;
  keys.palette = { title, items: history ? rank(items, loadHistory()) : items, query: '', index: 0, history, at: -1, draft: '', back };
}

/** What the palette shows for its query: orderless matches; M-x puts the
 * shorter ones first, other pickers keep their own order. */
export function matches(p) {
  const out = narrowItems(p.items, p.query);
  return p.history ? out : p.items.filter((it) => out.includes(it));
}

/** Closes the palette; picked: the item chosen, run after it closes. */
export function closePalette(picked = null) {
  const p = keys.palette;
  if (!p) return;
  keys.palette = null;
  if (picked) {
    if (p.history) saveHistory(record(loadHistory(), picked.name ?? '', p.query));
    if (p.back) selectField(p.back); // `i` goes back into it
    queueMicrotask(() => picked.run());
  } else if (p.back?.isConnected) {
    p.back.focus();
    syncMode();
  }
}

/** M-p (by 1) and M-n (by -1): the palette's input history. */
export function paletteHistory(by) {
  const p = keys.palette;
  if (!p?.history) return;
  const inputs = loadHistory().inputs;
  if (p.at < 0) p.draft = p.query;
  const s = step(inputs, p.at, by, p.draft);
  if (s.at === p.at) return say(by > 0 ? 'No older input' : 'No newer input');
  p.at = s.at;
  p.query = s.text;
  p.index = 0;
}
