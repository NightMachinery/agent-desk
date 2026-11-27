// The dashboard's API, at ./api relative to the page.
import { addRefs } from './refs.svelte.js';

// The last requests, oldest first, for an error report's log
// (errors.svelte.js): when, what, the answer's status and how long it took.
const LOG_MAX = 40;
export const apiLog = /** @type {{at: number, method: string, name: string, what: string, status: number, ms: number, error: string}[]} */ ([]);

// Requests safe to send again as they were: Retry in the error sheet
// offers itself only for these.
const AGAIN = new Set(['read', 'unread', 'pin', 'unpin', 'defer', 'undefer', 'seen', 'pref', 'dashboard-prefs', 'cache', 'render', 'sets', 'agent']);

// A request's arguments for the log, long strings cut short.
const brief = (v) => {
  try {
    return JSON.stringify(v, (_, x) => (typeof x === 'string' && x.length > 80 ? x.slice(0, 80) + '…' : x)) ?? '';
  } catch {
    return '';
  }
};

/** fetch, logged; a failure is an Error carrying the request (request:
 * {method, name, what, status, at}), its status (0: the desk was not
 * reached) and, for requests safe to repeat, retry(). */
async function call(method, name, url, init, args, again = null) {
  const entry = { at: Date.now(), method, name, what: brief(args), status: 0, ms: 0, error: '' };
  apiLog.push(entry);
  if (apiLog.length > LOG_MAX) apiLog.shift();
  try {
    const res = await fetch(url, init);
    entry.status = res.status;
    return await parse(res);
  } catch (err) {
    const e = /** @type {Error & {status?: number}} */ (err instanceof Error ? err : new Error(String(err)));
    if (!('status' in e)) Object.assign(e, { status: 0 }); // no answer: offline, or the desk is down
    entry.error = e.message;
    Object.assign(e, { request: { method, name, what: entry.what, status: e.status, at: entry.at } });
    if (again && AGAIN.has(name)) Object.assign(e, { retry: again });
    throw e;
  } finally {
    entry.ms = Date.now() - entry.at;
  }
}

async function parse(res) {
  let body = {};
  try {
    body = await res.json();
  } catch {
    /* not JSON */
  }
  if (!res.ok) {
    throw Object.assign(new Error(body.error || `${res.status} ${res.statusText}`), { status: res.status });
  }
  if (body?.refs) addRefs(body.refs);
  return body;
}

/** @param {AbortSignal} [signal] */
export async function get(name, params = {}, signal) {
  const qs = new URLSearchParams(Object.entries(params).filter(([, v]) => v !== '' && v != null && v !== false));
  return call('GET', name, `./api/${name}?${qs}`, { signal, headers: { Accept: 'application/json' } }, params);
}

/** @param {AbortSignal} [signal] */
export async function post(name, body, signal) {
  const init = { method: 'POST', signal, headers: { 'Content-Type': 'application/json', 'X-Agent-Desk': '1' }, body: JSON.stringify(body) };
  return call('POST', name, `./api/${name}`, init, body, () => post(name, body));
}

/** Stores a file the user attaches; returns {ref, view}. */
export async function upload(file) {
  const qs = new URLSearchParams({ name: file.name || 'pasted-file' });
  return parse(
    await fetch(`./api/upload?${qs}`, {
      method: 'POST',
      headers: { 'Content-Type': file.type || 'application/octet-stream', 'X-Agent-Desk': '1' },
      body: file,
    }),
  );
}

export async function uploadVoice(name, offset, audio) {
  return parse(await fetch(`./api/${name}/chunk?offset=${offset}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/octet-stream', 'X-Agent-Desk': '1' },
    body: audio,
  }));
}

/** Calls onChange whenever the store changes; reports the connection, the
 * server's build and the version of the user's keys file.
 * @param {() => void} onChange
 * @param {(status: string) => void} onStatus
 * @param {(build: string) => void} [onBuild]
 * @param {(stamp: string) => void} [onKeys] */
export function watch(onChange, onStatus, onBuild = () => {}, onKeys = () => {}) {
  let es;
  let closed = false;
  function open() {
    es = new EventSource('./api/stream');
    es.addEventListener('hello', () => onStatus('live'));
    es.addEventListener('build', (ev) => onBuild(ev.data));
    es.addEventListener('keys', (ev) => onKeys(ev.data));
    es.addEventListener('change', () => onChange());
    es.onerror = () => {
      onStatus('reconnecting');
      if (es.readyState === EventSource.CLOSED && !closed) setTimeout(open, 3000);
    };
  }
  open();
  return () => {
    closed = true;
    es.close();
  };
}
