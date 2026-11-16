<script>
  import { href } from '../lib/route.svelte.js';
  import { post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { agentName, short, stateClass, viaText } from '../lib/format.js';
  import Composer from './Composer.svelte';

  let { node, target, onchange, compact = false } = $props();

  const order = { status: 0, next: 1 };
  const slots = $derived(
    [...(node.slots ?? [])].sort((a, b) => (order[a.name] ?? 9) - (order[b.name] ?? 9) || a.name.localeCompare(b.name)),
  );
  const kind = $derived(node.depth === 1 ? 'Project' : node.depth === 2 ? 'Run' : 'Agent');
  const label = $derived(node.state === 'active' || node.state === 'quiet' ? `${node.state} ${node.age}` : node.state);
  let writing = $state(false);

  async function reply(text) {
    try {
      const res = await post('reply', { to: node.path, text });
      toast(res.note ? `Message sent. ${res.note}` : 'Message sent.', 'ok', res.note ? 7000 : 3000);
      writing = false;
      onchange();
      return true;
    } catch (err) {
      toast(err.message, 'error');
    }
  }
  async function readAll() {
    try {
      const res = await post('read', { target: node.path });
      toast(res.note || 'Marked read.', 'ok');
      onchange();
    } catch (err) {
      toast(err.message, 'error');
    }
  }
</script>

<section class="panel" class:compact>
  <div class="head">
    <div>
      <div class="kind faint">{kind}</div>
      <h2>{node.name}</h2>
    </div>
    <div class="chips">
      <span class="chip {stateClass(node.state)}">{label}</span>
      {#if node.open_questions}<span class="chip waiting">{node.open_questions} open</span>{/if}
      {#if node.unread}<span class="chip default">{node.unread} unread</span>{/if}
    </div>
  </div>

  {#if target}
    <div class="coord faint">
      Coordinator: {agentName(target.agent)} <span class="mono">{short(target.paseo || target.session)}</span>
      {#if target.host}· {target.host}{/if}
    </div>
  {/if}

  {#if slots.length}
    <div class="slots">
      {#each compact ? slots.filter((s) => s.name === 'status') : slots as s (s.event.id)}
        <a class="slot" class:unread={s.unread} href={href({ item: s.display_id })}>
          <div class="slot-name">{s.name}{#if s.unread}<span class="new"></span>{/if}</div>
          <div class="text slot-text">{s.file ? s.file : s.event.body}</div>
        </a>
      {/each}
    </div>
  {/if}

  {#if target}
    {#if compact && !writing}
      <div class="row-actions">
        <button class="btn small" onclick={() => (writing = true)}>Message coordinator</button>
        {#if node.unread}<button class="btn small" onclick={readAll}>Mark all read</button>{/if}
      </div>
    {:else}
      <div class="act">
        <Composer
          id={compact ? undefined : 'reply-box'}
          label={`Message the ${kind.toLowerCase()}'s coordinator`}
          placeholder="Tell it something, steer it, or ask what it is doing…"
          hint={viaText(target)}
          send={reply}
        />
      </div>
    {/if}
  {/if}

  {#if !compact}
    {#if node.unread}
      <div class="row-actions"><button class="btn small" onclick={readAll}>Mark all read under {node.name}</button></div>
    {/if}
    {#if node.children?.length}
      <div class="section-title">Inside</div>
      <div class="kids">
        {#each node.children as k (k.path)}
          <a class="kid" href={href({ path: k.path, item: '' })}>
            <span class="name">{k.name}</span>
            <span class="chip {stateClass(k.state)}">{k.state}</span>
            {#if k.open_questions}<span class="count hot">{k.open_questions}</span>{/if}
          </a>
        {/each}
      </div>
    {/if}
  {/if}
</section>

<style>
  .panel {
    padding: max(16px, env(safe-area-inset-top)) 24px 40px;
    max-width: 820px;
  }
  .panel.compact {
    padding: 14px;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
  }
  .head {
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
  }
  .kind {
    font-size: 11.5px;
    font-weight: 700;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }
  h2 {
    margin: 2px 0 0;
    font-size: 21px;
    letter-spacing: -0.015em;
  }
  .compact h2 {
    font-size: 17px;
  }
  .chips {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  .coord {
    margin-top: 6px;
    font-size: 12.5px;
  }
  .slots {
    margin-top: 16px;
    display: grid;
    gap: 8px;
  }
  .slot {
    display: block;
    padding: 10px 12px;
    border-radius: 10px;
    background: var(--sunken);
    border: 1px solid transparent;
  }
  .slot:hover {
    border-color: var(--line-strong);
  }
  .slot-name {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 11.5px;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--muted);
    margin-bottom: 3px;
  }
  .slot-text {
    font-size: 13.5px;
    max-height: 9.5em;
    overflow: hidden;
  }
  .new {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--default);
  }
  .act {
    margin-top: 18px;
  }
  .row-actions {
    display: flex;
    gap: 8px;
    margin-top: 14px;
  }
  .kids {
    display: grid;
    gap: 6px;
  }
  .kid {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 9px 12px;
    border-radius: 10px;
    border: 1px solid var(--line);
  }
  .kid:hover {
    border-color: var(--line-strong);
  }
  .kid .name {
    flex: 1;
    font-weight: 600;
  }
</style>
