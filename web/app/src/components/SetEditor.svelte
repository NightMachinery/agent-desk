<script>
  // Defines, changes and deletes project sets by ticking projects (or some
  // of their runs or roles) from what is on the desk. Each project expands
  // into its tree of runs and roles; the search finds a project or role by
  // name anywhere in it and opens the way to it. A path not on the desk yet
  // can be typed in.
  import Sheet from './Sheet.svelte';
  import { post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { setsUi } from '../lib/sets.svelte.js';
  import { untrack } from 'svelte';
  import { off, BUSY } from '../lib/why.js';
  import { ancestors, buildTree, marks, search, under, words } from '../lib/settree.js';

  let { sets = [], paths = [], onchange } = $props();

  let name = $state('');
  let picked = $state(new Set());
  let extra = $state('');
  let expanded = $state(new Set());
  let busy = $state(false);
  let query = $state('');
  // Nodes the search opened (or left shut) that the user toggled while it
  // ran; forgotten when the search changes.
  let flipped = $state(new Set());

  const projects = $derived(buildTree(paths));
  const found = $derived(search(projects, query));
  const ws = $derived(words(query));
  const known = $derived(new Set(paths));
  // Picked paths the desk has nothing under yet.
  const unknown = $derived([...picked].filter((p) => !known.has(p)));
  const editing = $derived(sets.find((s) => s.name === setsUi.name));

  // Load the set being edited whenever the sheet opens or switches set (not
  // when the desk changes underneath, which would undo the ticks).
  $effect(() => {
    if (!setsUi.open) return;
    const want = setsUi.name;
    const s = untrack(() => sets.find((x) => x.name === want));
    name = s?.name ?? '';
    picked = new Set(s?.prefixes ?? []);
    // Open the way to each run or role the set takes.
    expanded = new Set((s?.prefixes ?? []).flatMap(ancestors));
    extra = '';
    query = '';
  });

  // The picked path that takes in path from above, if any.
  const cover = (path) => ancestors(path).find((a) => picked.has(a));
  // Whether node's children show: the search opens the way to its hits.
  const isOpen = (path) => (found?.open.has(path) ? !flipped.has(path) : expanded.has(path));
  function toggle(path) {
    const next = new Set(picked);
    if (next.has(path)) next.delete(path);
    else {
      next.add(path);
      // A whole project replaces the runs ticked under it.
      for (const q of next) if (q !== path && under(q, path)) next.delete(q);
    }
    picked = next;
  }
  function toggleOpen(p) {
    const flip = (set) => {
      const next = new Set(set);
      next.has(p) ? next.delete(p) : next.add(p);
      return next;
    };
    if (found?.open.has(p)) flipped = flip(flipped);
    else expanded = flip(expanded);
  }
  // "3 runs", or "1 picked · 3 runs" when the set takes something under it.
  function childCount(n) {
    const noun = n.depth === 0 ? 'run' : 'role';
    const total = n.children.length;
    const some = [...picked].filter((p) => p !== n.path && under(p, n.path)).length;
    return `${some ? `${some} picked · ` : ''}${total} ${noun}${total > 1 ? 's' : ''}`;
  }
  function addExtra() {
    const p = extra.trim().replace(/^\/+|\/+$/g, '');
    if (!p) return;
    picked = new Set([...picked, p]);
    extra = '';
  }

  async function save(focus) {
    const n = name.trim();
    if (!n) return toast('Give the set a name.', 'error');
    if (!picked.size) return toast('Tick at least one project.', 'error');
    busy = true;
    try {
      // Renaming: define the new name, then delete the old one.
      await post('sets', { action: 'define', name: n, prefixes: [...picked] });
      if (editing && editing.name !== n) {
        if (editing.focused) focus = true;
        await post('sets', { action: 'delete', name: editing.name });
      }
      if (focus) await post('sets', { action: 'focus', name: n });
      toast(focus ? `Saved ${n}, and focused on it.` : `Saved ${n}.`, 'ok');
      setsUi.open = false;
      onchange?.();
    } catch (e) {
      toast(e.message, 'error', 8000);
    } finally {
      busy = false;
    }
  }

  async function remove() {
    if (!editing || !confirm(`Delete the set ${editing.name}?`)) return;
    busy = true;
    try {
      await post('sets', { action: 'delete', name: editing.name });
      toast(`Deleted ${editing.name}.`, 'ok');
      setsUi.open = false;
      onchange?.();
    } catch (e) {
      toast(e.message, 'error', 8000);
    } finally {
      busy = false;
    }
  }
</script>

{#snippet branch(nodes)}
  {#each nodes as n (n.path)}
    {#if !found || found.show.has(n.path)}
      {@const by = cover(n.path)}
      <div class="row" style:--depth={n.depth} class:deep={n.depth > 0}>
        <label class="pick">
          <input type="checkbox" checked={picked.has(n.path) || !!by} {...off(undefined, [by, `Included: ${by} is picked`])} onchange={() => toggle(n.path)} />
          <span class="path" title={n.depth ? n.path : undefined}>
            {#each found ? marks(n.name, ws) : [{ t: n.name, hit: false }] as m, k (k)}{#if m.hit}<mark>{m.t}</mark>{:else}{m.t}{/if}{/each}
          </span>
        </label>
        {#if n.children.length}
          <button data-keys="none" class="runs-toggle faint" aria-expanded={isOpen(n.path)} aria-label="{isOpen(n.path) ? 'Hide' : 'Show'} what is under {n.path}" onclick={() => toggleOpen(n.path)}>
            {childCount(n)}
            <svg width="10" height="10" viewBox="0 0 10 10" class:open={isOpen(n.path)} aria-hidden="true"><path d="M3 2l3.5 3L3 8" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" /></svg>
          </button>
        {/if}
      </div>
      {#if isOpen(n.path)}
        {@render branch(n.children)}
      {/if}
    {/if}
  {/each}
{/snippet}

<Sheet bind:open={setsUi.open} title={editing ? `Edit ${editing.name}` : 'New project set'}>
  {#if sets.length}
    <div class="tabs" role="tablist" aria-label="Sets">
      {#each sets as s (s.name)}
        <button data-keys="none" role="tab" aria-selected={setsUi.name === s.name} class:on={setsUi.name === s.name} onclick={() => (setsUi.name = s.name)}>{s.name}</button>
      {/each}
      <button data-keys="none" role="tab" aria-selected={!editing} class:on={!editing} onclick={() => (setsUi.name = '')}>+ New</button>
    </div>
  {/if}

  <label class="field">
    <span>Name</span>
    <input type="text" bind:value={name} placeholder="e.g. Work" maxlength="32" autocomplete="off" />
  </label>

  <div class="field"><span>Projects</span></div>
  {#if paths.length}
    <input class="search" type="search" bind:value={query} oninput={() => (flipped = new Set())} placeholder="Search projects, runs and roles" aria-label="Search projects, runs and roles" autocomplete="off" />
  {/if}
  <div class="list">
    {#each unknown as p (p)}
      <label class="row">
        <input type="checkbox" checked onchange={() => toggle(p)} />
        <span class="path">{p}</span>
        <span class="faint note">not on the desk yet</span>
      </label>
    {/each}
    {@render branch(projects)}
    {#if !projects.length}
      <p class="faint empty">No projects on the desk yet.</p>
    {:else if found && !found.show.size}
      <p class="faint empty">No project, run or role matches “{query.trim()}”.</p>
    {/if}
  </div>

  <form class="extra" onsubmit={(e) => (e.preventDefault(), addExtra())}>
    <input type="text" bind:value={extra} placeholder="Another path, e.g. private/linear-finetune" autocomplete="off" />
    <button data-keys="none" class="btn small" type="submit" {...off(undefined, [!extra.trim(), 'Type a path first'])}>Add</button>
  </form>

  <div class="foot">
    {#if editing}
      <button data-keys="none" class="btn small danger" {...off(undefined, [busy, BUSY])} onclick={remove}>Delete</button>
    {/if}
    <span class="grow"></span>
    <button data-keys="none" class="btn small" {...off(undefined, [busy, BUSY])} onclick={() => save(false)}>Save</button>
    <button data-keys="none" class="btn small primary" {...off(undefined, [busy, BUSY])} onclick={() => save(true)}>Save and focus</button>
  </div>
</Sheet>

<style>
  .tabs {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    margin-bottom: 12px;
  }
  .tabs button {
    height: 26px;
    padding: 0 10px;
    border-radius: 999px;
    border: 1px solid var(--line-strong);
    background: var(--panel);
    font-size: 12.5px;
    font-weight: 500;
  }
  .tabs button.on {
    background: var(--text);
    border-color: var(--text);
    color: var(--panel);
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 10px;
    font-size: 12px;
    font-weight: 600;
    color: var(--muted);
  }
  .field:has(+ .list),
  .field:has(+ .search) {
    margin-bottom: 4px;
  }
  input[type='text'],
  input[type='search'] {
    height: 32px;
    padding: 0 10px;
    border-radius: 8px;
    border: 1px solid var(--line-strong);
    background: var(--panel);
    color: var(--text);
    font-size: 14px;
    font-weight: 400;
  }
  input[type='text']:focus,
  input[type='search']:focus {
    outline: 0;
    border-color: var(--default);
    box-shadow: 0 0 0 3px var(--focus);
  }
  .list {
    max-height: 300px;
    overflow: auto;
    border: 1px solid var(--line);
    border-radius: 10px;
    padding: 4px;
  }
  .row,
  .pick {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 32px;
    padding: 0 6px;
    border-radius: 7px;
  }
  .row:hover:not(:disabled) {
    background: var(--sunken);
  }
  .pick {
    flex: 1;
    min-width: 0;
    padding: 0;
  }
  .search {
    width: 100%;
    margin-bottom: 6px;
  }
  .deep {
    margin-left: calc(var(--depth) * 22px);
    min-height: 28px;
    font-size: 13px;
  }
  mark {
    background: var(--focus);
    color: inherit;
    border-radius: 3px;
  }
  .empty {
    margin: 8px 6px;
    font-size: 13px;
  }
  .path {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .note {
    margin-left: auto;
    font-size: 12px;
  }
  .runs-toggle {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    border: 0;
    background: none;
    font-size: 12px;
    white-space: nowrap;
  }
  .runs-toggle svg {
    transition: transform 0.14s;
  }
  .runs-toggle svg.open {
    transform: rotate(90deg);
  }
  .extra {
    display: flex;
    gap: 8px;
    margin-top: 10px;
  }
  .extra input {
    flex: 1;
    min-width: 0;
  }
  .extra .btn {
    height: 32px;
  }
  .foot {
    display: flex;
    gap: 8px;
    margin-top: 14px;
  }
  .grow {
    flex: 1;
  }
  .danger {
    color: var(--blocked);
  }
</style>
