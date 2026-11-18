Your subagent {{.Title}} ({{.ID}}) stopped at its usage limit. Please keep its work moving without it.

Use the quota-management skill to pick a provider and account that has quota left now, and start a replacement there with the same role. Before you archive {{.ID}}, recover its partial work: read its transcript and its working directory ({{.Cwd}}) for what it finished, what it left half done, its uncommitted changes and any plan or state files, and brief the replacement on all of it so nothing is redone or lost.
{{- if .Children}}

It has subagents of its own: {{.Children}}. Do not stop the ones that still have quota; tell each that it now reports to the replacement (or to you).
{{- end}}

If waiting for its quota to reset is the better choice, say so and change nothing.
