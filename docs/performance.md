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
    "${pf_dir}/agent-desk" serve --listen 127.0.0.1:7797

The item ID is in `item-id`. Time the loopback GETs with a persistent
connection and check the list and long thread with the headless Chrome
helper. For repeatable thread timings, pin the thread root and mark it
read once before launching the server so opening it does not initiate a
write during the measurement. The Go benchmarks also accept the generated
JSONL with `AGENT_DESK_BENCH_FIXTURE`. [Dashboard browser workflow](dashboard.md#looking-at-a-change)
