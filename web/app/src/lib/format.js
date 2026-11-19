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
  if (item.response && e.kind !== 'ask') return 'response';
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
  task: 'Task',
  instruction: 'Your instruction',
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
      return 'Sent through Paseo.';
    case 'tmux':
      return 'Typed into its tmux pane, if the pane still runs it.';
    case 'command':
      return 'Handed to your deliver_command.';
    case 'merge':
      return t.pushed ? `Its session is on ${t.host}; pushed there at once.` : `Its session is on ${t.host}; delivered there after the next merge.`;
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

// Behaviours, as the composer offers them.
export const behaviorLabel = { steer: 'Send', queue: 'Send after its turn', interrupt: 'Stop and send' };
export const behaviorHelp = {
  steer: 'Joins the running turn at its next step; starts a turn if the agent is idle.',
  queue: 'Waits for the running turn to end, then starts the next one.',
  interrupt: 'Cancels the running turn, losing its work in progress, then sends.',
};

/** What a delivery receipt says happened to a message. */
export function dispositionText(d) {
  if (!d) return '';
  const words = {
    turn_started: 'started a new turn',
    steered: 'joined the running turn',
    interrupted: 'interrupted the turn',
    out_of_band: 'answered without a turn',
    typed: 'typed into its pane',
    command: 'handed to deliver_command',
    handled: 'handled by its command',
  };
  let t = d.disposition === 'queued' ? `queued${d.queue_position ? `, position ${d.queue_position}` : ''}` : words[d.disposition] || '';
  if (!t) t = d.via === 'tmux' ? 'typed into its pane' : 'delivered';
  if (d.revived) t += ' · revived the session';
  if (d.on_answer && d.on_answer.exit !== 0) t += ` · its command exited ${d.on_answer.exit} and passed it on`;
  return t;
}

/** The hover text of a delivery chip. */
export function deliveryTitle(d) {
  let t = `Delivered ${d.age} ago via ${d.via}`;
  if (d.on_answer) t += `\nIts on-answer command exited ${d.on_answer.exit}` + (d.on_answer.output ? `:\n${d.on_answer.output}` : '');
  return t;
}

/** A notify.desktop value in words. */
export function desktopText(v) {
  if (!v || v === 'none') return 'no desktop notification';
  if (v === 'origin') return 'on the machine it ran on';
  if (v === 'all') return 'on every machine';
  return 'on ' + v.split(',').map((h) => (h === 'origin' ? 'its machine' : h)).join(', ');
}

/** A notify.remote value in words; label names the remote channel. */
export function remoteText(v, label = '') {
  const on = label ? `by ${label}` : 'on your phone';
  return { off: label ? `not by ${label}` : 'not on your phone', away: `${on} when you are away`, always: on }[v] || v || '';
}

/** A notify.push value in words. */
export function pushText(v) {
  return { off: 'not pushed', away: 'pushed when you are away', always: 'pushed' }[v] || v || '';
}

/** Where a resolved setting comes from, seen from path. */
export function originText(o, path) {
  if (!o) return '';
  const cls = o.class && o.class !== '*' ? ` for ${o.class}` : '';
  if (o.source === 'pref') {
    if ((o.scope || '') === (path || '')) return `set here${cls}`;
    return `inherited from ${o.scope || 'the global setting'}${cls}`;
  }
  if (o.source === 'config') return `config default${cls}`;
  return 'built in';
}

// The first meaningful line of a body, without its markdown markers, for a
// one-line preview.
export function previewLine(text) {
  const line = (text || '').split('\n').find((l) => l.trim()) || '';
  return line
    .replace(/^\s{0,3}(#{1,6}\s+|>\s*|[-*+]\s+(\[[ xX]\]\s+)?|\d+[.)]\s+)/, '')
    .replace(/\*\*(.+?)\*\*|__(.+?)__/g, '$1$2')
    .replace(/`([^`]+)`/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]*\)/g, '$1')
    .trim();
}
