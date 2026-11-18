You are taking over from the Paseo agent {{.ID}} ("{{.Title}}"), which stopped at its usage limit. Finish its task.

First recover its work, so nothing is redone or lost: run `paseo inspect {{.ID}}`, read its transcript, and look in its working directory ({{.Cwd}}) for what it was asked to do, what it finished, what was in progress, its uncommitted changes, and any plan, state or checklist files it kept.
{{- if eq (len .Nodes) 1}}

It kept the agent-desk node {{.Node}}. Take it over: `agent-desk join {{.Join}} --project {{.Project}} --takeover`.
{{- else if .Nodes}}

It held {{len .Nodes}} agent-desk nodes. Take over each of them:
{{range .Nodes}}
    agent-desk join {{.Join}} --project {{.Project}} --takeover
{{- end}}

Leave none of them: the takeover already drops them from the stopped session. While you hold several nodes, every agent-desk command that writes to (or reads) "your node" needs `--node PATH` to say which one, for example `--node {{(index .Nodes 0).Path}}`.
{{- end}}
{{- if .Parent}}

It reported to the Paseo agent {{.Parent}}. Tell that agent you replace {{.ID}}, and report to it from now on.
{{- end}}
{{- if .Children}}

Its subagents ({{.Children}}) now report to you. Tell each of them.
{{- end}}

Once you have everything you need from it, archive it with `paseo archive {{.ID}}`, then carry on with the work.
