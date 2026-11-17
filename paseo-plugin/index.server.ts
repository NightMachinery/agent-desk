import type { PluginServerContext } from "@getpaseo/plugin/server";
import { act, counts, view } from "./server/desk";
import { actRpc, countsRpc, viewRpc } from "./shared/desk";

export default function contribute(server: PluginServerContext) {
  server.handle(viewRpc, view);
  server.handle(countsRpc, counts);
  server.handle(actRpc, act);
  return () => {};
}
