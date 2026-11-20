import { test } from 'node:test';
import assert from 'node:assert/strict';
import { arrival, arrivalHooks, arrivedAuto, cancelArrival, isAutoArrival, routeMoved, settleArrival } from './arrival.js';

const sent = [];
arrivalHooks({ poster: async (id) => sent.push(id) });
const reset = () => { sent.length = 0; arrivedAuto(''); };

test('an auto arrival stays unread until the user leaves it', async () => {
  reset();
  arrivedAuto('t-1');
  arrival.pending = true; // Detail: opening would have read it
  assert.ok(isAutoArrival('t-1'));
  routeMoved('t-0', 't-1'); // the move-on itself
  assert.deepEqual(sent, []);
  routeMoved('t-1', 't-2'); // the user goes on
  await Promise.resolve();
  assert.deepEqual(sent, ['t-1']);
  assert.ok(!isAutoArrival('t-1'));
});

test('leaving for the list reads it too', async () => {
  reset();
  arrivedAuto('t-1');
  arrival.pending = true;
  routeMoved('t-1', '');
  await Promise.resolve();
  assert.deepEqual(sent, ['t-1']);
});

test('acting on it (answer, message) reads it', async () => {
  reset();
  arrivedAuto('t-1');
  arrival.pending = true;
  settleArrival('t-1');
  await Promise.resolve();
  assert.deepEqual(sent, ['t-1']);
});

test('a question, or an item already read, is never marked read', async () => {
  reset();
  arrivedAuto('q-1'); // pending stays false
  routeMoved('q-1', 't-2');
  await Promise.resolve();
  assert.deepEqual(sent, []);
});

test('Back or Undo cancels it: left unread', async () => {
  reset();
  arrivedAuto('t-1');
  arrival.pending = true;
  cancelArrival('t-1');
  routeMoved('t-1', 't-0');
  await Promise.resolve();
  assert.deepEqual(sent, []);
});

test('items opened by the user are not arrivals', async () => {
  reset();
  routeMoved('t-5', 't-6');
  settleArrival('t-6');
  await Promise.resolve();
  assert.deepEqual(sent, []);
});
