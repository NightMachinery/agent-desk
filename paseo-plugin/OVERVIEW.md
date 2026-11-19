# Agent desk

Shows what your agents filed for you on [agent-desk](https://github.com/NightMachinery/agent-desk), a desk where coding agents leave questions, reports and status for their user, and lets you act on it without leaving Paseo.

- **Agent desk** in the sidebar footer opens a screen with everything that waits on you: open questions, responses to your messages and new items. The row shows how many questions are open.
- Answer a question in place, including multiple-choice questions (pick options, add a note, or write your own answer), take the agent's default or recommendation, or dismiss it. Message the agent behind any other item, or mark it read.
- Messaging an item's agent marks the item read. An item you answered, messaged, read or dismissed stays in place, with what you sent, until you leave the view; the agent's response arrives under **Responses to you**.
- An agent with open questions gets a **Desk** pill on its composer, which opens a panel with only that agent's items. The Command Center has both views too.
- Each item links to its page on the web dashboard when the desk's `base_url` is set.

## Setup

The daemon side runs the `agent-desk` command on the daemon's machine: `$AGENT_DESK_BIN` if set, else `~/go/bin/agent-desk`, else `agent-desk` on the daemon's `PATH`. Install it with `go install github.com/NightMachinery/agent-desk@latest`. It reads the desk at `$AGENT_DESK_HOME` (default `~/.agents/desk`) and the dashboard address from `~/.config/agent-desk/config.toml`.

Answers and messages are delivered by agent-desk itself, the same way as from its terminal inbox or web dashboard. The plugin reads the desk every 20 seconds while a view is open and sends nothing anywhere else.

To turn it off, disable it in **Settings → Plugins** or run `paseo plugin disable agent-desk`.
