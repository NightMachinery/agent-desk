<script>
  // The list's Tasks section: the tasks under the current path that are
  // started or open, in working order (started first), and with a span
  // (the heading's menu, ui.tasksShow) also those finished in it, newest
  // first. A row opens the task; its box marks it done, or reopens a
  // finished one. Deferred tasks have their own section, and the Tasks page
  // (the heading's All tasks) has the rest: filters, subtasks, New task.
  import { get, post } from '../lib/api.js';
  import { href, route } from '../lib/route.svelte.js';
  import { toast } from '../lib/toast.svelte.js';
  import { toldText } from '../lib/taskstatus.js';
  import ReopenForm from './ReopenForm.svelte';
  import StaleMark from './StaleMark.svelte';
  import { off, BUSY } from '../lib/why.js';
  import { failed } from '../lib/errors.svelte.js';

  let { path = '', stamp = '', base = '', collapsed = false, span = 0, count = $bindable(0), ids = $bindable([]), onchange } = $props();

  // A long checklist stays on the Tasks page; the list shows its top, and
  // as many of the finished.
  const LIMIT = 8;
  const LABEL = { bug: 'Bug', feature: 'Feature', idea: 'Idea', chore: 'Chore' };

  let tasks = $state(/** @type {any[]} */ ([]));
  let loaded = $state(false);
  let error = $state('');
  let busy = $state('');
  let seq = 0;
  async function load() {
    const mine = ++seq;
    try {
      const res = await get('tasks', { path });
      if (mine !== seq) return;
      tasks = res.tasks ?? [];
      error = '';
    } catch (err) {
      if (mine === seq) error = err.message;
    } finally {
      if (mine === seq) loaded = true;
    }
  }
  $effect(() => {
    stamp;
    path;
    load();
  });

  // The search box filters this section like the others.
  const words = $derived(route.q.trim().toLowerCase().split(/\s+/).filter(Boolean));
  const matches = (t) => {
    if (!words.length) return true;
    const hay = `${t.display_id} ${t.event.title} ${t.event.body || ''} ${t.event.path} ${t.type || ''} ${(t.tags ?? []).map((g) => '#' + g).join(' ')}`.toLowerCase();
    return words.every((w) => hay.includes(w));
  };
  // The clock for the span, so a finished task leaves it on time.
  let now = $state(Date.now());
  $effect(() => {
    stamp;
    now = Date.now();
    const t = setInterval(() => (now = Date.now()), 60e3);
    return () => clearInterval(t);
  });
  const isOpen = (t) => t.state === 'started' || t.state === 'open';
  const finished = (t) => t.state === 'done' || t.state === 'dropped';
  const kept = $derived(tasks.filter((t) => isOpen(t) && matches(t)));
  const recent = $derived(
    span > 0
      ? tasks
          .filter((t) => finished(t) && now - Date.parse(t.updated) <= span && matches(t))
          .sort((a, b) => Date.parse(b.updated) - Date.parse(a.updated))
      : [],
  );
  const shown = $derived([...kept.slice(0, LIMIT), ...recent.slice(0, LIMIT)]);
  const more = $derived(Math.max(0, kept.length - LIMIT) + Math.max(0, recent.length - LIMIT));
  $effect(() => {
    count = kept.length;
    ids = collapsed ? [] : shown.map((t) => t.display_id);
  });
  /** How long ago ts was, short: "4 min", "3 h", "2 d". */
  function ago(ts) {
    const m = Math.max(0, Math.round((now - Date.parse(ts)) / 60e3));
    return m < 1 ? 'just now' : m < 60 ? `${m} min ago` : m < 48 * 60 ? `${Math.round(m / 60)} h ago` : `${Math.round(m / 1440)} d ago`;
  }

  const lineage = (p) => (p === base ? '' : base && p.startsWith(base + '/') ? p.slice(base.length + 1) : p);

  // The box on a finished task asks first, in ReopenForm (the task's event
  // id here), what is still wrong. An Undo puts the state back and tells
  // nobody: the server tells each task's net change (taskchanges.go).
  let reopening = $state('');
  async function act(t, action, note = '', undo = false) {
    busy = t.event.id;
    try {
      const res = await post('task', { action, id: t.display_id, ...(note && { text: note }) });
      const said = undo ? 'Undone' : action === 'done' ? 'Done' : 'Reopened';
      const back = action === 'done' ? 'reopen' : t.state === 'dropped' ? 'drop' : 'done';
      toast(`${said}: ${t.display_id}.${undo ? '' : toldText(res, true)}`, 'ok', 7000, undo ? undefined : { label: 'Undo', run: () => act(t, back, '', true) });
      if (reopening === t.event.id) reopening = '';
      await load();
      onchange?.();
    } catch (err) {
      failed(err);
    } finally {
      busy = '';
    }
  }
</script>

{#if !collapsed}
  <div class="rows">
    {#each shown as t (t.event.id)}
      {@const over = finished(t)}
      <div class="task" class:selected={route.item === t.display_id} class:started={t.state === 'started'} class:done={t.state === 'done'} class:dropped={t.state === 'dropped'}>
        <button data-keys="none" class="box" class:on={t.state === 'done'} {...off(over ? 'Reopen it' : 'Mark it done', [busy === t.event.id, BUSY])}
          aria-label={over ? `Reopen ${t.display_id}` : `Mark ${t.display_id} done`} onclick={() => (over ? (reopening = t.event.id) : act(t, 'done'))}>
          <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="m2.5 6.2 2.3 2.3 4.7-5" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" /></svg>
        </button>
        <a class="main" data-item={t.display_id} href={href({ item: t.display_id })}>
          <span class="line1">
            {#if LABEL[t.type]}<span class="tchip {t.type}">{LABEL[t.type]}</span>{/if}
            <span class="title">{t.event.title}</span>
            {#if t.untitled}<span class="chip" title="Filed without a title: its details' first line stands in until the agent names it">untitled</span>{/if}
            {#if t.state === 'started'}<span class="chip active">started</span>{/if}
            <StaleMark why={t.stale} />
            {#if over}<span class="chip" title={t.updated}>{t.state} {ago(t.updated)}</span>{/if}
            {#if t.priority === 1}<span class="chip blocked" title="Priority 1">p1</span>{/if}
          </span>
          <span class="line2">
            {#each t.tags ?? [] as g (g)}<span class="tag">#{g}</span>{/each}
            {#if lineage(t.event.path)}<span class="where">{lineage(t.event.path)}</span>{/if}
            <span class="mono id">{t.display_id}</span>
          </span>
        </a>
      </div>
      {#if reopening === t.event.id && over}
        <div class="reopen-row"><ReopenForm id={t.display_id} onreopen={(note) => act(t, 'reopen', note)} oncancel={() => (reopening = '')} /></div>
      {/if}
    {:else}
      {#if loaded}
        <div class="empty">
          {#if error}Cannot load tasks: {error}{:else if route.q && tasks.length}No task matches.{:else if span}No open tasks here, and none finished in that time.{:else}No open tasks here.{/if}
        </div>
      {/if}
    {/each}
    {#if more}
      <a class="more" href={href({ item: '', tasks: true })}>{more} more on the Tasks page</a>
    {/if}
  </div>
{/if}

<style>
  .rows {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .task {
    position: relative;
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 10px 13px 9px 11px;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    transition: border-color 0.12s, box-shadow 0.12s;
  }
  .task:hover:not(:disabled) {
    border-color: var(--line-strong);
    box-shadow: var(--shadow);
  }
  .task.selected {
    border-color: var(--default);
    box-shadow: 0 0 0 3px var(--focus);
  }
  .task.started::before {
    content: '';
    position: absolute;
    left: -1px;
    top: 10px;
    bottom: 10px;
    width: 3px;
    border-radius: 0 3px 3px 0;
    background: var(--ok);
  }
  .box {
    flex: none;
    display: grid;
    place-items: center;
    width: 18px;
    height: 18px;
    margin-top: 1px;
    padding: 0;
    border-radius: 5px;
    border: 1.5px solid var(--line-strong);
    background: var(--panel);
    color: transparent;
    cursor: pointer;
  }
  .box.on {
    border-color: var(--ok);
    background: var(--ok);
    color: var(--panel);
  }
  .box.on:hover:not(:disabled) {
    color: var(--panel);
    opacity: 0.8;
  }
  .task.done .title,
  .task.dropped .title {
    color: var(--muted);
    font-weight: 500;
  }
  .task.dropped .title {
    text-decoration: line-through;
  }
  .box:hover:not(:disabled),
  .box:focus-visible {
    border-color: var(--ok);
    color: var(--ok);
    outline: none;
  }
  .main {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
    flex: 1;
    color: inherit;
    text-decoration: none;
  }
  .line1,
  .line2 {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: 6px;
    min-width: 0;
  }
  .title {
    font-weight: 600;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .line2 {
    font-size: 12px;
    color: var(--muted);
  }
  .where {
    overflow-wrap: anywhere;
  }
  .id {
    margin-left: auto;
    color: var(--faint);
    font-size: 11.5px;
  }
  .tag {
    padding: 0 7px;
    border: 1px dashed var(--line-strong);
    border-radius: 999px;
    font-size: 11.5px;
  }
  .tchip {
    padding: 0 7px;
    border-radius: 999px;
    background: var(--sunken);
    color: var(--muted);
    font-size: 11.5px;
    font-weight: 600;
  }
  .tchip.bug {
    background: var(--blocked-soft);
    color: var(--blocked);
  }
  .tchip.feature {
    background: var(--default-soft);
    color: var(--default);
  }
  .tchip.idea {
    background: var(--notice-soft);
    color: var(--notice);
  }
  .reopen-row {
    margin: -2px 0 4px 28px;
  }
  .more {
    align-self: flex-start;
    padding: 2px 4px;
    font-size: 12.5px;
    color: var(--muted);
  }
</style>
