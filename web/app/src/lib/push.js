// Web Push for this browser: the parts the Settings control and the nudge
// on the list share. The server names a device by the first 6 bytes of its
// endpoint's SHA-256 (internal/cli/push.go).
import { get, post } from './api.js';

export const pushSupported =
  typeof window !== 'undefined' &&
  window.isSecureContext &&
  'serviceWorker' in navigator &&
  'PushManager' in window &&
  'Notification' in window;

export const ios = typeof navigator !== 'undefined' && /iPhone|iPad/.test(navigator.userAgent);

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
  const br = /Firefox\//.test(ua) ? 'Firefox' : /Edg\//.test(ua) ? 'Edge' : /Chrome\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : 'a browser';
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

/** Asks for permission if needed, subscribes and registers this device. */
export async function subscribeHere(info) {
  info ??= await get('push');
  const perm = await Notification.requestPermission();
  if (perm !== 'granted')
    throw new Error(perm === 'denied' ? 'Notifications are blocked for this site: allow them in the site settings, then try again.' : 'Notifications were not allowed.');
  const [reg, old] = await current();
  let sub = old;
  // A subscription made for another key cannot carry the desk's pushes.
  if (sub && !sameKey(sub.options.applicationServerKey, info.key)) {
    await sub.unsubscribe();
    sub = null;
  }
  if (!sub) sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(info.key) });
  await post('push', { action: 'subscribe', subscription: sub.toJSON(), label: deviceLabel() });
  const id = await idOf(sub.endpoint);
  // Only a subscription the desk lists gets pushes: check before saying so.
  if (!(await confirmListed(id)))
    throw new Error(`The desk did not keep this device's subscription (${id}). Try again; the desk's push/log says what happened to it.`);
  return id;
}
