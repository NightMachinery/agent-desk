// A deliverable is read when the user opens what it delivers, not when its
// page is shown: its Open button, a file to download, an attachment (an
// image opened larger, audio or video played), or a link in its text that
// leaves the dashboard. One that offers none of these is read on opening,
// like any other item. An image shown inline does not count: it shows
// without the user choosing.
import { markdown } from './md.js';

/** Whether text has a link that leaves the dashboard (md.js opens those in
 * a new tab; a link to a desk item does not).
 * @param {string} text */
export function outsideLink(text) {
  return !!text && /<a [^>]*target="_blank"/.test(markdown(text));
}

/** Whether a deliverable offers something to open: an Open link, an
 * attachment of any kind still stored (a deleted one has no url), or a link
 * in its text.
 * @param {{ openURL?: string, media?: { url?: string }[], body?: string }} d */
export function deliversSomething({ openURL = '', media = [], body = '' }) {
  return !!openURL || media.some((m) => !!m?.url) || outsideLink(body);
}

/** Whether a click on el opens what an item delivers: a link that leaves
 * the dashboard (the Open button, an app link, a download, a new tab).
 * Links to desk items and pages do not.
 * @param {any} el the click's target */
export function opensDelivery(el) {
  const a = el?.closest?.('a[href]');
  if (!a) return false;
  return a.dataset?.cmd === 'deliverable-open' || a.hasAttribute('download') || a.getAttribute('target') === '_blank';
}
