<script>
  // A conversation: the user's messages and answers with their delivery and
  // response state, and the agents' responses, oldest first.
  import { href } from '../lib/route.svelte.js';
  import { dispositionText, desktopText, remoteText } from '../lib/format.js';
  import Markdown from './Markdown.svelte';

  let { entries = [], current = '', remoteLabel = '' } = $props();

  $effect(() => {
    if (!current) return;
    const el = document.getElementById('e-' + current);
    el?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
  });

  function label(t) {
    if (t.kind === 'response') return t.by || 'Agent';
    return t.kind === 'answer' ? 'Your answer' : 'Your message';
  }
  function notifyNote(n) {
    if (!n) return '';
    const parts = [];
    if (n.desktop) parts.push(desktopText(n.desktop));
    if (n.remote) parts.push(remoteText(n.remote, remoteLabel));
    return parts.length ? 'Its response notifies ' + parts.join(' and ') + '.' : '';
  }
</script>

<ol class="thread">
  {#each entries as t (t.event.id)}
    <li id={'e-' + t.display_id} class="entry {t.kind}" class:current={t.display_id === current}>
      <div class="head">
        <strong>{label(t)}</strong>
        {#if t.kind !== 'response' && t.by}<span class="faint">to {t.by}</span>{/if}
        {#if t.kind === 'response' && t.unread}<span class="dot" title="Unread"></span>{/if}
        <span class="faint">{t.age} ago</span>
        <a class="mono faint idlink" href={href({ item: t.display_id })}>{t.display_id}</a>
      </div>
      {#if t.kind !== 'response'}
        <div class="chips">
          {#if t.source === 'chat'}
            <span class="chip">given in chat</span>
          {:else}
            {#if t.behavior && t.behavior !== 'steer'}<span class="chip">{t.behavior === 'queue' ? 'after its turn' : 'stop and send'}</span>{/if}
            <span class="chip">sent</span>
            {#if t.delivery}
              <span class="chip ok" title={`Delivered ${t.delivery.age} ago via ${t.delivery.via}`}>{dispositionText(t.delivery)}</span>
            {:else if t.pending}
              <span class="chip waiting">{t.pending}</span>
            {/if}
          {/if}
          {#if t.status === 'responded'}
            <span class="chip response">responded</span>
          {:else if t.status === 'no_response'}
            <span class="chip blocked" title="Delivered over 15 minutes ago with no response. Marking it read clears this.">no response yet</span>
          {:else if t.status === 'awaiting'}
            <span class="chip faint-chip">waiting for a response</span>
          {/if}
        </div>
        {#if t.delivery?.fallback}<div class="note">Sent as {t.delivery.behavior}: {t.delivery.fallback}</div>{/if}
        {#if notifyNote(t.notify)}<div class="note">{notifyNote(t.notify)}</div>{/if}
      {:else if t.event.meta?.asked}
        <div class="note">Answers what you asked in chat: {t.event.meta.asked}</div>
      {:else if t.re}
        <div class="note">Response to <a class="mono" href={'#e-' + t.re} onclick={(e) => { e.preventDefault(); document.getElementById('e-' + t.re)?.scrollIntoView({ block: 'center', behavior: 'smooth' }); }}>{t.re}</a></div>
      {/if}
      {#if t.event.body}<Markdown text={t.event.body} />{/if}
    </li>
  {/each}
</ol>

<style>
  .thread {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .entry {
    padding: 10px 12px;
    border-radius: 10px;
    border: 1px solid transparent;
    scroll-margin: 80px;
    transition: box-shadow 0.3s;
  }
  .entry.answer {
    background: var(--ok-soft);
  }
  .entry.message {
    background: var(--default-soft);
  }
  .entry.response {
    background: var(--panel);
    border-color: var(--line);
    border-left: 3px solid var(--response);
  }
  .entry.current {
    box-shadow: 0 0 0 2px var(--focus);
  }
  .head {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 7px;
    font-size: 12.5px;
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: 5px;
    margin-top: 6px;
  }
  .faint-chip {
    color: var(--muted);
  }
  .note {
    margin-top: 5px;
    font-size: 12px;
    color: var(--muted);
  }
  .note a {
    text-decoration: underline;
  }
  .entry :global(.md) {
    margin-top: 6px;
  }
  .dot {
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--default);
  }
  .idlink {
    margin-left: auto;
  }
</style>
