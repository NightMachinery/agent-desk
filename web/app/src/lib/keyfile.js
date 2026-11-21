// Keymap files: the shared EDN format of the evilify skill
// (evilify/keymap-edn.md), read, checked and layered. keys.edn holds the
// built-in keys; the user's file (served by api/keys) overlays it. This
// module knows nothing of the page: keys.svelte.js dispatches from what it
// builds, and the tests run it under node.
//
// Terms (as in the shared spec):
// - named map: one table of bindings, such as :normal or :list;
// - chord: a key sequence such as "SPC b p"; each part is a stroke;
// - effective map: the bindings that apply at one moment, after overlay
//   and precedence; dispatch, which-key and help all read it.
//
// agent-desk's named maps, most specific first (only some are active at
// once; see effective()):
// - :dialog, while a sheet is open (then it is the only one);
// - a context map for what is open (:task, :settings, ...): its chords
//   follow the localleader, so "s" in :task is ", s" and "SPC m s";
// - the focused pane's: :list, :detail or :side;
// - :normal;
// - :insert, inside text boxes (then it is the only one).
//
// Extensions beyond the portable subset: a string labels a prefix in the
// key guide; :localleader lists the chords that start the context maps;
// :keyless lists commands left without a key on purpose; an uppercase
// letter is an alias for S- and the letter ("G" for "S-g"); Purple's names
// for keys (Up, PgUp, F1, ...) are aliases for Doom's.

import { parseEDNString } from 'edn-data';

export const PANES = ['list', 'detail', 'side'];
export const MAPS = ['normal', ...PANES, 'dialog', 'insert'];
export const CONTEXTS = ['task', 'deliverable', 'screen', 'message', 'quota', 'settings', 'verbatim'];
export const MAX_BYTES = 64 * 1024;
const MAX_STROKES = 4;

export class KeymapError extends Error {
  /** reason: what is wrong; entry: where, such as `:normal "g c"`. */
  constructor(reason, entry = '') {
    super(entry ? `${entry}: ${reason}` : reason);
    this.reason = reason;
    this.entry = entry;
  }
}

// --- chords ------------------------------------------------------------------

// Named keys, case-insensitive, to the spelling keys.svelte.js's token()
// gives a keydown (Doom's).
const NAMED = new Map(
  Object.entries({
    spc: 'SPC', ret: 'RET', esc: 'ESC', tab: 'TAB',
    del: 'DEL', backspace: 'DEL', '<delete>': '<delete>', delete: '<delete>',
    '<up>': '<up>', up: '<up>', '<down>': '<down>', down: '<down>',
    '<left>': '<left>', left: '<left>', '<right>': '<right>', right: '<right>',
    '<home>': '<home>', home: '<home>', '<end>': '<end>', end: '<end>',
    '<prior>': '<prior>', pgup: '<prior>', '<next>': '<next>', pgdown: '<next>',
  }),
);
const FKEY = /^<?f([1-9]|[12][0-9]|3[0-5])>?$/i;
const MODS = { C: 'c', M: 'm', S: 's', s: 'super', ctrl: 'c', alt: 'm', shift: 's', super: 'super' };

/** One stroke in its canonical spelling: modifiers in the order C- M- s-
 * S-, a shifted letter as the capital ("S-g" and "G" are "G"), named keys
 * as Doom writes them. Throws a KeymapError when it does not parse. */
export function normalizeStroke(raw) {
  const mods = { c: false, m: false, s: false, super: false };
  let rest = raw;
  for (;;) {
    const m = /^([CMSs])-(.+)$/.exec(rest) ?? /^(ctrl|alt|shift|super)\+(.+)$/i.exec(rest);
    if (!m) break;
    const mod = MODS[m[1].length > 1 ? m[1].toLowerCase() : m[1]];
    if (mods[mod]) throw new KeymapError(`"${raw}" names a modifier twice`);
    mods[mod] = true;
    rest = m[2];
  }
  let key = NAMED.get(rest.toLowerCase());
  const f = !key && rest.length > 1 && FKEY.exec(rest);
  if (f) key = `<f${f[1]}>`;
  const named = !!key;
  if (!key) {
    if ([...rest].length !== 1) throw new KeymapError(`"${raw}": unknown key "${rest}"`);
    key = rest;
    if (/^[A-Z]$/.test(key)) {
      mods.s = true;
      key = key.toLowerCase();
    }
    if (mods.s) {
      if (!/^[a-z]$/.test(key)) throw new KeymapError(`"${raw}": write the character Shift types, not S-`);
      key = key.toUpperCase();
    }
  }
  if (named && mods.s && key === 'SPC') throw new KeymapError(`"${raw}": Shift with Space types a space`);
  return (mods.c ? 'C-' : '') + (mods.m ? 'M-' : '') + (mods.super ? 's-' : '') + (named && mods.s ? 'S-' : '') + key;
}

/** A chord's strokes, each canonical. Strokes are separated by single spaces. */
export function normalizeChord(raw) {
  if (typeof raw !== 'string' || raw === '') throw new KeymapError('a chord must be a non-empty string');
  const parts = raw.split(' ');
  if (parts.some((p) => p === '')) throw new KeymapError(`"${raw}": separate strokes with single spaces`);
  if (parts.length > MAX_STROKES) throw new KeymapError(`"${raw}": more than ${MAX_STROKES} strokes`);
  return parts.map(normalizeStroke);
}

// --- reading a file ----------------------------------------------------------

/** @type {import('edn-data/dist/parse').ParseOptions} */
const OPTS = {
  mapAs: 'doubleArray',
  keywordAs: 'object',
  listAs: 'array',
  setAs: 'array',
  // Tagged literals are not evaluated; their values are then rejected.
  tagHandlers: { inst: (val) => ({ tag: 'inst', val }), uuid: (val) => ({ tag: 'uuid', val }) },
};
const END = 'agent-desk/end';
const isKw = (v) => !!v && typeof v === 'object' && typeof v.key === 'string' && Object.keys(v).length === 1;
const isMap = (v) => !!v && typeof v === 'object' && Array.isArray(v.map);
const show = (v) => (isKw(v) ? `:${v.key}` : typeof v === 'string' ? JSON.stringify(v) : v === null ? 'nil' : JSON.stringify(v));

/** @typedef {{cmd: string} | {label: string} | null} Value */
/** @typedef {{chord: string, raw: string, value: Value}} Entry */
/** @typedef {{maps: Map<string, Entry[]>, localleader?: string[], keyless?: string[]}} KeyFile */

/** Reads a keymap file into its named maps, each entry's chord canonical.
 * Throws a KeymapError naming the first problem. */
export function readKeyFile(text) {
  if (typeof text !== 'string') throw new KeymapError('no text');
  if (new TextEncoder().encode(text).length > MAX_BYTES) throw new KeymapError(`the file is larger than ${MAX_BYTES / 1024} KiB`);
  let forms;
  try {
    // Read as one vector ending in a sentinel: a second top-level form shows
    // as a second element, and a stray closing bracket loses the sentinel.
    forms = parseEDNString(`[${text}\n:${END}]`, OPTS);
  } catch (err) {
    // The reader fails on a stray closing bracket with a TypeError of its own.
    const why = err instanceof TypeError ? 'a closing bracket without its opening one' : (err?.message ?? String(err));
    throw new KeymapError(`not valid EDN: ${why}`);
  }
  if (!Array.isArray(forms) || !isKw(forms.at(-1)) || forms.at(-1).key !== END)
    throw new KeymapError('not valid EDN: a closing bracket without its opening one, or a #_ with nothing after it');
  forms = forms.slice(0, -1);
  if (forms.length === 0) throw new KeymapError('the file is empty: write {} for no changes');
  if (forms.length > 1 || !isMap(forms[0])) throw new KeymapError('the file must hold exactly one map, {...}');

  /** @type {KeyFile} */
  const out = { maps: new Map() };
  const seen = new Set();
  for (const [k, v] of forms[0].map) {
    if (!isKw(k)) throw new KeymapError(`${show(k)} is not a keyword: name a map like :normal`);
    const name = k.key;
    const where = `:${name}`;
    if (seen.has(name)) throw new KeymapError('appears twice', where);
    seen.add(name);
    if (name === 'localleader') {
      if (!Array.isArray(v)) throw new KeymapError('must be a vector of chords, like ["," "SPC m"]', where);
      out.localleader = v.map((c) => {
        try {
          return normalizeChord(c).join(' ');
        } catch (err) {
          throw new KeymapError(err.reason ?? String(err), where);
        }
      });
    } else if (name === 'keyless') {
      if (!Array.isArray(v) || !v.every(isKw)) throw new KeymapError('must be a vector of command keywords', where);
      out.keyless = v.map((c) => c.key);
    } else if (MAPS.includes(name) || CONTEXTS.includes(name)) {
      if (!isMap(v)) throw new KeymapError('must be a map from chords to commands', where);
      out.maps.set(name, readBindings(v, where));
    } else {
      throw new KeymapError(`unknown map (known: ${[...MAPS, ...CONTEXTS, 'localleader', 'keyless'].map((m) => ':' + m).join(' ')})`, where);
    }
  }
  return out;
}

function readBindings(v, where) {
  /** @type {Entry[]} */
  const out = [];
  const raws = new Map();
  const chords = new Map();
  for (const [k, val] of v.map) {
    const entry = `${where} ${show(k)}`;
    if (typeof k !== 'string') throw new KeymapError('a chord must be a string, like "g c"', entry);
    if (raws.has(k)) throw new KeymapError('appears twice', entry);
    raws.set(k, true);
    let chord;
    try {
      chord = normalizeChord(k).join(' ');
    } catch (err) {
      throw new KeymapError(err.reason ?? String(err), entry);
    }
    if (chords.has(chord)) throw new KeymapError(`the same chord as ${JSON.stringify(chords.get(chord))}`, entry);
    chords.set(chord, k);
    /** @type {Value} */
    let value;
    if (val === null) value = null;
    else if (isKw(val)) {
      if (val.key.includes('/')) throw new KeymapError(`${show(val)}: write the command without a namespace`, entry);
      value = { cmd: val.key };
    } else if (typeof val === 'string') {
      if (!val.trim()) throw new KeymapError('an empty label', entry);
      value = { label: val };
    } else throw new KeymapError(`${show(val)}: a binding is a command keyword, nil, or a label string`, entry);
    out.push({ chord, raw: k, value });
  }
  return out;
}

// --- building the keymap -----------------------------------------------------

/** @typedef {{pane?: string, anywhere?: boolean}} CommandInfo */
/** @typedef {{binds: Map<string, string>, labels: Map<string, string>}} NamedMap */
/** @typedef {{maps: Map<string, NamedMap>, localleader: string[], keyless: Set<string>, version: number}} Keymap */

let version = 0;
const starts = (seq, pre) => pre.length < seq.length && pre.every((t, i) => seq[i] === t);
const split = (chord) => chord.split(' ');
const entryOf = (map, chord) => `:${map} ${JSON.stringify(chord)}`;

/** The keymap from the built-in file and the user's (null: none), checked
 * against the commands (name -> {pane, anywhere}). Throws a KeymapError
 * when either is wrong; the caller then builds it without the user's. */
export function buildKeymap(base, user, commands) {
  /** @type {Keymap} */
  const km = { maps: new Map(), localleader: [], keyless: new Set(), version: ++version };
  for (const name of [...MAPS, ...CONTEXTS]) km.maps.set(name, { binds: new Map(), labels: new Map() });
  layer(km, base, commands, false);
  if (user) layer(km, user, commands, true);
  check(km, commands, !!user);
  return km;
}

function layer(km, file, commands, isUser) {
  if (file.localleader) km.localleader = file.localleader;
  for (const c of file.keyless ?? []) {
    if (!commands.has(c)) throw new KeymapError(`no command :${c}`, ':keyless');
    km.keyless.add(c);
  }
  for (const [name, entries] of file.maps) {
    const m = km.maps.get(name);
    for (const { chord, raw, value } of entries) {
      const entry = `:${name} ${JSON.stringify(raw)}`;
      if (isUser) reserved(name, chord, value, entry);
      if (value === null) {
        m.binds.delete(chord);
        m.labels.delete(chord);
      } else if ('cmd' in value) {
        const c = commands.get(value.cmd);
        if (!c) throw new KeymapError(`no command :${value.cmd}; SPC h f lists the commands`, entry);
        if (PANES.includes(name) && c.pane && c.pane !== name) throw new KeymapError(`:${value.cmd} works only in the ${c.pane} pane`, entry);
        if (CONTEXTS.includes(name) && c.pane && c.pane !== 'dialog') throw new KeymapError(`:${value.cmd} works only in the ${c.pane} pane, so not after the localleader`, entry);
        if (name === 'dialog' && c.pane !== 'dialog' && !c.anywhere) throw new KeymapError(`:${value.cmd} does not work in a sheet`, entry);
        if (name !== 'dialog' && c.pane === 'dialog') throw new KeymapError(`:${value.cmd} works only in a sheet (:dialog)`, entry);
        m.labels.delete(chord);
        m.binds.set(chord, value.cmd);
      } else {
        m.labels.set(chord, value.label);
      }
    }
  }
}

/** What a user's file may not take: the keys that back out of everything,
 * the counts, Command (s-) chords, which the browser keeps, and in a text
 * box anything that would type. */
function reserved(name, chord, value, entry) {
  const seq = split(chord);
  const first = seq[0];
  if (seq.some((s) => /(^|-)s-/.test(s))) throw new KeymapError('Command (s-) keys belong to the browser', entry);
  if (first === 'ESC' || first === 'C-g') throw new KeymapError(`${first} always backs out; it cannot be rebound`, entry);
  if (name === 'insert') {
    if (first === 'C-[') throw new KeymapError('C-[ always leaves the text box', entry);
    if (value && 'cmd' in value && !/^(C-|M-)/.test(first)) throw new KeymapError('in a text box a chord must start with C- or M-, or it would type', entry);
  } else if (!CONTEXTS.includes(name) && /^[1-9]$/.test(first)) {
    throw new KeymapError('digits start a count', entry);
  }
}

function check(km, commands, hasUser) {
  for (const [name, m] of km.maps) {
    const chords = [...m.binds.keys()].map(split);
    for (const a of chords)
      for (const b of chords)
        if (starts(b, a))
          throw new KeymapError(`runs a command and also starts ${JSON.stringify(b.join(' '))}`, entryOf(name, a.join(' ')));
    for (const [chord, label] of m.labels) {
      if (m.binds.has(chord)) throw new KeymapError(`a label (${JSON.stringify(label)}) on a chord that runs a command`, entryOf(name, chord));
      const seq = split(chord);
      const under = (mm) => [...mm.binds.keys()].some((c) => starts(split(c), seq));
      const lower = PANES.includes(name) ? [m, km.maps.get('normal')] : [m];
      if (!lower.some(under)) throw new KeymapError(`a label (${JSON.stringify(label)}) on a chord that starts nothing`, entryOf(name, chord));
    }
    if (name === 'insert')
      for (const c of m.binds.keys())
        if (!/^(C-|M-|s-)/.test(split(c)[0])) throw new KeymapError('in a text box a chord must start with C- or M-', entryOf(name, c));
  }
  const mx = [...km.maps.get('normal').binds.values()].includes('m-x');
  if (!mx) throw new KeymapError(hasUser ? 'the last key for :m-x in :normal was removed; commands need a way in' : 'no key for :m-x in :normal');
}

// --- the effective map -------------------------------------------------------

/** @typedef {{keys: string, seq: string[], cmd: string, map: string}} Binding */

/** Every binding in the keymap, context maps under each localleader. */
export function allBindings(km) {
  /** @type {Binding[]} */
  const out = [];
  for (const [map, m] of km.maps) {
    const ctx = CONTEXTS.includes(map);
    for (const [chord, cmd] of m.binds) {
      if (!ctx) out.push({ keys: chord, seq: split(chord), cmd, map });
      else for (const l of km.localleader) out.push({ keys: `${l} ${chord}`, seq: split(`${l} ${chord}`), cmd, map });
    }
  }
  return out;
}

/** The maps active in state ({insert, dialog, pane, context}), most
 * specific first. */
export function activeMaps(km, { insert = false, dialog = false, pane = 'list', context = '' } = {}) {
  if (insert) return ['insert'];
  if (dialog) return ['dialog'];
  const out = [];
  if (context && km.maps.get(context)?.binds.size && km.localleader.length) out.push(context);
  out.push(pane, 'normal');
  return out;
}

/** The effective map for state: the active maps' bindings, a more specific
 * map's binding hiding a less specific one with the same chord, or one it
 * collides with as a prefix (a "g" command hides "g s s"; "g s x" hides a
 * "g s" command). labels: prefix -> label, the most specific first. */
export function effective(km, state) {
  const maps = activeMaps(km, state);
  const all = allBindings(km);
  /** @type {Binding[]} */
  const binds = [];
  for (const map of maps) {
    for (const b of all) {
      if (b.map !== map) continue;
      const hidden = binds.some((h) => h.map !== map && (h.keys === b.keys || starts(b.seq, h.seq) || starts(h.seq, b.seq)));
      if (!hidden) binds.push(b);
    }
  }
  /** @type {Map<string, string>} */
  const labels = new Map();
  for (const map of maps) {
    const m = km.maps.get(map);
    if (CONTEXTS.includes(map)) {
      for (const l of km.localleader) {
        if (!labels.has(l)) labels.set(l, map);
        for (const [chord, label] of m.labels) if (!labels.has(`${l} ${chord}`)) labels.set(`${l} ${chord}`, label);
      }
    } else for (const [chord, label] of m.labels) if (!labels.has(chord)) labels.set(chord, label);
  }
  return { maps, binds, labels };
}

/** The binding for exactly seq in an effective map, and whether seq starts
 * longer ones. */
export function lookup(eff, seq) {
  const keys = seq.join(' ');
  return { hit: eff.binds.find((b) => b.keys === keys) ?? null, more: eff.binds.some((b) => starts(b.seq, seq)) };
}

/** The key a tooltip names for a command: its shortest chord, ties going to
 * the earlier map (:normal first) and then to the order in the file. */
export function primaryKey(km, cmd, bindings = allBindings(km)) {
  const order = ['normal', ...PANES, ...CONTEXTS, 'dialog', 'insert'];
  const bs = bindings.filter((b) => b.cmd === cmd);
  bs.sort((a, b) => a.seq.length - b.seq.length || order.indexOf(a.map) - order.indexOf(b.map));
  return bs[0]?.keys ?? '';
}
