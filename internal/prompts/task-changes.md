{{- define "advice"}}
{{- if .Note}} My note: {{.Note}}{{end}}
{{- if eq .Now "open"}} Pick it up again{{if .Note}} and deal with what the note says{{end}}, or tell me why not.
{{- else if eq .Now "done"}} Stop working on it; if something is still left, tell me.
{{- else if eq .Now "dropped"}} Stop working on it and do not finish it.
{{- else if eq .Now "later"}} Leave it for now, until I reopen it.
{{- else if eq .Now "deferred"}} Leave it until there is quota to spare.
{{- end}}{{end -}}
{{- define "state"}}{{if eq . "later"}}deferred{{else if eq . "deferred"}}deferred for quota{{else}}{{.}}{{end}}{{end -}}
{{- define "verb"}}{{if eq .Now "open"}}reopened{{else if eq .Now "done"}}marked done{{else if eq .Now "started"}}marked started{{else}}{{template "state" .Now}}{{end}}{{end -}}
{{if eq (len .Changes) 1 -}}
{{with index .Changes 0}}I {{if eq .Now "open"}}reopened this task{{else if eq .Now "done"}}marked this task done{{else if eq .Now "started"}}marked this task started{{else if eq .Now "later"}}deferred this task{{else if eq .Now "deferred"}}deferred this task for quota{{else}}{{.Now}} this task{{end}} (it was {{template "state" .Was}}).{{template "advice" .}}{{end}}
{{- else -}}
I changed {{len .Changes}} tasks you work on:
{{range .Changes}}
- {{.Task}} "{{.Title}}": {{template "verb" .}} (it was {{template "state" .Was}}).{{template "advice" .}}
{{- end}}
{{- end}}

A one-line acknowledgement (with --quiet) is enough unless you disagree.
