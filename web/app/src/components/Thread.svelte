<script>
  // A conversation: the user's messages and answers with their delivery and
  // response state, and the agents' responses, oldest first.
  import { untrack } from 'svelte';
  import { href } from '../lib/route.svelte.js';
  import { dispositionText, deliveryTitle, desktopText, remoteText } from '../lib/format.js';
  import Markdown from './Markdown.svelte';
  import Attachments from './Attachments.svelte';
  import { post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { acknowledgeMessages, retryPost } from '../lib/outbox.svelte.js';

  let { entries = [], pending = [], current = '', remoteLabel = '' } = $props();

  $effect(() => {
    const currentEntries = entries;
    untrack(() => acknowledgeMessages(currentEntries));
  });

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
  let retrying = $state('');
  async function retry(id, force = false, failureID = '') {
    if (force && !window.confirm('The agent may already have this message and could get it twice. Send it again anyway?')) return;
    retrying = id;
    try {
      const res = await post('deliver', { id, force, failure_id: failureID });
      toast(res.status === 'delivered' ? 'Already delivered.' : 'Retry queued.', 'info');
    } catch (e) {
      toast(e.message, 'error');
    } finally {
      retrying = '';
    }
  }
  async function voiceAction(entry, action) {
    retrying = entry.event.id;
    try {
      if (action === 'retry') {
        await post(`recordings/${entry.voice_failure.recording}`, { action });
        toast('Transcribing the recording again.', 'info');
      } else {
        const res = await post('deliver', {
          id: entry.event.id,
          action: 'omit-recording',
          failure_id: entry.voice_failure.id,
        });
        toast(res.status === 'delivered' ? 'Already delivered.' : 'Queued without the recording.', 'info');
      }
    } catch (err) {
      toast(err.message, 'error');
    } finally {
      retrying = '';
    }
  }
  function copy(text) {
    navigator.clipboard?.writeText(text).then(() => toast('Copied.', 'ok'), () => toast('Could not copy.', 'error'));
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
            {#if t.delivery}
              {@const disposition = dispositionText(t.delivery)}
              <span class="chip ok" title={deliveryTitle(t.delivery)}>{disposition.startsWith('delivered') ? disposition : 'delivered · ' + disposition}</span>
            {:else if t.pending && !t.failure && !t.voice_failure}
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
        {#if t.compaction_note}<div class="note">{t.compaction_note}</div>{/if}
        {#if t.voice_failure && !t.delivery}
          <div class="failure" data-failure>
            <div><strong>Transcription failed</strong> · {t.voice_failure.error}</div>
            <div class="voice-actions">
              {#if t.voice_failure.retryable}
                <button class="btn small" disabled={retrying === t.event.id} onclick={() => voiceAction(t, 'retry')}>Retry transcription</button>
              {/if}
              <button class="btn small" disabled={retrying === t.event.id} onclick={() => voiceAction(t, 'omit')}>Send without recording</button>
            </div>
          </div>
        {/if}
        {#if t.failure && !t.delivery}
          <div class="failure" data-failure>
            <div><strong>{t.failure.retrying ? 'Retrying' : t.failure.uncertain ? 'Delivery unconfirmed' : 'Not delivered'}</strong> <span class="faint">({t.failure.age} ago)</span>: {t.failure.error}</div>
            {#if t.failure.resume}
              <div class="resume">
                <span class="faint">Resume its session by hand:</span>
                <code class="mono">{t.failure.resume}</code>
                <button class="btn ghost small" onclick={() => copy(t.failure.resume)}>Copy</button>
              </div>
            {/if}
            {#if t.failure.uncertain}
              <div>Look for this message in the agent’s conversation before sending it again.</div>
              <div><button class="btn small" disabled={retrying === t.event.id} onclick={() => retry(t.event.id, true, t.failure.id)}>
                {retrying === t.event.id ? 'Queuing…' : 'Send again anyway'}
              </button></div>
            {:else if !t.failure.retrying}
              <div><button class="btn small" disabled={retrying === t.event.id} onclick={() => retry(t.event.id)} data-cmd="deliver-retry">{retrying === t.event.id ? 'Queuing…' : 'Retry'}</button></div>
            {/if}
          </div>
        {/if}
        {#if notifyNote(t.notify)}<div class="note">{notifyNote(t.notify)}</div>{/if}
      {:else if t.event.meta?.asked}
        <div class="note">Answers what you asked in chat: {t.event.meta.asked}</div>
      {:else if t.re}
        <div class="note">Response to <a class="mono" href={'#e-' + t.re} onclick={(e) => { e.preventDefault(); document.getElementById('e-' + t.re)?.scrollIntoView({ block: 'center', behavior: 'smooth' }); }}>{t.re}</a></div>
      {/if}
      {#if t.event.body}<Markdown text={t.event.body} />{/if}
      <Attachments media={t.media} />
    </li>
  {/each}
  {#each pending as message (message.key)}
    <li class="entry" class:answer={message.kind === 'answer'} class:message={message.kind === 'reply'}>
      <div class="head"><strong>{message.kind === 'answer' ? 'Your answer' : 'Your message'}</strong></div>
      {#if message.blocked}
        <div class="failure">
          <div><strong>Could not queue</strong>: {message.error}</div>
          <div>
            <button class="btn small" onclick={() => retryPost(message.key)}>Retry</button>
            {#if message.body.text}<button class="btn ghost small" onclick={() => copy(message.body.text)}>Copy text</button>{/if}
          </div>
        </div>
      {:else}
        <div class="chips"><span class="chip waiting" role="status">{message.error ? 'queued · reconnecting' : 'queued'}</span></div>
      {/if}
      {#if message.body.text}
        <Markdown text={message.body.text} />
      {:else if message.body.prompt === 'velocity-report'}
        <div class="note">Request for a velocity report</div>
      {/if}
      {#if message.body.media?.length}
        <div class="note">{message.body.media.map((ref) => ref.name).join(', ')}</div>
      {/if}
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
  .failure {
    margin-top: 6px;
    padding: 7px 9px;
    border-radius: 8px;
    border-left: 3px solid var(--blocked);
    background: var(--panel);
    font-size: 12.5px;
    display: flex;
    flex-direction: column;
    gap: 5px;
    overflow-wrap: anywhere;
  }
  .resume, .voice-actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
  }
  .resume code {
    font-size: 11.5px;
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
