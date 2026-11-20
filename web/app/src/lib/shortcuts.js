// What people are taught about the keys: the cheatsheet `?` opens, and the
// key each button's tooltip names. The bindings themselves are in keymap.js;
// keep the two in step when a key changes.

/** The key for each command button (data-cmd), for its tooltip. */
export const cmdKeys = {
  pin: 'p',
  'read-toggle': 'd · U',
  dismiss: 'd',
  undismiss: 'U',
  'copy-link': 'y y',
  'copy-id': 'y i',
  'copy-resume': 'y r',
  'copy-paseo': 'y a',
  session: 'g w',
  'open-node': 'g o',
  'thread-root': 'g t',
  quick: 't',
  send: '⌘ RET',
  'notify-menu': 'SPC i n',
  'send-menu': 'SPC i m',
  'node-settings': 'SPC n s',
  'agent-archive': 'SPC n a',
  'message-node': 'SPC n m',
  'read-all': 'SPC n r',
  'velocity-report': 'SPC n v',
  'select-mode': 'SPC t s',
  'show-all': 'SPC t a',
  sort: 'SPC t o',
  search: '/',
  'bulk-pin': 'p',
  'bulk-unread': 'U',
  'bulk-read': 'd',
  close: 'q',
  prev: 'K',
  next: 'J',
  'fold-needs': 'z a',
  'fold-pinned': 'z a',
  'fold-responses': 'z a',
  'fold-new': 'z a',
  'fold-runs': 'z a',
  'fold-deferred': 'z a',
  'fold-quota': 'z a',
};

/** A tooltip that teaches the key: "Pin to the top (p)". */
export function tip(label, cmd) {
  const k = cmdKeys[cmd];
  return k ? `${label}  (${k})` : label;
}

/** The cheatsheet: what is worth learning first, by task. */
export const cheatsheet = [
  {
    title: 'Move',
    rows: [
      [['j', 'k'], 'Next or previous item'],
      [['J', 'K'], 'Next or previous, from any pane'],
      [['g g', 'G'], 'First or last item'],
      [['}', '{'], 'Next or previous section'],
      [['C-d', 'C-u'], 'Half a page down or up'],
      [['5j'], 'A count repeats a motion'],
    ],
  },
  {
    title: 'The open item',
    rows: [
      [['a'], 'Answer the question'],
      [['t'], 'Take the recommendation or default'],
      [['r'], 'Message its agent'],
      [['d', 'U'], 'Read (dismiss) or unread'],
      [['p'], 'Pin or unpin'],
      [['q'], 'Close it'],
    ],
  },
  {
    title: 'Copy and go',
    rows: [
      [['y y'], 'Link to the item'],
      [['y a'], "Its agent's Paseo id"],
      [['y r'], "Its session's resume command"],
      [['g o'], 'Its node'],
      [['g w'], 'Everything its session wrote'],
      [['-'], 'Up one level'],
    ],
  },
  {
    title: 'Leader',
    rows: [
      [['SPC SPC'], 'Any command or place, by name'],
      [['M-x', ':'], 'Commands only, by name'],
      [['SPC ,'], 'Switch to an item'],
      [['SPC i', 'SPC n'], 'Item and node commands'],
      [['g v', 'SPC o'], 'Verbatim log; open pages'],
      [['SPC h k', 'SPC h f'], 'What a key or command does'],
    ],
  },
  {
    title: 'Sections and panes',
    rows: [
      [['z a'], 'Fold or unfold this section'],
      [['z M', 'z R'], 'Fold or unfold all'],
      [['C-w h', 'C-w l'], 'Pane to the left or right'],
      [['l', 'h'], 'Into the item, back to the list'],
      [['/'], 'Search'],
      [['g r'], 'Refresh'],
    ],
  },
  {
    title: 'Select, hints and typing',
    rows: [
      [['x'], 'Select the item'],
      [['V'], 'Visual mode: select by moving'],
      [['f'], 'Hints: click anything by label'],
      [['i'], 'The first message box'],
      [['⌘ RET'], 'Send'],
      [['Esc', 'C-g'], 'Leave a box; back out one step'],
    ],
  },
];
