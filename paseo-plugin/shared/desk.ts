// The contracts between the Paseo app and the daemon side, which runs the
// agent-desk command on the daemon's machine.
import { defineRpc } from "@getpaseo/plugin";
import { z } from "zod";

export const choiceQuestion = z.object({
  text: z.string(),
  options: z.array(z.object({ key: z.string(), label: z.string(), detail: z.string().optional() })),
  recommended: z.string().optional(),
  multi: z.boolean().optional(),
});
export type ChoiceQuestion = z.infer<typeof choiceQuestion>;

export const deskItem = z.object({
  id: z.string(), // display id: q-..., t-...
  kind: z.string(),
  title: z.string(),
  body: z.string(),
  node: z.string(),
  agent: z.string(), // the role label the desk shows
  paseo: z.string().optional(), // the Paseo agent id behind it
  age: z.string(),
  unread: z.boolean(),
  question: z.boolean(),
  blocked: z.boolean(),
  default: z.string().optional(),
  choices: z.array(choiceQuestion).optional(),
  url: z.string().optional(), // its page on the web dashboard, with base_url set
});
export type DeskItem = z.infer<typeof deskItem>;

export const viewRpc = defineRpc({
  name: "desk.view",
  // session: a Paseo agent id or native session id; omitted for the whole desk.
  input: z.object({ session: z.string().optional() }),
  output: z.object({
    needs: z.array(deskItem),
    responses: z.array(deskItem),
    fresh: z.array(deskItem),
    dashboard: z.string().optional(),
  }),
});

export const countsRpc = defineRpc({
  name: "desk.counts",
  input: z.object({}),
  // open: questions waiting on the user; byAgent: the same per Paseo agent id.
  output: z.object({ open: z.number(), byAgent: z.record(z.string(), z.number()) }),
});

export const actRpc = defineRpc({
  name: "desk.act",
  input: z.object({
    verb: z.enum(["answer", "reply", "read", "unread"]),
    target: z.string(), // an item id, or a node path for reply
    text: z.string().optional(),
  }),
  output: z.object({ output: z.string() }),
});
