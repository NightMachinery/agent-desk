// Auto arrivals: an item the page moved to by itself (moving on after an
// answer, a dismissal or a message) is not read just by being shown. It is
// read once the user acts: leaves it for another item or the list, or
// answers or messages from it. Back or Undo on the move-on toast cancels
// the arrival, leaving the item unread.
//
// pending: the open arrival is one opening would have read (not a question,
// which reading would dismiss, and still unread). Only then does settling
// mark it read. event: its full id, set with pending, for keeping its row.

export const arrival = { id: '', pending: false, event: '' };

// The app sets the request (App.svelte); this module stays free of the API
// so node tests can load it.
let send = async (/** @type {string} */ _id) => {};
let after = () => {};
let held = (/** @type {string} */ _id) => false;
let keep = (/** @type {string} */ _event, /** @type {string} */ _id) => {};

/**
 * poster(id) marks id read; onRead runs once it has; kept(id) says a draft
 * keeps it unread; hold(event, id) keeps its row listed, from before the
 * read is sent (linger.svelte.js).
 */
export function arrivalHooks({ poster, onRead, kept, hold } = {}) {
  if (poster) send = poster;
  if (onRead) after = onRead;
  if (kept) held = kept;
  if (hold) keep = hold;
}

/** The page is about to move to id by itself. */
export function arrivedAuto(id) {
  arrival.id = id || '';
  arrival.pending = false;
  arrival.event = '';
}

export const isAutoArrival = (id) => !!id && arrival.id === id;

/** Back, Undo, or an explicit read/unread: forget it without reading. */
export function cancelArrival(id = arrival.id) {
  if (arrival.id === id) arrivedAuto('');
}

/** The user acted on id or moved on from it: read it if opening would have. */
export function settleArrival(id) {
  if (!isAutoArrival(id)) return;
  const { pending, event } = arrival;
  arrivedAuto('');
  if (!pending || held(id)) return;
  if (event) keep(event, id);
  send(id).then(() => after(), () => {});
}

/** The open item changed from prev to cur. */
export function routeMoved(prev, cur) {
  if (prev && prev !== cur) settleArrival(prev);
}
