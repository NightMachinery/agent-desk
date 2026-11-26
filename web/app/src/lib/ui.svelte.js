// Settings of this device's dashboard, kept in localStorage: how it reads,
// not what the desk holds. The section order is the exception: shared by
// every device through the desk, and cached here.
import { get, post } from './api.js';
import { toast } from './toast.svelte.js';
import { normNav, isDefaultNav } from './moveon.js';
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
// The Tasks section lists the open tasks, and with a span also those
// finished (done or dropped) in it: the span's length in ms by name.
export const taskSpans = { open: 0, '15m': 15 * 60e3, '1h': 3600e3, '6h': 6 * 3600e3, '1d': 864e5, '3d': 3 * 864e5, '7d': 7 * 864e5, '30d': 30 * 864e5 };
export const taskModes = Object.keys(taskSpans);
export const taskModeLabel = {
  open: 'Open',
  '15m': 'Open + last 15 min',
  '1h': 'Open + last hour',
  '6h': 'Open + last 6 hours',
  '1d': 'Open + last day',
  '3d': 'Open + last 3 days',
  '7d': 'Open + last week',
  '30d': 'Open + last 30 days',
};
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
  // Which tasks the Tasks section lists (taskModes), per device.
  tasksShow: taskModes.includes(saved.tasksShow) ? saved.tasksShow : 'open',
  // The list's sections in order (orderedSections), shared by every device.
  sectionOrder: orderedSections(saved.sectionOrder),
  // Auto-navigation's order (moveon.js), shared by every device.
  nav: normNav(saved.nav),
});

export function saveUI() {
  try {
    localStorage.setItem(KEY, JSON.stringify({
        lingerSeconds: ui.lingerSeconds,
        nextAfterQuestion: ui.nextAfterQuestion,
        nextAfterMessage: ui.nextAfterMessage,
        collapsed: ui.collapsed,
        deliverablesShow: ui.deliverablesShow,
        tasksShow: ui.tasksShow,
        sectionOrder: ui.sectionOrder,
        nav: ui.nav,
      }),
    );
  } catch {
    /* private mode: settings last until reload */
  }
}

/** Whether the Deferred under section name is open: folded until opened. */
export const laterOpen = (name) => ui.collapsed['deferred:' + name] === false;

/** Opens or folds the Deferred under section name. */
export function toggleLater(name, open) {
  ui.collapsed['deferred:' + name] = !(open === undefined ? !laterOpen(name) : open);
  saveUI();
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

/** Which tasks the Tasks section lists: mode, or the next one when it is not a mode. */
export function showTasks(mode) {
  const at = taskModes.indexOf(ui.tasksShow);
  ui.tasksShow = taskModes.includes(mode) ? mode : taskModes[(at + 1) % taskModes.length];
  saveUI();
  return ui.tasksShow;
}

/** Take the section order and auto-navigation order shared on the desk, if they differ from this browser's. */
export async function loadSharedUI() {
  try {
    const prefs = await get('dashboard-prefs');
    const order = orderedSections(prefs.sections);
    const nav = normNav(prefs.nav);
    if (order.join() !== ui.sectionOrder.join() || JSON.stringify(nav) !== JSON.stringify(ui.nav)) {
      ui.sectionOrder = order;
      ui.nav = nav;
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

/** Set auto-navigation's order (moveon.js) on every device; changes merge
 * into the current one, and the default is saved as null. */
export async function setNav(change) {
  const before = ui.nav;
  ui.nav = normNav({ ...ui.nav, ...change });
  saveUI();
  try {
    await post('dashboard-prefs', { key: 'nav', data: isDefaultNav(ui.nav) ? null : ui.nav });
  } catch (e) {
    ui.nav = before;
    saveUI();
    toast(`The auto-navigation order was not saved: ${e.message}`, 'error');
  }
}
