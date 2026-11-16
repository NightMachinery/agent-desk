export const toasts = $state([]);
let next = 0;

/** Shows a short message; kind is info, ok or error. */
export function toast(text, kind = 'info', ms = 4500) {
  const id = ++next;
  toasts.push({ id, text, kind });
  setTimeout(() => {
    const i = toasts.findIndex((t) => t.id === id);
    if (i >= 0) toasts.splice(i, 1);
  }, ms);
}
