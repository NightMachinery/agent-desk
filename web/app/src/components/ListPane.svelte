<script>
  import { route, href, go } from '../lib/route.svelte.js';
  import { agentName, copy, short } from '../lib/format.js';
  import { toast } from '../lib/toast.svelte.js';
  import ItemRow from './ItemRow.svelte';
  import TreeNode from './TreeNode.svelte';
  import NodePanel from './NodePanel.svelte';

  let { view, error, onmenu, onchange } = $props();

  const needs = $derived(view?.view?.needs_you ?? []);
  const fresh = $derived(view?.view?.new ?? []);
  const runs = $derived(view?.view?.runs ?? []);
  const count = (gs) => gs.reduce((n, g) => n + g.items.length, 0);
  const crumbs = $derived(route.path ? route.path.split('/').map((name, i, all) => ({ name, path: all.slice(0, i + 1).join('/') })) : []);
  // Under a run or deeper, group headers repeat the scope; drop them.
  const deep = $derived(crumbs.length >= 2);

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
      <input id="search" type="search" placeholder="Search questions and notices" bind:value={q} oninput={onsearch} autocomplete="off" />
    </label>
    <a class="btn small toggle" class:on={route.all} href={href({ all: !route.all, item: '' })} title="Include read items and finished runs">
      {route.all ? 'Showing all' : 'Show all'}
    </a>
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
        {#if s?.resume}
          <button class="btn small" onclick={async () => toast((await copy(s.resume)) ? 'Resume command copied.' : s.resume, 'ok')}>Copy resume</button>
        {/if}
        <a class="btn small" href={href({ session: '', item: '' })}>Clear</a>
      </div>
    </div>
  {/if}
</header>

<div class="body">
  {#if error}
    <div class="empty error">Cannot reach the desk: {error}</div>
  {:else if !view}
    <div class="skeleton"></div>
    <div class="skeleton short"></div>
  {:else}
    {#if view.node}
      <div class="phone-node"><NodePanel node={view.node} target={view.target} {onchange} compact /></div>
    {/if}

    <h2 class="section-title">Needs you {#if count(needs)}<span class="count hot">{count(needs)}</span>{/if}</h2>
    {#each needs as g (g.project + '/' + g.run)}
      {#if !deep}<a class="group" href={href({ path: `${g.project}/${g.run}`, item: '' })}>{g.project} <span class="faint">/</span> {g.run}</a>{/if}
      <div class="rows">
        {#each g.items as it (it.event.id)}<ItemRow item={it} />{/each}
      </div>
    {:else}
      <div class="empty">Nothing is waiting on you{route.q ? ' that matches' : ''}.</div>
    {/each}

    <h2 class="section-title">{route.all ? 'Notices and slots' : 'New'} {#if count(fresh)}<span class="count">{count(fresh)}</span>{/if}</h2>
    {#each fresh as g (g.project + '/' + g.run)}
      {#if !deep}<a class="group" href={href({ path: `${g.project}/${g.run}`, item: '' })}>{g.project} <span class="faint">/</span> {g.run}</a>{/if}
      <div class="rows">
        {#each g.items as it (it.event.id)}<ItemRow item={it} />{/each}
      </div>
    {:else}
      <div class="empty">{route.all ? 'No notices here.' : 'All read.'}</div>
    {/each}

    {#if !route.q}
      <h2 class="section-title">Runs</h2>
      {#if runs.length}
        <div class="tree">
          {#each runs as n (n.path)}<TreeNode node={n} />{/each}
        </div>
      {:else}
        <div class="empty">No runs{route.all ? '' : ' with anything to show; Show all includes finished ones'}.</div>
      {/if}
    {/if}
  {/if}
</div>

<style>
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
