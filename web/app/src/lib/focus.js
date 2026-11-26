// Scrolling to the open item. In a conversation the item is one of its
// entries (id e-<display id>), which goes to the top of the item pane, just
// under its sticky toolbar; an item that is not in one scrolls the pane to
// its top. Re-clicking the open item's row and the focus-item command ask
// for it with FOCUS_EVENT.

export const FOCUS_EVENT = 'desk:focus-item';

/** Ask the open item's page to scroll to it. */
export function requestFocus() {
  window.dispatchEvent(new CustomEvent(FOCUS_EVENT));
}

/**
 * Scrolls the entry e-<display> to the top of its pane, or the pane to its
 * top when there is no such entry.
 * @param {string} display
 * @param {ScrollBehavior} behavior
 */
export function focusItem(display, behavior = 'smooth') {
  const pane = document.querySelector('[data-pane="detail"]');
  const el = display ? document.getElementById('e-' + display) : null;
  if (!el) return pane?.scrollTo({ top: 0, behavior });
  const bar = pane?.querySelector('.toolbar');
  el.style.scrollMarginTop = `${(bar instanceof HTMLElement ? bar.offsetHeight : 0) + 8}px`;
  el.scrollIntoView({ block: 'start', behavior });
}
