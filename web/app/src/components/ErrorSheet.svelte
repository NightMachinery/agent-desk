<script>
  // The error sheet: the first failure in errors.svelte.js, with what can be
  // done about it. A Sheet, so the keys work in it; Escape, q and Dismiss
  // drop the failure, and the next one shows.
  import Sheet from './Sheet.svelte';
  import { errors, dismissError, errorReport, explain } from '../lib/errors.svelte.js';
  import { copy } from '../lib/format.js';
  import { toast } from '../lib/toast.svelte.js';
  import { ctx } from '../lib/keys.svelte.js';

  const cur = $derived(errors.list[0]);
  const more = $derived(errors.list.length - 1);
  let busy = $state(false);

  async function retry() {
    const x = cur;
    busy = true;
    try {
      await x.retry();
      dismissError(x.id);
      toast('Done.', 'ok');
      ctx.refresh();
    } catch (err) {
      Object.assign(x, { message: err.message, status: err.status ?? x.status, at: Date.now(), count: x.count + 1 });
    } finally {
      busy = false;
    }
  }
  async function copyLog() {
    const ok = await copy(errorReport(cur));
    toast(ok ? 'Log copied: paste it to an agent, or into an issue.' : 'The browser would not copy it.', ok ? 'ok' : 'info', 5000);
  }
  function run(a) {
    const x = cur;
    a.run();
    dismissError(x.id);
  }
  const time = (ms) => new Date(ms).toLocaleTimeString();
</script>

<Sheet title={cur ? `Couldn’t ${cur.what}` : ''} done=""
  bind:open={() => !!cur, (v) => { if (!v && cur) dismissError(cur.id); }}>
  {#if cur}
    {#if explain(cur)}<p class="why">{explain(cur)}</p>{/if}
    <p class="msg">{cur.message}</p>
    <details>
      <summary>Details</summary>
      <dl>
        {#if cur.request}<dt>Request</dt><dd class="mono">{cur.request.method} api/{cur.request.name}</dd>{/if}
        {#if cur.status != null}<dt>Status</dt><dd class="mono">{cur.status || 'no answer'}</dd>{/if}
        <dt>When</dt><dd>{time(cur.at)}{cur.count > 1 ? ` (${cur.count} times)` : ''}</dd>
      </dl>
    </details>
    <div class="actions">
      {#if more > 0}<span class="faint more">{more} more after this</span>{/if}
      <button data-cmd="sheet-close" class="btn" onclick={() => dismissError(cur.id)}>Dismiss</button>
      <button data-keys="none" class="btn" title="Copy what failed and the last requests, to paste to an agent or into an issue" onclick={copyLog}>Copy log</button>
      {#each cur.actions as a (a.label)}
        <button data-keys="none" class="btn" onclick={() => run(a)}>{a.label}</button>
      {/each}
      {#if cur.retry}<button data-keys="none" class="btn primary" disabled={busy} onclick={retry}>{busy ? 'Retrying…' : 'Retry'}</button>{/if}
    </div>
  {/if}
</Sheet>

<style>
  .why {
    margin: 0 0 8px;
    line-height: 1.45;
  }
  .msg {
    margin: 0 0 10px;
    padding: 8px 10px;
    border-radius: 8px;
    background: var(--blocked-soft);
    color: var(--blocked);
    font-size: 13px;
    line-height: 1.45;
    overflow-wrap: anywhere;
  }
  details {
    margin: 0 0 14px;
    font-size: 12.5px;
    color: var(--muted);
  }
  summary {
    cursor: pointer;
  }
  dl {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 2px 10px;
    margin: 6px 0 0;
  }
  dt {
    color: var(--faint);
  }
  dd {
    margin: 0;
  }
  .mono {
    font-family: var(--mono);
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    align-items: center;
    gap: 8px;
  }
  .more {
    margin-right: auto;
    font-size: 12.5px;
  }
</style>
