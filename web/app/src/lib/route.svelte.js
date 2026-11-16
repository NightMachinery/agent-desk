// The view is the URL: #/p/<path>?session=&host=&all=1&q=&item=. Hash
// routing keeps the page at the root of wherever it is mounted.

export const route = $state({ path: '', item: '', q: '', session: '', host: '', all: false });

function parse() {
  const raw = location.hash.replace(/^#/, '') || '/';
  const [p, qs = ''] = raw.split('?');
  const params = new URLSearchParams(qs);
  let path = '';
  let item = params.get('item') || '';
  if (p.startsWith('/p/')) path = p.slice(3).split('/').map(decodeURIComponent).filter(Boolean).join('/');
  else if (p.startsWith('/i/')) item = decodeURIComponent(p.slice(3));
  route.path = path;
  route.item = item;
  route.q = params.get('q') || '';
  route.session = params.get('session') || '';
  route.host = params.get('host') || '';
  route.all = params.get('all') === '1';
}

parse();
window.addEventListener('hashchange', parse);

/** The URL of the current view with some parts replaced. */
export function href(over = {}) {
  const r = { ...route, ...over };
  const params = new URLSearchParams();
  if (r.session) params.set('session', r.session);
  if (r.host) params.set('host', r.host);
  if (r.all) params.set('all', '1');
  if (r.q) params.set('q', r.q);
  if (r.item) params.set('item', r.item);
  const base = r.path ? '#/p/' + r.path.split('/').map(encodeURIComponent).join('/') : '#/';
  const qs = params.toString();
  return base + (qs ? '?' + qs : '');
}

/** Go to the current view with some parts replaced. */
export function go(over = {}, { replace = false } = {}) {
  const url = href(over);
  if (url === location.hash) return;
  if (replace) {
    history.replaceState(null, '', url);
    parse();
  } else {
    location.hash = url;
  }
}
