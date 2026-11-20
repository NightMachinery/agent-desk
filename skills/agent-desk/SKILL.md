---
name: agent-desk
description: File questions, notices, done reports and current-state slots for the user with the agent-desk command, so nothing meant for them is lost in a transcript. Use whenever you write anything meant for the user, such as a question, a decision you need, a finding or a report, and when you start coordinating a run. Not for messages between agents.
---

# agent-desk

`agent-desk` is a store on this machine that the user reads from one place: a
terminal inbox, notifications, and answers typed back into the session that
asked. Chat stays as it is. The desk is the record the user can find again
after the transcript has scrolled away.

Every write prints one display id, such as `q-7f3a`. The letter gives the kind
(`q` question, `t` notice, `d` done report, `s` slot text).

## Who writes

Only a **coordinator** writes: a session that talks to the user and owns a
piece of work. Join a node once, at the start:

    agent-desk join rerun-nov/coord              # the run's top coordinator
    agent-desk join rerun-nov/coord/eval-lead    # nested, under the coordinator that started you
    agent-desk join rerun-nov/coord --takeover   # replacing a coordinator that is gone
    agent-desk takeover <paseo-id|session-id>    # replacing an agent: all the nodes it held

The path is `<run>/<role>[/<role>...]`: a short run slug, then role slugs (not
agent ids). The project is the repository you are in (`--project` to
override). A session working alone with the user can skip `join`; its first
write joins it to `<project>/adhoc/<slug>`.

**A second, unrelated run in the same session gets its own node.** If the user
starts you on separate work while you still coordinate a run, `join` a node for
it (`agent-desk join ablation-dec/coord`); do not file it under the first run,
where the user will not look for it. Join adds a node: you now hold both. From
then on every write needs `--node` saying which run it is for:

    agent-desk ask --blocked --title T --node ablation-dec/coord <<'Q' ... Q
    agent-desk set status 'seeds 3/5' --node rerun-nov/coord
    agent-desk done --title 'Ablation finished' --node ablation-dec/coord
    agent-desk leave ablation-dec/coord    # finished with it: back to one node

`--node` takes the path relative to the project (`run/role`) or in full, and
only nodes you hold. `tell --re` and `ask --re` need none: the message names its node. A write without it is refused with the list of your
nodes; never guess, and leave a node once you have finished its work (after
its `done`) so single-node writes work again. Workers still never write: a
nested node is yours only if you joined it.

**Workers do not write.** If another agent started you (a Paseo child, a tmux
subagent), report to that agent; `agent-desk` refuses you anyway. Your parent
can give you a node with `join` if the work calls for it. An in-process
subagent, such as one from Claude Code's Agent tool, shares its parent's
session, so the desk cannot tell it apart and would file as the parent: never
run `agent-desk` from one.

## How to write

The desk is for the user's executive reading: often on a phone, often cold,
between other things. Bodies, slots and reports render as Markdown.

- **Lead with the point:** the decision needed, the result, the state. Detail
  comes after, and only as much as the reader needs to act.
- Write the way a sharp colleague would say it: plain sentences, no filler,
  no hedging, no unexplained jargon. Name a term the first time you use it.
- Short paragraphs, separated by a blank line. Bullet lists for options,
  results and steps. **Bold** the recommendation or the number that matters;
  `code` for paths, commands and ids. Headings only in long reports.
- No tables: they do not fit a phone. Use a list.
- Concise beats complete. Cut what the user would not miss, and link to a
  file for the long version.

## Questions

When you need a decision, file it, and raise it in chat as you normally would,
with the id in the chat text:

    agent-desk ask --blocked --title 'Which split for the rerun?' <<'Q'
    The rerun needs a train/test split, and the cached one predates the
    leakage fix.

    - **A. Reuse the cached split.** Fast, but not comparable with runs after the fix.
    - **B. Regenerate it.** An hour longer; consistent with the fix.

    **I recommend B**: the rerun exists to compare against post-fix runs.
    Q

- The title is the question itself, readable on a phone notification.
- The body is everything needed to answer cold: the context, every option
  with its tradeoffs, and your recommendation. Do not refer to earlier chat.
- `--blocked`: you stop until answered. `--default 'B: regenerate'`: you go
  ahead with that choice unless told otherwise; say so in chat too.
- One question per `ask`. Do not refile a question that is still open; check
  with `agent-desk show --mine`.
- When several decisions are needed at once, file one `ask` with
  `--choices FILE` instead of several asks (see below).

**Choices.** `--choices FILE` adds multiple-choice
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
the picks. The answer arrives as text, one line per question, then the general message if any. Act on each line; a question marked "(no answer)" is still open, so ask it again later if you need it:

    1. B: Regenerate. Note: keep the old cache
    2. Other: only seed 7
    3. (no answer)

    And rerun the sync tonight.


**Record every answer you receive in chat** (typed reply or a question tool),
or the question stays open in the user's inbox:

    agent-desk answer q-7f3a --from-chat 'B, but keep the old cache around'

An answer given in the inbox arrives typed into your session as
`Answer to q-7f3a "Which split for the rerun?": B`. It is already recorded;
just act on it.

A message that starts `Message m-4c2d from the user via agent-desk` was sent
from the desk by a user who may not be watching your chat. Act on it, then
**always respond on the desk**, even with one line:

    agent-desk tell --re m-4c2d <<'R'
    Done; the old feed is fine.
    R

Respond even when you also answered in chat ("Will do after the current
seed." is enough). Add `--quiet` only for a bare acknowledgement. Without
`--title` the response is titled "Re: " and the title of the item the
message was about. Several messages before you respond are each their own
id: respond to each, or to the last one saying it covers the others.
`agent-desk show --mine` lists messages still awaiting your response. Only
the session at the message's node can respond to it; a coordinator that took
the node over may respond to its predecessor's messages.

If you cannot act on the message without a decision from the user, respond
with the question itself rather than a `tell` followed by a separate `ask`:

    agent-desk ask --re m-4c2d --blocked --title 'Which feed?' <<'Q'
    The message says "the old feed", and there are two: **v1** (retired in
    May) and **v2-legacy**. Which one?
    Q

That one question is your response: it marks the message responded, sits in
the message's thread, and is answered like any question (`--default` when
you can go ahead with a stated plan, `--choices` for options). Never file an
unlinked `ask` about a desk message. The same works for a chat question with
`ask --re-chat '...'`.

When the user asks you something **in chat** and your answer has substance
(a finding, an explanation, a decision, where something stands), file the
answer too, so it is not lost in the transcript:

    agent-desk tell --re-chat 'Why is seed 2 slow?' <<'R'
    Seed 2 drew shard 17, which has **duplicated rows**. Re-running without it.
    R

Quote or paraphrase the question in one line. Skip this for quick
back-and-forth and clarifications. Responses get their own section on the
desk, above other notices, and notify the user unless `--quiet`.

Any kind of item can be the response: `tell`, `ask`, `done` and
`deliverable` all take `--re` and `--re-chat`. When the message asked you
to ship something, respond with the deliverable itself, not a `tell` that
points at it.

## Notices

For something the user should know but need not answer: a result, a finished
milestone, a problem you worked around, a report written to a file.

    agent-desk tell --title 'Seeds 1-5 rerun: the fix holds' <<'R'
    Accuracy is **within 0.3 points** of the old run on all five seeds.

    Table: `results/rerun-nov.org`
    R

Add `--notify` only when it is worth interrupting the user for.

**Show, when a picture says it faster.** `ask`, `tell` and `done` take any
file (a plot, a screenshot, a voice note, a recording, a PDF), up to 8 per
item:

    agent-desk tell --title 'Seed 2 diverges' \
      --attach plots/loss.png --caption 'Seed 2 (orange) leaves the others at step 400' <<'R'
    ...
    R

Every `--attach` needs a `--caption` after it that says what the file shows
and why it matters: the user reads it first, and a terminal shows only it.
Keep the point in the text too, since old attachments are deleted after about
a month. Images up to 10 MB, audio 50 MB, video 100 MB, other files 25 MB;
the desk keeps its own copy.

Messages and answers from the user may carry files too. They arrive as paths
listed after the text ("The user attached these files; read them from these
paths:"); open them like any file.

## Deliverables

When something is ready for the user to open or install (a deployment, an
Android APK, a release, a published page), file a **deliverable**, not a
notice. The desk lists deliverables in their own section above responses,
with a large Open or Download button:

    agent-desk deliverable --title 'Android APK 1.4.2' --key android-apk \
      --attach build/app-release.apk --caption 'Release build, debug-signed' <<'D'
    Install it over 1.4.1; your data stays. New: dark mode in Settings.
    Checked on the emulator (API 35): login, sync and the new switch.
    D
    agent-desk deliverable --title 'Site on staging' --url https://staging.example.org/ <<'D'
    ...
    D

- Give `--url` (http or https), `--attach FILE --caption '...'`, or both.
  Nothing to open or download means it is a `tell`.
- The body says what it is, how to try it, and what you verified.
- `--key` names what this is a version of (`android-apk`, `staging-site`).
  A newer deliverable with the same key in the project replaces the older
  one on the desk, so the user never installs a stale build. Reuse the key
  for every new version.
- An APK may be up to 150 MB.
- The user may answer **Works** (you get "Works.") or describe a problem;
  both arrive as a message to respond to.

## Slots: what is true now

A slot is a named box at your node whose text you replace. The board shows the
current text; old text is kept but out of sight.

    agent-desk set status 'Seeds 3/5 done. ETA 14:00.'
    agent-desk set findings < findings.txt
    agent-desk set next $'Finish seeds 4 and 5.\nWrite the table.'   # $'...' for line breaks
    agent-desk clear next
    agent-desk link tasks TASKS.org     # older runs; new ones keep desk tasks (below)

On any task longer than a few turns, keep these standard slots, so every node
reads the same:

- `status`: what you are doing now (2 lines).
- `next`: your next steps, in order (5 lines).
- `done`: what is finished so far, one line per result, with where it lives (a
  commit, a path); newest first, older lines merged as it fills (15 lines).
- `velocity`, if you coordinate (you launch workers, or the run spans more than
  a session): recent pace (what got done per hour or per phase), token and
  quota efficiency (where tokens went: rework, polling, idle waits, oversized
  contexts, review fan-outs) and one to three concrete suggestions to improve
  either (15 lines). Base it on what you observed, such as commits, quota
  readings and context sizes; say "unknown" rather than guess. Update it at
  milestones, not every turn.

Then add others as they help: `findings`, `artifacts`, or any slug.

- Write what is true now, as if for someone arriving fresh. No history ("then
  I tried..."), no diary, no progress narration step by step. `done` is a
  summary of results, not a log.
- Update a slot when its truth changes, not on a timer.
- `status` holds 2 lines, `next` 5, other slots 15. A longer text is refused;
  shorten it rather than splitting it across slots.
- `tasks`, `user-verbatim` and `state` only take `link`, to the file that
  already holds them.

### Notes for agents only

Run state you keep for yourself (what a STATE.md holds: decisions and their
reasons, live workers and jobs, result paths, traps you hit) can live on the
desk as a note only agents see:

    agent-desk set --for agents state < STATE.md   # up to 400 lines, 64 KiB
    agent-desk get state                            # read it back after compaction

A note is never unread and never notifies; the user's views list it folded,
by name. It is replaced whole on each `set`, like any slot, so write what is
true now. `state` may be set this way; any other slug works too. Other agents
read yours with `agent-desk get <name> --node <path>`.

## Tasks, and work deferred for quota

Your checklist lives on the desk, not in a file: the user sees it on the
Tasks page, filters it by type and checks things off there.

    agent-desk task add --title 'Ship 1.4' [--priority 1] [--brief 'one line']
    agent-desk task add --title 'Login fails on Android 15' --type bug --parent w-3k9d
    agent-desk task start w-8f2a ; agent-desk task done w-8f2a [--note 'in results.md']
    agent-desk task list [--all] [--type bug]
    agent-desk task list --org          # the checklist as org: * node, - [ ] items

- `--type` is `task` (default), `bug`, `feature`, `idea` or `chore`.
- `--parent` makes it a step of another task (a milestone and its items).
- Start a task when you begin it and mark it done when it is, so the list
  says what is true now. Drop what you decided not to do.
- If your instructions also ask for a `TASKS.org` file, write it from the
  desk (`agent-desk task list --org --all > TASKS.org`) instead of keeping
  two lists by hand.
- The user files tasks too, from the dashboard. When they do, you get a
  message naming the task id: respond to it, and track the task like your
  own.

When you find work worth doing that the quota does not allow now (a cleanup,
a deeper check, a nice-to-have), defer it instead of dropping it, with a
brief that a fresh agent can act on alone: what, why, where, how to check it.

    agent-desk task add --defer --title 'Profile the loader' --size M --priority 2 <<'B'
    Epoch 1 takes 40 min, later epochs 6. Find out why in loader.py ...
    B

The user launches deferred tasks from the dashboard when quota is plentiful,
and the desk reminds them when a weekly window is about to reset unused. An
agent launched on one gets a prompt telling it to join the desk and run
`agent-desk task done <id>` when finished. `--defer` reads the brief from
stdin; otherwise pass `--brief TEXT` (or `--brief -` for stdin): `task add`
never reads stdin unasked, so it cannot hang on a held-open stdin.

## The screen: before you drive the GUI

Take the screen lock before anything that clicks, types into another app or
takes focus on the user's machine (any agent may, a worker too), and release
it the moment you are done:

    agent-desk screen take --reason 'Clicking through the settings app' [--for 20m] [--wait]
    agent-desk screen release

It is yours at once when the user has been idle 30 minutes (your own
clicks while you held it do not count as theirs). Otherwise the user gets a
request in Needs you, and `take` exits 3; `--wait` blocks until they approve
or until the desk grants it because they stayed away 30 minutes (the answer
also reaches you as a message: run `take` again).
While you hold it a banner tells the user to keep off the keyboard. Do not
take it for work that runs headless, in the background or without focus,
such as a headless browser's screenshots: that does not disturb the user.
If the user denies it, do not ask again at once; ask them what to do instead.

## Paseo prompt cache

When you expect to be woken within a few hours, for example after a child
finishes, the user replies, or CI reports, run `agent-desk cache warm`. Use
`--warm-hours N` when the default six-hour idle window does not fit. When
you are done or are blocked on something unlikely to clear within six
hours, run `agent-desk standby` to end warming and switch to compaction.
These commands use your Paseo agent and need agent-quotas running locally.

## Ending

`agent-done` files your done report and sets `status` for you where it is
installed. Otherwise, at the end of the work:

    agent-desk done --title 'Rerun finished' --report results/rerun-nov.org <<'S'
    All five seeds rerun; the fix holds. Two questions remain open: q-7f3a, q-b2c4.
    S

## Under a sandbox

The store (`~/.agents/desk` by default) is outside your workspace, so a
sandboxed Codex session cannot open it, not even to read: the command fails
and names the sandbox. Rerun that command with escalated permissions. The
user can avoid this for good by adding the store directory to
`sandbox_workspace_write.writable_roots` in Codex's `config.toml`.

Antigravity's sandbox lets shell commands write only temporary directories
and has no setting to widen it: run `agent-desk` there with a request to bypass
the sandbox.

## Settings are the user's

Where notifications go and how messages reach a busy agent are the user's
settings. `agent-desk pref` refuses an agent session, so if the user asks you
to change one, give them the command to run, such as
`agent-desk pref set notify.remote always --scope example-proj`, or point
them to the dashboard's Notify chip.

## Looking things up

    agent-desk whoami                 # your nodes, one per line, or exit 1 if you hold none
    agent-desk show --mine            # your node: open questions, slots, messages awaiting you
    agent-desk show rerun-nov         # one run
    agent-desk search 'leakage'
    agent-desk instructions           # the user's messages to your run, word for word

Where the prompt hook is installed, every message the user sends a session
that has joined a node is kept on the desk, word for word; automated prompts
(heartbeats, `Continue.`, `/compact`, other agents' messages, desk answers)
are not. After compaction, read `agent-desk instructions` before acting: it
is the user's own text, not a summary of it. It prints
`(no instructions recorded ...)` when the hook is not installed; then keep
the user's words in your own verbatim file as before.
