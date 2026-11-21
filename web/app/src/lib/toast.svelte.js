export const toasts = $state([]);
let next = 0;

/** Shows a short message; kind is info, ok or error. An action
 * ({label, run}) adds a button, such as Undo, that closes the toast. A key
 * makes the toast single: showing it again while it is up pulses the one
 * shown and restarts its timer instead of stacking a copy. */
export function toast(text, kind = 'info', ms = 4500, action = null, key = '') {
  const shown = key && toasts.find((t) => t.key === key);
  if (shown) {
    clearTimeout(shown.timer);
    shown.bump += 1;
    shown.timer = setTimeout(() => drop(shown.id), ms);
    return;
  }
  const id = ++next;
  toasts.push({ id, text, kind, action, key, bump: 0, timer: setTimeout(() => drop(id), ms) });
}

function drop(id) {
  const i = toasts.findIndex((t) => t.id === id);
  if (i >= 0) toasts.splice(i, 1);
}
