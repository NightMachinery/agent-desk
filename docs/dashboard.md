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
  `resume`, `age`), `status` (`responded`, `awaiting`, `no_response` after 15
  minutes, or `cleared` by a read) with the `responses`' ids, and `notify`,
  its per-message routing. A response has `by` (its agent) and `re`.
- A target has `via`, `behaviors` (each `supported`, or not and `why`),
  `default_behavior` (the `deliver.behavior` setting), `notify` (`desktop`
  and `remote` resolved for class `response`), `finished` (`done` or
  `archived`: sending revives it), `pushed` (its host is in `ssh_hosts`) and
  `remote_label`.
- `GET api/target?to=`: the target of an id or a node path.
- `GET api/prefs?path=&class=`: each setting at that scope for one class
  (none: all kinds), with its value, `origin`, `here` (an override stands at
  exactly this scope and class, so it can be reset), `differs` (the classes
  that resolve otherwise) and `options`; also every override on the desk.
- `GET api/stream`: server-sent events; `change` whenever the store changes.
- `POST api/answer {id, text, behavior?, notify?}`, `api/reply {to, text,
  behavior?, notify?}` (an id or a node path; `notify` is `{desktop,
  remote}` for the response to this message only), `api/read {target}` or
  `{targets: [...]}` (ids or paths; an open question named by id is
  dismissed), `api/unread {id}` or `{targets: [...]}`, `api/pin` and
  `api/unpin {id}`, `api/deliver {id}` (retry one answer, named by itself
  or its question, or one reply; 422 with the reason when it fails again).
- `POST api/pref {scope, class?, key, value}`: sets an override; a null
  `value` removes it. The browser acts as the user, so this works even when
  the server was started from an agent's shell.
- `GET api/quota?fresh=`: the agents out of quota (each with `paseo`,
  `title`, `provider`, `last_active`, `parent`, `children`, `node`, `scope`
  (the node it is filtered under: its own, or its nearest ancestor's),
  `auto_continue` and `dismissed`), whether `auto_continue` and
  `launch_ready` are configured, and `profiles` (the Replace menu). Under a
  focus only the agents whose `scope` is inside it are listed, unless
  `unfocused=1`.
  `POST api/quota {action, id, profile?}` with `auto-continue-on`,
  `auto-continue-off`, `ask-parent`, `replace`, `dismiss` or `undismiss`.
- `GET api/auto-continue?paseo=`: `{available, on}` for one agent, for the
  node panel's checkbox.
- `GET api/health`: `ok`.
