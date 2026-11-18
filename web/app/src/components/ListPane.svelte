<script>
  import CopyIds from './CopyIds.svelte';
  import { tip } from '../lib/shortcuts.js';
  import { route, href, go } from '../lib/route.svelte.js';
  import { agentName, copy, short } from '../lib/format.js';
  import { toast } from '../lib/toast.svelte.js';
  import ItemRow from './ItemRow.svelte';
  import TreeNode from './TreeNode.svelte';
  import NodePanel from './NodePanel.svelte';
  import Deferred from './Deferred.svelte';
  import ScreenStrip from './ScreenStrip.svelte';
  import QuotaHit from './QuotaHit.svelte';
  let quotaCount = $state(0);
  import { sel, toggleAll, clear, prune, markSelected, pinSelected, selecting } from '../lib/select.svelte.js';
  import { snapshot } from '../lib/nav.svelte.js';
  import { ui, toggleSection } from '../lib/ui.svelte.js';

  let { view, error, onmenu, onchange } = $props();

  // The server lists oldest first. Recent first (the default) puts each
  // group's newest item on top, and the groups of the project with the newest
  // item first; in Needs you, blocked questions still come before defaults.
  function recent(groups, blockedFirst) {
    const newest = (g) => g.items.reduce((m, it) => (it.event.id > m ? it.event.id : m), '');
    const byProject = {};
    for (const g of groups) if (newest(g) > (byProject[g.project] ?? '')) byProject[g.project] = newest(g);
    const rank = (it) => (blockedFirst && it.event.level === 'blocked' ? 0 : 1);
    return groups
      .map((g) => ({ ...g, items: [...g.items].sort((a, b) => rank(a) - rank(b) || (a.event.id < b.event.id ? 1 : -1)) }))
      .sort((a, b) => byProject[b.project].localeCompare(byProject[a.project]) || newest(b).localeCompare(newest(a)));
  }
  const needs = $derived(route.old ? (view?.view?.needs_you ?? []) : recent(view?.view?.needs_you ?? [], true));
  const fresh = $derived(route.old ? (view?.view?.new ?? []) : recent(view?.view?.new ?? [], false));
  const responses = $derived(route.old ? (view?.view?.responses ?? []) : recent(view?.view?.responses ?? [], false));
  const pinned = $derived(view?.view?.pinned ?? []);
  const runs = $derived(view?.view?.runs ?? []);
  const count = (gs) => gs.reduce((n, g) => n + g.items.length, 0);
  const idsOf = (gs) => gs.flatMap((g) => g.items.map((it) => it.display_id));
  const needIds = $derived(idsOf(needs));
  const freshIds = $derived(idsOf(fresh));
  const responseIds = $derived(idsOf(responses));
  const pinnedIds = $derived(pinned.map((it) => it.display_id));
  // Folded sections drop out of j/k and previous/next.
  const shown = (name, ids) => (ui.collapsed[name] ? [] : ids);
  const allIds = $derived([
    ...new Set([...shown('needs', needIds), ...shown('pinned', pinnedIds), ...shown('responses', responseIds), ...shown('new', freshIds)]),
  ]);
  // Previous and next walk the list in the order shown.
  $effect(() => snapshot(allIds, route.item));
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
    <button class="btn ghost menu" aria-label="Menu" onclick={onmenu}>
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
      <div class="phone-node"><NodePanel node={view.node} target={view.target} conversation={view.conversation} stamp={view.stamp} {onchange} compact /></div>
    {/if}

    {#if view.screens?.length}<ScreenStrip screens={view.screens} host={view.host} {onchange} />{/if}

    <h2 class="section-title" data-section="needs">
      {@render fold('needs', 'Needs you')}
      {#if count(needs)}<span class="count hot">{count(needs)}</span>{/if}
      {#if needIds.length > 1 && !ui.collapsed.needs}<button class="pick" onclick={() => toggleAll(needIds)}>{allOn(needIds) ? 'Deselect' : 'Select all'}</button>{/if}
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

    <div class:hidden={!quotaCount}>
      <h2 class="section-title" data-section="quota">
        {@render fold('quota', 'Out of quota')}
        <span class="count">{quotaCount}</span>
      </h2>
      <QuotaHit stamp={view.stamp} path={route.path} collapsed={!!ui.collapsed.quota} bind:count={quotaCount} />
    </div>

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

    {#if responses.length}
      <h2 class="section-title" data-section="responses">
        {@render fold('responses', 'Responses to you')}
        <span class="count response">{count(responses)}</span>
        {#if responseIds.length > 1 && !ui.collapsed.responses}<button class="pick" onclick={() => toggleAll(responseIds)}>{allOn(responseIds) ? 'Deselect' : 'Select all'}</button>{/if}
      </h2>
      {#if !ui.collapsed.responses}
        {#each responses as g (g.project + '/' + g.run)}
          {@render groupHead(g)}
          <div class="rows">
            {#each g.items as it (it.event.id)}<ItemRow item={it} base={baseOf(g)} />{/each}
          </div>
        {/each}
      {/if}
    {/if}

    <h2 class="section-title" data-section="new">
      {@render fold('new', route.all ? 'Notices and slots' : 'New')}
      {#if count(fresh)}<span class="count">{count(fresh)}</span>{/if}
      {#if freshIds.length > 1 && !ui.collapsed.new}<button class="pick" onclick={() => toggleAll(freshIds)}>{allOn(freshIds) ? 'Deselect' : 'Select all'}</button>{/if}
    </h2>
    {#if !ui.collapsed.new}
      {#each fresh as g (g.project + '/' + g.run)}
        {@render groupHead(g)}
        <div class="rows">
          {#each g.items as it (it.event.id)}<ItemRow item={it} base={baseOf(g)} />{/each}
        </div>
      {:else}
        <div class="empty">{route.all ? 'No notices here.' : 'All read.'}</div>
      {/each}
    {/if}

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
</div>

{#if selecting()}
  <div class="bulk" role="toolbar" aria-label="Selected items">
    <button class="btn small ghost x" aria-label="Clear the selection" title="Clear (Esc)" onclick={clear}>✕</button>
    <span class="n">{#if !picked}Tap items to select them{:else}<strong>{picked}</strong> selected{/if}{pickedQuestions ? ` · ${pickedQuestions} ${pickedQuestions === 1 ? 'question' : 'questions'} to dismiss` : ''}</span>
    {#if !allOn(allIds)}<button class="btn small ghost" onclick={() => toggleAll(allIds)}>All {allIds.length}</button>{/if}
    <button data-cmd="bulk-pin" class="btn small" disabled={!picked} onclick={() => pinSelected(pickedPinned < picked, onchange)}
      >{picked && pickedPinned === picked ? 'Unpin' : 'Pin'}</button
    >
    <button data-cmd="bulk-unread" class="btn small" disabled={!picked} onclick={() => markSelected(false, onchange)}>Mark unread</button>
    <button
      data-cmd="bulk-read"
      disabled={!picked}
      class="btn small primary"
      title={pickedQuestions ? 'Questions leave Needs you but stay open, so you can still answer them from Show all' : ''}
      onclick={() => markSelected(true, onchange, pickedQuestions)}>Mark read</button
    >
  </div>
{/if}

{#snippet fold(name, label)}
  <button class="fold" data-cmd={'fold-' + name} aria-expanded={!ui.collapsed[name]} onclick={() => toggleSection(name)} title={tip(ui.collapsed[name] ? 'Show this section' : 'Fold this section', 'fold-' + name)}>
    <svg class="chev" class:shut={ui.collapsed[name]} width="11" height="11" viewBox="0 0 12 12" aria-hidden="true"><path d="m3 4.5 3 3 3-3" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" /></svg>
    {label}
  </button>
{/snippet}

{#snippet groupHead(g)}
  {@const ids = g.items.map((it) => it.display_id)}
  <div class="group-line">
    {#if !deep}<a class="group" href={href({ path: `${g.project}/${g.run}`, item: '' })}>{g.project} <span class="faint">/</span> {g.run}</a>{:else}<span></span>{/if}
    {#if ids.length > 1 && (!deep || selecting())}<button class="pick" onclick={() => toggleAll(ids)}>{allOn(ids) ? 'Deselect' : 'Select'} {ids.length}</button>{/if}
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
  .crumbs a:hover,
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
  .actions {
    display: flex;
    gap: 6px;
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
  .fold:hover {
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
  .group-line .pick {
    opacity: 0;
  }
  .group-line:hover .pick,
  .group-line .pick:focus-visible {
    opacity: 1;
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
  .pick:hover {
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
  .group:hover {
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
</style>
