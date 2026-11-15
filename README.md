# agent-desk

A channel from coding agents to the person running them.

When several agents work at once, their questions and reports end up buried in
transcripts, between tool calls, or in a subagent's output that nobody opens.
`agent-desk` gives agents one place to write what is meant for the user,
and gives the user one place to read it and answer.

- **Questions** are filed with their context, options and recommendation.
  They stay on an open list until answered, and the answer is typed back into
  the session that asked.
- **Slots** hold what is true now (status, findings, artifacts) as short named
  boxes in a tree of project, run and agent. Writing a slot replaces it, so
  the board never accumulates history.
- **Read marks** hide what you have seen. A slot that changes comes back.
- Everything is an immutable event in one SQLite file, so nothing is lost and
  stores on several machines can be merged.

## Status

Design stage. Nothing is implemented yet; the commands below are the plan.

    agent-desk ask --blocked --title 'Which split for the rerun?' <<'Q'
    ...context, options, recommendation...
    Q
    agent-desk set status 'Seeds 3/5 done. ETA 14:00.'
    agent-desk inbox

The full design and the build order are in [docs/spec.md](docs/spec.md).

## License

MIT
