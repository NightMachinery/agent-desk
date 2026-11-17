// The bindings. Most commands press the same button a mouse would (tagged
// data-cmd in the components), so a key and a click do exactly the same.
import { route, go } from './route.svelte.js';
import { nav, step } from './nav.svelte.js';
import { sel, toggle, toggleAll, clear } from './select.svelte.js';
import { copy } from './format.js';
import { toast } from './toast.svelte.js';
import { toggleSection } from './ui.svelte.js';
import {
  keys, ctx, bind, group, pane, paneEl, cmdEl, say, dialog, moveFocus, focusables, startHints, openPalette,
  activeBindings, setVisual, syncMode,
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
const sections = ['needs', 'pinned', 'responses', 'new', 'runs', 'deferred'];
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
    if (p === 'list' && (route.item || route.settings)) return escape();
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
    if (!route.item && !route.settings && nav.order.length && !ctx.view()?.node) openItem(nav.order[0]);
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
  } else if (route.settings) go({ settings: false });
  else if (route.q) go({ q: '' }, { replace: true });
  else /** @type {HTMLElement} */ (document.activeElement)?.blur?.();
}

function parent() {
  if (!route.path) return say('Already at the top');
  go({ path: route.path.split('/').slice(0, -1).join('/'), item: '' });
}

// --- pickers -------------------------------------------------------------------

function commands() {
  const seen = new Set();
  const items = [];
  for (const b of activeBindings()) {
    if (b.palette === false || seen.has(b.desc)) continue;
    seen.add(b.desc);
    const keysFor = activeBindings()
      .filter((x) => x.desc === b.desc)
      .map((x) => x.keys);
    items.push({ label: b.desc, key: keysFor.slice(0, 2).join(', '), run: () => b.run(1) });
  }
  openPalette('M-x', items.sort((a, b) => a.label.localeCompare(b.label)));
}

function nodes() {
  const out = [{ label: 'Everything', detail: '', run: () => go({ path: '', item: '', session: '' }) }];
  const walk = (ns) => {
    for (const n of ns ?? []) {
      out.push({ label: n.path, detail: n.state, run: () => go({ path: n.path, item: '', session: '' }) });
      walk(n.children);
    }
  };
  walk(ctx.runs());
  openPalette('Go to project, run or agent', out);
}

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

// --- groups --------------------------------------------------------------------

group('SPC', 'leader');
group('SPC b', '+items');
group('SPC h', '+help');
group('SPC i', '+item');
group('SPC n', '+node');
group('SPC p', '+project');
group('SPC s', '+search');
group('SPC t', '+toggle');
group('SPC w', '+window');
group('SPC x', '+select');
group('g', '+go');
group('y', '+yank');
group('C-w', '+window');

// --- everywhere ------------------------------------------------------------------

bind('?', 'The cheatsheet', () => (keys.help = 'sheet'), { anywhere: true });
bind('SPC h b', 'Show every key binding', () => (keys.help = 'all'));
bind(':', 'Run a command (M-x)', commands, { palette: false, anywhere: true });
bind('M-x', 'Run a command (M-x)', commands, { palette: false, anywhere: true });
bind('SPC :', 'Run a command (M-x)', commands, { palette: false });
bind('SPC SPC', 'Go to a project, run or agent', nodes);
bind('SPC p p', 'Go to a project, run or agent', nodes);
bind('SPC ,', 'Switch to an item in the list', items);
bind('SPC b b', 'Switch to an item in the list', items);
bind('f', 'Click something on screen (hints)', startHints, { anywhere: true });
bind('SPC j', 'Click something on screen (hints)', startHints);
bind('/', 'Search', () => focusBox('search', 'No search box'));
bind('SPC s s', 'Search', () => focusBox('search', 'No search box'));
bind('SPC s c', 'Clear the search', () => go({ q: '', item: '' }, { replace: true }), { when: () => !!route.q });
bind('ESC', 'Back out: visual mode, selection, item, settings, search', escape, { palette: false });
bind('C-g', 'Back out: visual mode, selection, item, settings, search', escape, { palette: false });

bind('J', 'Next item', (n) => stepBy(n));
bind('K', 'Previous item', (n) => stepBy(-n));
bind('C-j', 'Next item', (n) => stepBy(n));
bind('C-k', 'Previous item', (n) => stepBy(-n));
bind('-', 'Up one level', parent);
bind('SPC p u', 'Up one level', parent);
bind('g h', 'Go to everything (the top)', () => go({ path: '', item: '', session: '', q: '' }));
bind('SPC p h', 'Go to everything (the top)', () => go({ path: '', item: '', session: '', q: '' }));
bind('g s', 'Settings here', () => go({ settings: true, item: '' }));
bind('g S', 'Global settings', () => go({ path: '', settings: true, item: '', session: '' }));
bind('g r', 'Refresh', () => (ctx.refresh(), say('Refreshed')));
bind('g o', "Open the item's node", () => click('open-node', 'Nothing open'), { when: open });
bind('g w', 'Show everything its session wrote', () => click('session', 'No session'), { when: has('session') });
bind('g t', 'Go to the item this conversation is about', () => click('thread-root'), { when: has('thread-root') });

bind('SPC t a', 'Toggle show all (read items, finished runs)', () => go({ all: !route.all, item: '' }));
bind('SPC t o', 'Toggle oldest first', () => go({ old: !route.old }, { replace: true }));
bind('SPC t s', 'Toggle select mode', () => click('select-mode'));

bind('SPC w h', 'Focus the pane on the left', () => nextPane(-1));
bind('SPC w l', 'Focus the pane on the right', () => nextPane(1));
bind('SPC w w', 'Focus the next pane', () => nextPane(1));
bind('SPC w s', 'Focus the sidebar', () => focusPane('side'));
bind('SPC w d', 'Focus the detail pane', () => focusPane('detail'));
bind('C-w h', 'Focus the pane on the left', () => nextPane(-1));
bind('C-w l', 'Focus the pane on the right', () => nextPane(1));
bind('C-w w', 'Focus the next pane', () => nextPane(1));

// The open item (or the selection).
bind('a', 'Answer the question', () => focusBox('answer-box', 'Not an open question'));
bind('SPC i a', 'Answer the question', () => focusBox('answer-box', 'Not an open question'));
bind('r', 'Message its agent', message);
bind('SPC i r', 'Message its agent', message);
bind('i', 'Write in the first message box', focusComposer);
bind('t', 'Take the recommendation or the default', () => click('quick', 'Not an open question'), { when: has('quick') });
bind('SPC i t', 'Take the recommendation or the default', () => click('quick'), { when: has('quick') });
bind('d', 'Mark read (a question: dismiss it)', after(markRead));
bind('SPC i d', 'Mark read (a question: dismiss it)', after(markRead));
bind('U', 'Mark unread (a question: back to Needs you)', after(markUnread));
bind('SPC i u', 'Mark unread (a question: back to Needs you)', after(markUnread));
bind('p', 'Pin or unpin', after(pin));
bind('SPC i p', 'Pin or unpin', after(pin));
bind('SPC i o', "Open the item's node", () => click('open-node', 'Nothing open'), { when: open });
bind('SPC i n', 'Notify me: where its response notifies you', () => click('notify-menu', 'No message box'), { when: has('notify-menu') });
bind('SPC i m', 'Send how? (steer, queue, interrupt)', () => click('send-menu', 'Write the message first'), { when: has('send-menu') });
bind('y y', 'Copy a link to the item', () => yank(open() ? location.href : '', 'Link'));
bind('y i', "Copy the item's id", () => yank(route.item, 'Id'));
bind('y r', 'Copy the resume command', () => yank(ctx.item()?.resume ?? '', 'Resume command'));
bind('y a', "Copy the agent's Paseo id", () => yank(ctx.item()?.from?.paseo || ctx.view()?.target?.paseo || '', 'Paseo id'));
bind('y p', 'Copy the path shown', () => yank(ctx.item()?.node ?? route.path, 'Path'));

// The node page (nothing open).
bind('SPC n m', 'Message the coordinator', message, { when: () => !!ctx.view()?.target });
bind('SPC n v', 'Ask for a velocity report', () => click('velocity-report', 'No velocity slot here'), { when: has('velocity-report') });
bind('SPC n r', 'Mark everything under the node read', () => click('read-all'), { when: has('read-all') });
bind('SPC n s', "The node's notify settings", () => click('node-settings'), { when: has('node-settings') });

// Selecting.
bind('x', 'Select or unselect the item', () => (route.item ? toggle(route.item) : say('Nothing open')));
bind('V', 'Visual mode: select from here as you move', () => {
  if (!route.item) edge(false);
  if (!route.item) return;
  setVisual(route.item);
  sel.ids.add(route.item);
});
bind('v', 'Visual mode: select from here as you move', () => {
  if (!route.item) edge(false);
  if (!route.item) return;
  setVisual(route.item);
  sel.ids.add(route.item);
}, { palette: false });
bind('SPC x a', 'Select every item in the list', () => toggleAll(sel.order));
bind('SPC x c', 'Clear the selection', () => (setVisual(''), clear()));

// --- the list --------------------------------------------------------------------

bind('j', 'Next item', (n) => stepBy(n), { pane: 'list' });
bind('k', 'Previous item', (n) => stepBy(-n), { pane: 'list' });
bind('<down>', 'Next item', (n) => stepBy(n), { pane: 'list', palette: false });
bind('<up>', 'Previous item', (n) => stepBy(-n), { pane: 'list', palette: false });
bind('g g', 'First item', () => edge(false), { pane: 'list' });
bind('G', 'Last item', () => edge(true), { pane: 'list' });
bind('}', 'Next section', (n) => section(n), { pane: 'list' });
bind('{', 'Previous section', (n) => section(-n), { pane: 'list' });
group('z', '+fold');
bind('z a', 'Fold or unfold this section', () => toggleSection(sectionHere()), { pane: 'list' });
bind('z c', 'Fold this section', () => toggleSection(sectionHere(), false), { pane: 'list' });
bind('z o', 'Unfold this section', () => toggleSection(sectionHere(), true), { pane: 'list' });
bind('z M', 'Fold every section', () => foldAll(false), { pane: 'list' });
bind('z R', 'Unfold every section', () => foldAll(true), { pane: 'list' });
bind('C-d', 'Scroll down half a page', () => scroll('list', 0.5), { pane: 'list' });
bind('C-u', 'Scroll up half a page', () => scroll('list', -0.5), { pane: 'list' });
bind('RET', 'Read the item in the detail pane', () => focusPane('detail'), { pane: 'list' });
bind('l', 'Read the item in the detail pane', () => focusPane('detail'), { pane: 'list' });
bind('o', 'Read the item in the detail pane', () => focusPane('detail'), { pane: 'list', palette: false });
bind('h', 'Focus the sidebar', () => focusPane('side'), { pane: 'list' });

// --- the detail pane ----------------------------------------------------------

bind('j', 'Scroll down', (n) => scroll('detail', 60 * n), { pane: 'detail' });
bind('k', 'Scroll up', (n) => scroll('detail', -60 * n), { pane: 'detail' });
bind('<down>', 'Scroll down', (n) => scroll('detail', 60 * n), { pane: 'detail', palette: false });
bind('<up>', 'Scroll up', (n) => scroll('detail', -60 * n), { pane: 'detail', palette: false });
bind('C-d', 'Scroll down half a page', () => scroll('detail', 0.5), { pane: 'detail' });
bind('C-u', 'Scroll up half a page', () => scroll('detail', -0.5), { pane: 'detail' });
bind('C-f', 'Scroll down a page', () => scroll('detail', 0.9), { pane: 'detail' });
bind('C-b', 'Scroll up a page', () => scroll('detail', -0.9), { pane: 'detail' });
bind('g g', 'Scroll to the top', () => scroll('detail', 'top'), { pane: 'detail' });
bind('G', 'Scroll to the bottom', () => scroll('detail', 'bottom'), { pane: 'detail' });
bind('h', 'Back to the list', () => focusPane('list'), { pane: 'detail' });
bind('q', 'Close the item', escape, { pane: 'detail' });

// --- the sidebar -----------------------------------------------------------------

bind('j', 'Next entry', (n) => moveFocus(paneEl('side'), n), { pane: 'side' });
bind('k', 'Previous entry', (n) => moveFocus(paneEl('side'), -n), { pane: 'side' });
bind('<down>', 'Next entry', (n) => moveFocus(paneEl('side'), n), { pane: 'side', palette: false });
bind('<up>', 'Previous entry', (n) => moveFocus(paneEl('side'), -n), { pane: 'side', palette: false });
bind('g g', 'First entry', () => moveFocus(paneEl('side'), -Infinity), { pane: 'side' });
bind('G', 'Last entry', () => moveFocus(paneEl('side'), Infinity), { pane: 'side' });
bind('l', 'Open the entry', () => /** @type {HTMLElement} */ (document.activeElement)?.click?.(), { pane: 'side' });
bind('o', 'Open the entry', () => /** @type {HTMLElement} */ (document.activeElement)?.click?.(), { pane: 'side', palette: false });
bind('RET', 'Open the entry', () => /** @type {HTMLElement} */ (document.activeElement)?.click?.(), { pane: 'side', palette: false });

// --- a sheet ------------------------------------------------------------------------

bind('j', 'Next control', (n) => moveFocus(dialog(), n), { pane: 'dialog' });
bind('k', 'Previous control', (n) => moveFocus(dialog(), -n), { pane: 'dialog' });
bind('C-n', 'Next control', (n) => moveFocus(dialog(), n), { pane: 'dialog', palette: false });
bind('C-p', 'Previous control', (n) => moveFocus(dialog(), -n), { pane: 'dialog', palette: false });
bind('<down>', 'Next control', (n) => moveFocus(dialog(), n), { pane: 'dialog', palette: false });
bind('<up>', 'Previous control', (n) => moveFocus(dialog(), -n), { pane: 'dialog', palette: false });
bind('g g', 'First control', () => moveFocus(dialog(), -Infinity), { pane: 'dialog' });
bind('G', 'Last control', () => moveFocus(dialog(), Infinity), { pane: 'dialog' });
const closeSheet = () => dialog()?.querySelector('.head button')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
bind('C-g', 'Close the sheet', closeSheet, { pane: 'dialog', palette: false });
bind('q', 'Close the sheet', () => dialog()?.querySelector('.head button')?.dispatchEvent(new MouseEvent('click', { bubbles: true })), {
  pane: 'dialog',
});
