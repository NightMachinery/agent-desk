// The commands. Most press the same button a mouse would (tagged data-cmd in
// the components), so a key and a click do exactly the same. Each command is
// defined once with command(name, ...); keys.edn binds keys to its name, so
// M-x, SPC SPC and help list a command even when it has no key.
import { moveOnTarget, ALL_CLEAR, ALL_CLEAR_KEY } from './moveon.js';
import { route, go, pageOpen } from './route.svelte.js';
import { nav, step } from './nav.svelte.js';
import { sel, toggle, toggleAll, clear } from './select.svelte.js';
import { copy } from './format.js';
import { toast, bump } from './toast.svelte.js';
import { get, post } from './api.js';
import { exportHref, download } from './exports.js';
import { toggleSection, showDeliverables, deliverableModeLabel, showTasks, taskModeLabel } from './ui.svelte.js';
import {
  keys, ctx, command, pane, paneEl, cmdEl, say, dialog, moveFocus, focusables, startHints, openPalette, setKeymap,
  commands, commandItems, keysOf, keyFor, applies, describeKey, describeCommand, setVisual, syncMode, selectedField, selectField,
} from './keys.svelte.js';

// --- helpers -----------------------------------------------------------------

const has = (name) => () => !!cmdEl(name);
function click(name, missing) {
  const el = cmdEl(name);
  if (!el) return say(missing);
  // A disabled button's tooltip says why (lib/why.js).
  if (el instanceof HTMLButtonElement && el.disabled) return say(el.title || missing || 'Not available now');
  el.click();
}
const rowEl = (id) => /** @type {HTMLElement | null} */ (document.querySelector(`[data-item="${CSS.escape(id)}"]`));
const show = (id) => rowEl(id)?.scrollIntoView({ block: 'nearest' });
const open = () => !!route.item;
const picking = () => sel.ids.size > 0;

function openItem(id) {
  if (!id) return;
  const keep = document.activeElement instanceof HTMLElement && document.activeElement.matches('[data-item]');
  go({ item: id }, { replace: true });
  show(id);
  // Keyboard focus follows the cursor when it was on a row.
  if (keep) queueMicrotask(() => rowEl(id)?.focus({ preventScroll: true }));
  if (keys.anchor) {
    // Visual mode: the selection is every row from the anchor to here.
    const o = sel.order;
    const a = o.indexOf(keys.anchor);
    const b = o.indexOf(id);
    if (a >= 0 && b >= 0) {
      sel.ids.clear();
      for (const x of o.slice(Math.min(a, b), Math.max(a, b) + 1)) sel.ids.add(x);
    }
  }
}
function stepBy(n) {
  let id = route.item;
  for (let i = 0; i < Math.abs(n); i++) id = step(Math.sign(n), id) || id;
  if (id === route.item) return say(n > 0 ? 'End of the list' : 'Top of the list');
  openItem(id);
}
const edge = (last) => openItem(last ? nav.order[nav.order.length - 1] : nav.order[0]);
/** n and N walk the open items like back and forward. n goes to the next
 * open item (where moving on after an answer goes) and remembers where it
 * came from; N retraces those steps, because opening an item reads it, so
 * the items behind are no longer open by the time you want them back. With
 * nothing to retrace, N goes to the nearest open item before. Pinned items
 * are skipped. */
const back = [];
const ahead = [];
const listed = (id) => id && id !== route.item && nav.order.includes(id);
function openOpen(dir) {
  const here = route.item;
  const [from, to] = dir > 0 ? [ahead, back] : [back, ahead];
  while (from.length) {
    const id = from.pop();
    if (!listed(id)) continue;
    if (here) to.push(here);
    return openItem(id);
  }
  let id = '';
  if (dir > 0) id = moveOnTarget(nav.order, here, nav.pinned, nav.open);
  else {
    const i = nav.order.indexOf(here);
    id = nav.order.slice(0, Math.max(0, i)).reverse().find((x) => nav.open.has(x) && !nav.pinned.has(x)) ?? '';
  }
  if (!id) {
    // n looks both ways, so nothing found there means nothing else is open;
    // it is a clear desk unless the item shown is itself still open.
    if (dir < 0 && moveOnTarget(nav.order, here, nav.pinned, nav.open)) return say('Nothing open before this');
    if (here && nav.open.has(here) && !nav.pinned.has(here)) return say('Nothing else open');
    return clearDesk();
  }
  if (here) (dir > 0 ? back : ahead).push(here);
  if (back.length > 100) back.shift();
  openItem(id);
}

/** n on a clear desk: the next thing is the work itself, so it opens the
 * Tasks page here, even an empty one (Back returns). A repeat pulses the
 * toast. */
function clearDesk() {
  if (route.tasks) return bump(ALL_CLEAR_KEY, 6000) || toast(ALL_CLEAR, 'ok', 4500, null, ALL_CLEAR_KEY);
  const from = { item: route.item, tasks: false };
  tasksHere();
  toast(`${ALL_CLEAR}: here are your tasks`, 'ok', 6000, { label: 'Back', run: () => go(from) }, ALL_CLEAR_KEY);
}

/** The first row of the section n sections away from the open item. */
function section(n) {
  const list = paneEl('list');
  if (!list) return;
  const heads = [...list.querySelectorAll('h2.section-title')];
  const firsts = heads
    .map((h) => {
      let el = h.nextElementSibling;
      while (el && !el.matches('h2.section-title')) {
        const row = el.querySelector('[data-item]');
        if (row) return row.getAttribute('data-item');
        el = el.nextElementSibling;
      }
      return null;
    })
    .filter(Boolean);
  if (!firsts.length) return;
  const cur = route.item ? nav.order.indexOf(route.item) : -1;
  let at = -1;
  firsts.forEach((id, i) => nav.order.indexOf(id) <= cur && (at = i));
  openItem(firsts[Math.max(0, Math.min(firsts.length - 1, at + n))]);
}

/** The list section the open item is in, else the first one. */
function sectionHere() {
  let el = route.item ? rowEl(route.item) : null;
  while (el && !el.matches?.('h2.section-title')) el = el.previousElementSibling || el.parentElement;
  return el?.dataset.section || paneEl('list')?.querySelector('h2.section-title')?.getAttribute('data-section') || 'needs';
}
const sections = ['needs', 'quota', 'deliverables', 'pinned', 'responses', 'tasks', 'new', 'runs', 'deferred'];
function foldAll(open) {
  for (const s of sections) toggleSection(s, open);
}

function scroll(p, by) {
  const el = paneEl(p);
  if (!el) return;
  const page = el.clientHeight;
  const dy = by === 'top' ? -el.scrollHeight : by === 'bottom' ? el.scrollHeight : by * (Math.abs(by) < 1 ? page : 1);
  el.scrollBy({ top: dy });
}

function focusPane(p) {
  if (matchMedia('(max-width: 900px)').matches) {
    if (p === 'list' && (route.item || pageOpen())) return escape();
    return say('One pane at a time on a narrow screen');
  }
  keys.pane = p;
  keys.used = true;
  const el = paneEl(p);
  if (p === 'side') {
    const cur = el?.querySelector('a.on') ?? focusables(el)[0];
    if (cur instanceof HTMLElement) cur.focus();
  } else if (p === 'list') {
    if (route.item) rowEl(route.item)?.focus({ preventScroll: true });
    else /** @type {HTMLElement} */ (document.activeElement)?.blur?.();
  } else {
    /** @type {HTMLElement} */ (document.activeElement)?.blur?.();
    if (!route.item && !pageOpen() && nav.order.length && !ctx.view()?.node) openItem(nav.order[0]);
  }
}
const panes = ['side', 'list', 'detail'];
const nextPane = (d) => focusPane(panes[(panes.indexOf(pane()) + d + panes.length) % panes.length]);

function focusBox(id, missing) {
  const el = document.getElementById(id);
  if (!el) return say(missing);
  el.focus();
  el.scrollIntoView({ block: 'nearest' });
  syncMode();
}
function focusComposer() {
  const el = paneEl('detail')?.querySelector('.composer textarea') ?? document.querySelector('.composer textarea');
  if (el instanceof HTMLElement && el.getClientRects().length) {
    el.focus();
    syncMode();
    el.scrollIntoView({ block: 'nearest' });
  } else if (cmdEl('message-node')) {
    click('message-node');
    setTimeout(focusComposer, 50);
  } else say('No message box here');
}
/** `i`: back into the field Escape left, else the first message box. */
function insert() {
  const el = selectedField();
  if (!el) return focusComposer();
  selectField(null);
  el.focus();
  syncMode();
  el.scrollIntoView({ block: 'nearest' });
}
function message() {
  if (document.getElementById('reply-box')) return focusBox('reply-box');
  focusComposer();
}

/** Marks the selection, or else the open item, read (dismissing a question). */
function markRead() {
  if (picking()) return click('bulk-read');
  if (!open()) return say('Nothing open');
  if (cmdEl('dismiss')) return click('dismiss');
  const t = cmdEl('read-toggle');
  if (t?.getAttribute('aria-label') === 'Mark read') return t.click();
  say('Already read');
}
function markUnread() {
  if (picking()) return click('bulk-unread');
  if (!open()) return say('Nothing open');
  if (cmdEl('undismiss')) return click('undismiss');
  const t = cmdEl('read-toggle');
  if (t?.getAttribute('aria-label') === 'Mark unread') return t.click();
  say(t ? 'Already unread' : 'An open question stays in Needs you');
}
function pin() {
  if (picking()) return click('bulk-pin');
  click('pin', 'Nothing open');
}
const exitVisual = () => keys.anchor && setVisual('');
const after = (f) => () => {
  f();
  exitVisual();
};

async function yank(text, what) {
  if (!text) return say(`No ${what.toLowerCase()} here`);
  toast((await copy(text)) ? `${what} copied.` : text, 'ok');
}

// y f: hints on every event on screen (list rows, conversation entries, the
// open item); the chosen one is copied as "id: text", so a paste carries its
// reference. The text comes from data the page already holds, else the item
// is fetched.
const EVENTS = '[data-item], [data-event]';
function shownEvent(id) {
  const it = ctx.item();
  const v = ctx.view();
  const pools = [[it?.item], it?.thread, ...Object.values(v?.view ?? {}), ...(v?.conversations ?? []).map((c) => c.entries)];
  for (const pool of pools) {
    const hit = Array.isArray(pool) && pool.find((x) => x?.display_id === id);
    if (hit) return hit.event;
  }
  return null;
}
async function yankEvent(el) {
  const id = el.dataset.item || el.dataset.event || '';
  const e = shownEvent(id) ?? (await get('item', { id }).catch(() => null))?.item?.event;
  if (!e) return say(`Could not read ${id}`);
  const text = [e.title, e.body].filter(Boolean).join('\n\n');
  yank(`${id}: ${text}`, id);
}

function escape() {
  if (selectedField()) selectField(null);
  else if (keys.anchor) {
    setVisual('');
    clear();
  } else if (sel.ids.size || sel.mode) clear();
  else if (route.item) {
    go({ item: '' });
    keys.pane = 'list';
  } else if (pageOpen()) go({ settings: false, verbatim: false, tasks: false });
  else if (route.q) go({ q: '' }, { replace: true });
  else /** @type {HTMLElement} */ (document.activeElement)?.blur?.();
}

function parent() {
  if (!route.path) return say('Already at the top');
  go({ path: route.path.split('/').slice(0, -1).join('/'), item: '' });
}

// --- pages ------------------------------------------------------------------------

const verbatimHere = () => go({ item: '', verbatim: true });
const verbatimAll = () => go({ path: '', item: '', session: '', verbatim: true });
const tasksHere = () => go({ item: '', tasks: true });
const tasksAll = () => go({ path: '', item: '', session: '', tasks: true });
/** Opens the Tasks page (here) with its New task form. */
function taskAdd() {
  if (!route.tasks) go({ item: '', tasks: true });
  let tries = 0;
  const openIt = () => {
    const details = document.getElementById('task-details');
    if (details) return details.focus();
    const b = cmdEl('task-add');
    if (b) return b.click();
    if (++tries < 20) setTimeout(openIt, 50);
  };
  openIt();
}
function recordings() {
  if (!route.settings) go({ settings: true, item: '' });
  let tries = 0;
  const openIt = () => {
    const d = /** @type {HTMLDetailsElement | null} */ (document.getElementById('recordings'));
    if (!d) return ++tries < 20 ? setTimeout(openIt, 50) : say('No recordings here');
    d.open = true;
    d.scrollIntoView({ block: 'nearest' });
  };
  openIt();
}
/** `/`: the search box of the page in view. */
function search() {
  if (route.verbatim && document.getElementById('verbatim-search')) return focusBox('verbatim-search');
  if (route.tasks && document.getElementById('tasks-search')) return focusBox('tasks-search');
  focusBox('search', 'No search box');
}

// --- pickers -------------------------------------------------------------------

const mx = () => openPalette('M-x', commandItems(), { history: true });

function places() {
  const out = [{ label: 'Everything', detail: 'go to', run: () => go({ path: '', item: '', session: '' }) }];
  const walk = (ns) => {
    for (const n of ns ?? []) {
      out.push({ label: n.path, detail: `go to · ${n.state}`, run: () => go({ path: n.path, item: '', session: '' }) });
      walk(n.children);
    }
  };
  walk(ctx.runs());
  return out;
}
const nodes = () => openPalette('Go to project, run or agent', places());
/** SPC SPC: every command that applies here, then every place. */
const anything = () => openPalette('Command or place', [...commandItems(), ...places()], { history: true });

function items() {
  const rows = /** @type {HTMLElement[]} */ ([...(paneEl('list')?.querySelectorAll('[data-item]') ?? [])]);
  const seen = new Set();
  const out = [];
  for (const r of rows) {
    const id = r.getAttribute('data-item');
    if (seen.has(id)) continue;
    seen.add(id);
    out.push({ label: r.innerText.replace(/\s+/g, ' ').trim(), key: id, run: () => openItem(id) });
  }
  if (!out.length) return say('No items in this list');
  openPalette('Switch to item', out);
}

/** SPC h f: every command, here or not, to describe. */
function describeFn() {
  const out = [...commands.values()]
    .map((c) => ({
      label: c.desc,
      detail: applies(c) ? c.name : `${c.name} · not here`,
      key: keysOf(c.name, true).slice(0, 2).join(', '),
      run: () => describeCommand(c.name),
    }))
    .sort((a, b) => a.label.localeCompare(b.label));
  openPalette('Describe command', out);
}

// --- commands --------------------------------------------------------------------
// command(name, description, run, { pane, when, need, palette, anywhere })

const C = command;
const visual = () => {
  if (!route.item) edge(false);
  if (!route.item) return;
  setVisual(route.item);
  sel.ids.add(route.item);
};
const sideFocus = () => /** @type {HTMLElement} */ (document.activeElement)?.click?.();
const closeSheet = () => dialog()?.querySelector('[data-cmd="sheet-close"], .head button')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
const tagged = (name, need) => ({ when: has(name), need });

// Help and pickers.
C('cheatsheet', 'The cheatsheet', () => (keys.help = 'sheet'), { anywhere: true });
C('bindings', 'Show every key binding here', () => (keys.help = 'all'));
C('describe-key', 'Describe a key: what it runs, without running it', describeKey);
C('describe-command', 'Describe a command: its keys and where it applies', describeFn);
C('m-x', 'Run a command by name (M-x)', mx, { palette: false, anywhere: true });
C('find', 'Find a command or a place', anything, { palette: false });
C('go-node', 'Go to a project, run or agent', nodes);
C('switch-item', 'Switch to an item in the list', items);
C('hints', 'Click something on screen (hints)', () => startHints(), { anywhere: true });
C('reply-entry', 'Reply to an entry of the conversation (hints on the reply buttons)', () => startHints('.reply-btn'), { when: () => !!document.querySelector('.reply-btn') });
C('next-open', 'Next open item: where answering moves on to (an open question, else an unread item)', () => openOpen(1));
C('prev-open', 'Back along your n steps (else the previous open item)', () => openOpen(-1));
C('search', 'Search', search);
C('search-clear', 'Clear the search', () => go({ q: '', item: '' }, { replace: true }), { when: () => !!route.q, need: 'a search' });
C('escape', 'Back out: selected field, visual mode, selection, item, page, search', escape, { palette: false });

// Going places.
C('next-item', 'Next item', (n) => stepBy(n));
C('prev-item', 'Previous item', (n) => stepBy(-n));
C('up', 'Up one level', parent);
C('top', 'Go to everything (the top)', () => go({ path: '', item: '', session: '', q: '' }));
C('settings', 'Settings here', () => go({ settings: true, item: '' }));
C('settings-global', 'Global settings', () => go({ path: '', settings: true, item: '', session: '' }));
// The section order card in global Settings, its first handle focused
// (arrow keys move that section).
function sectionOrder() {
  go({ path: '', settings: true, item: '', session: '' });
  let tries = 0;
  const focus = () => {
    const el = cmdEl('section-order');
    if (el) {
      el.scrollIntoView({ block: 'center' });
      el.focus();
    } else if (++tries < 40) setTimeout(focus, 50);
  };
  focus();
}
C('section-order', "Order the list's sections (Settings; every device)", sectionOrder);
C('org-render', 'Render the org files now (inbox, boards, ledger)', async () => {
  try {
    const r = await post('render', {});
    toast(`Org files rendered in ${r.took_ms} ms.`, 'ok');
  } catch (err) {
    toast(err.message, 'error');
  }
});
C('org-export', 'Export the whole desk as one .org file', () => download(exportHref()));
C('org-export-zip', 'Export the desk as org files in a zip (inbox, boards, ledger)', () => download(exportHref({ zip: '1' })));
C('org-export-node', 'Export this node (board, tasks, items, conversations) as one .org file', () => download(exportHref({ scope: 'node', path: route.path })), { when: () => !!route.path, need: 'a project, run or agent page' });
C('org-export-item', 'Export the open item with its conversation as one .org file', () => download(exportHref({ scope: 'item', id: route.item })), { when: open, need: 'an open item' });
C('org-export-inbox', 'Export the inbox (under this page) as one .org file', () => download(exportHref({ scope: 'inbox', path: route.path })));
C('section-order-reset', "Put the list's sections back in the default order", () => click('section-order-reset', `Open the section order first (${keyFor('section-order') || 'M-x section-order'})`));
C('verbatim', 'Verbatim log: your messages to agents, here', verbatimHere);
C('verbatim-all', 'Verbatim log: your messages to agents, everywhere', verbatimAll);
C('tasks', 'Tasks: the checklist here (tasks, bugs, ideas)', tasksHere);
C('tasks-all', 'Tasks: every project', tasksAll);
C('task-add', 'File a new task, bug or idea', taskAdd);
C('verbatim-more', 'Verbatim log: show older messages', () => click('verbatim-more'), tagged('verbatim-more', 'the Verbatim log with older messages'));
C('recordings', 'Recordings (voice messages)', recordings);
C('refresh', 'Refresh', () => (ctx.refresh(), say('Refreshed')));
C('open-node', "Open the item's node", () => click('open-node', 'Nothing open'), { when: open, need: 'an open item' });
C('session', 'Show everything its session wrote', () => click('session', 'No session'), tagged('session', 'an item from a session'));
C('thread-root', 'Go to the item this conversation is about', () => click('thread-root'), tagged('thread-root', 'a reply in a conversation'));

// Toggles.
C('toggle-all', 'Toggle show all (read items, finished runs)', () => go({ all: !route.all, item: '' }));
C('toggle-old', 'Toggle oldest first', () => go({ old: !route.old }, { replace: true }));
const deliverablesShow = (mode) => () => say(`Deliverables: ${deliverableModeLabel[showDeliverables(mode)]}`);
C('deliverables-show', 'Deliverables: switch between unread latest, latest and all', deliverablesShow());
C('deliverables-unread', 'Deliverables: the newest of each, unread', deliverablesShow('unread'));
C('deliverables-latest', 'Deliverables: the newest of each, read or not', deliverablesShow('latest'));
C('deliverables-all', 'Deliverables: every one, replaced ones too', deliverablesShow('all'));
C('tasks-show', 'Tasks section: open only, or also those finished in the last 15 minutes to 30 days (cycle)', () => say(`Tasks: ${taskModeLabel[showTasks()]}`));
C('toggle-select', 'Toggle select mode', () => click('select-mode'));
C('toggle-auto-continue', 'Toggle auto-continue', () => click('auto-continue'), tagged('auto-continue', 'the settings page'));
C('edit-sets', 'Edit focus sets', () => click('edit-sets'), tagged('edit-sets', 'the sidebar'));

// Panes.
C('pane-left', 'Focus the pane on the left', () => nextPane(-1));
C('pane-right', 'Focus the pane on the right', () => nextPane(1));
C('pane-next', 'Focus the next pane', () => nextPane(1));
C('pane-side', 'Focus the sidebar', () => focusPane('side'));
C('pane-detail', 'Focus the detail pane', () => focusPane('detail'));

// The open item (or the selection).
C('answer', 'Answer the question', () => focusBox('answer-box', 'Not an open question'));
C('message', 'Message its agent', message);
C('reply-to-cancel', 'Message: stop replying to a thread entry (send about the open item)', () => click('reply-to-cancel'), tagged('reply-to-cancel', 'a reply started with an entry\'s reply button'));
C('compose', 'Write in the first message box', focusComposer);
C('insert', 'Back into the selected field (the one Esc left), else the first message box', insert);
C('take', 'Take the recommendation or the default', () => click('quick', 'Not an open question'), tagged('quick', 'an open question with a recommendation or default'));
C('read', 'Mark read (a question: dismiss it)', after(markRead));
C('unread', 'Mark unread (a question: back to Needs you)', after(markUnread));
C('pin', 'Pin or unpin', after(pin));
C('notify-menu', 'Notify me: where its response notifies you', () => click('notify-menu', 'No message box'), tagged('notify-menu', 'a message box'));
C('send-menu', 'Send how? (steer, queue, interrupt)', () => click('send-menu', 'Write the message first'), tagged('send-menu', 'a message being written'));
C('attach', 'Attach a file to the message', () => click('attach'), tagged('attach', 'a message box'));
C('screen-grant', 'Screen request: grant it', () => click('screen-grant'), tagged('screen-grant', 'an open screen request'));
C('screen-deny', 'Screen request: deny it', () => click('screen-deny'), tagged('screen-deny', 'an open screen request'));
C('screen-release', 'Release the screen lock', () => click('screen-release'), tagged('screen-release', 'a screen lock held here'));
C('deliver-retry', 'Retry delivering the message', () => click('deliver-retry'), tagged('deliver-retry', 'a message that failed to deliver'));
C('yank-link', 'Copy a link to the item', () => yank(open() ? location.href : '', 'Link'));
C('yank-id', "Copy the item's id", () => yank(route.item, 'Id'));
C('yank-resume', 'Copy the resume command', () => yank(ctx.item()?.resume ?? '', 'Resume command'));
C('yank-paseo', "Copy the agent's Paseo id", () => yank(ctx.item()?.from?.paseo || ctx.view()?.target?.paseo || '', 'Paseo id'));
C('yank-path', 'Copy the path shown', () => yank(ctx.item()?.node ?? route.path, 'Path'));
C('yank-hint', 'Copy an event on screen by hint: its id and text', () => startHints(EVENTS, { act: yankEvent, none: 'No events on screen' }));

// The node page and the list.
C('node-message', 'Message the coordinator', message, { when: () => !!ctx.view()?.target, need: 'a node with a coordinator' });
C('node-velocity', 'Ask for a velocity report', () => click('velocity-report', 'No velocity slot here'), tagged('velocity-report', 'a node with a velocity slot'));
C('node-read-all', 'Mark everything under the node read', () => click('read-all'), tagged('read-all', 'a node page'));
C('agent-archive', "Archive the node's coordinator agent in Paseo", () => click('agent-archive'), tagged('agent-archive', 'a node whose coordinator is a Paseo agent here'));
C('node-settings', "The node's notify settings", () => click('node-settings'), tagged('node-settings', 'a node page'));
C('list-read-all', 'Mark every unread item in the list read', () => click('mark-all-read'), tagged('mark-all-read', 'unread items in the list'));
C('quota-copy-ids', 'Out of quota: copy the picked agent ids', () => click('quota-copy-ids'), tagged('quota-copy-ids', 'the Out of quota section'));
C('deliverable-open', 'Deliverable: open its link', () => click('deliverable-open'), tagged('deliverable-open', 'a deliverable with a link'));
C('task-status', 'Task: ask whoever works on it where it stands', () => click('task-status'), tagged('task-status', 'an open task'));
C('reload-page', 'Reload the page (drafts are kept): how a page left open gets a newer dashboard', () => location.reload());
C('push-logs', "Push: copy this device's push logs", () => click('push-logs'), tagged('push-logs', 'Settings, Push to this device'));
C('push-brave-setting', "Push: copy the address of Brave's push setting", () => click('push-brave-setting'), tagged('push-brave-setting', 'Settings, Push to this device, in Brave'));
C('task-defer', 'Task: defer it (not now, for any reason)', () => click('task-defer'), tagged('task-defer', 'an open or started task'));
C('task-defer-quota', 'Task: defer it until there is quota to spare', () => click('task-defer-quota'), tagged('task-defer-quota', 'an open or deferred task'));
C('task-undefer', 'Task: undefer it (back to open)', () => click('task-undefer'), tagged('task-undefer', 'a deferred task'));
C('task-reopen', 'Task: reopen it, saying what is still wrong', () => click('task-reopen'), tagged('task-reopen', 'a finished task'));
C('deliverable-download', 'Deliverable: download its first file', () => click('deliverable-download'), tagged('deliverable-download', 'a deliverable with a file'));
// Discard the draft in the box (or New task form) that has focus, else the first one with one.
function discardDraft() {
  const here = document.activeElement?.closest?.('.composer, form.add')?.querySelector('[data-cmd="draft-discard"]');
  const el = here || cmdEl('draft-discard');
  if (!el) return say('No draft here');
  if (el instanceof HTMLButtonElement && el.disabled) return say(el.title || 'Not available now');
  el.click();
}
C('draft-discard', 'Discard the draft in this box or the New task form (Undo brings it back)', discardDraft);
C('deliverable-works', 'Deliverable: send with "This worked." in front', () => click('deliverable-works'), tagged('deliverable-works', 'a deliverable whose agent can be messaged'));
C('quota-archive', 'Out of quota: archive the first agent in Paseo', () => click('quota-archive'), tagged('quota-archive', 'an agent out of quota'));
C('quota-replace', 'Out of quota: replace the first agent (pick its provider, model and effort)', () => click('quota-replace'), tagged('quota-replace', 'an agent out of quota that can be replaced'));
C('quota-dismiss', 'Out of quota: dismiss the agent (hidden until it runs and stops again)', () => click('quota-dismiss'), tagged('quota-dismiss', 'an agent out of quota'));
C('quota-ask-parent', 'Out of quota: ask its parent to reassign its role', () => click('quota-ask-parent'), tagged('quota-ask-parent', 'an out-of-quota agent whose parent has quota'));
C('deferred-launch', 'Deferred tasks: start an agent on each picked one', () => click('deferred-launch'), tagged('deferred-launch', 'the Deferred for quota section'));
C('deferred-launch-top', 'Deferred tasks: start agents on the first ones by priority', () => click('deferred-launch-top'), tagged('deferred-launch-top', 'the Deferred for quota section'));
C('task-done', 'Task: mark it done', () => click('task-done'), tagged('task-done', 'an unfinished task'));
C('task-drop', 'Task: drop it (Reopen brings it back)', () => click('task-drop'), tagged('task-drop', 'an unfinished task'));
C('task-start', 'Task: start an agent on it (launch_command)', () => click('task-start'), tagged('task-start', 'an open or deferred task'));
C('voice-toggle', 'Voice message: start or stop recording', () => click('voice-toggle'), tagged('voice-toggle', 'a message box'));
C('voice-recordings', "Voice message: pick a recording to insert its transcript in this box", () => click('voice-recordings'), tagged('voice-recordings', 'a message box'));
C('voice-retry', 'Voice message: retry its failed transcription', () => click('voice-retry'), tagged('voice-retry', 'a voice message whose transcription failed'));
C('voice-omit', 'Voice message: send it without the recording', () => click('voice-omit'), tagged('voice-omit', 'a voice message whose transcription failed'));
C('push-toggle', 'Push to this device: turn it on or off', () => click(cmdEl('push-off') ? 'push-off' : 'push-on'), { when: () => !!(cmdEl('push-on') || cmdEl('push-off')), need: 'Settings, Push to this device' });
C('push-test', 'Push: send this device a test', () => click('push-test'), tagged('push-test', 'Settings, with push on for this device'));

// The context map: what the localleader (, and SPC m) reaches, the keys only
// the open thing has. A zone tagged data-context (the Out of quota section)
// wins while focus is in it; then the page; then the open item's kind.
ctx.context = () => {
  const zone = /** @type {HTMLElement | null} */ (document.activeElement?.closest?.('[data-context]'));
  if (zone) return zone.dataset.context ?? '';
  if (route.settings) return 'settings';
  if (route.verbatim) return 'verbatim';
  if (!route.item) return '';
  const r = ctx.item();
  const e = r?.item?.event;
  // The item still loading: the last one's kind is not this one's.
  if (r?.item?.display_id !== route.item && e?.id !== route.item) return '';
  if (r?.task) return 'task';
  if (e?.kind === 'deliverable') return 'deliverable';
  if (e?.kind === 'ask' && e.meta?.screen && !r.answered) return 'screen';
  if (paneEl('detail')?.querySelector('[data-cmd="deliver-retry"], [data-cmd="voice-retry"], [data-cmd="voice-omit"]')) return 'message';
  return '';
};

// Selecting.
C('select', 'Select or unselect the item', () => (route.item ? toggle(route.item) : say('Nothing open')));
C('visual', 'Visual mode: select from here as you move', visual);
C('select-all', 'Select every item in the list', () => toggleAll(sel.order));
C('select-clear', 'Clear the selection', () => (setVisual(''), clear()));

// The list.
C('first-item', 'First item', () => edge(false), { pane: 'list' });
C('last-item', 'Last item', () => edge(true), { pane: 'list' });
C('next-section', 'Next section', (n) => section(n), { pane: 'list' });
C('prev-section', 'Previous section', (n) => section(-n), { pane: 'list' });
C('fold', 'Fold or unfold this section', () => toggleSection(sectionHere()), { pane: 'list' });
C('fold-close', 'Fold this section', () => toggleSection(sectionHere(), false), { pane: 'list' });
C('fold-open', 'Unfold this section', () => toggleSection(sectionHere(), true), { pane: 'list' });
C('fold-all', 'Fold every section', () => foldAll(false), { pane: 'list' });
C('unfold-all', 'Unfold every section', () => foldAll(true), { pane: 'list' });
C('list-half-down', 'Scroll down half a page', () => scroll('list', 0.5), { pane: 'list' });
C('list-half-up', 'Scroll up half a page', () => scroll('list', -0.5), { pane: 'list' });
C('read-in-detail', 'Read the item in the detail pane', () => focusPane('detail'), { pane: 'list' });

// The detail pane.
C('scroll-down', 'Scroll down', (n) => scroll('detail', 60 * n), { pane: 'detail' });
C('scroll-up', 'Scroll up', (n) => scroll('detail', -60 * n), { pane: 'detail' });
C('half-down', 'Scroll down half a page', () => scroll('detail', 0.5), { pane: 'detail' });
C('half-up', 'Scroll up half a page', () => scroll('detail', -0.5), { pane: 'detail' });
C('page-down', 'Scroll down a page', () => scroll('detail', 0.9), { pane: 'detail' });
C('page-up', 'Scroll up a page', () => scroll('detail', -0.9), { pane: 'detail' });
C('scroll-top', 'Scroll to the top', () => scroll('detail', 'top'), { pane: 'detail' });
C('scroll-bottom', 'Scroll to the bottom', () => scroll('detail', 'bottom'), { pane: 'detail' });
C('back-to-list', 'Back to the list', () => focusPane('list'), { pane: 'detail' });
C('close', 'Close the item or page', escape, { pane: 'detail' });

// The sidebar.
C('side-next', 'Next entry', (n) => moveFocus(paneEl('side'), n), { pane: 'side' });
C('side-prev', 'Previous entry', (n) => moveFocus(paneEl('side'), -n), { pane: 'side' });
C('side-first', 'First entry', () => moveFocus(paneEl('side'), -Infinity), { pane: 'side' });
C('side-last', 'Last entry', () => moveFocus(paneEl('side'), Infinity), { pane: 'side' });
C('side-open', 'Open the entry', sideFocus, { pane: 'side' });

// A sheet.
C('sheet-next', 'Next control', (n) => moveFocus(dialog(), n), { pane: 'dialog' });
C('sheet-prev', 'Previous control', (n) => moveFocus(dialog(), -n), { pane: 'dialog' });
C('sheet-first', 'First control', () => moveFocus(dialog(), -Infinity), { pane: 'dialog' });
C('sheet-last', 'Last control', () => moveFocus(dialog(), Infinity), { pane: 'dialog' });
C('sheet-close', 'Close the sheet', closeSheet, { pane: 'dialog' });
C('image-prev', 'Image viewer: the previous image', () => click('image-prev'), { pane: 'dialog', ...tagged('image-prev', 'the image viewer, on an item with several images') });
C('image-next', 'Image viewer: the next image', () => click('image-next'), { pane: 'dialog', ...tagged('image-next', 'the image viewer, on an item with several images') });

// --- keys ------------------------------------------------------------------------
// Keys live in keys.edn (and the user's keys file over it); the commands
// above must all be defined before the keymap is built against them.
setKeymap();
