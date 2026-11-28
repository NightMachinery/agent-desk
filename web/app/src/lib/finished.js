// The node panel's Recently finished: a node's tasks done or dropped in a
// span, newest first, each shown with the first line of its note. It stands
// where the retired done slot stood (q-42mm): what an agent finished, and
// how, is its tasks' notes.

/** Tasks finished (done or dropped) within span ms before now, newest first. */
export function recentlyFinished(tasks, span, now) {
  return (tasks ?? [])
    .filter((t) => (t.state === 'done' || t.state === 'dropped') && now - Date.parse(t.updated) <= span)
    .sort((a, b) => Date.parse(b.updated) - Date.parse(a.updated));
}

/** The first line of a note with text, without a leading heading, list or quote mark. */
export function noteLine(note) {
  const line = (note ?? '').split('\n').find((l) => l.trim()) ?? '';
  return line.trim().replace(/^(#{1,6}|[-*+>]|\d+[.)])\s+/, '');
}

/** How long before now ts was, short: "just now", "4 min", "3 h", "2 d". */
export function agoShort(ts, now) {
  const m = Math.max(0, Math.round((now - Date.parse(ts)) / 60e3));
  return m < 1 ? 'just now' : m < 60 ? `${m} min` : m < 48 * 60 ? `${Math.round(m / 60)} h` : `${Math.round(m / 1440)} d`;
}
