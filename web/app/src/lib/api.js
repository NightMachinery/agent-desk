// The dashboard's API, at ./api relative to the page.

async function parse(res) {
  let body = {};
  try {
    body = await res.json();
  } catch {
    /* not JSON */
  }
  if (!res.ok) throw new Error(body.error || `${res.status} ${res.statusText}`);
  return body;
}

export async function get(name, params = {}) {
  const qs = new URLSearchParams(Object.entries(params).filter(([, v]) => v !== '' && v != null && v !== false));
  return parse(await fetch(`./api/${name}?${qs}`, { headers: { Accept: 'application/json' } }));
}

export async function post(name, body) {
  return parse(
    await fetch(`./api/${name}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-Agent-Desk': '1' },
      body: JSON.stringify(body),
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
