#!/bin/zsh -f
# Builds a desk of fabricated data for looking at the dashboard and the inbox:
# three projects, open questions (one from another host), notices, slots, a
# done report and an old quiet run.
#
#   scripts/fixture.sh DIR      # the desk goes to DIR/desk; DIR/src holds fake repositories
#
# AGENT_DESK_BIN names the binary (default: agent-desk on PATH). Then:
#   AGENT_DESK_HOME=DIR/desk AGENT_DESK_HOST=host-a agent-desk serve --listen 127.0.0.1:7792
set -e
[[ $# -eq 1 && -n "${1}" ]] || { print -u2 "usage: fixture.sh DIR"; exit 2 }
dir=${1:A}
B=${AGENT_DESK_BIN:-agent-desk}
export AGENT_DESK_HOME=${dir}/desk AGENT_DESK_HOST=host-a AI_AGENT=claude-code_x
unset PASEO_AGENT_ID TMUX_PANE TMUX TMUX_SUBAGENT_NODE
[[ -e ${AGENT_DESK_HOME} ]] && { print -u2 "fixture.sh: ${AGENT_DESK_HOME} exists; pick a new DIR"; exit 1 }
root=${dir}/src
mkdir -p ${root}/example-proj/.git ${root}/site/.git ${root}/paper-figs/.git
s() { CLAUDE_CODE_SESSION_ID="00000000-0000-4000-8000-00000000000${1}" "${@:2}" }
cd ${root}/example-proj
s 1 ${B} join rerun-nov/coord >/dev/null
s 1 ${B} set status 'Seeds 3/5 done. ETA 14:00.' >/dev/null
s 1 ${B} set findings $'Seed 2 diverges at step 400.\nSeeds 1 and 3 match the baseline within 0.2%.' >/dev/null
s 1 ${B} set next $'Finish seeds 4 and 5.\nWrite the comparison table.' >/dev/null
s 1 ${B} ask --blocked --title 'Which split for the rerun?' >/dev/null <<'Q'
The rerun needs a train/test split, and the cached one predates the
label fix in `prep.py`.

- **A. Reuse the cached split.** Fast, but scores are not comparable with
  runs after the fix.
- **B. Regenerate it.** About an hour longer; consistent with the fix.

**I recommend B**: the rerun exists to compare against post-fix runs.
Q
s 2 ${B} join rerun-nov/coord/eval-lead >/dev/null
s 2 ${B} set status 'Evaluating seed 2 on the held-out set.' >/dev/null
s 3 ${B} join rerun-nov/coord/eval-lead/plots >/dev/null
s 3 ${B} set status 'Plotting loss curves.' >/dev/null
s 3 ${B} set artifacts $'/tmp/example/plots/loss-seed2.png\n/tmp/example/plots/loss-all.png' >/dev/null
s 2 ${B} tell --title 'Seed 2 diverged: bad shard' >/dev/null <<'R'
Shard 17 has duplicated rows; seed 2 drew it. Re-running without it.
R
cd ${root}/site
s 4 ${B} join adhoc/deploy >/dev/null
s 4 ${B} set status 'Waiting on DNS.' >/dev/null
s 4 ${B} ask --default 'keep the legacy feed for a month' --title 'Drop the legacy feed?' >/dev/null <<'Q'
Nobody has fetched the legacy feed for 30 days.
Dropping it simplifies the build. I will keep it a month unless told otherwise.
Q
s 4 ${B} tell --title 'deploy ok' </dev/null >/dev/null
cd ${root}/paper-figs
s 5 ${B} join figs-oct/coord >/dev/null
s 5 ${B} set status 'Finished; see report.' >/dev/null
# A done report, an old quiet run and another host's run, as raw events.
python3 - <<'PY' | ${B} import >/dev/null 2>&1
import json, datetime, random
A = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
def ulid(ts):
    ms = int(datetime.datetime.fromisoformat(ts).timestamp() * 1000)
    t = "".join(A[(ms >> (5 * (9 - i))) & 31] for i in range(10))
    return t + "".join(random.choice(A) for _ in range(16))
def ev(i, ts, host, kind, path, **kw):
    kw["id"] = ulid(ts)
    m = {"from": {"agent": "claude", "session": "00000000-0000-4000-8000-0000000000%02d" % i}, "source": "cli"}
    m.update(kw.pop("meta", {}))
    e = {"id": kw.pop("id"), "ts": ts, "host": host, "kind": kind, "path": path, "meta": m}
    e.update(kw); print(json.dumps(e))
now = datetime.datetime.now().astimezone()
def t(**d): return (now - datetime.timedelta(**d)).isoformat(timespec="milliseconds")
ev(5, t(seconds=0), "host-a", "done", "paper-figs/figs-oct/coord", id="01KZZZZZZZZZZZZZZZZZZZDN01", title="Figures for the October draft", body="All six figures regenerated.", meta={"report": "/tmp/example/report.org"})
ev(6, t(days=5), "host-a", "join", "paper-figs/old-run/coord", id="01JA0000000000000000000Q01")
ev(6, t(days=5), "host-a", "set", "paper-figs/old-run/coord/status", id="01JA0000000000000000000Q02", body="Paused until the reviewers reply.")
ev(7, t(hours=2), "host-b", "join", "example-proj/gpu-sweep/coord", id="01KY0000000000000000000H01")
ev(7, t(hours=2), "host-b", "set", "example-proj/gpu-sweep/coord/status", id="01KY0000000000000000000H02", body="Sweep 12/40 on the remote box.")
ev(7, t(hours=1), "host-b", "ask", "example-proj/gpu-sweep/coord", id="01KY0000000000000000000H03", level="blocked", title="Raise the budget cap?", body="The sweep will exceed the cap at run 30.\nA. Raise it.\nB. Stop at 30.\nI recommend B.")
PY
echo fixture ready
