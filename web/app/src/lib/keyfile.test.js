import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { normalizeStroke, normalizeChord, readKeyFile, buildKeymap, effective, lookup, allBindings, primaryKey, KeymapError } from './keyfile.js';

const here = new URL('.', import.meta.url);
const BASE = readFileSync(new URL('keys.edn', here), 'utf8');

/** The commands keymap.js defines, read from its source (it needs a page to
 * run): each C('name', ...) line, with the pane or anywhere it names. */
export function sourceCommands() {
  const src = readFileSync(new URL('keymap.js', here), 'utf8');
  const out = new Map();
  for (const m of src.matchAll(/^C\('([a-z0-9-]+)',(.*)$/gm)) {
    const pane = /pane: '([a-z]+)'/.exec(m[2])?.[1];
    out.set(m[1], { pane, anywhere: /anywhere: true/.test(m[2]) });
  }
  return out;
}
const CMDS = sourceCommands();
const base = () => readKeyFile(BASE);
const build = (user) => buildKeymap(base(), user === undefined ? null : readKeyFile(user), CMDS);
const fails = (fn, re) => assert.throws(fn, (e) => e instanceof KeymapError && re.test(e.message), re.source);

test('the built-in keys.edn builds against the commands in keymap.js', () => {
  assert.ok(CMDS.size > 100, `read ${CMDS.size} commands`);
  const km = build();
  const bs = allBindings(km);
  assert.ok(bs.length > 150, `${bs.length} bindings`);
  assert.deepEqual(km.localleader, [',', 'SPC m']);
});

test('strokes: canonical spelling, aliases, and what does not parse', () => {
  const ok = {
    'S-g': 'G', G: 'G', 'C-S-g': 'C-G', 'M-x': 'M-x', 'Ctrl+x': 'C-x', 'alt+shift+P': 'M-P', 'M-C-x': 'C-M-x',
    spc: 'SPC', Ret: 'RET', Backspace: 'DEL', Delete: '<delete>', Up: '<up>', PgUp: '<prior>', PgDown: '<next>',
    F5: '<f5>', '<f12>': '<f12>', 'S-TAB': 'S-TAB', '?': '?', '-': '-', 'C--': 'C--', 'C-[': 'C-[', 's-k': 's-k',
  };
  for (const [raw, want] of Object.entries(ok)) assert.equal(normalizeStroke(raw), want, raw);
  for (const raw of ['S-/', 'S-SPC', 'C-C-x', 'Foo', 'S-', 'F36']) assert.throws(() => normalizeStroke(raw), KeymapError, raw);
  assert.deepEqual(normalizeChord('SPC i Z'), ['SPC', 'i', 'Z']);
  for (const raw of ['', 'g  c', ' g', 'a b c d e']) assert.throws(() => normalizeChord(raw), KeymapError, raw);
});

test('reading a file: one map, known maps, and no duplicates', () => {
  const f = readKeyFile('; mine\n{:normal {"g c" nil, "C-c t" :task-add, "SPC b" "buffers"} #_ {:x 1}\n :task {"f" :task-defer}}\n');
  assert.deepEqual(f.maps.get('normal'), [
    { chord: 'g c', raw: 'g c', value: null },
    { chord: 'C-c t', raw: 'C-c t', value: { cmd: 'task-add' } },
    { chord: 'SPC b', raw: 'SPC b', value: { label: 'buffers' } },
  ]);
  fails(() => readKeyFile(''), /empty/);
  fails(() => readKeyFile('{} {}'), /exactly one map/);
  fails(() => readKeyFile('[]'), /exactly one map/);
  fails(() => readKeyFile('{}]'), /not valid EDN: a closing bracket without its opening one$/);
  fails(() => readKeyFile('{}\n]extra['), /closing bracket/);
  fails(() => readKeyFile('{:normal {"g" :top'), /not valid EDN/);
  fails(() => readKeyFile('{:normal {"x" :pin "x" :pin}}'), /:normal "x": appears twice/);
  fails(() => readKeyFile('{:normal {"S-g" :pin "G" :pin}}'), /:normal "G": the same chord as "S-g"/);
  fails(() => readKeyFile('{:normal {} :normal {}}'), /:normal: appears twice/);
  fails(() => readKeyFile('{:nromal {}}'), /:nromal: unknown map/);
  fails(() => readKeyFile('{:normal {"x" 1}}'), /a binding is a command keyword, nil, or a label/);
  fails(() => readKeyFile('{:normal {"x" :ns/pin}}'), /without a namespace/);
  fails(() => readKeyFile('{:normal {"x" #inst "2020-01-01"}}'), /a binding is/);
  fails(() => readKeyFile('{:normal {:x :pin}}'), /a chord must be a string/);
  fails(() => readKeyFile('{:normal {"g  c" :pin}}'), /single spaces/);
  fails(() => readKeyFile(';'.repeat(64 * 1024 + 1)), /larger than 64 KiB/);
});

test('overlay: replace, add, remove; and what a user file may not do', () => {
  const km = build('{:normal {"g c" nil, "C-c t" :task-add, "p" :take} :list {"z" nil}}');
  const n = km.maps.get('normal').binds;
  assert.equal(n.has('g c'), false);
  assert.equal(n.get('C-c t'), 'task-add');
  assert.equal(n.get('p'), 'take');
  assert.equal(n.get('g C'), 'tasks-all', 'nil removes only that entry');
  assert.equal(km.maps.get('list').labels.has('z'), false);
  assert.equal(km.maps.get('list').binds.get('z a'), 'fold', 'nil on a prefix leaves what extends it');

  fails(() => build('{:normal {"g" :top}}'), /:normal "g": runs a command and also starts "g /);
  fails(() => build('{:normal {"x" :no-such}}'), /no command :no-such/);
  fails(() => build('{:normal {"ESC" nil}}'), /ESC always backs out/);
  fails(() => build('{:list {"C-g" :fold}}'), /C-g always backs out/);
  fails(() => build('{:normal {"5" :pin}}'), /digits start a count/);
  fails(() => build('{:normal {"s-k" :pin}}'), /belong to the browser/);
  fails(() => build('{:insert {"x" :m-x}}'), /must start with C- or M-/);
  fails(() => build('{:normal {":" nil "M-x" nil "SPC :" nil}}'), /last key for :m-x/);
  fails(() => build('{:detail {"x" :first-item}}'), /works only in the list pane/);
  fails(() => build('{:dialog {"x" :pin}}'), /does not work in a sheet/);
  fails(() => build('{:normal {"x" :sheet-next}}'), /works only in a sheet/);
  fails(() => build('{:normal {"p" "pins"}}'), /a label \("pins"\) on a chord that runs a command/);
  fails(() => build('{:normal {"SPC y" "yank"}}'), /on a chord that starts nothing/);
  fails(() => build('{:task {"j" :first-item}}'), /works only in the list pane, so not after the localleader/);
  fails(() => build('{:keyless [:no-such]}'), /:keyless: no command :no-such/);
  // A context map's keys may start with a digit: they follow the localleader.
  assert.equal(build('{:task {"1" :task-defer}}').maps.get('task').binds.get('1'), 'task-defer');
});

test('effective map: precedence, prefix shadowing, contexts, sheets and text boxes', () => {
  const km = build('{:normal {"q" :pin, "h" :top, "z z" :top} :detail {"z" :close}, :task {"s" :task-status, "x" "defer", "x x" :task-defer}}');
  const at = (state, chord) => lookup(effective(km, state), chord.split(' '));
  assert.equal(at({ pane: 'detail' }, 'q').hit.cmd, 'close', 'a pane map hides :normal');
  assert.equal(at({ pane: 'list' }, 'q').hit.cmd, 'pin');
  assert.equal(at({ pane: 'detail' }, 'z').hit.cmd, 'close', 'a pane command hides a :normal prefix');
  assert.equal(at({ pane: 'detail' }, 'z').more, false);
  assert.equal(at({ pane: 'list' }, 'z').more, true);
  assert.equal(at({ pane: 'list' }, 'h').hit.cmd, 'pane-side');
  assert.equal(at({ pane: 'side' }, 'h').hit.cmd, 'top');
  assert.equal(at({ pane: 'list' }, ', s').hit, null, 'no context, no localleader');
  assert.equal(at({ pane: 'list', context: 'task' }, ', s').hit.cmd, 'task-status');
  assert.equal(at({ pane: 'list', context: 'task' }, 'SPC m s').hit.cmd, 'task-status');
  assert.equal(effective(km, { context: 'task' }).labels.get(','), 'task');
  assert.equal(effective(km, { context: 'task' }).labels.get('SPC m x'), 'defer');
  assert.equal(at({ pane: 'list', context: 'deliverable' }, ', s').hit, null);
  assert.equal(at({ dialog: true }, 'q').hit.cmd, 'sheet-close', 'a sheet has only :dialog');
  assert.equal(at({ dialog: true }, 'p').hit, null);
  assert.equal(at({ insert: true }, 'M-x').hit.cmd, 'm-x');
  assert.equal(at({ insert: true }, 'p').hit, null);
  // A more specific command chord that a less specific map uses as a prefix.
  const km2 = build('{:list {"g s" :fold}}');
  assert.equal(lookup(effective(km2, { pane: 'list' }), ['g', 's']).hit.cmd, 'fold');
  const km3 = build('{:list {"g s x" :fold}}');
  const e3 = effective(km3, { pane: 'list' });
  assert.equal(lookup(e3, ['g', 's']).hit, null, 'a longer chord in the pane hides "g s"');
  assert.equal(lookup(e3, ['g', 's']).more, true);
});

test('primary key: the shortest chord, :normal first', () => {
  const km = build();
  assert.equal(primaryKey(km, 'pin'), 'p');
  assert.equal(primaryKey(km, 'next-item'), 'J');
  assert.equal(primaryKey(km, 'close'), 'q');
  assert.equal(primaryKey(km, 'tasks'), 'g c');
  assert.equal(primaryKey(km, 'node-settings'), 'SPC d s');
  assert.equal(primaryKey(km, 'task-done'), ', d', 'a context key, by the shorter localleader');
  assert.equal(primaryKey(km, 'quota-copy-ids'), 'y q', ':normal before a context map');
  assert.equal(primaryKey(km, 'deliverables-all'), '');
});

test('the built-in scheme: each leader entry reuses its short key, and every context map is filled', () => {
  const km = build();
  const n = km.maps.get('normal').binds;
  // Rule 3: a short key for an item command is also its SPC b letter.
  for (const [short, leader] of [['a', 'SPC b a'], ['t', 'SPC b t'], ['r', 'SPC b r'], ['R', 'SPC b R'], ['d', 'SPC b d'], ['p', 'SPC b p'], ['x', 'SPC b x']]) {
    assert.equal(n.get(leader), n.get(short), `${leader} runs what ${short} runs`);
  }
  assert.equal(n.get('SPC b u'), n.get('U'));
  // Rule 7: nothing that cannot be undone sits on a short key.
  for (const cmd of ['agent-archive', 'task-drop', 'quota-archive']) {
    for (const b of allBindings(km).filter((b) => b.cmd === cmd)) assert.ok(b.seq.length > 1, `${cmd} on ${b.keys}`);
  }
  for (const c of ['task', 'deliverable', 'screen', 'message', 'quota', 'settings', 'verbatim']) {
    assert.ok(km.maps.get(c).binds.size > 0, `:${c} has keys`);
  }
  // Every leader group the guide labels has keys under it.
  for (const [chord] of km.maps.get('normal').labels) {
    assert.ok([...n.keys()].some((k) => k.startsWith(chord + ' ')), `${chord} starts something`);
  }
});
