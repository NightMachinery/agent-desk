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
  path), `api/read {target}` (an id or a path), `api/unread {id}`.
- `GET api/health`: `ok`.
