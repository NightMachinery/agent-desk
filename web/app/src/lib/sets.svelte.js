// Project sets and the focus: one focus for the whole desk (every device,
// and the notifications), kept on the desk itself.
import { post } from './api.js';
import { toast } from './toast.svelte.js';
import { failed } from './errors.svelte.js';

// The set editor's sheet; name is the set being edited, '' for a new one.
export const setsUi = $state({ open: false, name: '' });

export function editSets(name = '') {
  setsUi.name = name;
  setsUi.open = true;
}

/** Focuses the set called name, or turns the focus off with ''. */
export async function focusOn(name, onchange) {
  try {
    await post('sets', { action: 'focus', name });
    toast(name ? `Focus: ${name}. Only its items show and notify.` : 'Focus off: everything shows.', 'ok');
    onchange?.();
  } catch (e) {
    failed(e);
  }
}

/** "3 hidden: 1 waiting on you", or '' when the focus hides nothing. */
export function hiddenText(h) {
  const total = (h?.needs_you ?? 0) + (h?.unread ?? 0);
  if (!total) return '';
  return `${total} hidden` + (h.needs_you ? `: ${h.needs_you} waiting on you` : '');
}
