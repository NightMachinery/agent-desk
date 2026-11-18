<script>
  // Out of quota: the Paseo agents stopped at a usage limit ([quota]
  // stopped_command), with what can be done about each: let it resume when
  // its quota returns, ask its parent to hand its role to an agent with
  // quota, or start a new agent to take over from it.
  import CopyIds from './CopyIds.svelte';
  import { get, post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { short } from '../lib/format.js';

  let { stamp = '', count = $bindable(0) } = $props();
  let data = $state({ agents: [] });
  let busy = $state('');
  let profiles = $state({});

  async function load(fresh = false) {
    try {
      data = await get('quota', { fresh: fresh ? 1 : '' });
    } catch {
      data = { agents: [] };
    }
    count = data.agents?.length ?? 0;
  }
  $effect(() => {
    stamp; // reload when the desk changes, and every minute
    load();
    const t = setInterval(load, 60_000);
    return () => clearInterval(t);
  });

  async function act(a, action, label) {
    busy = a.paseo + action;
    try {
      const res = await post('quota', { action, id: a.paseo, profile: profiles[a.paseo] || '' });
      toast(res.note || label, 'ok', 6000);
      await load(true);
    } catch (err) {
      toast(err.message, 'error', 9000);
    } finally {
      busy = '';
    }
  }
  const ago = (iso) => {
    if (!iso) return '';
    const m = Math.round((Date.now() - new Date(iso)) / 60000);
    return m < 60 ? `${m}m ago` : m < 1440 ? `${Math.round(m / 60)}h ago` : `${Math.round(m / 1440)}d ago`;
  };
</script>

<div class="rows">
  {#each data.agents as a (a.paseo)}
    <div class="q">
      <div class="top">
        <span class="chip warn">Out of quota</span>
        <strong class="title">{a.title || short(a.paseo)}</strong>
        <CopyIds paseo={a.paseo} keys={false} />
      </div>
      <div class="meta faint">
        {a.provider}{#if a.last_active}&nbsp;· stopped {ago(a.last_active)}{/if}
        {#if a.parent} · parent {a.parent.title || short(a.parent.paseo)}{#if a.parent.stopped}&nbsp;(out of quota too){/if}{/if}
        {#if a.children.length} · {a.children.length} subagent{a.children.length > 1 ? 's' : ''}{#if a.children.some((c) => c.stopped)}, {a.children.filter((c) => c.stopped).length} out of quota{/if}{/if}
        {#if a.node} · {a.node}{/if}
      </div>
      <div class="acts">
        {#if data.auto_continue}
          <label class="toggle" title="Send it Continue. when its own quota returns (the auto-continue watcher)">
            <input type="checkbox" checked={a.auto_continue} disabled={!!busy}
              onchange={() => act(a, a.auto_continue ? 'auto-continue-off' : 'auto-continue-on', a.auto_continue ? 'Auto-continue off.' : 'Auto-continue on.')} />
            Auto-continue
          </label>
        {/if}
        {#if a.parent && !a.parent.stopped}
          <button class="btn small" disabled={!!busy} title="Message its parent: archive it and give its role to a new subagent on an account with quota left, recovering its partial work and minding its own subagents (prompt: reassign-role.md)"
            onclick={() => act(a, 'ask-parent', 'Asked its parent.')}>Ask parent to reassign</button>
        {/if}
        {#if data.launch_ready}
          <input class="profile" placeholder="profile (default)" title="The new agent's provider[/model][@thinking]; empty uses launch_command's default" bind:value={profiles[a.paseo]} />
          <button class="btn small" disabled={!!busy} title="Start a new agent that recovers its partial work, takes over its role and archives it (prompt: replace-agent.md)"
            onclick={() => act(a, 'replace', 'Started a replacement.')}>Replace with a new agent</button>
        {/if}
      </div>
    </div>
  {/each}
</div>

<style>
  .q {
    padding: 10px 12px;
    border-radius: 10px;
    background: var(--card, var(--sunken));
    border: 1px solid var(--line);
    display: grid;
    gap: 6px;
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
  }
  .meta {
    font-size: 0.9em;
  }
  .acts {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
  }
  .toggle {
    display: inline-flex;
    gap: 6px;
    align-items: center;
  }
  .profile {
    width: 11em;
    font: inherit;
    font-size: 0.9em;
    padding: 4px 6px;
  }
</style>
