# agent-desk: implementation spec

@Fable5.2 [2026-11-15 Sun]

For the implementing agent. Nothing here is built yet. Values in examples are
fabricated; keep it that way in code, tests and docs, because this repository
is public.

## Goal

Agents' questions and reports for the user get lost in transcripts. Build a
store that agents write to and the user reads from one place, with
notifications, a dashboard, and answers routed back to the asking agent.

## Terms

- **Event**: one immutable row in the store. Everything is an event.
- **Ledger**: the events addressed to the user that never change: questions,
  answers, notices, done reports.
- **Slot**: a named box of current-state text at a path. Writing a slot
  replaces its text; the previous text remains as an older event.
- **Board**: the current text of all slots, arranged as a tree.
- **Path**: where an event belongs: `project/run/role[/role...]`, plus a slot
  name for slot events.
- **Node**: a path prefix (a project, a run, or an agent within a run).
- **Coordinator**: a session that has joined a node. Only coordinators write.
- **Open question**: a question with no answer event.
- **Unread**: an item whose latest version the user has not marked read.
- **Reply address**: the ids stored with a question that let an answer be
  typed into the asking session later.

## Decided

All settled with the user on 2026-11-15.

- Store: SQLite, one append-only `events` table as the only source of truth.
- All commands are one Go program.
- Surfaces, in order: terminal, then an HTML app served by the tool on each
  machine. Emacs commands and a Paseo plugin come later.
- Only coordinators write. Workers report to their parent.
- One board per project; paths are hierarchical; agent segments are role
  slugs, not agent ids.
- Nothing is deleted when a run or session ends. The user marks things read,
  and read things are hidden from dashboards. A changed slot is unread again.
- A question appears both in chat and in the ledger. The agent files it with
  a command and raises it in chat however it likes. No hook files anything on
  the agent's behalf.
- Task lists, verbatim user instructions and run state stay in their existing
  files for now; the board links to them.
- Stores on different machines can be merged.

## Repository and integration boundary

This repository holds the generic tool and its skill:

    go.mod, main.go, internal/...   the Go module; `go install` gives the binary
    skills/agent-desk/SKILL.md   how agents use it (phase 6, not before)
    docs/spec.md                    this file
    docs/                           user documentation, written as phases land

The skill directory must not exist until the commands it describes work:
skill linkers pick up every `skills/*/SKILL.md` at agent launch, and a skill
for a missing tool would mislead every session.

Everything specific to one person's machine stays out of this repository and
is reached through commands named in `~/.config/agent-desk/config.toml`,
each run with the event as JSON on stdin:

    notify_command   raise a notification for an event
    dismiss_command  remove that notification
    open_command     open an item or a session for the user
    deliver_command  type an answer into a session the core cannot reach

Unset means: no notification, and delivery limited to what the core does
itself (Paseo, tmux). The author's own integration (a macOS notification app,
Telegram escalation, kitty delivery, `agent-done`, Emacs) lives in their
scripts repository, cloned at `~/scripts`. Paths below such as `zshlang/...`
and `docs/agent-done.md` refer to that repository; read its `AGENTS.md`
before changing anything there, and commit there separately.

The store location is `${AGENT_DESK_HOME:-~/.agents/desk}`.

## Paths

    example-proj/rerun-nov/coord/status
    example-proj/rerun-nov/coord/eval-lead/findings
    example-proj/adhoc/fix-login/status

- `project`: basename of the repository root of the working directory, or of
  the working directory itself outside a repository. When the repository root
  is the home directory (a dotfiles repository), it is the first directory
  below home on the way to the working directory. Override with `--project`.
- `run`: named at join time. `adhoc` when a session never joined.
- `role` segments: the chain of coordinators from the run's root down, each a
  slug `[a-z0-9-]{1,24}`. A nested coordinator sits under its parent's path.
- Slot names: free slugs. Conventional ones: `status` (2 lines), `findings`
  (15), `artifacts` (15), `next` (5). Default cap for others: 15 lines.
  Reserved for linked files: `tasks`, `user-verbatim`, `state`.

Why slugs and not agent ids: an id changes when a coordinator is
replaced or handed to another provider, which would leave the old agent's
slots stale beside the new one's; and ids are unreadable on a dashboard and
untypable in a command. The real ids are stored in every event's `meta.from`.

### Joining

A session gets its path by registering, never from an inherited environment
variable (children inherit the parent's environment, which is how
`PASEO_AGENT_ID` misleads; see "Which agent" in `docs/agent-done.md`).

    agent-desk join rerun-nov/coord              #: first coordinator of a run
    agent-desk join rerun-nov/coord/eval-lead    #: nested coordinator
    agent-desk join rerun-nov/coord --takeover   #: replacement coordinator

- `join` appends a `join` event binding this session id to the node.
- A node bound to another session refuses `join` without `--takeover`. After
  a takeover the old session's writes are refused.
- A nested coordinator joins only under a node that has been joined.
- A write from a session with no binding: if the session has no parent agent,
  auto-join `adhoc/<slug>` (slug from the Paseo title or the session's short
  id). If it has a parent, refuse with "workers report to their parent; ask it
  to give you a node", adding that a coordinator can `join` explicitly: an
  agent launched by another agent but reporting to the user has a parent too.
- Session identity is resolved in Go from the agent's environment:
  `CLAUDE_CODE_SESSION_ID`, `CODEX_THREAD_ID` (older `CODEX_SESSION_ID`) or
  `ANTIGRAVITY_CONVERSATION_ID`. Nested agents inherit each other's
  variables; `AI_AGENT`, which the innermost agent sets, picks which applies.
  No zsh runs on the write path.
- Parent detection, ported from `h-bell-agent-role` and
  `h-agent-done-paseo-id`:
  - Paseo: the record `$PASEO_HOME/agents/*/<PASEO_AGENT_ID>.json` names
    this session only if its `persistence.nativeHandle` is this session id.
    Then the parent is its `labels["paseo.parent-agent-id"]`, if any.
    Otherwise this session was started by that agent, which is its parent.
  - tmux (skipped for a confirmed Paseo agent, whose `TMUX_PANE` is the
    daemon's): an explicit `@agent_role` of `main` or `sub` on the pane's
    session wins; otherwise `TMUX_SUBAGENT_NODE` or an `ag--*` session name
    means a parent.

## Store

`~/.agents/desk/desk.db`, per machine, never inside a repository.

    CREATE TABLE events (
      id    TEXT PRIMARY KEY,  -- ULID: globally unique, sortable by time
      ts    TEXT NOT NULL,     -- RFC 3339 with offset
      host  TEXT NOT NULL,
      kind  TEXT NOT NULL,     -- join ask answer delivered tell done set clear
                               -- link read reply
      path  TEXT NOT NULL,
      ref   TEXT,              -- event this one refers to
      level TEXT,              -- ask: blocked | default.  tell: fyi | notify
      title TEXT,
      body  TEXT,
      meta  TEXT NOT NULL      -- JSON
    ) STRICT;

- Triggers abort every `UPDATE` and `DELETE` on `events`. Append-only is
  enforced by the database, not by convention.
- `PRAGMA journal_mode=WAL; synchronous=FULL; busy_timeout=5000`.
- Indexes on `kind`, `path`, `ref`. Current state comes from SQL views
  (`slot_current`, `question_state`, `unread`), never from stored flags.
- `meta.from`: agent, seat (the `CLAUDE_CONFIG_DIR` or `CODEX_HOME` that
  selects the account, when set), session id, Paseo agent id, parent, working
  directory, transcript path, tmux pane. `meta.source`: `cli`, `chat` or `web`.
  Other keys: `default` (ask), `file` (link), `supersedes`.
- Driver: `modernc.org/sqlite` (pure Go, no cgo).
- The store refuses a directory on a network filesystem (NFS, SMB, AFP,
  WebDAV, FUSE and the like, by `statfs`), where WAL mode's shared-memory
  locking does not work, and says to merge stores instead.
- `agent-desk export` prints the events as JSONL, and `import` inserts
  events whose ids are new.
- Display ids: kind letter plus the shortest unique suffix of the ULID's
  random part, at least 4 characters (`q-7f3a`, `t-k2m9`). Letters: `q` ask,
  `a` answer, `t` tell, `d` done, `s` set, `c` clear, `l` link, `j` join,
  `r` read, `v` delivered, `m` reply. Commands accept the display id, the part after the
  dash, any unique prefix of that part, or a full ULID.

Event semantics:

- `ask`: a question. `level=blocked` means the agent stops until answered;
  `level=default` means it continues on `meta.default` unless told otherwise.
- `answer`: `ref` is the `ask`. A second answer to the same question is
  allowed and is the one that counts (a changed decision).
- `delivered`: `ref` is an `answer` that has been typed into its session.
- `tell`: a notice or interim report. `level=notify` raises a notification.
- `done`: a finished session's summary and report path, from `agent-done`.
- `set`: new text for the slot at `path`. `clear`: the slot is empty from now.
- `link`: the slot at `path` shows the file `meta.file` (rendered at view
  time; never unread).
- `read`: `ref` is the exact event the user has seen. Marking a node read
  expands, at command time, into one `read` per current unread item under it.

Unread rules: a `tell`, a `done`, and the latest `set` of a slot are
unread until a `read` names that event. A new `set` is a new event, so an
updated slot is unread again. Otherwise fresh information in a slot the user
once dismissed would stay hidden, which is the original problem. Exception:
`status` slots are never unread; they change constantly and are shown while
their run is live.

## Commands

One binary, `agent-desk`, built from the module at the root of this
repository. Bodies come on stdin. Every write
prints one line: the display id or `ok`.

Agent side:

    agent-desk join <run>/<role-path> [--takeover]
    agent-desk ask --blocked --title T                 <<'Q' ... Q
    agent-desk ask --default 'A: reuse the cache' --title T <<'Q' ... Q
    agent-desk tell [--notify] --title T               <<'R' ... R
    agent-desk set <slot> [text]        #: or text on stdin; cap enforced
    agent-desk clear <slot>
    agent-desk link <slot> <file>
    agent-desk answer <id> --from-chat 'text'   #: record only, no delivery
    agent-desk done [--title T] [--report F]    <<'S' ... S   #: also sets status
    agent-desk show --mine [--format json]      #: only this session's node

`set` over the cap fails with the count and the cap, and writes nothing.

User side:

    agent-desk inbox                 #: interactive dashboard (below)
    agent-desk show [<path-prefix>] [--all] [--format org|md|json]
    agent-desk answer <id> 'text'    #: record, then deliver
    agent-desk read <id|path-prefix> #: mark read
    agent-desk unread <id>           #: append a superseding marker; see note
    agent-desk search <text>
    agent-desk export | import | snapshot <file> | merge <source>
    agent-desk deliver | render | serve

`read` refuses a question: it leaves "Needs you" only when answered. Agent-side
writes come only from agent sessions; a shell with no agent session id is refused.

Note on `unread`: there is no deleting a `read` event. Model it as a later
event that cancels it (`kind=read`, `meta.cancel=true`); the view takes the
latest.

After every write the tool re-renders plain files, atomically (temp file plus
rename), so a human or a recovering agent needs only `cat`:

    ~/.agents/desk/render/inbox.org
    ~/.agents/desk/render/board/<project>.org
    ~/.agents/desk/render/ledger/<yyyy-mm>.org

In the rendered org, each item is a heading with a `:DESK_ID:` property and
a keyword: `ASK` or `ANSWERED` for questions, `NEW` or none for the rest.

## Merging stores

    agent-desk snapshot /tmp/copy.db    #: consistent copy of a live store
    agent-desk merge /tmp/copy.db       #: take what that file has
    agent-desk merge ssh:otherhost      #: both directions

- `snapshot` uses `VACUUM INTO`. It is the only supported way to copy a store;
  copying the file of a live WAL database can produce a broken copy.
- `merge <file>` opens the other database read-only and, in one
  `BEGIN IMMEDIATE` transaction, inserts every event whose id is absent
  (`ATTACH`, then `INSERT OR IGNORE ... SELECT`). SQLite's own lock, with the
  busy timeout, serialises it against other writers.
- `merge ssh:<host>` pipes `agent-desk export` from the host into a local
  import, then the local export into `agent-desk import` on the host, which
  then runs `deliver`. No database file crosses the network.
  `AGENT_DESK_REMOTE_COMMAND` replaces the remote `agent-desk` (a full path,
  or one with variables such as `AGENT_DESK_HOME=...`), and `AGENT_DESK_SSH`
  replaces `ssh`. An answer the other side cannot deliver yet is reported but
  does not fail the merge.
- Merging is idempotent and order-free, because events are immutable and ids
  are global. Running it twice, or in either direction first, gives the same
  store.
- Imported events raise no notifications unless `--notify` is given.
- Views show all hosts. An item from another host carries its host name, and
  every view accepts `--host`.
- Delivery across hosts: an answer is typed into a session only by the host
  that asked the question. `agent-desk deliver` sends every answer whose
  question is local and which has no `delivered` event, then appends one.
  `merge` runs it at the end. So answering on one machine a question asked on
  another works after the next merge.

## Dashboard

One information design, two renderings (terminal now, HTML in phase 7).
Four views, in this order:

1. **Needs you**: open questions. `blocked` first, then `default`; oldest
   first within each. This view is never empty-by-hiding: an open question
   cannot be marked read away, only answered.
2. **New**: unread notices, done reports and changed slots, grouped by
   project then run.
3. **Runs**: the tree. Read slots are hidden unless "show all" is on.
   A run with nothing unread, no open question and a `done` from its root
   coordinator is hidden entirely.
4. **History**: search over the ledger.

Run state, computed: `waiting on you` (open blocked question), `finished`
(`done` from the root coordinator), otherwise `active` with the age of its
last event; older than 3 days shows as `quiet`.

### Using the hierarchy

- **Roll-up.** Every node shows the number of open questions and of unread
  items beneath it, and takes the most urgent state beneath it: a project is
  `waiting on you` if any agent in any of its runs is.
- **Folding.** A node is expanded only along the way to something that needs
  attention. Everything else is one line, which for an agent node is its
  `status` text. A quiet project costs one line however many agents it has.
- **Scoping.** Every view takes a path prefix:
  `agent-desk inbox example-proj/rerun-nov` shows all four views for that
  subtree only. In HTML each node has its own URL, selecting a node scopes to
  it, and a breadcrumb scopes back out.
- **Grouping.** "Needs you" and "New" group by project, then run, and label
  each item with the rest of its path, so one run's questions sit together.
- **Sibling order.** Waiting on you, then unread, then active by recency,
  then quiet. A coordinator's own slots come before its nested coordinators.
- **Node actions.** Mark the subtree read; copy the path.
- **Notifications** name the path and open the dashboard scoped to it.

Wide layout (HTML desktop; the terminal shows the left column as the list and
the right as the preview):

    +------------------------------+-------------------------------------+
    | NEEDS YOU (2)                | q-7f3a  BLOCKED  2h                 |
    | > BLOCKED 2h  example-proj   | example-proj / rerun-nov / coord    |
    |   Which split for the rerun? |                                     |
    |   DEFAULT 5h  site           | Which split for the rerun?          |
    |   Drop the legacy feed?      | The rerun needs a train/test split. |
    |                              | A. Reuse the cached split: ...      |
    | NEW (3)                      | B. Regenerate: ...                  |
    |   done   paper-figs  1h      | I recommend B.                      |
    |   slot   rerun-nov/findings  |                                     |
    |   tell   site: deploy ok     | [ answer...                       ] |
    |                              | (Send) (Take recommendation)        |
    | RUNS                         | (Open session) (Copy id)            |
    | v example-proj    1 ask 2 new|                                     |
    |   v rerun-nov  waiting 1 ask |                                     |
    |       status: Seeds 3/5 ...  |                                     |
    |     > eval-lead  active 10m  |                                     |
    |   > paper-figs  finished     |                                     |
    | > site       active 5h 1 ask |                                     |
    | [show all] [search]          |                                     |
    +------------------------------+-------------------------------------+

Narrow layout (phone): the left column alone; tapping a row opens the right
pane as its own page. Design for the phone first.

Actions per item:

- Question: answer (free text), "take recommendation" or "take default"
  (answers with a fixed sentence: "Take your recommendation." or "Go ahead
  with your default: <default>"), open session, copy id.
- Notice, done report, slot: mark read; open the linked report or file.
- Node (project, run, agent): mark everything under it read; expand.
- Global: show all, search, refresh.

Terminal rendering (`agent-desk inbox`): `fzf` over the list with the
preview pane, fed and called back through hidden subcommands (`inbox-list`,
`inbox-preview`, `inbox-act`) that share a state file for "show all" and
folding. Keys: Enter answers a question (one line, or `$EDITOR` when left
empty), opens an item in `$EDITOR`, or folds a node; `ctrl-r` marks read;
`ctrl-t` takes the recommendation or default; `ctrl-o` opens the session;
`alt-a` toggles show all; `ctrl-l` refreshes. The list reloads after each
action. It refuses to start without a terminal and points to `show`.

"Open session" uses the reply address: a Paseo agent opens in Paseo
(`paseo agent open <uuid>`), a live tmux pane on this host is selected,
otherwise the resume command (`cd <dir> && claude --resume <id>`, `codex
resume <id>` or `agy --conversation <id>`, with the seat's config variable) is
copied to the clipboard.

## Chat and ledger together

The user wants each question both in chat and filed, and decided against
hooks that file on the agent's behalf (2026-11-15). So:

- The agent files with `agent-desk ask`, `tell` and `set`, whenever it wants
  during its work. Filing is always an explicit command.
- The agent is free to raise the same matter in chat in whatever form suits:
  prose, a restatement, or an interactive question tool such as Claude Code's
  `AskUserQuestion`. The tool neither inspects nor constrains the chat.
- The chat mention carries the display id (`q-7f3a`), so the two can be
  matched by eye and by search.
- When the user answers in chat or through a question tool, the agent records
  it: `agent-desk answer q-7f3a --from-chat 'B, keep the old cache'`. This
  costs one short command and is the only thing keeping the ledger's open list
  honest, so the skill must state it plainly. An answer the agent forgot to
  record leaves the question visibly open in the inbox, where the user can
  close it.

No hook files questions, notices or slots; do not add one. The single
approved hook, deferred to phase 8, copies the user's own messages.

## Answer delivery

`agent-desk answer <id> 'text'` appends the `answer` event, then calls
`deliver`, which sends this prompt to the reply address and appends a
`delivered` event:

    Answer to q-7f3a "Which split for the rerun?": B, and keep the old cache.

- Paseo agent: `paseo send --no-wait <uuid> ...` (it revives an archived
  agent).
- tmux pane: the text through `tmux load-buffer` and `paste-buffer -p`
  (bracketed paste, so a multi-line answer stays one message), then Enter,
  after checking that the pane still runs the recorded session: its
  `pane_pid` is the one recorded at asking time, and the `@agent_session`
  identity the agents' tmux hooks leave on its session, if any, names it.
- Anything else: `deliver_command`. In the author's setup that is the typing
  path behind `h-agent-usage-continue-send` in
  `zshlang/auto-load/others/agent-usage.zsh`.
- Unreachable session: the answer stays recorded and undelivered; print how
  to resume the session. A later `deliver` retries, and exits nonzero while
  any answer stays undelivered.
- An answer recorded with `--from-chat` already reached its session and is
  never delivered.
- Deliveries take a lock in the store directory, so two runs never type one
  answer twice.

**Replies** (added 2026-11-16 at the user's request, so they can steer agents
from the dashboard): `agent-desk reply <id|path> 'text'` appends a `reply`
event about an item, or to a node's coordinator, recording where it goes
(`meta.to`, `meta.to_host`), and delivers it the same way:

    Message from the user via agent-desk, about t-k2m9 "Deploy finished": check the old feed too

A reply about an item goes to the session that wrote it, unless the node has
been taken over since, in which case it goes to the new coordinator. Replies
are delivered on the session's own machine, after a merge if need be. The
skill tells agents that a message arriving "via agent-desk" comes from a user
reading the desk, who may not see the chat, so the answer goes back with
`tell`.

A blocked agent ends its turn after asking and never polls. For a question
with a default, the agent may be mid-turn when the answer arrives. `paseo
send` with its default behaviour (`steer`) queues the message and hands it to
a mid-turn Claude agent without disturbing its running tool call, so deliver
sends at once; it must never pass `--behavior interrupt`. Codex agents behave
the same.

## Notifications

Core: on every `ask`, every `tell --notify` and every `done`, run
`notify_command`; on an answer or a `read`, run `dismiss_command` for the
question or item it closes.

- Each command runs under `sh -c`, detached, so a slow notifier never holds
  up an agent's write; its output goes to `~/.agents/desk/hooks.log`.
- Stdin is one JSON object: `action` (`notify`, `dismiss`, `open`,
  `deliver`), `display_id`, `path` (the node), `event` (the item notified or
  dismissed), `cause` (the answer or read that dismisses), `local` (whether
  the item was written on this host), `agent_desk` (this binary's path, for
  click actions) and `desk_home`.
- `agent-desk open <id|path>` is what a click runs: it hands the item to
  `open_command`, or without one opens the inbox scoped to the item's node.
- Imported and merged events notify only with `--notify`; the dismissals they
  call for always run, so a question answered on another machine loses its
  card here after the next merge.
- The config file is `${AGENT_DESK_CONFIG:-~/.config/agent-desk/config.toml}`;
  an unknown key is an error, so a typo does not silently disable a command.

The author's integration, in `~/scripts`:

- A new app "Agent Desk" via `macos/notification-apps/build.py` and
  `notif-app-install desk`, like Agent Done.
- One notification group per event id, so one question never replaces
  another. Dismissal uses `notif-os-remove`.
- Card: title is the item title; body is the path and level, such as
  "example-proj/rerun-nov · blocked". Click runs `open_command`.
- A `blocked` question while the machine is idle also goes to Telegram via
  `tnotif`, title only. Reuse the idle check in `agent-usage.zsh`.
- `agent-done` appends a `done` event with the summary and report path, sets
  the node's `status` slot to "Finished; see report.", and mentions open
  `blocked` questions in the report. It refuses nothing and removes nothing.
  It runs `agent-desk done` (the report path in `meta.report`) and, when the
  desk accepts it and has a `notify_command`, skips its own Agent Done card: the desk's done card is the
  one notification, and clicking it opens the report. A worker, which the
  desk refuses, keeps the Agent Done card.

## Phases

One atomic commit group each. Stop after each phase and run its check.

1. **Store and write commands.** Module, schema, triggers, views, identity,
   `join`, `ask`, `tell`, `answer` (record only), `set`, `clear`, `link`,
   `read`, `show`, `search`, `export`, `import`, `snapshot`, `merge <file>`,
   `render`. Hermetic Go tests against a temporary store and a faked identity.
   Check: two processes writing in a loop lose nothing; `UPDATE` is rejected;
   an over-cap `set` writes nothing; a worker session is refused; merging two
   diverged stores in either order gives identical views. Doc: `README.md`
   and `docs/usage.md`.
2. **Terminal dashboard.** `inbox` with the four views, the hierarchy
   behaviour and every action except delivery. Check by hand against
   fabricated events, including a project with nested coordinators.
3. **Notifications.** The two commands in core; the app in `~/scripts`.
4. **Answer delivery**, with `deliver` and `merge ssh:`. Test against a
   disposable Paseo agent and a tmux pane running `cat`.
5. **`agent-done` integration**, in `~/scripts`.
6. **Skill.** `skills/agent-desk/SKILL.md`, covering: join, when to file,
   the id in chat, recording chat answers, slot discipline (state what is
   true now; no history; no progress narration), and the worker rule. Then
   one line in the author's global agent instructions saying the skill must
   be used when writing anything meant for the user. That file is
   `PE/Agents/AGENTS.md` in `~/scripts`; ask the user before editing it,
   commit it alone, and run `agents-md-sync`.
7. **HTML app.** `agent-desk serve` on loopback: a JSON API and one page
   with no build step, assets embedded in the binary and referenced with a
   content hash in the URL. Phone layout first. Each machine serves its own
   store; exposing the port on a private network is deployment and belongs
   to the machine's own configuration, not to this repository. Write actions
   (answer, mark read) need protection against cross-site requests even on a
   private network.
   Every view is a URL, so any view or filter can be bookmarked (asked by the
   user, 2026-11-16): a project or any subtree by path, one item by display
   id, and filters such as one session, one host, show all and a search as
   query parameters.
   Built (2026-11-16) as a Svelte 5 page compiled by Vite. The user left the
   framework to the builder and asked for a polished dashboard that can also
   message the agent behind an item ("reply"); agents run a Node build
   without trouble, so a framework's cost is small. The site this is
   deployed on strips its mount path before proxying, so the page uses hash
   routes (`#/p/<path>?session=&host=&all=1&q=&item=`) and relative,
   content-hashed asset URLs. `web/dist` is committed so `go install` needs
   no Node, and a Go test fails when it is older than `web/app`. Writes need
   an `X-Agent-Desk` header and a same-origin JSON request; a TCP listener
   also checks the `Host` header against DNS rebinding. `serve --listen`
   takes `HOST:PORT` or `unix:PATH`; `GET /api/health` answers `ok`.
8. **Later, each needing the user's go-ahead:**
   - Emacs commands on rendered headings.
   - An `instruction` event kind, written by a prompt hook that copies each
     message the user sends to a coordinator, verbatim, replacing the
     hand-kept verbatim file. The user approved this hook (it records the
     user's text, not the agent's). It must skip automated prompts: resume
     pings, cache-warming pings, compaction commands and prompts sent by
     other agents.
   - Run state as slots with an agent-only audience.
   - Tasks as events.
   - A Paseo plugin.

## To verify early

- Whether a sandboxed Codex session may write to the store directory; if not,
  the skill must say how to allow it. Checked 2026-11-15 with Codex 0.169: it
  can neither write nor read it (SQLite in WAL mode writes its `-shm` file
  even to read), from a Paseo agent in auto-review mode and under
  `codex sandbox -P :workspace`. Naming the store in
  `sandbox_workspace_write.writable_roots` fixes it, as does running the
  command with escalated permissions. Under `CODEX_SANDBOX` the open error
  says both; the skill and `docs/usage.md` say both.
  Antigravity (checked 2026-11-16, `agy acp` with its OS sandbox): the same
  failure, and its sandbox has no writable-path setting, so the route is a
  sandbox bypass per command; the open error says so under agy.
- An in-process subagent shares its parent's session and so writes as the
  parent; the skill tells it not to. Done.
