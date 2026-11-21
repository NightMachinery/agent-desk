<script>
  import { untrack } from 'svelte';
  import CopyIds from './CopyIds.svelte';
  import Choices from './Choices.svelte';
  import { tip } from '../lib/shortcuts.js';
  import { route, href, go } from '../lib/route.svelte.js';
  import { post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { chooseSendCache } from '../lib/cache.svelte.js';
  import { clearDraft, discardDrafts, hasDraft, itemDraftKeys, onDraftChange } from '../lib/drafts.js';
  import { pendingMessages, queueMessage } from '../lib/outbox.svelte.js';
  import { desk } from '../lib/desk.svelte.js';
  import { agentName, copy, deliverableURL, kindLabel, kindOf, sessionOf, short, slotName, titleOf, viaText } from '../lib/format.js';
  import Composer from './Composer.svelte';
  import Markdown from './Markdown.svelte';
  import Attachments from './Attachments.svelte';
  import { hold, release } from '../lib/linger.svelte.js';
  import Icon from './Icon.svelte';
  import Thread from './Thread.svelte';
  import { autoRead, nav, position, step } from '../lib/nav.svelte.js';
  import { moveOnTarget, ALL_CLEAR, ALL_CLEAR_KEY } from '../lib/moveon.js';
  import { arrival, arrivedAuto, cancelArrival, settleArrival } from '../lib/arrival.js';
  import { ui } from '../lib/ui.svelte.js';
  import { keys, syncMode } from '../lib/keys.svelte.js';
  import { askStatus, noteLabel, toldText } from '../lib/taskstatus.js';
  import ReopenForm from './ReopenForm.svelte';
  import ReplyingTo from './ReplyingTo.svelte';
  import { focusBoxIn, replyRef } from '../lib/replyto.js';
  import { off } from '../lib/why.js';

  let { item, error, onchange } = $props();

  const it = $derived(item?.item);
  const e = $derived(it?.event);
  const kind = $derived(it ? kindOf(it) : '');
  const isQuestion = $derived(e?.kind === 'ask');
  const pending = $derived(pendingMessages([e?.id, it?.display_id, item?.thread_of], item?.thread));
  const answerQueued = $derived(pending.some((m) => m.kind === 'answer' && !m.blocked));
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
    !isQuestion || e.meta?.screen
      ? []
      : item.default
        ? [{ label: 'Go with the default', text: `Go ahead with your default: ${item.default}` }]
        : [{ label: 'Take your recommendation', text: 'Take your recommendation.' }],
  );

  // Opening an item reads it. Not a question: reading one dismisses it.
  // Nor an item the page moved to by itself (arrival.js): that is read once
  // the user acts on it or moves on. Nor one with a draft (here or on the
  // desk): it stays unread until the draft is sent or discarded, and is
  // read then. Nor one the cursor reached in visual mode: that selects rows
  // to act on, so they stay as they were until the action (not tracked, so
  // leaving visual mode reads nothing either).
  let draftTick = $state(0);
  $effect(() => onDraftChange(() => draftTick++));
  $effect(() => {
    void draftTick;
    if (!it || isQuestion || !it.unread || autoRead.has(e.id)) return;
    if (untrack(() => keys.anchor)) return;
    if (hasDraft(it.display_id) || (it.draft && draftTick === 0)) return;
    if (arrival.id === it.display_id) {
      arrival.pending = true;
      arrival.event = e.id;
      return;
    }
    autoRead.add(e.id);
    const id = e.id, display = it.display_id;
    // Held first: the live stream can reload the list before this request
    // returns, and the row must not drop out meanwhile.
    hold(id, display);
    post('read', { target: display })
      .then(() => onchange())
      .catch(() => (autoRead.delete(id), release(id)));
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

  // Reopen asks first, in ReopenForm, what is still wrong.
  let reopening = $state('');
  // The thread entry the message box replies to, set by an entry's reply
  // button; none means the open item. It lasts until sent or cleared, and
  // never outlives the open item.
  let replyingTo = $state(/** @type {{ id: string, display: string, who: string, snippet: string } | null} */ (null));
  $effect(() => {
    void route.item;
    replyingTo = null;
  });
  function replyToEntry(t) {
    replyingTo = t.event.id === e.id ? null : replyRef(t);
    focusBoxIn(null, syncMode);
  }
  async function taskAct(action, note = '', id = it.display_id) {
    try {
      const res = await post('task', { action, id, ...(note && { text: note }) });
      const told = toldText(res);
      toast({ launch: 'Launched.', done: 'Marked done.', drop: 'Dropped.', defer: 'Deferred for quota.', reopen: 'Reopened.', start: 'Marked started.' }[action] + told, 'ok', told ? 7000 : undefined);
      reopening = '';
      onchange();
    } catch (err) {
      toast(err.message, 'error', 9000);
      onchange();
    }
  }

  async function screenAct(action) {
    try {
      await post('screen', { action, id: it.display_id });
      moveOn(action === 'grant' ? 'Approved: the agent has the screen.' : 'Denied.');
      onchange();
    } catch (err) {
      toast(err.message, 'error');
    }
  }

  // Once a question is dealt with (answered, approved, denied, dismissed) or
  // a message is sent in a conversation, move on: on a wide screen to the
  // next unpinned item (moveon.js), on a phone back to the list. The toast's
  // Back returns to it. Settings can keep either case on the item
  // (ui.nextAfterQuestion, ui.nextAfterMessage); then only the toast shows,
  // with Undo when there is one.
  const wide = () => typeof matchMedia === 'function' && matchMedia('(min-width: 901px)').matches;
  function moveOn(text, back = null, enabled = ui.nextAfterQuestion) {
    settleArrival(route.item); // acting on an arrival reads it
    // The last open item dealt with: say so beside the usual toast, with the
    // Tasks page one click away (n would open it).
    if (!moveOnTarget(nav.order, route.item, nav.pinned, nav.open)) toast(ALL_CLEAR, 'ok', 6000, { label: 'Tasks', run: () => go({ item: '', tasks: true }) }, ALL_CLEAR_KEY);
    if (!enabled) {
      toast(text, 'ok', 6000, back ? { label: 'Undo', run: back } : null);
      return;
    }
    const here = route.item;
    const next = wide() ? moveOnTarget(nav.order, here, nav.pinned, nav.open) : '';
    arrivedAuto(next);
    setTimeout(() => go({ item: next }), 0); // after the composer has finished with its draft
    toast(text, 'ok', 6000, {
      label: back ? 'Undo' : 'Back',
      run: () => {
        cancelArrival(next); // a glance, not a read
        if (back) back();
        go({ item: here });
      },
    });
  }

  // Discarding a draft is done with the item, like sending: move on the
  // same way, with Undo bringing the draft back here.
  function discarded(restore) {
    moveOn('Draft discarded.', restore, ui.nextAfterMessage);
  }
  async function answer(text, opts = {}) {
    const { id } = e;
    const page = route.item;
    const choice = await chooseSendCache('answer', id);
    if (choice === 'cancel' || route.item !== page) return false;
    const queued = queueMessage('answer', { id, text, ...opts, ...(choice === 'compact' && { after_compaction: true }) }, id, { quiet: true });
    if (queued) {
      clearDraft('choices:' + it.display_id);
      moveOn(`Answer to ${it.display_id} queued.`);
    }
    return queued;
  }
  async function reply(text, opts = {}) {
    const to = (!opts.prompt && replyingTo?.id) || e.id;
    const page = route.item;
    const choice = await chooseSendCache('reply', to);
    if (choice === 'cancel' || route.item !== page) return false;
    const queued = queueMessage('reply', { to, text, ...opts, ...(choice === 'compact' && { after_compaction: true }) }, to, { quiet: ui.nextAfterMessage });
    if (queued && to !== e.id) replyingTo = null;
    if (queued && ui.nextAfterMessage) moveOn(`Message on ${it.display_id} queued.`, null, true);
    return queued;
  }
  async function mark(read) {
    if (read && !isQuestion && hasDraft(it.display_id)) {
      const display = it.display_id;
      toast('It has a draft, so it stays unread until you send or discard it.', 'info', 8000, {
        label: 'Discard and mark read',
        run: () => {
          const restore = discardDrafts(itemDraftKeys(display));
          toast('Draft discarded.', 'ok', 8000, { label: 'Undo', run: restore });
        },
      });
      return;
    }
    cancelArrival(it.display_id); // the user said read or unread
    try {
      await post(read ? 'read' : 'unread', read ? { target: it.display_id } : { id: it.display_id });
      if (isQuestion && read) {
        const display = it.display_id;
        moveOn(
          `Dismissed: out of Needs you, still open. ${e.level === 'blocked' ? 'The agent stays blocked until you answer.' : 'The agent goes ahead with its default unless you answer.'}`,
          () => post('unread', { id: display }).then(onchange),
        );
      } else if (isQuestion) toast('Back in Needs you.', 'ok', 5000);
      else toast(read ? 'Marked read.' : 'Marked unread.', 'ok');
      onchange();
    } catch (err) {
      toast(err.message, 'error');
    }
  }
  // A deliverable's Works, beside Send: the message starts "This worked.",
  // with anything typed after it.
  const tried = [
    { label: 'Works', cmd: 'deliverable-works', title: 'Send with "This worked." in front (the box may be empty)', wrap: (t) => (t ? `This worked.\n\n${t}` : 'This worked.') },
  ];
  const files = $derived(kind === 'deliverable' ? (item?.media ?? []).filter((m) => m.kind === 'file') : []);
  const shownMedia = $derived(kind === 'deliverable' ? (item?.media ?? []).filter((m) => m.kind !== 'file') : (item?.media ?? []));
  const openURL = $derived(kind === 'deliverable' ? deliverableURL(e) : '');
  const openHost = $derived.by(() => {
    try {
      return openURL ? new URL(openURL).host : '';
    } catch {
      return '';
    }
  });
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
      {#if item.task}<span class="chip" class:ok={item.task.state === 'done'}>{item.task.state}</span>{/if}
      <span class="spacer"></span>
      <button data-cmd="pin" class="btn ghost small iconbtn" class:on={it.pinned} aria-pressed={it.pinned} title={tip(it.pinned ? 'Unpin' : 'Pin to the top', 'pin')} onclick={() => pin(!it.pinned)}
        ><Icon name="pin" /></button
      >
      {#if !isQuestion && !item.task}
        <button data-cmd="read-toggle" class="btn ghost small iconbtn" title={tip(it.unread ? 'Mark read' : 'Mark unread', it.unread ? 'dismiss' : 'undismiss')} aria-label={it.unread ? 'Mark read' : 'Mark unread'} onclick={() => mark(!!it.unread)}
          ><Icon name={it.unread ? 'read' : 'unread'} /></button
        >
      {/if}
      <button data-cmd="copy-link" class="btn ghost small mono" title={tip('Copy a link to this item', 'copy-link')} onclick={() => copyText(location.href, 'Link')}>{it.display_id}</button>
    </div>

    <h1 data-event={inThread ? undefined : it.display_id}>{title}</h1>

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

    {#if isQuestion && !item.answered && e.meta?.screen}
      <div class="task-bar">
        <span class="faint">Asks for the screen on {e.meta.screen.host} for {e.meta.screen.minutes ? `${e.meta.screen.minutes} min` : e.meta.screen.for}</span>
        <span class="spacer"></span>
        <button data-cmd="screen-grant" class="btn small primary" title="Hand it the screen now; a banner shows while it holds it" onclick={() => screenAct('grant')}>Approve</button>
        <button data-cmd="screen-deny" class="btn small" onclick={() => screenAct('deny')}>Deny</button>
      </div>
    {/if}
    {#if item.task}
      {@const t = item.task}
      <div class="task-bar">
        {#if t.type && t.type !== 'task'}<span class="chip {t.type === 'bug' ? 'blocked' : t.type === 'feature' ? 'default' : t.type === 'idea' ? 'notice' : ''}">{t.type}</span>{/if}
        {#if t.parent_display}<span class="faint">under <a class="mono" href={href({ item: t.parent_display })}>{t.parent_display}</a></span>{/if}
        {#each t.tags ?? [] as g (g)}<span class="faint mono">#{g}</span>{/each}
        <a class="faint" href={href({ path: e.path, item: '', tasks: true })}>All tasks here</a>
        {#if t.priority}<span class="chip">p{t.priority}</span>{/if}
        {#if t.size}<span class="chip">{t.size}</span>{/if}
        {#if t.profile}<span class="chip mono">{t.profile}</span>{/if}
        {#if t.agent}<span class="faint mono" title="What launch_command printed, or the agent that started it">{t.agent}</span>{/if}
        <span class="spacer"></span>
        <span class="task-acts">
        {#if t.state === 'deferred' || t.state === 'open'}
          <button class="btn small primary" {...off('Start an agent on this task', [!item.launch_ready, 'Set launch_command under [tasks] in config.toml first'])} onclick={() => taskAct('launch')}>Launch</button>
        {/if}
        {#if t.state === 'open'}<button class="btn small" onclick={() => taskAct('defer')}>Defer for quota</button>{/if}
        {#if t.state === 'done' || t.state === 'dropped'}<button data-cmd="task-reopen" class="btn small" onclick={() => (reopening = it.display_id)}>Reopen</button>
        {:else}
          <button data-cmd="task-status" class="btn small" title={tip('Ask whoever works on it for a short executive summary of where it stands', 'task-status')} onclick={() => askStatus(e.id, it.display_id)}>Status</button>
          <button class="btn small" onclick={() => taskAct('done')}>Done</button>
          <button class="btn small ghost" onclick={() => taskAct('drop')}>Drop</button>
        {/if}
        </span>
      </div>
      {#if reopening === it.display_id && (t.state === 'done' || t.state === 'dropped')}
        <div class="reopen-box"><ReopenForm id={it.display_id} onreopen={(note) => taskAct('reopen', note)} oncancel={() => (reopening = '')} /></div>
      {/if}
      {#if t.note}<div class="task-note"><span class="faint">{noteLabel(t.state)}:</span> {t.note}</div>{/if}
    {/if}

    {#if it.response && it.asked && !inThread}
      <div class="asked">
        <div class="asked-label">{it.asked_via === 'answer' ? 'Your answer' : 'You asked'}{it.asked_via === 'chat' ? ' in chat' : ''}</div>
        <Markdown text={it.asked} />
      </div>
    {/if}
    {#if kind === 'deliverable'}
      <div class="deliver">
        <div class="deliver-get">
          {#if openURL}
            <a data-cmd="deliverable-open" class="btn primary big" href={openURL} target="_blank" rel="noopener noreferrer" title={openURL}
              ><Icon name="link" /> Open {openHost}</a
            >
          {/if}
          {#each files as m (m.sha256)}
            {#if m.url}
              <a data-cmd="deliverable-download" class="btn big" class:primary={!openURL} href={'./' + m.url} download={m.name} title={m.caption}
                ><Icon name="download" /> {m.name} <span class="size">{m.size_text}</span></a
              >
            {:else}
              <span class="faint">{m.name}: {m.gone}</span>
            {/if}
          {/each}
        </div>
        {#each files.filter((m) => m.caption && m.caption !== m.name) as m (m.sha256)}
          <div class="faint">{m.name}: {m.caption}</div>
        {/each}
        {#if it.superseded_by}
          <div class="faint">A newer one has replaced this: <a class="link" href={href({ item: it.superseded_by })}>{it.superseded_by}</a>.</div>
        {/if}
      </div>
    {/if}
    {#if !inThread}
      {#if e.body}<Markdown class="body" text={e.body} />{/if}
      <Attachments media={shownMedia} />
    {/if}
    {#if item.subtasks?.length}
      {@const done = item.subtasks.filter((s) => s.state === 'done').length}
      <section class="subtasks">
        <h2>Subtasks <span class="faint">{done} of {item.subtasks.length} done</span></h2>
        <ul>
          {#each item.subtasks as s (s.display_id)}
            {@const over = s.state === 'done' || s.state === 'dropped'}
            <li class={s.state} data-event={s.display_id}>
              <button class="box" class:on={s.state === 'done'} aria-label={`Mark ${s.display_id} done`}
                {...off('Mark it done', [over, `${s.state === 'done' ? 'Done' : 'Dropped'}: open it to reopen`])}
                onclick={() => taskAct('done', '', s.display_id)}>
                <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="m2.5 6.2 2.3 2.3 4.7-5" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" /></svg>
              </button>
              <a class="sub-title" href={href({ item: s.display_id })}>{s.event.title}</a>
              {#if s.state !== 'open'}<span class="chip" class:ok={s.state === 'done'} class:active={s.state === 'started'}>{s.state}</span>{/if}
              <span class="mono faint">{s.display_id}</span>
            </li>
          {/each}
        </ul>
      </section>
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

    {#if item.thread.length || pending.length}
      <div class="thread">
        {#if item.thread_of}
          <div class="thread-of faint">
            In the conversation
            {#if item.thread_title}about <a data-cmd="thread-root" href={href({ item: item.thread_of })}>{item.thread_title}</a>{:else}with <a href={href({ path: item.thread_of, item: '', session: '', q: '' })}>{item.thread_of}</a>{/if}
          </div>
        {:else}
          <div class="section-title">Conversation</div>
        {/if}
        <Thread entries={item.thread} {pending} current={item.thread_of ? it.display_id : ''} remoteLabel={item.target?.remote_label} replyingTo={replyingTo?.display ?? ''} onreply={item.target ? replyToEntry : null} />
      </div>
    {/if}

    {#if isQuestion && !item.answered && !answerQueued}
      <section class="act">
        {#if e.meta?.choices}
          <Choices choices={e.meta.choices} draft={'choices:' + it.display_id} bind:lines={choiceLines} bind:count={choiceCount} />
        {/if}
        <Composer
          id="answer-box"
          draft={'answer:' + it.display_id}
          linked={e.meta?.choices ? ['choices:' + it.display_id] : []}
          voiceNode={['set', 'clear', 'link'].includes(e.kind) ? e.path.slice(0, e.path.lastIndexOf('/')) : e.path}
          label={e.meta?.choices ? 'A message with your answers (optional)' : 'Answer'}
          placeholder={e.meta?.choices ? 'Anything that is not about one question…' : 'Type your answer…'}
          compose={e.meta?.choices ? (t) => choiceLines + (t.trim() ? '\n\n' + t.trim() : '') : null}
          ready={!!e.meta?.choices && choiceCount > 0}
          sendLabel="Answer"
          quick={e.meta?.choices ? [] : quick}
          hint={item.target ? viaText(item.target) : ''}
          target={item.target}
          send={answer}
          ondiscard={discarded}
        />
      </section>
    {/if}

    {#if item.target}
      <section class="act">
        {#if replyingTo}
          <ReplyingTo to={replyingTo} back={it.display_id} oncancel={() => (replyingTo = null)} />
        {/if}
        <Composer
          id="reply-box"
          draft={'reply:' + it.display_id}
          voiceNode={['set', 'clear', 'link'].includes(e.kind) ? e.path.slice(0, e.path.lastIndexOf('/')) : e.path}
          label={replyingTo ? `Reply to ${replyingTo.display}` : `Message ${agentName(item.target.agent)}`}
          placeholder={replyingTo ? `Your reply about ${replyingTo.display}…` : isQuestion && !item.answered ? 'Or say something else to the agent…' : 'Say something to the agent that wrote this…'}
          hint={replyingTo || (isQuestion && !item.answered) ? '' : viaText(item.target)}
          target={item.target}
          send={reply}
          ondiscard={discarded}
          alongside={kind === 'deliverable' ? tried : []}
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
      {#if kindOf(it) === 'slot' && slotName(e) === 'velocity'}
        <button data-cmd="velocity-report" class="btn small" title={tip('Ask the agent for an executive report on its velocity, and whether the work is progressing well', 'velocity-report')} onclick={() => reply('', { prompt: 'velocity-report' })}>Ask for a velocity report</button>
      {/if}
      <a data-cmd="open-node" class="btn small" href={href({ path: item.node, item: '', session: '', q: '' })}>Open {item.node.split('/').slice(-1)[0]}</a>
    </div>

    <nav class="stepper" aria-label="Previous and next">
      <button class="btn small" {...off(tip('Previous item', 'prev'), [!prevId, 'This is the first item in the list'])} onclick={() => go({ item: prevId }, { replace: true })}><Icon name="prev" size={14} /> Previous</button>
      <span class="faint pos">{pos.at ? `${pos.at} of ${pos.of}` : ''}</span>
      <button class="btn small" {...off(tip('Next item', 'next'), [!nextId, 'This is the last item in the list'])} onclick={() => go({ item: nextId }, { replace: true })}>Next <Icon name="next" size={14} /></button>
    </nav>
  </article>
{/if}

<style>
  .deliver {
    display: flex;
    flex-direction: column;
    gap: 10px;
    margin: 14px 0;
    padding: 12px;
    border: 1px solid color-mix(in srgb, var(--deliverable) 40%, var(--line));
    border-radius: 10px;
    background: var(--deliverable-soft);
  }
  .deliver-get {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .deliver .big {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    height: 38px;
    padding: 0 14px;
    font-size: 14px;
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    text-decoration: none;
  }
  .deliver .size {
    opacity: 0.7;
    font-size: 12px;
  }
  .task-bar {
    display: flex;
    gap: 8px;
    align-items: center;
    flex-wrap: wrap;
    margin: 4px 0 12px;
  }
  .task-bar .spacer {
    flex: 1;
  }
  .reopen-box {
    margin: -4px 0 12px;
  }
  /* The task's buttons wrap as one group, not one by one. */
  .task-acts {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
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
  .crumbs a:hover:not(:disabled),
  .who:hover:not(:disabled) {
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
  .task-note {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .subtasks {
    margin-top: 14px;
  }
  .subtasks h2 {
    margin: 0 0 6px;
    font-size: 13px;
    font-weight: 600;
  }
  .subtasks h2 .faint {
    font-weight: 400;
  }
  .subtasks ul {
    display: flex;
    flex-direction: column;
    gap: 5px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .subtasks li {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    font-size: 13.5px;
  }
  .subtasks li > .chip,
  .subtasks li > .mono {
    flex: none;
    white-space: nowrap;
  }
  .sub-title {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: inherit;
  }
  .subtasks li.done .sub-title,
  .subtasks li.dropped .sub-title {
    color: var(--muted);
  }
  .subtasks li.dropped .sub-title {
    text-decoration: line-through;
  }
  .box {
    flex: none;
    display: grid;
    place-items: center;
    width: 18px;
    height: 18px;
    padding: 0;
    border-radius: 5px;
    border: 1.5px solid var(--line-strong);
    background: var(--panel);
    color: transparent;
    cursor: pointer;
  }
  .box.on {
    border-color: var(--ok);
    background: var(--ok);
    color: var(--panel);
  }
  .box:disabled {
    cursor: default;
  }
  .box:hover:not(:disabled),
  .box:focus-visible {
    border-color: var(--ok);
    color: var(--ok);
    outline: none;
  }
</style>
