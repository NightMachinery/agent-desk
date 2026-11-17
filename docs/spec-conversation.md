# agent-desk: conversations, delivery behaviour and notification routing

@Fable5.2 [2026-11-16 Mon]

For the implementing agent. This extends [spec.md](spec.md); where the two
disagree, this file wins. Decisions were settled with the user on 2026-11-16.
Every value in the examples is fabricated; keep it that way.

## Terms

- **Message**: a `reply` event, the user's text for an agent.
- **Response**: a `tell` whose `ref` is a message or an answer.
- **Thread**: an item, or a node, with the messages, answers and responses
  that lead back to it through `ref`.
- **Behaviour**: what a message does to a busy agent. `steer` joins the
  running turn at its next step, `queue` waits for the turn to end,
  `interrupt` cancels the turn.
- **Disposition**: what the transport reports actually happened to a message.
- **Setting**: a named value resolved for one event by scope and class.
- **Scope**: the empty path (global) or a project, run or node path.
- **Class**: the kind of event a setting applies to: `ask-blocked`,
  `ask-default`, `tell-notify`, `tell`, `done`, `response`, or `*`.
- **Channel**: one way of notifying. There are two: `desktop` and `remote`.
- **Handle**: what a channel returns for a notification it raised, so the
  notification can be taken back later.

## Decisions and their reasons

1. A conversation lives on what it is about. No thread entity: a thread is
   derived from `ref`. A separate entity would add a "which thread" choice to
   every write for no gain in steering agents.
2. The agent responds with `agent-desk tell --re <id>`. The typed message
   carries its own id and that command, so linking does not depend on the
   agent remembering the skill.
3. No acknowledgement command. A message is sent, delivered (with its
   disposition) or responded. A one-line response is the acknowledgement;
   a separate command would be forgotten and say less.
4. Steer is the default behaviour, and the behaviour is a setting. Queue is
   used where the transport supports it and otherwise falls back to steer,
   visibly. Interrupt is only ever a per-message choice with a confirmation,
   never a stored default, because it destroys in-flight work.
5. Notification routing is a setting with two independent axes, `desktop`
   and `remote`, so that "which machines" and "also my phone" do not multiply
   into one long list.
6. The core knows no machine, chat or product name. It resolves settings and
   passes the result to the machine's commands. Machine facts live in the
   config file; overrides live in the store as events, so they can be set
   from the dashboard, merge between machines and keep their history.
7. Notifications are taken back automatically when their item is read,
   answered, dismissed or replaced. Handles are events, so any machine can
   take back a remote notification after a merge.
8. A click on any notification opens the web dashboard at the item's URL.
9. No hook writes to the desk. The author's turn-end hooks are instead
   silenced for sessions the desk already covers (see "The author's
   integration").

## Events

New kinds and fields; all other kinds are unchanged.

- `reply`: gains `meta.behavior` (`steer`, `queue` or `interrupt`, the one
  requested) and optional `meta.notify` (per-message overrides for the
  response: `{"desktop": ..., "remote": ...}`).
- `tell`: may carry `ref`. Allowed targets are a `reply` delivered to this
  node, or an `answer` to one of this node's questions. Anything else is
  refused. `meta.quiet=true` when given `--quiet`.
- `delivered`: gains `meta.behavior` (the one used), `meta.disposition`
  (Paseo's own values `turn_started`, `steered`, `interrupted`, `queued` and
  `out_of_band`, or `typed` for tmux and `command` for `deliver_command`),
  `meta.queue_position` (with `queued`), `meta.revived` (boolean) and
  `meta.fallback` (a reason, such as `queue unsupported`, when the behaviour
  used differs from the one requested).
- `pref` (letter `p`): `path` is the scope (empty for global); `meta` holds
  `key`, `class` and `value`. A null `value` removes the override. The latest
  `pref` for a scope, key and class counts. Only the user writes it: a
  command from an agent session is refused.
- `notified` (letter `n`): `ref` is the item; `meta` holds `channel`,
  `handle`, `dest` and the `host` that raised it.
- `retracted` (letter `x`): `ref` is a `notified` event.

Thread root of an event: follow `ref` upwards through replies, answers and
responses. The first event that is none of those is the root item. A reply
with no `ref` belongs to its node's thread.

Derived state:

- A message is **responded** when a `tell` refers to it.
- A response is unread like any `tell`. `read` on a root item also marks its
  unread responses read. `read` on a message clears its "no response" flag.
- A delivered message with no response after 15 minutes is flagged
  "no response yet" in every view. Answers are never flagged.

## Typed text

    Message m-4c2d from the user via agent-desk, about t-k2m9 "Deploy finished": check the old feed too
    (Respond on the desk: agent-desk tell --re m-4c2d)

    Message m-9a1e from the user via agent-desk, to example-proj/rerun-nov/coord: pause after seed 4
    (Respond on the desk: agent-desk tell --re m-9a1e)

Answers keep their text (`Answer to q-7f3a "...": B`) and expect no response.

## Commands

    agent-desk tell --re <id> [--quiet] [--title T]  <<'R' ... R
    agent-desk reply <id|path> [--behavior steer|queue|interrupt]
                     [--notify-desktop V] [--notify-remote V] 'text'
    agent-desk pref set <key> <value> [--scope <path>] [--class <class>]
    agent-desk pref unset <key> [--scope <path>] [--class <class>]
    agent-desk pref show [<path>] [--class <class>] [--format json]
    agent-desk whoami            #: the calling session's node, or exit 1

- `tell --re` without `--title` uses "Re: " plus the root item's title, or
  the first line of the body in a node thread.
- `show --mine` gains a section "Messages awaiting your response".
- `pref show` prints each key's resolved value and where it comes from.
- `answer` takes `--behavior` too.

## Delivery behaviour

The behaviour of a message is its `--behavior`, else the setting
`deliver.behavior` resolved at the target node, else `steer`. The setting
accepts only `steer` and `queue`.

Paseo:

- Always pass `--behavior` explicitly, and remove `PASEO_AGENT_ID` from the
  environment of the call. Without the flag a caller outside an agent
  interrupts; with the variable the message is attributed to that agent.
- Queue support is detected without sending: `queue` appears among the
  `--behavior` choices in `paseo send --help`. Cache the result per binary
  path and modification time. Unsupported means send with `steer` and record
  `meta.fallback`.
- Send with `--json` and record the receipt's `disposition`,
  `queuePosition` and `revived` when present. A receipt without them gives
  `disposition` unset, shown as plain "delivered".
- Paseo falls back from steer to replacing the turn when it cannot steer: a
  provider without steering, a compaction in progress, or a text that is a
  slash command. The disposition then says `interrupted`; show it as such.
  A message whose text starts with `/` is therefore sent with `queue` where
  supported, and otherwise refused for a busy agent unless the user chose
  interrupt.
- A queued message is held by the Paseo daemon, survives its restart, and is
  delivered in order after the turn ends however it ends. An idle agent
  starts at once (`turn_started`). Archiving the agent drops the queue. The
  desk does not track a queued message further: one that never produces a
  response keeps its "no response yet" flag. (`paseo inspect <id> --json`
  lists `QueuedMessages`, should a later version want to show the queue.)
- A second check, for a remote daemon: `paseo daemon status --json` has
  `features.agentMessageQueue`. A queue send to a daemon without it fails
  and sends nothing; treat that failure as unsupported and resend as steer.

tmux: `steer` is the existing paste plus Enter (`typed`). `queue` is
unsupported and falls back to steer. `interrupt` is unsupported too and
falls back to steer: the user decided on 2026-11-17 not to interrupt tmux
panes. The Escape-then-paste code that did it is kept, out of the build, in
`disabled/tmux_interrupt.go`.

`deliver_command`: the payload gains `behavior`; the command may print one
line of JSON with `disposition`.

`api/target` and the item and view responses report, per target,
`behaviors` (each of the three with `supported` and, when not, `why`) and
`default_behavior`.

## Settings

Keys:

- `deliver.behavior`: `steer` or `queue`. Class is ignored.
- `notify.desktop`: `none`, `origin` (the machine the event was written on),
  `all` (origin plus every host in `ssh_hosts`), or host names separated by
  commas.
- `notify.remote`: `off`, `away` (only when `away_command` exits 0 on the
  origin machine) or `always`.
- `notify.remote.dest`: free text handed to the remote channel.

Resolution for one event, first match wins:

1. The per-message override (`meta.notify` or `--behavior`).
2. `pref` events, from the event's node upwards to the global scope. Within
   one scope the event's class beats `*`. A nearer scope beats a farther one
   whatever the class.
3. The config file's `[defaults]`, class before `*`.
4. Built in: `notify.desktop` is `origin` for every class except `tell`
   (`none`); `notify.remote` is `away` for `ask-blocked` and `response`, and
   `off` otherwise; `deliver.behavior` is `steer`.

Classes: a question is `ask-blocked` or `ask-default`; a `tell` is
`response` when it has a `ref`, else `tell-notify` with `--notify`, else
`tell`; a done report is `done`. `tell --re --quiet` notifies nowhere.

Config file additions (unknown keys stay an error):

    base_url = "https://desk.example.net/"
    away_command = "my-away-check"
    remote_label = "Phone"
    [ssh_hosts]
    otherhost = "otherhost.example.net"
    [defaults]
    "notify.remote.dest" = "example-chat"
    [defaults.class.ask-blocked]
    "notify.remote" = "always"

## Notifications

Raising, on a notifiable event written locally:

- Resolve the two axes. Run `notify_command` once per channel that applies,
  with the payload below. The command prints its handle as one line on
  stdout; the core appends a `notified` event.
- So that a slow notifier never holds up an agent's write, the write spawns
  a detached `agent-desk notify-run <event id>` that does the above.
- When the desktop targets include another host found in `ssh_hosts`, the
  detached process also runs `merge ssh:<host>` for it. An unreachable host
  is skipped; it catches up at its next merge.
- On import or merge, for each new event that is still open or unread:
  resolve its settings, and when this host is a desktop target with no
  `notified` event of its own for that item, raise the desktop notification
  here. This replaces the blanket `--notify` flag, which stays as "notify
  for every imported event".
- The remote channel is only ever raised by the origin host.

Taking back, when an item is read, answered, dismissed, or (for a slot) when
its path gets a newer `set`, `clear` or `link`:

- For each `notified` event of the item with no `retracted`: a `desktop`
  one is taken back by the host that raised it; a `remote` one by whichever
  host sees the cause first. Run `dismiss_command` with the handle, then
  append `retracted`. Both run again after a merge brings the cause in.
- Taking back must be idempotent on the command's side: two hosts may both
  do it before they merge.

Payload additions for `notify`, `dismiss` and `open`: `channel`, `dest`,
`handle` (dismiss), `url` (the item in the dashboard, from `base_url`),
`project`, `class`, and `agent_title`. `meta.from` gains `title`, recorded at
write time from the Paseo agent's title when there is one; `agent_title`
falls back to the agent name and short session id.

`agent-desk open <id|path>` with no `open_command` opens `url` in the
browser when `base_url` is set, and falls back to the inbox otherwise.

## Surfaces

Web dashboard:

- Item page: the thread in time order under the item. Each message shows
  its chips: sent, then the disposition ("joined the running turn", "started
  a new turn", "queued, position 2", "interrupted the turn", "revived the
  session"), a fallback note when there is one, then "responded" or, after
  15 minutes, "no response yet". Responses render with their body.
- Node page: a "Conversation" section with the node thread, newest last.
- Composer: one Send button using the resolved behaviour, and a menu beside
  it with the other behaviours. An unsupported one is disabled with its
  reason. "Stop and send" (interrupt) asks for confirmation. A "Notify me"
  control shows the two resolved values for the response and lets either be
  changed for this message.
- A target whose node has a done report, or whose Paseo agent is archived,
  shows "This session has finished; sending revives it" above the composer.
- Node page and a global settings page: one dropdown per key, showing the
  resolved value and its origin ("inherited from example-proj"), with a
  class selector that defaults to "all kinds", and "reset to inherited".
- New: a response is listed as "Response to m-4c2d" under its root item's
  group. Its page, and a message's, shows the conversation it belongs to
  (the root item's, or the node's) with itself highlighted, and links the
  root item. It stays its own page so that opening it marks it read and
  Previous and Next keep their place in the list.
- Phone: the composer sticks to the bottom; the behaviour menu and "Notify
  me" open as sheets; settings sit behind one "Notify" chip on the node.

API: `GET api/item` and `api/view` return threads as described and the
target's behaviours, its notify values and whether it has finished; `POST api/reply` and `api/answer` accept `behavior` and
`notify`; `GET api/prefs?path=&class=` and `POST api/pref {scope, class,
key, value}`.

Terminal inbox: the preview shows the thread. `alt-m` messages the session
behind the selected row. A response row is marked `re`.

Org render: responses and messages are child headings of their root item,
with keywords `MSG` and `RE` and the delivery state as a tag.

## Edge cases

- **Finished or archived session.** Sending is allowed after the warning
  above; `meta.revived` records it.
- **Takeover.** A message about an old coordinator's item goes to the new
  coordinator, as now, and the new coordinator may respond to messages
  delivered to its predecessor. The thread says who received it.
- **Workers.** They have no node, so they cannot be messaged or respond.
  A coordinator relays.
- **Another machine.** A message for a session on a host in `ssh_hosts` is
  pushed with `merge ssh:` at once; otherwise it waits for the next merge
  and shows "waiting for a merge to otherhost".
- **Answered in chat.** If the agent replies only in chat, the message is
  flagged "no response yet"; the user clears that with `read` on it. The
  skill asks for a one-line `tell --re` even then.
- **Several messages before a response.** Each is its own event; a response
  refers to one. The others are cleared by `read`, or by the agent
  responding to each.

## Skill changes

Replace the paragraph on desk messages with: a message that starts
`Message m-... from the user via agent-desk` comes from a user who may not
see the chat. Act on it, then always respond with `agent-desk tell --re
<id>`, even with one line ("Done.", "Will do after the current seed."). Use
`--quiet` only for a bare acknowledgement. `show --mine` lists messages
still awaiting a response.

## The author's integration (scripts repository, separate commits)

- `agent-desk-notify` branches on `channel`. `desktop`: the existing card,
  group as handle, click opens `url`. `remote`: one rich Telegram message to
  `dest` through `tsend --print-ids --parse-mode=rich`, with the project and
  path, the agent's title, kind and level, the item's title, a link to
  `url`, and the body in an expandable quote. The handle is the destination
  and message id.
- `agent-desk-dismiss`: `tsend delete`, or an edit down to one line when the
  message is older than the 48 hours bots may delete.
- `agent-desk-open` opens `url`. `agent-desk-config-install` writes
  `base_url`, `away_command`, `remote_label`, `ssh_hosts` and the default
  destination.
- A tracked send and retract pair, shared with the bell hooks, so their
  Telegram messages are removed when their fact stops holding.
- The turn-end bell is skipped for a session that `agent-desk whoami`
  places on a node while the desk has a `notify_command`: its questions and
  done report already notify. Other sessions keep the bell. A variable
  restores the old behaviour.

## Migration

- Old `reply` events have no `meta.behavior`: treat as `steer`. Old
  `delivered` events have no disposition: show "delivered".
- Old notices are not linked to anything and stay as they are.
- Notifications raised before this version have no `notified` event; the
  existing group-by-event-id dismissal keeps working for them, so
  `dismiss_command` is still called once per item with no handle when an
  item has no `notified` events.
- A config with only the four old keys behaves as before, with the built-in
  defaults.

## Acceptance checks

1. A message to a busy Paseo agent calls `paseo send` with `--behavior
   steer` and without `PASEO_AGENT_ID` in its environment.
2. With a fake `paseo` whose help lacks `queue`, a queue message is sent as
   steer, `meta.fallback` is set, and `api/target` reports queue unsupported.
   With one that lists it, the message goes out with `--behavior queue` and
   the receipt's disposition is recorded.
3. `reply --behavior interrupt` works; `pref set deliver.behavior interrupt`
   is refused.
4. `tell --re m-...` from the target session links; from another node's
   session, or naming a notice, it is refused. The item's thread lists
   message, delivery and response in order, and the message reads responded.
5. A delivered message with no response shows the flag after 15 minutes
   (with a faked clock) and loses it on `read`.
6. Resolution: a node `pref` beats a run `pref` beats a config default; at
   one scope a class beats `*`; a nearer `*` beats a farther class; unset
   restores inheritance; `pref show` names the origin. An agent session
   cannot write a `pref`.
7. A blocked question with `notify.remote=always` runs `notify_command`
   twice, once per channel, and records two `notified` events with the
   printed handles. Answering it runs `dismiss_command` for both and records
   two `retracted` events. A newer `set` takes back the old slot's.
8. Two stores: an event written on A with `notify.desktop=all` raises a
   desktop notification on B at import, once; reading it on B takes back
   A's after the merge, and the remote one exactly as often as hosts saw the
   read before merging.
9. A response notifies under class `response`; with `--quiet` it does not;
   a message sent with a per-message override uses it.
10. The dashboard on a phone-sized viewport: thread, behaviour menu with a
    disabled queue and its reason, "Notify me" sheet, settings chip.
11. Stores written by the first version load, render and merge unchanged.

## Commit groups

1. Delivery fix: explicit behaviour, clean environment, dispositions.
2. Responses: `tell --re`, typed text, thread derivation, `show --mine`,
   `whoami`.
3. Behaviour choice: `--behavior`, queue detection and fallback, interrupt.
4. Settings: `pref` events, resolution, config defaults, `pref` commands.
5. Notification routing: channels, handles, `notify-run`, taking back,
   import-time notification, push to `ssh_hosts`, `base_url` and `open`.
6. Dashboard and API: threads, composer menus, settings controls.
7. Terminal inbox and org render.
8. Skill and user documentation.
9. In the scripts repository: the channel commands, the tracked send and
   retract pair, and the turn-end bell rule.

## Open, to confirm before the affected group

- Paseo's queue interface above is final in the fork's code but not yet in
  a fork release. Group 3 builds against a fake `paseo` until the release
  carries it, then is checked against the real one.
- Settled, for the record: the "tool use was rejected" text agents saw came
  from interrupts, not steers. A steer during a running tool lets the tool
  finish; a steer during an open permission prompt denies that prompt and
  the agent asks again.
- Settled 2026-11-17: no tmux interrupt (see Delivery above).
