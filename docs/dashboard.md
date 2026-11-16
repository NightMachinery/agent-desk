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
- The server sends a Content-Security-Policy that allows only the page's own
  files and the inline scripts in `index.html`, by hash, so no other origin
  can be loaded.

The API, all JSON:

- `GET api/view?path=&session=&host=&all=1&q=`: the three sections, the node
  at `path` with where a message to it goes, the session's details, and the
  lists of projects and machines.
- `GET api/item?id=`: one item, its answers and replies with how each was
  delivered, and where a reply goes.
- `GET api/stream`: server-sent events; `change` whenever the store changes.
- `POST api/answer {id, text}`, `api/reply {to, text}` (an id or a node
  path), `api/read {target}` or `{targets: [...]}` (ids or paths; an open
  question named by id is dismissed), `api/unread {id}` or `{targets: [...]}`.
- `GET api/health`: `ok`.
