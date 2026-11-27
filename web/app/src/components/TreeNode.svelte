<script>
  import TreeNode from './TreeNode.svelte';
  import CopyIds from './CopyIds.svelte';
  import CacheControl from './CacheControl.svelte';
  import Icon from './Icon.svelte';
  import { route, href, go } from '../lib/route.svelte.js';
  import { stateClass } from '../lib/format.js';
  import { post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { failed } from '../lib/errors.svelte.js';

  let { node } = $props();
  const status = $derived(node.slots?.find((s) => s.name === 'status')?.event?.body || '');
  const kids = $derived(node.children ?? []);
  // Unfold only the way to something that needs you, or, in a search, to
  // every match (the server keeps only matches and their ancestors).
  let open = $state(null);
  const shown = $derived(open ?? (!!route.q || node.state === 'waiting on you' || node.depth === 1));
  // Row tools (shown on hover, always on touch screens): copy its Paseo id,
  // message its agent (for a project or a run, the coordinator working
  // under it), mark everything under it read.
  function message() {
    go({ path: node.path, item: '', session: '', q: '' });
    setTimeout(() => {
      const box = document.getElementById('reply-box');
      if (box) box.focus();
      else toast(`No agent works under ${node.name} to message.`, 'info');
    }, 400);
  }
  const whom = $derived(node.depth >= 3 ? node.name : `the coordinator under ${node.name}`);
  async function readAll() {
    try {
      const res = await post('read', { target: node.path });
      toast(res.note || `Marked ${node.name} read.`, 'ok');
    } catch (err) {
      failed(err);
    }
  }
  const label = $derived(
    node.state === 'active' || node.state === 'quiet' ? `${node.state} ${node.age}` : node.state,
  );
</script>

<div class="node" style="--depth: {node.depth}">
  <div class="line" class:here={route.path === node.path}>
    {#if kids.length}
      <button data-keys="none" class="caret" class:open={shown} aria-label={shown ? 'Fold' : 'Unfold'} onclick={() => (open = !shown)}>
        <svg width="10" height="10" viewBox="0 0 10 10"><path d="M3 1.5 7 5l-4 3.5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
      </button>
    {:else}
      <span class="caret"></span>
    {/if}
    <a class="main" href={href({ path: node.path, item: '' })}>
      <span class="name">{node.name}</span>
      <span class="chip {stateClass(node.state)}">{label}</span>
      {#if node.open_questions}<span class="count hot" title="Open questions">{node.open_questions}</span>{/if}
      {#if node.unread}<span class="count" title="Unread">{node.unread}</span>{/if}
      {#if node.host}<span class="host">@{node.host}</span>{/if}
    </a>
    {#if node.paseo}<CacheControl paseo={node.paseo} />{/if}
    <div class="tools">
      {#if node.paseo}<CopyIds paseo={node.paseo} keys={false} />{/if}
      <button data-keys="none" class="btn ghost small iconbtn" title={`Message ${whom}`} aria-label={`Message ${whom}`} onclick={message}><Icon name="message" /></button>
      <!-- Mark read keeps the last slot, read or not, so the icons line up. -->
      {#if node.unread}
        <button data-keys="none" class="btn ghost small iconbtn" title={`Mark everything under ${node.name} read`} aria-label="Mark everything under it read" onclick={readAll}><Icon name="readall" /></button>
      {:else}
        <span class="spacer"></span>
      {/if}
    </div>
  </div>
  {#if status && node.depth >= 3}
    <div class="status">{status}</div>
  {/if}
  {#if shown}
    {#each kids as k (k.path)}<TreeNode node={k} />{/each}
  {/if}
</div>

<style>
  .node {
    padding-left: calc((var(--depth) - 1) * 16px);
  }
  .line {
    display: flex;
    align-items: center;
    gap: 2px;
    border-radius: 8px;
  }
  .line:hover:not(:disabled),
  .line.here {
    background: var(--sunken);
  }
  .tools {
    display: flex;
    align-items: center;
    gap: 2px;
    flex: none;
    padding-right: 4px;
  }
  .tools :global(.iconbtn),
  .spacer {
    width: 30px;
    height: 28px;
    padding: 0;
    display: inline-grid;
    place-items: center;
  }
  /* With a mouse, the tools show on the row under it (or keyboard focus). */
  @media (hover: hover) and (pointer: fine) {
    .tools {
      visibility: hidden;
    }
    .line:hover:not(:disabled) .tools,
    .line:focus-within .tools {
      visibility: visible;
    }
  }
  .caret {
    width: 22px;
    height: 30px;
    flex: none;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: 0;
    background: none;
    color: var(--faint);
    padding: 0;
  }
  .caret svg {
    transition: transform 0.12s;
  }
  .caret.open svg {
    transform: rotate(90deg);
  }
  .main {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 8px;
    height: 30px;
    padding-right: 8px;
  }
  .name {
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .host {
    font-size: 12px;
    color: var(--notice);
  }
  .status {
    margin: -2px 0 4px 24px;
    padding-left: 0;
    font-size: 12.5px;
    color: var(--muted);
    white-space: pre-wrap;
  }
</style>
