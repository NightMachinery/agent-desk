# Performance

@GPT-6.2 Sol

The November 2026 pass used an Apple M2 with synthetic data. The loopback
and CLI fixture held 7,501 events across 60 coordinators, with slot
revisions, prompt records, questions, read markers and deliveries. The
item test opened a 180-entry conversation. Each CLI median used ten new
processes after a warm-up. Each HTTP median used 15 requests to a warm
server. The quota command returned one fabricated agent.

## Wall time before and after

- `whoami` 284 to 129 ms. `hook prompt` 380 to 124 ms.
- `tell` 9.57 to 1.14 s. `set` 11.67 to 1.41 s.
- `show --mine` 744 to 476 ms. `render` 11.67 to 0.27 s.
- `GET /api/view` 204 to 53 ms. `GET /api/item` 398 to 8.6 ms.
- `GET /api/quota` 165 to 0.58 ms cached and 310 to 20.5 ms with
  `fresh=1`. The fresh time includes the fixture's stub and local records.

The default Go benchmark is a second synthetic workload of the same size
with more slot revisions per path. It profiles the event load, the warm
view and item handlers, and one stream stamp check.

- Desk load 540 to 137 ms, with 14.14 to 14.33 MB allocated per operation.
- View 581 to 17.0 ms, with 17.82 to 2.88 MB allocated.
- Long item 829 to 3.17 ms, with 20.02 to 0.48 MB allocated.
- Stream stamp 1.32 ms to 85 microseconds per poll. The stream still
  checks once per second.

## Browser

Headless Chrome rendered 420 list rows and 180 thread entries on desktop
and a 390 px viewport. The root view dropped from two identical requests to
one, saving about 0.4 MB of uncompressed JSON per initial load. A thread
response was about 0.12 MB. The scripts and styles totaled 348,336 to
348,459 bytes uncompressed.

- Desktop view response to list paint 178 to 164 ms. Tap or click to
  thread paint 717 to 150 ms, including the item request.
- Phone-width view response to list paint 172 to 168 ms. Tap or click
  to thread paint 783 to 133 ms, including the item request.
- Navigation to list paint varied from seconds to tens of seconds in
  headless Chrome. The follow-up capture attributed most of that wait to
  the initial HTML navigation before any desk API call. The timings
  above use Resource Timing and two animation frames after rendering.
  A device over the tailnet was outside this measurement.

## What changed

SQLite statement execution accounted for 63% of the initial Go CPU
profile. Scanning events accounted for 35% of allocations directly. A
partial SQLite index on slot path and ID reduced the current-slot query
from 26 to 5 ms on a copy of the fixture. The load also indexes
conversations and delivery marks so rendering the ledger can look up each
thread without traversing the event log again. Indexing increases
allocations slightly on individual loads.

The server reuses store-derived data until the SQLite stamp changes. Each
request refreshes its clock and obtains the latest cached Paseo statuses.
Separate COUNT and MAX subqueries preserve the stamp and let SQLite use
its fast paths. Quota refresh reuses the same snapshot it used for focus.
The CLI checks membership using the kind index for joins and leaves.
The dashboard uses plain API response objects, shares identical view
requests, and computes each list group's newest ID once when sorting.

The CLI's write pipeline still loads the desk for validation, display
IDs, rendering under its file lock, and notifications when configured.
That explains much of the remaining cost in `tell` and `set`. The
database keeps its WAL and durability settings. Individual response
payloads and bundle contents stayed the same size for practical purposes.

## Reproduce

Run the Go benchmarks and inspect the CPU and allocation profiles with
Go pprof.

    pf_dir="$(mktemp -d)"
    go test ./internal/cli -run '^$' -bench 'Benchmark(DeskLoad|Dashboard)' \
      -benchmem -benchtime 2s -cpuprofile "${pf_dir}/cpu.pprof" \
      -memprofile "${pf_dir}/alloc.pprof" -o "${pf_dir}/bench.test"

For loopback and browser tests, build the CLI and generate an isolated
desk. Use a fresh fixture for each build. Run these commands in a
subshell so the fixture environment is discarded when you leave it.

    pf_dir="$(mktemp -d)/fixture"
    python3 web/app/scripts/perf-fixture.py "${pf_dir}"
    go build -o "${pf_dir}/agent-desk" .
    export AGENT_DESK_HOME="${pf_dir}/desk" AGENT_DESK_HOST=fixture-host
    export PASEO_HOME="${pf_dir}/paseo" AGENT_DESK_CONFIG="${pf_dir}/config.toml"
    unset PASEO_AGENT_ID TMUX TMUX_PANE
    mkdir "${pf_dir}/bin"
    printf '#!/bin/sh\nexit 0\n' > "${pf_dir}/bin/paseo"
    chmod +x "${pf_dir}/bin/paseo"
    export PATH="${pf_dir}/bin:${PATH}"
    export AI_AGENT=codex CODEX_THREAD_ID="$(cat "${pf_dir}/session-id")"
    "${pf_dir}/agent-desk" import < "${pf_dir}/fixture.jsonl"
    "${pf_dir}/agent-desk" serve --listen 127.0.0.1:7801

The item ID is in `item-id`. Time the loopback GETs with a persistent
connection and check the list and long thread with the headless Chrome
helper. The generator pins the thread root and marks it read. The Go
benchmarks also accept the generated JSONL with `AGENT_DESK_BENCH_FIXTURE`. [Dashboard browser workflow](dashboard.md#looking-at-a-change)

## Second pass, November 2026

@GPT-6.2 Astra

Against revision `7132016`, both builds used the same 8,393 synthetic
events, 60 coordinators, 120 short WAV files, 40 completed retries and
60 cache changes. Measurements used Go 1.27.3 on an Apple M2. CLI medians
used ten new processes and HTTP medians used 15 warm loopback requests.
The first pass used a smaller workload.

- CLI medians in milliseconds were `whoami` 17 to 16, `hook prompt`
  18 to 18, `tell` 412 to 323, `set` 382 to 309, `show --mine` 106 to
  101, and `render` 148 to 156.
- HTTP medians in milliseconds were 23.6 to 23.0 for the view, 6.32 to 6.17
  for the long item, 0.55 to 0.54 for cached quota and 15.9 to 16.6
  for fresh quota. Global recordings fell from 300 to 12.8 and a
  two-recording node from 7.42 to 1.49.
- Five keyed reply POSTs to an inert transport took 66.1 to 2.85 ms to
  acknowledge and 208 to 96 ms from POST to the committed delivery
  receipt, checked every 5 ms. A worker pass with 120 future retries
  fell from 81.3 to 1.92 ms and from 20.7 to 0.50 MB allocated.
- A mock keeper withholding its list for three seconds held the first
  cache GET open for 2,006 ms before and 4.4 ms after. With its 60-agent
  snapshot populated, a list took 0.41 to 0.68 ms and a cold-target
  preflight 0.78 to 1.13 ms. The server starts one background read at
  startup. Until it completes, the list reports loading and a send has
  no cold estimate. Explicit policy changes still return the keeper's
  confirmation.

Three alternating pairs of Go benchmark runs put desk load at 76.7 to 76.0 ms,
warm view at 17.2 to 17.3 ms, long item at 3.99 to 3.90 ms and stream
stamp at 29.6 to 27.5 microseconds. The respective allocations stayed
near 19.9 MB, 5.7 MB, 0.83 MB and 664 bytes. In the baseline CPU profile,
SQLite stepping took 46% of samples and short-ID lookup took 2.44 of
the recordings handler's 3.01 CPU seconds. Desk loading accounted for
34% of allocated bytes. An expression index finds colliding ID suffixes
for receipts and CLI output. Node recording lists scope delivery history
by path. Dashboard actions and the worker reuse the server's stamp-checked
desk, checking again inside the delivery lock. CLI validation, notification
and ledger rendering still load current data.

### Browser and transfer

Three fresh Chrome profiles per case rendered 454 rows and 180 thread
entries. The initial scripts and styles fell from 377,758 to 202,771
uncompressed bytes. The final server sent 67,298 bytes with gzip. Across
all routes and the worklet, the build grew from 378,760 to 387,247 raw
bytes. This build loaded Detail, node and recordings code on demand. The recorder
still requests the microphone inside its button gesture.

- Desktop medians in milliseconds were 156 to 161 from the view response
  to list paint, 123 to 137 from click to thread paint, 101 to 107 from
  item response to composer paint, and 34 to 33 from editing a draft
  to painting its enabled Send button. At 390 px these
  were 158 to 161, 133 to 135, 104 to 112 and 33 to 32.
- HTML response to list paint was 271 to 213 ms desktop and 226 to
  217 ms at 390 px. Navigation to paint varied with the wait for HTML.
  Desktop medians were 7.01 to 0.88 s and phone-width medians 0.64 to
  7.16 s. These loopback navigation times did not give a stable ranking.
- At 390 px, 2 Mbps down, 80 ms latency and fourfold CPU slowdown,
  HTML response to usable inbox fell from 4.20 to 2.95 s. Splitting
  alone gave 3.44 s. First click to the long thread rose from 1.03 to
  1.35 s (1.73 s with splitting alone) because the browser fetched its
  renderer on first use. These are simulated phone timings.

### Idle panel prefetch

The dashboard now schedules Detail and NodePanel downloads once per page
after the populated inbox paints. It uses an idle callback when available
and a timer otherwise. Recordings still loads when opened.

We reran the 390 px phone case against `cc4981c` with 2 Mbps down,
1 Mbps up, 80 ms latency and fourfold CPU slowdown. Each number is the
median of three fresh Chrome profiles per build and click timing, using
the same 454-row inbox and 180-entry thread.

- After three seconds reading the inbox, HTML response to usable inbox
  took 2,914 ms before and 2,915 ms after. Click to the painted thread
  and composer fell from 1,293 to 986 ms (24%).
- With a click at the first usable inbox paint, HTML to inbox took
  2,862 ms before and 2,896 ms after. Click to thread took 1,275 ms
  before and 1,291 ms after.

With prefetch enabled, Chrome had fetched both panel scripts before
every delayed click and neither before the immediate clicks. Disabling the idle callback for
one additional run confirmed the timer fetched both panels. Recordings
was absent from all pre-click resource lists.

### Repeat the second pass

Use the fixture environment and server above. Include the queue benchmark
when profiling. For the browser, run the following from the repository
root in another shell with the same fixture directory. Omit `--slow-phone`
for loopback and use `--width 1360` for desktop.

    AGENT_DESK_BENCH_FIXTURE="${pf_dir}/fixture.jsonl" go test ./internal/cli \
      -run '^$' -bench 'Benchmark(DeskLoad|Dashboard|SendQueueRetryPass)$' \
      -benchmem -benchtime 2s -cpuprofile "${pf_dir}/cpu" \
      -memprofile "${pf_dir}/mem" -o "${pf_dir}/bench.test"
    node web/app/scripts/shot.mjs http://127.0.0.1:7801/ "${pf_dir}/phone.png" \
      --width 390 --slow-phone --wait 0 --timeout 180000 \
      --init-file web/app/scripts/perf-browser.js --js 'return await window.fixtureTiming'

For a click after three seconds of reading, use the same command with
the URL `http://127.0.0.1:7801/?fixtureDwellMs=3000`. The probe reports
`htmlToListMs`, `clickToThreadMs` and the panel scripts downloaded before
the click in `panelsBeforeClick`.
