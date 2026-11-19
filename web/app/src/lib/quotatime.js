// When a stopped agent's account has quota again, in words: "quota back in
// 2h 14m, at 21:30" (a weekday when it is not today), "quota available now",
// or '' when the keeper has not said.

/**
 * @param {{verdict?: string, available_at?: number} | null | undefined} seat
 * @param {number} now milliseconds since the epoch
 */
export function availableText(seat, now = Date.now()) {
  if (!seat) return '';
  if (seat.verdict === 'usable') return 'quota available now';
  if (seat.verdict !== 'blocked') return '';
  if (!seat.available_at) return 'quota used up; reset time unknown';
  const at = seat.available_at * 1000;
  const mins = Math.ceil((at - now) / 60000);
  if (mins <= 0) return 'quota due back now';
  const d = Math.floor(mins / 1440), h = Math.floor((mins % 1440) / 60), m = mins % 60;
  const span = d ? `${d}d ${h}h` : h ? `${h}h ${m}m` : `${m}m`;
  const when = new Date(at);
  const clock = when.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  const sameDay = new Date(now).toDateString() === when.toDateString();
  const day = sameDay ? '' : when.toLocaleDateString([], { weekday: 'short' }) + ' ';
  return `quota back in ${span}, at ${day}${clock}`;
}
