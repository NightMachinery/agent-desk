<script>
  import { tip } from '../lib/shortcuts.js';
  // A text box that sends on ⌘/Ctrl+Enter, with optional quick replies.
  // Given a target, the Send button uses its resolved behaviour, a menu
  // beside it offers the others, and "Notify me" routes the response.
  // Files can be attached (the clip button, paste or drop): each is uploaded
  // at once and sent with the message; the session gets their paths.
  import { behaviorLabel, behaviorHelp, desktopText, remoteText } from '../lib/format.js';
  import { upload } from '../lib/api.js';
  import { clearDraft, loadDraft, saveDraft } from '../lib/drafts.js';
  import Sheet from './Sheet.svelte';

  // compose turns the typed text into what is sent (a question's picks plus
  // the text); ready allows sending with the box empty. draft names the box
  // (e.g. "reply:t-k2m9"): its text is saved until sent, and a new name
  // starts from that box's own draft, with no files or notify overrides
  // carried over from the last one.
  let { draft = '', id = undefined, label, placeholder, send, hint = '', quick = [], sendLabel = 'Send', target = null, compose = null, ready = false } = $props();
  let text = $state('');
  let busy = $state(false);
  let menu = $state(false);
  let notifySheet = $state(false);
  let confirming = $state(false);
  let notify = $state({ desktop: '', remote: '' }); // overrides for this message
  const MAX_FILES = 8;
  let files = $state([]); // {key, name, size, status: uploading|ready|error, ref, error}
  let picker = $state();
  let dragging = $state(false);
  let seq = 0;
  let draftOf = null; // the box whose text is in the field
  $effect(() => {
    const k = draft;
    const t = text;
    if (k !== draftOf) {
      draftOf = k;
      text = loadDraft(k) ?? '';
      files = [];
      notify = { desktop: '', remote: '' };
      return;
    }
    saveDraft(k, t, !t.trim());
  });
  const uploading = $derived(files.some((f) => f.status === 'uploading'));
  const readyFiles = $derived(files.filter((f) => f.status === 'ready'));

  function sizeText(n) {
    return n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : n >= 1024 ? `${Math.round(n / 1024)} KB` : `${n} B`;
  }
  async function addFiles(list) {
    for (const file of list) {
      if (files.length >= MAX_FILES) {
        files.push({ key: ++seq, name: file.name, size: file.size, status: 'error', error: `at most ${MAX_FILES} files` });
        break;
      }
      const key = ++seq;
      files.push({ key, name: file.name || 'pasted-file', size: file.size, status: 'uploading' });
      try {
        const res = await upload(file);
        const f = files.find((x) => x.key === key);
        if (f) Object.assign(f, { status: 'ready', ref: res.ref, name: res.ref.name });
      } catch (err) {
        const f = files.find((x) => x.key === key);
        if (f) Object.assign(f, { status: 'error', error: err.message });
      }
    }
  }
  function removeFile(key) {
    files = files.filter((f) => f.key !== key);
  }
  function onpaste(e) {
    const list = [...(e.clipboardData?.files ?? [])];
    if (list.length) {
      e.preventDefault();
      addFiles(list);
    }
  }
  function ondrop(e) {
    dragging = false;
    const list = [...(e.dataTransfer?.files ?? [])];
    if (list.length) {
      e.preventDefault();
      addFiles(list);
    }
  }

  const def = $derived(target?.default_behavior || 'steer');
  const defSupport = $derived((target?.behaviors ?? []).find((b) => b.name === def));
  const others = $derived((target?.behaviors ?? []).filter((b) => b.name !== def));
  const mainLabel = $derived(def === 'steer' ? sendLabel : `${sendLabel === 'Send' ? '' : sendLabel + ': '}${behaviorLabel[def]}`);
  const resolved = $derived(target?.notify ?? null);
  const desktopNow = $derived(notify.desktop || resolved?.desktop || '');
  const remoteNow = $derived(notify.remote || resolved?.remote || '');
  const overridden = $derived(!!(notify.desktop || notify.remote));
  const remoteName = $derived(target?.remote_label || 'Phone');

  async function submit(value = text, behavior = '') {
    if (compose) value = compose(value);
    if ((!value.trim() && !readyFiles.length) || busy || uploading) return;
    const key = draft;
    busy = true;
    try {
      const opts = {};
      if (behavior) opts.behavior = behavior;
      if (overridden) opts.notify = Object.fromEntries(Object.entries(notify).filter(([, v]) => v));
      if (readyFiles.length) opts.media = readyFiles.map((f) => f.ref);
      if (await send(value, opts)) {
        clearDraft(key);
        if (draft === key) {
          text = '';
          notify = { desktop: '', remote: '' };
          files = [];
        }
      }
    } finally {
      busy = false;
    }
  }
  function onkeydown(e) {
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      submit();
    }
  }
  function pick(b) {
    if (!b.supported) return;
    if (b.name === 'interrupt' && !confirming) {
      confirming = true;
      return;
    }
    menu = false;
    confirming = false;
    submit(text, b.name);
  }
  function setNotify(key, v) {
    notify[key] = v === resolved?.[key] ? '' : v;
  }
  const desktopChoices = $derived.by(() => {
    const base = ['origin', 'all', 'none'];
    const cur = resolved?.desktop;
    return cur && !base.includes(cur) ? [cur, ...base] : base;
  });
</script>

<div class="composer" class:dragging role="group"
  ondragover={(e) => { if (e.dataTransfer?.types?.includes('Files')) { e.preventDefault(); dragging = true; } }}
  ondragleave={() => (dragging = false)} {ondrop}>
  {#if label}<label class="label" for={id}>{label}</label>{/if}
  {#if target?.finished}
    <div class="finished">
      {target.finished === 'archived' ? 'Its Paseo agent is archived' : 'This session has finished'}; sending revives it.
    </div>
  {/if}
  {#if defSupport && !defSupport.supported}
    <div class="finished">Your default, {behaviorLabel[def].toLowerCase()}, is unavailable here: {defSupport.why}.</div>
  {/if}
  <textarea {id} class="input" rows="3" {placeholder} bind:value={text} {onkeydown} {onpaste} disabled={busy}></textarea>
  {#if files.length}
    <ul class="files">
      {#each files as f (f.key)}
        <li class="file {f.status}" title={f.error || ''}>
          <span class="fname">{f.name}</span>
          <span class="faint">{f.status === 'uploading' ? 'uploading…' : f.status === 'error' ? f.error : sizeText(f.ref?.size ?? f.size)}</span>
          <button class="x" aria-label={`Remove ${f.name}`} onclick={() => removeFile(f.key)} disabled={busy}>×</button>
        </li>
      {/each}
    </ul>
  {/if}
  <input bind:this={picker} type="file" multiple hidden onchange={(e) => { addFiles([...e.currentTarget.files]); e.currentTarget.value = ''; }} />
  <div class="bar">
    <button data-cmd="attach" class="btn ghost small attach" disabled={busy || files.length >= MAX_FILES} onclick={() => picker.click()}
      title={tip('Attach files: images, audio, video or anything else (you can also paste or drop them)', 'attach')} aria-label="Attach files">
      <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true"><path d="M10.5 4.5 5.8 9.2a1.3 1.3 0 0 0 1.9 1.9l5-5a2.7 2.7 0 0 0-3.8-3.8l-5 5a4 4 0 0 0 5.7 5.7l4.2-4.2" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" /></svg>
      Attach
    </button>
    {#each quick as q (q.label)}
      <button data-cmd="quick" class="btn small" disabled={busy} onclick={() => submit(q.text)} title={tip(q.text, 'quick')}>{q.label}</button>
    {/each}
    {#if resolved}
      <button data-cmd="notify-menu" class="btn ghost small notify" class:set={overridden} onclick={() => (notifySheet = true)} title={tip('Where the response to this notifies you', 'notify-menu')}>
        <span class="bell" aria-hidden="true">◔</span>
        <span class="notify-text">{remoteNow === 'off' ? 'Desktop only' : remoteNow === 'always' ? `${remoteName} too` : `${remoteName} when away`}{overridden ? ' · changed' : ''}</span>
      </button>
    {/if}
    <span class="hint faint">{hint}</span>
    <div class="split">
      <button data-cmd="send" class="btn primary small main" disabled={busy || uploading || !(text.trim() || ready || readyFiles.length)} onclick={() => submit()} title={tip(behaviorHelp[def] || 'Send', 'send')}>
        {busy ? 'Sending…' : mainLabel}
      </button>
      {#if others.length}
        <button data-cmd="send-menu" class="btn primary small more" disabled={busy || uploading || !(text.trim() || ready || readyFiles.length)} aria-label="Other ways to send" title={tip('Other ways to send', 'send-menu')} onclick={() => { confirming = false; menu = true; }}>
          <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><path d="m2 3.5 3 3 3-3" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
        </button>
      {/if}
    </div>
  </div>
</div>

<Sheet bind:open={menu} title="Send how?">
  <div class="choices">
    <button class="choice current" onclick={() => { menu = false; submit(); }}>
      <span class="choice-name">{behaviorLabel[def]} <span class="faint">· default</span></span>
      <span class="choice-help">{defSupport && !defSupport.supported ? `Unavailable: ${defSupport.why}` : behaviorHelp[def]}</span>
    </button>
    {#each others as b (b.name)}
      <button class="choice" class:danger={b.name === 'interrupt'} disabled={!b.supported} aria-disabled={!b.supported} onclick={() => pick(b)}>
        <span class="choice-name">{behaviorLabel[b.name]}</span>
        <span class="choice-help">{b.supported ? behaviorHelp[b.name] : `Unavailable: ${b.why}`}</span>
      </button>
      {#if b.name === 'interrupt' && confirming}
        <div class="confirm">
          <p>This cancels the agent's running turn and loses its work in progress. Stop it and send?</p>
          <div class="confirm-bar">
            <button class="btn small" onclick={() => (confirming = false)}>Cancel</button>
            <button class="btn small stop" onclick={() => pick(b)}>Stop and send</button>
          </div>
        </div>
      {/if}
    {/each}
  </div>
</Sheet>

<Sheet bind:open={notifySheet} title="Notify me of the response">
  <div class="fields">
    <label class="field">
      <span class="field-name">Desktop</span>
      <select class="input" value={desktopNow} onchange={(e) => setNotify('desktop', e.currentTarget.value)}>
        {#each desktopChoices as v (v)}
          <option value={v}>{desktopText(v)}{v === resolved?.desktop ? ' (default)' : ''}</option>
        {/each}
      </select>
    </label>
    <label class="field">
      <span class="field-name">{remoteName}</span>
      <select class="input" value={remoteNow} onchange={(e) => setNotify('remote', e.currentTarget.value)}>
        {#each ['off', 'away', 'always'] as v (v)}
          <option value={v}>{remoteText(v, target?.remote_label)}{v === resolved?.remote ? ' (default)' : ''}</option>
        {/each}
      </select>
    </label>
    <p class="faint small">For this message only. Standing defaults are under Settings.</p>
    {#if overridden}<button class="btn small" onclick={() => (notify = { desktop: '', remote: '' })}>Back to the defaults</button>{/if}
  </div>
</Sheet>

<style>
  .composer {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .label {
    font-weight: 600;
    font-size: 13px;
  }
  .composer.dragging .input {
    outline: 2px dashed var(--accent);
    outline-offset: 2px;
  }
  .files {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .file {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    max-width: 100%;
    padding: 3px 4px 3px 9px;
    border: 1px solid var(--line);
    border-radius: 999px;
    background: var(--panel);
    font-size: 12.5px;
  }
  .file.error {
    border-color: var(--blocked);
    color: var(--blocked);
  }
  .fname {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 22ch;
  }
  .x {
    border: 0;
    background: none;
    color: var(--muted);
    cursor: pointer;
    font-size: 15px;
    line-height: 1;
    padding: 0 4px;
  }
  .attach {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    color: var(--muted);
  }
  .finished {
    font-size: 12.5px;
    padding: 7px 10px;
    border-radius: 8px;
    background: var(--accent-soft);
    color: var(--accent);
  }
  .bar {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
  }
  .hint {
    flex: 1;
    min-width: 120px;
    font-size: 12px;
  }
  .notify {
    color: var(--muted);
  }
  .notify.set {
    color: var(--default);
  }
  .bell {
    font-size: 12px;
  }
  .split {
    display: inline-flex;
    margin-left: auto;
  }
  .split .main:not(:last-child) {
    border-top-right-radius: 0;
    border-bottom-right-radius: 0;
  }
  .split .more {
    border-top-left-radius: 0;
    border-bottom-left-radius: 0;
    border-left: 1px solid color-mix(in srgb, var(--panel) 35%, transparent);
    padding: 0 7px;
  }
  .choices {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .choice {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 2px;
    text-align: left;
    padding: 10px 12px;
    border-radius: 10px;
    border: 1px solid var(--line);
    background: var(--panel);
    color: var(--text);
    font: inherit;
    cursor: pointer;
  }
  .choice:hover:not(:disabled) {
    border-color: var(--line-strong);
    background: var(--sunken);
  }
  .choice:disabled {
    cursor: not-allowed;
    opacity: 0.55;
  }
  .choice.current {
    border-color: var(--line-strong);
  }
  .choice.danger .choice-name {
    color: var(--blocked);
  }
  .choice-name {
    font-weight: 600;
    font-size: 13.5px;
  }
  .choice-help {
    font-size: 12.5px;
    color: var(--muted);
  }
  .confirm {
    padding: 10px 12px;
    border-radius: 10px;
    background: var(--blocked-soft);
    font-size: 13px;
  }
  .confirm p {
    margin: 0 0 8px;
  }
  .confirm-bar {
    display: flex;
    justify-content: flex-end;
    gap: 6px;
  }
  .stop {
    background: var(--blocked);
    border-color: var(--blocked);
    color: #fff;
  }
  .fields {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .field-name {
    font-size: 12px;
    font-weight: 600;
    color: var(--muted);
  }
  .small {
    font-size: 12px;
    margin: 0;
  }
  @media (max-width: 900px) {
    .hint {
      flex-basis: 100%;
      order: 9;
    }
  }
</style>
