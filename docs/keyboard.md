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
  every row from there. `d`, `U` and `p` then act on the selection; `Esc`
  ends it and clears the selection.

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
  Pinned, Responses to you, New).
- `za` folds or unfolds the section the open item is in; `zc` / `zo` fold
  or unfold it, `zM` / `zR` every section. A folded section stays folded on
  this device, and `j` / `k` skip its items. Clicking a section's title does
  the same.
- `C-d` / `C-u`: scroll half a page.
- `J` / `K` (or `C-j` / `C-k`) move to the next or previous item from any
  pane.

## The detail pane

`j` / `k` scroll a line, `C-d` / `C-u` half a page, `C-f` / `C-b` a page,
`gg` / `G` to the top or bottom.

## The open item

- `a`: answer the question. `t`: take the recommendation or the default.
- `r`: message its agent (on a node page, the coordinator). `i`: the first
  message box on the page.
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

- `SPC SPC` or `SPC p p`: go to a project, run or agent, by name.
- `SPC ,` or `SPC b b`: switch to an item in the list.
- `SPC :`, `:` or `M-x`: run any command by name. Each row shows its keys, so
  the palette also teaches them.
- `SPC i`: the open item (`a` answer, `r` message, `d` read, `u` unread, `p`
  pin, `t` take, `o` its node, `n` "Notify me", `m` "Send how?").
- `SPC n`: the node page (`m` message the coordinator, `r` mark everything
  under it read, `s` its notify settings).
- `SPC s s` search, `SPC s c` clear the search; `/` searches too.
- `SPC t a` show all, `SPC t o` oldest first, `SPC t s` select mode.
- `SPC x a` select every item, `SPC x c` clear the selection.
- `SPC w`: panes, as `C-w`.
- `SPC h b`: every binding that applies here (`?` shows the cheatsheet).

In the pickers (`SPC SPC`, `SPC ,`, `M-x`) the words you type match in any
order. `C-n` / `C-p`, `C-j` / `C-k` or the arrows move, `RET` picks, `Esc` or
`C-g` closes.

## Anything else: hints

`f` puts a short label on everything clickable on screen (buttons, links,
fields, the controls inside an open sheet). Type a label to click it, or to
focus it if it is a field. `Esc` cancels. Hints reach whatever has no key of
its own.

## Going places

`-` goes up one level of the path, `gh` to everything, `gs` to the settings
for the current view, `gS` to the global settings, `gr` refreshes.

## Sheets

The menus that slide up ("Send how?", "Notify me", settings) take `j` / `k`
(or `C-n` / `C-p`) to move between their controls, `RET` to press one, `f` for
hints, and `q`, `Esc` or `C-g` to close. Other keys do nothing while a sheet
is open.

## Back out with Esc

Outside a text box, `Esc` (or `C-g`) undoes one thing at a time: visual mode,
then the selection, then the open item, then the settings page, then the
search.

## For developers

The engine is `web/app/src/lib/keys.svelte.js`: tokens, modes, panes,
sequences, which-key, hints and the palette. The bindings are in
`web/app/src/lib/keymap.js`; `lib/shortcuts.js` holds the cheatsheet and the
key each `data-cmd` button's tooltip names (`tip(label, cmd)`), so update it
with a key. A binding is

    bind('SPC i p', 'Pin or unpin', run, { pane, when, palette, anywhere })

where `pane` limits it to `side`, `list`, `detail` or `dialog` (a sheet),
`when` hides it when it does not apply (and from which-key, `?` and `M-x`),
`palette: false` keeps it out of `M-x`, and `anywhere` lets it work inside a
sheet. Most commands press the same button a click would: tag the button
`data-cmd="name"` in its component and call `click('name')`, so the key and
the click cannot drift apart. `group('SPC i', '+item')` names a prefix for
which-key. `components/Keys.svelte` draws the mode line, which-key, the
binding list, the palette and the hints.
