<script>
  import { off } from '../lib/why.js';
  // Reopen a finished task, with an optional note on what is still wrong:
  // the note stays on the task and goes to the agent working on it
  // (taskchanges.go). Enter reopens, with or without a note; Escape cancels.
  let { id = '', onreopen, oncancel } = $props();
  let note = $state('');
  let busy = $state(false);
  /** @type {HTMLInputElement | undefined} */
  let input = $state();
  $effect(() => input?.focus());
  async function submit(e) {
    e?.preventDefault();
    if (busy) return;
    busy = true;
    try {
      await onreopen(note.trim());
    } finally {
      busy = false;
    }
  }
</script>

<form class="reopen" onsubmit={submit}>
  <input bind:this={input} class="input" bind:value={note} placeholder="What is still wrong? (optional)" aria-label={`Why reopen ${id}`} autocomplete="off"
    onkeydown={(e) => e.key === 'Escape' && (e.preventDefault(), oncancel())} />
  <button data-keys="none" class="btn small primary" type="submit" {...off(undefined, [busy, 'Wait: reopening'])}>Reopen</button>
  <button data-keys="none" class="btn small ghost" type="button" onclick={oncancel}>Cancel</button>
</form>

<style>
  .reopen {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }
  .input {
    flex: 1 1 220px;
    min-width: 0;
  }
</style>
