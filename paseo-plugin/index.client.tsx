import type {
  PluginAgentPanelProps,
  PluginButtonRegistration,
  PluginClientContext,
  PluginScreenProps,
  PluginSidebarItemProps,
} from "@getpaseo/plugin/client";
import { ScrollView, SidebarRow } from "@getpaseo/plugin/client/ui";
import { View } from "react-native";
import { CountBadge, DeskView, REFRESH_MS, useOpenCount } from "./client/desk";
import { countsRpc } from "./shared/desk";

function DeskScreen({ theme, layout }: PluginScreenProps) {
  return (
    <ScrollView style={{ flex: 1, backgroundColor: theme.colors.surface0 }}>
      <DeskView theme={theme} compact={layout.compact} />
    </ScrollView>
  );
}

function AgentDeskPanel({ theme, layout, params }: PluginAgentPanelProps) {
  return (
    <View style={{ flex: 1, backgroundColor: theme.colors.surface0 }}>
      <ScrollView style={{ flex: 1 }}>
        <DeskView theme={theme} compact={layout.compact} session={params.agentId} />
      </ScrollView>
    </View>
  );
}

function DeskItem({ theme, currentScreen, openScreen }: PluginSidebarItemProps) {
  const open = useOpenCount();
  return (
    <SidebarRow
      icon="Inbox"
      label="Agent desk"
      active={currentScreen?.screenId === "desk"}
      trailing={<CountBadge theme={theme} n={open} />}
      onPress={() => openScreen({ screenId: "desk" })}
    />
  );
}

// A pill on the composer of each agent with questions waiting on the user,
// opening that agent's desk panel. Counts come from one poll for all agents.
function contributePills(client: PluginClientContext) {
  const pills = new Map<string, { workspaceId: string; pill: PluginButtonRegistration }>();
  const lifetime = new AbortController();
  let counts: Record<string, number> = {};
  const show = (agentId: string) => {
    const p = pills.get(agentId);
    const n = counts[agentId] ?? 0;
    p?.pill.update({ visible: n > 0, label: `Desk ${n}` });
  };
  const register = (agent: { id: string; workspaceId?: string | null }) => {
    if (lifetime.signal.aborted || !agent.workspaceId) return;
    const agentId = agent.id;
    const workspaceId = agent.workspaceId;
    pills.get(agentId)?.pill.remove();
    const pill = client.addComposerPill({
      id: "desk",
      workspaceId,
      agentId,
      button: {
        title: "Questions this agent asked you on the desk",
        icon: "Inbox",
        label: "Desk",
        visible: false,
        behavior: { kind: "action", onPress: () => client.openPanel("desk", { workspaceId, agentId }) },
      },
    });
    pills.set(agentId, { workspaceId, pill });
    show(agentId);
  };
  const removeAll = () => {
    for (const p of pills.values()) p.pill.remove();
    pills.clear();
  };
  void client.paseo.agents
    .list({ subscribe: {}, signal: lifetime.signal })
    .then(({ subscription }) => {
      subscription.subscribe({
        snapshot: ({ entries }) => {
          removeAll();
          for (const { agent } of entries) register(agent);
        },
        update: (message) => {
          if (message.type !== "agent_update") return;
          const update = message.payload;
          if (update.kind === "upsert") return register(update.agent);
          pills.get(update.agentId)?.pill.remove();
          pills.delete(update.agentId);
        },
      });
      return undefined;
    })
    .catch((error) => {
      if (!lifetime.signal.aborted) console.error("agent-desk: agent observation failed", error);
    });
  const poll = async () => {
    try {
      counts = (await client.rpc(countsRpc, {})).byAgent;
      for (const id of pills.keys()) show(id);
    } catch (error) {
      console.error("agent-desk: counts failed", error);
    }
  };
  void poll();
  const timer = setInterval(poll, REFRESH_MS);
  return () => {
    clearInterval(timer);
    lifetime.abort();
    removeAll();
  };
}

export default function contribute(client: PluginClientContext) {
  client.addScreen({ id: "desk", title: "Agent desk", Component: DeskScreen });
  client.addSidebarFooterItem({ id: "desk", title: "Agent desk", Component: DeskItem });
  client.addAgentPanel({ id: "desk", title: "Desk", icon: "Inbox", Component: AgentDeskPanel });
  client.addCommandCenterItem({
    id: "open-desk",
    title: "Agent desk: what waits on you",
    icon: "Inbox",
    keywords: ["questions", "answers", "agent-desk"],
    context: "global",
    onSelect: ({ openScreen }) => openScreen({ screenId: "desk" }),
  });
  client.addCommandCenterItem({
    id: "open-agent-desk",
    title: "Agent desk: this agent's items",
    icon: "Inbox",
    keywords: ["questions", "agent-desk"],
    context: "agent",
    onSelect: ({ openPanel }) => openPanel("desk"),
  });
  const stopPills = contributePills(client);
  return () => stopPills();
}
