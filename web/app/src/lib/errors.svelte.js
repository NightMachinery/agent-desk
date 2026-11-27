// Failures of what the user asked for, shown one at a time in the error
// sheet (ErrorSheet.svelte): what could not be done, why in words, the
// desk's own message, and what can be done now (Retry when the request is
// safe to send again, an action the caller offers such as putting a message
// back in its box, Copy log, Dismiss). The same failure again while it is
// up counts instead of stacking.
//
// failed(err, what, opts): err as thrown (api.js puts the request on it);
// what, a phrase after "Couldn't" (default: from the request's name);
// opts.actions [{label, run}]; opts.retry, a function redoing the whole
// action, instead of sending the request again.
import { apiLog } from './api.js';
import { build } from './build.svelte.js';

export const errors = $state({ list: /** @type {any[]} */ ([]) });
let next = 0;

// What a request does, after "Couldn't".
const WHAT = {
  read: 'mark it read',
  unread: 'mark it unread',
  pin: 'pin it',
  unpin: 'unpin it',
  defer: 'defer it',
  undefer: 'undefer it',
  answer: 'send your answer',
  reply: 'send your message',
  task: 'change the task',
  quota: 'act on the agent',
  agent: 'archive the agent',
  sets: 'save the project set',
  screen: 'answer the screen request',
  deliver: 'retry the delivery',
  'dashboard-prefs': 'save the setting',
  pref: 'save the setting',
  render: 'render the org files',
  push: 'change push notifications',
  cache: 'change the cache policy',
  recordings: 'change the recording',
  target: 'find who to ask',
};

export function failed(err, what = '', { actions = [], retry = null } = {}) {
  const e = err instanceof Error ? err : new Error(String(err));
  const req = /** @type {any} */ (e).request ?? null;
  const title = what || WHAT[req?.name] || 'do that';
  const key = `${title}\n${e.message}`;
  const same = errors.list.find((x) => x.key === key);
  if (same) {
    same.count++;
    same.at = Date.now();
    return;
  }
  errors.list.push({
    id: ++next,
    key,
    what: title,
    message: e.message,
    status: /** @type {any} */ (e).status ?? null,
    request: req,
    at: Date.now(),
    retry: retry ?? /** @type {any} */ (e).retry ?? null,
    actions,
    count: 1,
  });
}

export function dismissError(id) {
  const i = errors.list.findIndex((x) => x.id === id);
  if (i >= 0) errors.list.splice(i, 1);
}

/** Why it failed, in words, from the status. */
export function explain(x) {
  const s = x.status;
  if (s === 0) return 'The desk could not be reached, so nothing changed there. Check the connection, or that the desk is running, and try again.';
  if (s === 403) return 'The desk refused a request from this page.';
  if (s === 503) return 'The desk could not do it now.';
  if (s >= 500) return 'The desk failed while doing it.';
  if (s >= 400) return 'The desk refused it.';
  return '';
}

const iso = (ms) => new Date(ms).toISOString();

/** The text Copy log puts on the clipboard: the failure, the page, and the
 * last requests (arguments cut short), to paste to an agent or an issue. */
export function errorReport(x) {
  const lines = [
    `agent-desk dashboard: couldn't ${x.what}`,
    `error: ${x.message}${x.status != null ? ` (status ${x.status})` : ''}${x.count > 1 ? `, ${x.count} times` : ''}`,
    `when: ${iso(x.at)}`,
  ];
  if (x.request) lines.push(`request: ${x.request.method} api/${x.request.name} ${x.request.what}`);
  lines.push(`page: ${location.pathname}${location.hash}`, `build: ${build.mine || 'unknown'}${build.stale ? ` (desk serves ${build.served})` : ''}`, `browser: ${navigator.userAgent}`, '', 'recent requests, oldest first:');
  for (const r of apiLog.slice(-25)) {
    lines.push(`${iso(r.at)} ${r.method} api/${r.name} ${r.status || 'no answer'} ${r.ms}ms ${r.what}${r.error ? ` -> ${r.error}` : ''}`);
  }
  return lines.join('\n');
}
