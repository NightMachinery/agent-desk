// npm test (node --test)
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { navTarget, normNav, isDefaultNav, defaultNav } from './moveon.js';

const none = new Set();
const all = (order) => new Set(order);
/** info from { id: [kind, ts, views] } */
const infoOf = (m) => (id) => (m[id] ? { kind: m[id][0], ts: m[id][1], views: m[id][2] } : undefined);

test('fewer views first: a question already opened goes after everything unseen', () => {
  const order = ['q-a', 'q-b', 'b-c', 't-d'];
  const views = { 'q-a': 1, 'q-b': 0, 'b-c': 0, 't-d': 0 };
  const kinds = { 'q-a': 'blocked', 'q-b': 'blocked', 'b-c': 'deliverable', 't-d': 'notice' };
  const info = (id) => ({ kind: kinds[id], ts: order.indexOf(id), views: views[id] });
  const open = all(order);
  const walk = [];
  let here = '';
  for (let i = 0; i < 5; i++) {
    here = navTarget(order, here, none, open, info);
    walk.push(here);
    views[here]++;
    if (!here.startsWith('q')) open.delete(here); // read on opening; a question stays open
  }
  assert.deepEqual(walk, ['q-b', 'b-c', 't-d', 'q-a', 'q-b']);
});

test('two open questions do not trap it once each has a view', () => {
  const order = ['q-a', 'q-b', 'b-c'];
  const views = { 'q-a': 0, 'q-b': 0, 'b-c': 0 };
  const info = (id) => ({ kind: id.startsWith('q') ? 'blocked' : 'deliverable', ts: 1, views: views[id] });
  const seen = [];
  let here = '';
  for (let i = 0; i < 3; i++) {
    here = navTarget(order, here, none, all(order), info);
    views[here]++; // opening counts a view
    seen.push(here);
  }
  assert.deepEqual(seen, ['q-a', 'q-b', 'b-c']);
});

test('kinds: blocked, default, deliverable, response, notice; then oldest first', () => {
  const order = ['t-n', 't-r', 'b-d', 'q-d', 'q-b2', 'q-b1'];
  const info = infoOf({ 't-n': ['notice', 1], 't-r': ['response', 1], 'b-d': ['deliverable', 1], 'q-d': ['default', 1], 'q-b2': ['blocked', 9], 'q-b1': ['blocked', 5] });
  const walk = [];
  const open = all(order);
  let here = '';
  while ((here = navTarget(order, here, none, open, info))) {
    walk.push(here);
    open.delete(here); // read on opening
  }
  assert.deepEqual(walk, ['q-b1', 'q-b2', 'q-d', 'b-d', 't-r', 't-n']);
});

test('the settings: views off, kinds reordered, newest first', () => {
  const order = ['q-a', 'b-old', 'b-new'];
  const info = infoOf({ 'q-a': ['blocked', 1, 3], 'b-old': ['deliverable', 1, 0], 'b-new': ['deliverable', 2, 0] });
  const open = all(order);
  assert.equal(navTarget(order, '', none, open, info, { ...defaultNav, views: false }), 'q-a');
  assert.equal(navTarget(order, '', none, open, info, { ...defaultNav, views: false, kinds: ['deliverable', 'blocked', 'default', 'response', 'notice'] }), 'b-old');
  assert.equal(navTarget(order, '', none, open, info, { ...defaultNav, age: 'new' }), 'b-new');
});

test('skips here, pinned and read items; nothing else open gives ""', () => {
  const info = infoOf({ 'q-a': ['blocked', 1], 'q-p': ['blocked', 0], 'b-read': ['deliverable', 0] });
  assert.equal(navTarget(['q-p', 'b-read', 'q-a'], '', new Set(['q-p']), new Set(['q-p', 'q-a']), info), 'q-a');
  assert.equal(navTarget(['q-a', 'b-read'], 'q-a', none, new Set(['q-a']), info), '');
});

test('an item with no kind or time ranks after those with them, then by list order', () => {
  const order = ['x-1', 'x-2', 'b-c'];
  const info = infoOf({ 'b-c': ['deliverable', 5] });
  assert.equal(navTarget(order, '', none, all(order), info), 'b-c');
  assert.equal(navTarget(order, 'b-c', none, all(order), info), 'x-1');
});

test('normNav fills in and orders what is saved', () => {
  assert.deepEqual(normNav(null), defaultNav);
  assert.deepEqual(normNav({ views: false, kinds: ['notice', 'bogus', 'notice'], age: 'new' }), {
    views: false,
    kinds: ['notice', 'blocked', 'default', 'deliverable', 'response'],
    age: 'new',
  });
  assert.ok(isDefaultNav({}));
  assert.ok(!isDefaultNav({ age: 'new' }));
});
