<script>
  import TreeNode from './TreeNode.svelte';
  import { route, href } from '../lib/route.svelte.js';
  import { stateClass } from '../lib/format.js';

  let { node } = $props();
  const status = $derived(node.slots?.find((s) => s.name === 'status')?.event?.body || '');
  const kids = $derived(node.children ?? []);
  // Unfold only the way to something that needs you.
  let open = $state(null);
  const shown = $derived(open ?? (node.state === 'waiting on you' || node.depth === 1));
  const label = $derived(
    node.state === 'active' || node.state === 'quiet' ? `${node.state} ${node.age}` : node.state,
  );
</script>

<div class="node" style="--depth: {node.depth}">
  <div class="line" class:here={route.path === node.path}>
    {#if kids.length}
      <button class="caret" class:open={shown} aria-label={shown ? 'Fold' : 'Unfold'} onclick={() => (open = !shown)}>
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
  .line:hover,
  .line.here {
    background: var(--sunken);
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
