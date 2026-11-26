// The text side of voice notes: the tokens a recording leaves in a draft
// while it runs, and the transcript block that replaces it. No browser APIs,
// so tests import it directly.

const voicePattern = /\[voice (v-[a-z0-9]{12}) \d+:[0-5]\d, (?:recording|transcribing|failed)\]/g;

export function voiceTokens(text) {
  return [...text.matchAll(voicePattern)].map((match) => {
    const [token, id] = match;
    const start = match.index;
    return { id, token, start, end: start + token.length };
  });
}

export function voiceToken(rec) {
  const secs = Math.floor(rec.bytes / 32000);
  return `[voice ${rec.id} ${Math.floor(secs / 60)}:${String(secs % 60).padStart(2, '0')}, ${rec.status}]`;
}

export function transcriptBlock(text) {
  const trimmed = text.trim();
  let fenceLength = 3;
  for (const [run] of trimmed.matchAll(/`+/g)) fenceLength = Math.max(fenceLength, run.length + 1);
  const fence = '`'.repeat(fenceLength);
  return `${fence}speech-to-text\n${trimmed}\n${fence}`;
}

export function placeBlock(text, start, end, block) {
  const left = text.slice(0, start);
  const right = text.slice(end);
  return (left && !left.endsWith('\n\n') ? (left.endsWith('\n') ? '\n' : '\n\n') : '') + block
    + (right && !right.startsWith('\n\n') ? (right.startsWith('\n') ? '\n' : '\n\n') : '');
}

const words = (s) => new Set(s.toLowerCase().match(/[\p{L}\p{N}']+/gu) ?? []);

/** Whether text still holds a transcript block, verbatim or edited: an
 * edited block (a typo fixed, a sentence cut) keeps most of its words. */
export function keepsTranscript(text, block) {
  if (text.includes(block)) return true;
  const want = words(block.split('\n').slice(1, -1).join('\n'));
  if (!want.size) return false;
  const have = words(text);
  let kept = 0;
  for (const w of want) if (have.has(w)) kept++;
  return kept / want.size >= 0.5;
}

/** Sorts a draft's recordings ({id, block?}) at send time. used: their
 * transcript is in the text, so the message links them. ask: their token
 * went before the transcript ever reached the text, so the sender has not
 * seen it and is asked whether to put it back. A transcript the sender saw
 * and then edited or deleted, and a failed or deleted recording, are
 * neither: nothing is lost by leaving them out. status maps id to the
 * recording's last known status. */
export function sortVoice(text, draft, status = {}) {
  const pending = new Set(voiceTokens(text).map(({ id }) => id));
  const used = draft.filter((item) => item.block && keepsTranscript(text, item.block));
  const ask = draft.filter((item) => !item.block && !pending.has(item.id) && status[item.id] !== 'failed');
  return { used, ask };
}
