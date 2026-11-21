// Settings of this device's dashboard, kept in localStorage: how it reads,
// not what the desk holds. The section order is the exception: shared by
// every device through the desk, and cached here.
import { get, post } from './api.js';
import { toast } from './toast.svelte.js';
const KEY = 'agent-desk.ui';

function load() {
  try {
    return JSON.parse(localStorage.getItem(KEY) ?? '{}') ?? {};
  } catch {
    return {};
  }
}
const saved = load();

// The list's sections: their names, in the default order. The order shown
// is set in Settings and shared by every device (api/dashboard-prefs); this
// browser keeps the last one it saw, so the list never jumps on load.
export const sectionNames = {
  needs: 'Needs you',
  quota: 'Out of quota',
  deliverables: 'Deliverables',
  pinned: 'Pinned',
  responses: 'Responses to you',
  tasks: 'Tasks',
  new: 'New',
  runs: 'Runs',
  deferred: 'Deferred for quota',
};
export const defaultSections = Object.keys(sectionNames);

/** order made whole: known names once each, any missing one after its default neighbour. */
export function orderedSections(order) {
  const out = (Array.isArray(order) ? order : []).filter((s, i, a) => defaultSections.includes(s) && a.indexOf(s) === i);
  defaultSections.forEach((s, i) => {
    if (out.includes(s)) return;
    const before = defaultSections.slice(0, i).reverse().find((x) => out.includes(x));
    out.splice(before ? out.indexOf(before) + 1 : 0, 0, s);
  });
  return out;
}
export const deliverableModes = ['unread', 'latest', 'all'];
export const deliverableModeLabel = { unread: 'Unread latest', latest: 'Latest', all: 'All' };

export const ui = $state({
  // How long a row read by opening it stays in the list, dimmed, after you
  // move on. 0 drops it at once.
  lingerSeconds: Number.isFinite(saved.lingerSeconds) ? saved.lingerSeconds : 60,
  // Whether a wide screen moves to the next item once a question is dealt
  // with (answered, approved, denied, dismissed), and once a message is sent
  // in a conversation. A phone goes back to the list instead.
  nextAfterQuestion: saved.nextAfterQuestion ?? true,
  nextAfterMessage: saved.nextAfterMessage ?? true,
  // Sections folded away, by name: needs, pinned, responses, new, runs.
  collapsed: saved.collapsed ?? {},
  // Which deliverables the list shows: unread (the newest of each key,
  // unread), latest (the newest of each key, read or not) or all.
  deliverablesShow: deliverableModes.includes(saved.deliverablesShow) ? saved.deliverablesShow : 'unread',
  // The list's sections in order (orderedSections), shared by every device.
  sectionOrder: orderedSections(saved.sectionOrder),
});

export function saveUI() {
  try {
    localStorage.setItem(KEY, JSON.stringify({
        lingerSeconds: ui.lingerSeconds,
        nextAfterQuestion: ui.nextAfterQuestion,
        nextAfterMessage: ui.nextAfterMessage,
        collapsed: ui.collapsed,
        deliverablesShow: ui.deliverablesShow,
        sectionOrder: ui.sectionOrder,
      }),
    );
  } catch {
    /* private mode: settings last until reload */
  }
}

export function toggleSection(name, open) {
  ui.collapsed[name] = open === undefined ? !ui.collapsed[name] : !open;
  saveUI();
}

/** List deliverables by mode (deliverableModes); omitted, the next one. */
export function showDeliverables(mode) {
  const at = deliverableModes.indexOf(ui.deliverablesShow);
  ui.deliverablesShow = deliverableModes.includes(mode) ? mode : deliverableModes[(at + 1) % deliverableModes.length];
  saveUI();
  return ui.deliverablesShow;
}

/** Take the section order shared on the desk, if it differs from this browser's. */
export async function loadSharedUI() {
  try {
    const prefs = await get('dashboard-prefs');
    const order = orderedSections(prefs.sections);
    if (order.join() !== ui.sectionOrder.join()) {
      ui.sectionOrder = order;
      saveUI();
    }
  } catch {
    // an older desk or offline: the cached order stands
  }
}

/** Show the list's sections in order on every device; null is the default order. */
export async function setSectionOrder(order) {
  const before = ui.sectionOrder;
  ui.sectionOrder = orderedSections(order);
  saveUI();
  try {
    await post('dashboard-prefs', { key: 'sections', data: order ? ui.sectionOrder : null });
  } catch (e) {
    ui.sectionOrder = before;
    saveUI();
    toast(`The order was not saved: ${e.message}`, 'error');
  }
}
