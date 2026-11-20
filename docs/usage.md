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
  `b` deliverable, `s` slot text, `l` linked slot, `c` cleared slot, `j` join, `o` leave,
  `r` read mark).
  Commands accept the display id, its part after the dash, any unique prefix of
  that part, or a full id.

## Agent side

Agents learn this part from the skill, [skills/agent-desk](../skills/agent-desk/SKILL.md).

Only coordinators write. A coordinator first joins a node:

    agent-desk join rerun-nov/coord              # first coordinator of a run
    agent-desk join rerun-nov/coord/eval-lead    # nested, under its parent
    agent-desk join rerun-nov/coord --takeover   # replacing a coordinator
    agent-desk takeover <paseo-id|session-id>    # every node a stopped agent held

The project is the repository the working directory is in (`--project` to
override). A node already held by another session refuses `join` unless
`--takeover` is given, and the session it was taken from can no longer write
there. A nested coordinator can only join under a node someone has joined.
`takeover` does that for every node another agent holds on this machine, in
one step: name it by its Paseo id (any session it ran as counts) or its
session id. That is how a replacement inherits a stopped agent's whole role.

A session **holds** every node it joined, until it leaves the node or another
session takes it over. One session can coordinate two unrelated runs this way:

    agent-desk join rerun-nov/coord
    agent-desk join ablation-dec/coord
    agent-desk whoami                       # both, one per line
    agent-desk set status 'x'               # refused: which node?
    agent-desk set status 'seeds 3/5' --node rerun-nov/coord
    agent-desk ask --blocked --title T --node ablation-dec/coord <<'Q' ... Q
    agent-desk done --title 'Ablation finished' --node ablation-dec/coord
    agent-desk leave ablation-dec/coord     # back to one node: no --node needed

While a session holds one node, nothing changes. While it holds several, `ask`,
`tell`, `set`, `clear`, `link`, `done` and `task add` (and `get`,
`show --mine` and `instructions`) refuse without `--node PATH`, listing the
held nodes; there is no default. (`tell --re` and `ask --re` need no `--node`: the message
it responds to names the node.) `--node` takes the full path
(`project/run/role`) or one relative to the project, and only a node the
session holds. `leave` drops a node; a node its holder left is free for
another session to join. Messages and answers to any held node reach the
session.

A session that switched nodes by joining again, before a session could hold
several, now holds all the nodes it still owns. It gets the refusal until it
passes `--node`, or runs `agent-desk leave` on the nodes it has finished with.

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

**A script that acts on the answer itself.** `--on-answer CMD` stores a
command with the question. The question is still filed from an agent
session (a script the agent runs, as daily-sync does), but the answer need
not cost that agent a turn. When the user answers, the desk runs it on the
machine that asked, before it reaches any session:

    agent-desk ask --blocked --title 'Conflict in notes/private' \
      --choices /tmp/picks.json \
      --on-answer 'daily-sync apply-answer notes/private' <<'Q'
    ...
    Q

- The answer's text is on its standard input, in the format above.
- `AGENT_DESK_QUESTION` and `AGENT_DESK_ANSWER` hold the display ids
  (`q-7f3a`, `a-2c9d`), `AGENT_DESK_QUESTION_ID` and `AGENT_DESK_ANSWER_ID`
  the event ids, `AGENT_DESK_PATH` and `AGENT_DESK_TITLE` the question's node
  and title.
- It runs under `sh -c` in the asker's working directory, for at most 10
  minutes. The dashboard and the other answer paths do not wait for it: it
  runs in a background `agent-desk deliver`, whose output goes to
  `hooks.log` in the desk directory.
- **Exit 0** means it handled the answer. The answer counts as delivered and
  no session is woken. The dashboard shows "handled by its command", with the
  command's last 4,000 bytes of output on hover.
- **Any other exit** passes the answer to the asking session as usual, with
  the exit status and output after it. That is the way to hand an agent what
  the script cannot decide alone (an "Other" answer, say).
- It runs once per answer. If the session cannot be reached afterwards,
  `agent-desk deliver` retries the session, not the command.
- Only the machine that asked runs it, like any delivery. Anyone who can
  write events into your desk (your own machines, through merge) can file a
  question with a command, so merge only with machines you trust.

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
`agent-desk whoami` prints the nodes this session holds, one per line, and
exits 1 when it holds none.

A substantive answer to something the user asked in chat is filed the same
way, with the question in one line instead of an id:

    agent-desk tell --re-chat 'Why is seed 2 slow?' <<'R'
    Seed 2 drew shard 17, which has duplicated rows.
    R

Responses of both kinds are listed in their own section, **Responses to
you**, between Needs you and New, and notify unless sent with `--quiet`.

**Every kind can respond.** A notice, a question, a done report and a
deliverable all take `--re` or `--re-chat`, and then sit in that message's
thread. Each is still listed where its kind belongs: a question response in
Needs you, a deliverable in Deliverables, a notice or a done report in
Responses to you.

When the agent needs a decision before it can act on a message, it responds
with a question instead, a **question response**:

    agent-desk ask --re m-4c2d --blocked --title 'Which feed?' <<'Q'
    There are two old feeds: v1 and v2-legacy. Which one?
    Q

It takes `--blocked` or `--default`, and `--choices`, like any question, and
`--re-chat` for a chat question. It counts as the message's response, is
listed in Needs you (not Responses) and in the message's thread, notifies as
a question, and your answer goes back to the agent like any other. A
message's own notify setting can make it louder, never silence it, and
reading the item the message was about does not dismiss it.

When a session ends, its summary goes on stdin:

    agent-desk done --title 'Rerun finished' --report ~/reports/rerun.org <<'S'
    All five seeds done; the fix holds.
    S

This files a done report and sets the node's `status` to "Finished." (or
"Finished; see report."). `--re` makes it the response to a message.

**Deliverables.** When something is ready for the user to open or install
(a deployment, an Android APK, a release), file a deliverable instead of a
notice:

    agent-desk deliverable --title 'Android APK 1.4.2' --key android-apk \
      --attach build/app-release.apk --caption 'Release build' <<'D'
    Install it over 1.4.1; your data stays. Checked on the emulator: login and sync.
    D
    agent-desk deliverable --title 'Site on staging' --url https://staging.example.org/ <<'D'
    The comparison table is live.
    D

It needs `--url` (http or https), `--attach`, or both. The body says what it
is, how to try it and what you checked. `--key` names what it is a version
of: a newer deliverable with the same key in the same project supersedes the
older one, which leaves the Deliverables section (it stays in its thread and
in Show all). `--re` and `--re-chat` work as for `tell`. Android packages
(`.apk`) may be up to 150 MB.

The dashboard lists deliverables in their own section, **Deliverables**,
above Responses to you and below Needs you. The item page shows a large
**Open** button for the link and a download button per file, with **Works**
(messages the agent "Works.") and **Problem…** (opens the message box).
On the keyboard: `SPC i g` opens the link, `SPC i G` downloads the file,
`SPC i w` says it works. Their notify class is `deliverable` (desktop: the
machine it came from; phone when away), set in Settings like any other. A done report from a run's root coordinator marks
the run finished. `agent-done` does this for you where it is installed.
`agent-desk show --mine` shows only the calling session's own node.

**Attachments.** `ask`, `tell` and `done` take any file: a plot, a
screenshot, a voice note, a screen recording, a PDF, a CSV. Each `--attach`
needs a `--caption` right after it, saying what the file shows and why it
matters, since the caption is what the user reads first and what a terminal
shows:

    agent-desk tell --title 'Seed 2 diverges' \
      --attach plots/loss.png --caption 'Seed 2 (orange) leaves the others at step 400' <<'R'
    Shard 17 has duplicated rows; seed 2 drew it.
    R

Up to 8 files per item. Images may be 10 MB, audio 50 MB, video 100 MB and
anything else 25 MB. The dashboard shows images and plays audio and video
(mp4, webm, mov); every other file is a download under its own name, never
opened in the page. A file of a type the desk does not know is kept as
`application/octet-stream`. The desk copies each file into `<store>/media/`,
named by its content hash, so the agent may delete its own copy. The inbox,
`show` and the ledger list each file with its caption and path.

**The user's files.** The answer and message boxes in the dashboard have an
Attach button, and take pasted or dropped files too. Each file is uploaded
when it is added and goes out with the answer or message; a message may be
files alone. From the CLI, `answer` and `reply` take `--attach FILE`, with
`--caption` optional. The session gets the message followed by the files'
paths on its machine, one per line with type, size and caption, so the
agent reads them as files. An answer for a session on another machine waits
there until its files have arrived with the merge.

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
    agent-desk sets define work agent-desk private/linear-finetune   # a project set
    agent-desk focus work                    # show and notify only for it; focus off ends it
    agent-desk show --unfocused              # everything, whatever the focus

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
the session, and `agent-desk deliver` tries again later. The reason is kept
on the desk: the dashboard shows it under the answer, with the resume command
and a **Retry** button, and the inbox and the org ledger show it too. Every
attempt, delivered or not, adds a line to `deliver.log` in the desk
directory. A question asked on another machine is answered there after the
next merge.

### Sending from the dashboard

Pressing Send or Answer shows **queued** immediately. The browser keeps the
message in local storage until the desk records it. If the request fails
because the phone goes offline or the server is unavailable, the dashboard
retries while open and resumes the queue on the next load. A repeated HTTP
request returns the existing message.

The desk retries a confirmed delivery failure after 15 seconds, doubles the
delay after each failure up to 10 minutes, and stops within 24 hours of the
first attempt. The conversation shows **retrying** with the last error.
The desk stops early if the message has no transport or its recorded tmux
pane is confirmed dead or replaced and there is no delivery command.
After retries stop, **Retry** starts a new window. A successful delivery
shows how it reached the agent, including whether the agent itself queued
it until its turn ended. For a session on another machine, the conversation
shows when it is waiting for a merge.

If the transport may already have accepted a message, the desk shows
**delivery unconfirmed**. This can happen on a timeout, a lost connection
during send, an interrupted desk process, or a custom command that exits
with an error after it starts. Check the agent's conversation for the
message. **Send again anyway** asks for confirmation because the agent
could receive it twice. At a terminal, `agent-desk deliver --force <id>`
makes the same explicit choice for one answer, question, or reply.

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
  its status. A Paseo agent is `active` while it is running; anything else is
  `active` for an hour after its last event, then `quiet`.

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
leaves "Needs you" and no longer makes its run show "waiting on you", but it
stays open: its agent is still blocked (or goes ahead with its default), you
can still answer it from `--all` or "Show all", and
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

The sidebar's Focus list switches the focus between your project sets (see
Project sets and the focus); the gear beside Focus edits the focused set (or
the first one), and creates others, by ticking projects or some of a
project's runs. While a set is focused, "All projects" counts what the focus
hides that needs you (hover it for the breakdown); clicking it leaves the
focus.

The same sections as the inbox, on a desktop in three panes and on a phone
in one column: Needs you, Pinned (when anything is), Responses to you (the
agents' answers to what you asked), New, and the run tree. Click a section's
title to fold it. Each row names its project, run and agent below what the
view or its group heading already says. Opening an item marks it read (a
question excepted: reading one dismisses it), but the row stays where it
is, dimmed once you open another, for a minute (Settings: "Keep a read item
in the list for", per device). The icon beside its id marks it unread
again, and the pin icon keeps it on top. Items you mark read with a
section's mark-all button or the selection's Mark read leave the list at
once; if the desk refuses, they come back with the error.
An item id written in any text on the desk ("see q-8tqk", or `` `q-8tqk` ``
alone in code) links to that item; hover it for the title and state. Only
ids the desk knows link, so "t-test" stays text, and nothing inside a code
block or a longer code span links. The org files link such ids to the item's
heading when it is in the same file, and notifications carry the links for
the notify command.
Previous and Next at the bottom of an item walk the list in the order
shown. Once you answer a question, approve or deny a screen request, or
dismiss a question, the page moves on: on a wide screen to the next question
still in the list (else the next item), skipping pinned items wherever they
are listed, and back to the list when only pinned items are left; on a phone
back to the list. Sending a message in a conversation moves on the same way.
The toast's Back (Undo for a dismissal) returns to it. Settings, "Move to the
next item" (per device), turns either case off, so you stay on the item.
An item the page moved you to is not marked read just by being shown: it
becomes read once you act on it (answer, message) or move on from it (to
another item or the list). Back or Undo leaves it unread, and so does
marking it read or unread yourself, which settles it. The search filters every section, and the run tree by project, run
or agent name and by the session or Paseo id of whoever joined a node.
A row of the run tree has icons on its right (on hover with a mouse, always
on a touch screen): copy the Paseo id of the agent behind it (when it runs
under Paseo on this machine), message that agent (opens its node with the
message box ready), and mark everything under it read.
Clicking a part of an item's path shows that subtree. Every view is a URL you can bookmark: a project, run or
agent (`#/p/example-proj/rerun-nov`), one session's writing (`?session=`),
one machine (`?host=`), read items too (`?all=1`), a search (`?q=`), oldest
first (`?sort=old`; recent first is the default) and an open item
(`?item=q-7f3a`). From it you answer questions (with one-click "take your
recommendation" or "go with the default"), message the agent behind any item
or any coordinator, dismiss a question without answering it, and mark things
read. What you type is kept as a draft for that item and that box (your
picks too), in the browser's storage: switching items or reloading loses
nothing, and it is dropped when you send it or after 30 days.

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
where its response notifies you. When the session has finished (its Paseo
agent is archived, or a session Paseo does not know filed a done report) the
composer says that sending revives it. A done report from a live Paseo agent
is a milestone, not an end. The Notify chip on a node opens its settings, and Settings in the sidebar
opens the global ones: one control per setting with where its value comes
from ("inherited from example-proj"), for all kinds of event or one, and
"Reset to inherited" where an override stands. To act on many items at once, press Select (or long-press a row on a
phone, or cmd-click one on a desktop), then tap rows, shift-click a range or
use "Select all" per section or group, and mark them read or unread. The
double check on the New and Responses headings marks that whole section read
in one click, with Undo in the toast, and so does each project and run heading
within them (a fainter icon, marking just that group); Needs you has none,
because reading a question there dismisses it. It
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
message (messaging an item's agent marks it read, and what you sent stays
in its card until you leave the screen); an agent with open questions gets
a **Desk** pill on its composer, opening a panel with only its items; and the
Command Center has both. Its daemon side runs `agent-desk` on the daemon's machine (`$AGENT_DESK_BIN`,
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
`sh -c` with the event as JSON on stdin. A store other than
`~/.agents/desk`, named by `AGENT_DESK_HOME`, never reads that file: give it
its own with `AGENT_DESK_CONFIG`. So a test or fixture store cannot run your
notifier.

    notify_command  = "my-notifier"        # raise a notification; print its handle
    dismiss_command = "my-notifier --remove"   # take one back
    open_command    = "my-opener"          # what `agent-desk open <id>` does
    base_url        = "https://desk.example.net/"   # the web dashboard, for links
    away_command    = "my-away-check"      # exits 0 while you are away from this machine
    remote_label    = "Phone"              # what the dashboard calls the remote channel
    media_keep_days = 30                   # see Attachments are temporary; 0 keeps them
    [ssh_hosts]                            # desk host name = ssh destination
    otherhost = "otherhost.example.net"

**Channels.** A notification goes out on up to three channels, chosen by
the settings below (see Settings):

- `desktop`: a card on a machine. `notify.desktop` says which machines:
  `origin` (where the event was written), `all`, or host names.
- `remote`: something that reaches you away from the desk, such as a chat
  message to your phone. `notify.remote` says when: `off`, `away` (only
  while `away_command` exits 0) or `always`. Only the machine the event was
  written on raises it.
- `push`: Web Push to the browsers that turned it on, such as the dashboard
  installed on an Android phone. Built in, no command needed: see Push to
  your phone below. `notify.push` says when, like `notify.remote`.

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
They have no terminal: anything that prompts or opens `/dev/tty` fails
rather than waits. A command still running after a minute is killed with
its children. One `notify-run` works at a time; one that waits more than
three minutes for the others gives up and logs which process holds
`notify.lock` and since when. With no config, nothing is run.

### Push to your phone

The web dashboard can notify a phone directly, even while the app is closed.
On the phone, open the dashboard (over HTTPS; on Android, Chrome; on an
iPhone, only the copy added to the Home Screen), install it if you like, go
to Settings and press **Turn on** under "Push to this device". The browser
asks for permission and then subscribes; **Test** sends one at once.
Allowing notifications for the app in the phone's own settings is not
enough: the page has to subscribe. So on a phone or an installed app that
does not receive pushes yet, a strip above the list says so, with **Turn on**
(the same thing) and **Not now** (hidden on that device for 30 days).
**Turn on** reports success only once the desk lists the device. A browser
that still holds a subscription the desk no longer lists registers it again
by itself, without the strip, and when the browser replaces its
subscription the service worker subscribes again and tells the desk.

- What is pushed is the `notify.push` setting: `off`, `away` or `always`,
  per class. Built in: blocked questions, always. To add done reports:
  `agent-desk pref set notify.push always --class done`.
- Tapping a notification opens the item. Once the item is answered, read or
  dismissed anywhere, a second push closes the notification.
- The machine that serves the dashboard sends the pushes, for events
  written there and those it merges in from other machines. Subscriptions
  and its key pair live in `~/.agents/desk/push/` (owner-only), not in the
  event log, so they do not travel with a merge. `push/log` there records
  every subscribe, unsubscribe and removal, every device dropped because the
  push service called it gone, and every failed push, with the device's id
  and label: start there when a phone stops getting notifications.
- Messages go through the browser maker's push service (Google's, for
  Chrome) encrypted end to end (RFC 8291): the service sees when a push
  happens, not what it says. The desk signs its requests with its own key
  (VAPID, RFC 8292); the contact it gives the service names this software,
  not you, unless you set one:

      [push]
      subject = "mailto:you@example.net"   # optional
      off = true                           # stop pushing, keep the devices

- A device the push service reports gone is dropped. The Settings page lists
  every subscribed device, with its last push and a Remove button.
- Chrome expects every push to show something. A take-back that finds its
  notification already cleared shows nothing, which Chrome tolerates now and
  then; if Android ever shows "This site has been updated in the
  background", that is why.

`agent-desk open <id|path>` hands the item to `open_command`; without one it
opens `url` in the browser when `base_url` is set, and otherwise opens the
inbox scoped to the item's node. A notification click has no terminal to show
an error, so every `open`, what `open_command` printed to stderr and any
failure also go to `hooks.log` in the desk directory.

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
- `notify.push`: `off`, `away` or `always`, for Web Push to the devices
  that turned it on (see Push to your phone).

For one event, the first match wins:

1. what the message itself asks for (`--behavior`);
2. a `pref` at the event's node, then at each parent up to the global
   scope. At one scope a setting for the event's class beats one for any
   class, and a nearer scope beats a farther one whatever the class;
3. the config file's defaults, a class's table before the general one;
4. the built-in values: steer; desktop notifications on the origin machine
   for everything but a plain `tell`; remote notifications only for blocked
   questions and responses, and only while you are away; pushes for
   blocked questions.

Overrides are `pref` events in the store, so they merge between machines and
keep their history. Only you write them: `pref set` from an agent session is
refused. Defaults go in `config.toml`:

    [defaults]
    "notify.remote.dest" = "example-chat"
    [defaults.class.ask-blocked]
    "notify.remote" = "always"

## Project sets and the focus

A **project set** is a name and a list of path prefixes. A prefix is
usually a project (`agent-desk`), but it can be any path on the desk, such
as one run (`private/linear-finetune`); it matches whole path segments, so
`site` does not take in `site-old`. The **focus** is one set, or none.
While a set is focused, the desk shows you only what is under its
prefixes, and notifies you only about that, so you can work on a group of
projects without the others pulling at you.

    agent-desk sets                          # list them; * marks the focused one
    agent-desk sets define work agent-desk caap private/linear-finetune
    agent-desk sets define work agent-desk   # defining again replaces it
    agent-desk sets delete work
    agent-desk focus work
    agent-desk focus                         # the focus, and what it hides
    agent-desk focus off

What the focus changes:

- `show`, the inbox and the dashboard list only the items, runs, tasks,
  Needs you entries and Out of quota agents under the set. The desk's own
  items (a screen lock request) still show. `show --unfocused` ignores the
  focus; `show --mine`, an agent's own view, never uses it.
- Notifications on every channel (desktop, remote, push) are raised only for
  items inside the set. An item outside is still stored and stays unread;
  when the focus ends it simply shows again. It is not notified then, which
  would bring a flood at once.
- `show` says on stderr what the focus hides ("4 hidden: 1 waiting on
  you"), and the dashboard shows it beside the focus.

Nothing else changes: agents write, ask and receive your answers as usual,
and you can still open any item by its id.

The focus is one for the whole desk, not per device, because it governs
notifications: the phone and the laptop agree on it. Sets and the focus are
events (`projectset` and `focus`), so they merge between machines and the
latest wins. An agent session may define sets but may not change the focus,
which is yours, as settings are. Deleting the focused set turns the focus
off. A machine running an older agent-desk skips these events when it
merges and takes them in once updated.

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
open, started, deferred, done or dropped. Desk tasks replace a `TASKS.org`
checklist: agents keep theirs with `task add`, `task start`, `task done`;
the node panel shows "Tasks: 3 of 7 done" with the open ones, and `show`
lists the ones still active.

- `--type` says what it is: `task` (the default), `bug`, `feature`, `idea`
  or `chore`.
- `--parent w-...` makes it a step of another task, so a milestone holds
  its items.
- `task list --type bug` keeps one type; `task list --org [--all]` prints
  the checklist as org, one heading per node and subtasks indented
  (`[-]` started, `[X]` done, dropped ones struck out with `--all`), for a
  human or a compaction recovery that wants one file.

**The Tasks page** (sidebar **Tasks**, `g c` here, `g C` everywhere, also
`SPC o t`/`SPC o T`) lists every task under the current path, grouped by
node with subtasks under their parent. Filter by Open, Finished or All, by
type, and by search (`/`). The box checks a task off (Undo in the toast)
or reopens it; clicking its title shows its brief with Start, Drop and
Open. **New task** (`SPC n t`, or "+ New" on a node page) files one at any
project, run or agent: a title, a type, an optional parent and details.
When the node has a coordinator, "Message its coordinator about it" (on by
default) also sends it a message naming the task, so it acts on it now.

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

## The screen lock

An agent that must drive the GUI of the machine it runs on (click through an
app, type into a window, take focus) takes the machine's screen first, so you
know to keep off the keyboard, and two agents do not fight over it:

    agent-desk screen take --reason 'Clicking through the settings app' --for 20m
    agent-desk screen release
    agent-desk screen status            # who holds it, who asks, how long you have been idle

The lock is per machine and outside every project (its events are under
`_desk/screen/<host>`), so a worker may take it too. `take` grants it at
once when the screen is free, nobody asked first, and you have been idle at
least `[screen] idle_minutes` (30): on macOS the HID idle time from `ioreg`,
elsewhere whatever `[screen] idle_command` prints in seconds (without one, it
is never granted unasked). Input while an agent holds the screen, and for 2
minutes after its hold runs out (not after it releases it), is the agent's:
an agent that clicked through its hold and asks again finds you as long away
as when it was first granted, not freshly back. Otherwise it files a request
in Needs you and exits 3. While the dashboard's server runs, it checks every
minute and grants the oldest request once the screen is free and you have
been away long enough; the agent hears it as the answer. You may approve a request at any time, whatever your idle time:
**Approve** or **Deny** on the dashboard, `agent-desk screen grant|deny
<q-id>`, or any answer (an answer that starts with "no", "deny" or "not
now" denies it). The answer reaches the agent as usual; `take --wait` also
notices the grant itself. After a denial the agent may not ask again for 10
minutes.

A grant lasts `--for` (30 minutes by default, at most 2 hours); `take` again
renews it. While an agent holds the screen, the dashboard shows who, why and
until when above Needs you, with **Take it back**, which releases it (the
agent is not stopped). `[screen] banner_on` runs when an agent gets the
screen, with `AGENT_DESK_REASON` and `AGENT_DESK_SECONDS` set, and
`banner_off` when it lets go: point them at whatever tells you to keep off,
such as a banner across the screen. Each grant tells the agent to hold the
screen only while its automation truly needs focus or needs you away.

## Agents out of quota

Under Needs you, the dashboard's **Out of quota** section lists the Paseo
agents whose last turn stopped at a usage limit, with their provider, when
they stopped, their parent and subagents (marked when those are out of quota
too), the desk nodes their session holds (one path, or "N nodes" with
the paths in the tooltip), and, where the agent-quotas keeper runs, when its
account has quota again ("quota back in 2h 14m, at 21:30", or "quota
available now"; the tooltip names the account). Under a node, it lists only the agents working
there: an agent belongs to every node its own session holds, or else to
those of its nearest Paseo ancestor that holds any, and it is listed under
each of them. For each:

- **Auto-continue** registers it to be sent `Continue.` when its own quota
  returns, or unregisters it.
- **Ask parent to reassign** (only when its parent still has quota) messages
  the parent: pick an account with quota left (the quota-management skill),
  start a replacement with the same role that takes over all of its desk
  nodes with one `agent-desk takeover <its Paseo id>`, recover the stopped agent's partial work, archive it, and look after
  its subagents.
- **Replace with a new agent** starts an agent through `[tasks]
  launch_command` (with an optional profile) that recovers the stopped
  agent's work, takes over all of its desk nodes in one step (`agent-desk
  takeover <its Paseo id>`, which also drops them from the stopped session;
  with several it then writes with `--node`), tells its parent and subagents, and archives it.
  **Replace…** opens a dialog listing every enabled, available Paseo
  provider (`paseo provider ls`), each with its account's quota where the
  keeper knows it ("work · quota left", "default · out of quota, back Mon
  15:00"). Picking a provider fills in its model and effort, both still
  editable: the stopped agent's own model and effort when the provider
  offers that model, else what you last started on that provider, else
  Paseo's first model at its default effort. The dialog preselects a
  provider with quota that offers the agent's model on another account
  (Claude to Claude (Work)), else the one you last used, else the first with
  quota. The `[quota] profiles` show as presets. The dashboard remembers
  your last choice per provider in the browser. The keyboard command is
  `quota-replace` (`SPC SPC` or `M-x`).
- The id icon copies its Paseo id. To copy several, select them (the list's
  Select mode shows a checkbox on each; ⌘/Ctrl-click a card also selects it,
  and shift-click a range), then **Copy Paseo IDs** copies the selected ids,
  one per line.
- **×** dismisses it: hidden until it runs and stops again. "Show N
  dismissed" brings dismissed ones back into view, each with an undo.
- **Archive** archives it in Paseo (`paseo archive`, after you confirm), so
  it leaves the list for good. Messaging it from the desk revives it. The
  keyboard command is `quota-archive`.

The node panel of any agent's node also has the **Auto-continue** checkbox,
so you can arm it before the agent runs out, and **Archive agent** (`SPC n
a`), which archives the coordinator's Paseo agent when it runs on this
machine. The server only passes `paseo archive` an id this machine's Paseo
has a record of (`POST api/agent` with `{"action": "archive", "id": ...}`).

The same from a terminal:

    agent-desk quota list [--format json]
    agent-desk quota auto-continue on|off <paseo-id>
    agent-desk quota ask-parent <paseo-id> [--dry-run]     # --dry-run prints the prompt
    agent-desk quota replace <paseo-id> [--profile claude/opus@high] [--dry-run]
    agent-desk quota dismiss|undismiss <paseo-id>

The two prompts are `internal/prompts/reassign-role.md` and
`replace-agent.md` (Go templates); a file of the same name in
`~/.config/agent-desk/prompts/` replaces either. They get `.Nodes`, the
agent's desk nodes (each with `.Path`, and `.Join` and `.Project` for `join`);
`.Node`, `.Join` and `.Project` are the first of them, for prompts written
when an agent held one. The list comes from
`[quota] stopped_command`, which prints the stopped agents' ids one per line;
the section reads each agent's details from its Paseo record
(`$PASEO_HOME`, default `~/.paseo`). `[quota] auto_continue_command` runs
under sh with `list`, `on ID` or `off ID`; `list` prints the registered ids.
Without `stopped_command` the section stays hidden. Dismissals are kept in
`<desk>/quota-dismissed.json`, each with the agent's last activity time, so an
agent that runs again and stops again shows up again. In the dashboard, open
**Show dismissed** to restore an agent. Select mode can dismiss or restore
several agents together.

    [quota]
    profiles = ['codex/gpt-6.1-terra@high', 'claude/opus@high']   # the Replace menu

## Paseo prompt cache

When agent-quotas is available, each local Paseo agent has a cache policy
control in its node panel, run-tree row and Out of quota row. A **cold**
label means the keeper estimates that a context above its size threshold has
expired.

- **Off** leaves the agent's cache alone.
- **Compact** compacts a large context once before its cache expires.
- **Warm** uses keepalive turns up to the idle limit (six hours by default).
  A large context is compacted before the keeper starts pinging. When the idle
  limit is reached, the display says **dormant**, and the next real turn can
  rearm warming.
- **Standby** switches permanently from warm to compact until you change the
  policy again. It is available through the CLI.

    agent-desk cache
    agent-desk cache warm --warm-hours 3
    agent-desk cache off <paseo-id>
    agent-desk standby [<paseo-id>]

Commands default to the current session's `PASEO_AGENT_ID`. The user can
name any local agent, and agents can change their own policy. The desk
records changes made through the dashboard or CLI in its monthly ledger
(`<desk>/render/ledger/`). Agent-quotas owns the policy and the scheduler.
The desk uses `~/.local/state/agent-quotas/agent-quotas.sock` by default.
Set `socket` under `[keeper]` in the desk's TOML config to use another Unix
socket.

When an answer or message targets a local Paseo agent with a probable cold
cache and a large context, the dashboard offers **Cancel**, **Send anyway**
and **Compact then send**. Compaction reads the full context once and can
lose detail. It can save time and tokens when more turns follow. If you
choose compaction, the thread says **waiting for compaction** until the
keeper finishes. When a message includes a recording, the desk waits for
transcription before requesting compaction. If compaction fails or is
canceled, the desk sends the message and records the outcome in the thread.

Without agent-quotas, the node panel reports its absence and the dashboard
queues sends normally.

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

They never appear in the inbox, the dashboard lists or notifications. The
dashboard shows them on their own page instead: **Verbatim log** in the
sidebar footer. It lists the messages newest first, grouped by day, scoped
to the project or run you are viewing (one link widens it to everything).
The search box keeps messages that contain every word you type, in the text
or the path, and marks the hits; each message has a Copy button. The page
reads `GET /api/verbatim?prefix=&q=&host=&limit=&offset=`, which returns
`{items, total}`, newest first, with the same filtering as `instructions`.

A session holding several nodes keeps each prompt under every one of them;
`instructions` shows such a prompt once.

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

## Dashboard voice transcription

Use **Record voice** beside an answer or message box. Speak while the desk
shows the partial transcript, then select **Stop recording**. The final
transcript goes into the message as a fenced `speech-to-text` block. Phone
recording requires opening the dashboard over HTTPS.

You can select **Send** while the recording or transcription is still running.
Sending stops the microphone and queues the message with its voice token.
Delivery starts when every recording is ready. If transcription fails, the
thread shows why. **Retry transcription** uses the saved audio and resumes the
queued send when it succeeds. **Send without recording** queues the same
message with a short note in place of the failed recording.

The running desk server owns transcription. Its config can select a backend
and set the recording retention period.

```toml
[voice]
stt_backend = "paseo"
keep_days = 7
```

- `paseo` is the default. It connects to the local running Paseo daemon
  using its own runtime endpoint and local credential. The daemon chooses the
  speech engine. Paseo defaults to local Parakeet through sherpa-onnx and can
  be configured to use another engine.
- `handy` uses the local Unix control socket of a Handy build with live
  socket transcription. Open Handy and select a streaming-capable model.
  Set `handy_socket` to the absolute socket path if Handy and the desk use
  different temporary directories.
- `command` runs the `command` string under `sh` and streams signed 16-bit
  little-endian PCM at 16 kHz (mono) to stdin. The program should read audio
  as it arrives, print newline-delimited JSON objects such as
  `{"type":"partial","text":"so far"}` and
  `{"type":"final","text":"finished text"}` to stdout, and exit successfully
  after stdin reaches EOF. Configure the command to make it available for
  retries with that backend too.

**Recordings** is available beside the composer and globally under
**Settings**. Recordings stay available for seven days from capture by default.
`voice.keep_days = 0` keeps them until manually deleted. `agent-desk gc`
prunes recordings independently of attachment retention, and
`agent-desk gc --dry-run` previews both. Recordings are kept on the server
that received them, under its media directory.
