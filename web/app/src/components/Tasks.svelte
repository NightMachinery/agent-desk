<script>
  // The Tasks page: every task under the current path, the desk's
  // replacement for a TASKS.org checklist. Grouped by node, subtasks under
  // their parent; filtered by state, type and search. Tasks can be checked
  // off, started, dropped or reopened here, and new ones filed at any node,
  // optionally messaging its coordinator about it.
  import { get, post, upload } from '../lib/api.js';
  import { leaveField, typing } from '../lib/keys.svelte.js';
  import { href, go } from '../lib/route.svelte.js';
  import { toast } from '../lib/toast.svelte.js';
  import { queueMessage } from '../lib/outbox.svelte.js';
  import { chooseSendCache } from '../lib/cache.svelte.js';
  import { tip } from '../lib/shortcuts.js';
  import Markdown from './Markdown.svelte';
  import { askStatus, noteLabel, toldText } from '../lib/taskstatus.js';
  import ReopenForm from './ReopenForm.svelte';
  import { off, BUSY } from '../lib/why.js';
  import { listDrafts, loadDraft, saveDraft, clearDraft, discardDrafts } from '../lib/drafts.js';
  import { untrack } from 'svelte';

  let { path = '', stamp = '', runs = [], adding = $bindable(false) } = $props();

  const LABEL = { task: 'Task', bug: 'Bug', feature: 'Feature', idea: 'Idea', chore: 'Chore' };
  const ACTIVE = new Set(['open', 'started', 'deferred']);

  let tasks = $state(/** @type {any[]} */ ([]));
  let types = $state(['task', 'bug', 'feature', 'idea', 'chore']);
  let error = $state('');
  let loaded = $state(false);
  let show = $state(/** @type {'active'|'finished'|'all'} */ ('active'));
  let type = $state('');
  let tag = $state(''); // a free tag to keep, '' for all
  let q = $state('');
  let expanded = $state(/** @type {Record<string, boolean>} */ ({}));
  let busy = $state('');

  let seq = 0;
  async function load() {
    const mine = ++seq;
    try {
      const res = await get('tasks', { path });
      if (mine !== seq) return;
      tasks = res.tasks ?? [];
      types = res.types ?? types;
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
    loadDrafts();
  });

  const kindOf = (t) => t.type || 'task';
  const inState = (t) => (show === 'all' ? true : show === 'active' ? ACTIVE.has(t.state) : !ACTIVE.has(t.state));
  const words = $derived(q.trim().toLowerCase().split(/\s+/).filter(Boolean));
  const matches = (t) => {
    if (!words.length) return true;
    const hay = `${t.display_id} ${t.event.title} ${t.event.body || ''} ${t.event.path} ${(t.tags ?? []).map((g) => '#' + g).join(' ')}`.toLowerCase();
    return words.every((w) => hay.includes(w));
  };
  const scoped = $derived(tasks.filter((t) => inState(t) && matches(t)));
  const counts = $derived(scoped.reduce((m, t) => ((m[kindOf(t)] = (m[kindOf(t)] || 0) + 1), m), /** @type {Record<string, number>} */ ({})));
  // Free tags in what the state and search keep, most used first.
  const tagCounts = $derived(
    Object.entries(scoped.reduce((m, t) => {
      for (const g of t.tags ?? []) m[g] = (m[g] || 0) + 1;
      return m;
    }, /** @type {Record<string, number>} */ ({}))).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])),
  );
  const shown = $derived(scoped.filter((t) => (!type || kindOf(t) === type) && (!tag || (t.tags ?? []).includes(tag))));
  const ids = $derived(new Set(shown.map((t) => t.event.id)));
  // Each node's top-level tasks, with their subtasks under them. A subtask
  // whose parent is filtered out stands on its own.
  const groups = $derived.by(() => {
    const kids = /** @type {Record<string, any[]>} */ ({});
    const byNode = new Map();
    for (const t of shown) {
      if (t.parent && ids.has(t.parent)) (kids[t.parent] ??= []).push(t);
      else {
        if (!byNode.has(t.event.path)) byNode.set(t.event.path, []);
        byNode.get(t.event.path).push(t);
      }
    }
    return { kids, nodes: [...byNode.entries()].sort((a, b) => a[0].localeCompare(b[0])) };
  });
  const rel = (p) => (path && p.startsWith(path + '/') ? p.slice(path.length + 1) : p === path ? '' : p);
  const totals = $derived({
    active: tasks.filter((t) => ACTIVE.has(t.state)).length,
    finished: tasks.filter((t) => !ACTIVE.has(t.state)).length,
  });

  // Tags typed as text: "#ui, phone sync" (the desk cleans them).
  const tagList = (text) => text.split(/[\s,]+/).map((g) => g.replace(/^#/, '').toLowerCase()).filter(Boolean);
  let tagEdit = $state(/** @type {Record<string, string>} */ ({}));
  async function saveTags(t) {
    busy = t.event.id;
    try {
      await post('task', { action: 'tags', id: t.display_id, tags: tagList(tagEdit[t.event.id] ?? '') });
      delete tagEdit[t.event.id];
      toast(`Tags saved: ${t.display_id}.`, 'ok', 3000);
      await load();
    } catch (err) {
      toast(err.message, 'error');
    } finally {
      busy = '';
    }
  }

  // Reopen asks first, in ReopenForm (the task's event id here), what is
  // still wrong. An Undo puts the state back and tells nobody: the server
  // tells each task's net change once the changes stop (taskchanges.go).
  let reopening = $state('');
  async function act(t, action, note = '', undo = false) {
    busy = t.event.id;
    try {
      const res = await post('task', { action, id: t.display_id, ...(note && { text: note }) });
      const said = undo ? 'Undone' : { done: 'Done', reopen: 'Reopened', start: 'Started', drop: 'Dropped', defer: 'Deferred' }[action];
      const canUndo = !undo && (action === 'done' || action === 'drop');
      toast(`${said}: ${t.display_id}.${undo ? '' : toldText(res, canUndo)}`, 'ok', 7000, canUndo ? { label: 'Undo', run: () => act(t, 'reopen', '', true) } : undefined);
      if (reopening === t.event.id) reopening = '';
      await load();
    } catch (err) {
      toast(err.message, 'error');
    } finally {
      busy = '';
    }
  }

  // --- adding -----------------------------------------------------------------
  /** Every node in the tree, for the node picker. */
  const nodes = $derived.by(() => {
    const out = [];
    const walk = (ns) => {
      for (const n of ns ?? []) {
        if (!n.path.startsWith('_desk')) out.push(n);
        walk(n.children);
      }
    };
    walk(runs);
    return out;
  });
  let title = $state('');
  let newType = $state('task');
  let node = $state('');
  let brief = $state('');
  let parent = $state('');
  let newTags = $state('');
  let tell = $state(true);
  let target = $state(/** @type {any} */ (null));
  let filing = $state(false);
  /** @type {HTMLFormElement | undefined} */
  let formEl = $state();

  // Files for the new task: pasted (anywhere in the form), dropped on it or
  // attached. Each uploads at once, like the message box's; they stay on
  // the task and go with the message to its agent, which gets their paths.
  const MAX_FILES = 8;
  let files = $state(/** @type {any[]} */ ([]));
  let fileSeq = 0;
  /** @type {HTMLInputElement | undefined} */
  let picker = $state();
  const uploading = $derived(files.some((f) => f.status === 'uploading'));
  const readyRefs = $derived(files.filter((f) => f.status === 'ready').map((f) => f.ref));
  async function addFiles(list) {
    for (const f of list) {
      const key = ++fileSeq;
      if (files.length >= MAX_FILES) {
        files.push({ key, name: f.name, status: 'error', error: `at most ${MAX_FILES} files` });
        break;
      }
      files.push({ key, name: f.name || 'pasted-file', status: 'uploading' });
      try {
        const res = await upload(f);
        const x = files.find((y) => y.key === key);
        if (x) Object.assign(x, { status: 'ready', ref: res.ref, name: res.ref.name });
      } catch (err) {
        const x = files.find((y) => y.key === key);
        if (x) Object.assign(x, { status: 'error', error: err.message });
      }
    }
  }
  function onpaste(e) {
    const list = [...(e.clipboardData?.files ?? [])];
    if (list.length) {
      e.preventDefault();
      addFiles(list);
    }
  }
  function ondrop(e) {
    const list = [...(e.dataTransfer?.files ?? [])];
    if (list.length) {
      e.preventDefault();
      addFiles(list);
    }
  }

  $effect(() => {
    if (!adding) return;
    if (!node) node = nodes.some((n) => n.path === path) ? path : '';
  });
  // Who to tell: the node's agent, or for a project or a run (which no
  // agent holds) the coordinator working under it. targetFor says which
  // node is being looked up, so a stale answer never shows.
  let targetFor = $state('');
  $effect(() => {
    const at = node;
    target = null;
    targetFor = '';
    if (!adding || !at) return;
    get('target', { to: at, coordinator: '1' }).then(
      (t) => node === at && ((target = t), (targetFor = at)),
      () => node === at && (targetFor = at),
    );
  });
  const parents = $derived(tasks.filter((t) => t.event.path === node && ACTIVE.has(t.state) && !t.parent));

  // A title is optional when there are details: the agent names the task
  // (agent-desk task title), and the brief's first line stands in until then.
  const canFile = $derived(!!(title.trim() || brief.trim() || readyRefs.length) && !!node && !filing && !uploading);
  // Why File it is disabled right now, the same conditions as canFile.
  const fileWhy = $derived(/** @type {[unknown, string][]} */ ([
    [filing, 'Wait: filing'],
    [uploading, 'Wait: the files are still uploading'],
    [!node, 'Pick where it goes first'],
    [!(title.trim() || brief.trim() || readyRefs.length), 'Write what it is first'],
  ]));
  // Drafts: the form is saved as you type, each New task in its own box
  // (task:<id>), here and on the desk (lib/drafts.js). Closing the form
  // keeps it; the unfiled ones are listed at the top of the page, and one
  // opens back into the form. Filing drops it.
  let draftKey = $state('');
  let drafts = $state(/** @type {{key: string, value: any, t: number}[]} */ ([]));
  async function loadDrafts() {
    drafts = await listDrafts('task');
  }
  const newDraftKey = () => 'task:' + Array.from(crypto.getRandomValues(new Uint8Array(5)), (b) => b.toString(16).padStart(2, '0')).join('');
  const draftValue = $derived({
    node,
    type: newType,
    title,
    brief,
    parent,
    tags: newTags,
    files: files.filter((f) => f.status === 'ready').map((f) => ({ name: f.name, ref: f.ref })),
  });
  const drafted = $derived(!!(title.trim() || brief.trim() || newTags.trim() || readyRefs.length));
  $effect(() => {
    if (!adding || !draftKey) return;
    saveDraft(draftKey, $state.snapshot(draftValue), !drafted);
  });
  function resetForm() {
    title = '';
    brief = '';
    parent = '';
    newTags = '';
    newType = 'task';
    files = [];
  }
  // Opening the form starts a new draft unless one was picked; closing it
  // leaves the draft saved and empties the form for the next one.
  $effect(() => {
    if (adding) {
      untrack(() => draftKey || (draftKey = newDraftKey()));
      return;
    }
    untrack(() => {
      if (!draftKey) return;
      draftKey = '';
      resetForm();
      loadDrafts();
    });
  });
  function resume(d) {
    const v = d.value ?? {};
    draftKey = d.key;
    node = v.node ?? '';
    newType = v.type || 'task';
    title = v.title ?? '';
    brief = v.brief ?? '';
    parent = v.parent ?? '';
    newTags = v.tags ?? '';
    files = (v.files ?? []).map((f) => ({ key: ++fileSeq, name: f.name, ref: f.ref, status: 'ready' }));
    adding = true;
    setTimeout(() => document.getElementById('task-details')?.focus(), 0);
  }
  function discard(d) {
    // A draft only the desk has is copied here first, so Undo can bring it back.
    if (loadDraft(d.key) == null) saveDraft(d.key, d.value);
    const undo = discardDrafts([d.key]);
    drafts = drafts.filter((x) => x.key !== d.key);
    toast('Draft discarded.', 'ok', 6000, { label: 'Undo', run: () => (undo(), loadDrafts()) });
  }
  const shownDrafts = $derived(drafts.filter((d) => d.key !== draftKey && (!path || !d.value?.node || d.value.node === path || d.value.node.startsWith(path + '/'))));
  const draftTitle = (v) => v?.title?.trim() || v?.brief?.trim().split('\n')[0] || (v?.files?.length ? v.files.map((f) => f.name).join(', ') : 'Untitled');
  function draftAge(t) {
    const m = Math.max(0, Math.round((Date.now() - t) / 60e3));
    return m < 1 ? 'just now' : m < 60 ? `${m} min ago` : m < 48 * 60 ? `${Math.round(m / 60)} h ago` : `${Math.round(m / 1440)} d ago`;
  }

  function message(id) {
    const what = newType === 'task' ? 'task' : newType;
    const named = title.trim() ? `${id} "${title.trim()}"` : `${id}, without a title`;
    const filed = target?.for
      ? `The user filed a ${what} at ${node} on the desk, and you are the coordinator working under it: ${named}.`
      : `The user filed a ${what} for you on the desk: ${named}.`;
    // Their words go in a quote, apart from the instructions around them;
    // a quote holds anything pasted, code fences included.
    const quoted = brief.trim() ? `\n\nWhat they wrote:\n\n${brief.trim().split('\n').map((l) => (l.trim() ? '> ' + l : '>')).join('\n')}` : '';
    return filed + quoted +
      (title.trim() ? '' : `\n\nGive it a short title first: agent-desk task title ${id} 'Title'.`) +
      `\n\nIf it has several parts, split it first: file each as a step under it (agent-desk task add --parent ${id} --title '...'), and work through them.` +
      `\n\nIt is on your task list (agent-desk task list). Mark it with agent-desk task start ${id}, and when finished agent-desk task done ${id} --note 'how it was done and checked'.`;
  }

  async function file(e) {
    e?.preventDefault();
    if (!canFile) return;
    filing = true;
    try {
      const media = $state.snapshot(readyRefs);
      const res = await post('task', { action: 'add', to: node, title: title.trim(), type: newType, text: brief, parent, tags: tagList(newTags), ...(media.length && { media }) });
      let told = '';
      if (tell && target) {
        // About the task, not to the node: it reaches the same agent (a
        // task's messages go to whoever works on it), and the message and
        // its responses form the task's own thread, not the node's running
        // conversation.
        const to = res.id;
        const choice = await chooseSendCache('reply', to);
        if (choice !== 'cancel') {
          queueMessage('reply', { to, text: message(res.id), auto: 'task-filed', ...(media.length && { media }), ...(choice === 'compact' && { after_compaction: true }) }, to, { quiet: true });
          told = ` and told ${target.node}`;
        }
      }
      toast(`Filed ${res.id}${told}.`, 'ok', 6000);
      clearDraft(draftKey);
      draftKey = '';
      title = '';
      brief = '';
      parent = '';
      newTags = '';
      files = [];
      adding = false;
      await load();
    } catch (err) {
      toast(err.message, 'error', 9000);
    } finally {
      filing = false;
    }
  }
  // The form's keys, before the page's: ⌘/Ctrl+Enter files it. Escape in
  // a field leaves insert mode (focus moves to the form, which stays open,
  // and the field stays selected for `i`);
  // Escape in normal mode closes it, and so does C-g, from anywhere in it.
  // The draft is kept until it is filed.
  function formKeys(e) {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) return file(e);
    const quit = e.key === 'g' && e.ctrlKey && !e.metaKey && !e.altKey;
    if (e.key !== 'Escape' && !quit) return;
    e.preventDefault();
    e.stopPropagation();
    if (!quit && typing(e.target)) leaveField(/** @type {HTMLElement} */ (e.target), formEl);
    else adding = false;
  }
</script>

<section class="tasks-page">
  <div class="bar">
    <button class="btn ghost small" onclick={() => go({ tasks: false })}>← Back</button>
    <span class="spacer"></span>
    <button class="btn small primary" data-cmd="task-add" title={tip('File a new task, bug or idea at a node', 'task-add')} onclick={() => (adding = !adding)}>
      {adding ? 'Cancel' : 'New task'}
    </button>
  </div>
  <h1>Tasks</h1>
  <p class="faint lede">
    {#if path}Under <a class="mono" href={href({ path, tasks: false })}>{path}</a> (<a href={href({ path: '', tasks: true })}>show everywhere</a>).{:else}Every project.{/if}
    Agents keep these with <code>agent-desk task</code>; <code>agent-desk task list --org</code> prints them as a checklist.
  </p>

  {#if adding}
    <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
    <form class="add" bind:this={formEl} tabindex="-1" onsubmit={file} onkeydown={formKeys} {onpaste} {ondrop} ondragover={(e) => e.preventDefault()}>
      <!-- svelte-ignore a11y_autofocus -->
      <textarea id="task-details" class="input" rows="4" placeholder="What needs doing: what you saw, what done looks like" bind:value={brief} autofocus aria-label="Details"></textarea>
      <input id="task-title" class="input" placeholder="Title (optional: the agent names it from the details)" bind:value={title} aria-label="Task title" />
      <div class="types" role="radiogroup" aria-label="Type">
        {#each types as t (t)}
          <button type="button" role="radio" aria-checked={newType === t} class="tchip {t}" class:on={newType === t} onclick={() => (newType = t)}>{LABEL[t] ?? t}</button>
        {/each}
      </div>
      <div class="row2">
        <label>
          <span class="faint">At</span>
          <select class="input" bind:value={node} aria-label="Node">
            <option value="" disabled>Pick a project, run or agent</option>
            {#each nodes as n (n.path)}<option value={n.path}>{n.path}</option>{/each}
          </select>
        </label>
        {#if parents.length}
          <label>
            <span class="faint">Under</span>
            <select class="input" bind:value={parent} aria-label="Parent task">
              <option value="">(top level)</option>
              {#each parents as t (t.event.id)}<option value={t.display_id}>{t.event.title}</option>{/each}
            </select>
          </label>
        {/if}
      </div>
      <div class="files">
        <input bind:this={picker} type="file" multiple hidden onchange={(e) => (addFiles([...(e.currentTarget.files ?? [])]), (e.currentTarget.value = ''))} />
        <button type="button" class="btn ghost small" onclick={() => picker?.click()} {...off('Attach files; you can also paste or drop them on the form', [files.length >= MAX_FILES, `At most ${MAX_FILES} files`])}>Attach</button>
        {#each files as f (f.key)}
          <span class="file" class:err={f.status === 'error'}>
            <span class="mono">{f.name}</span>
            <span class="faint">{f.status === 'uploading' ? 'uploading…' : f.status === 'error' ? f.error : ''}</span>
            <button type="button" class="btn ghost small x" aria-label={`Remove ${f.name}`} onclick={() => (files = files.filter((x) => x.key !== f.key))}>✕</button>
          </span>
        {:else}
          <span class="faint hint">or paste or drop files</span>
        {/each}
      </div>
      <input class="input" placeholder="Tags (optional): ui, phone" bind:value={newTags} aria-label="Tags" autocomplete="off" />
      <div class="row2">
        {#if target}
          <label class="check"><input type="checkbox" bind:checked={tell} />
            <span>{#if target.for}Message <span class="mono">{target.node}</span>, the coordinator working under it, about it{:else}Message its coordinator about it{/if}</span></label>
        {:else if node && targetFor === node}
          <span class="warn">No agent works under {node}, so nobody will be told: it waits on the list until an agent looks.</span>
        {/if}
        <span class="spacer"></span>
        <span class="faint hint">⌘↵ to file · Esc twice or C-g to close</span>
        <button class="btn small primary" type="submit" {...off(undefined, ...fileWhy)}>{filing ? 'Filing…' : 'File it'}</button>
      </div>
    </form>
  {/if}

  <div class="filters">
    <div class="seg" role="radiogroup" aria-label="State">
      <button role="radio" aria-checked={show === 'active'} class:on={show === 'active'} onclick={() => (show = 'active')}>Open <span class="n">{totals.active}</span></button>
      <button role="radio" aria-checked={show === 'finished'} class:on={show === 'finished'} onclick={() => (show = 'finished')}>Finished <span class="n">{totals.finished}</span></button>
      <button role="radio" aria-checked={show === 'all'} class:on={show === 'all'} onclick={() => (show = 'all')}>All</button>
    </div>
    <div class="types" role="radiogroup" aria-label="Type filter">
      <button role="radio" aria-checked={!type} class="tchip" class:on={!type} onclick={() => (type = '')}>All types</button>
      {#each types.filter((t) => counts[t]) as t (t)}
        <button role="radio" aria-checked={type === t} class="tchip {t}" class:on={type === t} onclick={() => (type = type === t ? '' : t)}>{LABEL[t] ?? t} <span class="n">{counts[t]}</span></button>
      {/each}
    </div>
    {#if tagCounts.length || tag}
      <div class="types tags" role="radiogroup" aria-label="Tag filter">
        <button role="radio" aria-checked={!tag} class="tag" class:on={!tag} onclick={() => (tag = '')}>All tags</button>
        {#each tagCounts as [g, n] (g)}
          <button role="radio" aria-checked={tag === g} class="tag" class:on={tag === g} onclick={() => (tag = tag === g ? '' : g)}>#{g} <span class="n">{n}</span></button>
        {/each}
      </div>
    {/if}
    <input id="tasks-search" class="input search" type="search" placeholder="Search tasks  (/)" bind:value={q} aria-label="Search tasks" />
  </div>

  {#if shownDrafts.length}
    <h2 class="node">Drafts</h2>
    <ul class="list drafts">
      {#each shownDrafts as d (d.key)}
        <li class="task draft" style="--depth: 0">
          <div class="line">
            <button class="draft-open" title="Open it in the form to finish and file it" onclick={() => resume(d)}>
              <span class="chip">Draft</span>
              {#if d.value?.type && d.value.type !== 'task'}<span class="tchip small {d.value.type} on">{LABEL[d.value.type] ?? d.value.type}</span>{/if}
              <span class="title">{draftTitle(d.value)}</span>
              <span class="faint">{d.value?.node || 'no node yet'} · {draftAge(d.t)}</span>
            </button>
            <button class="icon" title="Discard this draft (Undo brings it back)" aria-label="Discard this draft" onclick={() => discard(d)}>✕</button>
          </div>
        </li>
      {/each}
    </ul>
  {/if}

  {#if error}
    <div class="empty err">{error}</div>
  {:else if loaded && !shown.length}
    <div class="empty faint">
      {#if tasks.length}Nothing {show === 'active' ? 'open' : show === 'finished' ? 'finished' : ''}{type ? ` of type ${LABEL[type] ?? type}` : ''}{tag ? ` tagged #${tag}` : ''}{q.trim() ? ' matching' : ''} here.
      {:else}No tasks here yet. Agents file them with <code>agent-desk task add</code>, or use New task.{/if}
    </div>
  {/if}

  {#snippet row(t, depth)}
    {@const done = t.state === 'done'}
    {@const dropped = t.state === 'dropped'}
    <li class="task" class:done class:dropped style={`--depth: ${depth}`}>
      <div class="line">
        <button class="box" class:on={done} role="checkbox" aria-checked={done} {...off(undefined, [busy === t.event.id, BUSY], [dropped, 'Dropped: reopen it first'])}
          title={done ? 'Reopen it' : 'Mark it done'} aria-label={done ? `Reopen ${t.display_id}` : `Mark ${t.display_id} done`}
          onclick={() => (done ? (reopening = t.event.id) : act(t, 'done'))}>
          <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="m2.5 6.2 2.3 2.3 4.7-5" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" /></svg>
        </button>
        {#if kindOf(t) !== 'task'}<span class="tchip small {kindOf(t)} on">{LABEL[kindOf(t)] ?? kindOf(t)}</span>{/if}
        <button class="title" onclick={() => (expanded[t.event.id] = !expanded[t.event.id])} aria-expanded={!!expanded[t.event.id]}>{t.event.title}</button>
        {#if t.untitled}<span class="chip" title="Filed without a title: its details' first line stands in until the agent names it">untitled</span>{/if}
        {#if t.state === 'started'}<span class="chip active">started</span>{:else if t.state === 'deferred'}<span class="chip waiting">deferred</span>{:else if dropped}<span class="chip">dropped</span>{/if}
        {#if t.priority === 1}<span class="chip blocked" title="Priority 1">p1</span>{/if}
        {#each t.tags ?? [] as g (g)}<button class="tag small" class:on={tag === g} title={`Only #${g}`} onclick={() => (tag = tag === g ? '' : g)}>#{g}</button>{/each}
        {#if t.event.meta?.source === 'web'}<span class="faint you" title="You filed it">you</span>{/if}
        <a class="mono faint id" href={href({ item: t.display_id })}>{t.display_id}</a>
      </div>
      {#if reopening === t.event.id && !ACTIVE.has(t.state)}
        <div class="reopen-row"><ReopenForm id={t.display_id} onreopen={(note) => act(t, 'reopen', note)} oncancel={() => (reopening = '')} /></div>
      {/if}
      {#if expanded[t.event.id]}
        <div class="more">
          {#if t.event.body}<Markdown text={t.event.body} />{/if}
          {#if t.note}<div class="task-note"><span class="faint">{noteLabel(t.state)}:</span> {t.note}</div>{/if}
          {#if t.agent}<div class="faint">Agent: <span class="mono">{t.agent}</span></div>{/if}
          <form class="tag-edit" onsubmit={(e) => (e.preventDefault(), saveTags(t))}>
            <input class="input" placeholder="Tags: ui, phone" aria-label={`Tags of ${t.display_id}`} autocomplete="off"
              value={tagEdit[t.event.id] ?? (t.tags ?? []).join(', ')} oninput={(e) => (tagEdit[t.event.id] = e.currentTarget.value)} />
            <button class="btn small" type="submit" {...off(undefined, [busy, BUSY], [tagEdit[t.event.id] === undefined, 'Change the tags first'])}>Save tags</button>
          </form>
          <div class="acts">
            {#if ACTIVE.has(t.state)}
              <button class="btn small" title="Ask whoever works on it for a short executive summary of where it stands" onclick={() => askStatus(t.event.id, t.display_id)}>Status</button>
              {#if t.state !== 'started'}<button class="btn small" {...off(undefined, [busy, BUSY])} onclick={() => act(t, 'start')}>Start</button>{/if}
              <button class="btn small" {...off(undefined, [busy, BUSY])} onclick={() => act(t, 'drop')}>Drop</button>
            {:else}
              <button class="btn small" {...off(undefined, [busy, BUSY])} onclick={() => (reopening = t.event.id)}>Reopen</button>
            {/if}
            <a class="btn small" href={href({ item: t.display_id })} title="The task on its own page: its conversation, messaging its agent, pinning">Full page</a>
          </div>
        </div>
      {/if}
    </li>
    {#each groups.kids[t.event.id] ?? [] as c (c.event.id)}{@render row(c, depth + 1)}{/each}
  {/snippet}

  {#each groups.nodes as [at, list] (at)}
    <h2 class="node"><a href={href({ path: at, tasks: true })} title={`Only ${at}`}>{rel(at) || at}</a></h2>
    <ul class="list">
      {#each list as t (t.event.id)}{@render row(t, 0)}{/each}
    </ul>
  {/each}
</section>

<style>
  .add:focus {
    outline: none;
  }
  .files {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }
  .file {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 1px 2px 1px 9px;
    border: 1px solid var(--line);
    border-radius: 999px;
    font-size: 12px;
  }
  .file.err {
    border-color: var(--blocked);
  }
  .file .x {
    padding: 0 6px;
  }
  .reopen-row {
    margin: 6px 0 2px 26px;
  }
  .warn {
    color: var(--blocked);
    font-size: 13px;
  }
  .tasks-page {
    padding: 16px 20px 40px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .bar,
  .row2 {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .spacer {
    flex: 1;
  }
  h1 {
    margin: 4px 0 0;
  }
  .lede {
    margin: 0;
    font-size: 13px;
  }
  .lede code {
    white-space: nowrap;
  }
  .add {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    border: 1px solid var(--line);
    border-radius: 10px;
    background: var(--panel);
  }
  .add label {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  .add select {
    max-width: 100%;
  }
  .check {
    font-size: 13px;
  }
  .hint {
    font-size: 12px;
  }
  @media (pointer: coarse) {
    .hint {
      display: none;
    }
  }
  .filters {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-top: 6px;
  }
  .seg {
    display: inline-flex;
    align-self: flex-start;
    border: 1px solid var(--line);
    border-radius: 8px;
    overflow: hidden;
  }
  .seg button {
    border: 0;
    background: none;
    padding: 5px 12px;
    font: inherit;
    font-size: 13px;
    color: var(--muted);
    cursor: pointer;
  }
  .seg button.on {
    background: var(--sunken);
    color: var(--text);
    font-weight: 600;
  }
  .n {
    opacity: 0.7;
    font-size: 11.5px;
  }
  .tag {
    padding: 2px 9px;
    border: 1px dashed var(--line-strong);
    border-radius: 999px;
    background: none;
    color: var(--muted);
    font: inherit;
    font-size: 12px;
    cursor: pointer;
  }
  .tag.small {
    padding: 0 7px;
    font-size: 11.5px;
  }
  .tag.on {
    border-style: solid;
    border-color: var(--text);
    color: var(--text);
  }
  .tag .n {
    opacity: 0.6;
  }
  .tag-edit {
    display: flex;
    gap: 6px;
    align-items: center;
  }
  .tag-edit .input {
    flex: 1;
    min-width: 0;
  }
  .types {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .tchip {
    border: 1px solid var(--line);
    background: none;
    border-radius: 999px;
    padding: 2px 10px;
    font: inherit;
    font-size: 12.5px;
    color: var(--muted);
    cursor: pointer;
  }
  .tchip.on {
    background: var(--sunken);
    color: var(--text);
    border-color: transparent;
    font-weight: 600;
  }
  .tchip.small {
    padding: 0 7px;
    font-size: 11.5px;
    cursor: default;
  }
  .tchip.bug.on {
    background: var(--blocked-soft);
    color: var(--blocked);
  }
  .tchip.feature.on {
    background: var(--default-soft);
    color: var(--default);
  }
  .tchip.idea.on {
    background: var(--notice-soft);
    color: var(--notice);
  }
  .tchip.chore.on {
    background: var(--sunken);
    color: var(--muted);
  }
  .search {
    width: 100%;
  }
  .empty {
    padding: 18px 0;
  }
  .err {
    color: var(--blocked);
  }
  .node {
    font-size: 12px;
    letter-spacing: 0.04em;
    color: var(--muted);
    margin: 14px 0 0;
    font-weight: 600;
  }
  .node a {
    color: inherit;
    text-decoration: none;
  }
  .draft {
    border-style: dashed;
  }
  .draft-open {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1;
    min-width: 0;
    padding: 0;
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }
  .draft-open .title {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .draft-open .faint {
    flex: none;
    margin-left: auto;
    font-size: 12px;
  }
  .draft .icon {
    flex: none;
    border: 0;
    background: none;
    color: var(--muted);
    cursor: pointer;
    padding: 0 4px;
  }
  .drafts {
    margin-bottom: 4px;
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .task {
    margin-left: calc(var(--depth) * 22px);
    border: 1px solid var(--line);
    border-radius: 8px;
    background: var(--panel);
    padding: 6px 10px;
  }
  .line {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .box {
    flex: none;
    width: 18px;
    height: 18px;
    border-radius: 5px;
    border: 1.5px solid var(--line);
    background: var(--panel);
    color: transparent;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    padding: 0;
  }
  .box:hover:not(:disabled) {
    border-color: var(--ok);
    color: color-mix(in srgb, var(--ok) 50%, transparent);
  }
  .box.on {
    background: var(--ok);
    border-color: var(--ok);
    color: #fff;
  }
  .title {
    flex: 1;
    min-width: 0;
    text-align: left;
    border: 0;
    background: none;
    padding: 0;
    font: inherit;
    font-size: 14px;
    color: var(--text);
    cursor: pointer;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .task.done .title,
  .task.dropped .title {
    text-decoration: line-through;
    color: var(--muted);
  }
  .you {
    font-size: 11.5px;
  }
  .id {
    font-size: 12px;
    text-decoration: none;
  }
  .more {
    margin: 6px 0 2px 26px;
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: 13.5px;
  }
  .acts {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  .task-note {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
</style>
