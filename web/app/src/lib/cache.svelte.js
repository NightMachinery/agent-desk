import { get } from './api.js';

export const cache = $state({ state: null });
export const coldCache = $state({ open: false, enabled: false });
let resolveChoice;
let loading;

export function refreshCache() {
  if (!loading) {
    loading = get('cache', {}, AbortSignal.timeout(4500))
      .then((result) => { cache.state = result; })
      .catch(() => { cache.state = null; })
      .finally(() => { loading = null; });
  }
  return loading;
}

export function watchCache() {
  refreshCache();
  const refresh = () => {
    if (!document.hidden) refreshCache();
  };
  const timer = setInterval(refresh, 8000);
  document.addEventListener('visibilitychange', refresh);
  return () => {
    clearInterval(timer);
    document.removeEventListener('visibilitychange', refresh);
  };
}

// Whether a send would reach a cold cache, asked while the user types
// (warmSendCache), so Send has the answer at once and the page acts on the
// click without a round trip first. A minute old at most; a send makes the
// cache warm, so it is asked again after one.
const FRESH_MS = 60_000;
const known = new Map();
function ask(kind, target) {
  const k = kind + ' ' + target;
  const hit = known.get(k);
  if (hit && Date.now() - hit.at < FRESH_MS) return hit.p;
  const p = get('cache', { kind, target }, AbortSignal.timeout(4500));
  known.set(k, { at: Date.now(), p });
  p.catch(() => known.delete(k));
  return p;
}

/** Asks now whether sending kind to target would reach a cold cache, for
 * chooseSendCache to answer from. */
export function warmSendCache(kind, target) {
  if (target) ask(kind, target).catch(() => {});
}

export async function chooseSendCache(kind, target) {
  if (coldCache.open) return 'cancel';
  let status;
  try {
    status = await ask(kind, target);
  } catch {
    return 'send';
  } finally {
    known.delete(kind + ' ' + target);
  }
  if (coldCache.open) return 'cancel';
  if (!status.cold) return 'send';
  return new Promise((resolve) => {
    resolveChoice = resolve;
    coldCache.enabled = status.enabled;
    coldCache.open = true;
  });
}

export function finishCacheChoice(choice) {
  coldCache.open = false;
  resolveChoice?.(choice);
  resolveChoice = null;
}
