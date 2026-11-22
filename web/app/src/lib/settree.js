// The set editor's tree of desk paths (projects, runs, roles) and its search.

/**
 * @typedef {{ path: string, name: string, depth: number, children: TreeNode[] }} TreeNode
 */

/** Whether path is prefix or below it, by whole segments. */
export const under = (path, prefix) => path === prefix || path.startsWith(prefix + '/');

/** The strict ancestors of path, nearest last: a/b/c gives a, a/b. */
export function ancestors(path) {
  const out = [];
  for (let i = path.indexOf('/'); i >= 0; i = path.indexOf('/', i + 1)) out.push(path.slice(0, i));
  return out;
}

/**
 * The tree of paths, siblings in name order. A missing ancestor is added, so
 * the order of paths does not matter.
 * @param {string[]} paths
 * @returns {TreeNode[]} the projects
 */
export function buildTree(paths) {
  /** @type {Map<string, TreeNode>} */
  const nodes = new Map();
  /** @type {TreeNode[]} */
  const roots = [];
  /** @param {string} path @returns {TreeNode} */
  const get = (path) => {
    let n = nodes.get(path);
    if (n) return n;
    const i = path.lastIndexOf('/');
    const parent = i < 0 ? null : get(path.slice(0, i));
    n = { path, name: path.slice(i + 1), depth: parent ? parent.depth + 1 : 0, children: [] };
    nodes.set(path, n);
    (parent ? parent.children : roots).push(n);
    return n;
  };
  for (const p of paths) if (p) get(p);
  const sort = (/** @type {TreeNode[]} */ list) => {
    list.sort((a, b) => (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
    for (const n of list) sort(n.children);
  };
  sort(roots);
  return roots;
}

/** The words of a search, lower case. */
export const words = (query) => query.trim().toLowerCase().split(/\s+/).filter(Boolean);

/**
 * Whether node is a hit for the words: every word is in its path, and one of
 * them reaches into its own name. So "agent" finds the project agent-desk
 * but not every role under it, "agent-desk coord" finds that project's coord
 * roles, and "desk/build" finds the run build.
 * @param {TreeNode} node
 * @param {string[]} ws
 */
export function hit(node, ws) {
  const full = node.path.toLowerCase();
  const start = full.length - node.name.length;
  let own = false;
  for (const w of ws) {
    const at = full.lastIndexOf(w);
    if (at < 0) return false;
    if (at + w.length > start) own = true;
  }
  return own;
}

/**
 * What a search shows: the hits, everything under a hit, and the ancestors
 * of a hit, which open on their own so the hit is in view.
 * @param {TreeNode[]} roots
 * @param {string} query
 * @returns {null | { show: Set<string>, open: Set<string>, hits: Set<string> }} null for no search
 */
export function search(roots, query) {
  const ws = words(query);
  if (!ws.length) return null;
  const show = new Set(), open = new Set(), hits = new Set();
  /** @param {TreeNode} n @param {boolean} inHit @returns {boolean} whether n or one below it is a hit */
  const walk = (n, inHit) => {
    const self = hit(n, ws);
    if (self) hits.add(n.path);
    let below = false;
    for (const c of n.children) if (walk(c, inHit || self)) below = true;
    if (below) open.add(n.path);
    if (inHit || self || below) show.add(n.path);
    return self || below;
  };
  for (const r of roots) walk(r, false);
  return { show, open, hits };
}

/**
 * name cut into pieces, the ones matching a search word marked.
 * @param {string} name
 * @param {string[]} ws lower case
 * @returns {{ t: string, hit: boolean }[]}
 */
export function marks(name, ws) {
  const lower = name.toLowerCase();
  const on = new Array(name.length).fill(false);
  for (const w of ws) {
    for (let at = lower.indexOf(w); w && at >= 0; at = lower.indexOf(w, at + 1)) {
      for (let k = at; k < at + w.length; k++) on[k] = true;
    }
  }
  /** @type {{ t: string, hit: boolean }[]} */
  const out = [];
  for (let k = 0; k < name.length; k++) {
    const last = out.at(-1);
    if (last && last.hit === on[k]) last.t += name[k];
    else out.push({ t: name[k], hit: on[k] });
  }
  return out;
}
