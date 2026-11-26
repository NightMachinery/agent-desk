// Markdown owns code blocks, code spans and escapes. Math is recognized before
// the inline escape rule so TeX delimiters and commands keep their backslashes.
function openMarker(src, start, end) {
  if (src[start] === '$') {
    if (src[start + 1] !== '$' || start + 1 === end) return '$';
    return src[start + 2] !== '$' || start + 2 === end ? '$$' : '';
  }
  if (start + 1 < end && (src.startsWith('\\(', start) || src.startsWith('\\[', start))) {
    return src.slice(start, start + 2);
  }
  return '';
}

function closeMarker(open) {
  if (open === '\\(') return '\\)';
  if (open === '\\[') return '\\]';
  return open;
}

function isDisplay(open) {
  return open === '$$' || open === '\\[';
}

// Return both the end of the backtick run and the end of any Markdown code
// span it starts. Only a run of the same length can close the span.
function backticksAt(src, start, end) {
  const runs = /`+/g;
  runs.lastIndex = start;
  const openEnd = Math.min(end, runs.exec(src)[0].length + start);
  for (let match; (match = runs.exec(src)) && match.index < end;) {
    if (Math.min(runs.lastIndex, end) - match.index === openEnd - start) {
      return { openEnd, codeEnd: Math.min(runs.lastIndex, end) };
    }
  }
  return { openEnd, codeEnd: openEnd };
}

function findClose(src, start, end, close, duringInline = false) {
  for (let pos = start; pos < end; pos++) {
    if (src[pos] === '`' && duringInline) {
      const { openEnd, codeEnd } = backticksAt(src, pos, end);
      if (codeEnd > openEnd) return -1;
      pos = openEnd - 1;
      continue;
    }
    if (src[pos] === '$' && close[0] === '$') {
      let runEnd = pos + 1;
      while (runEnd < end && src[runEnd] === '$') runEnd++;
      if (runEnd - pos === close.length) return pos;
      if (close === '$') return -1;
      pos = runEnd - 1;
    } else if (src[pos] === '\\') {
      if (pos + close.length <= end && src.startsWith(close, pos)) return pos;
      pos++; // Escaped delimiters, including a backslash before a backslash.
    }
  }
  return -1;
}

function mathToken(state, open, content) {
  const token = state.push(isDisplay(open) ? 'math_display' : 'math_inline', '', 0);
  token.content = content;
  token.markup = open;
  state.env.hasMath = true;
  return token;
}

function inlineMath(state, silent, literal) {
  const { src, pos, posMax } = state;

  // In labels and captions these characters used to be literal. Preserve them
  // while giving code spans and escaped dollars the same math rules as prose.
  if (literal && (src[pos] === '`' || src.startsWith('\\$', pos) || src.startsWith('\\\\', pos))) {
    const end = src[pos] === '`' ? backticksAt(src, pos, posMax).codeEnd : pos + 2;
    if (!silent) state.pending += src.startsWith('\\$', pos) ? '$' : src.slice(pos, end);
    state.pos = end;
    return true;
  }

  const open = openMarker(src, pos, posMax);
  if (open) {
    const start = pos + open.length;
    const close = closeMarker(open);
    if (open !== '$' || !/\s/.test(src[start] ?? '')) {
      const end = findClose(src, start, posMax, close, true);
      const after = end + close.length;
      if (end > start && src.slice(start, end).trim() &&
          (open !== '$' || (!/\s/.test(src[end - 1]) && !/\d/.test(src[after] ?? '')))) {
        if (!silent) mathToken(state, open, src.slice(start, end));
        state.pos = after;
        return true;
      }
    }
  }
  if (src[pos] === '$') {
    // An unmatched dollar run is literal as a whole. Its second dollar must
    // never become an inline opener.
    let end = pos + 1;
    while (end < posMax && src[end] === '$') end++;
    if (!silent) state.pending += src.slice(pos, end);
    state.pos = end;
    return true;
  }
  return false;
}

function blockMath(state, first, last, silent) {
  if (state.sCount[first] - state.blkIndent >= 4) return false;
  const offset = state.bMarks[first] + state.tShift[first];
  const open = openMarker(state.src, offset, state.eMarks[first]);
  if (!isDisplay(open)) return false;
  const close = closeMarker(open);

  const lines = [];
  for (let line = first; line < last; line++) {
    if (line !== first && (state.isEmpty(line) || state.sCount[line] < state.blkIndent)) break;
    const text = state.getLines(line, line + 1, state.blkIndent, false);
    // An unclosed display opener must leave a following fenced code block
    // for Markdown to handle.
    if (line !== first && /^ {0,3}(?:`{3,}|~{3,})/.test(text)) break;
    const start = line === first ? text.indexOf(open) + open.length : 0;
    const end = findClose(text, start, text.length, close, line === first);
    if (end === -1) {
      lines.push(text.slice(start));
      continue;
    }
    // A closer followed by prose belongs to the inline parser.
    if (text.slice(end + close.length).trim()) return false;
    lines.push(text.slice(start, end));
    const content = lines.join('\n');
    if (!content.trim()) return false;
    if (!silent) {
      state.line = line + 1;
      mathToken(state, open, content).map = [first, state.line];
    }
    return true;
  }
  return false;
}

export function math(md, { literal = false } = {}) {
  md.inline.ruler.before('escape', 'math', (state, silent) => inlineMath(state, silent, literal));
  if (!literal) md.block.ruler.before('paragraph', 'math', blockMath);
  function sourceHtml(source, error = '') {
    const title = error ? ` title="${md.utils.escapeHtml(error)}"` : '';
    return `<span class="math-source"${title}>${md.utils.escapeHtml(source)}</span>`;
  }
  function render(tokens, i, _options, env) {
    const { content, markup } = tokens[i];
    const display = isDisplay(markup);
    const source = markup + content + closeMarker(markup);
    let html;
    try {
      html = env.renderMath?.(content.trim(), display);
    } catch (error) {
      // KaTeX includes input in its error messages. Escape the tooltip too.
      html = sourceHtml(source, error.message);
    }
    html ??= sourceHtml(source);
    if (!display) return html;
    const newline = tokens[i].block ? '\n' : '';
    return `<span class="math-display">${html}</span>${newline}`;
  }
  md.renderer.rules.math_inline = render;
  md.renderer.rules.math_display = render;
}
