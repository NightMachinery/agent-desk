<script>
  import { onMount } from 'svelte';
  import { get, post } from '../lib/api.js';
  import { href } from '../lib/route.svelte.js';
  import { off, BUSY } from '../lib/why.js';

  let { node = '', insert = null } = $props();
  let items = $state([]);
  let backends = $state([]);
  let choices = $state({});
  let loading = $state(true);
  let error = $state('');
  let confirm = $state('');
  let busy = $state('');
  let keep = $state();

  async function refresh() {
    const result = await get('recordings', { node });
    items = result.recordings;
    backends = result.backends;
    keep = result.keep_days;
  }

  onMount(() => {
    let stopped = false;
    async function poll() {
      try {
        await refresh();
      } catch (err) {
        if (!stopped) error = err.message;
      } finally {
        if (!stopped) loading = false;
      }
    }
    void poll();
    const timer = setInterval(() => {
      if (!busy && items.some((item) => item.status === 'recording' || item.status === 'transcribing')) void poll();
    }, 1000);
    return () => { stopped = true; clearInterval(timer); };
  });

  async function act(item, action) {
    busy = item.id;
    error = '';
    try {
      await post(`recordings/${item.id}`, { action, backend: choices[item.id] || item.backend });
      await refresh();
      confirm = '';
    } catch (err) {
      error = err.message;
    } finally {
      busy = '';
    }
  }

  const labels = { recording: 'Recording', transcribing: 'Transcribing', ready: 'Ready', failed: 'Transcription failed' };
</script>

{#if error}<p class="error" role="alert">{error}</p>{/if}
{#if loading}
  <p class="faint">Loading recordings…</p>
{:else if !items.length}
  <p class="faint">No recordings yet.</p>
{:else}
  {#if keep}<p class="faint">Audio can be retried for {keep} {keep === 1 ? 'day' : 'days'}.</p>{/if}
  <ul class="recordings">
    {#each items as item (item.id)}
      <li>
        <div class="meta">
          <time datetime={item.created}>{new Date(item.created).toLocaleString()}</time>
          <span>{item.backend} · {labels[item.status]}</span>
          {#if !node}
            <a href={href({ path: item.node, item: '', session: '', settings: false })}>{item.node}</a>
          {/if}
        </div>
        {#if item.audio && item.status !== 'recording'}
          <audio controls preload="none" src={item.audio}></audio>
        {/if}
        {#if item.text || item.partial}<p class="transcript">{item.text || item.partial}</p>{/if}
        {#if item.error}<p class="error">{item.error}</p>{/if}
        {#if item.messages?.length}
          <div class="meta">
            Sent in
            {#each item.messages as message (message)}
              <a href={href({ item: message, path: '', session: '', settings: false })}>{message}</a>
            {/each}
          </div>
        {/if}
        {#if item.status !== 'recording' && item.status !== 'transcribing'}
          <div class="actions">
            {#if insert && item.text}
              <button data-keys="none" class="btn small" onclick={() => insert(item)}>Insert transcript</button>
            {/if}
            {#if item.audio}
              <select class="input" aria-label="Transcription backend" bind:value={choices[item.id]}>
                <option value={undefined}>{item.backend} (last used)</option>
                {#each backends.filter((b) => b !== item.backend) as backend (backend)}
                  <option value={backend}>{backend}</option>
                {/each}
              </select>
              <button data-keys="none" class="btn small" {...off(undefined, [busy, BUSY])} onclick={() => act(item, 'retry')}>Retry transcription</button>
            {/if}
            {#if confirm === item.id}
              <button data-keys="none" class="btn small" {...off(undefined, [busy, BUSY])} onclick={() => act(item, 'delete')}>Delete recording?</button>
              <button data-keys="none" class="btn small" onclick={() => (confirm = '')}>Cancel</button>
            {:else}
              <button data-keys="none" class="btn ghost small" onclick={() => (confirm = item.id)}>Delete</button>
            {/if}
          </div>
        {/if}
      </li>
    {/each}
  </ul>
{/if}

<style>
  .recordings { list-style: none; padding: 0; margin: 0; display: grid; gap: 8px; }
  li { padding: 10px; display: grid; gap: 8px; border: 1px solid var(--line); border-radius: 8px; }
  .meta, .actions { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
  .meta { color: var(--muted); font-size: 12px; }
  .actions select { width: auto; max-width: 100%; font: inherit; padding: 4px; }
  audio { width: 100%; height: 36px; }
  .transcript { white-space: pre-wrap; overflow-wrap: anywhere; margin: 0; }
  .error { color: var(--blocked); margin: 0; overflow-wrap: anywhere; }
</style>
