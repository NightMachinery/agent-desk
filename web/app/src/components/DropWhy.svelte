<script>
  // The arrow beside a task's Drop button: "Drop why?" offers the usual
  // reasons (DROP_REASONS) or one you type. The reason is the drop note,
  // which the task shows ("Why it was dropped") and its agent is told
  // (taskchanges.go). keyed: the task page's arrow, which , x clicks.
  import Sheet from './Sheet.svelte';
  import { tip } from '../lib/shortcuts.js';
  import { off } from '../lib/why.js';
  import { DROP_REASONS } from '../lib/taskstatus.js';

  let { id = '', ondrop, keyed = false, busy = false } = $props();
  let open = $state(false);
  let why = $state('');
  const title = $derived(tip('Drop it with a reason: inconclusive, not worth it, or your own', keyed ? 'task-drop-why' : ''));
  function pick(note) {
    open = false;
    why = '';
    ondrop(note);
  }
</script>

{#snippet arrow()}<svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><path d="m2 3.5 3 3 3-3" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>{/snippet}
{#if keyed}
  <button data-cmd="task-drop-why" class="btn small ghost why" aria-label={`Drop ${id} with a reason`} {...off(title, [busy, 'Wait: the last request is still running'])} onclick={() => (open = true)}>{@render arrow()}</button>
{:else}
  <button data-keys="none" class="btn small why" aria-label={`Drop ${id} with a reason`} {...off(title, [busy, 'Wait: the last request is still running'])} onclick={() => (open = true)}>{@render arrow()}</button>
{/if}

<Sheet bind:open title={`Drop ${id} why?`}>
  <div class="choices">
    {#each DROP_REASONS as r (r)}
      <button data-keys="none" class="btn" onclick={() => pick(r)}>{r}</button>
    {/each}
    <form class="other" onsubmit={(e) => (e.preventDefault(), why.trim() && pick(why.trim()))}>
      <input class="input" bind:value={why} placeholder="Or say why" aria-label={`Why drop ${id}`} autocomplete="off" />
      <button data-keys="none" class="btn small" type="submit" {...off('Drop it with this reason', [!why.trim(), 'Say why first'])}>Drop</button>
    </form>
  </div>
</Sheet>

<style>
  .why {
    padding-left: 6px;
    padding-right: 6px;
  }
  .choices {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .choices > .btn {
    justify-content: flex-start;
  }
  .other {
    display: flex;
    gap: 6px;
    margin-top: 4px;
  }
  .other .input {
    flex: 1;
    min-width: 0;
  }
</style>
