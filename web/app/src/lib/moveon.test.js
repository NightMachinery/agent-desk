// npm test (node --test)
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { moveOnTarget } from './moveon.js';

const none = new Set();

test('next question after here', () => {
  assert.equal(moveOnTarget(['q-a', 'q-b', 't-c', 'q-d'], 'q-a', none), 'q-b');
});

test('skips a pinned question that is next in the list', () => {
  assert.equal(moveOnTarget(['q-a', 'q-p', 'q-b'], 'q-a', new Set(['q-p'])), 'q-b');
});

test('a pinned question listed under Needs is skipped too', () => {
  // q-p shows both in Needs and Pinned; the order keeps its first place.
  assert.equal(moveOnTarget(['q-a', 'q-p', 't-x', 'q-b'], 'q-a', new Set(['q-p'])), 'q-b');
});

test('nearest question before when none after', () => {
  assert.equal(moveOnTarget(['q-a', 'q-b', 'q-c', 't-d'], 'q-c', none), 'q-b');
});

test('any unpinned item when no question is left', () => {
  assert.equal(moveOnTarget(['q-a', 't-p', 't-b'], 'q-a', new Set(['t-p'])), 't-b');
  assert.equal(moveOnTarget(['t-b', 't-p', 'q-a'], 'q-a', new Set(['t-p'])), 't-b');
});

test('back to the list when only pinned items are left', () => {
  assert.equal(moveOnTarget(['q-a', 'q-p', 't-p'], 'q-a', new Set(['q-p', 't-p'])), '');
});

test('here not in the order: first unpinned question', () => {
  assert.equal(moveOnTarget(['q-p', 't-x', 'q-b'], 'q-gone', new Set(['q-p'])), 'q-b');
});
