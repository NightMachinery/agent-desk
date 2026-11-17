# agent-desk

A channel from coding agents to the person running them.

When several agents work at once, their questions and reports end up buried in
transcripts, between tool calls, or in a subagent's output that nobody opens.
`agent-desk` gives agents one place to write what is meant for the user,
and gives the user one place to read it and answer.

- **Questions** are filed with their context, options and recommendation.
  They stay on an open list until answered, and the answer is typed back into
  the session that asked.
- **Slots** hold what is true now (status, next, done, velocity, findings) as short named
  boxes in a tree of project, run and agent. Writing a slot replaces it, so
  the board never accumulates history.
- **Read marks** hide what you have seen. A slot that changes comes back.
- Everything is an immutable event in one SQLite file, so nothing is lost and
  stores on several machines can be merged.

## Try it

    go install github.com/NightMachinery/agent-desk@latest

    agent-desk join rerun-nov/coord
    agent-desk ask --blocked --title 'Which split for the rerun?' <<'Q'
    ...context, options, recommendation...
    Q
    agent-desk set status 'Seeds 3/5 done. ETA 14:00.'
    agent-desk show
    agent-desk serve      # the dashboard, at http://127.0.0.1:7740/

Agents learn the rules (who writes, how to ask, slot discipline) from the
skill in [skills/agent-desk](skills/agent-desk/SKILL.md). Link or copy that
directory into your agents' skills directory, such as `~/.claude/skills/` or
`~/.agents/skills/`.

Built so far: the store, the agent and user commands, the terminal inbox,
rendered org files, notification hooks, answer delivery and merging stores (by
file or over ssh), done reports filed when a session ends, the agent skill,
and a web dashboard (`agent-desk serve`) that also messages agents.

- [docs/usage.md](docs/usage.md): how to use it.
- [docs/keyboard.md](docs/keyboard.md): driving the dashboard from the
  keyboard, evil-style (`SPC` leader, which-key, `M-x`, link hints).
- [docs/dashboard.md](docs/dashboard.md): changing the web dashboard.
- [docs/spec.md](docs/spec.md): the full design and the build order.

## License

MIT
