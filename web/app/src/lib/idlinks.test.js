// npm test (node --test)
import { test } from 'node:test';
import assert from 'node:assert/strict';
import MarkdownIt from 'markdown-it';
import { idLinks } from './idlinks.js';

const md = new MarkdownIt({ html: false, linkify: true });
md.core.ruler.push('id_links', idLinks);
const refs = { 'q-8tqk': { title: 'Which split?', kind: 'question', state: 'open, blocked', url: '#/p/x/r1/coord?item=q-8tqk' } };
const render = (s) => md.render(s, { refs });
const link = '<a href="#/p/x/r1/coord?item=q-8tqk" class="idlink" title="q-8tqk, open, blocked: Which split?">';

test('a real id links; t-test and a missing id stay plain', () => {
  const html = render('See q-8tqk, then run a t-test; q-zzzz is gone.');
  assert.equal(html, `<p>See ${link}q-8tqk</a>, then run a t-test; q-zzzz is gone.</p>\n`);
});

test('an inline code span that is exactly an id links with code style', () => {
  assert.equal(render('Answer `q-8tqk` first.'), `<p>Answer ${link}<code>q-8tqk</code></a> first.</p>\n`);
});

test('code blocks, longer spans and existing links do not link', () => {
  assert.ok(!render('```\nq-8tqk\n```').includes('idlink'));
  assert.ok(!render('    q-8tqk').includes('idlink'));
  assert.ok(!render('Run `agent-desk read q-8tqk`.').includes('idlink'));
  assert.ok(!render('[see q-8tqk](https://example.net)').includes('idlink'));
});
