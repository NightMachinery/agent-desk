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

export async function chooseSendCache(kind, target) {
  if (coldCache.open) return 'cancel';
  let status;
  try {
    status = await get('cache', { kind, target }, AbortSignal.timeout(4500));
  } catch {
    return 'send';
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
