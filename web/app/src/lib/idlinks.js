// A markdown-it rule that links id mentions ("see q-8tqk") to their items:
// plain-text mentions, and inline code spans that are exactly one id. Never
// code blocks, longer code spans, or text already inside a link. Only ids in
// env.refs (confirmed by the server) link.
const ID = /\b[a-z]-[0-9a-hjkmnp-tv-z]{4,}\b/g;

export function refTitle(id, r) {
  return `${id}, ${r.state ? r.state + ': ' : r.kind + ': '}${r.title}`;
}

function linkTokens(state, id, r, inner) {
  const open = new state.Token('link_open', 'a', 1);
  open.attrs = [['href', r.url], ['class', 'idlink'], ['title', refTitle(id, r)]];
  open.meta = { deskId: true };
  const close = new state.Token('link_close', 'a', -1);
  close.meta = { deskId: true };
  return [open, inner, close];
}

export function idLinks(state) {
  const refs = state.env?.refs;
  if (!refs) return;
  for (const block of state.tokens) {
    if (block.type !== 'inline' || !block.children) continue;
    const out = [];
    let inLink = 0;
    for (const t of block.children) {
      if (t.type === 'link_open') inLink++;
      if (t.type === 'link_close') inLink--;
      if (inLink > 0) {
        out.push(t);
        continue;
      }
      if (t.type === 'code_inline') {
        const r = refs[t.content];
        out.push(...(r ? linkTokens(state, t.content, r, t) : [t]));
        continue;
      }
      if (t.type !== 'text' || !t.content.includes('-')) {
        out.push(t);
        continue;
      }
      let last = 0;
      for (const m of t.content.matchAll(ID)) {
        const r = refs[m[0]];
        if (!r) continue;
        if (m.index > last) {
          const before = new state.Token('text', '', 0);
          before.content = t.content.slice(last, m.index);
          out.push(before);
        }
        const text = new state.Token('text', '', 0);
        text.content = m[0];
        out.push(...linkTokens(state, m[0], r, text));
        last = m.index + m[0].length;
      }
      if (last === 0) out.push(t);
      else if (last < t.content.length) {
        const after = new state.Token('text', '', 0);
        after.content = t.content.slice(last);
        out.push(after);
      }
    }
    block.children = out;
  }
}
