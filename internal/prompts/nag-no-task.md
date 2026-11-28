{{- /*
  Printed by `agent-desk hook prompt` (the UserPromptSubmit hook) into the
  agent's prompt when a message arrives and the agent has no started task at
  its node. Settings > Reminders to agents chooses which messages. Fields:
  .Node is the agent's node; .Occasion is user, desk, heartbeat or agent;
  .Open lists up to three of the node's open tasks, each with .ID and .Title.
  Keep it to one line: the agent reads it with every such message.
*/ -}}
agent-desk: you have no started task at {{.Node}}.{{if .Open}} Open here: {{range $i, $t := .Open}}{{if $i}}; {{end}}{{$t.ID}} {{$t.Title}}{{end}}.{{end}} If this message asks for work, file it (agent-desk task add --title ... --brief ...) or pick an open one, and run agent-desk task start ID before you begin, so the user sees what you are on.
