# Keymap redesign (w-v7ms): proposal

@Opus5.5 [2026-11-21 Sat]. Status: approved with changes (q-nbpa, see
"Decisions"); the map below already includes them. Nothing is built yet.

The task: move every binding into an EDN file, let the user layer their own EDN
file on top, agree the EDN format with the agent that evilified Purple (the
Telegram Desktop fork) so the evilify skill can share it, and rewrite every
binding under one consistent mnemonic scheme. Two later requests join it: every
action gets a key (w-f8a5), and an action may be keyless on purpose, marked
`none` (m-f7pq).

## Summary

- **Keys move to EDN.** `web/app/src/lib/keys.edn` holds every built-in
  binding. Your file, `~/.config/agent-desk/keys.edn` on the desk machine,
  overlays it. The dashboard re-reads your file live when it changes.
- **Format.** It is Purple's format, generalized: a map from keymap name to
  a map from chord to command. The draft of the shared spec is with Purple's
  agent for review (see "The EDN format").
- **Library.** `edn-data` (npm, MIT, no dependencies, 35 KB, updated this
  week) parses both files in the browser.
- **Scheme.** Seven rules, below. I re-derived every binding from them. Most of
  today's single keys and vim prefixes survive because they already follow
  vim or Doom. What moves is the leader tree: item commands go to `SPC b`,
  message-box commands to `SPC i`, node commands to `SPC d`, and commands
  specific to the open item or page go to a localleader, `,`.
- **Coverage.** A test fails when any command has no key and is not marked
  keyless, or when any button has neither a command nor a `none` mark.
- **Decisions** are at the end: the leader groups, the localleader key, and
  where your keys file lives.

## The rules

1. **Every command has one home in a group**: a leader group (`SPC b`,
   `SPC o`, ...), a vim prefix (`g`, `y`, `z`, `C-w`) or the localleader.
   The key guide for those groups is therefore a complete map. Frequent
   commands also get a short key (one keystroke) on top.
2. **Leader groups mean what they mean in Doom**:
   - `SPC b` buffer: the open item;
   - `SPC i` insert: the message box;
   - `SPC o` open: pages;
   - `SPC d` nodes (the one group off Doom's letters: `p` is a reach,
     `d` sits on the home row);
   - `SPC q` session;
   - `SPC s` search;
   - `SPC t` toggle: view switches;
   - `SPC w` window: panes;
   - `SPC h` help.

   `SPC n` means "new", as in Purple. `SPC x` holds selection commands,
   because `x` selects.
3. **A leader entry reuses the letter of its short key.** `p` pins, so
   `SPC b p` pins; `d` reads, so `SPC b d` reads. Learn a letter once.
4. **The localleader holds what only the open thing has.** That is `,` (also
   a localleader in your Emacs now) and `SPC m` (Doom's default), for a task, a deliverable,
   a screen request, a failed delivery, an out-of-quota agent, or the Settings
   and Verbatim pages. The same letter means the same idea across kinds:
   `\ o` opens (a deliverable's link, a task again), `\ r` retries or
   releases, `\ y` and `\ n` say yes and no.
5. **Vim prefixes keep their vim meaning.** `g` goes somewhere, `y` copies
   (everything copyable lives under `y`), `z` folds, `C-w` moves between panes.
6. **Shift means the reverse direction or the wider scope**, as `n`/`N`,
   `g v`/`g V` (here, everywhere) and `\ z`/`\ Z` (defer, defer for quota)
   do.
7. **Short keys never do something you cannot undo.** Archiving an agent,
   dropping a task and interrupting an agent live only under the leader or
   localleader, behind their usual confirmation.
8. **Among equally good letters, the easier reach wins**: the home row, then
   the bottom row, then the top. The Doom letter or the short key's letter
   still comes first where it carries a habit.

Deliberately keyless commands are listed under `:keyless` in `keys.edn`, and
buttons reached only by hints (`f`) or Tab carry `data-keys="none"`: per-row
buttons such as a recording's Insert or a device's Test, and a dialog's own
Cancel and Save. `SPC h f` shows such a command as "no key (on purpose)".

## The full map

Every command, with its key under this proposal. Anything not marked keeps
its key from today. "New key" means the command had none; "new command"
means a button that had no command gets one.

### Single keys (normal mode)

Unchanged:

- Moving: `j` `k`, `J` `K` (`C-j` `C-k`), `n` `N`, `g g` `G`, `}` `{`,
  `C-d` `C-u`, `C-f` `C-b`, `h` `l`, `RET` `o`, `-`.
- The open item: `a` answer, `t` take, `r` message its agent, `R` reply to
  an entry, `i` write in the message box, `d` read, `U` unread, `p` pin,
  `x` select, `v` `V` visual, `q` close.
- Help and pickers: `?` cheatsheet, `:` and `M-x` commands, `f` hints,
  `/` search, `ESC` and `C-g` back out.

New:

- `c` new task (capture): the most frequent thing you create.

### `g`: go

Unchanged: `g h` top, `g s` / `g S` settings here / global, `g v` / `g V`
verbatim log here / everywhere, `g c` / `g C` tasks here / everywhere, `g r`
refresh, `g R` reload the page, `g o` the item's node, `g w` what its session
wrote, `g t` the item a conversation is about.

New: `g a` recordings (audio).

### `y`: copy

Unchanged: `y y` link, `y i` id, `y r` resume command, `y a` Paseo id,
`y p` path.

New keys:

- `y q` copies the picked out-of-quota agent ids;
- `y l` copies this device's push logs;
- `y b` copies the address of Brave's push setting.

New command: `y f` labels every event on screen with a hint, and copies the
chosen one's text with its id in front (w-wnm8).

### `z` and `C-w`

Unchanged: `z a` `z c` `z o` `z M` `z R`; `C-w h` `C-w l` `C-w w`.

### Leader `SPC`

Top level, unchanged: `SPC SPC` command or place (your Doom's `SPC SPC`
is M-x; this finds commands too, so the habit works), `SPC :` M-x, `SPC ,`
switch item, `SPC ?` every binding, `SPC j` hints.

`SPC b`: the open item. It used to be `SPC i`.

- `b a` answer, `b t` take, `b r` message, `b R` reply to an entry;
- `b d` read, `b u` unread, `b p` pin, `b x` select;
- `b n` / `b N` next or previous open item, `b b` switch item (unchanged);
- `b o` its node (was `SPC i o`), `b w` what its session wrote.

`SPC i`: the message box. It used to be "item".

- `i i` write;
- `i f` attach a file (new key);
- `i v` start or stop voice recording (new command);
- `i r` recordings, to insert a transcript;
- `i n` notify me (unchanged), `i s` send how (was `SPC i m`);
- `i x` discard the draft (unchanged);
- `i c` stop replying to an entry (new key).

`SPC n`: new. It used to be "node", and its three keys keep their meaning.

- `n t` task, `n m` message to the coordinator, `n v` velocity report.

`SPC d`: the current node.

- `d f` find and go to a node (was `SPC p p`), `d u` up (was `SPC p u`),
  `d h` top (was `SPC p h`).
- `d d` mark everything under it read (was `SPC n r`).
- `d s` its notify settings (was `SPC n s`).
- `d a` archive its coordinator agent (was `SPC n a`).

`SPC o`: pages.

- Unchanged: `o s` / `o S` settings, `o v` / `o V` verbatim log, `o t` /
  `o T` tasks, `o r` recordings, `o o` section order.
- New key: `o f` edit focus sets.

`SPC q`: session.

- `q r` reload the page (also `g R`);
- `q s` take the screen back from an agent holding the lock (new key).

`SPC s`: unchanged, `s s` search and `s c` clear it.

`SPC t`: toggles.

- Unchanged: `t a` show all, `t o` oldest first, `t d` deliverables (cycles
  unread latest, latest, all), `t t` tasks section, `t s` select mode.
- New key: `t c` auto-continue.

`SPC w`: unchanged, `w h` `w l` `w w` `w s` `w d`.

`SPC x`: selection.

- Unchanged: `x a` select all, `x c` clear.
- New key: `x d` mark every unread item in the list read.

`SPC h`: unchanged, `h k` describe a key, `h f` describe a command, `h b`
every binding.

### Localleader `,` (also `SPC m`)

What it offers depends on what is open.

A task:

- `\ s` ask where it stands (was `SPC i s`);
- `\ o` reopen it (new key);
- `\ d` done, `\ D` drop (new commands);
- `\ z` defer, `\ Z` defer for quota (new commands, w-pcr8);
- `\ l` start an agent on it (new command).

A deliverable:

- `\ o` open its link (was `SPC i g`);
- `\ g` download its file (was `SPC i G`);
- `\ w` send "This worked." (was `SPC i w`).

A screen request: `\ y` grant, `\ n` deny (new keys).

A message:

- `\ r` retry a failed delivery (new key);
- `\ t` retry a failed transcription, `\ s` send it without the recording
  (new commands).

An out-of-quota agent: `\ a` archive the first, `\ r` replace the first
(new keys), `\ y` copy the picked ids (same as `y q`).

The Settings page:

- `\ o` section order (same as `SPC o o`), `\ O` back to the default order
  (new key);
- `\ r` render the org files now, `\ e` export them (new keys);
- `\ p` turn push on or off, `\ t` send a test push (new commands);
- `\ l` copy push logs, `\ b` copy Brave's push setting (same as `y l`,
  `y b`).

The Verbatim log: `\ m` show older messages (new key).

### Insert mode (inside a text box)

- `M-x` commands, which w-2077 asks for;
- `⌘ RET` send;
- `ESC` and `C-g` leave the box.

Other keys type text as usual.

### Keyless on purpose

- `deliverables-unread`, `deliverables-latest` and `deliverables-all`:
  `SPC t d` cycles through them.
- Per-row buttons, reached by hints: a recording's Insert, Retry and Delete,
  a device's Test and Remove, a verbatim message's Copy, a sidebar entry's
  message and read buttons, a section's Select-all.
- A dialog's own buttons (Cancel, Save, Send anyway): Tab or `j`/`k`, then
  `RET`.

### What you would relearn

- Item commands under the leader: `SPC i <letter>` becomes `SPC b <letter>`.
  The short keys (`a`, `d`, `p`, ...) do not change.
- Send how: `SPC i m` becomes `SPC i s`.
- Deliverable and task commands go to the localleader: `SPC i g`, `SPC i G`,
  `SPC i w` and `SPC i s` become `\ o`, `\ g`, `\ w` and `\ s`.
- Node commands: `SPC p p`, `SPC p u` and `SPC p h` become `SPC d f`,
  `SPC d u` and `SPC d h`; `SPC n r`, `SPC n s` and `SPC n a` become
  `SPC d d`, `SPC d s` and `SPC d a`.

## The EDN format

The built-in file, abridged:

```clojure
; Built-in agent-desk keymap. ~/.config/agent-desk/keys.edn overlays it.
{:normal  {"SPC b"   "+item"
           "SPC b p" :pin
           "p"       :pin
           "c"       :task-add
           "g a"     :recordings}
 :list    {"j" :next-item, "k" :prev-item}
 :detail  {"j" :scroll-down, "k" :scroll-up}
 :task    {"d" :task-done, "D" :task-drop}   ; after the localleader
 :insert  {"M-x" :m-x}
 :localleader ["," "SPC m"]
 :keyless [:deliverables-unread :deliverables-latest :deliverables-all]}
```

The context keymaps (`:task`, `:deliverable`, ...) hold the keys typed after
the localleader, and `:localleader` lists the keys that start it, so changing
the localleader is one edit.

Your file holds only changes:

```clojure
{:normal {"g c" nil              ; drop a default
          "C-c t" :task-add}     ; add your own
 :task   {"f" :task-defer}}       ; , f (and SPC m f) defer a task
```

The format is Purple's:

- keymap names map to chord maps;
- a chord maps to a command keyword, or to `nil` to remove a default;
- chords use Doom notation (`SPC`, `C-`, `M-`, `RET`, ...).

It adds three things. A string labels a prefix in the key guide. A list of
deliberately keyless commands goes under `:keyless`. `:localleader` names the
keys that start the context keymaps. A file with any error
(an unknown command, a duplicate chord, a chord that is also a prefix of
another) is rejected whole. The dashboard then keeps the defaults and shows a
strip naming the bad entry.

agent-desk's keymaps, most specific first:

- `:dialog`;
- a context keymap for the open item's kind or page: `:task`,
  `:deliverable`, `:screen`, `:message`, `:quota`, `:settings`,
  `:verbatim`;
- the focused pane's: `:list`, `:detail` or `:side`;
- `:normal`;
- `:insert`, inside text boxes only.

The shared draft is at
`~/.agents/runs/agent-desk/build/keymap-edn-draft.md`. It went to Purple's
coordinator agent, which built Purple's EDN reader. Once we agree, it goes
into the evilify skill as `evilify/keymap-edn.md`.

## How it is built

- **Commands stay in code** (`keymap.js`): the evilify rule is that a keymap
  file remaps commands but never defines actions.
- **One source for keys.** Tooltips, the cheatsheet and the key guide read
  the effective keymap, so `cmdKeys` in `shortcuts.js` and its hand-kept key
  strings go away and cannot drift.
- **Your file.** The server reads `~/.config/agent-desk/keys.edn` and serves
  it as text (`GET /api/keys`). The event stream announces a change, so a
  saved edit applies without a reload. Parsing and checking happen in the
  browser, where the command list lives.
- **Coverage test.** It runs with the other web checks and fails on:
  - a command with no key that is not under `:keyless`;
  - a button without `data-cmd` or `data-keys="none"`;
  - a `data-cmd` that names no command.
- **Order of work.** First the loader, with today's bindings converted to EDN
  as they are. Then the remap in one commit, so the change in keys is a
  single diff of `keys.edn`. Then the new commands, the coverage test and
  the docs.

## Decisions

Decided in q-nbpa:

1. **Leader groups: Doom-aligned** (A), with one change asked for: letters
   that are easier to reach win where the mnemonic allows (rule 8), so the
   node group is `SPC d`, not `SPC p`.
2. **Localleader: `,` and `SPC m`** (B). `,` is also a second localleader in
   the user's Doom Emacs now (doom.d `night-doom-keybindings.el`), where it
   replaces evil-snipe's reverse repeat.
3. **Your keys file: one file on the desk machine** (A),
   `~/.config/agent-desk/keys.edn`, shared by every device.
