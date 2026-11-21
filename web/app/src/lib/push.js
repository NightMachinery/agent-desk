// Web Push for this browser: the parts the Settings control and the nudge
// on the list share. The server names a device by the first 6 bytes of its
// endpoint's SHA-256 (internal/cli/push.go).
import { get, post } from './api.js';
import { copy } from './format.js';
import { toast } from './toast.svelte.js';

export const pushSupported =
  typeof window !== 'undefined' &&
  window.isSecureContext &&
  'serviceWorker' in navigator &&
  'PushManager' in window &&
  'Notification' in window;

export const ios = typeof navigator !== 'undefined' && /iPhone|iPad/.test(navigator.userAgent);
/** Brave, which hides itself in its user agent but not in navigator. */
export const brave = typeof navigator !== 'undefined' && 'brave' in navigator;
/**
 * Brave's settings filtered to its push toggle, "Use Google services for
 * push messaging" (off as shipped: Brave has no push service but Google's).
 * A page cannot open brave:// itself, so the dashboard offers to copy it.
 */
export const braveSetting = 'brave://settings/?search=push%20messaging';

// --- what happened, for Copy logs ----------------------------------------------
// The steps of the last Turn on, kept on the device so they survive a reload.
const TRACE = 'agent-desk:push-trace';
let trace = [];
try {
  trace = JSON.parse(localStorage.getItem(TRACE) || '[]');
} catch {}
function step(what, detail = '') {
  trace.push(`${new Date().toISOString()} ${what}${detail ? ': ' + detail : ''}`);
  try {
    localStorage.setItem(TRACE, JSON.stringify(trace));
  } catch {}
}
const errText = (e) => (e?.name && e.name !== 'Error' ? `${e.name}: ${e.message}` : String(e?.message ?? e));
const hostOf = (endpoint) => {
  try {
    return new URL(endpoint).host; // not the whole endpoint: it is the device's address
  } catch {
    return '?';
  }
};

/** A failed Turn on: what to do about it (message), and the browser's own words (raw). */
export class PushError extends Error {
  constructor(message, raw = '') {
    super(message);
    this.raw = raw;
  }
}

/** The browser's error from subscribing, as what to do about it. */
function explain(e) {
  const raw = errText(e);
  if (e?.name === 'AbortError' && /push service/i.test(e.message ?? ''))
    return new PushError(
      brave
        ? `Brave sends push only through Google's push service, which it leaves off. Turn on "Use Google services for push messaging" (paste ${braveSetting} into the address bar), relaunch Brave, then Turn on again.`
        : 'The browser could not reach its push service (Google\'s, for Chrome). A VPN, firewall or blocker may be in the way: check that, then Turn on again.',
      raw,
    );
  if (e?.name === 'NotAllowedError') return new PushError('The browser refused: notifications are blocked for this site. Allow them in the site settings, then Turn on again.', raw);
  if (e?.name === 'InvalidStateError') return new PushError('The page\'s service worker is not ready. Reload the page, then Turn on again.', raw);
  return new PushError(raw, raw);
}

export async function idOf(endpoint) {
  const b = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(endpoint));
  return [...new Uint8Array(b).slice(0, 6)].map((x) => x.toString(16).padStart(2, '0')).join('');
}
function keyBytes(b64) {
  const s = atob(b64.replace(/-/g, '+').replace(/_/g, '/'));
  return Uint8Array.from(s, (c) => c.charCodeAt(0));
}
function sameKey(buf, b64) {
  if (!buf) return false;
  const a = new Uint8Array(buf);
  const b = keyBytes(b64);
  return a.length === b.length && a.every((x, i) => x === b[i]);
}
export async function current() {
  const reg = await navigator.serviceWorker.ready;
  return [reg, await reg.pushManager.getSubscription()];
}

export function deviceLabel() {
  const ua = navigator.userAgent;
  const os = /Android/.test(ua) ? 'Android' : ios ? 'iOS' : /Mac OS X/.test(ua) ? 'macOS' : /Windows/.test(ua) ? 'Windows' : /Linux/.test(ua) ? 'Linux' : 'a device';
  const br = brave ? 'Brave' : /Firefox\//.test(ua) ? 'Firefox' : /Edg\//.test(ua) ? 'Edge' : /Chrome\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : 'a browser';
  const app = matchMedia('(display-mode: standalone)').matches ? ', installed app' : '';
  return `${br} on ${os}${app}`;
}

/** Whether this browser holds a subscription the desk knows. info is GET api/push. */
export async function subscribedHere(info) {
  if (!pushSupported || !info) return false;
  const [, sub] = await current();
  if (!sub) return false;
  const id = await idOf(sub.endpoint);
  return info.devices.some((d) => d.id === id);
}

/**
 * Makes sure the desk knows this browser's subscription. When the browser
 * holds one for the desk's key that the server does not list (it dropped
 * it, or a re-subscription never reached it), registers it again without
 * asking: permission was granted when it was made. Returns its id, or ''
 * when the browser holds no usable subscription (the user must Turn on).
 */
export async function ensureRegistered(info) {
  if (!pushSupported || !info || Notification.permission !== 'granted') return '';
  const [, sub] = await current();
  if (!sub || !sameKey(sub.options.applicationServerKey, info.key)) return '';
  const id = await idOf(sub.endpoint);
  if (info.devices.some((d) => d.id === id)) return id;
  await post('push', { action: 'subscribe', subscription: sub.toJSON(), label: deviceLabel() });
  return (await confirmListed(id)) ? id : '';
}

/** Whether GET api/push lists device id. */
async function confirmListed(id) {
  const info = await get('push');
  return info.devices.some((d) => d.id === id);
}

/**
 * Asks for permission if needed, subscribes and registers this device. It
 * fails with a PushError saying what to do, and records each step for
 * pushReport.
 */
export async function subscribeHere(info) {
  trace = [];
  step('Turn on', deviceLabel());
  try {
    info ??= await get('push');
    step('desk', `key of ${info.key.length} chars, push ${info.off ? 'off in the config' : 'on'}, ${info.devices.length} device(s)`);
    const perm = await Notification.requestPermission();
    step('permission', perm);
    if (perm !== 'granted')
      throw new PushError(perm === 'denied' ? 'Notifications are blocked for this site: allow them in the site settings, then Turn on again.' : 'Notifications were not allowed.', perm);
    const [reg, old] = await current();
    step('service worker', `${reg.active?.state ?? 'none active'}, scope ${reg.scope}`);
    let sub = old;
    // A subscription made for another key cannot carry the desk's pushes.
    if (sub && !sameKey(sub.options.applicationServerKey, info.key)) {
      step('old subscription', `${hostOf(sub.endpoint)}, for another key: dropped`);
      await sub.unsubscribe();
      sub = null;
    } else if (sub) step('old subscription', `${hostOf(sub.endpoint)}, kept`);
    if (!sub) {
      step('subscribing');
      sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(info.key) });
      step('subscribed', hostOf(sub.endpoint));
    }
    await post('push', { action: 'subscribe', subscription: sub.toJSON(), label: deviceLabel() });
    const id = await idOf(sub.endpoint);
    step('desk told', id);
    // Only a subscription the desk lists gets pushes: check before saying so.
    if (!(await confirmListed(id)))
      throw new PushError(`The desk did not keep this device's subscription (${id}). Try again; Copy logs shows the desk's push log.`);
    step('done: the desk lists it');
    return id;
  } catch (e) {
    const err = e instanceof PushError ? e : explain(e);
    step('failed', err.raw || err.message);
    throw err;
  }
}

/**
 * The report Copy logs copies: this browser and its push state, the steps
 * of the last Turn on, and the desk's devices and push log. It holds no
 * endpoint (a device's push address), only its host and device id.
 */
export async function pushReport() {
  const yes = (b) => (b ? 'yes' : 'no');
  const lines = [`agent-desk push report, ${new Date().toISOString()}`];
  const add = (k, v) => lines.push(`${k}: ${v}`);
  add('page', location.origin + location.pathname + (matchMedia('(display-mode: standalone)').matches ? ' (installed app)' : ''));
  add('browser', `${deviceLabel()}; ${navigator.userAgent}`);
  add('support', `secure context ${yes(window.isSecureContext)}, service worker ${yes('serviceWorker' in navigator)}, PushManager ${yes('PushManager' in window)}, Notification ${yes('Notification' in window)}`);
  if ('Notification' in window) add('notification permission', Notification.permission);
  if ('serviceWorker' in navigator) {
    try {
      const reg = await navigator.serviceWorker.getRegistration();
      add('service worker', reg ? `${reg.active?.state ?? 'none active'} ${reg.active?.scriptURL ?? ''}, scope ${reg.scope}` : 'not registered');
      if (reg && 'pushManager' in reg) {
        add('push permission', await reg.pushManager.permissionState({ userVisibleOnly: true }).catch(errText));
        const sub = await reg.pushManager.getSubscription();
        add('subscription', sub ? `${hostOf(sub.endpoint)}, device ${await idOf(sub.endpoint)}` : 'none');
      }
    } catch (e) {
      add('service worker', `cannot read: ${errText(e)}`);
    }
  }
  lines.push('', 'last Turn on on this device:', ...(trace.length ? trace.map((l) => '  ' + l) : ['  (none)']));
  try {
    const info = await get('push', { log: '1' });
    lines.push('', `desk on ${info.host}: push ${info.off ? 'off in the config' : 'on'}; devices:`);
    for (const d of info.devices) lines.push(`  ${d.id} ${d.label}${d.last_ok ? `, last push ${d.last_ok}` : ''}${d.last_error ? `, failed ${d.last_error}` : ''}`);
    lines.push('', 'desk push/log, last lines:', ...(info.log?.length ? info.log.map((l) => '  ' + l) : ['  (empty)']));
  } catch (e) {
    lines.push('', `desk: cannot read api/push: ${errText(e)}`);
  }
  return lines.join('\n') + '\n';
}

/** Copies pushReport and says so; returns whether it copied, and the report. */
export async function copyPushLogs() {
  const text = await pushReport();
  const ok = await copy(text);
  toast(ok ? 'Push logs copied.' : 'The browser would not copy the push logs.', ok ? 'ok' : 'error', 6000);
  return { ok, text };
}
