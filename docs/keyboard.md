# Driving the dashboard from the keyboard

The dashboard can be used without a mouse, in the style of Doom Emacs's evil
mode: modal keys, a `SPC` leader with a which-key popup, `M-x`, and link hints
for anything else. Press `?` on the page for a cheatsheet, and `TAB` in it
(or `SPC h b`) for every binding that applies where you are. Every button's
tooltip names its key. This page explains the model.

## Modes

- **Normal** (the default): keys are commands.
- **Insert**: a text box or a dropdown has focus, and keys type. `Esc`,
  `C-g` or `C-[` leaves it, keeping the text. In a message box,
  ⌘/Ctrl+Enter sends.
- **Visual**: `V` starts it on the open item; moving with `j`/`k` selects
  every row from there. Moving only selects: the rows it passes stay unread
  (opening an item reads it everywhere else). `d`, `U` and `p` then act on
  the selection; `Esc` ends it and clears the selection, reading nothing.

A number before a motion repeats it: `5j` moves five items down.

The mode line along the bottom shows the mode, the pane keys act on, keys
typed so far (`SPC i-`), the position in the list and the current view. It
appears only where a mouse or trackpad is the pointer, so a phone keeps its
layout.

## Panes

On a wide screen the page has three panes: the sidebar, the list and the
detail. Keys like `j`, `k`, `gg` and `G` act on the focused pane, which the
keyboard marks with a line along its top.

- `C-w h` / `C-w l` (or `SPC w h` / `SPC w l`) focus the pane to the left or
  right; `C-w w` the next one. Clicking in a pane focuses it too.
- In the list, `l`, `o` or `RET` reads the open item in the detail pane, and
  `h` goes to the sidebar. In the detail pane `h` comes back to the list and
  `q` closes the item.

On a phone there is one pane at a time: the list, or the open item.

## The list

- `j` / `k`: the next or previous item, opened as you move (it is marked
  read, as a click would). `gg` / `G`: the first or last item.
- `}` / `{`: the first item of the next or previous section (Needs you,
  Out of quota, Deliverables, Pinned, Responses to you, New).
- `za` folds or unfolds the section the open item is in; `zc` / `zo` fold
  or unfold it, `zM` / `zR` every section. A folded section stays folded on
  this device, and `j` / `k` skip its items. Clicking a section's title does
  the same.
- `C-d` / `C-u`: scroll half a page.
- `J` / `K` (or `C-j` / `C-k`) move to the next or previous item from any
  pane.
- `n`: the next open item, where answering would move on to: the next open
  question, else the next unread item, skipping read and pinned ones. `N`
  retraces your `n` steps, back to the item each came from, though opening
  it has read it by now; `n` then goes forward again along them. With nothing
  to retrace, `N` goes to the nearest open item before. When nothing open is
  left anywhere, `n` says "🎉 All caught up" (pressing it again pulses that
  toast instead of adding another); answering or sending on your last open
  item adds the same note to its toast.

## The detail pane

`j` / `k` scroll a line, `C-d` / `C-u` half a page, `C-f` / `C-b` a page,
`gg` / `G` to the top or bottom.

## The open item

- `a`: answer the question. `t`: take the recommendation or the default.
- `r`: message its agent (on a node page, the coordinator). `i`: the first
  message box on the page. `R`: reply to an entry of its conversation
  (with several, hints label the reply buttons): the box then replies to
  that entry, until you send or press the ✕ beside "Replying to"
  (`M-x reply-to-cancel`).
- `d`: mark read; on a question, dismiss it. `U`: mark unread; on a dismissed
  question, put it back in Needs you. `p`: pin or unpin. With a selection,
  these act on every selected item.
- `x`: select or unselect it.
- `yy` copies a link to it, `yi` its id, `ya` its agent's Paseo id, `yr`
  the session's resume command, `yp` the path. The two icons beside the
  agent's name copy the Paseo id and the resume command too.
- `go` opens its node, `gw` shows everything its session wrote, `gt` the item
  its conversation is about.

## The leader, SPC

`SPC` starts a sequence; wait a moment and the which-key popup lists what can
follow, with groups in colour.

- `SPC SPC`: find anything by name: every command that applies here (with its
  keys, so the palette also teaches them), then every project, run and agent.
- `SPC p p`: places only (projects, runs and agents).
- `SPC ,` or `SPC b b`: switch to an item in the list.
- `SPC :`, `:` or `M-x`: commands only. Every command is listed, including the
  ones with no key (screen requests, retrying a delivery, focus sets,
  auto-continue, reopening a finished task with a note).
- `SPC i`: the open item (`a` answer, `r` message, `d` read, `u` unread, `p`
  pin, `t` take, `o` its node, `n` "Notify me", `m` "Send how?"; on a
  deliverable, `g` open its link, `G` download its file, `w` Works: send
  with "This worked." in front; on an open task, `s` Status: ask whoever
  works on it where it stands; `x` discard the draft in the box, with
  Undo).
- Tasks: `g c` the Tasks page here, `g C` everywhere (`SPC o t`, `SPC o T`),
  `SPC n t` a new task (in its form `⌘ RET` files it, `ESC` in a field
  leaves insert mode and a second `ESC` closes the form, `C-g` closes it
  from anywhere); on the page `/` searches. The list's Tasks section
  folds like the others (`z a`, `z M`), `j`/`k` walk its rows, and `SPC t t`
  cycles its menu: open only, or also those finished in the last 15 minutes
  to 30 days.
- `SPC o o`: the order of the list's sections, in Settings, with the first
  handle focused; `↑` / `↓` on a handle move its section.
- `SPC n`: the node page (`m` message the coordinator, `r` mark everything
  under it read, `s` its notify settings, `v` ask for a velocity report, `a`
  archive its coordinator's Paseo agent).
- `SPC s s` search, `SPC s c` clear the search; `/` searches too.
- `SPC t a` show all, `SPC t o` oldest first, `SPC t s` select mode, `SPC t d`
  which deliverables to list (unread latest, latest, all).
- `SPC x a` select every item, `SPC x c` clear the selection.
- `SPC o`: open a page: `v` the Verbatim log here, `V` everywhere, `s`
  settings, `S` global settings, `r` recordings.
- `SPC w`: panes, as `C-w`.
- `SPC h`: help. `SPC h b` (or `SPC ?`) lists every binding that applies here;
  `?` shows the cheatsheet. `SPC h k` then a key sequence says what that key
  runs here and what it does elsewhere, without running it (`C-g` cancels).
  `SPC h f` picks any command, available here or not, and shows its keys and,
  when it does not apply, what it needs.

In the pickers (`SPC SPC`, `SPC h f`, `SPC ,`, `M-x`) the words you type match in any
order. `C-n` / `C-p`, `C-j` / `C-k` or the arrows move, `RET` picks, `Esc` or
`C-g` closes.

## Anything else: hints

`f` puts a short label on everything clickable on screen (buttons, links,
fields, the controls inside an open sheet). Type a label to click it, or to
focus it if it is a field. `Esc` cancels. Hints reach whatever has no key of
its own.

## Going places

`-` goes up one level of the path, `gh` to everything, `gs` to the settings
for the current view, `gS` to the global settings, `gv` to the Verbatim log
for the current view, `gV` to the whole Verbatim log, `gr` refreshes, `gR`
reloads the page (drafts are kept), as the "dashboard has been updated"
strip's Reload does. On the
Verbatim log, `/` focuses its search box.

## Sheets

The menus that slide up ("Send how?", "Notify me", settings) take `j` / `k`
(or `C-n` / `C-p`) to move between their controls, `RET` to press one, `f` for
hints, and `q`, `Esc` or `C-g` to close. Other keys do nothing while a sheet
is open.

## Back out with Esc

Outside a text box, `Esc` (or `C-g`) undoes one thing at a time: visual mode,
then the selection, then the open item, then the settings page or the
Verbatim log, then the search.

## For developers

The engine is `web/app/src/lib/keys.svelte.js`: tokens, modes, panes,
sequences, which-key, hints, the palette, the command and binding tables, and
describe-key/describe-command. The commands and keys are in
`web/app/src/lib/keymap.js`. Every action is a named command, defined once:

    command('pin', 'Pin or unpin', run, { pane, when, need, palette, anywhere })

and keys are bound to its name:

    bind('p', 'pin')
    bind('SPC i p', 'pin')
    bind('j', 'next-item', { pane: 'list' })

`pane` limits a command (or one key) to `side`, `list`, `detail` or `dialog`
(a sheet); `when` hides it when it does not apply (from which-key, `?`,
`M-x` and `SPC SPC`), and `need` says what it needs, which `SPC h f` shows;
`palette: false` keeps it out of `M-x`; `anywhere` lets it work inside a
sheet. Dispatch, which-key, the pickers, the binding list and the describe
sheets all read these two tables, so a new page or action needs only a
`command()` to be reachable by name, and a `bind()` for a key. So a change that adds a page,
panel or action adds its command too; otherwise the new feature is mouse-only. Most commands
press the same button a click would: tag the button `data-cmd="name"` in its
component and call `click('name')`, so the key and the click cannot drift
apart. `group('SPC i', '+item')` names a prefix for which-key.
`lib/shortcuts.js` holds the cheatsheet and the key each `data-cmd` button's
tooltip names (`tip(label, cmd)`); update it with a key.
A disabled control says why in its tooltip: spread `off(title, [condition,
reason]...)` from `lib/why.js` on it instead of writing `disabled=` and
`title=` separately, so the reason cannot drift from the condition. Hover
styles go on `:hover:not(:disabled)`, so a disabled button never looks live.
`components/Keys.svelte` draws the mode line, which-key, the binding list, the
describe sheet, the palette and the hints.
