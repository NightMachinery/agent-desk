# Using agent-desk

All example values here are made up.

## Install

    go install github.com/NightMachinery/agent-desk@latest

or, from a clone, `go install .`. The binary lands in `$(go env GOBIN)`, or in
`~/go/bin` when that is unset; make sure that directory is on `PATH` for the
shells your agents run.

The store lives in `${AGENT_DESK_HOME:-~/.agents/desk}`: the database
`desk.db` and the rendered files under `render/`. It must be on a local
filesystem; the tool refuses a network one, because SQLite's WAL mode does not
work there. To combine machines, merge their stores (below).

## Words

- **Event**: one immutable row. Everything is an event; nothing is changed or
  deleted, and the database itself refuses `UPDATE` and `DELETE`.
- **Path**: where an event belongs, `project/run/role[/role...]`, such as
  `example-proj/rerun-nov/coord/eval-lead`.
- **Node**: a path prefix: a project, a run, or an agent in a run.
- **Slot**: a named box of current-state text at a node, such as `status`.
  Writing it replaces the text shown; the old text stays as an older event.
- **Display id**: the short id printed by every write, such as `q-7f3a`. The
  letter is the kind (`q` question, `a` answer, `t` notice, `d` done report,
  `s` slot text, `l` linked slot, `c` cleared slot, `j` join, `r` read mark).
  Commands accept the display id, its part after the dash, any unique prefix of
  that part, or a full id.

## Agent side

Agents learn this part from the skill, [skills/agent-desk](../skills/agent-desk/SKILL.md).

Only coordinators write. A coordinator first joins a node:

    agent-desk join rerun-nov/coord              # first coordinator of a run
    agent-desk join rerun-nov/coord/eval-lead    # nested, under its parent
    agent-desk join rerun-nov/coord --takeover   # replacing a coordinator

The project is the repository the working directory is in (`--project` to
override). A node already bound to another session refuses `join` unless
`--takeover` is given, and the session it was taken from can no longer write
there. A nested coordinator can only join under a node someone has joined.

A session that never joined and has no parent agent is joined automatically to
`<project>/adhoc/<slug>` on its first write. A session with a parent agent (a
Paseo agent's child, a tmux subagent) is refused: workers report to their
parent, which can give one a node if it should write.

Questions, with the context, options and recommendation on stdin:

    agent-desk ask --blocked --title 'Which split for the rerun?' <<'Q'
    The rerun needs a train/test split.
    A. Reuse the cached split: fast, but it predates the fix.
    B. Regenerate: an hour longer, consistent with the fix.
    I recommend B.
    Q

`--blocked` means the agent stops until answered. `--default 'A: reuse the
cache'` means it continues on that choice unless told otherwise.

**Several decisions at once.** `--choices FILE` adds multiple-choice
questions, as many as needed (up to 30), each with 2 to 12 options:

    {"questions": [
      {"text": "Which split?", "options": ["Reuse the cached split",
        {"key": "B", "label": "Regenerate", "detail": "an hour longer"}],
       "recommended": "B"},
      {"text": "Which seeds rerun?", "options": ["1-3", "4-5", "all"], "multi": true}]}

An option is a label or `{key, label, detail}`; keys default to A, B, C.
`multi` lets the user pick several. For each question the dashboard shows the
options and one text field: a note on the pick, or the user's own answer when
nothing is picked; clicking a pick again unpicks it. Below them, one message
box takes anything that is not about a single question, and sends it with
the picks. The answer arrives as text, one line per question (unanswered ones say so), then the general message if any:

    1. B: Regenerate. Note: keep the old cache
    2. Other: only seed 7
    3. (no answer)

    And rerun the sync tonight.

Mention the printed id (`q-7f3a`) when raising the question in chat. When the
user answers in chat, record it, or the question stays open in the inbox:

    agent-desk answer q-7f3a --from-chat 'B, keep the old cache'

Notices and reports:

    agent-desk tell --title 'Deploy finished' <<'R'
    The site is live; the old feed is gone.
    R

`--notify` asks for a notification.

A message from the user arrives in your session as

    Message m-4c2d from the user via agent-desk, about t-k2m9 "Deploy finished": check the old feed too
    (Respond on the desk: agent-desk tell --re m-4c2d)

The user may not be watching the chat, so act on it and respond on the desk,
even with one line:

    agent-desk tell --re m-4c2d <<'R'
    Done; the old feed is fine.
    R

The response is a notice titled "Re: Deploy finished" (`--title` overrides
it) that sits in the item's thread. `--quiet` marks a bare acknowledgement.
You may respond to a message sent to your node, or to an answer to one of
your questions; anything else is refused. A message with no response 15
minutes after it was delivered is flagged "no response yet", and
`show --mine` lists every message still awaiting your response.
`agent-desk whoami` prints the node this session writes to.

A substantive answer to something the user asked in chat is filed the same
way, with the question in one line instead of an id:

    agent-desk tell --re-chat 'Why is seed 2 slow?' <<'R'
    Seed 2 drew shard 17, which has duplicated rows.
    R

Responses of both kinds are listed in their own section, **Responses to
you**, between Needs you and New, and notify unless sent with `--quiet`.

When a session ends, its summary goes on stdin:

    agent-desk done --title 'Rerun finished' --report ~/reports/rerun.org <<'S'
    All five seeds done; the fix holds.
    S

This files a done report and sets the node's `status` to "Finished." (or
"Finished; see report."). A done report from a run's root coordinator marks
the run finished. `agent-done` does this for you where it is installed.
`agent-desk show --mine` shows only the calling session's own node.

**Attachments.** `ask`, `tell` and `done` take images and audio: a plot,
a screenshot, a voice note. Each `--attach` needs a `--caption` right after
it, saying what the file shows and why it matters, since the caption is what
the user reads first and what a terminal shows:

    agent-desk tell --title 'Seed 2 diverges' \
      --attach plots/loss.png --caption 'Seed 2 (orange) leaves the others at step 400' <<'R'
    Shard 17 has duplicated rows; seed 2 drew it.
    R

Up to 8 files per item; an image may be 10 MB and an audio file 50 MB. The
types accepted are png, jpg, gif, webp, svg, heic and avif images, and mp3,
m4a, aac, ogg, opus, wav, flac and weba audio. The desk copies each file into
`<store>/media/`, named by its content hash, so the agent may delete its own
copy. The dashboard shows the images and plays the audio; the inbox, `show`
and the ledger list each file with its caption and path.

The files are not part of the event history: the events hold only a
reference, and old files are deleted (see Attachments are temporary, below).

Slots hold what is true now, not history:

    agent-desk set status 'Seeds 3/5 done. ETA 14:00.'
    agent-desk set findings < findings.txt
    agent-desk clear next
    agent-desk link tasks TASKS.org

Every agent on a task of more than a few turns keeps three **standard** slots:
`status` (now), `next` (the next steps) and `done` (results so far, with where
they live). A coordinator also keeps `velocity`: its recent pace, where its
tokens went, and one to three suggestions to improve either. The board shows
these first, in that order, so every node reads the same.
On the dashboard a `velocity` slot has an **Ask for a velocity report**
button (`SPC n v`): it messages the agent with a prepared prompt asking for a
short executive report on its pace and whether the work is progressing well
(`reply --prompt velocity-report`; edit it by copying
`internal/prompts/velocity-report.md` into `~/.config/agent-desk/prompts/`).

Caps: `status` 2 lines, `next` 5, everything else 15. A `set` over its cap
fails, says by how much, and writes nothing. `tasks`, `user-verbatim` and
`state` only take `link`: the board shows the linked file's current content.

### Under an agent's sandbox

A sandboxed Codex session may write only its workspace, and SQLite in WAL mode
must write even to read, so every `agent-desk` command fails there; the error
says so. Either the agent reruns the command with escalated permissions, or
you allow the store once, in Codex's `config.toml`:

    [sandbox_workspace_write]
    writable_roots = ["/Users/you/.agents/desk"]

Antigravity's sandbox lets shell commands write only temporary directories
and has no setting to widen it, so there the agent requests a sandbox bypass
for each `agent-desk` command. A sandboxed agy subagent is a worker and does
not write anyway.

## User side

    agent-desk inbox                         # interactive dashboard (needs fzf)
    agent-desk inbox example-proj/rerun-nov  # scoped to one subtree
    agent-desk show                          # needs you, new, runs
    agent-desk show example-proj/rerun-nov   # one subtree
    agent-desk show --all --format md        # include read items and finished runs
    agent-desk show --format json
    agent-desk show --session 1f0c...       # what one session wrote (native or Paseo id)
    agent-desk answer q-7f3a 'B'             # record it and type it into the asking session
    agent-desk reply t-k2m9 'Check the old feed too'      # message the session behind an item
    agent-desk reply example-proj/rerun-nov/coord 'Pause after seed 4'   # or a node's coordinator
    agent-desk reply t-k2m9 --notify-remote always 'Tell me on the phone when done'
                                             # route the notification of the response
    agent-desk reply s-v4lc --prompt velocity-report   # ask for an executive report on velocity
    agent-desk answer q-7f3a --notify-desktop none --notify-remote always 'B'   # answers take them too
    agent-desk read t-k2m9                   # mark one item read, with the responses in its thread
    agent-desk read example-proj/rerun-nov   # mark everything under a node read
    agent-desk read q-7f3a                   # dismiss a question without answering it
    agent-desk read t-k2m9 s-p4d1            # several at once
    agent-desk read m-4c2d                   # clear a message's "no response yet" flag
    agent-desk unread t-k2m9                 # undo a read mark or a dismissal
    agent-desk pin t-k2m9 s-p4d1             # keep on top of every view; unpin to undo
    agent-desk search cache

### Answers reach the agent

`agent-desk answer` (and the inbox) records the answer, then types this into
the session that asked:

    Answer to q-7f3a "Which split for the rerun?": B

How it lands on a busy agent is its **behaviour**, chosen per message with
`--behavior` (on `answer` and `reply`, or in the dashboard):

- `steer` (the default, unless the `deliver.behavior` setting says
  otherwise; see Settings below): the agent reads it within its current
  turn, which carries on uninterrupted.
- `queue`: it waits until the turn ends. Only a Paseo that lists `queue`
  among `paseo send --behavior` choices can do this (the desk reads its
  help once per Paseo binary); elsewhere it is sent as steer, and the
  delivery records why.
- `interrupt`: the running turn is cancelled first. Use it only to stop
  work that is going wrong. Only Paseo agents can be interrupted; a tmux
  pane gets the message as a steer, and the delivery records why.

A Paseo agent gets it through `paseo send` with the behaviour named. An agent in a tmux pane gets it pasted into that pane, but only while the
pane still runs the same process and session. Anything else goes to
`deliver_command` if you configured one. A reply travels the same way, as
`Message m-4c2d from the user via agent-desk, about t-k2m9 "Deploy finished": ...`,
followed by the command that responds to it (see the agent side above).
When the session cannot be reached,
the answer stays recorded and undelivered, the command prints how to resume
the session, and `agent-desk deliver` tries again later. A question asked on
another machine is answered there after the next merge.

### The inbox

`agent-desk inbox` lists three sections, with a preview of the selected row:

- **NEEDS YOU**: open questions, blocked ones first, oldest first, grouped
  by project and run.
- **RESPONSES**: unread responses from agents to what you sent them, each
  row marked `re` and naming the message it responds to (`to m-4c2d`, or
  `to chat` for something you asked in its chat).
- **NEW**: unread notices, done reports and changed slots.
- **RUNS**: the tree of projects, runs and agents. Each node shows its state
  (`waiting on you`, `active 10m`, `quiet 4d`, `finished`) and how many open
  questions and unread items lie beneath it. Only the way to something that
  needs you is unfolded; everything else is one line, an agent's line carrying
  its status.

The preview of an item shows its conversation under it: your messages and
answers, what happened to each (`joined the running turn`, `queued`,
`waiting for a merge to host-b`), whether it got a response, and the
responses. A response's preview shows the conversation it belongs to, with
itself marked `▸`; a node's preview shows the messages sent to the node
itself.

Typing filters the list, as in any fzf. Keys:

- `enter`: answer a question (type a line, or leave it empty to write the
  answer in `$EDITOR`), open an item in `$EDITOR` (its linked file or report
  when it has one), or fold and unfold a node.
- `ctrl-r`: mark read; on a node, everything under it; on a question, dismiss it.
- `ctrl-t`: answer a question with "Take your recommendation." or, for a
  question with a default, "Go ahead with your default: ...".
- `ctrl-o`: open the session that wrote the item: in Paseo for a Paseo agent,
  by selecting its tmux pane, or else by copying its resume command to the
  clipboard.
- `alt-m`: message the session behind the row, as `agent-desk reply` does:
  about the item on an item's row, to the node on a node's row. Type a line,
  or leave it empty to write it in `$EDITOR`. It goes with your
  `deliver.behavior` setting.
- `alt-a`: show all, including read items and finished runs.
- `ctrl-l`: refresh. `esc`: quit.

What counts as unread: notices, done reports, and the current text of each
slot, until marked read. A slot that changes is unread again. `status` slots
are never unread; they are shown while their node is live.

A **dismissed** question is an open question you marked read by its id. It
leaves "Needs you" but stays open: its agent is still blocked (or goes ahead
with its default), you can still answer it from `--all` or "Show all", and
`unread` puts it back. Marking a node read never dismisses questions.

A run that is finished (its root coordinator filed a done report), has no open
question and nothing unread is hidden unless `--all` is given. Every view
accepts `--host` to show one machine's events.

After every write the tool re-renders plain org files, so `cat` is enough:

    ~/.agents/desk/render/inbox.org
    ~/.agents/desk/render/board/<project>.org
    ~/.agents/desk/render/ledger/<yyyy-mm>.org

Each item is a heading with a `:DESK_ID:` property and a keyword: `ASK` or
`ANSWERED` for questions, `NEW` for unread items. In the ledger an item's
conversation hangs under it as child headings: `MSG` for a message or an
answer you sent, tagged with its delivery state (`:pending:`, `:delivered:`,
`:in_chat:` for an answer given in the agent's chat, plus `:no_response:`
once a delivered message has waited 15 minutes), and `RE` for an agent's
response. A message with no item to hang under (one sent to a node, or
about a slot) is a top-level `MSG` heading with its responses under it. The
files are rendered at each write, so a `:no_response:` tag appears at the
first write after the 15 minutes. `agent-desk render` re-renders by hand.

### In Emacs

[emacs/agent-desk.el](../emacs/agent-desk.el) acts on the item at point in
these files. Load it (`(load "/path/to/agent-desk/emacs/agent-desk.el")`) and
every file under the render directory opens read-only, with
`agent-desk-org-mode` on: it reverts the file when the desk changes and
binds, in evil's normal state, the dashboard's keys:

- `a` answer the question (`t` takes its default or recommendation, after
  asking you to confirm);
- `r` message the item's agent, or on a node heading the node's
  coordinator;
- `d` mark read (on a question, dismiss it), `U` mark unread, `p` pin or
  unpin (an item under Pinned is unpinned; `C-u p` always unpins);
- `o` open it as a notification click would (`open_command`);
- `gr` render the desk again.

Without evil the same commands are under `C-c C-d` (`C-c C-d a`, and so on),
plus `C-c C-d y` to copy the id and `C-c C-d i` for the inbox. An answer or a
message is written in a compose buffer below the item: `C-c C-c` or `ZZ`
sends, `C-c C-k` or `ZQ` cancels, and a prefix argument (`C-u a`, or
`C-u C-c C-c`) asks how to deliver it (steer, queue or interrupt). A
multiple-choice question opens with one line per question, `N. KEY`, filled
with the recommendation where there is one: write the key or keys (`2. A, C`),
words after them as a note (`1. B keep the seed`), or your own words instead
(`1. neither, ask me later`). Lines without a number go along as a general
message. The tests are in `emacs/agent-desk-test.el`; `go test ./...` runs
them when Emacs is installed.

### The web dashboard

    agent-desk serve                          # http://127.0.0.1:7740/
    agent-desk serve --listen unix:/path/to/desk.sock   # behind a proxy
    agent-desk serve --config config.json     # listen, pid_file, allow_hosts, origin, title, device, color from a supervisor's JSON

The same sections as the inbox, on a desktop in three panes and on a phone
in one column: Needs you, Pinned (when anything is), Responses to you (the
agents' answers to what you asked), New, and the run tree. Click a section's
title to fold it. Each row names its project, run and agent below what the
view or its group heading already says. Opening an item marks it read (a
question excepted: reading one dismisses it), but the row stays where it
is, dimmed once you open another, for a minute (Settings: "Keep a read item
in the list for", per device). The icon beside its id marks it unread
again, and the pin icon keeps it on top.
Previous and Next at the bottom of an item walk the list in the order
shown. The search filters every section, and the run tree by project, run
or agent name and by the session or Paseo id of whoever joined a node.
Clicking a part of an item's path shows that subtree. Every view is a URL you can bookmark: a project, run or
agent (`#/p/example-proj/rerun-nov`), one session's writing (`?session=`),
one machine (`?host=`), read items too (`?all=1`), a search (`?q=`), oldest
first (`?sort=old`; recent first is the default) and an open item
(`?item=q-7f3a`). From it you answer questions (with one-click "take your
recommendation" or "go with the default"), message the agent behind any item
or any coordinator, dismiss a question without answering it, and mark things
read.

An item shows its conversation under it: your messages and answers, each
with what happened to it ("joined the running turn", "queued, position 2",
"waiting for a merge to host-b", a fallback note when the behaviour you asked
for was unavailable), then "responded" or, 15 minutes after delivery, "no
response yet"; the agents' responses follow in order. A node shows its own
conversation the same way. A response in the list reads "Response to
m-4c2d", and its page shows the conversation it belongs to. The Send button
uses your `deliver.behavior` setting; the menu beside it offers the others,
greys out one the session cannot do and says why, and asks before "Stop and
send" (interrupt). "Notify me" beside it changes, for this message only,
where its response notifies you. When the session has finished (a done
report, or an archived Paseo agent) the composer says that sending revives
it. The Notify chip on a node opens its settings, and Settings in the sidebar
opens the global ones: one control per setting with where its value comes
from ("inherited from example-proj"), for all kinds of event or one, and
"Reset to inherited" where an override stands. To act on many items at once, press Select (or long-press a row on a
phone, or cmd-click one on a desktop), then tap rows, shift-click a range or
use "Select all" per section or group, and mark them read or unread. It
updates live. Bodies, slots, messages and linked `.md` files render as
Markdown; raw HTML shows as text, and images are not loaded. A single line
break inside a sentence flows as a soft wrap, so hard-wrapped text reads
well on a phone; any other line break is kept. Served over HTTPS (or on localhost), it can be installed as an
app: "Install" or "Add to Home screen" in the browser's menu.

The dashboard works without a mouse, in the style of Doom Emacs's evil
mode: `j`/`k` through the list, a `SPC` leader with a which-key popup,
`M-x`, `f` to click anything by its label, and ⌘/Ctrl+Enter to send. Press `?`
on the page for every key, or read [keyboard.md](keyboard.md).

It serves this machine's store only. To reach it from a phone, put it behind
something that authenticates you, such as a reverse proxy on a private
network; the server refuses cross-site writes and, on a TCP port, any `Host`
it was not told about (`--allow-host desk.example.net`). A proxy that rewrites
`Host` needs the page's public origin (`--allow-origin https://desk.example.net`,
or `origin` in `--config`); on a unix socket the proxy's `X-Forwarded-Host`
also counts. See
[dashboard.md](dashboard.md) to change the page.

### In Paseo

[paseo-plugin/](../paseo-plugin/OVERVIEW.md) is a Paseo plugin that shows the
desk inside the Paseo app: an **Agent desk** row in the sidebar footer (with
the number of open questions) opens a screen where you answer, dismiss and
message; an agent with open questions gets a **Desk** pill on its composer,
opening a panel with only its items; and the Command Center has both. Its
daemon side runs `agent-desk` on the daemon's machine (`$AGENT_DESK_BIN`,
else `~/go/bin/agent-desk`, else `PATH`), so answers are delivered exactly
as from the dashboard.

Paseo runs plugins only when they are enabled for the daemon (Settings →
Plugins, or `pluginsEnabled` in its `config.json`); a plugin is trusted,
unsandboxed code. Then:

    cd paseo-plugin && npm install && npm run typecheck
    paseo plugin install "$PWD"          # once; the plugin's id is agent-desk
    paseo plugin reload agent-desk       # after editing its source

To turn it off and on again, without removing it:

    paseo plugin disable agent-desk
    paseo plugin enable agent-desk

`paseo plugin remove agent-desk` uninstalls it and keeps this directory.

## Notifications and other machine-specific commands

The core knows nothing about your desktop. It runs commands you name in
`~/.config/agent-desk/config.toml` (or `$AGENT_DESK_CONFIG`), each under
`sh -c` with the event as JSON on stdin:

    notify_command  = "my-notifier"        # raise a notification; print its handle
    dismiss_command = "my-notifier --remove"   # take one back
    open_command    = "my-opener"          # what `agent-desk open <id>` does
    base_url        = "https://desk.example.net/"   # the web dashboard, for links
    away_command    = "my-away-check"      # exits 0 while you are away from this machine
    remote_label    = "Phone"              # what the dashboard calls the remote channel
    media_keep_days = 30                   # see Attachments are temporary; 0 keeps them
    [ssh_hosts]                            # desk host name = ssh destination
    otherhost = "otherhost.example.net"

**Channels.** A notification goes out on up to two channels, chosen by the
settings below (see Settings):

- `desktop`: a card on a machine. `notify.desktop` says which machines:
  `origin` (where the event was written), `all`, or host names.
- `remote`: something that reaches you away from the desk, such as a chat
  message to your phone. `notify.remote` says when: `off`, `away` (only
  while `away_command` exits 0) or `always`. Only the machine the event was
  written on raises it.

`notify_command` runs once per channel. Whatever it prints on its first line
is the notification's **handle** (a card group, a message id), which the
desk records as a `notified` event.

**Taking back.** When the item stops needing you (you read it, answer or
dismiss it, or a slot gets newer text), each of its notifications is taken
back: `dismiss_command` runs with the handle, and the desk records a
`retracted` event. A desktop notification is taken back by the machine that
raised it; a remote one by whichever machine sees the cause first. Since
handles are events, this works across a merge, and two machines may both
take back the same remote notification before they merge, so the command
must not mind being run twice. Items notified before this version have no
handle; for them `dismiss_command` runs once, without one.

**The JSON** carries `action` (`notify`, `dismiss`, `open` or `deliver`),
`channel`, `dest` (`notify.remote.dest`, for the remote channel), `handle`
(for dismiss), `display_id`, `path`, `project`, `class`, `agent_title` (the
Paseo agent's title, else the agent and a short session id), `url` (the
item's dashboard page, with `base_url`), `event` (the full event), `cause`
(for a dismissal, the answer, read or newer slot), `local` (written on this
machine), and `agent_desk`, this binary's path, for building a click action
such as `agent-desk open q-7f3a`.

The commands run in a detached `agent-desk notify-run`, so a slow notifier
never holds up an agent's write; output goes to `~/.agents/desk/hooks.log`.
With no config, nothing is run.

`agent-desk open <id|path>` hands the item to `open_command`; without one it
opens `url` in the browser when `base_url` is set, and otherwise opens the
inbox scoped to the item's node.

## Settings

A **setting** is a named value that the desk looks up for each event. You
set it for a **scope**: everywhere, or one project, run or node path. A
notification setting can also be set for one **class** of event:
`ask-blocked`, `ask-default`, `response`, `tell-notify`, `tell` or `done`.

    agent-desk pref set deliver.behavior queue --scope example-proj
    agent-desk pref set notify.remote always --class ask-blocked
    agent-desk pref unset notify.remote --class ask-blocked
    agent-desk pref show example-proj/rerun-nov/coord   # each value, and where it comes from

The settings:

- `deliver.behavior`: `steer` or `queue`, the behaviour of a message sent
  without `--behavior`. `interrupt` is refused here: it cancels the agent's
  work in progress, so it is only ever chosen for one message.
- `notify.desktop`: `none`, `origin` (the machine the event was written
  on), `all`, or host names separated by commas.
- `notify.remote`: `off`, `away` or `always`, for a channel that reaches you
  away from your desk, such as your phone.
- `notify.remote.dest`: free text handed to that channel.

For one event, the first match wins:

1. what the message itself asks for (`--behavior`);
2. a `pref` at the event's node, then at each parent up to the global
   scope. At one scope a setting for the event's class beats one for any
   class, and a nearer scope beats a farther one whatever the class;
3. the config file's defaults, a class's table before the general one;
4. the built-in values: steer; desktop notifications on the origin machine
   for everything but a plain `tell`; remote notifications only for blocked
   questions and responses, and only while you are away.

Overrides are `pref` events in the store, so they merge between machines and
keep their history. Only you write them: `pref set` from an agent session is
refused. Defaults go in `config.toml`:

    [defaults]
    "notify.remote.dest" = "example-chat"
    [defaults.class.ask-blocked]
    "notify.remote" = "always"

## Attachments are temporary

Attachments are copies kept for reading, not an archive. Once a day, at the
first write, the desk deletes the file of every item that is

- older than `media_keep_days` (default 30),
- read, or for a question answered or dismissed, and
- not pinned.

A file several items share stays while any of them keeps it, and a file no
item names is deleted after a day (an `ask` that failed after copying it).
The item itself stays, and says its attachment was deleted. Pin an item to
keep its files; `media_keep_days = 0` keeps every file. To run it now:

    agent-desk gc --dry-run         # list what would go
    agent-desk gc [--days N]        # delete

Links to attachments in the rendered ledger point at the stored file, so
they stop working once it is deleted.

## Tasks, and work deferred for quota

A task is a work item at a node (display id `w-...`), in one of the states
open, started, deferred, done or dropped. Agents keep their checklist with
`task add`, `task start`, `task done`; the node panel shows "Tasks: 3 of 7
done" and `show` lists the ones still active.

A deferred task waits for quota to spare. It carries a brief (stdin), and
optionally `--size S|M|L`, `--priority 1|2|3` (1 first), `--profile NAME`
(passed to the launcher) and `--cwd DIR` (default: where it was filed). The
dashboard's "Deferred for quota" section lists them; pick some and Launch,
or Launch top N (N is `max_launch`). From a terminal:

    agent-desk task list --deferred
    agent-desk task launch w-3k9d w-8f2a
    agent-desk task launch --deferred [<prefix>] [--max N] [--dry-run]
    agent-desk task prompt w-3k9d     # the prompt a launched agent gets

Launching runs `launch_command` under sh with the task as JSON on stdin
(`id`, `display_id`, `title`, `brief`, `path`, `project`, `run`, `role`,
`cwd`, `size`, `profile`, `priority`, `prompt`); the last line it prints is
recorded as the agent started, and the task becomes started. `prompt` is
`prompts/start-task.md`, a Go text/template; a file of that name in
`~/.config/agent-desk/prompts/` replaces it.

**The nudge.** Quota left in a window when it resets is lost. `agent-desk
task nudge`, run from cron every few hours, runs `quota_command`, which
prints `{"gauges": {"<name>": {"pct": N, "reset": <unix seconds>}}}`. When a
gauge matching `nudge_gauges` (default the weekly ones, `\.7d$`) resets within
`nudge_hours` (24) at most `nudge_below` (60) percent used, and deferred
tasks are waiting, it files one notice per window and reset at
`_desk/quota/nudge`, listing them.

    [tasks]
    launch_command = 'my-launcher'    # starts an agent; prints its id last
    max_launch = 3
    quota_command = 'my-quota-json'
    nudge_gauges = '\.7d$'
    nudge_below = 60
    nudge_hours = 24

`task add` reads stdin only with `--defer` or `--brief -`, so it never waits
on an agent shell's held-open stdin.

## Notes for agents only

`agent-desk set --for agents <slot>` writes a slot only agents read: run
state an agent keeps for itself, up to 400 lines and 64 KiB. It is never
unread, never notifies, and stays out of the New list; `show` names it on its
node ("Agent notes: state (120 lines, 2h)") and the dashboard's node panel
lists it folded. `agent-desk get <slot> [--node PATH]` prints any slot's
current text, the calling session's node by default.

## The user's messages, kept word for word

A prompt hook copies every message the user sends to a session on the desk,
verbatim, as an `instruction` event (display id `u-...`) at the session's
node. A run then keeps its owner's instructions through compaction without a
hand-kept file. Agents read them back:

    agent-desk instructions                    # this session's run
    agent-desk instructions tdesktop/rerun-nov [--since 2d] [--format json]

They never appear in the inbox, the dashboard lists or notifications.

What is recorded: only sessions that have joined a node, only coordinators
(a tmux worker's prompts are typed by its parent), and only prompts the user
typed. Skipped: messages from other agents (Paseo's `<paseo-system>`
envelope), Paseo schedules and heartbeats, `Continue.`, a bare slash command
such as `/compact`, harness notices, and the answers and replies the desk
delivers (already on the desk). A slash command with arguments is kept.

Install it as a `UserPromptSubmit` hook. Claude Code (`settings.json`):

    {"hooks": {"UserPromptSubmit": [{"hooks": [{"type": "command",
      "command": "agent-desk hook prompt --agent claude", "timeout": 10}]}]}}

Codex (`hooks.json`, same shape) uses `--agent codex`. The hook prints
nothing and always exits 0; its errors go to `hook-errors.log` in the store
directory. In `config.toml`:

    [instructions]
    record = false                  # turn it off
    skip = ['^PING ', '^Nightly:']  # more prompts that are not the user's

## Several machines

    agent-desk snapshot /tmp/copy.db   # consistent copy of the live store
    agent-desk merge /tmp/copy.db      # add the events it has that this store lacks
    agent-desk merge ssh:otherhost     # both ways over ssh; no database file is copied
    agent-desk export > events.jsonl   # every event as JSON lines
    agent-desk import < events.jsonl   # add the ones that are new

`merge ssh:` runs `agent-desk` on the other machine; set
`AGENT_DESK_REMOTE_COMMAND` if it is not on that machine's PATH for
non-interactive ssh (for example `~/go/bin/agent-desk`). Each side then
delivers the answers whose questions it asked.

Copy a store only with `snapshot`: copying the file of a live database can
give a broken copy.

A merged or imported event raises this machine's desktop notification when
its `notify.desktop` setting names this machine (`all`, or its host name)
and the event is less than a day old, so a first merge does not raise one
for every old item. `--notify` raises one for every imported event instead.
A notification raised here is taken back here when the merge brings in its
item's read or answer.

With `[ssh_hosts]` in the config, the desk pushes at once instead of
waiting for the next merge: an event whose desktop notification belongs on
another listed machine, and a message or answer for a session there, start
a `merge ssh:` to it in the background. An unreachable machine catches up at
its next merge. Merging is idempotent and order-free, since events never
change and ids are globally unique. Items from another machine show its name.

Attachments travel too, as best effort: after the events, `merge ssh:`
copies the files each side keeps and the other lacks (`agent-desk media
list|cat|put` on the other side), and `merge <file>` copies them from the
`media/` directory next to that file. A file that has not arrived shows as
"not fetched yet". `import` brings no files.
