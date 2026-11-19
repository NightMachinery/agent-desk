// npm test (node --test)
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { availableText } from './quotatime.js';

const now = new Date(2026, 10, 19, 19, 16, 0).getTime();
const at = (d) => d.getTime() / 1000;

test('no seat or unknown verdict says nothing', () => {
  assert.equal(availableText(null, now), '');
  assert.equal(availableText({ verdict: 'unknown' }, now), '');
});

test('usable now', () => {
  assert.equal(availableText({ verdict: 'usable' }, now), 'quota available now');
});

test('later today: span and clock', () => {
  const s = availableText({ verdict: 'blocked', available_at: at(new Date(2026, 10, 19, 21, 30)) }, now);
  assert.match(s, /^quota back in 2h 14m, at \d/);
  assert.ok(!/Mon|Tue|Wed|Thu|Fri|Sat|Sun/.test(s), s);
});

test('another day names the weekday', () => {
  const s = availableText({ verdict: 'blocked', available_at: at(new Date(2026, 10, 23, 14, 59)) }, now);
  assert.match(s, /^quota back in 3d 19h, at \S+ \d/);
});

test('minutes only, past due, unknown time', () => {
  assert.equal(availableText({ verdict: 'blocked', available_at: at(new Date(2026, 10, 19, 19, 40)) }, now).split(',')[0], 'quota back in 24m');
  assert.equal(availableText({ verdict: 'blocked', available_at: at(new Date(2026, 10, 19, 19, 0)) }, now), 'quota due back now');
  assert.equal(availableText({ verdict: 'blocked' }, now), 'quota used up; reset time unknown');
});
