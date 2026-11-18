import { SvelteMap } from 'svelte/reactivity';
import { post } from './api.js';
import { toast } from './toast.svelte.js';

const prefix = 'agent-desk:send:';
const messages = new SvelteMap();
const sending = new Set();
let refresh = () => {};

function save(message) {
  localStorage.setItem(prefix + message.key, JSON.stringify(message));
}

function remove(key) {
  try {
    localStorage.removeItem(prefix + key);
  } catch {
    // The same key is safe to replay if a stale copy survives.
  }
}

function restore() {
  try {
    for (let i = 0; i < localStorage.length; i++) {
      const k = localStorage.key(i);
      if (!k?.startsWith(prefix) || messages.has(k.slice(prefix.length))) continue;
      try {
        const message = JSON.parse(localStorage.getItem(k));
        if (message && ['answer', 'reply'].includes(message.kind) && k === prefix + message.key) {
          messages.set(message.key, message);
        }
      } catch {
        // Ignore a corrupt entry and keep reading the remaining queue.
      }
    }
  } catch {
    // Local storage can be disabled by the browser.
  }
}

export function startOutbox(onRecorded) {
  refresh = onRecorded;
  restore();
  flush();
  const online = () => flush(true);
  window.addEventListener('online', online);
  const timer = setInterval(flush, 3000);
  return () => {
    refresh = () => {};
    clearInterval(timer);
    window.removeEventListener('online', online);
  };
}

export function queueMessage(kind, body, target = body.id || body.to) {
  const bytes = crypto.getRandomValues(new Uint8Array(16));
  const key = Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('');
  const message = { key, kind, target, body: { ...body, send_key: key }, time: Date.now() };
  try {
    save(message);
  } catch {
    toast('This browser could not save the send queue. Your draft is still in the composer.', 'error');
    return false;
  }
  messages.set(key, message);
  toast(kind === 'answer' ? 'Answer queued.' : 'Message queued.', 'info');
  flush();
  return true;
}

export function pendingMessages(targets, entries = []) {
  const existing = new Set(entries.map((entry) => entry.event.meta?.send_key));
  return [...messages.values()]
    .filter((message) => targets.includes(message.target) && !existing.has(message.key))
    .sort((a, b) => a.time - b.time);
}

export function acknowledgeMessages(entries) {
  for (const entry of entries) {
    const key = entry.event.meta?.send_key;
    if (key && messages.has(key)) {
      remove(key);
      messages.delete(key);
    }
  }
}

export function retryPost(key) {
  const message = messages.get(key);
  if (message) messages.set(key, { ...message, blocked: false, next: 0 });
  flush();
}

function flush(force = false) {
  for (const message of messages.values()) {
    if (!message.recorded && !message.blocked && !sending.has(message.key) && (force || Date.now() >= (message.next || 0))) {
      send(message);
    }
  }
}

async function send(message) {
  sending.add(message.key);
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 15_000);
  try {
    const res = await post(message.kind, message.body, controller.signal);
    if (res.status !== 'queued' || !res.event_id) throw new Error('No queue acknowledgement from the desk.');
    remove(message.key);
    if (messages.has(message.key)) messages.set(message.key, { ...message, recorded: true, error: '' });
    refresh();
  } catch (err) {
    if (!messages.has(message.key)) return;
    const blocked = err.status >= 400 && err.status < 500 && ![408, 429].includes(err.status);
    const failures = (message.failures || 0) + 1;
    const next = Date.now() + Math.min(30_000, 3000 * 2 ** Math.min(failures - 1, 4));
    const updated = { ...message, blocked, error: err.message, failures, next };
    messages.set(message.key, updated);
    try {
      save(updated);
    } catch {
      // The original queued message is already persisted.
    }
    if (blocked) toast(err.message, 'error');
  } finally {
    clearTimeout(timer);
    sending.delete(message.key);
  }
}

