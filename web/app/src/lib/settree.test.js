// npm test (node --test)
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ancestors, buildTree, search, marks } from './settree.js';

const paths = [
  'agent-desk', 'agent-desk/build', 'agent-desk/build/coord', 'agent-desk/build/coord/worker',
  'agent-desk/ui', 'agent-desk/ui/coord', 'agent-quotas', 'agent-quotas/r1', 'caap', 'caap/run/agent-helper',
];
const roots = buildTree(paths);
const sorted = (s) => [...s].sort();

test('builds the tree in name order, adding missing ancestors', () => {
  const t = buildTree(['b/x', 'a', 'a-b', 'a/z/q', 'a/y']);
  const flat = [];
  const walk = (ns) => ns.forEach((n) => (flat.push(`${n.depth}:${n.path}`), walk(n.children)));
  walk(t);
  assert.deepEqual(flat, ['0:a', '1:a/y', '1:a/z', '2:a/z/q', '0:a-b', '0:b', '1:b/x']);
});

test('no search shows everything', () => {
  assert.equal(search(roots, '  '), null);
});

test('a project name finds the project, not every role under it', () => {
  const r = search(roots, 'agent');
  // caap/run/agent-helper is a hit too, so caap and its run open.
  assert.deepEqual(sorted(r.hits), ['agent-desk', 'agent-quotas', 'caap/run/agent-helper']);
  assert.deepEqual(sorted(r.open), ['caap', 'caap/run']);
  // What is under a hit shows when its parent is opened by hand.
  assert.ok(r.show.has('agent-desk/build/coord/worker'));
});

test('a role name finds it in every project and opens its ancestors', () => {
  const r = search(roots, 'coord');
  assert.deepEqual(sorted(r.hits), ['agent-desk/build/coord', 'agent-desk/ui/coord']);
  assert.deepEqual(sorted(r.open), ['agent-desk', 'agent-desk/build', 'agent-desk/ui']);
  assert.ok(!r.show.has('agent-quotas') && !r.show.has('caap'));
  assert.ok(r.show.has('agent-desk/build/coord/worker')); // under a hit
});

test('several words narrow by the path; a word with / matches across segments', () => {
  assert.deepEqual(sorted(search(roots, 'desk worker').hits), ['agent-desk/build/coord/worker']);
  assert.deepEqual(sorted(search(roots, 'desk/build').hits), ['agent-desk/build']);
  assert.deepEqual(sorted(search(roots, 'Quotas R1').hits), ['agent-quotas/r1']);
  assert.equal(search(roots, 'nothing-here').show.size, 0);
});

test('ancestors and marks', () => {
  assert.deepEqual(ancestors('a/b/c'), ['a', 'a/b']);
  assert.deepEqual(ancestors('a'), []);
  assert.deepEqual(marks('agent-desk', ['desk']), [{ t: 'agent-', hit: false }, { t: 'desk', hit: true }]);
  assert.deepEqual(marks('Coord', ['co', 'rd']), [{ t: 'Co', hit: true }, { t: 'o', hit: false }, { t: 'rd', hit: true }]);
});
