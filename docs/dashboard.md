# Changing the dashboard

The page is a Svelte 5 app in `web/app`, built by Vite into `web/dist`, which
`web/embed.go` embeds in the binary. `web/dist` is committed, so
`go install` needs no Node.

    cd web/app
    npm ci
    npm run dev       # beside `agent-desk serve`; Vite proxies /api to 127.0.0.1:7740
    npm run check     # svelte-check
    npm run build     # writes ../dist and ../dist/.source-hash

Commit `web/dist` with the sources. `go test ./web` fails when `web/dist` is
older than the sources: the build stamps a hash of everything it read
(`scripts/stamp.mjs`), and the test recomputes it.

## Looking at a change

Check a change in a real browser, on a desk of fabricated data, before
committing it. `scripts/fixture.sh` builds the desk; `scripts/shot.mjs`
screenshots a URL in headless Chrome, optionally after running JavaScript in
the page, and prints what that code returns. It needs Node 22 or later and
Chrome (`CHROME` names another binary), and installs nothing.

    d=$(mktemp -d)
    go build -o $d/agent-desk .
    AGENT_DESK_BIN=$d/agent-desk web/app/scripts/fixture.sh $d
    AGENT_DESK_HOME=$d/desk AGENT_DESK_HOST=host-a $d/agent-desk serve --listen 127.0.0.1:7792 &
    node web/app/scripts/shot.mjs 'http://127.0.0.1:7792/#/p/' $d/desk.png
    node web/app/scripts/shot.mjs 'http://127.0.0.1:7792/#/p/' $d/phone.png --width 520 --height 900 \
      --js 'document.querySelectorAll("button.check")[0].click(); return document.querySelector(".bulk")?.innerText'

A width under 600 emulates a phone. `--wait` (default 1500 ms) is the time
given to the page to load first. An exception in the page script exits 1.
The page's live stream never goes idle, so tools that wait for the network to
settle (such as Chrome's `--virtual-time-budget`) hang on it; this script
waits a fixed time instead. Rebuild the binary after `npm run build`: the page
is embedded in it.

Rules the page keeps:

- Every view is in the hash URL (`src/lib/route.svelte.js`), so any view can
  be bookmarked, and the page always loads from the root of wherever it is
  mounted. A proxy may strip a path prefix; nothing may assume the root of
  the host. Asset URLs are relative and carry a content hash.
- It talks only to `./api/...` (`src/lib/api.js`). Writes send
  `X-Agent-Desk: 1` and a JSON body; the server refuses anything else.
- It is an installable app: `public/manifest.json`, the icons, and
  `public/sw.js`, a service worker that caches nothing but
  `public/offline.html`, shown when the server cannot be reached. These keep
  stable names, as browsers and home screens find them by URL, and the server
  sends everything outside `assets/` as `no-cache`. Never let the worker cache
  the page or its data: an installed app must not show a stale desk. The
  worker also shows push messages (`{type: "show"|"close", tag, title, body,
  url, sticky}`, from `internal/cli/push.go`) and opens the item on a tap;
  `components/PushSettings.svelte` subscribes the device. To check the
  handlers without a phone, deliver a message through the DevTools
  protocol's `ServiceWorker.deliverPushMessage` after granting the
  `notifications` permission.
  `scripts/icons.sh` renders the PNG icons from the drawing in
  `public/favicon.svg`. The manifest has no `id`: one resolves against the
  origin, so `"./"` would make it the same app as every other on the host.
  When `serve --config` names `title`, `device` and `color`, the server sends
  the manifest with the device in its name (`Agent desk · laptop`) and the
  device's colour as its theme, so copies installed from two devices tell
  apart.
- The server sends a Content-Security-Policy that allows only the page's own
  files and the inline scripts in `index.html`, by hash, so no other origin
  can be loaded.

The API, all JSON:

- `GET api/view?path=&session=&host=&all=1&q=&linger=&unfocused=1`: the
  sections, the node at `path` with its target (where a message to it goes)
  and `conversation` (the node's own thread), the session's details, and the
  lists of projects and machines; each node in the run tree carries `paseo`,
  the Paseo agent of its latest join on this machine, when there is one. The
  sections follow the focus unless
  `unfocused=1`. Also `sets` (each `name`, `prefixes`, `focused`), `focus`
  (`name`, `prefixes` and `hidden`: `needs_you` and `unread`, what the focus
  hides in this view; null when nothing is focused) and `paths` (every
  project, each followed by its runs, for the set editor).
- `POST api/sets {action, name, prefixes?}`: `define` (create or replace a
  set), `delete`, or `focus` (an empty `name` turns the focus off), as the
  `sets` and `focus` commands do.
- `GET api/item?id=`: one item, its `thread` and its target. For a message
  or a response the thread is the one it belongs to, named by `thread_of`
  (an item's id or a node's path).
- A thread is a list of entries, oldest first. Each has a `kind` (`message`,
  `answer` or `response`) and, for the user's words: `by` (the session it
  went to), the `behavior` asked for, `delivery` (the receipt: `via`,
  `behavior`, `disposition`, `queue_position`, `revived`, `fallback`) or a
  `pending` reason with, after a failed attempt, `failure` (`error`,
  `id`, `resume`, `age`, `retrying`, `uncertain`), `status` (`responded`, `awaiting`, `no_response` after 15
  minutes, or `cleared` by a read) with the `responses`' ids, and `notify`,
  its per-message routing. A response has `by` (its agent) and `re`.
- A target has `via`, `behaviors` (each `supported`, or not and `why`),
  `default_behavior` (the `deliver.behavior` setting), `notify` (`desktop`
  and `remote` resolved for class `response`), `finished` (`done` or
  `archived`: sending revives it), `pushed` (its host is in `ssh_hosts`) and
  `remote_label`.
- `GET api/target?to=&coordinator=1`: the target of an id or a node path.
  With `coordinator=1`, a project or run (no agent holds it) gives the
  coordinator working under it, with `for` naming the node asked for: what
  New task messages.
- `GET api/prefs?path=&class=`: each setting at that scope for one class
  (none: all kinds), with its value, `origin`, `here` (an override stands at
  exactly this scope and class, so it can be reset), `differs` (the classes
  that resolve otherwise) and `options`; also every override on the desk.
- `GET api/stream`: server-sent events; `change` whenever the store changes.
- `POST api/answer {id, text, behavior?, notify?, send_key?}` and
  `api/reply {to, text, behavior?, notify?, send_key?}` record the message
  and return `{id, event_id, status: "queued"}`. The dashboard persists a
  random `send_key` with the outgoing message before the first request.
  Replaying the same body with that key returns the original event id.
  `to` is an item id or node path. `notify` is `{desktop, remote}` for the
  response to this message. With `after_compaction: true`, a local Paseo
  send waits for the keeper to finish compaction. The queue persists the
  keeper's compaction ID and long-polls it outside the delivery lock. A
  failed or canceled compaction releases the send and adds a thread note.
  Paseo delivery holds the keeper's existing per-agent flock around
  `paseo send`.
- `POST api/deliver {id}` queues a delivery retry for an answer, its
  question, or a reply. For an unconfirmed send, the dashboard asks the user
  to confirm the duplicate risk, then sends `force: true` and the
  displayed `failure_id`. The retry resets the delay and records that
  the user forced it.
- The queue writes an attempt marker before calling the transport. A
  failure writes another event with the next retry time. An always-failing
  transport with instant responses and uninterrupted service can make 149
  attempts within the 24-hour window, up to 298 delivery events per message.
  If the desk resumes after a retry window expires, one final marker
  closes the window without starting the missed attempt.
- The automatic screen sweep queues its answer and commits the grant in
  the same transaction. The commit checks whether the screen changed
  during the idle probe.
- `POST api/read {target}` or `{targets: [...]}` accepts ids and paths.
  An open question named by id is dismissed. `api/unread` accepts `{id}`
  or `{targets: [...]}`, and `api/pin` and `api/unpin` accept `{id}`.
- `POST api/pref {scope, class?, key, value}`: sets an override; a null
  `value` removes it. The browser acts as the user, so this works even when
  the server was started from an agent's shell.
- `GET api/quota?fresh=`: the agents out of quota (each with `paseo`,
  `title`, `provider`, `last_active`, `parent`, `children`, `nodes` (every
  desk node its session holds, in join order; a session may hold several),
  `scopes` (the nodes it is filtered under: its own, else its nearest
  ancestor's), `model` and `thinking` it was started with, `auto_continue`
  and `dismissed`), whether `auto_continue` and `launch_ready` are
  configured, and `profiles` (presets in the Replace dialog). Under a
  focus only the agents with a scope inside it are listed, unless
  `unfocused=1`. (`nodes` and `scopes` are lists since agents could hold
  several nodes; they replace the single `node` and `scope`.)
  `POST api/quota {action, id, profile?}` accepts `auto-continue-on`,
  `auto-continue-off`, `ask-parent`, `replace`, `dismiss` or `undismiss`.
  Dismiss and Undismiss also accept `targets` for multiple agent ids.
  `quota_activity` carries the last activity shown for each selected agent.
- `GET api/providers?fresh=`: the providers the Replace dialog offers:
  each enabled, available Paseo provider as `{id, label, models: [{id,
  label, description, thinking, default_thinking}], account, account_label,
  verdict, available_at, reasons}`. The account is the keeper account most
  of the provider's live agents are seated on (the keeper names accounts
  per agent, not per Paseo provider). Served from a five-minute cache,
  refreshed in the background; `fresh=1` waits for Paseo.
  Dismissal flags are applied to every quota response, including cached lists.
- `GET api/view` also returns `deliverables`, grouped like `responses`;
  an item superseded by a newer deliverable of the same key carries
  `superseded_by`. `deliverables=unread|latest|all` picks which are listed
  (unread: the newest of each key, unread, the default; latest: the newest
  of each key; all: every one; without it, `all=1` means all), and
  `deliverables_any` counts every deliverable in scope.
- `GET api/tasks?path=`: every task under the path (all states), with
  `type`, `tags`, `parent` (full id) and `parent_display`, and `types`, the
  task types there are. `POST api/task {action: "add", to, title, type,
  text, parent, tags}` files one at node `to` (a project, run or agent on
  the desk) as the user (`meta.source` `web`) and returns its `id`;
  `{action: "tags", id, tags}` replaces a task's tags.
- `POST api/agent {action: "archive", id}`: archives a Paseo agent this
  machine's Paseo has a record of (`paseo archive`); an unknown id is
  refused before Paseo runs. `note` says what happened.
- `GET api/draft?key=reply:t-k2m9`: the draft saved on the desk for one
  box, `{key, value, updated, device}`; `value` is any JSON (text, or a
  question's picks), null when there is none, and `updated` (milliseconds,
  the desk's clock) only grows. `POST api/draft {key, draft, device}` saves
  it, and a null `draft` drops it, leaving the newer `updated` so other
  devices drop theirs. Keys are a box name and a colon (`answer:`, `reply:`,
  `choices:`, `voice:`, `node:`); a draft is at most 256 KB. An item with
  an `answer:`, `reply:` or `choices:` draft (not only spaces) is kept in
  `api/view` read or not, and carries `draft: true` there and in
  `api/item`.
- `GET api/dashboard-prefs`: the dashboard settings shared by every device,
  `{"sections": [...]}` once the order of the list's sections is set.
  `POST api/dashboard-prefs {key: "sections", data: [...]}` sets it (each of
  needs, quota, deliverables, pinned, responses, new, tasks, runs, deferred at most
  once; one left out keeps its default place), and a null `data` puts back
  the default. `{key: "render", data: {enabled, debounce}}` is when the
  server renders the org files: on or off, and the seconds (1 to 600) the
  desk must be quiet first; turning it on renders at once.
- `GET api/render`: the org files' state for Settings, `{enabled, debounce,
  dir, last, took_ms, error}`. `POST api/render` renders them now, whatever
  the setting, and returns the same.
- `GET api/export`: the desk as org files in a zip, rendered fresh (the
  render directory is left alone), named `agent-desk-org-<date>.zip`.
- `GET api/auto-continue?paseo=`: `{available, on}` for one agent, for the
  node panel's checkbox.
- `GET api/cache`: cache policies and estimated expiry for local agents,
  read separately from the page view. The server answers from its last
  keeper snapshot at once and refreshes it in the background when it is
  older than a minute (keeper reads can take seconds); only the
  first read after start waits, up to 2 seconds, and otherwise says
  "Cache status loading". Sends reuse each agent's lock path from the
  snapshot instead of asking the keeper again.
  `GET api/cache?kind=answer|reply&target=...` resolves the actual recipient
  and returns `{cold, enabled}` for the pre-send dialog.
  `POST api/cache {id, action, warm_hours?}` changes the keeper's policy and
  records a `cache` event in the desk ledger. The action accepts `off`,
  `compact`, `warm` and `standby`.
- `GET api/health`: `ok`.

## Voice recordings

Voice capture needs a secure browser context (HTTPS or localhost).
The AudioWorklet converts the microphone feed to 16 kHz mono PCM and the
browser posts a chunk every quarter second. Requests use the same origin
checks as attachment uploads. The server writes a playable WAV file and
passes the audio to the configured transcriber. The composer shows the
partial text as the transcriber reports it.

The phone and desktop use the same textarea. A voice token marks the insertion
point during transcription. Backspace or Delete at the boundary removes the
whole token, and cutting and pasting the token moves it. Typing inside a token
moves the caret past it. The text replaces that token as a
`speech-to-text` Markdown block. If a recording has disappeared from the
draft, Send asks whether to put it at the end or leave it out.

Open **Recordings** beside the composer to play audio, retry transcription,
insert a transcript into your draft, or delete an entry. Settings contains
the list for all agents on that desk. Send saves the message in the outbox
with any unresolved voice tokens. The delivery worker waits for transcription
before opening a transport attempt. A failed recording stays visible in the
thread with its error and buttons to retry it or send without it. The
delivered thread shows the text that was sent.
