{{- /*
  Printed by `agent-desk task done` to a coordinator (an agent holding a
  run's top node, or a node with nodes under it) when its velocity slot is
  missing or older than Settings > Reminders to agents allows. Fields:
  .Node is the agent's node; .Task the task just finished (w-...); .Age how
  long ago the velocity slot was set, empty when there is none.
*/ -}}
agent-desk: {{if .Age}}the velocity slot at {{.Node}} was last set {{.Age}} ago{{else}}{{.Node}} has no velocity slot yet{{end}}. With {{.Task}} done, bring it up to date: recent pace, where tokens went (rework, polling, idle waits, oversized contexts, review fan-outs) and one to three suggestions; write "unknown" rather than guess (agent-desk set velocity < file, 15 lines).
