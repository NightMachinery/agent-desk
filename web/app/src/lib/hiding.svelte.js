// Items being marked read leave the list at once, before the server has
// confirmed: the round trip through the proxy can take seconds. They stay
// hidden until a refreshed view no longer lists them (or for at most
// HIDE_MS); a failed request or an Undo shows them again.

const HIDE_MS = 30000;

/** Display id -> true while its read is pending. */
export const hiding = $state({ ids: /** @type {Record<string, true>} */ ({}) });

/** Hides ids from Needs you, Responses and New (not Pinned: a read item stays pinned). */
export function hide(ids) {
  for (const id of ids) hiding.ids[id] = true;
  setTimeout(() => show(ids), HIDE_MS);
}

/** Shows ids again. */
export function show(ids) {
  for (const id of ids) delete hiding.ids[id];
}

/** Forgets the hidden ids a refreshed view no longer lists. */
export function settle(listed) {
  for (const id of Object.keys(hiding.ids)) if (!listed.has(id)) delete hiding.ids[id];
}

/** groups without the hidden items, and without groups left empty. */
export function unhidden(groups) {
  if (!Object.keys(hiding.ids).length) return groups;
  return groups
    .map((g) => ({ ...g, items: g.items.filter((it) => !hiding.ids[it.display_id]) }))
    .filter((g) => g.items.length);
}
