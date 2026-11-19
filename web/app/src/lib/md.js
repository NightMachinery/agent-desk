// Markdown for what agents write: bodies, slots, thread entries, linked .md
// files. Raw HTML stays text (html: false), so a body cannot inject markup;
// markdown-it also refuses javascript: and similar link targets. Images are
// off until the desk has its own attachments: a remote image would load from
// wherever an agent pointed it.
import MarkdownIt from 'markdown-it';
import { idLinks } from './idlinks.js';

// A single newline is ambiguous on the desk. Agents hard-wrap prose at about
// 72 columns, where it must flow; older bodies and slots are plain lines
// ("Raise it.\nB. Keep it.", one path per line), where it must stay. So a
// newline is a soft wrap only mid-sentence: the line ends in a letter, digit
// or comma and the next starts with a lowercase letter. Anything else is a
// line break.
function lastText(children, i) {
  for (let k = i - 1; k >= 0; k--) {
    const t = children[k];
    if (t.type === 'text' || t.type === 'code_inline') return t.content;
    if (t.type === 'softbreak' || t.type === 'hardbreak') return '';
  }
  return '';
}
function nextText(children, i) {
  for (let k = i + 1; k < children.length; k++) {
    const t = children[k];
    if (t.type === 'text' || t.type === 'code_inline') return t.type === 'text' ? t.content : '`';
    if (t.type === 'softbreak' || t.type === 'hardbreak') return '';
  }
  return '';
}
function smartBreaks(state) {
  for (const block of state.tokens) {
    if (block.type !== 'inline' || !block.children) continue;
    const ch = block.children;
    ch.forEach((t, i) => {
      if (t.type !== 'softbreak') return;
      const flows = /[\p{L}\p{N},]$/u.test(lastText(ch, i)) && /^\p{Ll}/u.test(nextText(ch, i));
      if (!flows) t.type = 'hardbreak';
    });
  }
}

function make(links) {
  const md = new MarkdownIt({ html: false, linkify: links, breaks: false, typographer: false });
  md.core.ruler.push('smart_breaks', smartBreaks);
  if (links) md.core.ruler.push('id_links', idLinks);
  md.disable(['image']);
  if (!links) md.disable(['link', 'autolink']);
  const open = md.renderer.rules.link_open || ((t, i, o, e, self) => self.renderToken(t, i, o));
  md.renderer.rules.link_open = (tokens, i, opts, env, self) => {
    // An id link navigates inside the dashboard; others open a new tab.
    if (!tokens[i].meta?.deskId) {
      tokens[i].attrSet('target', '_blank');
      tokens[i].attrSet('rel', 'noopener noreferrer');
    }
    return open(tokens, i, opts, env, self);
  };
  return md;
}

const full = make(true);
// For text shown inside a link (a slot card): nested links are invalid HTML.
const plain = make(false);

export function markdown(text, { links = true, refs = null } = {}) {
  return (links ? full : plain).render(text || '', { refs });
}
