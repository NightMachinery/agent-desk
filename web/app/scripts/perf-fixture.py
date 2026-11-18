"""Generate an isolated 7,500-event fixture and inert Paseo records.

Run with the path to a new scratch directory. Import fixture.jsonl using
the fixture configuration as described in docs/performance.md.
"""

import datetime as dt
import json
import pathlib
import random
import sys
import uuid

if len(sys.argv) != 2:
    raise SystemExit("usage: python3 web/app/scripts/perf-fixture.py NEW_SCRATCH_DIR")
root = pathlib.Path(sys.argv[1])
root.mkdir(parents=True)
(root / "paseo/agents").mkdir(parents=True)
r = random.Random(42)
out = []
now = dt.datetime(2026, 1, 5, tzinfo=dt.timezone.utc)
letters = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
text = "The preview is ready. Review the test results and confirm the next step."
markdown = "The **preview** is ready. Review [the example](https://example.org/) and confirm the next step.\n\n- Check the main case\n- Check the follow-up case"
def add(kind, path, meta=None, **fields):
    timestamp = now + dt.timedelta(seconds=7 * len(out))
    n = (int(timestamp.timestamp() * 1000) << 80) | r.getrandbits(80)
    event_id = "".join(letters[(n >> (i * 5)) & 31] for i in range(25, -1, -1))
    out.append(dict(id=event_id, ts=timestamp.isoformat(), host="fixture-host", kind=kind, path=path, meta=meta or {}, **fields))
    return event_id

nodes = []
for i in range(60):
    path = f"project-{i//12}/run-{i//2%6}/coord" + ("/review" if i%2 else "")
    session = str(uuid.UUID(int=i+1, version=4))
    paseo = str(uuid.UUID(int=i+1000, version=4))
    actor = {"from": {"session": session, "paseo": paseo, "agent": "codex", "cwd": "/example/project"}}
    nodes.append((path, actor))
    (root / "paseo/agents" / f"{paseo}.json").write_text(json.dumps({
        "id": paseo, "title": f"Coordinator {i}", "provider": "codex", "lastStatus": "idle",
        "lastActivityAt": now.isoformat(), "persistence": {"nativeHandle": session},
    }))
    add("join", path, actor)

path, actor = nodes[0]
head = add("tell", path, actor, title="Review the long thread", body=markdown)
for i in range(90):
    m = add("reply", path, {"to": actor["from"], "to_host": "fixture-host"}, ref=head, body=markdown)
    if i % 13 == 0:
        add("undelivered", path, {"error": "Fixture failure"}, ref=m)
    add("delivered", path, {"via": "fixture"}, ref=m)
    add("tell", path, actor, ref=m, title="Follow-up", body=markdown)
    if i % 4:
        add("read", path, ref=out[-1]["id"])

for j in range(10000):
    if len(out) >= 7500:
        break
    path, actor = r.choice(nodes)
    x = r.randrange(100)
    if x < 46:
        slot = r.choice(("status", "status", "next", "findings", "done", "state"))
        kind = r.choices(("set", "clear", "link"), (94, 5, 1))[0]
        if kind == "set":
            meta = actor | ({"audience": "agents"} if slot == "state" else {})
            add(kind, f"{path}/{slot}", meta, body=text)
        elif kind == "link":
            add(kind, f"{path}/{slot}", actor | {"file": "/example/missing"})
        else:
            add(kind, f"{path}/{slot}", actor)
    elif x < 70:
        add("instruction", path, actor, body=text * r.randint(1, 10))
    elif x < 88:
        t = add(r.choice(("tell", "tell", "done")), path, actor, title="Fixture update", body=markdown)
        if r.random() < 0.82:
            add("read", path, ref=t)
    elif x < 95:
        q = add("ask", path, actor, level=r.choice(("default", "blocked")), title="Fixture question", body=markdown)
        if r.random() < 0.85:
            a = add("answer", path, {"source": "web"}, ref=q, body=text)
            add("delivered", path, {"via": "fixture"}, ref=a)
    else:
        m = add("reply", path, {"to": actor["from"], "to_host": "fixture-host"}, body=text)
        add("delivered", path, {"via": "fixture"}, ref=m)
        if r.random() < 0.75:
            t = add("tell", path, actor, ref=m, title="Fixture response", body=text)
            if r.random() < 0.75:
                add("read", path, ref=t)

(root / "fixture.jsonl").write_text("".join(json.dumps(e) + "\n" for e in out))
(root / "item-id").write_text(head)
(root / "session-id").write_text(nodes[0][1]["from"]["session"])
(root / "config.toml").write_text("""
notify_command = "true"
dismiss_command = "true"
deliver_command = "true"
[quota]
stopped_command = "printf '%s\\\\n' '00000000-0000-4000-8000-0000000003e8'"
""")
print(f"Generated {len(out)} events for {len(nodes)} nodes.")
