{{- /* Sent to a stopped Paseo agent by Resume (Stopped by an error, or Out of
quota): .ID and .Title name it, .Error is what its failed turn said (empty
when it stopped at a usage limit), .Login whether that is a login error. */ -}}
Continue: your last turn stopped {{if .Error}}with an error ("{{.Error}}"){{else}}at your usage limit{{end}}{{if .Login}}, and the user has logged in again{{end}}. The user asked you to resume. Pick up the work that turn was doing, checking first what it finished, so nothing is done twice.
