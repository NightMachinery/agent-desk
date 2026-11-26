<script>
  import { tip } from '../lib/shortcuts.js';
  import CopyIds from './CopyIds.svelte';
  import { href, route } from '../lib/route.svelte.js';
  import { post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { acknowledgeMessages, pendingMessages, queueMessage } from '../lib/outbox.svelte.js';
  import { agentName, short, stateClass, viaText } from '../lib/format.js';
  import Composer from './Composer.svelte';
  import Markdown from './Markdown.svelte';
  import Thread from './Thread.svelte';
  import { untrack } from 'svelte';
  import Sheet from './Sheet.svelte';
  import Settings from './Settings.svelte';
  import AutoContinue from './AutoContinue.svelte';
  import CacheControl from './CacheControl.svelte';
  import { chooseSendCache } from '../lib/cache.svelte.js';
  import { desk } from '../lib/desk.svelte.js';
  import { archiveAgent } from '../lib/agents.js';

  // conversations: the node's threads, one per message sent to it about no
  // item, newest first (api/view). The panel does not list them: a message
  // is an item like any other, and its response arrives in Responses to you
  // and hides once read. Only your messages not delivered yet show here.
  let { node, target, conversations = [], stamp = '', onchange, compact = false } = $props();
  const allEntries = $derived(conversations.flatMap((c) => c.entries));
  const undelivered = $derived(allEntries.filter((x) => x.kind === 'message' && !x.delivery && x.source !== 'chat' && x.pending));
  let settingsOpen = $state(false);

  const order = { status: 0, next: 1, done: 2, velocity: 3, findings: 4, artifacts: 5 };
  const slots = $derived(
    [...(node.slots ?? [])].sort((a, b) => (order[a.name] ?? 9) - (order[b.name] ?? 9) || a.name.localeCompare(b.name)),
  );
  const kind = $derived(node.depth === 1 ? 'Project' : node.depth === 2 ? 'Run' : 'Agent');
  const label = $derived(node.state === 'active' || node.state === 'quiet' ? `${node.state} ${node.age}` : node.state);
  let writing = $state(false);

  // A message from the box starts a thread of its own. The send queue lets
  // go of a message once the desk lists it, wherever this panel shows it.
  const pending = $derived(pendingMessages([node.path], allEntries));
  $effect(() => {
    const entries = allEntries;
    untrack(() => acknowledgeMessages(entries));
  });

  async function queueReply(body) {
    const to = node.path;
    const page = route.path;
    const choice = await chooseSendCache('reply', to);
    if (choice === 'cancel' || route.path !== page) return false;
    return queueMessage('reply', { to, ...body, ...(choice === 'compact' && { after_compaction: true }) });
  }
  async function reply(text, opts = {}) {
    const queued = await queueReply({ text, ...opts });
    if (queued) writing = false;
    return queued;
  }
  // The prompt is rendered on the server, using the node's current status.
  function askVelocity() {
    queueReply({ prompt: 'velocity-report' });
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
      <button data-cmd="node-settings" class="chip chip-btn" title={tip(`Settings for ${node.path}: delivery and notifications`, 'node-settings')} onclick={() => (settingsOpen = true)}>
        <svg width="11" height="11" viewBox="0 0 16 16" aria-hidden="true"><path d="M8 2.5a3.5 3.5 0 0 0-3.5 3.5v2.5L3 11h10l-1.5-2.5V6A3.5 3.5 0 0 0 8 2.5zM6.5 13a1.5 1.5 0 0 0 3 0" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
        Notify
      </button>
    </div>
  </div>

  {#if target}
    <div class="coord faint">
      Coordinator: {agentName(target.agent)} <span class="mono">{short(target.paseo || target.session)}</span>
      {#if target.host}· {target.host}{/if}
      {#if target.for}· at <a class="mono" href={href({ path: target.node, item: '' })} title="No agent holds {target.for}, so messages and new tasks here go to the coordinator working under it">{target.node.slice(target.for.length + 1) || target.node}</a>{/if}
      <CopyIds paseo={target.paseo} resume={target.resume} keys={false} />
      {#if target.paseo && !compact}<AutoContinue paseo={target.paseo} />{/if}
      {#if target.paseo && target.host === desk.host}<CacheControl paseo={target.paseo} showUnavailable={!compact} />{/if}
      {#if target.paseo && target.host === desk.host && target.finished !== 'archived'}
        <button data-cmd="agent-archive" class="btn small archive" title={tip("Archive the coordinator's Paseo agent (stops it if running; messaging it revives it)", 'agent-archive')}
          onclick={async () => { if (await archiveAgent(target.paseo, agentName(target.agent))) onchange?.(); }}>Archive agent</button>
      {/if}
    </div>
  {/if}

  {#if slots.length}
    <div class="slots">
      {#each compact ? slots.filter((s) => s.name === 'status') : slots as s (s.event.id)}
        <a class="slot" class:unread={s.unread} href={href({ item: s.display_id })}>
          <div class="slot-name">{s.name}{#if s.unread}<span class="new"></span>{/if}</div>
          {#if s.file}<div class="plain-text slot-text">{s.file}</div>{:else}<Markdown class="slot-text" text={s.event.body} links={false} />{/if}
        </a>
        {#if s.name === 'velocity' && target}
          <div class="slot-act">
            <button data-cmd="velocity-report" class="btn small" title={tip('Ask the agent for an executive report on its velocity, and whether the work is progressing well', 'velocity-report')} onclick={askVelocity}>Ask for a velocity report</button>
          </div>
        {/if}
      {/each}
    </div>
  {/if}

  {#if !compact}
    {@const all = node.tasks ?? []}
    {@const done = all.filter((t) => t.state === 'done').length}
    <div class="tasks">
      <div class="slot-name">
        <a class="tasks-link" href={href({ path: node.path, item: '', tasks: true })} title="This node's tasks, bugs and ideas on the Tasks page">Tasks</a>
        {#if all.length}<span class="faint">{done} of {all.length} done</span>{/if}
        <a class="add-task faint" data-cmd="node-task-add" href={href({ path: node.path, item: '', tasks: true })} onclick={() => setTimeout(() => document.querySelector('[data-cmd="task-add"]')?.click(), 120)} title={tip('File a task, bug or idea here', 'task-add')}>+ New</a>
      </div>
      {#each all.filter((t) => t.state !== 'done' && t.state !== 'dropped') as t (t.event.id)}
        <a class="task" href={href({ item: t.display_id })}><span class="chip">{t.type && t.type !== 'task' ? t.type : t.state}</span> {t.event.title}</a>
      {/each}
    </div>
  {/if}

  {#if !compact && node.notes?.length}
    <details class="notes">
      <summary title="Run state the agents keep for themselves (agent-desk set --for agents). Never unread.">
        Agent notes ({node.notes.length})
      </summary>
      {#each node.notes as s (s.event.id)}
        <details class="note">
          <summary><span class="slot-name">{s.name}</span> <span class="faint">{s.lines} lines</span></summary>
          <Markdown class="slot-text" text={s.event.body} links={false} />
        </details>
      {/each}
    </details>
  {/if}

  {#if undelivered.length || pending.length}
    <div class="section-title" title="Your messages to this node that have not reached its agent yet. Each leaves this list once delivered; the agent's response comes in Responses to you.">Not delivered yet</div>
    <Thread entries={undelivered} {pending} remoteLabel={target?.remote_label} />
  {/if}

  {#if target}
    {#if compact && !writing}
      <div class="row-actions">
        <button data-cmd="message-node" class="btn small" title={tip('Message the coordinator', 'message-node')} onclick={() => (writing = true)}>Message coordinator</button>
        {#if node.unread}<button data-cmd="read-all" class="btn small" title={tip('Mark everything under it read', 'read-all')} onclick={readAll}>Mark all read</button>{/if}
      </div>
    {:else}
      <div class="act">
        <Composer
          id={compact ? undefined : 'reply-box'}
          draft={'node:' + node.path}
          voiceNode={node.path}
          label={`Message the ${kind.toLowerCase()}'s coordinator`}
          placeholder="A new conversation: tell it something, steer it, or ask what it is doing…"
          hint={viaText(target)}
          {target}
          send={reply}
        />
      </div>
    {/if}
  {/if}


  {#if !compact}
    {#if node.unread}
      <div class="row-actions"><button data-cmd="read-all" class="btn small" onclick={readAll}>Mark all read under {node.name}</button></div>
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

<Sheet bind:open={settingsOpen} title="Settings">
  <Settings path={node.path} {stamp} {onchange} />
</Sheet>

<style>
  .chip-btn {
    border: 0;
    cursor: pointer;
    font: inherit;
    font-size: 11.5px;
    font-weight: 600;
  }
  .chip-btn:hover:not(:disabled) {
    background: var(--line);
  }
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
  .tasks-link {
    color: inherit;
    text-decoration: none;
  }
  .tasks-link:hover:not(:disabled) {
    text-decoration: underline;
  }
  .add-task {
    margin-left: 8px;
    font-weight: 500;
    text-decoration: none;
    text-transform: none;
    letter-spacing: 0;
  }
  .tasks {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin: 10px 0 12px;
  }
  .tasks .task {
    color: inherit;
    font-size: 14px;
  }
  .notes {
    margin: 4px 0 12px;
    color: var(--muted);
  }
  .notes > summary {
    cursor: pointer;
    font-size: 13px;
  }
  .note {
    margin: 6px 0 0 14px;
    color: var(--text);
  }
  .note summary {
    cursor: pointer;
  }
  .note summary .slot-name {
    display: inline;
    margin-right: 6px;
  }
  .slots {
    margin-top: 16px;
    display: grid;
    gap: 8px;
  }
  .slot-act {
    margin: -2px 0 4px;
  }
  .slot {
    display: block;
    padding: 10px 12px;
    border-radius: 10px;
    background: var(--sunken);
    border: 1px solid transparent;
  }
  .slot:hover:not(:disabled) {
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
  .slot :global(.slot-text) {
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
  .kid:hover:not(:disabled) {
    border-color: var(--line-strong);
  }
  .kid .name {
    flex: 1;
    font-weight: 600;
  }
</style>
