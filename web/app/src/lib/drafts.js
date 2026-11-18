// Drafts: what the user has typed but not sent, kept per box (an item's
// answer, its message box, a question's picks, a node's message box) in
// localStorage. Switching items, reloading, or a notification click that
// reuses the tab loses nothing, and one item's text never shows up in
// another's box. A draft is dropped when it is sent, when it is emptied, or
// after KEEP_DAYS.

const PREFIX = 'agent-desk:draft:';
const KEEP_DAYS = 30;
const KEEP = KEEP_DAYS * 24 * 3600 * 1000;

function store() {
  try {
    return globalThis.localStorage ?? null;
  } catch {
    return null; // storage blocked: drafts live only as long as the box
  }
}

/** The saved value for key, or null. */
export function loadDraft(key) {
  const s = store();
  if (!key || !s) return null;
  try {
    const raw = s.getItem(PREFIX + key);
    if (!raw) return null;
    const d = JSON.parse(raw);
    if (Date.now() - d.t > KEEP) {
      s.removeItem(PREFIX + key);
      return null;
    }
    return d.v;
  } catch {
    return null;
  }
}

/** Save value under key, or drop the draft when empty is true. */
export function saveDraft(key, value, empty = false) {
  const s = store();
  if (!key || !s) return;
  try {
    if (empty) s.removeItem(PREFIX + key);
    else s.setItem(PREFIX + key, JSON.stringify({ t: Date.now(), v: value }));
  } catch {
    // full or blocked: the box still holds the text
  }
}

export function clearDraft(key) {
  saveDraft(key, null, true);
}

// Drop drafts older than KEEP_DAYS once per page load.
(() => {
  const s = store();
  if (!s) return;
  try {
    for (let i = s.length - 1; i >= 0; i--) {
      const k = s.key(i);
      if (k?.startsWith(PREFIX)) loadDraft(k.slice(PREFIX.length));
    }
  } catch {
    // nothing to prune
  }
})();
