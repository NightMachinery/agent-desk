<script>
  // Defines, changes and deletes project sets by ticking projects (or some
  // of their runs) from what is on the desk. A path not on the desk yet can
  // be typed in.
  import Sheet from './Sheet.svelte';
  import { post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { setsUi } from '../lib/sets.svelte.js';
  import { untrack } from 'svelte';
  import { off, BUSY } from '../lib/why.js';

  let { sets = [], paths = [], onchange } = $props();

  let name = $state('');
  let picked = $state(new Set());
  let extra = $state('');
  let expanded = $state(new Set());
  let busy = $state(false);

  // The projects, each with its runs.
  const projects = $derived.by(() => {
    const out = [];
    for (const p of paths) {
      const i = p.indexOf('/');
      if (i < 0) out.push({ path: p, runs: [] });
      else out.at(-1)?.runs.push(p);
    }
    return out;
  });
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
    // Show the runs of a project the set takes only part of.
    expanded = new Set((s?.prefixes ?? []).filter((p) => p.includes('/')).map((p) => p.split('/')[0]));
    extra = '';
  });

  const under = (p, prefix) => p === prefix || p.startsWith(prefix + '/');
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
  function toggleRuns(p) {
    const next = new Set(expanded);
    next.has(p) ? next.delete(p) : next.add(p);
    expanded = next;
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
  <div class="list">
    {#each unknown as p (p)}
      <label class="row">
        <input type="checkbox" checked onchange={() => toggle(p)} />
        <span class="path">{p}</span>
        <span class="faint note">not on the desk yet</span>
      </label>
    {/each}
    {#each projects as p (p.path)}
      <div class="row">
        <label class="pick">
          <input type="checkbox" checked={picked.has(p.path)} onchange={() => toggle(p.path)} />
          <span class="path">{p.path}</span>
        </label>
        {#if p.runs.length}
          {@const some = p.runs.filter((r) => picked.has(r)).length}
          <button data-keys="none" class="runs-toggle faint" aria-expanded={expanded.has(p.path)} onclick={() => toggleRuns(p.path)}>
            {some ? `${some} of ${p.runs.length} runs` : `${p.runs.length} run${p.runs.length > 1 ? 's' : ''}`}
            <svg width="10" height="10" viewBox="0 0 10 10" class:open={expanded.has(p.path)} aria-hidden="true"><path d="M3 2l3.5 3L3 8" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" /></svg>
          </button>
        {/if}
      </div>
      {#if expanded.has(p.path)}
        {#each p.runs as r (r)}
          <label class="row run">
            <input type="checkbox" checked={picked.has(r) || picked.has(p.path)} {...off(undefined, [picked.has(p.path), 'Included: the whole project is picked'])} onchange={() => toggle(r)} />
            <span class="path">{r.slice(p.path.length + 1)}</span>
          </label>
        {/each}
      {/if}
    {:else}
      <p class="faint">No projects on the desk yet.</p>
    {/each}
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
  .field:has(+ .list) {
    margin-bottom: 4px;
  }
  input[type='text'] {
    height: 32px;
    padding: 0 10px;
    border-radius: 8px;
    border: 1px solid var(--line-strong);
    background: var(--panel);
    color: var(--text);
    font-size: 14px;
    font-weight: 400;
  }
  input[type='text']:focus {
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
  .run {
    margin-left: 22px;
    min-height: 28px;
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
