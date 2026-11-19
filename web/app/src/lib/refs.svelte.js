// Id mentions the server has confirmed: display id -> { title, kind, state,
// url }. Every API response that carries `refs` adds to it (api.js), and
// Markdown links only the ids found here, so "t-test" never links.
export const refs = $state(/** @type {Record<string, {title: string, kind: string, state?: string, url: string}>} */ ({}));

export function addRefs(more) {
  if (more && typeof more === 'object') Object.assign(refs, more);
}
