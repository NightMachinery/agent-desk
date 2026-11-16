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

The path is `<run>/<role>[/<role>...]`: a short run slug, then role slugs (not
agent ids). The project is the repository you are in (`--project` to
override). A session working alone with the user can skip `join`; its first
write joins it to `<project>/adhoc/<slug>`.

**Workers do not write.** If another agent started you (a Paseo child, a tmux
subagent), report to that agent; `agent-desk` refuses you anyway. Your parent
can give you a node with `join` if the work calls for it. An in-process
subagent, such as one from Claude Code's Agent tool, shares its parent's
session, so the desk cannot tell it apart and would file as the parent: never
run `agent-desk` from one.

## Questions

When you need a decision, file it, and raise it in chat as you normally would,
with the id in the chat text:

    agent-desk ask --blocked --title 'Which split for the rerun?' <<'Q'
    The rerun needs a train/test split.
    A. Reuse the cached split: fast, but it predates the leakage fix.
    B. Regenerate: an hour longer, consistent with the fix.
    I recommend B.
    Q

- The title is the question itself, readable on a phone notification.
- The body is everything needed to answer cold: the context, every option
  with its tradeoffs, and your recommendation. Do not refer to earlier chat.
- `--blocked`: you stop until answered. `--default 'B: regenerate'`: you go
  ahead with that choice unless told otherwise; say so in chat too.
- One question per `ask`. Do not refile a question that is still open; check
  with `agent-desk show --mine`.

**Record every answer you receive in chat** (typed reply or a question tool),
or the question stays open in the user's inbox:

    agent-desk answer q-7f3a --from-chat 'B, but keep the old cache around'

An answer given in the inbox arrives typed into your session as
`Answer to q-7f3a "Which split for the rerun?": B`. It is already recorded;
just act on it.

A message that starts `Message from the user via agent-desk` was sent from
the desk by a user who may not be watching your chat. Act on it, and put
anything they should read in reply on the desk (`tell`, or a slot), not only
in chat.

## Notices

For something the user should know but need not answer: a result, a finished
milestone, a problem you worked around, a report written to a file.

    agent-desk tell --title 'Seeds 1-5 rerun: the fix holds' <<'R'
    Accuracy is within 0.3 points of the old run on all five seeds.
    Table: results/rerun-nov.org
    R

Add `--notify` only when it is worth interrupting the user for.

## Slots: what is true now

A slot is a named box at your node whose text you replace. The board shows the
current text; old text is kept but out of sight.

    agent-desk set status 'Seeds 3/5 done. ETA 14:00.'
    agent-desk set findings < findings.txt
    agent-desk clear next
    agent-desk link tasks TASKS.org

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

## Looking things up

    agent-desk show --mine            # your node: your open questions, slots
    agent-desk show rerun-nov         # one run
    agent-desk search 'leakage'
