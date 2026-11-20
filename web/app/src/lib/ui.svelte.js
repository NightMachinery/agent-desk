// Settings of this device's dashboard, kept in localStorage: how it reads,
// not what the desk holds.
const KEY = 'agent-desk.ui';

function load() {
  try {
    return JSON.parse(localStorage.getItem(KEY) ?? '{}') ?? {};
  } catch {
    return {};
  }
}
const saved = load();
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
});

export function saveUI() {
  try {
    localStorage.setItem(KEY, JSON.stringify({
        lingerSeconds: ui.lingerSeconds,
        nextAfterQuestion: ui.nextAfterQuestion,
        nextAfterMessage: ui.nextAfterMessage,
        collapsed: ui.collapsed,
        deliverablesShow: ui.deliverablesShow,
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
