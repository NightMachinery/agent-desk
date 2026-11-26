---
name: agent-desk
description: Report to the user through the agent-desk command (questions, notices, deliverables, done reports, current-state slots and the task checklist) and respond to the messages they send from the desk, so nothing meant for them is lost in a transcript. Use whenever you write anything meant for the user, when a prompt arrives "from the user via agent-desk", and when you start coordinating a run. Not for messages between agents.
---

# agent-desk

`agent-desk` is a store on this machine that the user reads in one place: a
dashboard (often on a phone), a terminal inbox and notifications. Chat stays
as it is; the desk is the record the user finds again after the transcript
has scrolled away.

Every write prints a display id whose letter gives its kind: `q` question,
`t` notice, `b` deliverable, `d` done report, `w` task, `s` slot. The user's
messages are `m-...`, their answers `a-...`.

**In short:**

1. A coordinator joins a node once; workers never write.
2. Every desk message from the user gets a desk response: run the command
   on its last line.
3. A decision you need is an `ask`, with `--blocked` or `--default`.
4. Your checklist is desk tasks: `task add --brief`, `task start`,
   `task done --note`.
5. Results go in `tell`, things to open in `deliverable`, the end in `done`.
   Keep the `status` and `next` slots true.

## Who writes

Only a **coordinator** writes: a session that talks to the user and owns a
piece of work. Join a node once, at the start:

    agent-desk join rerun-nov/coord              # the run's top coordinator
    agent-desk join rerun-nov/coord/eval-lead    # nested, under the coordinator that started you
    agent-desk join rerun-nov/coord --takeover   # replacing a coordinator that is gone
    agent-desk takeover <paseo-id|session-id>    # replacing an agent: every node it held

The path is `<run>/<role>[/<role>...]`: a short run slug, then role slugs
(not agent ids), in the project of the repository you are in (`--project`
overrides). A session working alone with the user may skip `join`; its first
write joins `<project>/adhoc/<slug>`.

**Separate work gets its own node.** When the user starts you on unrelated
work while you hold a run, `join` a node for it too, so it shows where they
will look for it. While you hold several nodes, add `--node` to every write
to say which one it is for (`set status '...' --node ablation-dec/coord`);
`run/role` or the full path both work. A `--re` response needs no `--node`:
its message already belongs to a node. `leave` a node after its `done`.

**Workers never write.** If another agent started you (a Paseo child, a tmux
subagent), report to it; `agent-desk` refuses you anyway. An in-process
subagent (Claude Code's Agent tool) shares its parent's session and would
file as the parent, so never run `agent-desk` from one.

## How to write

The user reads the desk cold, between other things, often on a phone. Text
renders as Markdown.

- Lead with the point: the decision needed, the result, the state. Then
  only the detail needed to act on it.
- Plain sentences, no filler or hedging. Name a term the first time you use it.
- Short paragraphs. Lists for options, results and steps. **Bold** the
  recommendation or the number that matters; `code` for paths, commands
  and ids. No tables: they do not fit a phone.
- Concise beats complete: link a file for the long version.

## Messages from the user

A prompt that starts `Message m-4c2d from the user via agent-desk` comes from
a user who may not be watching your chat. Act on it, then **always respond on
the desk**, even with one line, with the command on its last line:

    agent-desk tell --re m-4c2d <<'R'
    Done; the old feed is fine.
    R

- For a message about a task (one the user filed, a change they made, a
  status request), that last line names the task (`tell --re w-8f2a`), so
  the response sits on the task.
- Respond even when you also answered in chat. Add `--quiet` only to a bare
  acknowledgement.
- Several messages before you respond: answer each, or the last one saying
  it covers the others. `agent-desk show --mine` lists those still waiting.
- If you need a decision before you can act, the question is your response:
  `agent-desk ask --re m-4c2d --blocked --title 'Which feed?'`. Never file an
  unlinked `ask` about a desk message.
- If it asked you to ship something, respond with the deliverable itself
  (`deliverable --re m-4c2d`).
- Files the user attached arrive as paths after the text; open them like any
  file.

**Answers.** One given on the desk arrives as `Answer to q-7f3a "Which
split?": B`. It is already recorded: act on it. One given in chat (typed, or
through a question tool) must be recorded, or the question stays open in the
user's inbox:

    agent-desk answer q-7f3a --from-chat 'B, but keep the old cache'

**Chat questions.** When the user asks something in chat and your answer has
substance (a finding, an explanation, where something stands), file it too,
with their question in one line. Skip quick back-and-forth.

    agent-desk tell --re-chat 'Why is seed 2 slow?' <<'R'
    Seed 2 drew shard 17, which has **duplicated rows**. Re-running without it.
    R

## Questions

File a decision you need, and raise it in chat as well, with its id:

    agent-desk ask --blocked --title 'Which split for the rerun?' <<'Q'
    The rerun needs a train/test split, and the cached one predates the leakage fix.

    - **A. Reuse the cached split.** Fast, but not comparable with runs after the fix.
    - **B. Regenerate it.** An hour longer; consistent with the fix.

    **I recommend B**: the rerun exists to compare against post-fix runs.
    Q

- Give exactly one of `--blocked` (you stop until answered) or
  `--default 'B: regenerate'` (you go ahead with that unless told otherwise;
  say so in chat).
- The title is the question itself, readable in a notification. The body
  lets the user answer cold: the context, every option with its tradeoffs,
  your recommendation. Never point at earlier chat.
- One question per `ask`; never refile one still open (`show --mine`).

**Several decisions at once** go in one `ask` with `--choices FILE`, up to 30
questions of 2 to 12 options each:

    {"questions": [
      {"text": "Which split?", "options": ["Reuse the cached split",
        {"key": "B", "label": "Regenerate", "detail": "an hour longer"}],
       "recommended": "B"},
      {"text": "Which seeds rerun?", "options": ["1-3", "4-5", "all"], "multi": true}]}

An option is a label or `{key, label, detail}` (keys default to A, B, C);
`multi` allows several picks. The answer is one line per question, then any
general message. Act on each line; "(no answer)" means that question is
still open:

    1. B: Regenerate. Note: keep the old cache
    2. Other: only seed 7
    3. (no answer)

    And rerun the sync tonight.

## Notices and files

A notice is for what the user should know but need not answer: a result, a
milestone, a problem you worked around.

    agent-desk tell --title 'Seeds 1-5 rerun: the fix holds' <<'R'
    Accuracy is **within 0.3 points** of the old run on all five seeds.
    Table: `results/rerun-nov.org`
    R

Add `--notify` only when it is worth interrupting the user.

`ask`, `tell`, `done` and `deliverable` take up to 8 files (a plot, a
screenshot, a recording, a PDF), each followed by a `--caption` that says
what it shows and why it matters:

    agent-desk tell --title 'Seed 2 diverges' \
      --attach plots/loss.png --caption 'Seed 2 (orange) leaves the others at step 400' <<'R'
    ...
    R

Keep the point in the text too: files are deleted a month after the item is
filed, once it is read. Limits: images 10 MB, audio 50 MB, video 100 MB, app packages
(APK) 256 MB, anything else 25 MB.

## Deliverables

When something is ready to open or install (a deployment, an APK, a release,
a published page), file a **deliverable**, not a notice. It gets its own
section with an Open or Download button:

    agent-desk deliverable --title 'Android APK 1.4.2' --key android-apk \
      --attach build/app-release.apk --caption 'Release build, debug-signed' <<'D'
    Install it over 1.4.1; your data stays. New: dark mode in Settings.
    Checked on the emulator (API 35): login, sync and the new switch.
    D

- Give `--url`, `--attach`, or both; with neither it is a `tell`. `--url`
  is a web address or an app link the user's machine handles, such as
  `myapp://open?path=~/x.pdf&page=3`; the dashboard's Open button hands an
  app link to its app.
- The body says what it is, how to try it, and what you checked.
- `--key` names what it is a version of. A newer deliverable with the same
  key replaces the older one, so the user never installs a stale build.
- The user may answer **Works** or describe a problem; either arrives as a
  message to respond to.

## Tasks

Your checklist lives on the desk: the user sees it, filters it and checks it
off on the Tasks page. Keep no second list by hand; if your instructions ask
for `TASKS.org`, write it with `agent-desk task list --org --all > TASKS.org`.

    agent-desk task add --title 'Ship 1.4' --brief 'Release the loader fix and the new plots; users on 1.3 lose runs.'
    agent-desk task add --title 'Login fails on Android 15' --type bug --parent w-3k9d --tag android
    agent-desk task start w-8f2a
    agent-desk task done w-8f2a --note 'Table in results.md, seeds 1-5; checked against the logs.'
    agent-desk task drop w-8f2a --note 'Superseded by w-9c1e.'
    agent-desk task list [--all] [--type bug] [--tag android]

- `--brief` is what and why in one to three sentences, shown under the
  title. Every task needs one except a step (`--parent`).
- `--type` is `task` (the default), `bug`, `feature`, `idea` or `chore`.
  `--tag` adds free tags; `task tag|untag <id> TAG...` changes them later.
  `--priority` is 1 to 3, `--size` is S, M or L.
- Start a task when you begin it. Finish or drop it with a `--note`
  (`--note -` reads stdin): how it was done and checked, or why not. The
  user reads it on the task.
- **Tasks the user files** arrive as a message naming the task: respond on
  the task and track it like your own. If it has several parts, first file
  each as a step under it (`task add --parent <id>`). If it has no title,
  name it: `agent-desk task title <id> 'Short title'`.
- **Changes the user makes** to your tasks (done, dropped with a reason,
  reopened, deferred) arrive as messages that say what to do.

**Not now.** `task defer <id>` parks a task until someone undefers or starts
it: it waits on a decision, or on other work. Work worth doing that the quota
does not allow now (a cleanup, a deeper check) is deferred **for quota**
instead of dropped. The user launches it from the dashboard when quota is
spare, so its brief must let a fresh agent act alone: what, why, where, how
to check.

    agent-desk task add --quota --title 'Profile the loader' --size M --priority 2 <<'B'
    Epoch 1 takes 40 min, later epochs 6. Find out why in loader.py ...
    B

`task defer --quota <id>` does the same for a task already filed. `task add
--defer` files a task parked. With `--quota` or `--defer`, `task add` reads
the brief from stdin; otherwise give `--brief TEXT` (or `--brief -`).

## Slots: what is true now

A slot is a named box at your node whose text you replace; the board shows
the current text.

    agent-desk set status 'Seeds 3/5 done. ETA 14:00.'
    agent-desk set next $'Finish seeds 4 and 5.\nWrite the table.'   # $'...' for line breaks
    agent-desk set findings < findings.txt
    agent-desk clear next

On anything longer than a few turns, keep the standard slots so every node
reads the same:

- `status` (2 lines): what you are doing now.
- `next` (5 lines): your next steps, in order.
- `done` (15 lines): results so far, one line each with where it lives (a
  commit, a path), newest first.
- `velocity` (15 lines), if you coordinate workers or a run longer than a
  session: recent pace, where tokens went (rework, polling, idle waits,
  oversized contexts, review fan-outs), and one to three suggestions. Base
  it on what you observed, write "unknown" rather than guess, and update it
  at milestones.

Add others as they help (`findings`, `artifacts`, any slug; 15 lines). Write
what is true now, for someone arriving fresh: no diary. Update a slot when
its truth changes. A text over the limit is refused: shorten it.

**Notes for agents.** Run state you keep for yourself (decisions and their
reasons, live workers and jobs, result paths, traps) can be a note only
agents see. It never shows as unread and never notifies the user.

    agent-desk set --for agents state < STATE.md   # up to 400 lines, 64 KiB
    agent-desk get state                            # read it back after compaction
    agent-desk get state --node <path>              # another agent's

## Ending

`agent-done`, where installed, files your done report and sets `status`.
Otherwise:

    agent-desk done --title 'Rerun finished' --report results/rerun-nov.org <<'S'
    All five seeds rerun; the fix holds. Two questions remain open: q-7f3a, q-b2c4.
    S

## The screen

Before anything that clicks, types into another app or takes focus on the
user's machine, take the screen lock (any agent, workers too), and release it
the moment you are done:

    agent-desk screen take --reason 'Clicking through the settings app' [--for 20m] [--idle 5m] [--wait]
    agent-desk screen release

It is yours at once if the user has been idle 30 minutes. Otherwise they get
a request and `take` exits 3; `--wait` blocks until they grant it or stay
away 30 minutes. `--idle 5m` shortens that wait (at least `1m`); pass it only
when the user asked for it. A banner asks them to keep off the keyboard while you hold
it. Headless work, such as a headless browser's screenshots, needs no lock.
If they deny it, do not ask again at once; ask what to do instead. An
unanswered request lapses after 2 hours (or your longer `--wait --timeout`):
take again if you still need the screen.

## While you wait: the prompt cache

Inside Paseo, when you expect to be woken within a few hours (a child
finishing, the user replying, CI), run `agent-desk cache warm`
(`--warm-hours N` when six hours does not fit). When you are done, or blocked
on something unlikely to clear soon, run `agent-desk standby` to stop warming
and compact instead. Both need agent-quotas running.

## Under a sandbox

The store (`~/.agents/desk`) is outside your workspace. In a sandboxed Codex
session the command fails and names the sandbox: rerun it with escalated
permissions. The user can fix this for good by adding the store to
`sandbox_workspace_write.writable_roots` in Codex's `config.toml`. Under
Antigravity, run `agent-desk` with a request to bypass the sandbox.

## Settings are the user's

`agent-desk pref` refuses agents. If the user asks you to change where
notifications go or how messages reach a busy agent, give them the command
(`agent-desk pref set notify.remote always --scope example-proj`) or point
them to the dashboard's Notify chip.

## Looking things up

    agent-desk whoami                       # your nodes, or exit 1 if none
    agent-desk show --mine                  # your node: open questions, slots, messages awaiting you
    agent-desk show rerun-nov               # one run
    agent-desk render --print item w-8f2a   # one item and its thread, as org
    agent-desk search 'leakage'
    agent-desk instructions                 # the user's messages to your run, word for word

Where the prompt hook is installed, every message the user types to a
session that has joined a node is kept word for word; automated prompts are
skipped. After compaction, read `agent-desk instructions` before acting: it
is the user's own text, not a summary. If it prints `(no instructions
recorded ...)`, the hook is missing; keep the user's words in your own file.
