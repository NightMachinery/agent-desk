// The desk inside Paseo: what waits on you, with answers and read marks.
import type { PluginTheme } from "@getpaseo/plugin";
import { useRpc } from "@getpaseo/plugin/client";
import { copyText, Markdown, TextInput, useToast } from "@getpaseo/plugin/client/ui";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Pressable, Text, View } from "react-native";
import { actRpc, type ChoiceQuestion, countsRpc, type DeskItem, viewRpc } from "../shared/desk";
import { openExternal } from "./web";

export const REFRESH_MS = 20_000;

type Styles = ReturnType<typeof makeStyles>;

function makeStyles(theme: PluginTheme, compact: boolean) {
  const c = theme.colors;
  return {
    page: { gap: compact ? 12 : 16, padding: compact ? 12 : 20 },
    section: { color: c.foregroundMuted, fontSize: 13, fontWeight: "600" as const, marginTop: 4 },
    card: { borderWidth: 1, borderColor: c.border, borderRadius: 10, padding: compact ? 10 : 14, gap: 8, backgroundColor: c.surface1 },
    title: { color: c.foreground, fontSize: 15, fontWeight: "600" as const },
    meta: { color: c.foregroundMuted, fontSize: 12 },
    text: { color: c.foreground },
    muted: { color: c.foregroundMuted },
    danger: { color: c.statusDanger },
    row: { flexDirection: "row" as const, flexWrap: "wrap" as const, gap: 8, alignItems: "center" as const },
    button: { paddingVertical: 7, paddingHorizontal: 12, borderRadius: 8, backgroundColor: c.surface2 },
    primary: { paddingVertical: 7, paddingHorizontal: 12, borderRadius: 8, backgroundColor: c.accent },
    buttonText: { color: c.foreground, fontSize: 13 },
    primaryText: { color: c.accentForeground, fontSize: 13 },
    option: { paddingVertical: 6, paddingHorizontal: 10, borderRadius: 8, borderWidth: 1, borderColor: c.border },
    optionOn: { paddingVertical: 6, paddingHorizontal: 10, borderRadius: 8, borderWidth: 1, borderColor: c.accent, backgroundColor: c.surface2 },
    input: { color: c.foreground, borderWidth: 1, borderColor: c.border, borderRadius: 8, padding: 8, minHeight: 40 },
    badge: { color: c.accentForeground, backgroundColor: c.accent, borderRadius: 9, paddingHorizontal: 6, fontSize: 12, overflow: "hidden" as const },
  };
}

function Button({ s, label, onPress, primary, disabled }: { s: Styles; label: string; onPress(): void; primary?: boolean; disabled?: boolean }) {
  return (
    <Pressable accessibilityRole="button" accessibilityLabel={label} disabled={disabled} onPress={onPress}
      style={[primary ? s.primary : s.button, disabled ? { opacity: 0.5 } : null]}>
      <Text style={primary ? s.primaryText : s.buttonText}>{label}</Text>
    </Pressable>
  );
}

// The answer text the desk expects for a set of choices: one line per
// question, "N. B: label", "N. B: label. Note: ...", "N. Other: ..." or
// "N. (no answer)", as the web dashboard writes it.
export function choiceAnswer(qs: readonly ChoiceQuestion[], picked: string[][], notes: string[]): string {
  return qs
    .map((q, i) => {
      const note = (notes[i] ?? "").trim();
      const keys = picked[i] ?? [];
      if (!keys.length) return `${i + 1}. ${note ? "Other: " + note : "(no answer)"}`;
      const os = q.options.filter((o) => keys.includes(o.key));
      const line = `${i + 1}. ${os.map((o) => o.key).join(", ")}: ${os.map((o) => o.label).join("; ")}`;
      return note ? `${line}. Note: ${note}` : line;
    })
    .join("\n");
}

function Choices({ s, qs, picked, setPicked, notes, setNotes }: {
  s: Styles; qs: readonly ChoiceQuestion[]; picked: string[][]; setPicked(p: string[][]): void; notes: string[]; setNotes(n: string[]): void;
}) {
  return (
    <View style={{ gap: 10 }}>
      {qs.map((q, i) => (
        <View key={i} style={{ gap: 6 }}>
          <Text style={s.text}>{`${i + 1}. ${q.text}`}</Text>
          <View style={s.row}>
            {q.options.map((o) => {
              const on = (picked[i] ?? []).includes(o.key);
              const toggle = () => {
                const cur = picked[i] ?? [];
                const next = on ? cur.filter((k) => k !== o.key) : q.multi ? [...cur, o.key] : [o.key];
                setPicked(qs.map((_, j) => (j === i ? next : picked[j] ?? [])));
              };
              return (
                <Pressable key={o.key} accessibilityRole="button" accessibilityLabel={`${o.key}. ${o.label}`} onPress={toggle} style={on ? s.optionOn : s.option}>
                  <Text style={s.text}>{`${o.key}. ${o.label}${o.key === q.recommended ? "  (recommended)" : ""}`}</Text>
                  {o.detail ? <Text style={s.meta}>{o.detail}</Text> : null}
                </Pressable>
              );
            })}
          </View>
          <TextInput style={s.input} placeholderTextColor={s.muted.color}
            placeholder={(picked[i] ?? []).length ? "A note on your pick (optional)" : "Or your own answer"}
            value={notes[i] ?? ""} onChangeText={(t) => setNotes(qs.map((_, j) => (j === i ? t : notes[j] ?? "")))} />
        </View>
      ))}
    </View>
  );
}

// What you sent from a card, shown in it until you leave the view: the desk
// keeps the thread, but this view lists only what waits on you, so an item
// you answered or wrote about would otherwise vanish with no trace.
export type Sent = { verb: "answer" | "reply"; text: string; output: string };
type Verb = "answer" | "reply" | "read" | "unread";

function ItemCard({ s, item, sent, gone, onDone }: {
  s: Styles; item: DeskItem; sent: Sent[]; gone: boolean; onDone(verb: Verb, text: string | undefined, output: string): void;
}) {
  const act = useRpc(actRpc);
  const queries = useQueryClient();
  const toast = useToast();
  const [text, setText] = useState("");
  const [more, setMore] = useState(false);
  const qs = item.choices ?? [];
  const [picked, setPicked] = useState<string[][]>(() => qs.map(() => []));
  const [notes, setNotes] = useState<string[]>(() => qs.map(() => ""));
  const send = useMutation({
    mutationFn: (v: { verb: Verb; text?: string }) => act({ verb: v.verb, target: item.id, text: v.text }),
    onSuccess: (r, v) => {
      onDone(v.verb, v.text, r.output);
      toast.show(v.verb === "answer" ? `Answered ${item.id}` : v.verb === "reply" ? "Message sent" : `Marked ${item.id} ${v.verb}`, { variant: "success" });
      setText("");
      void queries.invalidateQueries({ queryKey: ["agent-desk"] });
    },
    onError: (e) => toast.show(String((e as Error).message ?? e), { variant: "error" }),
  });

  const long = item.body.length > 700;
  const body = long && !more ? item.body.slice(0, 700) + "…" : item.body;
  const quick = item.default ? `Go ahead with your default: ${item.default}` : "Take your recommendation.";
  const answer = () => {
    const lines = qs.length ? choiceAnswer(qs, picked, notes) : "";
    const all = [lines, text.trim()].filter(Boolean).join("\n\n");
    send.mutate({ verb: "answer", text: all });
  };
  const ready = qs.length ? picked.some((p) => p.length) || notes.some((n) => n.trim()) || text.trim() !== "" : text.trim() !== "";

  return (
    <View style={s.card}>
      <Text style={s.title}>{`${item.unread ? "● " : ""}${item.title || item.kind}`}</Text>
      <Text style={s.meta}>{[item.id, item.agent, item.node, item.age, item.blocked ? "blocked" : ""].filter(Boolean).join(" · ")}</Text>
      {body ? <Markdown>{body}</Markdown> : null}
      {long ? <Button s={s} label={more ? "Less" : "More"} onPress={() => setMore(!more)} /> : null}
      {sent.map((m, i) => (
        <View key={i} style={{ gap: 2 }}>
          <Text style={s.text}>{`${m.verb === "answer" ? "You answered" : "You wrote"}: ${m.text}`}</Text>
          <Text style={s.meta}>{m.output || (m.verb === "answer" ? "Answer recorded." : "Message sent.")}</Text>
        </View>
      ))}
      {gone ? <Text style={s.meta}>{item.question ? "Settled. Kept here until you leave this view." : "Read. Kept here until you leave this view."}</Text> : null}
      {item.question && gone ? (
        <View style={s.row}><ItemLinks s={s} item={item} /></View>
      ) : item.question ? (
        <>
          {qs.length ? <Choices s={s} qs={qs} picked={picked} setPicked={setPicked} notes={notes} setNotes={setNotes} /> : null}
          <TextInput style={s.input} multiline placeholderTextColor={s.muted.color}
            placeholder={qs.length ? "Anything else (optional)" : "Your answer"} value={text} onChangeText={setText} />
          <View style={s.row}>
            <Button s={s} primary label="Answer" disabled={!ready || send.isPending} onPress={answer} />
            {qs.length ? (
              qs.some((q) => q.recommended) ? (
                <Button s={s} label="Pick the recommendations" onPress={() => setPicked(qs.map((q) => (q.recommended ? [q.recommended] : [])))} />
              ) : null
            ) : (
              <Button s={s} label={item.default ? "Go with the default" : "Take the recommendation"} disabled={send.isPending}
                onPress={() => send.mutate({ verb: "answer", text: quick })} />
            )}
            <Button s={s} label="Dismiss" disabled={send.isPending} onPress={() => send.mutate({ verb: "read" })} />
            <ItemLinks s={s} item={item} />
          </View>
        </>
      ) : (
        <>
          <TextInput style={s.input} multiline placeholderTextColor={s.muted.color} placeholder="Message its agent" value={text} onChangeText={setText} />
          <View style={s.row}>
            {text.trim() ? <Button s={s} primary label="Send" disabled={send.isPending} onPress={() => send.mutate({ verb: "reply", text })} /> : null}
            <Button s={s} label={item.unread ? "Mark read" : "Mark unread"} disabled={send.isPending}
              onPress={() => send.mutate({ verb: item.unread ? "read" : "unread" })} />
            <ItemLinks s={s} item={item} />
          </View>
        </>
      )}
      {send.error ? <Text style={s.danger}>{String((send.error as Error).message)}</Text> : null}
    </View>
  );
}

function ItemLinks({ s, item }: { s: Styles; item: DeskItem }) {
  return (
    <>
      {item.url ? <Button s={s} label="Dashboard" onPress={() => void openExternal(item.url!)} /> : null}
      <Button s={s} label="Copy id" onPress={() => void copyText(item.id)} />
    </>
  );
}

// DeskView lists the desk's items: everything, or one agent's (session is
// its Paseo agent id).
export function DeskView({ theme, compact, session }: { theme: PluginTheme; compact: boolean; session?: string }) {
  const s = useMemo(() => makeStyles(theme, compact), [theme, compact]);
  const view = useRpc(viewRpc);
  const [acted, setActed] = useState<Record<string, { item: DeskItem; section: string; sent: Sent[] }>>({});
  const q = useQuery({
    queryKey: ["agent-desk", "view", session ?? ""],
    queryFn: () => view({ session }),
    refetchInterval: REFRESH_MS,
  });
  if (q.error) {
    return (
      <View style={s.page}>
        <Text style={s.danger}>{`The desk could not be read: ${(q.error as Error).message}`}</Text>
        <Text style={s.muted}>The plugin runs agent-desk on the daemon's machine: $AGENT_DESK_BIN, ~/go/bin/agent-desk, or agent-desk on PATH.</Text>
      </View>
    );
  }
  if (!q.data) return <View style={s.page}><Text style={s.muted}>Reading the desk…</Text></View>;
  const { needs, deliverables, responses, fresh, dashboard } = q.data;
  // An item you acted on stays where it was until you leave the view, read,
  // with what you sent, even after the desk stops listing it.
  const section = (title: string, listed: DeskItem[]) => {
    const ids = new Set(listed.map((i) => i.id));
    const kept = Object.values(acted).filter((a) => a.section === title && !ids.has(a.item.id)).map((a) => ({ ...a.item, unread: false }));
    const list = [...listed, ...kept];
    const done = (i: DeskItem) => (verb: Verb, text: string | undefined, output: string) =>
      setActed((m) => {
        const prev = m[i.id];
        const add = (verb === "answer" || verb === "reply") && text ? [{ verb, text: text.trim(), output }] : [];
        return { ...m, [i.id]: { item: prev?.item ?? i, section: prev?.section ?? title, sent: [...(prev?.sent ?? []), ...add] } };
      });
    return list.length ? (
      <View style={{ gap: 10 }}>
        <Text style={s.section}>{`${title} (${listed.length})`}</Text>
        {list.map((i) => <ItemCard key={i.id} s={s} item={i} sent={acted[i.id]?.sent ?? []} gone={!ids.has(i.id)} onDone={done(i)} />)}
      </View>
    ) : null;
  };
  const keptAny = Object.keys(acted).length > 0;
  return (
    <View style={s.page}>
      {needs.length + deliverables.length + responses.length + fresh.length === 0 && !keptAny ? (
        <Text style={s.muted}>{session ? "Nothing from this agent waits on you." : "Nothing waits on you."}</Text>
      ) : null}
      {section("Needs you", needs)}
      {section("Deliverables", deliverables)}
      {section("Responses to you", responses)}
      {section("New", fresh)}
      {dashboard ? <View style={s.row}><Button s={s} label="Open the web dashboard" onPress={() => void openExternal(dashboard)} /></View> : null}
    </View>
  );
}

// useOpenCount is the number of questions waiting on the user.
export function useOpenCount(): number | undefined {
  const counts = useRpc(countsRpc);
  const q = useQuery({ queryKey: ["agent-desk", "counts"], queryFn: () => counts({}), refetchInterval: REFRESH_MS });
  return q.data?.open;
}

export function CountBadge({ theme, n }: { theme: PluginTheme; n: number | undefined }) {
  const s = useMemo(() => makeStyles(theme, false), [theme]);
  return n ? <Text style={s.badge}>{String(n)}</Text> : null;
}
