<script>
  import { coldCache, finishCacheChoice } from '../lib/cache.svelte.js';
  import { off } from '../lib/why.js';

  let dialog = $state();

  $effect(() => {
    if (!dialog) return;
    if (coldCache.open) dialog.showModal();
    else if (dialog.open) dialog.close();
  });
</script>

<dialog bind:this={dialog} aria-labelledby="cold-cache-title"
  onkeydown={(e) => e.stopPropagation()}
  oncancel={(e) => { e.preventDefault(); finishCacheChoice('cancel'); }}>
  <h3 id="cold-cache-title">This agent’s cache is probably cold</h3>
  <p>Compaction reads the full context once and can lose detail. It can save time and tokens if more turns follow.</p>
  <div class="actions">
    <button class="btn" onclick={() => finishCacheChoice('cancel')}>Cancel</button>
    <button class="btn" onclick={() => finishCacheChoice('send')}>Send anyway</button>
    <button class="btn primary" {...off(undefined, [!coldCache.enabled, 'Compaction is unavailable for this agent'])}
      onclick={() => finishCacheChoice('compact')}>Compact then send</button>
  </div>
</dialog>

<style>
  dialog {
    box-sizing: border-box;
    width: min(440px, calc(100% - 24px));
    padding: 20px;
    border: 1px solid var(--line);
    border-radius: 12px;
    color: var(--text);
    background: var(--panel);
  }
  dialog::backdrop {
    background: rgb(0 0 0 / 42%);
  }
  h3 { margin: 0; }
  p { color: var(--muted); }
  .actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 8px;
  }
</style>
