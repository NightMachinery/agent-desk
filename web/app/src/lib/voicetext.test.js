import { test } from 'node:test';
import assert from 'node:assert/strict';
import { keepsTranscript, sortVoice, transcriptBlock } from './voicetext.js';

const block = transcriptBlock('Can you add these plots to the page for me? Compare with patching a single layer.');
const token = (id, status = 'transcribing') => `[voice ${id} 0:12, ${status}]`;
const A = 'v-aaaaaaaaaaaa';
const B = 'v-bbbbbbbbbbbb';

test('a verbatim transcript is kept', () => {
  assert.ok(keepsTranscript(`Hi\n\n${block}`, block));
});

test('an edited transcript is kept', () => {
  const edited = block.replace('patching', 'pinned patching').replace('Can you', 'Could you');
  assert.ok(keepsTranscript(edited, block));
});

test('a deleted transcript is not kept', () => {
  assert.ok(!keepsTranscript('Just send it.', block));
});

test('a transcript the sender saw is never asked about, edited or deleted', () => {
  const draft = [{ id: A, block }];
  assert.deepEqual(sortVoice(block.replace('plots', 'figures'), draft), { used: draft, ask: [] });
  assert.deepEqual(sortVoice('Never mind.', draft), { used: [], ask: [] });
});

test('a failed recording whose token was deleted is not asked about', () => {
  assert.deepEqual(sortVoice('Text only.', [{ id: A }], { [A]: 'failed' }).ask, []);
});

test('a running or unseen recording whose token was deleted is asked about', () => {
  const draft = [{ id: A }, { id: B }];
  assert.deepEqual(sortVoice('Text only.', draft, { [A]: 'transcribing', [B]: 'ready' }).ask, draft);
  assert.deepEqual(sortVoice('Text only.', [{ id: A }]).ask, [{ id: A }]);
});

test('a token still in the text is not asked about', () => {
  assert.deepEqual(sortVoice(`Look: ${token(A)}`, [{ id: A }], { [A]: 'transcribing' }), { used: [], ask: [] });
});
