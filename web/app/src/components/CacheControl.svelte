<script>
  import { post } from '../lib/api.js';
  import { cache, refreshCache } from '../lib/cache.svelte.js';
  import { toast } from '../lib/toast.svelte.js';

  let { paseo, showUnavailable = false } = $props();
  let busy = $state(false);
  const agent = $derived(cache.state?.agents?.[paseo?.toLowerCase()]);
  const cold = $derived(agent?.big && agent.expires_at != null && agent.expires_at * 1000 < Date.now());
  const label = $derived.by(() => {
    let text = 'off';
    if (agent?.phase === 'warming') {
      text = agent.warm_until
        ? 'warm until ' + new Date(agent.warm_until * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
        : 'warm';
    } else if (agent) {
      text = agent.phase || agent.policy;
    }
    return cold ? (text === 'off' ? 'cold' : text + ' · cold') : 'cache ' + text;
  });
  const policies = ['off', 'compact', 'warm'];

  async function setPolicy(e) {
    const control = e.currentTarget;
    const action = control.value;
    const id = paseo.toLowerCase();
    busy = true;
    try {
      const result = await post('cache', { id, action });
      if (cache.state?.agents) cache.state.agents[id] = result;
      await refreshCache();
    } catch (err) {
      control.value = agent?.policy || 'off';
      toast(err.message, 'error');
    } finally {
      busy = false;
    }
  }
</script>

{#if agent}
  <select aria-label="Cache policy" title={cache.state.message || 'Cache policy'} value={agent.policy}
    disabled={busy || !cache.state.enabled || agent.archived} onchange={setPolicy}>
    {#each policies as policy (policy)}
      <option value={policy}>{agent.policy === policy ? label : policy}</option>
    {/each}
  </select>
{:else if showUnavailable && cache.state && !cache.state.available}
  <span class="faint">{cache.state.message}</span>
{/if}

<style>
  select {
    flex: none;
    max-width: 100%;
    border: 1px solid var(--line);
    border-radius: 6px;
    background: var(--panel);
    color: var(--muted);
    font: inherit;
    font-size: 12px;
    padding: 2px;
  }
  span { font-size: 12px; }
</style>
