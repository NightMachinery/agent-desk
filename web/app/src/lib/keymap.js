// The commands and their keys. Most commands press the same button a mouse
// would (tagged data-cmd in the components), so a key and a click do exactly
// the same. Each command is defined once with command(name, ...); keys are
// bound to its name with bind(keys, name), so M-x, SPC SPC and help list a
// command even when it has no key.
import { route, go, pageOpen } from './route.svelte.js';
import { nav, step } from './nav.svelte.js';
import { sel, toggle, toggleAll, clear } from './select.svelte.js';
import { copy } from './format.js';
import { toast } from './toast.svelte.js';
import { toggleSection } from './ui.svelte.js';
import {
  keys, ctx, command, bind, group, pane, paneEl, cmdEl, say, dialog, moveFocus, focusables, startHints, openPalette,
  commands, commandItems, keysOf, applies, describeKey, describeCommand, setVisual, syncMode,
} from './keys.svelte.js';

// --- helpers -----------------------------------------------------------------

const has = (name) => () => !!cmdEl(name);
function click(name, missing) {
  const el = cmdEl(name);
  if (!el) return say(missing);
  if (el instanceof HTMLButtonElement && el.disabled) return say(missing);
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
const sections = ['needs', 'quota', 'pinned', 'deliverables', 'responses', 'new', 'runs', 'deferred'];
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

function escape() {
  if (keys.anchor) {
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
    const title = document.getElementById('task-title');
    if (title) return title.focus();
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

const mx = () => openPalette('M-x', commandItems());

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
const anything = () => openPalette('Command or place', [...commandItems(), ...places()]);

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
const closeSheet = () => dialog()?.querySelector('.head button')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
const tagged = (name, need) => ({ when: has(name), need });

// Help and pickers.
C('cheatsheet', 'The cheatsheet', () => (keys.help = 'sheet'), { anywhere: true });
C('bindings', 'Show every key binding here', () => (keys.help = 'all'));
C('describe-key', 'Describe a key: what it runs, without running it', describeKey);
C('describe-command', 'Describe a command: its keys and where it applies', describeFn);
C('m-x', 'Run a command by name (M-x)', mx, { palette: false, anywhere: true });
C('find', 'Find a command or a place (SPC SPC)', anything, { palette: false });
C('go-node', 'Go to a project, run or agent', nodes);
C('switch-item', 'Switch to an item in the list', items);
C('hints', 'Click something on screen (hints)', startHints, { anywhere: true });
C('search', 'Search', search);
C('search-clear', 'Clear the search', () => go({ q: '', item: '' }, { replace: true }), { when: () => !!route.q, need: 'a search' });
C('escape', 'Back out: visual mode, selection, item, page, search', escape, { palette: false });

// Going places.
C('next-item', 'Next item', (n) => stepBy(n));
C('prev-item', 'Previous item', (n) => stepBy(-n));
C('up', 'Up one level', parent);
C('top', 'Go to everything (the top)', () => go({ path: '', item: '', session: '', q: '' }));
C('settings', 'Settings here', () => go({ settings: true, item: '' }));
C('settings-global', 'Global settings', () => go({ path: '', settings: true, item: '', session: '' }));
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
C('compose', 'Write in the first message box', focusComposer);
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

// The node page and the list.
C('node-message', 'Message the coordinator', message, { when: () => !!ctx.view()?.target, need: 'a node with a coordinator' });
C('node-velocity', 'Ask for a velocity report', () => click('velocity-report', 'No velocity slot here'), tagged('velocity-report', 'a node with a velocity slot'));
C('node-read-all', 'Mark everything under the node read', () => click('read-all'), tagged('read-all', 'a node page'));
C('agent-archive', "Archive the node's coordinator agent in Paseo", () => click('agent-archive'), tagged('agent-archive', 'a node whose coordinator is a Paseo agent here'));
C('node-settings', "The node's notify settings", () => click('node-settings'), tagged('node-settings', 'a node page'));
C('list-read-all', 'Mark every unread item in the list read', () => click('mark-all-read'), tagged('mark-all-read', 'unread items in the list'));
C('quota-copy-ids', 'Out of quota: copy the picked agent ids', () => click('quota-copy-ids'), tagged('quota-copy-ids', 'the Out of quota section'));
C('deliverable-open', 'Deliverable: open its link', () => click('deliverable-open'), tagged('deliverable-open', 'a deliverable with a link'));
C('deliverable-download', 'Deliverable: download its first file', () => click('deliverable-download'), tagged('deliverable-download', 'a deliverable with a file'));
C('deliverable-works', 'Deliverable: tell the agent it works', () => click('deliverable-works'), tagged('deliverable-works', 'a deliverable whose agent can be messaged'));
C('deliverable-problem', 'Deliverable: tell the agent what is wrong (opens the message box)', () => click('deliverable-problem'), tagged('deliverable-problem', 'a deliverable whose agent can be messaged'));
C('quota-archive', 'Out of quota: archive the first agent in Paseo', () => click('quota-archive'), tagged('quota-archive', 'an agent out of quota'));
C('quota-replace', 'Out of quota: replace the first agent (pick its provider, model and effort)', () => click('quota-replace'), tagged('quota-replace', 'an agent out of quota that can be replaced'));

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

// --- groups --------------------------------------------------------------------

group('SPC', 'leader');
group('SPC b', '+items');
group('SPC h', '+help');
group('SPC i', '+item');
group('SPC n', '+node');
group('SPC o', '+open');
group('SPC p', '+project');
group('SPC s', '+search');
group('SPC t', '+toggle');
group('SPC w', '+window');
group('SPC x', '+select');
group('g', '+go');
group('y', '+yank');
group('z', '+fold');
group('C-w', '+window');

// --- keys -------------------------------------------------------------------------

// Everywhere.
bind('?', 'cheatsheet');
bind('SPC ?', 'bindings');
bind('SPC h b', 'bindings');
bind('SPC h k', 'describe-key');
bind('SPC h f', 'describe-command');
bind(':', 'm-x');
bind('M-x', 'm-x');
bind('SPC :', 'm-x', { anywhere: false });
bind('SPC SPC', 'find');
bind('SPC p p', 'go-node');
bind('SPC ,', 'switch-item');
bind('SPC b b', 'switch-item');
bind('f', 'hints');
bind('SPC j', 'hints', { anywhere: false });
bind('/', 'search');
bind('SPC s s', 'search');
bind('SPC s c', 'search-clear');
bind('ESC', 'escape');
bind('C-g', 'escape');

bind('J', 'next-item');
bind('K', 'prev-item');
bind('C-j', 'next-item');
bind('C-k', 'prev-item');
bind('-', 'up');
bind('SPC p u', 'up');
bind('g h', 'top');
bind('SPC p h', 'top');
bind('g s', 'settings');
bind('g S', 'settings-global');
bind('g v', 'verbatim');
bind('g V', 'verbatim-all');
bind('g r', 'refresh');
bind('g o', 'open-node');
bind('g w', 'session');
bind('g t', 'thread-root');
bind('SPC o s', 'settings');
bind('SPC o S', 'settings-global');
bind('SPC o v', 'verbatim');
bind('SPC o V', 'verbatim-all');
bind('SPC o r', 'recordings');
bind('SPC o t', 'tasks');
bind('SPC o T', 'tasks-all');
bind('g c', 'tasks');
bind('g C', 'tasks-all');
bind('SPC n t', 'task-add');

bind('SPC t a', 'toggle-all');
bind('SPC t o', 'toggle-old');
bind('SPC t s', 'toggle-select');

bind('SPC w h', 'pane-left');
bind('SPC w l', 'pane-right');
bind('SPC w w', 'pane-next');
bind('SPC w s', 'pane-side');
bind('SPC w d', 'pane-detail');
bind('C-w h', 'pane-left');
bind('C-w l', 'pane-right');
bind('C-w w', 'pane-next');

// The open item (or the selection).
bind('a', 'answer');
bind('SPC i a', 'answer');
bind('r', 'message');
bind('SPC i r', 'message');
bind('i', 'compose');
bind('t', 'take');
bind('SPC i t', 'take');
bind('d', 'read');
bind('SPC i d', 'read');
bind('U', 'unread');
bind('SPC i u', 'unread');
bind('p', 'pin');
bind('SPC i p', 'pin');
bind('SPC i o', 'open-node');
bind('SPC i n', 'notify-menu');
bind('SPC i m', 'send-menu');
bind('SPC i g', 'deliverable-open');
bind('SPC i G', 'deliverable-download');
bind('SPC i w', 'deliverable-works');
bind('y y', 'yank-link');
bind('y i', 'yank-id');
bind('y r', 'yank-resume');
bind('y a', 'yank-paseo');
bind('y p', 'yank-path');

// The node page (nothing open).
bind('SPC n m', 'node-message');
bind('SPC n v', 'node-velocity');
bind('SPC n r', 'node-read-all');
bind('SPC n s', 'node-settings');
bind('SPC n a', 'agent-archive');

// Selecting.
bind('x', 'select');
bind('V', 'visual');
bind('v', 'visual');
bind('SPC x a', 'select-all');
bind('SPC x c', 'select-clear');

// The list.
bind('j', 'next-item', { pane: 'list' });
bind('k', 'prev-item', { pane: 'list' });
bind('<down>', 'next-item', { pane: 'list' });
bind('<up>', 'prev-item', { pane: 'list' });
bind('g g', 'first-item');
bind('G', 'last-item');
bind('}', 'next-section');
bind('{', 'prev-section');
bind('z a', 'fold');
bind('z c', 'fold-close');
bind('z o', 'fold-open');
bind('z M', 'fold-all');
bind('z R', 'unfold-all');
bind('C-d', 'list-half-down');
bind('C-u', 'list-half-up');
bind('RET', 'read-in-detail');
bind('l', 'read-in-detail');
bind('o', 'read-in-detail');
bind('h', 'pane-side', { pane: 'list' });

// The detail pane.
bind('j', 'scroll-down');
bind('k', 'scroll-up');
bind('<down>', 'scroll-down');
bind('<up>', 'scroll-up');
bind('C-d', 'half-down');
bind('C-u', 'half-up');
bind('C-f', 'page-down');
bind('C-b', 'page-up');
bind('g g', 'scroll-top');
bind('G', 'scroll-bottom');
bind('h', 'back-to-list');
bind('q', 'close');

// The sidebar.
bind('j', 'side-next');
bind('k', 'side-prev');
bind('<down>', 'side-next');
bind('<up>', 'side-prev');
bind('g g', 'side-first');
bind('G', 'side-last');
bind('l', 'side-open');
bind('o', 'side-open');
bind('RET', 'side-open');

// A sheet.
bind('j', 'sheet-next');
bind('k', 'sheet-prev');
bind('C-n', 'sheet-next');
bind('C-p', 'sheet-prev');
bind('<down>', 'sheet-next');
bind('<up>', 'sheet-prev');
bind('g g', 'sheet-first');
bind('G', 'sheet-last');
bind('C-g', 'sheet-close');
bind('q', 'sheet-close');
