<script>
  import { route, href, go } from '../lib/route.svelte.js';
  import { post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { desk } from '../lib/desk.svelte.js';
  import { agentName, copy, kindLabel, kindOf, sessionOf, short, titleOf, viaText } from '../lib/format.js';
  import Composer from './Composer.svelte';

  let { item, error, onchange } = $props();

  const it = $derived(item?.item);
  const e = $derived(it?.event);
  const kind = $derived(it ? kindOf(it) : '');
  const isQuestion = $derived(e?.kind === 'ask');
  const crumbs = $derived(
    item ? item.node.split('/').map((name, i, all) => ({ name, path: all.slice(0, i + 1).join('/') })) : [],
  );
  const sess = $derived(e ? sessionOf(e) : '');
  const quick = $derived(
    !isQuestion
      ? []
      : item.default
        ? [{ label: 'Go with the default', text: `Go ahead with your default: ${item.default}` }]
        : [{ label: 'Take your recommendation', text: 'Take your recommendation.' }],
  );

  function report(res, what) {
    toast(res.note ? `${what}. ${res.note}` : `${what}.`, res.note && /not delivered|merge/.test(res.note) ? 'info' : 'ok', res.note ? 7000 : 3000);
  }

  async function answer(text) {
    try {
      report(await post('answer', { id: it.display_id, text }), 'Answer recorded');
      onchange();
      return true;
    } catch (err) {
      toast(err.message, 'error');
    }
  }
  async function reply(text) {
    try {
      report(await post('reply', { to: it.display_id, text }), 'Message sent');
      onchange();
      return true;
    } catch (err) {
      toast(err.message, 'error');
    }
  }
  async function mark(read) {
    try {
      await post(read ? 'read' : 'unread', read ? { target: it.display_id } : { id: it.display_id });
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
      <button class="btn ghost back" onclick={() => go({ item: '' })} aria-label="Close">
        <svg width="16" height="16" viewBox="0 0 16 16"><path d="M10 3 5 8l5 5" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" /></svg>
        <span class="back-label">Back</span>
      </button>
      <span class="chip {kind}">{kind === 'slot' ? `Slot · ${item.slot}` : kindLabel[kind]}</span>
      {#if isQuestion && item.answered}<span class="chip ok">Answered</span>{/if}
      {#if it.unread}<span class="chip default">Unread</span>{/if}
      <span class="spacer"></span>
      <button class="btn ghost small mono" title="Copy a link to this item" onclick={() => copyText(location.href, 'Link')}>{it.display_id}</button>
    </div>

    <h1>{titleOf(it)}</h1>

    <div class="meta">
      <nav class="crumbs">
        {#each crumbs as c, i (c.path)}
          {#if i}<span class="faint">/</span>{/if}
          <a href={href({ path: c.path, item: route.item })}>{c.name}</a>
        {/each}
      </nav>
      <span class="faint">·</span>
      <span>{it.age} ago</span>
      {#if e.host !== desk.host}<span class="faint">·</span><span class="remote">on {e.host}</span>{/if}
    </div>
    <div class="meta">
      {#if sess}
        <a class="who" href={href({ session: sess, item: route.item })} title="Show everything this session wrote">
          {agentName(item.from.agent)} · <span class="mono">{short(sess)}</span>
        </a>
      {/if}
      {#if item.resume}
        <button class="btn ghost small" onclick={() => copyText(item.resume, 'Resume command')}>Copy resume command</button>
      {/if}
    </div>

    {#if e.body}<div class="text body">{e.body}</div>{/if}

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
        <pre>{item.file_text}</pre>
      </details>
    {/if}
    {#if item.superseded}
      <div class="callout">
        This text has since been replaced. <a class="link" href={href({ item: item.superseded })}>See the current one</a>.
      </div>
    {/if}

    {#if item.thread.length}
      <div class="thread">
        {#each item.thread as t (t.event.id)}
          <div class="entry {t.event.kind}">
            <div class="entry-head">
              <strong>{t.event.kind === 'answer' ? 'Answer' : 'Your message'}</strong>
              <span class="faint">{t.age} ago</span>
              {#if t.source === 'chat'}<span class="chip">in chat</span>{/if}
              {#if t.delivered}<span class="chip ok">delivered · {t.delivered}</span>{:else if t.source !== 'chat'}<span class="chip waiting">not delivered yet</span>{/if}
              <a class="mono faint idlink" href={href({ item: t.display_id })}>{t.display_id}</a>
            </div>
            <div class="text">{t.event.body}</div>
          </div>
        {/each}
      </div>
    {/if}

    {#if isQuestion && !item.answered}
      <section class="act">
        <Composer
          id="answer-box"
          label="Answer"
          placeholder="Type your answer…"
          sendLabel="Answer"
          {quick}
          hint={item.target ? viaText(item.target) : ''}
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
          send={reply}
        />
      </section>
    {/if}

    <div class="foot">
      {#if !isQuestion}
        {#if it.unread}
          <button class="btn small" onclick={() => mark(true)}>Mark read</button>
        {:else}
          <button class="btn small" onclick={() => mark(false)}>Mark unread</button>
        {/if}
      {/if}
      <a class="btn small" href={href({ path: item.node, item: '' })}>Open {item.node.split('/').slice(-1)[0]}</a>
    </div>
  </article>
{/if}

<style>
  .pad {
    padding: max(12px, env(safe-area-inset-top)) 24px calc(40px + env(safe-area-inset-bottom));
    max-width: 820px;
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
  .body {
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
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .entry {
    padding: 10px 12px;
    border-radius: 10px;
    border: 1px solid var(--line);
  }
  .entry.answer {
    background: var(--ok-soft);
    border-color: transparent;
  }
  .entry.reply {
    background: var(--default-soft);
    border-color: transparent;
  }
  .entry-head {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 7px;
    font-size: 12.5px;
    margin-bottom: 4px;
  }
  .idlink {
    margin-left: auto;
  }
  .act {
    margin-top: 22px;
    padding-top: 18px;
    border-top: 1px solid var(--line);
  }
  .foot {
    display: flex;
    gap: 8px;
    margin-top: 26px;
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
