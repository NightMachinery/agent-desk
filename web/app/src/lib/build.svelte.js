// Whether this page runs an older dashboard than the server serves. The
// event stream names the server's build on every (re)connection; the first
// one this page sees is its own (it was just loaded from that server), and
// a different one later means the server was upgraded while the page stayed
// open, still running the old code. ListPane then offers to reload.
export const build = $state({ mine: '', served: '', stale: false });

export function sawBuild(id) {
  if (!id) return;
  if (!build.mine) build.mine = id;
  build.served = id;
  build.stale = build.served !== build.mine;
}
