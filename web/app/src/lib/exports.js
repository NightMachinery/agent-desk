// Org exports (GET api/export, orgfiles.go): one scope of the desk as one
// .org file, or with zip the files <desk>/render holds.

/** The export address for a scope: {scope: 'node', path}, {scope: 'item', id},
 * {scope: 'inbox', path}, {zip: '1'}, or nothing for the whole desk. */
export function exportHref(params = {}) {
  const q = new URLSearchParams(Object.entries(params).filter(([, v]) => v));
  const s = q.toString();
  return './api/export' + (s ? '?' + s : '');
}

/** Downloads url as a file. */
export function download(url) {
  const a = document.createElement('a');
  a.href = url;
  a.download = '';
  document.body.append(a);
  a.click();
  a.remove();
}
