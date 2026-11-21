// Status: ask whoever works on a task for a short executive summary of where
// it stands. It is a message on the task (the server's prepared message
// task-status), so it reaches the agent that started the task, else the
// task's node or the coordinator working under it, and the summary comes
// back into the task's conversation.
import { get } from './api.js';
import { chooseSendCache } from './cache.svelte.js';
import { queueMessage } from './outbox.svelte.js';
import { toast } from './toast.svelte.js';

/** Ask about the task with event id `id`, shown as `display`. */
export async function askStatus(id, display) {
  let target;
  try {
    target = await get('target', { to: id });
  } catch (err) {
    toast(`Nobody to ask about ${display}: ${err.message}`, 'error', 9000);
    return false;
  }
  const choice = await chooseSendCache('reply', id);
  if (choice === 'cancel') return false;
  const queued = queueMessage('reply', { to: id, prompt: 'task-status', ...(choice === 'compact' && { after_compaction: true }) }, id, { quiet: true });
  if (queued) toast(`Asked ${target.node} where ${display} stands. The summary comes back in the task's conversation.`, 'ok', 7000);
  return queued;
}
