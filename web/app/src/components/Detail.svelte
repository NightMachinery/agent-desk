<script>
  import CopyIds from './CopyIds.svelte';
  import Choices from './Choices.svelte';
  import { tip } from '../lib/shortcuts.js';
  import { route, href, go } from '../lib/route.svelte.js';
  import { post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { desk } from '../lib/desk.svelte.js';
  import { agentName, copy, kindLabel, kindOf, sessionOf, short, titleOf, viaText } from '../lib/format.js';
  import Composer from './Composer.svelte';
  import Markdown from './Markdown.svelte';
  import Attachments from './Attachments.svelte';
  import { hold } from '../lib/linger.svelte.js';
  import Icon from './Icon.svelte';
  import Thread from './Thread.svelte';
  import { autoRead, position, step } from '../lib/nav.svelte.js';

  let { item, error, onchange } = $props();

  const it = $derived(item?.item);
  const e = $derived(it?.event);
  const kind = $derived(it ? kindOf(it) : '');
  const isQuestion = $derived(e?.kind === 'ask');
  const crumbs = $derived(
    item ? item.node.split('/').map((name, i, all) => ({ name, path: all.slice(0, i + 1).join('/') })) : [],
  );
  const sess = $derived(e ? sessionOf(e) : '');
  let choiceLines = $state('');
  let choiceCount = $state(0);
  // An answer, message or response is shown inside its conversation, marked;
  // repeating its text above that would show it twice.
  const inThread = $derived(!!item?.thread_of && item.thread.some((t) => t.display_id === it?.display_id));
  const title = $derived.by(() => {
    if (!it) return '';
    const about = item.thread_title ? ` to “${item.thread_title}”` : '';
    if (e.kind === 'answer') return `Your answer${about}`;
    if (e.kind === 'reply' && !e.title) return item.thread_title ? `Your message about “${item.thread_title}”` : 'Your message';
    return titleOf(it);
  });
  const quick = $derived(
    !isQuestion
      ? []
      : item.default
        ? [{ label: 'Go with the default', text: `Go ahead with your default: ${item.default}` }]
        : [{ label: 'Take your recommendation', text: 'Take your recommendation.' }],
  );

  // Opening an item reads it. Not a question: reading one dismisses it.
  $effect(() => {
    if (!it || isQuestion || !it.unread || autoRead.has(e.id)) return;
    autoRead.add(e.id);
    const id = e.id, display = it.display_id;
    post('read', { target: display })
      .then(() => (hold(id, display), onchange()))
      .catch(() => autoRead.delete(e.id));
  });

  const prevId = $derived(step(-1, route.item));
  const nextId = $derived(step(1, route.item));
  const pos = $derived(position(route.item));

  async function pin(on) {
    try {
      await post(on ? 'pin' : 'unpin', { id: it.display_id });
      toast(on ? 'Pinned to the top.' : 'Unpinned.', 'ok');
      onchange();
    } catch (err) {
      toast(err.message, 'error');
    }
  }

  function report(res, what) {
    toast(res.note ? `${what}. ${res.note}` : `${what}.`, res.note && /not delivered|merge/.test(res.note) ? 'info' : 'ok', res.note ? 7000 : 3000);
  }

  async function answer(text, opts = {}) {
    try {
      report(await post('answer', { id: it.display_id, text, ...opts }), 'Answer recorded');
      onchange();
      return true;
    } catch (err) {
      toast(err.message, 'error');
    }
  }
  async function reply(text, opts = {}) {
    try {
      report(await post('reply', { to: it.display_id, text, ...opts }), 'Message sent');
      onchange();
      return true;
    } catch (err) {
      toast(err.message, 'error');
    }
  }
  async function mark(read) {
    try {
      await post(read ? 'read' : 'unread', read ? { target: it.display_id } : { id: it.display_id });
      if (isQuestion)
        toast(
          read
            ? `Dismissed: out of Needs you, still open. ${e.level === 'blocked' ? 'The agent stays blocked until you answer.' : 'The agent goes ahead with its default unless you answer.'}`
            : 'Back in Needs you.',
          'ok',
          5000,
        );
      else toast(read ? 'Marked read.' : 'Marked unread.', 'ok');
      onchange();
    } catch (err) {
      toast(err.message, 'error');
    }
  }
  async function copyText(text, what) {
    toast((await copy(text)) ? `${what} copied.` : text, 'ok');
  }
</script>

{#if error}
  <div class="pad">
    <button class="btn ghost back" onclick={() => go({ item: '' })}>← Back</button>
    <div class="empty">{error}</div>
  </div>
{:else if !item}
  <div class="pad"><div class="skeleton"></div></div>
{:else}
  <article class="pad">
    <div class="toolbar">
      <button class="btn ghost back" onclick={() => go({ item: '' })} aria-label="Close" title={tip('Close', 'close')}>
        <svg width="16" height="16" viewBox="0 0 16 16"><path d="M10 3 5 8l5 5" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" /></svg>
        <span class="back-label">Back</span>
      </button>
      <span class="chip {kind}">{kind === 'slot' ? `Slot · ${item.slot}` : kindLabel[kind]}</span>
      {#if isQuestion && item.answered}<span class="chip ok">Answered</span>{/if}
      {#if it.dismissed}<span class="chip">Dismissed</span>{/if}
      {#if it.unread}<span class="chip default">Unread</span>{/if}
      <span class="spacer"></span>
      <button data-cmd="pin" class="btn ghost small iconbtn" class:on={it.pinned} aria-pressed={it.pinned} title={tip(it.pinned ? 'Unpin' : 'Pin to the top', 'pin')} onclick={() => pin(!it.pinned)}
        ><Icon name="pin" /></button
      >
      {#if !isQuestion}
        <button data-cmd="read-toggle" class="btn ghost small iconbtn" title={tip(it.unread ? 'Mark read' : 'Mark unread', it.unread ? 'dismiss' : 'undismiss')} aria-label={it.unread ? 'Mark read' : 'Mark unread'} onclick={() => mark(!!it.unread)}
          ><Icon name={it.unread ? 'read' : 'unread'} /></button
        >
      {/if}
      <button data-cmd="copy-link" class="btn ghost small mono" title={tip('Copy a link to this item', 'copy-link')} onclick={() => copyText(location.href, 'Link')}>{it.display_id}</button>
    </div>

    <h1>{title}</h1>

    <div class="meta">
      <nav class="crumbs">
        {#each crumbs as c, i (c.path)}
          {#if i}<span class="faint">/</span>{/if}
          <a href={href({ path: c.path, item: '', session: '', q: '' })} title={`Show ${c.path}`}>{c.name}</a>
        {/each}
      </nav>
      <span class="faint">·</span>
      <span>{it.age} ago</span>
      {#if e.host !== desk.host}<span class="faint">·</span><span class="remote">on {e.host}</span>{/if}
    </div>
    <div class="meta">
      {#if sess}
        <a data-cmd="session" class="who" href={href({ session: sess, item: route.item })} title="Show everything this session wrote">
          {agentName(item.from.agent)} · <span class="mono">{short(sess)}</span>
        </a>
      {/if}
      <CopyIds paseo={item.from?.paseo} resume={item.resume} />
    </div>

    {#if it.response && it.asked && !inThread}
      <div class="asked">
        <div class="asked-label">{it.asked_via === 'answer' ? 'Your answer' : 'You asked'}{it.asked_via === 'chat' ? ' in chat' : ''}</div>
        <Markdown text={it.asked} />
      </div>
    {/if}
    {#if !inThread}
      {#if e.body}<Markdown class="body" text={e.body} />{/if}
      <Attachments media={item.media} />
    {/if}

    {#if item.default}
      <div class="callout default"><strong>Default:</strong> {item.default}</div>
    {/if}
    {#if item.report}
      <div class="callout">
        <strong>Report:</strong> <span class="mono">{item.report}</span>
        <button class="btn ghost small" onclick={() => copyText(item.report, 'Path')}>Copy</button>
      </div>
    {/if}
    {#if item.file}
      <details class="file" open>
        <summary><span class="mono">{item.file}</span></summary>
        {#if /\.(md|markdown)$/i.test(item.file)}<div class="file-md"><Markdown text={item.file_text} /></div>{:else}<pre>{item.file_text}</pre>{/if}
      </details>
    {/if}
    {#if item.superseded}
      <div class="callout">
        This text has since been replaced. <a class="link" href={href({ item: item.superseded })}>See the current one</a>.
      </div>
    {/if}

    {#if item.thread.length}
      <div class="thread">
        {#if item.thread_of}
          <div class="thread-of faint">
            In the conversation
            {#if item.thread_title}about <a data-cmd="thread-root" href={href({ item: item.thread_of })}>{item.thread_title}</a>{:else}with <a href={href({ path: item.thread_of, item: '', session: '', q: '' })}>{item.thread_of}</a>{/if}
          </div>
        {:else}
          <div class="section-title">Conversation</div>
        {/if}
        <Thread entries={item.thread} current={item.thread_of ? it.display_id : ''} remoteLabel={item.target?.remote_label} />
      </div>
    {/if}

    {#if isQuestion && !item.answered}
      <section class="act">
        {#if e.meta?.choices}
          <Choices choices={e.meta.choices} bind:lines={choiceLines} bind:count={choiceCount} />
        {/if}
        <Composer
          id="answer-box"
          label={e.meta?.choices ? 'A message with your answers (optional)' : 'Answer'}
          placeholder={e.meta?.choices ? 'Anything that is not about one question…' : 'Type your answer…'}
          compose={e.meta?.choices ? (t) => choiceLines + (t.trim() ? '\n\n' + t.trim() : '') : null}
          ready={!!e.meta?.choices && choiceCount > 0}
          sendLabel="Answer"
          quick={e.meta?.choices ? [] : quick}
          hint={item.target ? viaText(item.target) : ''}
          target={item.target}
          send={answer}
        />
      </section>
    {/if}

    {#if item.target}
      <section class="act">
        <Composer
          id="reply-box"
          label={`Message ${agentName(item.target.agent)}`}
          placeholder={isQuestion && !item.answered ? 'Or say something else to the agent…' : 'Say something to the agent that wrote this…'}
          hint={isQuestion && !item.answered ? '' : viaText(item.target)}
          target={item.target}
          send={reply}
        />
      </section>
    {/if}

    <div class="foot">
      {#if isQuestion && !item.answered}
        {#if it.dismissed}
          <button data-cmd="undismiss" class="btn small" onclick={() => mark(false)}>Back to Needs you</button>
        {:else}
          <button data-cmd="dismiss" class="btn small" title="Hide it from Needs you without answering. It stays open, and Show all still lists it." onclick={() => mark(true)}>Dismiss without answering</button>
        {/if}
      {/if}
      <a data-cmd="open-node" class="btn small" href={href({ path: item.node, item: '', session: '', q: '' })}>Open {item.node.split('/').slice(-1)[0]}</a>
    </div>

    <nav class="stepper" aria-label="Previous and next">
      <button class="btn small" title={tip('Previous item', 'prev')} disabled={!prevId} onclick={() => go({ item: prevId }, { replace: true })}><Icon name="prev" size={14} /> Previous</button>
      <span class="faint pos">{pos.at ? `${pos.at} of ${pos.of}` : ''}</span>
      <button class="btn small" title={tip('Next item', 'next')} disabled={!nextId} onclick={() => go({ item: nextId }, { replace: true })}>Next <Icon name="next" size={14} /></button>
    </nav>
  </article>
{/if}

<style>
  .pad {
    padding: max(12px, env(safe-area-inset-top)) 24px 0;
    max-width: 820px;
  }
  article.pad {
    display: flex;
    flex-direction: column;
    min-height: 100%;
    box-sizing: border-box;
  }
  .iconbtn {
    padding: 0 7px;
    color: var(--muted);
  }
  .iconbtn.on {
    color: var(--accent);
  }
  .asked {
    margin-top: 16px;
    padding: 8px 12px;
    border-left: 3px solid var(--response);
    background: var(--response-soft);
    border-radius: 0 8px 8px 0;
    font-size: 13.5px;
  }
  .asked-label {
    font-size: 11.5px;
    font-weight: 700;
    letter-spacing: 0.05em;
    text-transform: uppercase;
    color: var(--response);
    margin-bottom: 2px;
  }
  .stepper {
    position: sticky;
    bottom: 0;
    margin-top: auto;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 10px 0 max(10px, env(safe-area-inset-bottom));
    background: color-mix(in srgb, var(--panel) 92%, transparent);
    backdrop-filter: blur(10px);
    -webkit-backdrop-filter: blur(10px);
    border-top: 1px solid var(--line);
    z-index: 3;
  }
  .stepper .btn {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .pos {
    font-size: 12.5px;
  }
  .toolbar {
    display: flex;
    align-items: center;
    gap: 8px;
    position: sticky;
    top: 0;
    padding: 6px 0;
    background: var(--panel);
    z-index: 2;
  }
  .back {
    padding: 0 8px;
    margin-left: -8px;
  }
  .back-label {
    display: none;
  }
  .spacer {
    flex: 1;
  }
  h1 {
    margin: 10px 0 8px;
    font-size: 21px;
    line-height: 1.3;
    letter-spacing: -0.015em;
  }
  .meta {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
    color: var(--muted);
    font-size: 13px;
    margin-top: 4px;
  }
  .crumbs {
    display: flex;
    gap: 5px;
  }
  .crumbs a:hover,
  .who:hover {
    color: var(--text);
    text-decoration: underline;
  }
  .remote {
    color: var(--notice);
  }
  article :global(.md.body) {
    margin-top: 18px;
    font-size: 14.5px;
  }
  .callout {
    margin-top: 14px;
    padding: 10px 12px;
    border-radius: 10px;
    background: var(--sunken);
    font-size: 13.5px;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
  }
  .callout.default {
    background: var(--default-soft);
  }
  .link {
    text-decoration: underline;
  }
  .file {
    margin-top: 14px;
    border: 1px solid var(--line);
    border-radius: 10px;
    overflow: hidden;
  }
  .file summary {
    padding: 8px 12px;
    background: var(--sunken);
    cursor: pointer;
  }
  .file-md {
    padding: 4px 14px;
    max-height: 520px;
    overflow: auto;
  }
  .file pre {
    margin: 0;
    padding: 12px;
    max-height: 420px;
    overflow: auto;
    font: 12.5px/1.5 var(--mono);
    white-space: pre-wrap;
  }
  .thread {
    margin-top: 22px;
  }
  .thread-of {
    font-size: 12.5px;
    margin-bottom: 8px;
  }
  .thread-of a {
    color: var(--text);
    text-decoration: underline;
  }
  .act {
    margin-top: 22px;
    padding-top: 18px;
    border-top: 1px solid var(--line);
  }
  .foot {
    display: flex;
    gap: 8px;
    margin: 26px 0 28px;
  }
  .skeleton {
    height: 160px;
    border-radius: var(--radius);
    background: var(--sunken);
  }
  @media (max-width: 900px) {
    .pad {
      padding-left: 16px;
      padding-right: 16px;
    }
    .back-label {
      display: inline;
    }
  }
</style>
