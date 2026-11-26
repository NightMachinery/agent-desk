import assert from 'node:assert/strict';
import test from 'node:test';
import { markdown, parseMarkdown } from './md.js';
import { outsideLink } from './opens.js';

function formulas(source, options) {
  const parsed = parseMarkdown(source, options);
  const calls = [];
  const html = parsed.render((tex, display) => {
    calls.push([tex, display]);
    return '<span class="formula">rendered</span>';
  });
  assert.equal(parsed.hasMath, calls.length > 0);
  return { calls, html };
}

test('renders both inline delimiters inside Markdown formatting and lists', () => {
  const tex = String.raw`\sum_{i:\,g(i)=k}\frac{\bar{x}_i}{\sqrt{2}} \cdot \tilde{x}`;
  const { calls, html } = formulas(String.raw`- **Use $${tex}$ and \(y_2 + \text{units}\).**`);
  assert.deepEqual(calls, [[tex, false], [String.raw`y_2 + \text{units}`, false]]);
  assert.match(html, /<li><strong>Use <span class="formula">/);
});

test('renders both display delimiters on their own and inside paragraphs', () => {
  const source = String.raw`Before.

$$
\sum_{i=0}^n i \qquad \sqrt{2}
$$
then **\[x^2\]** and $$y^2$$ after.

- \[
  \begin{aligned}
    a &= 1 \\
    b &= 2
  \end{aligned}
  \]

> $$ z = 3 $$`;
  const { calls, html } = formulas(source);
  assert.deepEqual(calls, [
    [String.raw`\sum_{i=0}^n i \qquad \sqrt{2}`, true],
    ['x^2', true],
    ['y^2', true],
    [String.raw`\begin{aligned}
  a &= 1 \\
  b &= 2
\end{aligned}`, true],
    ['z = 3', true],
  ]);
  assert.match(html, /<p>Before\.<\/p>\n<span class="math-display">/);
  assert.match(html, /<strong><span class="math-display">/);
  assert.match(html, /<blockquote>\s*<span class="math-display">/);
});

test('applies the single dollar whitespace, digit and escape rules', () => {
  for (const source of [
    'costs $5 and $10',
    '$HOME',
    'a $ sign',
    '$ x$',
    '$x $',
    '$x$2',
    '$$$x$$$',
    String.raw`\$x\$`,
    String.raw`\\(x\\)`,
  ]) {
    assert.deepEqual(formulas(source).calls, [], source);
  }
  const { calls, html } = formulas(String.raw`Costs \$5 or $10, and $2+2$ or $a+\$b$. \(c\\)d\)`);
  assert.deepEqual(calls, [
    ['2+2', false],
    [String.raw`a+\$b`, false],
    [String.raw`c\\)d`, false],
  ]);
  assert.match(html, /Costs \$5 or \$10/);
});

test('leaves inline, fenced and indented code alone', () => {
  const { calls, html } = formulas([
    '$HOME and `$x$` and ``\\(y\\)``',
    '',
    '    $$indented$$',
    '',
    '```text',
    '$$fenced$$',
    '\\[fenced\\]',
    '```',
    '',
    '$$',
    '~~~',
    '$$',
    '~~~',
    '',
    '$z$',
  ].join('\n'));
  assert.deepEqual(calls, [['z', false]]);
  assert.match(html, /<code>\$x\$<\/code>/);
  assert.match(html, /<code>\\\(y\\\)<\/code>/);
  assert.match(html, /<pre><code>\$\$indented\$\$/);
  assert.match(html, /<pre><code class="language-text">\$\$fenced\$\$\n\\\[fenced\\\]/);
});

test('keeps literal captions and labels as text around math', () => {
  const source = String.raw`**Caption** [link](https://example.test) ` + '`$code$`' + String.raw` at \$5: \(x\)`;
  const { calls, html } = formulas(source, { literalText: true });
  assert.deepEqual(calls, [['x', false]]);
  assert.equal(html, '**Caption** [link](https://example.test) `$code$` at $5: <span class="formula">rendered</span>');
});

test('escapes source before loading and escapes parser errors in tooltips', () => {
  const parsed = parseMarkdown(String.raw`$<b>"x"</b>$ and \[y\]`);
  assert.equal(parsed.hasMath, true);
  assert.match(parsed.render(), /\$&lt;b&gt;&quot;x&quot;&lt;\/b&gt;\$/);
  assert.match(parsed.render(() => { throw new Error('<bad> "quote"'); }),
    /title="&lt;bad&gt; &quot;quote&quot;">\$&lt;b&gt;&quot;x&quot;&lt;\/b&gt;\$/);
  assert.match(markdown('<b>raw</b>'), /&lt;b&gt;raw&lt;\/b&gt;/);
  assert.equal(outsideLink(String.raw`$x$ and $\url{https://example.test}$`), false);
  assert.equal(outsideLink('$x$ and https://example.test'), true);
});
