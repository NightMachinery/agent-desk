// What people are taught about the keys: the key each button's tooltip
// names, and the cheatsheet `?` opens. Both read the keymap in use
// (keys.edn and the user's file), so they name the user's keys.
import { keyFor, keyIn, localleaders } from './keys.svelte.js';

/** The command a button (data-cmd) runs, where its name differs; a list
 * names both directions of a toggle. */
const buttonCmds = {
  'read-toggle': ['read', 'unread'],
  dismiss: 'read',
  undismiss: 'unread',
  'copy-link': 'yank-link',
  'copy-id': 'yank-id',
  'copy-resume': 'yank-resume',
  'copy-paseo': 'yank-paseo',
  quick: 'take',
  'task-new': 'task-add',
  'node-task-add': 'task-add',
  'message-node': 'node-message',
  'read-all': 'node-read-all',
  'velocity-report': 'node-velocity',
  'select-mode': 'toggle-select',
  'show-all': 'toggle-all',
  sort: 'toggle-old',
  'bulk-pin': 'pin',
  'bulk-unread': 'unread',
  'bulk-read': 'read',
  prev: 'prev-item',
  next: 'next-item',
};
/** Buttons whose key is not a command's: the composer sends on its own. */
const fixedKeys = { send: '⌘ RET' };

/** The key for a button, '' for none. */
export function buttonKey(button) {
  if (fixedKeys[button]) return fixedKeys[button];
  const cmds = [buttonCmds[button] ?? (button.startsWith('fold-') ? 'fold' : button)].flat();
  return cmds.map(keyFor).filter(Boolean).join(' · ');
}

/** A tooltip that teaches the key: "Pin to the top  (p)". */
export function tip(label, button) {
  const k = buttonKey(button);
  return k ? `${label}  (${k})` : label;
}

// A cheatsheet key: a command's key (in one map; prefer: the chord to show
// while it is still bound to it), the localleader keys (LL), or a literal
// string.
const k = (cmd, map = '', prefer = '') => ({ cmd, map, prefer });
const LL = /** @type {{localleader: true}} */ ({ localleader: true });

/** @typedef {string | {cmd: string, map: string, prefer: string} | {localleader: true}} SheetKey */
/** The cheatsheet: what is worth learning first, by task.
 * @type {{title: string, rows: [SheetKey[], string][]}[]} */
const cheatsheet = [
  {
    title: 'Move',
    rows: [
      [[k('next-item', 'list'), k('prev-item', 'list')], 'Next or previous item'],
      [[k('next-item', 'normal'), k('prev-item', 'normal')], 'Next or previous, from any pane'],
      [[k('next-open'), k('prev-open')], 'Next open item, or back along those steps'],
      [[k('first-item'), k('last-item')], 'First or last item'],
      [[k('next-section'), k('prev-section')], 'Next or previous section'],
      [[k('list-half-down'), k('list-half-up')], 'Half a page down or up'],
      [['5j'], 'A count repeats a motion'],
    ],
  },
  {
    title: 'The open item',
    rows: [
      [[k('answer')], 'Answer the question'],
      [[k('take')], 'Take the recommendation or default'],
      [[LL], 'What only it has: a task, a deliverable, Settings…'],
      [[k('reply-entry')], 'Reply to an entry of its conversation'],
      [[k('message')], 'Message its agent'],
      [[k('read'), k('unread')], 'Read (dismiss) or unread'],
      [[k('pin')], 'Pin or unpin'],
      [[k('close')], 'Close it'],
    ],
  },
  {
    title: 'Copy and go',
    rows: [
      [[k('yank-link')], 'Link to the item'],
      [[k('yank-paseo')], "Its agent's Paseo id"],
      [[k('yank-hint')], 'Any event on screen, by hint: its id and text'],
      [[k('yank-resume')], "Its session's resume command"],
      [[k('open-node')], 'Its node'],
      [[k('session')], 'Everything its session wrote'],
      [[k('up')], 'Up one level'],
    ],
  },
  {
    title: 'Leader',
    rows: [
      [[k('find')], 'Any command or place, by name'],
      [[k('m-x', 'normal', 'M-x'), k('m-x', 'normal', ':')], 'Commands only, by name'],
      [[k('switch-item')], 'Switch to an item'],
      [['SPC b', 'SPC d'], 'The open item; the node shown'],
      [[k('task-add'), 'SPC n'], 'New task; new anything'],
      [[k('verbatim'), 'SPC o'], 'Verbatim log; open pages'],
      [[k('describe-key'), k('describe-command')], 'What a key or command does'],
    ],
  },
  {
    title: 'Sections and panes',
    rows: [
      [[k('fold')], 'Fold or unfold this section'],
      [[k('fold-all'), k('unfold-all')], 'Fold or unfold all'],
      [[k('pane-left'), k('pane-right')], 'Pane to the left or right'],
      [[k('read-in-detail', 'list', 'l'), k('back-to-list')], 'Into the item, back to the list'],
      [[k('search')], 'Search'],
      [[k('refresh')], 'Refresh'],
    ],
  },
  {
    title: 'Select, hints and typing',
    rows: [
      [[k('select')], 'Select the item'],
      [[k('visual')], 'Visual mode: select by moving'],
      [[k('hints')], 'Hints: click anything by label'],
      [[k('insert')], 'Back into the box Esc left, else the first message box'],
      [['⌘ RET'], 'Send'],
      [['Esc', 'C-g'], 'Leave a box; back out one step'],
    ],
  },
];

/** The cheatsheet with the keys in use; a row whose commands have no key
 * is left out. */
export function cheatsheetNow() {
  return cheatsheet
    .map((c) => ({
      title: c.title,
      rows: c.rows
        .map(([ks, desc]) => [ks.flatMap((x) => (typeof x === 'string' ? x : 'localleader' in x ? localleaders() : keyIn(x.cmd, x.map, x.prefer))).filter(Boolean), desc])
        .filter(([ks]) => ks.length),
    }))
    .filter((c) => c.rows.length);
}
