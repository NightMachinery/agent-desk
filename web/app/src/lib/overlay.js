// The pure part of pending.svelte.js (laying the user's changes over what
// the desk sent), free of Svelte so node tests can load it.
//
// A change: { n, ids (display ids), patch (fields to set), leave (sections
// to take them out of), done (the last ticket out when the desk took it; 0
// while on its way) }. shown: the newest ticket shown per kind of answer
// ('view', 'item').

/** The changes an answer of kind may predate: on their way, or taken no
 * earlier than the last ticket shown for that kind. */
export const live = (changes, shown, kind) => changes.filter((c) => !c.done || shown[kind] <= c.done);

/** Whether a change can go: every kind of answer shown since shows it. */
export const settled = (c, shown) => !!c.done && shown.view > c.done && shown.item > c.done;

/** item with the patches of cs that name it, oldest first (the same object
 * when none do). */
export function patchItem(item, cs) {
  if (!item) return item;
  const mine = cs.filter((c) => c.ids.includes(item.display_id));
  return mine.length ? Object.assign({ ...item }, ...mine.map((c) => c.patch)) : item;
}

/** Whether one of cs takes id out of section. */
export const leaves = (cs, id, section) => cs.some((c) => c.ids.includes(id) && c.leave.includes(section));

/** groups ([{project, run, items}]) of section with cs laid over: items
 * patched, those taken out left out, groups left empty dropped. */
export function patchGroups(groups, section, cs) {
  if (!groups) return [];
  if (!cs.length) return groups;
  return groups
    .map((g) => ({ ...g, items: g.items.filter((it) => !leaves(cs, it.display_id, section)).map((it) => patchItem(it, cs)) }))
    .filter((g) => g.items.length);
}
