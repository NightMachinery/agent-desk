This is a task filed on agent-desk to be done when there was quota to spare: {{.Display}} "{{.Title}}". It is yours now.

{{.Brief}}

Before you start, join the desk where it was filed:

    agent-desk join {{.Run}}/{{.Role}} --project {{.Project}}

Keep your status slot current as you work. When you finish, report with `agent-desk done`, then close the task with `agent-desk task done {{.Display}}`. If you cannot finish it, say why with `agent-desk tell`, and leave the task open so it can be picked up again.
