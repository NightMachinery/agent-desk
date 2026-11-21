// Rows read by opening them stay in the list for a while: in place while
// open, then dimmed for ui.lingerSeconds after you move to another item, so
// a row never vanishes from under the cursor. The read itself is recorded at
// once; only the list waits. A row is held before its read is sent: the
// desk can report the change (and the list reload) before the read's own
// request returns.
import { ui } from './ui.svelte.js';
import { route } from './route.svelte.js';

/** Event id -> { display, until }; until is 0 while the item is open. */
export const linger = $state({ items: {} });
let onExpire = () => {};

export function onLingerEnd(f) {
  onExpire = f;
}

/** The item just opened (and marked read) stays listed. */
export function hold(eventId, display) {
  if (ui.lingerSeconds <= 0) return;
  linger.items[eventId] = { display, until: 0 };
  // Held once the user has moved on (an auto arrival, read on leaving it):
  // its clock starts now.
  if (route.item !== display) moved(route.item);
}

/** The read failed: the row is unread, and listed without help. */
export function release(eventId) {
  delete linger.items[eventId];
}

/** Full ids of the rows kept, for the view request. */
export const lingerIds = () => Object.keys(linger.items);

/** A kept row that is not the open item. */
export const dimmed = (eventId) => (linger.items[eventId]?.until ?? 0) > 0;

/** The open item is now `current`: start the clock on every other kept row. */
export function moved(current) {
  const now = Date.now();
  const ms = ui.lingerSeconds * 1000;
  for (const [id, x] of Object.entries(linger.items)) {
    if (x.display === current) x.until = 0;
    else if (x.until === 0) {
      x.until = now + ms;
      setTimeout(() => expire(id), ms + 50);
    }
  }
}

function expire(id) {
  const x = linger.items[id];
  if (!x || x.until === 0 || x.until > Date.now()) return;
  delete linger.items[id];
  onExpire();
}
