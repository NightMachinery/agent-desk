import katex from 'katex';
import 'katex/dist/katex.min.css';

export function renderFormula(tex, displayMode) {
  // Markdown escapes raw HTML. KaTeX generates escaped markup, and this trust
  // handler rejects every command that could add links, images or attributes.
  return katex.renderToString(tex, {
    displayMode,
    trust({ command }) {
      throw new katex.ParseError('Unsupported LaTeX command ' + command);
    },
  });
}

function formulaAt(node) {
  const element = node instanceof Element ? node : node.parentElement;
  const formula = element?.closest('.md :is(.katex, .katex-display)');
  return formula?.closest('.katex-display') ?? formula;
}

// Native selection can copy both KaTeX layers as scattered glyphs. Keep its
// HTML for rich paste and use the MathML annotations for plain-text paste.
document.addEventListener('copy', (event) => {
  const selection = document.getSelection();
  if (!event.clipboardData || !selection?.rangeCount || selection.isCollapsed) return;

  const range = selection.getRangeAt(0).cloneRange();
  const first = formulaAt(range.startContainer);
  const last = formulaAt(range.endContainer);
  if (first) range.setStartBefore(first);
  if (last) range.setEndAfter(last);
  const fragment = range.cloneContents();
  const formulas = fragment.querySelectorAll('.katex');
  if (!formulas.length) return;

  const content = document.createElement('div');
  content.append(fragment);
  event.clipboardData.setData('text/html', content.innerHTML);
  for (const formula of formulas) {
    const tex = formula.querySelector('annotation[encoding="application/x-tex"]');
    if (!tex) continue;
    const display = formula.closest('.katex-display');
    const delimiter = display ? '$$' : '$';
    (display ?? formula).replaceWith(delimiter + tex.textContent + delimiter);
  }
  event.clipboardData.setData('text/plain', content.textContent);
  event.preventDefault();
});
