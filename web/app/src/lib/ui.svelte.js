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

export const ui = $state({
  // How long a row read by opening it stays in the list, dimmed, after you
  // move on. 0 drops it at once.
  lingerSeconds: Number.isFinite(saved.lingerSeconds) ? saved.lingerSeconds : 60,
  // Sections folded away, by name: needs, pinned, responses, new, runs.
  collapsed: saved.collapsed ?? {},
});

export function saveUI() {
  try {
    localStorage.setItem(KEY, JSON.stringify({ lingerSeconds: ui.lingerSeconds, collapsed: ui.collapsed }));
  } catch {
    /* private mode: settings last until reload */
  }
}

export function toggleSection(name, open) {
  ui.collapsed[name] = open === undefined ? !ui.collapsed[name] : !open;
  saveUI();
}
