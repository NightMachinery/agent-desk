<script>
  // Out of quota: the Paseo agents stopped at a usage limit ([quota]
  // stopped_command), with what can be done about each: let it resume when
  // its quota returns, ask its parent to hand its role to an agent with
  // quota, or start a new agent to take over from it. Under a node, only the
  // agents working there (their own node, or an ancestor's) are listed.
  // Several can be selected (checkboxes in the list's Select mode, or
  // ⌘/Ctrl-click a card) to copy their Paseo ids, one per line.
  import { SvelteSet } from 'svelte/reactivity';
  import CopyIds from './CopyIds.svelte';
  import { sel } from '../lib/select.svelte.js';
  import { get, post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { copy, short } from '../lib/format.js';

  let { stamp = '', path = '', collapsed = false, count = $bindable(0) } = $props();
  let data = $state({ agents: [], profiles: [] });
  let busy = $state('');
  let menu = $state(''); // the agent whose Replace menu is open
  let showDismissed = $state(false);

  const inScope = (a) => !path || a.scope === path || (a.scope ?? '').startsWith(path + '/');
  const scoped = $derived((data.agents ?? []).filter(inScope));
  const dismissed = $derived(scoped.filter((a) => a.dismissed).length);
  const shown = $derived(showDismissed ? scoped : scoped.filter((a) => !a.dismissed));
  $effect(() => {
    count = scoped.length - dismissed;
  });

  const picked = new SvelteSet(); // Paseo ids
  let anchor = '';
  const picking = $derived(sel.mode || picked.size > 0);
  const shownIds = $derived(shown.map((a) => a.paseo));
  const allPicked = $derived(shownIds.length > 0 && shownIds.every((x) => picked.has(x)));
  // Leaving the list's Select mode clears these too; agents that leave the
  // list leave the selection.
  let wasMode = false;
  $effect(() => {
    if (wasMode && !sel.mode) picked.clear();
    wasMode = sel.mode;
  });
  $effect(() => {
    for (const x of [...picked]) if (!shownIds.includes(x)) picked.delete(x);
  });
  function pick(id, shift = false) {
    const a = shownIds.indexOf(anchor);
    const b = shownIds.indexOf(id);
    if (shift && a >= 0 && b >= 0) {
      const on = !picked.has(id) || picked.has(anchor);
      for (const x of shownIds.slice(Math.min(a, b), Math.max(a, b) + 1)) on ? picked.add(x) : picked.delete(x);
    } else if (picked.has(id)) picked.delete(id);
    else picked.add(id);
    anchor = id;
  }
  function pickAll() {
    if (allPicked) picked.clear();
    else shownIds.forEach((x) => picked.add(x));
  }
  async function copyPicked() {
    const ids = shownIds.filter((x) => picked.has(x));
    if (await copy(ids.join('\n'))) toast(`Copied ${ids.length} Paseo id${ids.length === 1 ? '' : 's'}, one per line.`, 'ok');
    else toast('Could not copy: the browser refused the clipboard.', 'error');
  }
  function cardClick(e, a) {
    if (!(e.metaKey || e.ctrlKey || (picking && e.shiftKey))) return;
    if (e.target.closest('button, a, input, label, .menu')) return;
    e.preventDefault();
    pick(a.paseo, e.shiftKey);
  }

  async function load(fresh = false) {
    try {
      data = await get('quota', { fresh: fresh ? 1 : '' });
    } catch {
      data = { agents: [], profiles: [] };
    }
  }
  $effect(() => {
    stamp; // reload when the desk changes, and every minute
    load();
    const t = setInterval(load, 60_000);
    return () => clearInterval(t);
  });

  async function act(a, action, label, profile = '') {
    busy = a.paseo + action;
    menu = '';
    try {
      const res = await post('quota', { action, id: a.paseo, profile });
      toast(res.note || label, 'ok', 6000);
      await load(true);
    } catch (err) {
      toast(err.message, 'error', 9000);
    } finally {
      busy = '';
    }
  }
  function custom(a) {
    const p = prompt('Profile for the new agent: provider[/model][@thinking], e.g. codex/gpt-6.1-terra@high');
    if (p?.trim()) act(a, 'replace', 'Started a replacement.', p.trim());
  }
  const ago = (iso) => {
    if (!iso) return '';
    const m = Math.round((Date.now() - new Date(iso)) / 60000);
    return m < 60 ? `${m}m ago` : m < 1440 ? `${Math.round(m / 60)}h ago` : `${Math.round(m / 1440)}d ago`;
  };
</script>

<svelte:window onclick={() => (menu = '')} />

<div class="rows" class:hidden={collapsed}>
  {#if picking && shown.length}
    <div class="selbar">
      <button class="btn small ghost" onclick={pickAll}>{allPicked ? 'Deselect all' : 'Select all'}</button>
      <span class="faint">{picked.size} selected</span>
      <button class="btn small" data-cmd="quota-copy-ids" disabled={!picked.size} onclick={copyPicked}
        title="Copy the selected agents' Paseo ids, one per line">Copy Paseo IDs</button>
      {#if picked.size && !sel.mode}
        <button class="icon" title="Clear the selection" aria-label="Clear the selection" onclick={() => picked.clear()}>
          <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" /></svg>
        </button>
      {/if}
    </div>
  {/if}
  {#each shown as a (a.paseo)}
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
    <div class="q" class:faded={a.dismissed} class:picked={picked.has(a.paseo)} onclick={(e) => cardClick(e, a)}>
      <div class="top">
        {#if picking}
          <input type="checkbox" class="pickbox" checked={picked.has(a.paseo)} aria-label={`Select ${a.title || short(a.paseo)}`}
            onclick={(e) => pick(a.paseo, e.shiftKey)} />
        {/if}
        <strong class="title" title={a.title}>{a.title || short(a.paseo)}</strong>
        <div class="tools">
          <CopyIds paseo={a.paseo} keys={false} />
          {#if a.dismissed}
            <button class="icon" title="Show it again" disabled={!!busy} onclick={() => act(a, 'undismiss', 'Shown again.')}>
              <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true"><path d="M3 8a5 5 0 1 0 1.5-3.5M3 2.5V5h2.5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
            </button>
          {:else}
            <button class="icon" data-cmd="quota-dismiss" title="Dismiss: hide it until it runs and stops again" disabled={!!busy} onclick={() => act(a, 'dismiss', 'Dismissed.')}>
              <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" /></svg>
            </button>
          {/if}
        </div>
      </div>
      <div class="meta faint">
        <span>{a.provider}</span>
        {#if a.last_active}<span>stopped {ago(a.last_active)}</span>{/if}
        {#if a.parent}<span>parent {a.parent.title || short(a.parent.paseo)}{#if a.parent.stopped}&nbsp;(out of quota too){/if}</span>{/if}
        {#if a.children.length}<span>{a.children.length} subagent{a.children.length > 1 ? 's' : ''}{#if a.children.some((c) => c.stopped)}, {a.children.filter((c) => c.stopped).length} out of quota{/if}</span>{/if}
        {#if a.node && a.node !== path}<span class="mono">{a.node}</span>{/if}
      </div>
      <div class="acts">
        {#if data.auto_continue}
          <label class="toggle" title="Send it Continue. when its own quota returns (the auto-continue watcher)">
            <input type="checkbox" checked={a.auto_continue} disabled={!!busy}
              onchange={() => act(a, a.auto_continue ? 'auto-continue-off' : 'auto-continue-on', a.auto_continue ? 'Auto-continue off.' : 'Auto-continue on.')} />
            Auto-continue
          </label>
        {/if}
        <div class="btns">
        {#if a.parent && !a.parent.stopped}
          <button class="btn small" disabled={!!busy} title="Message its parent: archive it and give its role to a new subagent on an account with quota left, recovering its partial work and minding its own subagents (prompt: reassign-role.md)"
            onclick={() => act(a, 'ask-parent', 'Asked its parent.')}>Ask parent to reassign</button>
        {/if}
        {#if data.launch_ready}
          <div class="split">
            <button class="btn small main" disabled={!!busy} title="Start a new agent, with launch_command's default profile, that recovers its partial work, takes over its role and archives it (prompt: replace-agent.md)"
              onclick={() => act(a, 'replace', 'Started a replacement.')}>Replace</button>
            <button class="btn small caret" disabled={!!busy} aria-label="Replace with another profile" title="Replace with another profile"
              onclick={(e) => { e.stopPropagation(); menu = menu === a.paseo ? '' : a.paseo; }}>
              <svg width="10" height="10" viewBox="0 0 16 16" aria-hidden="true"><path d="M4 6l4 4 4-4" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" /></svg>
            </button>
            {#if menu === a.paseo}
              <div class="menu" role="menu" onclick={(e) => e.stopPropagation()}>
                <div class="menu-head faint">Replace with a new agent on</div>
                <button role="menuitem" onclick={() => act(a, 'replace', 'Started a replacement.')}>The default profile</button>
                {#each data.profiles ?? [] as p (p)}
                  <button role="menuitem" class="mono" onclick={() => act(a, 'replace', 'Started a replacement.', p)}>{p}</button>
                {/each}
                <button role="menuitem" onclick={() => custom(a)}>Another profile…</button>
              </div>
            {/if}
          </div>
        {/if}
        </div>
      </div>
    </div>
  {/each}
  {#if dismissed}
    <button class="more faint" onclick={() => (showDismissed = !showDismissed)}>
      {showDismissed ? 'Hide' : 'Show'} {dismissed} dismissed
    </button>
  {/if}
</div>

<style>
  .hidden {
    display: none !important;
  }
  .rows {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .q {
    padding: 10px 12px;
    border-radius: 10px;
    background: var(--panel);
    border: 1px solid var(--line);
    border-left: 3px solid var(--accent);
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .q.faded {
    opacity: 0.6;
  }
  .q.picked {
    background: var(--accent-soft);
  }
  .pickbox {
    flex: none;
    margin: 0;
    width: 15px;
    height: 15px;
    accent-color: var(--accent);
    cursor: pointer;
  }
  .selbar {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12.5px;
  }
  .selbar .btn:first-child {
    margin-left: -6px;
  }
  .selbar [data-cmd='quota-copy-ids'] {
    margin-left: auto;
  }
  .top {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 14px;
  }
  .tools {
    display: flex;
    align-items: center;
    gap: 4px;
    flex: none;
  }
  .icon {
    display: inline-grid;
    place-items: center;
    width: 26px;
    height: 26px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--muted);
    cursor: pointer;
  }
  .icon:hover {
    background: var(--sunken);
    color: var(--text);
  }
  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: 0 6px;
    font-size: 12.5px;
  }
  .meta span + span::before {
    content: '·';
    margin-right: 6px;
  }
  .acts {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
    margin-top: 2px;
  }
  .btns {
    /* the buttons wrap as one group, kept to the right */
    display: flex;
    gap: 8px;
    align-items: center;
    margin-left: auto;
  }
  .toggle {
    display: inline-flex;
    gap: 6px;
    align-items: center;
    font-size: 13px;
    cursor: pointer;
  }
  .split {
    position: relative;
    display: inline-flex;
  }
  .split .main {
    border-top-right-radius: 0;
    border-bottom-right-radius: 0;
  }
  .split .caret {
    border-top-left-radius: 0;
    border-bottom-left-radius: 0;
    border-left: 0;
    padding-left: 6px;
    padding-right: 6px;
  }
  .menu {
    position: absolute;
    right: 0;
    top: calc(100% + 4px);
    z-index: 20;
    min-width: 230px;
    padding: 4px;
    border-radius: 10px;
    background: var(--panel);
    border: 1px solid var(--line);
    box-shadow: 0 8px 24px rgb(0 0 0 / 0.14);
    display: flex;
    flex-direction: column;
  }
  .menu-head {
    font-size: 11.5px;
    padding: 4px 8px 2px;
  }
  .menu button {
    text-align: left;
    border: 0;
    background: transparent;
    padding: 6px 8px;
    border-radius: 6px;
    font: inherit;
    font-size: 13px;
    color: inherit;
    cursor: pointer;
  }
  .menu button:hover {
    background: var(--sunken);
  }
  .more {
    align-self: flex-start;
    border: 0;
    background: transparent;
    font: inherit;
    font-size: 12.5px;
    cursor: pointer;
    text-decoration: underline;
    padding: 2px 0;
  }
</style>
