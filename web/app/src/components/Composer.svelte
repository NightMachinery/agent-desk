<script>
  // A text box that sends on ⌘/Ctrl+Enter, with optional quick replies.
  let { id = undefined, label, placeholder, send, hint = '', quick = [], sendLabel = 'Send' } = $props();
  let text = $state('');
  let busy = $state(false);

  async function submit(value = text) {
    if (!value.trim() || busy) return;
    busy = true;
    try {
      if (await send(value)) text = '';
    } finally {
      busy = false;
    }
  }
  function onkeydown(e) {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      submit();
    }
  }
</script>

<div class="composer">
  {#if label}<label class="label" for={id}>{label}</label>{/if}
  <textarea {id} class="input" rows="3" {placeholder} bind:value={text} {onkeydown} disabled={busy}></textarea>
  <div class="bar">
    {#each quick as q (q.label)}
      <button class="btn small" disabled={busy} onclick={() => submit(q.text)} title={q.text}>{q.label}</button>
    {/each}
    <span class="hint faint">{hint}</span>
    <button class="btn primary small" disabled={busy || !text.trim()} onclick={() => submit()}>
      {busy ? 'Sending…' : sendLabel}
    </button>
  </div>
</div>

<style>
  .composer {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .label {
    font-weight: 600;
    font-size: 13px;
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  .hint {
    flex: 1;
    min-width: 120px;
    font-size: 12px;
  }
</style>
