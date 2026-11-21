# Driving the dashboard from the keyboard

The dashboard can be used without a mouse, in the style of Doom Emacs's evil
mode: modal keys, a `SPC` leader with a which-key popup, `M-x`, and link hints
for anything else. Press `?` on the page for a cheatsheet, and `TAB` in it
(or `SPC h b`) for every binding that applies where you are. Every button's
tooltip names its key. Any key can be changed in your keys file (see "Your
own keys"). This page explains the model.

## Modes

- **Normal** (the default): keys are commands.
- **Insert**: a text box or a dropdown has focus, and keys type. `Esc`,
  `C-g` or `C-[` leaves it, keeping the text, and the box stays *selected*
  (a dashed outline): keys are commands again, you can move around, and `i`
  goes back into it with the cursor where it was. In a message box,
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
  left anywhere, `n` says "🎉 All caught up" and opens the Tasks page here,
  even an empty one (the toast's Back returns; pressing `n` again pulses
  the toast instead of adding another). Answering or sending on your last
  open item shows the same toast beside its own, with a Tasks button.

## The detail pane

`j` / `k` scroll a line, `C-d` / `C-u` half a page, `C-f` / `C-b` a page,
`gg` / `G` to the top or bottom.

## The open item

- `a`: answer the question. `t`: take the recommendation or the default.
- `r`: message its agent (on a node page, the coordinator). `i`: back into
  the selected box (the one `Esc` left), else the first message box on the
  page. `R`: reply to an entry of its conversation (with several, hints
  label the reply buttons): the box then replies to that entry, until you
  send or press the ✕ beside "Replying to" (`M-x reply-to-cancel`).
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
  works on it where it stands, `z` Defer, `Z` Defer for quota; `e` export it
  with its conversation as one .org file; `x` discard the draft in the box,
  with Undo). Undefer has no key: `M-x task-undefer`.
- Org export: `SPC n e` this node as one .org file; `M-x org-export` the
  whole desk, `org-export-inbox`, `org-export-zip` (also in Settings).
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
order, each as plain text, a regular expression or an initialism (`mrt` for
"Mark read toggle"). `C-n` / `C-p`, `C-j` / `C-k` or the arrows move, `RET`
picks, `Esc` or `C-g` closes.

`M-x` and `SPC SPC` order commands as Doom's `M-x` does (counsel with amx):
with nothing typed, the last 7 you ran come first, most recent first, then
the rest by how often you ran them, then shorter names, then A to Z; what you
type keeps that order with shorter matches first. `M-p` and `M-n` step back
and forward through what you typed in it before. `M-x` also works while you
type in a box; `Esc` brings you back to the box, and after a pick `i` does.
The order and the history are kept in this browser.

## Anything else: hints

`f` puts a short label on everything clickable on screen (buttons, links,
fields, the controls inside an open sheet). Type a label to click it, or to
focus it if it is a field. `Esc` cancels. Hints reach whatever has no key of
its own.

`yf` labels every event on screen instead: the list's rows, the entries of
the open conversation, and the open item. Typing a label copies that event as
its id and its text, such as `m-6599: yf should show hints…`, so a paste
carries its reference.

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

Outside a text box, `Esc` (or `C-g`) undoes one thing at a time: the selected
box, then visual mode, then the selection, then the open item, then the settings page or the
Verbatim log, then the search.

## Your own keys

Put a keys file next to the desk's `config.toml`, normally
`~/.config/agent-desk/keys.edn`, on the machine the desk runs on. It holds
only your changes, and every device uses it. Saving it applies it at once,
without reloading the page. The format is the evilify skill's shared EDN
keymap format (`evilify/keymap-edn.md` in the
[evilify skill](https://github.com/NightMachinery/evilify-skill)):

```clojure
; ~/.config/agent-desk/keys.edn
{:normal {"g c"   nil             ; remove a built-in key
          "C-c t" :tasks          ; add one: a chord and a command
          "SPC q" "quit"}         ; label a prefix in the key guide
 :list   {"x" :fold}}             ; a key for one pane only
```

A chord maps to a command keyword, to `nil` (remove the built-in key with
that chord in that map), or to a string, which labels a prefix. An entry
replaces the built-in one with the same chord in the same map. `SPC h f`
finds a command's name; the built-in keys are in
`web/app/src/lib/keys.edn`.

**The maps**, from the most specific down:

- `:dialog`: while a sheet is open, only these keys work;
- a context map for what is open, holding the keys typed after the
  localleader: `:task`, `:deliverable`, `:screen`, `:message`, `:quota`,
  `:settings` and `:verbatim`;
- the focused pane's: `:list`, `:detail` or `:side`;
- `:normal`;
- `:insert`, inside a text box (then the only one). Its chords start with
  `C-` or `M-`, so they never type.

A key in a more specific map hides the same chord below it, and also a chord
it collides with as a prefix: a `"z"` command in `:detail` hides `:normal`'s
`"z ..."` keys while the detail pane has focus.

**Extensions** beyond the shared format's portable subset:

- `:localleader ["," "SPC m"]`: the chords that start the context maps
  (these are the defaults);
- `:keyless [:command ...]`: commands left without a key on purpose;
- a string value labels a prefix;
- an uppercase letter is `S-` with the letter (`"G"` is `"S-g"`);
- Purple's names are aliases for Doom's: `Up`, `PgUp`, `F1`, `Backspace`,
  `Delete` and so on, and `Ctrl+`, `Alt+`, `Shift+`, `Super+` for the
  modifiers.

**What your file may not do:** take or remove `ESC` or `C-g` (they always
back out), or `C-[` in a text box; bind a digit at the start of a chord
(digits start a count); bind Command (`s-`) chords, which the browser keeps;
or remove the last key for `:m-x`.

**When the file has an error** (EDN that does not parse, an unknown map or
command, two entries with the same chord, a chord that runs a command and
also starts a longer one, a value that is not a keyword, `nil` or a string),
none of it applies: the built-in keys stay in use, and a strip above the list
names the entry and its line. The file may be at most 64 KiB.

## For developers

The engine is `web/app/src/lib/keys.svelte.js`: tokens, modes, panes,
sequences, which-key, hints, the palette, the command table, and
describe-key/describe-command. The commands are in
`web/app/src/lib/keymap.js`, each defined once:

    command('pin', 'Pin or unpin', run, { pane, when, need, palette, anywhere })

Keys are bound to command names in `web/app/src/lib/keys.edn`, in the format
above, and the user's file overlays it. `lib/keyfile.js` reads and checks
both files and builds the effective map; it knows nothing of the page, so
`keyfile.test.js` runs it under node, and checks that `keys.edn` builds
against the commands in `keymap.js`.

`pane` limits a command to `side`, `list`, `detail` or `dialog` (a sheet);
`when` hides it when it does not apply (from which-key, `?`, `M-x` and
`SPC SPC`; its key then says what it needs), and `need` says what it needs,
which `SPC h f` shows; `palette: false` keeps it out of `M-x`; `anywhere`
lets it work inside a sheet (bind it in `:dialog` too). Dispatch, which-key,
the pickers, the binding list, tooltips, the cheatsheet and the describe
sheets all read the same effective map, so a new page or action needs only a
`command()` to be reachable by name, and an entry in `keys.edn` for a key. So
a change that adds a page, panel or action adds its command too; otherwise
the new feature is mouse-only. Most commands press the same button a click
would: tag the button `data-cmd="name"` in its component and call
`click('name')`, so the key and the click cannot drift apart.
`lib/shortcuts.js` holds the cheatsheet (rows name commands, not keys) and
`tip(label, button)`, the tooltip naming a button's key; a button whose
`data-cmd` differs from its command's name gets an entry in its
`buttonCmds`.
A disabled control says why in its tooltip: spread `off(title, [condition,
reason]...)` from `lib/why.js` on it instead of writing `disabled=` and
`title=` separately, so the reason cannot drift from the condition. Hover
styles go on `:hover:not(:disabled)`, so a disabled button never looks live.
`components/Keys.svelte` draws the mode line, which-key, the binding list, the
describe sheet, the palette and the hints.
