// Archiving a Paseo agent from the desk (POST api/agent). Paseo interrupts
// it if it is running; messaging it from the desk later revives it.
import { post } from './api.js';
import { toast } from './toast.svelte.js';

/** Asks, then archives. Resolves true once archived. */
export async function archiveAgent(paseo, title = '') {
  const name = title || paseo.slice(0, 8);
  if (!window.confirm(`Archive ${name}?\n\nPaseo stops it if it is running. Messaging it from the desk revives it.`)) return false;
  try {
    const res = await post('agent', { action: 'archive', id: paseo });
    toast(res.note || `Archived ${name}.`, 'ok', 6000);
    return true;
  } catch (err) {
    toast(err.message, 'error', 9000);
    return false;
  }
}
