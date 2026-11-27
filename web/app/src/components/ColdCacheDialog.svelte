<script>
  // Asks before a message goes to an agent whose prompt cache is probably
  // cold. A Sheet, so the keys work in it (f, j and k, q); Escape, a tap
  // outside and Cancel all cancel the send.
  import Sheet from './Sheet.svelte';
  import { coldCache, finishCacheChoice } from '../lib/cache.svelte.js';
  import { off } from '../lib/why.js';
</script>

<Sheet title="This agent’s cache is probably cold" done=""
  bind:open={() => coldCache.open, (v) => { if (!v && coldCache.open) finishCacheChoice('cancel'); }}>
  <p>Compaction reads the full context once and can lose detail. It can save time and tokens if more turns follow.</p>
  <div class="actions">
    <button data-cmd="sheet-close" class="btn" onclick={() => finishCacheChoice('cancel')}>Cancel</button>
    <button data-keys="none" class="btn" onclick={() => finishCacheChoice('send')}>Send anyway</button>
    <button data-keys="none" class="btn primary" {...off(undefined, [!coldCache.enabled, 'Compaction is unavailable for this agent'])}
      onclick={() => finishCacheChoice('compact')}>Compact then send</button>
  </div>
</Sheet>

<style>
  p {
    margin: 0 0 14px;
    color: var(--muted);
    line-height: 1.45;
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 8px;
  }
</style>
