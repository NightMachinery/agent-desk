// The view is the URL: #/p/<path>?session=&host=&all=1|show=hour&q=&sort=old|read&item=&settings=1&verbatim=1&tasks=1.
// Hash routing keeps the page at the root of wherever it is mounted.
//
// The list shows the unread items (the default), with hour also those read
// in the last hour, or with all every one. sort is '' (newest first), 'old'
// (oldest first) or 'read' (most recently read first, the unread on top),
// which the unread-only list cannot use.

export const route = $state({ path: '', item: '', q: '', session: '', host: '', all: false, hour: false, sort: '', settings: false, verbatim: false, tasks: false });

/** The list's show mode: 'unread', 'hour' or 'all'. */
export const showMode = () => (route.all ? 'all' : route.hour ? 'hour' : 'unread');
/** The sort in effect: read order needs read items listed. */
export const sortMode = () => (route.sort === 'read' && showMode() === 'unread' ? '' : route.sort);
/** The route parts that switch the show mode, dropping a read sort the
 * unread-only list cannot use. */
export const showOver = (mode) => ({ all: mode === 'all', hour: mode === 'hour', ...(mode === 'unread' && route.sort === 'read' ? { sort: '' } : {}) });

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
  route.all = params.get('all') === '1' || params.get('show') === 'all';
  route.hour = !route.all && params.get('show') === 'hour';
  route.sort = ['old', 'read'].includes(params.get('sort') ?? '') ? /** @type {string} */ (params.get('sort')) : '';
  route.settings = params.get('settings') === '1';
  route.verbatim = params.get('verbatim') === '1';
  route.tasks = params.get('tasks') === '1';
}

parse();
window.addEventListener('hashchange', parse);

/** A page (settings, the verbatim log, tasks) fills the detail pane. */
export const pageOpen = () => route.settings || route.verbatim || route.tasks;

/** The URL of the current view with some parts replaced. A page (settings,
 * the verbatim log) is left by any link that does not ask for it. */
export function href(over = {}) {
  const r = { ...route, settings: false, verbatim: false, tasks: false, ...over };
  const params = new URLSearchParams();
  if (r.session) params.set('session', r.session);
  if (r.host) params.set('host', r.host);
  if (r.all) params.set('all', '1');
  else if (r.hour) params.set('show', 'hour');
  if (r.q) params.set('q', r.q);
  if (r.sort) params.set('sort', r.sort);
  if (r.item) params.set('item', r.item);
  if (r.settings) params.set('settings', '1');
  if (r.verbatim) params.set('verbatim', '1');
  if (r.tasks) params.set('tasks', '1');
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
