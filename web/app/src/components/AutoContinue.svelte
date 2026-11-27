<script>
  // Auto-continue for one Paseo agent: when its own quota runs out, the
  // watcher ([quota] auto_continue_command) sends it Continue. once the
  // quota returns. Hidden when the desk has no auto_continue_command.
  // Where agent-quotas runs, an agent nobody decided for follows the
  // auto-continue.main or .subagents setting inherited at its node; ticking
  // the box decides for it alone, and Use default undoes that.
  import { get, post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { off, BUSY } from '../lib/why.js';
  import { tip } from '../lib/shortcuts.js';
  import { failed } from '../lib/errors.svelte.js';

  let { paseo = '' } = $props();
  /** @typedef {{available: boolean, on: boolean, set_by?: string, default?: {key: string, on: boolean, scope: string, orphan?: boolean, origin?: {source: string, scope?: string}}}} ACState */
  let ac = $state(/** @type {ACState} */ ({ available: false, on: false }));
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

  const explicit = $derived(!!ac.set_by && ac.set_by !== 'default');
  const tipText = $derived.by(() => {
    const d = ac.default;
    const state = ac.on ? 'on' : 'off';
    if (!d) return 'When this agent runs out of quota, send it Continue. once its quota returns (the auto-continue watcher)';
    const kind = d.key === 'auto-continue.subagents' ? 'subagents' : 'main agents';
    const from = d.orphan
      ? 'off, since its parent is archived'
      : `${d.on ? 'on' : 'off'}, ${d.origin?.source === 'pref' ? (d.origin.scope ? `set at ${d.origin.scope}` : 'set globally') : d.origin?.source === 'config' ? 'a config default' : 'built in'}`;
    if (explicit) return `Auto-continue ${state} for this agent alone: its own choice. The default for ${kind} here is ${from}; Use default follows it again`;
    if (ac.on === d.on) return `Auto-continue ${state}: the default for ${kind} here (${from}). Ticking the box decides for this agent alone`;
    return `Auto-continue ${state} for now: the default for ${kind} here is ${from}, and applies within a few minutes, or once the agent works again`;
  });

  async function useDefault() {
    busy = true;
    try {
      const res = await post('quota', { action: 'auto-continue-default', id: paseo });
      toast(res.note || 'It follows its default again.', 'ok');
      await load();
    } catch (err) {
      failed(err);
    } finally {
      busy = false;
    }
  }

  async function toggle() {
    busy = true;
    const on = !ac.on;
    try {
      await post('quota', { action: on ? 'auto-continue-on' : 'auto-continue-off', id: paseo });
      toast(on ? 'Auto-continue on: it resumes when its quota returns.' : 'Auto-continue off.', 'ok');
      await load();
    } catch (err) {
      failed(err);
    } finally {
      busy = false;
    }
  }
</script>

{#if ac.available}
  <label class="toggle" title={tipText}>
    <input type="checkbox" checked={ac.on} {...off(undefined, [busy, BUSY])} onchange={toggle} data-cmd="auto-continue" />
    Auto-continue
  </label>
  {#if explicit && ac.default}
    <button data-cmd="auto-continue-default" class="btn ghost small" {...off(tip(`Drop this agent's own choice: it follows the default for its role again (${ac.default.on ? 'on' : 'off'})`, 'auto-continue-default'), [busy, BUSY])} onclick={useDefault}>Use default</button>
  {/if}
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
