// The view is the URL: #/p/<path>?session=&host=&all=1&q=&sort=old&item=&settings=1&verbatim=1. Hash
// routing keeps the page at the root of wherever it is mounted.

export const route = $state({ path: '', item: '', q: '', session: '', host: '', all: false, old: false, settings: false, verbatim: false });

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
  route.old = params.get('sort') === 'old';
  route.settings = params.get('settings') === '1';
  route.verbatim = params.get('verbatim') === '1';
}

parse();
window.addEventListener('hashchange', parse);

/** A page (settings, the verbatim log) fills the detail pane. */
export const pageOpen = () => route.settings || route.verbatim;

/** The URL of the current view with some parts replaced. A page (settings,
 * the verbatim log) is left by any link that does not ask for it. */
export function href(over = {}) {
  const r = { ...route, settings: false, verbatim: false, ...over };
  const params = new URLSearchParams();
  if (r.session) params.set('session', r.session);
  if (r.host) params.set('host', r.host);
  if (r.all) params.set('all', '1');
  if (r.q) params.set('q', r.q);
  if (r.old) params.set('sort', 'old');
  if (r.item) params.set('item', r.item);
  if (r.settings) params.set('settings', '1');
  if (r.verbatim) params.set('verbatim', '1');
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
