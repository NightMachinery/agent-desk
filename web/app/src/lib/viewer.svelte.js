// The image viewer (ImageViewer.svelte): one image of an item's images at a
// time, over the page. Attachments opens it; at is -1 while it is closed.

/** @typedef {{url: string, name: string, caption?: string}} ViewerImage */

export const viewer = $state({ images: /** @type {ViewerImage[]} */ ([]), at: -1 });

/** @param {ViewerImage[]} images @param {number} at */
export function openImage(images, at) {
  viewer.images = images;
  viewer.at = at;
}

export function closeImage() {
  viewer.at = -1;
}

/** Move n images on, wrapping around. */
export function stepImage(n) {
  const k = viewer.images.length;
  if (k && viewer.at >= 0) viewer.at = (((viewer.at + n) % k) + k) % k;
}
