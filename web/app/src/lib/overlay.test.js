import { test } from 'node:test';
import assert from 'node:assert/strict';
import { leaves, live, patchGroups, patchItem, settled } from './overlay.js';

const item = (id, extra = {}) => ({ display_id: id, unread: true, draft: true, ...extra });
const groups = () => [
  { project: 'p', run: 'r', items: [item('q-1'), item('q-2')] },
  { project: 'p', run: 's', items: [item('q-3')] },
];

test('a change on its way applies to every answer, however new', () => {
  const c = { ids: ['q-1'], patch: {}, leave: [], done: 0 };
  assert.deepEqual(live([c], { view: 99, item: 99 }, 'view'), [c]);
});

test('a change the desk took applies until an answer asked for after it is shown', () => {
  const c = { ids: ['q-1'], patch: {}, leave: [], done: 5 };
  assert.deepEqual(live([c], { view: 5, item: 0 }, 'view'), [c], 'ticket 5 went out before the desk took it');
  assert.deepEqual(live([c], { view: 6, item: 0 }, 'view'), [], 'ticket 6 went out after');
  assert.deepEqual(live([c], { view: 6, item: 3 }, 'item'), [c], 'each kind of answer on its own');
});

test('a change goes once both kinds of answer have shown it', () => {
  assert.equal(settled({ done: 0 }, { view: 9, item: 9 }), false);
  assert.equal(settled({ done: 5 }, { view: 6, item: 5 }), false);
  assert.equal(settled({ done: 5 }, { view: 6, item: 6 }), true);
});

test('patches apply oldest first, and an item no change names stays the same object', () => {
  const cs = [
    { ids: ['q-1'], patch: { unread: false }, leave: [] },
    { ids: ['q-1', 'q-2'], patch: { unread: true, draft: false }, leave: [] },
  ];
  assert.deepEqual(patchItem(item('q-1'), cs), item('q-1', { unread: true, draft: false }));
  const other = item('q-9');
  assert.equal(patchItem(other, cs), other);
});

test('an answered question leaves Needs you at once, and its group goes when empty', () => {
  const cs = [{ ids: ['q-3'], patch: { answered: true, draft: false }, leave: ['needs'] }];
  const out = patchGroups(groups(), 'needs', cs);
  assert.deepEqual(out.map((g) => g.items.map((it) => it.display_id)), [['q-1', 'q-2']]);
  assert.equal(leaves(cs, 'q-3', 'needs'), true);
  assert.equal(leaves(cs, 'q-3', 'pinned'), false);
  // Another section keeps it, patched.
  assert.deepEqual(patchGroups(groups(), 'pinned', cs)[1].items[0], item('q-3', { answered: true, draft: false }));
});

test('no changes: the same groups; no groups: an empty list', () => {
  const gs = groups();
  assert.equal(patchGroups(gs, 'needs', []), gs);
  assert.deepEqual(patchGroups(undefined, 'needs', []), []);
});
