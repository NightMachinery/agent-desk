// y m: the open item's full text as Markdown. The title becomes a heading;
// the body is Markdown already and is kept as written; a question adds its
// default and its multiple-choice part, which the page shows outside the
// body.

/**
 * @param {{ title?: string, body?: string, meta?: any }} e the item's event
 * @param {string} [dflt] a question's default, if it has one
 */
export function itemMarkdown(e, dflt = '') {
  const parts = [];
  if (e.title?.trim()) parts.push(`# ${e.title.trim()}`);
  if (e.body?.trim()) parts.push(e.body.trim());
  if (dflt?.trim()) parts.push(`**Default:** ${dflt.trim()}`);
  const qs = e.meta?.choices?.questions ?? [];
  qs.forEach((/** @type {any} */ q, /** @type {number} */ i) => {
    const opts = (q.options ?? []).map((/** @type {any} */ o) => `- **${o.key}:** ${o.label}${o.key === q.recommended ? ' (recommended)' : ''}`);
    parts.push([`${i + 1}. ${String(q.text ?? '').trim()}`, '', ...opts].join('\n'));
  });
  return parts.join('\n\n');
}
