// The dashboard's API, at ./api relative to the page.

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
  return body;
}

export async function get(name, params = {}) {
  const qs = new URLSearchParams(Object.entries(params).filter(([, v]) => v !== '' && v != null && v !== false));
  return parse(await fetch(`./api/${name}?${qs}`, { headers: { Accept: 'application/json' } }));
}

/** @param {AbortSignal} [signal] */
export async function post(name, body, signal) {
  return parse(
    await fetch(`./api/${name}`, {
      method: 'POST',
      signal,
      headers: { 'Content-Type': 'application/json', 'X-Agent-Desk': '1' },
      body: JSON.stringify(body),
    }),
  );
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

/** Calls onChange whenever the store changes; reports the connection. */
export function watch(onChange, onStatus) {
  let es;
  let closed = false;
  function open() {
    es = new EventSource('./api/stream');
    es.addEventListener('hello', () => onStatus('live'));
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
