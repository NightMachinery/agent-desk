<script>
  // Machines whose screen an agent holds (agent-desk screen take): who,
  // why and until when, and who the user approved to go next, with a button
  // to take it back on this machine.
  import { post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { agentName } from '../lib/format.js';
  import { failed } from '../lib/errors.svelte.js';

  let { screens = [], host = '', onchange } = $props();
  const held = $derived(screens.filter((s) => s.held));
  const time = (iso) => new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });

  async function takeBack() {
    try {
      const res = await post('screen', { action: 'release' });
      toast(res?.next ? `Took the screen back. ${agentName(res.next)}, which you approved next, has it now.` : 'Took the screen back. The banner is down.', 'ok');
      onchange();
    } catch (err) {
      failed(err);
    }
  }
</script>

{#each held as s (s.host)}
  <div class="screen" role="status">
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><rect x="1.5" y="2.5" width="13" height="9" rx="1.5" fill="none" stroke="currentColor" stroke-width="1.5" /><path d="M5.5 14h5M8 11.5V14" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" /></svg>
    <div class="what">
      <strong>{agentName(s.agent) || 'An agent'}</strong> is driving the screen on {s.host} until {time(s.until)}{s.by === 'auto' ? ' (you were away)' : ''}.
      <span class="faint">{s.reason}</span>
      {#each s.queued ?? [] as q (q.request)}
        <span class="faint">Next, approved by you: <strong>{agentName(q.agent) || 'an agent'}</strong> ({q.request}), {q.reason}</span>
      {/each}
    </div>
    {#if s.host === host}<button data-cmd="screen-release" class="btn small" title="Release the screen lock and take the banner down; the agent is not stopped" onclick={takeBack}>Take it back</button>{/if}
  </div>
{/each}

<style>
  .screen {
    display: flex;
    align-items: center;
    gap: 10px;
    margin: 4px 0 12px;
    padding: 10px 12px;
    border-radius: 10px;
    background: var(--sunken);
    border: 1px solid var(--line-strong);
  }
  .what {
    flex: 1;
    min-width: 0;
  }
  .what .faint {
    display: block;
    font-size: 0.9em;
  }
</style>
