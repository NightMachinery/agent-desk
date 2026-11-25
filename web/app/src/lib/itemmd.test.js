// npm test (node --test)
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { itemMarkdown } from './itemmd.js';

test('title as a heading, then the body as written', () => {
  assert.equal(itemMarkdown({ title: 'Build ready', body: 'It **works**.\n\n- a\n- b' }), '# Build ready\n\nIt **works**.\n\n- a\n- b');
});

test('no title: the body alone; nothing at all: empty', () => {
  assert.equal(itemMarkdown({ body: 'Just text.' }), 'Just text.');
  assert.equal(itemMarkdown({}), '');
});

test("a question's default and choices follow the body", () => {
  const e = {
    title: 'Port',
    body: 'Which port?',
    meta: { choices: { questions: [{ text: 'Port?', options: [{ key: 'A', label: '8080' }, { key: 'B', label: '9090' }], recommended: 'A' }] } },
  };
  assert.equal(itemMarkdown(e, 'A'), '# Port\n\nWhich port?\n\n**Default:** A\n\n1. Port?\n\n- **A:** 8080 (recommended)\n- **B:** 9090');
});
