<script>
  // Tasks deferred until there is quota to spare. Pick some and launch them,
  // or launch the first few by priority; launch_command starts an agent on
  // each.
  import { href } from '../lib/route.svelte.js';
  import { post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { tip } from '../lib/shortcuts.js';

  let { tasks = [], launch = { ready: false, max: 3 }, base = '', onchange } = $props();
  let picked = $state(new Set());
  let busy = $state(false);
  $effect(() => {
    const ids = new Set(tasks.map((t) => t.display_id));
    if ([...picked].some((x) => !ids.has(x))) picked = new Set([...picked].filter((x) => ids.has(x)));
  });

  function toggle(id) {
    const s = new Set(picked);
    s.has(id) ? s.delete(id) : s.add(id);
    picked = s;
  }
  const lineage = (p) => (p === base ? '' : base && p.startsWith(base + '/') ? p.slice(base.length + 1) : p);

  async function act(action, ids) {
    busy = true;
    try {
      await post('task', { action, targets: ids });
      toast(action === 'launch' ? `Launched ${ids.length} ${ids.length === 1 ? 'task' : 'tasks'}.` : 'Done.', 'ok');
      picked = new Set();
      onchange?.();
    } catch (e) {
      toast(e.message, 'error', 9000);
      onchange?.();
    } finally {
      busy = false;
    }
  }
  const top = $derived(tasks.slice(0, launch.max).map((t) => t.display_id));
</script>

<div class="deferred">
  {#if !launch.ready}
    <div class="hint">To launch from here, set <code>launch_command</code> under <code>[tasks]</code> in config.toml. Meanwhile <code>agent-desk task prompt &lt;id&gt;</code> prints a task's prompt.</div>
  {/if}
  <div class="rows">
    {#each tasks as t (t.event.id)}
      <div class="task" class:on={picked.has(t.display_id)}>
        <input type="checkbox" aria-label={'Pick ' + t.event.title} checked={picked.has(t.display_id)} onchange={() => toggle(t.display_id)} />
        <a class="main" href={href({ item: t.display_id })}>
          <span class="title">{t.event.title}</span>
          <span class="meta">
            {#if t.priority}<span class="chip" class:hot={t.priority === 1}>p{t.priority}</span>{/if}
            {#if t.size}<span class="chip">{t.size}</span>{/if}
            {#if t.profile}<span class="chip mono">{t.profile}</span>{/if}
            <span class="faint">{lineage(t.event.path)}</span>
          </span>
          {#if t.event.body}<span class="brief">{t.event.body.split('\n')[0]}</span>{/if}
        </a>
        <button class="btn small ghost drop" title="Drop this task" aria-label="Drop" disabled={busy} onclick={() => act('drop', [t.display_id])}>✕</button>
      </div>
    {/each}
  </div>
  <div class="bar">
    <button class="btn small primary" disabled={!launch.ready || busy || !picked.size || picked.size > launch.max}
      title={picked.size > launch.max ? `At most ${launch.max} at once (max_launch)` : 'Start an agent on each picked task'}
      onclick={() => act('launch', [...picked])}>Launch {picked.size || ''} picked</button>
    <button class="btn small" disabled={!launch.ready || busy} title={tip(`Launch the first ${top.length} by priority`)} onclick={() => act('launch', top)}>Launch top {top.length}</button>
  </div>
</div>

<style>
  .deferred {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .hint {
    font-size: 13px;
    color: var(--muted);
  }
  .rows {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .task {
    display: grid;
    grid-template-columns: auto 1fr auto;
    gap: 10px;
    align-items: start;
    padding: 10px 12px;
    border: 1px solid var(--line);
    border-radius: var(--radius);
    background: var(--panel);
  }
  .task.on {
    border-color: var(--default);
    background: var(--accent-soft);
  }
  .task input {
    margin-top: 3px;
  }
  .main {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
    color: inherit;
  }
  .title {
    font-weight: 600;
  }
  .meta {
    display: flex;
    gap: 6px;
    align-items: center;
    flex-wrap: wrap;
    font-size: 12px;
  }
  .chip.hot {
    color: var(--blocked);
  }
  .brief {
    color: var(--muted);
    font-size: 13px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .bar {
    display: flex;
    gap: 8px;
    justify-content: flex-end;
  }
</style>
