// Runs the agent-desk command on the daemon's machine.
import { spawn } from "node:child_process";
import { existsSync, readFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import type { RpcInput, RpcOutput } from "@getpaseo/plugin";
import type { actRpc, countsRpc, DeskItem, viewRpc } from "../shared/desk";

// The binary: $AGENT_DESK_BIN, else ~/go/bin/agent-desk (where `go install`
// puts it, often missing from the daemon's PATH), else agent-desk on PATH.
function program(): string {
  const env = process.env.AGENT_DESK_BIN;
  if (env) return env;
  const installed = join(homedir(), "go", "bin", "agent-desk");
  return existsSync(installed) ? installed : "agent-desk";
}

// run executes agent-desk with args. Standard input is always closed, since
// some commands read it when given no text; input, if any, is written first.
export function run(args: string[], input?: string): Promise<string> {
  return new Promise((resolve, reject) => {
    const child = spawn(program(), args, { stdio: ["pipe", "pipe", "pipe"] });
    let out = "";
    let err = "";
    child.stdout.on("data", (b: Buffer) => (out += b.toString()));
    child.stderr.on("data", (b: Buffer) => (err += b.toString()));
    child.on("error", reject);
    child.on("close", (code) => {
      if (code === 0) resolve(out);
      else reject(new Error((err || out).trim().split("\n").pop() || `agent-desk exited ${code}`));
    });
    child.stdin.end(input ?? "");
  });
}

// baseURL is the dashboard's address from the desk's config, if set.
function baseURL(): string | undefined {
  const file = process.env.AGENT_DESK_CONFIG || join(homedir(), ".config", "agent-desk", "config.toml");
  try {
    const m = /^base_url\s*=\s*"([^"]+)"/m.exec(readFileSync(file, "utf8"));
    return m ? m[1].replace(/\/*$/, "/") : undefined;
  } catch {
    return undefined;
  }
}

type Json = Record<string, any>;
const slotKinds = new Set(["set", "link", "clear"]);

function itemOf(raw: Json, base: string | undefined): DeskItem {
  const e: Json = raw.event ?? {};
  const meta: Json = raw.question?.meta ?? e.meta ?? {};
  const node = slotKinds.has(e.kind) ? String(e.path).replace(/\/[^/]*$/, "") : String(e.path);
  const isQ = e.kind === "ask";
  const url = base
    ? `${base}#/p/${node.split("/").map(encodeURIComponent).join("/")}?item=${encodeURIComponent(raw.display_id)}`
    : undefined;
  return {
    id: String(raw.display_id),
    kind: String(e.kind),
    title: String(e.title ?? ""),
    body: String(e.body ?? ""),
    node,
    agent: String(raw.label ?? ""),
    paseo: meta.from?.paseo ?? e.meta?.from?.paseo ?? undefined,
    age: String(raw.age ?? ""),
    unread: Boolean(raw.unread),
    question: isQ,
    blocked: isQ && e.level === "blocked",
    default: meta.default || undefined,
    choices: meta.choices?.questions ?? undefined,
    url,
  };
}

function items(groups: Json[] | null | undefined, base: string | undefined): DeskItem[] {
  return (groups ?? []).flatMap((g) => (g.items ?? []).map((i: Json) => itemOf(i, base)));
}

async function show(session?: string): Promise<Json> {
  const args = ["show", "--format", "json"];
  if (session) args.push("--session", session);
  return JSON.parse(await run(args));
}

export async function view({ session }: RpcInput<typeof viewRpc>): Promise<RpcOutput<typeof viewRpc>> {
  const v = await show(session);
  const base = baseURL();
  return {
    needs: items(v.needs_you, base),
    deliverables: items(v.deliverables ?? [], base),
    responses: items(v.responses, base),
    fresh: items(v.new, base),
    dashboard: base,
  };
}

export async function counts(_: RpcInput<typeof countsRpc>): Promise<RpcOutput<typeof countsRpc>> {
  const needs = items((await show()).needs_you, undefined).filter((i) => i.question);
  const byAgent: Record<string, number> = {};
  for (const i of needs) if (i.paseo) byAgent[i.paseo] = (byAgent[i.paseo] ?? 0) + 1;
  return { open: needs.length, byAgent };
}

export async function act({ verb, target, text }: RpcInput<typeof actRpc>): Promise<RpcOutput<typeof actRpc>> {
  if ((verb === "answer" || verb === "reply") && !text?.trim()) throw new Error(`${verb} needs text`);
  const out = await run([verb, target], text ? text.trim() + "\n" : undefined);
  // Writing to an item's agent means you have read it, as opening it does on
  // the dashboard. Best effort: the message itself already went out.
  if (verb === "reply" && !target.includes("/")) await run(["read", target]).catch(() => undefined);
  return { output: out.trim() };
}
