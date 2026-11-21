import { test } from 'node:test';
import assert from 'node:assert/strict';
import { fresh, rank, narrow, record, step, load, save, RECENT } from './mx.js';

const it = (name, label) => ({ name, label, run() {} });
const labels = (xs) => xs.map((x) => x.label);

test('rank: recent first, then by use, then shorter, then A to Z', () => {
  const items = [it('b', 'Bbbb'), it('a', 'Aaaa'), it('c', 'Cc'), it('d', 'Dd'), it('e', 'Eeeeee'), { label: 'Place' }];
  let h = fresh();
  h = record(h, 'e', '');
  h = record(h, 'b', '');
  h = record(h, 'b', '');
  // b ran last, then e; the rest never ran: shorter, then A to Z; places last.
  assert.deepEqual(labels(rank(items, h)), ['Bbbb', 'Eeeeee', 'Cc', 'Dd', 'Aaaa', 'Place']);
  // Past the 7 most recent, frequency decides.
  for (const n of ['n1', 'n2', 'n3', 'n4', 'n5', 'n6', 'n7']) h = record(h, n, '');
  assert.equal(h.recent.length, RECENT);
  assert.deepEqual(labels(rank(items, h)), ['Bbbb', 'Eeeeee', 'Cc', 'Dd', 'Aaaa', 'Place']);
  assert.equal(h.counts.b, 2);
});

test('narrow: words in any order, regexp, initialism; shorter first', () => {
  const items = [it('task-defer', 'Defer this task'), it('read-toggle', 'Mark read or unread'), it('reload', 'Reload the page')];
  assert.deepEqual(labels(narrow(items, 'task defer')), ['Defer this task']);
  assert.deepEqual(labels(narrow(items, 'mrou')), ['Mark read or unread']);
  assert.deepEqual(labels(narrow(items, '^re')), ['Reload the page']); // ^ anchors at the start of the label
  assert.deepEqual(labels(narrow(items, 're')), ['Reload the page', 'Mark read or unread']);
  assert.deepEqual(labels(narrow(items, '(')), []); // not a regexp: plain text
  assert.deepEqual(narrow(items, '  '), items);
});

test('record and step: input history, newest first, M-p back, M-n forward', () => {
  let h = fresh();
  h = record(h, 'x', 'first');
  h = record(h, 'y', 'second');
  h = record(h, 'x', 'first');
  assert.deepEqual(h.inputs, ['first', 'second']);
  let s = step(h.inputs, -1, 1, 'draft');
  assert.deepEqual(s, { at: 0, text: 'first' });
  s = step(h.inputs, s.at, 1, 'draft');
  assert.deepEqual(s, { at: 1, text: 'second' });
  s = step(h.inputs, s.at, 1, 'draft'); // the oldest stays
  assert.deepEqual(s, { at: 1, text: 'second' });
  s = step(h.inputs, s.at, -1, 'draft');
  s = step(h.inputs, s.at, -1, 'draft');
  assert.deepEqual(s, { at: -1, text: 'draft' });
});

test('load and save survive damage', () => {
  const m = new Map();
  const store = { getItem: (k) => m.get(k) ?? null, setItem: (k, v) => m.set(k, v) };
  assert.deepEqual(load(store), fresh());
  save(record(fresh(), 'a', 'aa'), store);
  assert.deepEqual(load(store).recent, ['a']);
  m.set('agent-desk.mx', '{bad');
  assert.deepEqual(load(store), fresh());
});
