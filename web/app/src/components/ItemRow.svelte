<script>
  import { route, href } from '../lib/route.svelte.js';
  import { kindOf, kindLabel, titleOf, previewLine } from '../lib/format.js';
  import Icon from './Icon.svelte';
  import { desk } from '../lib/desk.svelte.js';
  import { sel, toggle, selecting } from '../lib/select.svelte.js';
  import { dimmed } from '../lib/linger.svelte.js';

  // base: the path the surroundings already name (the view's scope, or the
  // project and run of the group heading); the row shows its lineage below it.
  let { item, base = '' } = $props();
  const kind = $derived(kindOf(item));
  const lineage = $derived.by(() => {
    const p = item.event.path.split('/');
    if (kind === 'slot') p.pop();
    const node = p.join('/');
    let rel = node;
    if (base && (node === base || node.startsWith(base + '/'))) rel = node.slice(base.length + 1);
    return (rel || p[p.length - 1] || 'coordinator').split('/').join(' / ');
  });
  const remote = $derived(item.event.host && item.event.host !== desk.host);
  const preview = $derived(previewLine(item.event.body));
  const picked = $derived(sel.ids.has(item.display_id));

  // Cmd-, ctrl- or shift-click selects instead of opening; so does any tap
  // while selecting.
  let pressed = false;
  function onclick(e) {
    if (pressed) {
      pressed = false;
      e.preventDefault();
      return;
    }
    if (e.shiftKey || e.metaKey || e.ctrlKey || selecting()) {
      e.preventDefault();
      toggle(item.display_id, e.shiftKey);
    }
  }
  function oncheck(e) {
    e.preventDefault();
    toggle(item.display_id, e.shiftKey);
  }

  // A long press on a touch screen selects the row and starts selecting.
  let timer = 0;
  let start = { x: 0, y: 0 };
  function onpointerdown(e) {
    if (e.pointerType === 'mouse') return;
    start = { x: e.clientX, y: e.clientY };
    timer = window.setTimeout(() => {
      timer = 0;
      pressed = true;
      sel.mode = true;
      if (!sel.ids.has(item.display_id)) toggle(item.display_id);
      navigator.vibrate?.(12);
    }, 450);
  }
  function cancelPress(e) {
    if (!timer) return;
    if (e.type === 'pointermove' && Math.hypot(e.clientX - start.x, e.clientY - start.y) < 8) return;
    clearTimeout(timer);
    timer = 0;
  }
</script>

<div class="wrap" class:picking={selecting()}>
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
  class:lingering={dimmed(item.event.id) && route.item !== item.display_id}
  data-item={item.display_id}
  href={href({ item: item.display_id })}
  {onclick}
  {onpointerdown}
  onpointermove={cancelPress}
  onpointerup={cancelPress}
  onpointercancel={cancelPress}
  oncontextmenu={(e) => pressed && e.preventDefault()}
>
  <div class="line1">
    <span class="chip {kind}">{kind === 'slot' ? 'Slot' : kindLabel[kind]}</span>
    <span class="title">{titleOf(item)}</span>
    {#if item.pinned}<span class="pin" title="Pinned"><Icon name="pin" size={13} /></span>{/if}
    {#if item.attachments}<span class="pin" title={item.attachments + (item.attachments === 1 ? ' attachment' : ' attachments')}><Icon name="clip" size={13} />{#if item.attachments > 1}<span class="n">{item.attachments}</span>{/if}</span>{/if}
    {#if item.dismissed}<span class="chip">Dismissed</span>{/if}
    {#if item.unread}<span class="new" title="Unread"></span>{/if}
  </div>
  {#if item.response && item.asked}
    <div class="asked">
      {#if item.re}<span>Response to <span class="mono">{item.re}</span> ·</span>{/if}
      <span>{{ answer: 'Your answer', message: 'Your message', chat: 'You asked in chat' }[item.asked_via] || 'You asked'}: {previewLine(item.asked)}</span>
    </div>
  {/if}
  {#if preview && kind !== 'slot'}
    <div class="preview">{preview}</div>
  {:else if kind === 'slot'}
    <div class="preview">{preview || item.event.meta?.file || ''}</div>
  {/if}
  <div class="meta">
    <span class="lineage" title={item.event.path}>{lineage}</span>
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
    opacity: 0;
    transform: scale(0.6);
    pointer-events: none;
    transition: opacity 0.14s, transform 0.14s, background 0.12s, border-color 0.12s, color 0.12s;
  }
  .picking .check {
    opacity: 1;
    transform: none;
    pointer-events: auto;
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
    padding: 11px 13px 10px;
    -webkit-touch-callout: none;
    -webkit-user-select: none;
    user-select: none;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    transition: border-color 0.12s, box-shadow 0.12s, transform 0.12s, padding 0.14s;
    position: relative;
  }
  .picking .row {
    padding-left: 39px;
  }
  .row.lingering {
    opacity: 0.5;
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
  .row.blocked::before,
  .row.response::before {
    content: '';
    position: absolute;
    left: -1px;
    top: 10px;
    bottom: 10px;
    width: 3px;
    border-radius: 0 3px 3px 0;
    background: var(--blocked);
  }
  .row.response::before {
    background: var(--response);
  }
  .pin .n {
    font-size: 11px;
    margin-left: 1px;
  }
  .pin {
    color: var(--muted);
    display: inline-flex;
    flex: none;
  }
  .asked {
    margin-top: 4px;
    font-size: 12.5px;
    color: var(--response);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
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
