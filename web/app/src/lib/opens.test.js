// npm test (node --test)
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { deliversSomething, opensDelivery, outsideLink } from './opens.js';

test('a link in the text counts only when it leaves the dashboard', () => {
  assert.ok(outsideLink('The deck: https://example.test/deck.pdf'));
  assert.ok(outsideLink('See [the report](https://example.test/r).'));
  assert.ok(!outsideLink('Done; see t-abcd and the notes in plain words.'));
  assert.ok(!outsideLink(''));
});

test('what a deliverable offers to open', () => {
  assert.ok(deliversSomething({ openURL: 'https://example.test/' }));
  assert.ok(deliversSomething({ media: [{ kind: 'image', url: 'api/media/x.png' }] }));
  assert.ok(!deliversSomething({ media: [{ kind: 'file', gone: 'deleted' }] }), 'a deleted file offers nothing');
  assert.ok(deliversSomething({ body: 'at https://example.test/x' }));
  assert.ok(!deliversSomething({ body: 'Just text.' }));
});

/** A stand-in for the clicked element inside an <a>. */
const inLink = (attrs, dataset = {}) => ({
  closest: () => ({ dataset, hasAttribute: (k) => k in attrs, getAttribute: (k) => attrs[k] ?? null }),
});

test('which clicks open the delivery', () => {
  assert.ok(opensDelivery(inLink({ href: 'sioyek://x' }, { cmd: 'deliverable-open' })));
  assert.ok(opensDelivery(inLink({ href: './api/media/f.pdf', download: 'f.pdf' })));
  assert.ok(opensDelivery(inLink({ href: 'https://example.test/', target: '_blank' })));
  assert.ok(!opensDelivery(inLink({ href: '#/?item=t-abcd' })));
  assert.ok(!opensDelivery({ closest: () => null }));
  assert.ok(!opensDelivery(null));
});
