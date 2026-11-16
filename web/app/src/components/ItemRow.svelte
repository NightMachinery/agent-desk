<script>
  import { route, href } from '../lib/route.svelte.js';
  import { kindOf, kindLabel, titleOf } from '../lib/format.js';
  import { desk } from '../lib/desk.svelte.js';

  let { item } = $props();
  const kind = $derived(kindOf(item));
  const remote = $derived(item.event.host && item.event.host !== desk.host);
  const preview = $derived((item.event.body || '').split('\n').find((l) => l.trim()) || '');
</script>

<a
  class="row {kind}"
  class:selected={route.item === item.display_id}
  class:unread={item.unread}
  data-item={item.display_id}
  href={href({ item: item.display_id })}
>
  <div class="line1">
    <span class="chip {kind}">{kind === 'slot' ? 'Slot' : kindLabel[kind]}</span>
    <span class="title">{titleOf(item)}</span>
    {#if item.unread}<span class="new" title="Unread"></span>{/if}
  </div>
  {#if preview && kind !== 'slot'}
    <div class="preview">{preview}</div>
  {:else if kind === 'slot'}
    <div class="preview">{preview || item.event.meta?.file || ''}</div>
  {/if}
  <div class="meta">
    <span>{(kind === 'slot' ? item.label.split('/').slice(0, -1).join('/') : item.label) || 'coordinator'}</span>
    <span class="faint">·</span>
    <span>{item.age} ago</span>
    {#if remote}<span class="faint">·</span><span class="host">@{item.event.host}</span>{/if}
    <span class="mono faint id">{item.display_id}</span>
  </div>
</a>

<style>
  .row {
    display: block;
    padding: 11px 13px 10px;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    transition: border-color 0.12s, box-shadow 0.12s, transform 0.12s;
    position: relative;
  }
  .row:hover {
    border-color: var(--line-strong);
    box-shadow: var(--shadow);
  }
  .row.selected {
    border-color: var(--default);
    box-shadow: 0 0 0 3px var(--focus);
  }
  .row.blocked::before {
    content: '';
    position: absolute;
    left: -1px;
    top: 10px;
    bottom: 10px;
    width: 3px;
    border-radius: 0 3px 3px 0;
    background: var(--blocked);
  }
  .line1 {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .title {
    flex: 1;
    min-width: 0;
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .new {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--default);
    flex: none;
  }
  .preview {
    margin-top: 4px;
    color: var(--muted);
    font-size: 13px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .meta {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 6px;
    font-size: 12px;
    color: var(--muted);
    min-width: 0;
  }
  .meta span:first-child {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .id {
    margin-left: auto;
    flex: none;
  }
  .host {
    color: var(--notice);
  }
</style>
