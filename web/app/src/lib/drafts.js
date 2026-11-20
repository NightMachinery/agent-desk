// Drafts: what the user has typed but not sent, one per box (an item's
// answer, its message box, a question's picks and notes, a node's message
// box). Each is saved in this browser at once and on the desk a moment
// later (api/draft), so a draft started on the laptop is in the same box on
// the phone. Switching items, reloading, or a notification click that
// reuses the tab loses nothing, and one item's text never shows up in
// another's box. Sending or emptying a box drops its draft everywhere.
//
// Which copy wins: a box picks up the desk's draft when it opens and when
// the tab comes back into view, if the desk's is newer than the copy here
// and this device has no edits the desk has not taken yet. Edits not yet
// on the desk win: they go up as soon as the desk can be reached.

import { get } from './api.js';

const PREFIX = 'agent-desk:draft:';
const KEEP_DAYS = 30;
const KEEP = KEEP_DAYS * 24 * 3600 * 1000;
const QUIET = 800; // ms without typing before a save goes to the desk

// A record here: { t: when it changed here (ms), v: the value, null once
// dropped, s: the desk's `updated` this copy matches (0: never synced),
// d: true while the desk lacks this copy }. Records from before drafts
// synced have only t and v, and count as not on the desk yet.
const dirty = (r) => r.d !== false;

function store() {
  try {
    return globalThis.localStorage ?? null;
  } catch {
    return null; // storage blocked: drafts live only as long as the box
  }
}

function read(key) {
  const s = store();
  if (!key || !s) return null;
  try {
    const raw = s.getItem(PREFIX + key);
    if (!raw) return null;
    const r = JSON.parse(raw);
    if (Date.now() - r.t > KEEP) {
      s.removeItem(PREFIX + key);
      return null;
    }
    return r;
  } catch {
    return null;
  }
}

function write(key, r) {
  try {
    store()?.setItem(PREFIX + key, JSON.stringify(r));
  } catch {
    // full or blocked: the box still holds the text
  }
}

function remove(key) {
  try {
    store()?.removeItem(PREFIX + key);
  } catch {
    // nothing to remove
  }
}

const same = (a, b) => JSON.stringify(a ?? null) === JSON.stringify(b ?? null);

// This browser's name on the desk, for telling devices apart in a draft.
const device = (() => {
  const s = store();
  try {
    let id = s?.getItem('agent-desk:device');
    if (!id) {
      id = Array.from(crypto.getRandomValues(new Uint8Array(6)), (b) => b.toString(16).padStart(2, '0')).join('');
      s?.setItem('agent-desk:device', id);
    }
    return id;
  } catch {
    return '';
  }
})();

/** The saved value for key, or null. */
export function loadDraft(key) {
  const r = read(key);
  return r && r.v != null ? r.v : null;
}

/** Save value under key, or drop the draft when empty is true. */
export function saveDraft(key, value, empty = false) {
  if (!key || !store()) return;
  const v = empty ? null : value;
  const r = read(key);
  if (r ? same(r.v, v) : v === null) return;
  write(key, { t: Date.now(), v, s: r?.s ?? 0, d: true });
  later(key);
}

export function clearDraft(key) {
  saveDraft(key, null, true);
}

// --- syncing with the desk ------------------------------------------------

const timers = new Map();
const inflight = new Set();
const again = new Set();

function later(key) {
  clearTimeout(timers.get(key));
  timers.set(key, setTimeout(() => push(key), QUIET));
}

async function push(key, keepalive = false) {
  clearTimeout(timers.get(key));
  timers.delete(key);
  const r = read(key);
  if (!r || !dirty(r)) return;
  if (inflight.has(key)) {
    again.add(key);
    return;
  }
  inflight.add(key);
  try {
    const res = await fetch('./api/draft', {
      method: 'POST',
      keepalive,
      headers: { 'Content-Type': 'application/json', 'X-Agent-Desk': '1' },
      body: JSON.stringify({ key, draft: r.v, device }),
    });
    const body = await res.json().catch(() => ({}));
    const now = read(key);
    if (!res.ok) {
      // A draft the desk refuses (too large, a key it does not take) stays
      // on this device only; anything else is tried again later.
      if (res.status >= 400 && res.status < 500 && ![408, 429].includes(res.status) && now && now.t === r.t) write(key, { ...now, d: false });
      return;
    }
    if (!now) return;
    if (now.t !== r.t) write(key, { ...now, s: body.updated }); // typed on meanwhile: still to go up
    else if (r.v === null) remove(key);
    else write(key, { ...now, s: body.updated, d: false });
  } catch {
    // offline: flush tries again
  } finally {
    inflight.delete(key);
    if (again.delete(key)) push(key);
  }
}

/** Send every draft the desk lacks; keepalive lets it outlive the page. */
function flush(keepalive = false) {
  const s = store();
  if (!s) return;
  try {
    for (let i = s.length - 1; i >= 0; i--) {
      const k = s.key(i);
      if (!k?.startsWith(PREFIX)) continue;
      const key = k.slice(PREFIX.length);
      const r = read(key); // also drops drafts older than KEEP_DAYS
      if (r && dirty(r)) push(key, keepalive);
    }
  } catch {
    // nothing to send
  }
}

const watchers = new Set();

async function pull(w) {
  let srv;
  try {
    srv = await get('draft', { key: w.key });
  } catch {
    return; // offline or an older desk: the copy here stands
  }
  if (!watchers.has(w)) return;
  const r = read(w.key);
  if (r && dirty(r)) return;
  if (!(srv.updated > (r?.s ?? 0))) return;
  if (srv.value == null) {
    remove(w.key);
    if (!r || r.v == null) return;
  } else {
    write(w.key, { t: Date.now(), v: srv.value, s: srv.updated, d: false });
  }
  w.apply(srv.value ?? null);
}

/**
 * Keep the box for key in step with the desk: apply(value) runs when the
 * desk has a newer draft (null: dropped elsewhere) than this device, now
 * and whenever the tab comes back into view. Returns the function that
 * stops it.
 */
export function watchDraft(key, apply) {
  if (!key || !store()) return () => {};
  const w = { key, apply };
  watchers.add(w);
  pull(w);
  return () => watchers.delete(w);
}

if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'hidden') {
      for (const key of [...timers.keys()]) push(key, true);
    } else {
      flush();
      for (const w of watchers) pull(w);
    }
  });
  window.addEventListener('pagehide', () => {
    for (const key of [...timers.keys()]) push(key, true);
  });
  window.addEventListener('online', () => flush());
  // Drafts typed before this page loaded (offline, or before drafts
  // synced) go up once the page has settled.
  setTimeout(() => flush(), 2000);
}
