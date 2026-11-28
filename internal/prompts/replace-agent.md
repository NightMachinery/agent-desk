You are taking over from the Paseo agent {{.ID}} ("{{.Title}}"), which stopped {{if .Error}}with an error: "{{.Error}}"{{else}}at its usage limit{{end}}. Finish its task.

First recover its work, so nothing is redone or lost: run `paseo inspect {{.ID}}`, read its transcript, and look in its working directory ({{.Cwd}}) for what it was asked to do, what it finished, what was in progress, its uncommitted changes, and any plan, state or checklist files it kept.
{{- if .Nodes}}

It held the agent-desk {{if eq (len .Nodes) 1}}node {{.Node}}{{else}}nodes {{range $i, $n := .Nodes}}{{if $i}}, {{end}}{{$n.Path}}{{end}}{{end}}. Take {{if eq (len .Nodes) 1}}it{{else}}them all{{end}} over in one step: `agent-desk takeover {{.ID}}`.
{{- if gt (len .Nodes) 1}} While you hold several nodes, every agent-desk command that writes to (or reads) "your node" needs `--node PATH` to say which one, for example `--node {{(index .Nodes 0).Path}}`.{{end}}
{{- end}}
{{- if .Parent}}

It reported to the Paseo agent {{.Parent}}. Tell that agent you replace {{.ID}}, and report to it from now on.
{{- end}}
{{- if .Children}}

Its subagents ({{.Children}}) now report to you. Tell each of them.
{{- end}}

Once you have everything you need from it, archive it with `paseo archive {{.ID}}`, then carry on with the work.
