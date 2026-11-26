import { test } from 'node:test';
import assert from 'node:assert/strict';
import { parseTip, withKey, ownLabel } from './tooltip.js';

test('a title splits into heading, body and keys', () => {
  assert.deepEqual(parseTip('Wait for quota to spare: the quota nudge offers it  (, Z)'), {
    head: 'Wait for quota to spare',
    body: 'The quota nudge offers it',
    keys: [[',', 'Z']],
  });
  assert.deepEqual(parseTip('Pin to the top  (p · SPC b p)').keys, [['p'], ['SPC', 'b', 'p']]);
  assert.deepEqual(parseTip('Send  (⌘ RET)'), { head: 'Send', body: '', keys: [['⌘', 'RET']] });
});

test('a sentence break or a trailing note makes the body', () => {
  assert.deepEqual(parseTip('Delivered over 15 minutes ago with no response. Marking it read clears this.'), {
    head: 'Delivered over 15 minutes ago with no response',
    body: 'Marking it read clears this.',
    keys: [],
  });
  assert.deepEqual(parseTip('Discard this draft (Undo brings it back)'), { head: 'Discard this draft', body: 'Undo brings it back', keys: [] });
  assert.deepEqual(parseTip('Unread'), { head: 'Unread', body: '', keys: [] });
  assert.deepEqual(parseTip("Archive the coordinator's Paseo agent (stops it if running; messaging it revives it)  (SPC d a)"), {
    head: "Archive the coordinator's Paseo agent",
    body: 'Stops it if running; messaging it revives it',
    keys: [['SPC', 'd', 'a']],
  });
});

test('a long text with no early break is all body; brackets inside are not keys', () => {
  const long = 'No agent holds this node, so messages and new tasks here go to the coordinator working under it';
  assert.deepEqual(parseTip(long), { head: '', body: long, keys: [] });
  assert.equal(parseTip('Open it (in a tab)').head, 'Open it');
  assert.deepEqual(parseTip(''), { head: '', body: '', keys: [] });
});

test('a data-cmd control names its key once, with its label when it has no title', () => {
  assert.equal(withKey('https://x.org/deck.pdf', ', o', 'Open'), 'https://x.org/deck.pdf  (, o)');
  assert.equal(parseTip('Open: https://x.org/deck.pdf  (O)').body, 'https://x.org/deck.pdf');
  assert.deepEqual(parseTip(withKey('Take it back', 'SPC q r', 'x')).keys, [['SPC', 'q', 'r']]);
  assert.equal(withKey('Pin to the top  (p)', 'p', 'Pin'), 'Pin to the top  (p)');
  assert.equal(withKey('', 'h · q', '← Back'), '← Back  (h · q)');
  assert.equal(withKey('Clear', '', 'Clear'), 'Clear');
  assert.equal(ownLabel('Which items to list', 'All Unread Old', 'list-show'), 'Which items to list');
  assert.equal(ownLabel('', '  Mark all\n read ', 'read-all'), 'Mark all read');
  assert.equal(ownLabel('', 'x'.repeat(60), 'deferred-fold'), 'Deferred fold');
});
