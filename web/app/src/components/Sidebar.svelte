<script>
  import { route, href } from '../lib/route.svelte.js';
  import { stateClass } from '../lib/format.js';

  let { view, live } = $props();

  const projects = $derived(view?.view?.runs ?? []);
  const open = $derived(projects.reduce((n, p) => n + p.open_questions, 0));
  const unread = $derived(projects.reduce((n, p) => n + p.unread, 0));
  const current = $derived(route.path.split('/')[0]);
</script>

<nav>
  <a class="brand" href={href({ path: '', item: '', session: '', q: '' })}>
    <img src="./favicon.svg" alt="" width="22" height="22" />
    <span>agent-desk</span>
    {#if view}<span class="host faint">{view.host}</span>{/if}
  </a>

  <a class="scope" class:on={!route.path && !route.session} href={href({ path: '', item: '', session: '' })}>
    <span class="name">Everything</span>
    {#if open}<span class="count hot" title="Open questions">{open}</span>{/if}
    {#if unread}<span class="count" title="Unread">{unread}</span>{/if}
  </a>

  <div class="label">Projects</div>
  {#each projects as p (p.path)}
    <a class="scope" class:on={current === p.name && !route.session} href={href({ path: p.path, item: '', session: '' })}>
      <span class="state {stateClass(p.state)}" title={p.state}></span>
      <span class="name">{p.name}</span>
      {#if p.open_questions}<span class="count hot">{p.open_questions}</span>{/if}
      {#if p.unread}<span class="count">{p.unread}</span>{/if}
    </a>
    {#if current === p.name && p.children?.length}
      <div class="runs">
        {#each p.children as r (r.path)}
          <a class="scope run" class:on={route.path.startsWith(r.path) && !route.session} href={href({ path: r.path, item: '', session: '' })}>
            <span class="state {stateClass(r.state)}" title={r.state}></span>
            <span class="name">{r.name}</span>
            {#if r.open_questions}<span class="count hot">{r.open_questions}</span>{/if}
          </a>
        {/each}
      </div>
    {/if}
  {:else}
    <p class="faint none">No projects yet. Agents appear here when they file.</p>
  {/each}

  {#if (view?.hosts?.length ?? 0) > 1}
    <div class="label">Machines</div>
    <a class="scope" class:on={!route.host} href={href({ host: '', item: '' })}><span class="name">All machines</span></a>
    {#each view.hosts as h (h)}
      <a class="scope" class:on={route.host === h} href={href({ host: h, item: '' })}><span class="name">{h}</span></a>
    {/each}
  {/if}

  <div class="foot">
    <span class="live {live}"><span class="dot"></span>{live === 'live' ? 'Live' : live === 'reconnecting' ? 'Reconnecting…' : 'Connecting…'}</span>
    <a class="faint" class:on={route.settings} href={href({ path: '', item: '', session: '', settings: true })}>Settings</a>
    <a class="faint" href="https://github.com/NightMachinery/agent-desk" target="_blank" rel="noreferrer">About</a>
  </div>
</nav>

<style>
  nav {
    display: flex;
    flex-direction: column;
    min-height: 100%;
    padding: 14px 10px calc(14px + env(safe-area-inset-bottom));
    gap: 1px;
  }
  .brand {
    display: flex;
    align-items: center;
    gap: 9px;
    padding: 4px 8px 16px;
    font-weight: 700;
    font-size: 15px;
    letter-spacing: -0.01em;
  }
  .brand img {
    border-radius: 6px;
  }
  .host {
    margin-left: auto;
    font-weight: 500;
    font-size: 12px;
  }
  .label {
    margin: 18px 8px 6px;
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--faint);
  }
  .scope {
    display: flex;
    align-items: center;
    gap: 8px;
    height: 32px;
    padding: 0 8px;
    border-radius: 8px;
    color: var(--muted);
    font-weight: 500;
  }
  .scope:hover {
    background: var(--sunken);
    color: var(--text);
  }
  .scope.on {
    background: var(--sunken);
    color: var(--text);
    font-weight: 600;
  }
  .name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .runs {
    margin: 1px 0 4px 14px;
    padding-left: 6px;
    border-left: 1px solid var(--line);
  }
  .run {
    height: 28px;
    font-size: 13px;
  }
  .state {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex: none;
    background: var(--line-strong);
  }
  .state.waiting {
    background: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-soft);
  }
  .state.active {
    background: var(--ok);
  }
  .state.finished {
    background: transparent;
    border: 1.5px solid var(--faint);
  }
  .none {
    padding: 4px 8px;
    margin: 0;
    font-size: 13px;
  }
  .foot {
    margin-top: auto;
    padding: 16px 8px 0;
    display: flex;
    justify-content: space-between;
    font-size: 12px;
  }
  .live {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: var(--faint);
  }
  .live.live {
    color: var(--ok);
  }
  .live.reconnecting {
    color: var(--accent);
  }
</style>
