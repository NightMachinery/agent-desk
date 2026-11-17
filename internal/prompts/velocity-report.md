Please send me a short executive report on your velocity for {{.Node}}: is the work progressing well?

Say what has been finished since your last report, what is in progress and how long it should still take, and how the pace compares with the plan. Name anything that slows you down or puts the result at risk, and what you would change about it. If it is going well, say so plainly.

Write it for a busy reader: a few short paragraphs or bullets in plain words, numbers where you have them. Then bring your velocity slot up to date.
{{- if .Velocity}}

Your velocity slot now says:

{{.Velocity}}
{{- end}}
