<script>
  // Auto-continue for one Paseo agent: when its own quota runs out, the
  // watcher ([quota] auto_continue_command) sends it Continue. once the
  // quota returns. Hidden when the desk has no auto_continue_command.
  import { get, post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { off, BUSY } from '../lib/why.js';

  let { paseo = '' } = $props();
  let ac = $state({ available: false, on: false });
  let busy = $state(false);

  async function load() {
    if (!paseo) return;
    try {
      ac = await get('auto-continue', { paseo });
    } catch {
      ac = { available: false, on: false };
    }
  }
  $effect(() => {
    void paseo;
    load();
  });

  async function toggle() {
    busy = true;
    const on = !ac.on;
    try {
      await post('quota', { action: on ? 'auto-continue-on' : 'auto-continue-off', id: paseo });
      toast(on ? 'Auto-continue on: it resumes when its quota returns.' : 'Auto-continue off.', 'ok');
      await load();
    } catch (err) {
      toast(err.message, 'error');
    } finally {
      busy = false;
    }
  }
</script>

{#if ac.available}
  <label class="toggle" title="When this agent runs out of quota, send it Continue. once its quota returns (the auto-continue watcher)">
    <input type="checkbox" checked={ac.on} {...off(undefined, [busy, BUSY])} onchange={toggle} data-cmd="auto-continue" />
    Auto-continue
  </label>
{/if}

<style>
  .toggle {
    display: inline-flex;
    gap: 5px;
    align-items: center;
    font-size: 12.5px;
    color: var(--muted);
    cursor: pointer;
    margin-left: 6px;
  }
</style>
