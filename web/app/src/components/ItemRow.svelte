<script>
  import { route, href } from '../lib/route.svelte.js';
  import { kindOf, kindLabel, titleOf } from '../lib/format.js';
  import { desk } from '../lib/desk.svelte.js';
  import { sel, toggle } from '../lib/select.svelte.js';

  let { item } = $props();
  const kind = $derived(kindOf(item));
  const remote = $derived(item.event.host && item.event.host !== desk.host);
  const preview = $derived((item.event.body || '').split('\n').find((l) => l.trim()) || '');
  const picked = $derived(sel.ids.has(item.display_id));

  // Shift- or cmd-click on a row selects instead of opening it.
  function onclick(e) {
    if (e.shiftKey || e.metaKey || e.ctrlKey || sel.ids.size) {
      e.preventDefault();
      toggle(item.display_id, e.shiftKey);
    }
  }
  function oncheck(e) {
    e.preventDefault();
    toggle(item.display_id, e.shiftKey);
  }
</script>

<div class="wrap" class:picking={sel.ids.size > 0}>
<button
  class="check"
  class:on={picked}
  role="checkbox"
  aria-checked={picked}
  aria-label={`Select ${item.display_id}`}
  onclick={oncheck}
>
  <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="m2.5 6.2 2.3 2.3 4.7-5" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" /></svg>
</button>
<a
  class="row {kind}"
  class:selected={route.item === item.display_id}
  class:picked
  class:unread={item.unread}
  data-item={item.display_id}
  href={href({ item: item.display_id })}
  {onclick}
>
  <div class="line1">
    <span class="chip {kind}">{kind === 'slot' ? 'Slot' : kindLabel[kind]}</span>
    <span class="title">{titleOf(item)}</span>
    {#if item.dismissed}<span class="chip">Dismissed</span>{/if}
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
</div>

<style>
  .wrap {
    position: relative;
  }
  .check {
    position: absolute;
    z-index: 1;
    left: 11px;
    top: 13px;
    width: 18px;
    height: 18px;
    padding: 0;
    display: grid;
    place-items: center;
    border-radius: 6px;
    border: 1.5px solid var(--line-strong);
    background: var(--panel);
    color: transparent;
    opacity: 0.55;
    transition: opacity 0.12s, background 0.12s, border-color 0.12s, color 0.12s;
  }
  .wrap:hover .check,
  .picking .check,
  .check:focus-visible {
    opacity: 1;
  }
  .check:hover {
    border-color: var(--default);
  }
  .check.on {
    opacity: 1;
    background: var(--default);
    border-color: var(--default);
    color: #fff;
  }
  .row {
    display: block;
    padding: 11px 13px 10px 39px;
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
  .row.picked {
    background: var(--default-soft);
    border-color: color-mix(in srgb, var(--default) 45%, var(--line));
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
