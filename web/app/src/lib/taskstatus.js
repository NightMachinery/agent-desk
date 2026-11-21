// Messages about tasks. Status asks whoever works on a task for a short
// executive summary of where it stands: a message on the task (the server's
// prepared message task-status), so it reaches the agent that started the
// task, else the task's node or the coordinator working under it, and the
// summary comes back into the task's conversation. The user's own changes
// of a task's state reach the same agent from the server (taskchanges.go);
// toldText words that for the toast.
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

/**
 * The toast's sentence on who hears of a task change, from the task
 * action's answer ({tell, after}); '' when nobody will. `undo`: the toast
 * offers Undo, which cancels the message too.
 */
export function toldText(res, undo = false) {
  if (!res?.tell) return '';
  const after = res.after < 60 ? `${res.after} s` : `${Math.round(res.after / 60)} min`;
  return ` ${res.tell} is told in ${after}${undo ? ' (Undo cancels that)' : ''}.`;
}

/** What a task's note says, by its state: an agent writes how it finished a task or why it dropped it. */
export const noteLabel = (state) => (state === 'done' ? 'How it was done' : state === 'dropped' ? 'Why it was dropped' : 'Note');

// The two defers. Defer (state later) is "not now" for any reason; Defer
// for quota (state deferred) waits for quota to spare, where the quota nudge
// offers it and Launch deferred starts it.
export const DEFER_WHY = 'Not now: it waits until you undefer, start or launch it';
export const QUOTA_WHY = 'Wait for quota to spare: the quota nudge offers it, and launching deferred tasks starts an agent on it';
/**
 * Why Defer (or, with quota, Defer for quota) does not apply to a task in
 * state, or '' when it does. Both buttons stay on an open task's row,
 * disabled with this reason, so the row keeps its shape.
 */
export function deferOff(state, quota = false) {
  if (!quota) return state === 'later' ? 'Already deferred: Undefer brings it back' : '';
  if (state === 'deferred') return 'Already deferred for quota: Undefer brings it back';
  if (state === 'started') return 'Started: an agent already has it, so there is nothing to launch when quota is spare. Defer pauses it';
  return '';
}
/** How a task state reads on a chip. */
export const stateLabel = (state) => ({ later: 'deferred', deferred: 'for quota' })[state] ?? state;
