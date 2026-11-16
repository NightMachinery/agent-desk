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

Mention the printed id (`q-7f3a`) when raising the question in chat. When the
user answers in chat, record it, or the question stays open in the inbox:

    agent-desk answer q-7f3a --from-chat 'B, keep the old cache'

Notices and reports:

    agent-desk tell --title 'Deploy finished' <<'R'
    The site is live; the old feed is gone.
    R

`--notify` asks for a notification.

When a session ends, its summary goes on stdin:

    agent-desk done --title 'Rerun finished' --report ~/reports/rerun.org <<'S'
    All five seeds done; the fix holds.
    S

This files a done report and sets the node's `status` to "Finished." (or
"Finished; see report."). A done report from a run's root coordinator marks
the run finished. `agent-done` does this for you where it is installed.
`agent-desk show --mine` shows only the calling session's own node.

Slots hold what is true now, not history:

    agent-desk set status 'Seeds 3/5 done. ETA 14:00.'
    agent-desk set findings < findings.txt
    agent-desk clear next
    agent-desk link tasks TASKS.org

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
    agent-desk read t-k2m9                   # mark one item read
    agent-desk read example-proj/rerun-nov   # mark everything under a node read
    agent-desk unread t-k2m9                 # undo a read mark
    agent-desk search cache

### Answers reach the agent

`agent-desk answer` (and the inbox) records the answer, then types this into
the session that asked:

    Answer to q-7f3a "Which split for the rerun?": B

A Paseo agent gets it through `paseo send`, which queues it if the agent is
busy. An agent in a tmux pane gets it pasted into that pane, but only while the
pane still runs the same process and session. Anything else goes to
`deliver_command` if you configured one. A reply travels the same way, as
`Message from the user via agent-desk, about t-k2m9 "Deploy finished": ...`.
When the session cannot be reached,
the answer stays recorded and undelivered, the command prints how to resume
the session, and `agent-desk deliver` tries again later. A question asked on
another machine is answered there after the next merge.

### The inbox

`agent-desk inbox` lists three sections, with a preview of the selected row:

- **NEEDS YOU**: open questions, blocked ones first, oldest first, grouped
  by project and run.
- **NEW**: unread notices, done reports and changed slots.
- **RUNS**: the tree of projects, runs and agents. Each node shows its state
  (`waiting on you`, `active 10m`, `quiet 4d`, `finished`) and how many open
  questions and unread items lie beneath it. Only the way to something that
  needs you is unfolded; everything else is one line, an agent's line carrying
  its status.

Typing filters the list, as in any fzf. Keys:

- `enter`: answer a question (type a line, or leave it empty to write the
  answer in `$EDITOR`), open an item in `$EDITOR` (its linked file or report
  when it has one), or fold and unfold a node.
- `ctrl-r`: mark read; on a node, everything under it.
- `ctrl-t`: answer a question with "Take your recommendation." or, for a
  question with a default, "Go ahead with your default: ...".
- `ctrl-o`: open the session that wrote the item: in Paseo for a Paseo agent,
  by selecting its tmux pane, or else by copying its resume command to the
  clipboard.
- `alt-a`: show all, including read items and finished runs.
- `ctrl-l`: refresh. `esc`: quit.

What counts as unread: notices, done reports, and the current text of each
slot, until marked read. A slot that changes is unread again. `status` slots
are never unread; they are shown while their node is live. An open question is
never hidden by reading: it leaves "Needs you" only when answered.

A run that is finished (its root coordinator filed a done report), has no open
question and nothing unread is hidden unless `--all` is given. Every view
accepts `--host` to show one machine's events.

After every write the tool re-renders plain org files, so `cat` is enough:

    ~/.agents/desk/render/inbox.org
    ~/.agents/desk/render/board/<project>.org
    ~/.agents/desk/render/ledger/<yyyy-mm>.org

Each item is a heading with a `:DESK_ID:` property and a keyword: `ASK` or
`ANSWERED` for questions, `NEW` for unread items. `agent-desk render`
re-renders by hand.

### The web dashboard

    agent-desk serve                          # http://127.0.0.1:7740/
    agent-desk serve --listen unix:/path/to/desk.sock   # behind a proxy
    agent-desk serve --config config.json     # listen, pid_file, allow_hosts from a supervisor's JSON

The same three sections as the inbox, on a desktop in three panes and on a
phone in one column. Every view is a URL you can bookmark: a project, run or
agent (`#/p/example-proj/rerun-nov`), one session's writing (`?session=`),
one machine (`?host=`), read items too (`?all=1`), a search (`?q=`) and an
open item (`?item=q-7f3a`). From it you answer questions (with one-click
"take your recommendation" or "go with the default"), message the agent
behind any item or any coordinator, and mark things read. It updates live.

It serves this machine's store only. To reach it from a phone, put it behind
something that authenticates you, such as a reverse proxy on a private
network; the server refuses cross-site writes and, on a TCP port, any `Host`
it was not told about (`--allow-host desk.example.net`). A proxy that rewrites
`Host` needs the page's public origin (`--allow-origin https://desk.example.net`,
or `origin` in `--config`); on a unix socket the proxy's `X-Forwarded-Host`
also counts. See
[dashboard.md](dashboard.md) to change the page.

## Notifications and other machine-specific commands

The core knows nothing about your desktop. It runs commands you name in
`~/.config/agent-desk/config.toml` (or `$AGENT_DESK_CONFIG`), each under
`sh -c` with the event as JSON on stdin:

    notify_command  = "my-notifier"        # a question, a tell --notify, a done report
    dismiss_command = "my-notifier --remove"   # its question was answered or it was read
    open_command    = "my-opener"          # what `agent-desk open <id>` does

The JSON carries `action`, `display_id`, `path`, `event` (the full event),
`cause` (for a dismissal, the answer or read), `local` (written on this
machine), and `agent_desk`, this binary's path, for building a click action
such as `agent-desk open q-7f3a`. Notify and dismiss run detached; their
output goes to `~/.agents/desk/hooks.log`. With no config, nothing is run.

`agent-desk open <id|path>` hands the item to `open_command`, or without one
opens the inbox scoped to the item's node.

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
give a broken copy. Merged and imported events raise notifications only with
`--notify`, but a question answered elsewhere still has its notification
dismissed here. Merging is idempotent and order-free, since events never
change and ids are globally unique. Items from another machine show its name.
