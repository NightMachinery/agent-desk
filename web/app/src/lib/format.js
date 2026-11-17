export function from(event) {
  return event?.meta?.from || {};
}

/** The id the session filter takes for an event's session. */
export function sessionOf(event) {
  const f = from(event);
  return f.paseo || f.session || '';
}

export function short(id) {
  return id ? id.slice(0, 8) : '';
}

export function agentName(a) {
  return { claude: 'Claude', codex: 'Codex', agy: 'Antigravity' }[a] || a || 'agent';
}

export function kindOf(item) {
  const e = item.event;
  if (item.response) return 'response';
  if (e.kind === 'ask') return e.level === 'blocked' ? 'blocked' : 'default';
  if (e.kind === 'tell') return 'notice';
  if (e.kind === 'done') return 'done';
  if (e.kind === 'set' || e.kind === 'link' || e.kind === 'clear') return 'slot';
  return e.kind;
}

export const kindLabel = {
  blocked: 'Blocked',
  default: 'Has default',
  notice: 'Notice',
  response: 'Response',
  done: 'Done',
  slot: 'Slot',
  answer: 'Answer',
  reply: 'Your message',
};

export function slotName(event) {
  return event.path.slice(event.path.lastIndexOf('/') + 1);
}

export function titleOf(item) {
  const e = item.event;
  if (kindOf(item) === 'slot') return slotName(e);
  return e.title || item.display_id;
}

export function stateClass(state) {
  return { 'waiting on you': 'waiting', active: 'active', quiet: 'quiet', finished: 'finished' }[state] || 'quiet';
}

export function viaText(t) {
  if (!t) return '';
  switch (t.via) {
    case 'paseo':
      return 'Sent to its Paseo agent; if it is busy, it reads this mid-turn without being interrupted.';
    case 'tmux':
      return 'Typed into its tmux pane, if the pane still runs it.';
    case 'command':
      return 'Handed to your deliver_command.';
    case 'merge':
      return `Its session is on ${t.host}; delivered there after the next merge.`;
    default:
      return 'This session cannot be reached now; the message is kept and sent when it can be.';
  }
}

export async function copy(text) {
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}
