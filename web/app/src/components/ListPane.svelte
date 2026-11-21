<script>
  import { cancelArrival } from '../lib/arrival.js';
  import LazyPanel from './LazyPanel.svelte';
  import CopyIds from './CopyIds.svelte';
  import { tip } from '../lib/shortcuts.js';
  import { route, href, go } from '../lib/route.svelte.js';
  import { agentName, copy, short } from '../lib/format.js';
  import { toast } from '../lib/toast.svelte.js';
  import { post } from '../lib/api.js';
  import ItemRow from './ItemRow.svelte';
  import TreeNode from './TreeNode.svelte';
  import Deferred from './Deferred.svelte';
  import ScreenStrip from './ScreenStrip.svelte';
  import PushNudge from './PushNudge.svelte';
  import { build } from '../lib/build.svelte.js';
  import QuotaHit from './QuotaHit.svelte';
  import TaskRows from './TaskRows.svelte';
  const NOPICK = 'Select items first (x)';
  let taskCount = $state(0);
  let taskIds = $state(/** @type {string[]} */ ([]));
  let quotaCount = $state(0);
  let quotaHidden = $state(0);
  import { sel, toggleAll, clear, prune, markSelected, pinSelected, selecting } from '../lib/select.svelte.js';
  import { snapshot } from '../lib/nav.svelte.js';
  import { ui, toggleSection, deliverableModes, deliverableModeLabel, showDeliverables, orderedSections, taskModes, taskModeLabel, taskSpans, showTasks } from '../lib/ui.svelte.js';
  import { hide, show, settle, unhidden } from '../lib/hiding.svelte.js';
  import { hasDraft } from '../lib/drafts.js';
  import { runCommand, keymap } from '../lib/keys.svelte.js';
  import { off } from '../lib/why.js';


  let { view, error, onmenu, onchange } = $props();

  // The server lists oldest first. Recent first (the default) puts each
  // group's newest item on top, and the groups of the project with the newest
  // item first; in Needs you, blocked questions still come before defaults.
  function recent(groups, blockedFirst) {
    const byProject = new Map();
    const rank = (it) => (blockedFirst && it.event.level === 'blocked' ? 0 : 1);
    const ranked = groups.map((group) => {
      const newest = group.items.reduce((m, it) => (it.event.id > m ? it.event.id : m), '');
      if (!byProject.has(group.project) || newest > byProject.get(group.project)) byProject.set(group.project, newest);
      const items = [...group.items].sort((a, b) => rank(a) - rank(b) || (a.event.id < b.event.id ? 1 : -1));
      return { group: { ...group, items }, newest };
    });
    return ranked
      .sort((a, b) => byProject.get(b.group.project).localeCompare(byProject.get(a.group.project)) || b.newest.localeCompare(a.newest))
      .map(({ group }) => group);
  }
  // Items being marked read are hidden at once (hiding.svelte.js); a view
  // that no longer lists them settles them.
  $effect(() => {
    const v = view?.view;
    if (v) settle(new Set([v.needs_you, v.new, v.responses, v.deliverables].flatMap((gs) => idsOf(gs ?? []))));
  });
  const needs = $derived(unhidden(route.old ? (view?.view?.needs_you ?? []) : recent(view?.view?.needs_you ?? [], true)));
  const fresh = $derived(unhidden(route.old ? (view?.view?.new ?? []) : recent(view?.view?.new ?? [], false)));
  const responses = $derived(unhidden(route.old ? (view?.view?.responses ?? []) : recent(view?.view?.responses ?? [], false)));
  const deliverables = $derived(unhidden(route.old ? (view?.view?.deliverables ?? []) : recent(view?.view?.deliverables ?? [], false)));
  const pinned = $derived(view?.view?.pinned ?? []);
  const deliverablesAny = $derived(view?.view?.deliverables_any ?? 0);
  const sectionOrder = $derived(orderedSections(ui.sectionOrder));
  const runs = $derived(view?.view?.runs ?? []);
  const count = (gs) => gs.reduce((n, g) => n + g.items.length, 0);
  // A section's badge counts what is unread: an item read a moment ago
  // stays listed (until you move on, or with Show all) but no longer counts.
  const unreadCount = (gs) => gs.reduce((n, g) => n + g.items.filter((it) => it.unread).length, 0);
  const idsOf = (gs) => gs.flatMap((g) => g.items.map((it) => it.display_id));
  const needIds = $derived(idsOf(needs));
  const freshIds = $derived(idsOf(fresh));
  const responseIds = $derived(idsOf(responses));
  const deliverableIds = $derived(idsOf(deliverables));
  const pinnedIds = $derived(pinned.map((it) => it.display_id));
  const unreadOf = (gs) => gs.flatMap((g) => g.items.filter((it) => it.unread).map((it) => it.display_id));
  const freshUnread = $derived(unreadOf(fresh));
  const responseUnread = $derived(unreadOf(responses));
  const deliverableUnread = $derived(unreadOf(deliverables));

  // Marks a whole section read, with Undo. Not offered on Needs you: reading
  // a question there dismisses it, too much for one unguarded click.
  // An item with a draft is left unread (and listed) until the draft is sent
  // or discarded.
  const byId = $derived(new Map([...needs, ...deliverables, ...responses, ...fresh].flatMap((g) => g.items).concat(pinned).map((it) => [it.display_id, it])));
  const drafted = (id) => !!byId.get(id)?.draft || hasDraft(id);
  async function markAllRead(all) {
    const kept = all.filter(drafted);
    const targets = all.filter((t) => !kept.includes(t));
    const keptNote = kept.length ? ` ${kept.length} with a draft stayed unread.` : '';
    if (!targets.length) return toast(`${kept.length === 1 ? 'It has a draft' : 'They have drafts'}: send or discard first.`, 'info');
    targets.forEach((t) => cancelArrival(t));
    const hidden = route.all ? [] : targets; // Show all keeps read items listed
    hide(hidden);
    try {
      await post('read', { targets });
      toast(`${targets.length} ${targets.length === 1 ? 'item' : 'items'} marked read.${keptNote}`, 'ok', 7000, {
        label: 'Undo',
        run: async () => {
          show(hidden);
          try {
            await post('unread', { targets });
            onchange?.();
          } catch (err) {
            toast(err.message, 'error');
          }
        },
      });
      onchange?.();
    } catch (err) {
      show(hidden);
      toast(err.message, 'error');
    }
  }
  // Folded sections drop out of j/k and previous/next.
  const shown = (name, ids) => (ui.collapsed[name] ? [] : ids);
  const allIds = $derived([
    ...new Set(sectionOrder.flatMap((n) => shown(n, { needs: needIds, deliverables: deliverableIds, pinned: pinnedIds, responses: responseIds, new: freshIds, tasks: taskIds }[n] ?? []))),
  ]);
  // Pinned items in any section, including a pinned question under Needs.
  const pinnedAll = $derived(
    new Set([...pinnedIds, ...[...needs, ...deliverables, ...responses, ...fresh].flatMap((g) => g.items.filter((it) => it.pinned).map((it) => it.display_id))]),
  );
  // Open: what is still waiting (Needs you, and unread items elsewhere).
  const openAll = $derived(new Set([...needIds, ...freshUnread, ...responseUnread, ...deliverableUnread, ...pinned.filter((it) => it.unread).map((it) => it.display_id)]));
  // Previous and next walk the list in the order shown.
  $effect(() => snapshot(allIds, route.item, pinnedAll, openAll));
  const pickedPinned = $derived(pinnedIds.filter((x) => sel.ids.has(x)).length);
  const allOn = (ids) => ids.length > 0 && ids.every((x) => sel.ids.has(x));
  $effect(() => prune(allIds));
  const picked = $derived(sel.ids.size);
  const pickedQuestions = $derived(needs.flatMap((g) => g.items).filter((it) => !it.dismissed && sel.ids.has(it.display_id)).length);
  const crumbs = $derived(route.path ? route.path.split('/').map((name, i, all) => ({ name, path: all.slice(0, i + 1).join('/') })) : []);
  // Under a run or deeper, group headers repeat the scope; drop them.
  const deep = $derived(crumbs.length >= 2);
  // What a row's lineage leaves out: its group heading, or the scope when
  // headings are dropped.
  const baseOf = (g) => (deep ? route.path : `${g.project}/${g.run}`);

  let q = $state(route.q);
  $effect(() => {
    q = route.q;
  });
  let typing;
  function onsearch() {
    clearTimeout(typing);
    typing = setTimeout(() => go({ q: q.trim(), item: '' }, { replace: true }), 220);
  }
</script>

<header>
  <div class="top">
    <button data-keys="none" class="btn ghost menu" aria-label="Menu" onclick={onmenu}>
      <svg width="18" height="18" viewBox="0 0 18 18"><path d="M2 4.5h14M2 9h14M2 13.5h14" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" /></svg>
    </button>
    <nav class="crumbs" aria-label="Scope">
      <a href={href({ path: '', item: '' })} class:here={!crumbs.length}>Everything</a>
      {#each crumbs as c, i (c.path)}
        <span class="sep">/</span>
        <a href={href({ path: c.path, item: '' })} class:here={i === crumbs.length - 1}>{c.name}</a>
      {/each}
    </nav>
  </div>
  <div class="filters">
    <label class="search">
      <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true"><circle cx="7" cy="7" r="5" fill="none" stroke="currentColor" stroke-width="1.6" /><path d="m11 11 3.5 3.5" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" /></svg>
      <input id="search" type="search" placeholder="Search items and runs  (/)" bind:value={q} oninput={onsearch} autocomplete="off" />
    </label>
    <a class="btn small toggle" class:on={route.all} href={href({ all: !route.all, item: '' })} title={tip('Include read items and finished runs', 'show-all')}>
      {route.all ? 'Showing all' : 'Show all'}
    </a>
    <a
      class="btn small icon"
      href={href({ old: !route.old })}
      title={tip(route.old ? 'Oldest first; switch to recent first' : 'Recent first; switch to oldest first', 'sort')}
      aria-label={route.old ? 'Sorted oldest first' : 'Sorted recent first'}
    >
      <svg width="15" height="15" viewBox="0 0 16 16" aria-hidden="true"
        ><path d={route.old ? 'M5 13V3M2.5 5.5 5 3l2.5 2.5' : 'M5 3v10M2.5 10.5 5 13l2.5-2.5'} fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /><path
          d="M9.5 4.5h4M9.5 8h3M9.5 11.5h2"
          stroke="currentColor"
          stroke-width="1.6"
          stroke-linecap="round"
        /></svg
      >
      <span class="label">{route.old ? 'Oldest' : 'Recent'}</span>
    </a>
    <button data-cmd="select-mode" title={tip('Select items', 'select-mode')} class="btn small" class:on={selecting()} onclick={() => (selecting() ? clear() : (sel.mode = true))}>{selecting() ? 'Done' : 'Select'}</button>
  </div>
  {#if route.session}
    {@const s = view?.session}
    <div class="session">
      <div>
        <div class="who">
          {agentName(s?.agent)} session <span class="mono">{short(route.session)}</span>
        </div>
        {#if s?.node}<div class="faint">{s.node}{s.last_age ? ` · last wrote ${s.last_age} ago` : ''}{s.host && s.host !== view?.host ? ` · on ${s.host}` : ''}</div>{/if}
      </div>
      <div class="actions">
        <CopyIds paseo={s?.paseo} resume={s?.resume} keys={false} />
        <a class="btn small" href={href({ session: '', item: '' })}>Clear</a>
      </div>
    </div>
  {/if}
</header>

<div class="body" class:selecting={selecting()}>
  {#if error}
    <div class="empty error">Cannot reach the desk: {error}</div>
  {:else if !view}
    <div class="skeleton"></div>
    <div class="skeleton short"></div>
  {:else}
    {#if view.node}
      <div class="phone-node"><LazyPanel view={import('./NodePanel.svelte')} node={view.node} target={view.target} conversations={view.conversations ?? []} stamp={view.stamp} {onchange} compact /></div>
    {/if}

    {#if build.stale}
      <div class="update" role="status">
        <span>The dashboard has been updated; this page still runs the old version.</span>
        <button class="btn small primary" data-cmd="reload-page" onclick={() => location.reload()} title={tip('Reload the page; drafts are kept', 'reload-page')}>Reload</button>
      </div>
    {/if}
    {#if keymap.error && keymap.dismissed !== keymap.error}
      <div class="update keys-bad" role="alert">
        <span>Your keys file <span class="mono">{keymap.path}</span> was not applied, so the built-in keys are in use. {keymap.error}</span>
        <button class="btn small ghost" data-keys="none" onclick={() => (keymap.dismissed = keymap.error)}>Dismiss</button>
      </div>
    {/if}
    <PushNudge />
    {#if view.screens?.length}<ScreenStrip screens={view.screens} host={view.host} {onchange} />{/if}

    {#each sectionOrder as name (name)}
      {#if name === 'needs'}
    <h2 class="section-title" data-section="needs">
      {@render fold('needs', 'Needs you')}
      {#if count(needs)}<span class="count hot">{count(needs)}</span>{/if}
      {#if needIds.length > 1 && !ui.collapsed.needs}<button data-keys="none" class="pick" onclick={() => toggleAll(needIds)}>{allOn(needIds) ? 'Deselect' : 'Select all'}</button>{/if}
    </h2>
    {#if !ui.collapsed.needs}
      {#each needs as g (g.project + '/' + g.run)}
        {@render groupHead(g)}
        <div class="rows">
          {#each g.items as it (it.event.id)}<ItemRow item={it} base={baseOf(g)} />{/each}
        </div>
      {:else}
        <div class="empty">Nothing is waiting on you{route.q ? ' that matches' : ''}.</div>
      {/each}
    {/if}
      {:else if name === 'quota'}
    <div class:hidden={!quotaCount && !quotaHidden}>
      <h2 class="section-title" data-section="quota">
        {@render fold('quota', 'Out of quota')}
        <span class="count">{quotaCount}</span>
      </h2>
      <QuotaHit stamp={view.stamp} path={route.path} collapsed={!!ui.collapsed.quota} bind:count={quotaCount} bind:hiddenCount={quotaHidden} />
    </div>
      {:else if name === 'deliverables'}
    {#if deliverables.length || deliverablesAny}
      <h2 class="section-title" data-section="deliverables">
        {@render fold('deliverables', 'Deliverables')}
        {#if unreadCount(deliverables)}<span class="count deliverable">{unreadCount(deliverables)}</span>{/if}
        <label class="show-pick" title={tip('Which deliverables to list: the newest of each, unread; the newest of each; or every one, replaced ones too', 'deliverables-show')}>
          <select data-cmd="deliverables-show" aria-label="Which deliverables to list" value={ui.deliverablesShow} onchange={(e) => showDeliverables(e.currentTarget.value)}>
            {#each deliverableModes as m (m)}<option value={m}>{deliverableModeLabel[m]}</option>{/each}
          </select>
          <svg width="9" height="9" viewBox="0 0 10 10" aria-hidden="true"><path d="m2 3.5 3 3 3-3" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
        </label>
        {#if deliverableIds.length > 1 && !ui.collapsed.deliverables}<button data-keys="none" class="pick" onclick={() => toggleAll(deliverableIds)}>{allOn(deliverableIds) ? 'Deselect' : 'Select all'}</button>{/if}
        {#if deliverableUnread.length}{@render markAll(deliverableUnread)}{/if}
      </h2>
      {#if !ui.collapsed.deliverables}
        {#each deliverables as g (g.project + '/' + g.run)}
          {@render groupHead(g, true)}
          <div class="rows">
            {#each g.items as it (it.event.id)}<ItemRow item={it} base={baseOf(g)} />{/each}
          </div>
        {:else}
          <div class="empty">
            {#if route.q}No deliverable matches.{:else if ui.deliverablesShow === 'unread'}Nothing new. <button data-cmd="deliverables-latest" class="linkish" onclick={() => showDeliverables('latest')}>Latest</button> lists the ones you have seen.{:else}No deliverables here.{/if}
          </div>
        {/each}
      {/if}
    {/if}
      {:else if name === 'pinned'}
    {#if pinned.length}
      <h2 class="section-title" data-section="pinned">
        {@render fold('pinned', 'Pinned')}
        <span class="count">{pinned.length}</span>
      </h2>
      {#if !ui.collapsed.pinned}
        <div class="rows">
          {#each pinned as it (it.event.id)}<ItemRow item={it} base={route.path} />{/each}
        </div>
      {/if}
    {/if}
      {:else if name === 'responses'}
    {#if responses.length}
      <h2 class="section-title" data-section="responses">
        {@render fold('responses', 'Responses to you')}
        {#if unreadCount(responses)}<span class="count response">{unreadCount(responses)}</span>{/if}
        {#if responseIds.length > 1 && !ui.collapsed.responses}<button data-keys="none" class="pick" onclick={() => toggleAll(responseIds)}>{allOn(responseIds) ? 'Deselect' : 'Select all'}</button>{/if}
        {#if responseUnread.length}{@render markAll(responseUnread)}{/if}
      </h2>
      {#if !ui.collapsed.responses}
        {#each responses as g (g.project + '/' + g.run)}
          {@render groupHead(g, true)}
          <div class="rows">
            {#each g.items as it (it.event.id)}<ItemRow item={it} base={baseOf(g)} />{/each}
          </div>
        {/each}
      {/if}
    {/if}
      {:else if name === 'new'}
    <h2 class="section-title" data-section="new">
      {@render fold('new', route.all ? 'Notices and slots' : 'New')}
      {#if unreadCount(fresh)}<span class="count">{unreadCount(fresh)}</span>{/if}
      {#if freshIds.length > 1 && !ui.collapsed.new}<button data-keys="none" class="pick" onclick={() => toggleAll(freshIds)}>{allOn(freshIds) ? 'Deselect' : 'Select all'}</button>{/if}
      {#if freshUnread.length}{@render markAll(freshUnread)}{/if}
    </h2>
    {#if !ui.collapsed.new}
      {#each fresh as g (g.project + '/' + g.run)}
        {@render groupHead(g, true)}
        <div class="rows">
          {#each g.items as it (it.event.id)}<ItemRow item={it} base={baseOf(g)} />{/each}
        </div>
      {:else}
        <div class="empty">{route.all ? 'No notices here.' : 'All read.'}</div>
      {/each}
    {/if}
      {:else if name === 'tasks'}
    <h2 class="section-title" data-section="tasks">
      {@render fold('tasks', 'Tasks')}
      {#if taskCount}<span class="count">{taskCount}</span>{/if}
      <label class="show-pick tasks-pick" title={tip('Which tasks to list: the open ones always, and with a span also those finished or dropped in it', 'tasks-show')}>
        <select data-cmd="tasks-show" aria-label="Which tasks to list" value={ui.tasksShow} onchange={(e) => showTasks(e.currentTarget.value)}>
          {#each taskModes as m (m)}<option value={m}>{taskModeLabel[m]}</option>{/each}
        </select>
        <svg width="9" height="9" viewBox="0 0 10 10" aria-hidden="true"><path d="m2 3.5 3 3 3-3" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
      </label>
      <span class="heads">
        <button data-cmd="task-new" class="pick" title={tip('File a new task, bug or idea', 'task-add')} onclick={() => runCommand('task-add')}>New</button>
        <a class="pick" data-cmd="tasks" href={href({ item: '', tasks: true })} title={tip('The Tasks page: finished tasks too, filters, subtasks', 'tasks')}>All tasks</a>
      </span>
    </h2>
    <TaskRows path={route.path} stamp={view.stamp} base={route.path} collapsed={!!ui.collapsed.tasks} span={taskSpans[ui.tasksShow]} bind:count={taskCount} bind:ids={taskIds} {onchange} />
      {:else if name === 'runs'}
    <h2 class="section-title" data-section="runs">{@render fold('runs', 'Runs')}</h2>
    {#if ui.collapsed.runs}
      <!-- folded -->
    {:else if runs.length}
      <div class="tree">
        {#each runs as n (n.path)}<TreeNode node={n} />{/each}
      </div>
    {:else if route.q}
      <div class="empty">No project, run or agent matches{route.all ? '' : '; Show all includes finished runs'}.</div>
    {:else}
      <div class="empty">No runs{route.all ? '' : ' with anything to show; Show all includes finished ones'}.</div>
    {/if}
      {:else if name === 'deferred'}
    {#if view.deferred?.length}
      <h2 class="section-title" data-section="deferred">
        {@render fold('deferred', 'Deferred for quota')}
        <span class="count">{view.deferred.length}</span>
      </h2>
      {#if !ui.collapsed.deferred}
        <Deferred tasks={view.deferred} launch={view.launch} base={route.path} {onchange} />
      {/if}
    {/if}
      {/if}
    {/each}
  {/if}
</div>

{#if selecting()}
  <div class="bulk" role="toolbar" aria-label="Selected items">
    <button data-cmd="select-clear" class="btn small ghost x" aria-label="Clear the selection" title="Clear (Esc)" onclick={clear}>✕</button>
    <span class="n">{#if !picked}Tap items to select them{:else}<strong>{picked}</strong> selected{/if}{pickedQuestions ? ` · ${pickedQuestions} ${pickedQuestions === 1 ? 'question' : 'questions'} to dismiss` : ''}</span>
    {#if !allOn(allIds)}<button data-cmd="select-all" class="btn small ghost" onclick={() => toggleAll(allIds)}>All {allIds.length}</button>{/if}
    <button data-cmd="bulk-pin" class="btn small" {...off(undefined, [!picked, NOPICK])} onclick={() => pinSelected(pickedPinned < picked, onchange)}
      >{picked && pickedPinned === picked ? 'Unpin' : 'Pin'}</button
    >
    <button data-cmd="bulk-unread" class="btn small" {...off(undefined, [!picked, NOPICK])} onclick={() => markSelected(false, onchange)}>Mark unread</button>
    <button
      data-cmd="bulk-read"
      class="btn small primary"
      {...off(pickedQuestions ? 'Questions leave Needs you but stay open, so you can still answer them from Show all' : '', [!picked, NOPICK])}
      onclick={() => markSelected(true, onchange, pickedQuestions)}>Mark read</button
    >
  </div>
{/if}

{#snippet markAll(ids, under = '')}
  <button class="mark-all" class:sub={!!under} data-cmd={under ? undefined : 'mark-all-read'}
    title={under ? `Mark the ${ids.length} unread in ${under} read` : `Mark all ${ids.length} read`}
    aria-label={under ? `Mark the ${ids.length} unread in ${under} read` : `Mark all ${ids.length} read`}
    onclick={(e) => { e.stopPropagation(); markAllRead(ids); }}>
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><path d="M1.5 8.5l3 3 6-7M7.5 11.5l.5.5 6.5-8" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
  </button>
{/snippet}

{#snippet fold(name, label)}
  <button class="fold" data-cmd={'fold-' + name} aria-expanded={!ui.collapsed[name]} onclick={() => toggleSection(name)} title={tip(ui.collapsed[name] ? 'Show this section' : 'Fold this section', 'fold-' + name)}>
    <svg class="chev" class:shut={ui.collapsed[name]} width="11" height="11" viewBox="0 0 12 12" aria-hidden="true"><path d="m3 4.5 3 3 3-3" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" /></svg>
    {label}
  </button>
{/snippet}

{#snippet groupHead(g, readable = false)}
  {@const ids = g.items.map((it) => it.display_id)}
  {@const unread = readable && !deep ? g.items.filter((it) => it.unread).map((it) => it.display_id) : []}
  <div class="group-line">
    {#if !deep}<a class="group" href={href({ path: `${g.project}/${g.run}`, item: '' })}>{g.project} <span class="faint">/</span> {g.run}</a>{:else}<span></span>{/if}
    {#if ids.length > 1 && (!deep || selecting())}<button data-keys="none" class="pick" onclick={() => toggleAll(ids)}>{allOn(ids) ? 'Deselect' : 'Select'} {ids.length}</button>{/if}
    {#if unread.length}{@render markAll(unread, `${g.project}/${g.run}`)}{/if}
  </div>
{/snippet}

<style>
  .hidden {
    display: none;
  }
  header {
    position: sticky;
    top: 0;
    z-index: 5;
    background: color-mix(in srgb, var(--bg) 88%, transparent);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    border-bottom: 1px solid var(--line);
    padding: max(10px, env(safe-area-inset-top)) 18px 10px;
  }
  .top {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 32px;
  }
  .menu {
    display: none;
    padding: 0 7px;
    margin-left: -7px;
  }
  .crumbs {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    font-size: 15px;
    font-weight: 600;
  }
  .crumbs a {
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .crumbs a:hover:not(:disabled),
  .crumbs a.here {
    color: var(--text);
  }
  .sep {
    color: var(--faint);
  }
  .filters {
    display: flex;
    gap: 8px;
    margin-top: 8px;
  }
  .search {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 7px;
    height: 32px;
    padding: 0 10px;
    border-radius: 8px;
    border: 1px solid var(--line-strong);
    background: var(--panel);
    color: var(--faint);
  }
  .search:focus-within {
    border-color: var(--default);
    box-shadow: 0 0 0 3px var(--focus);
  }
  .search input {
    flex: 1;
    min-width: 0;
    border: 0;
    outline: 0;
    background: transparent;
    color: var(--text);
  }
  .toggle {
    height: 32px;
  }
  .toggle.on {
    background: var(--text);
    border-color: var(--text);
    color: var(--panel);
  }
  .session {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    margin-top: 10px;
    padding: 9px 12px;
    border-radius: 10px;
    background: var(--default-soft);
    font-size: 13px;
  }
  .who {
    font-weight: 600;
  }
  .body {
    padding: 4px 18px 60px;
  }
  .fold {
    all: unset;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    border-radius: 4px;
  }
  .fold:hover:not(:disabled) {
    color: var(--text);
  }
  .fold:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .chev {
    transition: transform 0.14s;
  }
  .chev.shut {
    transform: rotate(-90deg);
  }
  .group-line {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  /* The Deliverables section's "show" menu: a quiet pill that reads as part
     of the heading until hovered. */
  .show-pick {
    --tone: var(--deliverable);
    --tone-soft: var(--deliverable-soft);
    position: relative;
    display: inline-flex;
    align-items: center;
    text-transform: none;
    letter-spacing: 0;
  }
  .show-pick select {
    appearance: none;
    -webkit-appearance: none;
    field-sizing: content;
    height: 22px;
    padding: 0 20px 0 9px;
    border: 1px solid color-mix(in srgb, var(--tone) 35%, var(--line));
    border-radius: 999px;
    background: var(--tone-soft);
    color: var(--tone);
    font: inherit;
    font-size: 11.5px;
    font-weight: 600;
    cursor: pointer;
  }
  .show-pick select:hover:not(:disabled),
  .show-pick select:focus-visible {
    border-color: var(--tone);
    outline: none;
  }
  .show-pick svg {
    position: absolute;
    right: 7px;
    pointer-events: none;
    color: var(--tone);
  }
  .tasks-pick {
    --tone: var(--ok);
    --tone-soft: var(--ok-soft);
  }
  .linkish {
    padding: 0;
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    text-decoration: underline;
    cursor: pointer;
  }
  .group-line .pick {
    opacity: 0;
    margin-left: auto;
  }
  /* A group's mark-read sits under its section's, a step quieter. */
  .mark-all.sub {
    color: var(--faint);
  }
  .group-line:hover:not(:disabled) .pick,
  .group-line .pick:focus-visible {
    opacity: 1;
  }
  .mark-all {
    margin-left: auto;
    display: inline-grid;
    place-items: center;
    width: 26px;
    height: 24px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--muted);
    cursor: pointer;
  }
  .pick + .mark-all {
    margin-left: 2px;
  }
  .mark-all:hover:not(:disabled) {
    background: var(--panel);
    color: var(--text);
  }
  .heads {
    display: inline-flex;
    gap: 2px;
    margin-left: auto;
  }
  .heads .pick {
    margin-left: 0;
    text-decoration: none;
  }
  .pick {
    margin-left: auto;
    padding: 2px 6px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--muted);
    font-size: 11.5px;
    font-weight: 600;
    letter-spacing: 0;
    text-transform: none;
    transition: opacity 0.12s, background 0.12s;
  }
  .pick:hover:not(:disabled) {
    background: var(--sunken);
    color: var(--text);
  }
  .bulk {
    position: sticky;
    bottom: max(12px, env(safe-area-inset-bottom));
    z-index: 6;
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0 14px;
    padding: 8px;
    border-radius: 12px;
    background: var(--panel);
    border: 1px solid var(--line-strong);
    box-shadow: var(--shadow);
    animation: rise 0.16s ease-out;
  }
  .bulk .x {
    padding: 0 8px;
  }
  .bulk .n {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 13px;
    color: var(--muted);
  }
  .bulk .n strong {
    color: var(--text);
  }
  @keyframes rise {
    from {
      transform: translateY(8px);
    }
  }
  .selecting .group-line .pick {
    opacity: 1;
  }
  .btn.on {
    background: var(--text);
    border-color: var(--text);
    color: var(--panel);
  }
  .icon {
    padding: 0 9px;
    height: 32px;
  }
  .filters > .btn {
    height: 32px;
  }
  @media (max-width: 600px) {
    .filters {
      flex-wrap: wrap;
      justify-content: flex-end;
    }
    .search {
      flex-basis: 100%;
    }
  }
  .group {
    display: block;
    margin: 12px 2px 6px;
    font-size: 12.5px;
    font-weight: 600;
    color: var(--muted);
  }
  .group:hover:not(:disabled) {
    color: var(--text);
  }
  .rows {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .tree {
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    padding: 4px;
  }
  .phone-node {
    display: none;
  }
  .error {
    color: var(--blocked);
    border-color: var(--blocked);
    margin-top: 16px;
  }
  .skeleton {
    height: 64px;
    margin-top: 16px;
    border-radius: var(--radius);
    background: linear-gradient(90deg, var(--sunken), var(--line), var(--sunken));
    background-size: 200% 100%;
    animation: shimmer 1.2s infinite;
  }
  .skeleton.short {
    width: 70%;
  }
  @keyframes shimmer {
    to {
      background-position: -200% 0;
    }
  }
  @media (max-width: 900px) {
    .menu {
      display: inline-flex;
    }
    header {
      padding-left: 14px;
      padding-right: 14px;
    }
    .body {
      padding: 4px 14px 60px;
    }
    .phone-node {
      display: block;
      margin-top: 12px;
    }
  }
  .update {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
    margin: 0 0 10px;
    padding: 8px 10px;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--accent-soft);
    font-size: 13px;
  }
  .keys-bad {
    background: var(--blocked-soft);
  }
</style>
