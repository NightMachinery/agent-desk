import { test } from 'node:test';
import assert from 'node:assert/strict';
import { recentlyFinished, noteLine, agoShort } from './finished.js';

const now = Date.parse('2026-11-28T12:00:00Z');
const at = (h) => new Date(now - h * 3600e3).toISOString();

test('recentlyFinished keeps done and dropped tasks in the span, newest first', () => {
  const tasks = [
    { display_id: 'w-a', state: 'done', updated: at(11) },
    { display_id: 'w-b', state: 'started', updated: at(1) },
    { display_id: 'w-c', state: 'dropped', updated: at(2) },
    { display_id: 'w-d', state: 'done', updated: at(13) },
    { display_id: 'w-e', state: 'open', updated: at(0) },
  ];
  assert.deepEqual(recentlyFinished(tasks, 12 * 3600e3, now).map((t) => t.display_id), ['w-c', 'w-a']);
  assert.deepEqual(recentlyFinished(undefined, 3600e3, now), []);
});

test('noteLine takes the first line with text, without its markdown mark', () => {
  assert.equal(noteLine('\n  b042a43: skill and docs.\nMore.'), 'b042a43: skill and docs.');
  assert.equal(noteLine('## Done\nx'), 'Done');
  assert.equal(noteLine('- first step'), 'first step');
  assert.equal(noteLine('2. second'), 'second');
  assert.equal(noteLine(''), '');
  assert.equal(noteLine(undefined), '');
});

test('agoShort', () => {
  assert.equal(agoShort(at(0), now), 'just now');
  assert.equal(agoShort(at(0.5), now), '30 min');
  assert.equal(agoShort(at(3), now), '3 h');
  assert.equal(agoShort(at(72), now), '3 d');
});
