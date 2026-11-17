<script>
  // A text box that sends on ⌘/Ctrl+Enter, with optional quick replies.
  // Given a target, the Send button uses its resolved behaviour, a menu
  // beside it offers the others, and "Notify me" routes the response.
  import { behaviorLabel, behaviorHelp, desktopText, remoteText } from '../lib/format.js';
  import Sheet from './Sheet.svelte';

  let { id = undefined, label, placeholder, send, hint = '', quick = [], sendLabel = 'Send', target = null } = $props();
  let text = $state('');
  let busy = $state(false);
  let menu = $state(false);
  let notifySheet = $state(false);
  let confirming = $state(false);
  let notify = $state({ desktop: '', remote: '' }); // overrides for this message

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
    if (!value.trim() || busy) return;
    busy = true;
    try {
      const opts = {};
      if (behavior) opts.behavior = behavior;
      if (overridden) opts.notify = Object.fromEntries(Object.entries(notify).filter(([, v]) => v));
      if (await send(value, opts)) {
        text = '';
        notify = { desktop: '', remote: '' };
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

<div class="composer">
  {#if label}<label class="label" for={id}>{label}</label>{/if}
  {#if target?.finished}
    <div class="finished">
      {target.finished === 'archived' ? 'Its Paseo agent is archived' : 'This session has finished'}; sending revives it.
    </div>
  {/if}
  {#if defSupport && !defSupport.supported}
    <div class="finished">Your default, {behaviorLabel[def].toLowerCase()}, is unavailable here: {defSupport.why}.</div>
  {/if}
  <textarea {id} class="input" rows="3" {placeholder} bind:value={text} {onkeydown} disabled={busy}></textarea>
  <div class="bar">
    {#each quick as q (q.label)}
      <button class="btn small" disabled={busy} onclick={() => submit(q.text)} title={q.text}>{q.label}</button>
    {/each}
    {#if resolved}
      <button class="btn ghost small notify" class:set={overridden} onclick={() => (notifySheet = true)} title="Where the response to this notifies you">
        <span class="bell" aria-hidden="true">◔</span>
        <span class="notify-text">{remoteNow === 'off' ? 'Desktop only' : remoteNow === 'always' ? `${remoteName} too` : `${remoteName} when away`}{overridden ? ' · changed' : ''}</span>
      </button>
    {/if}
    <span class="hint faint">{hint}</span>
    <div class="split">
      <button class="btn primary small main" disabled={busy || !text.trim()} onclick={() => submit()} title={behaviorHelp[def]}>
        {busy ? 'Sending…' : mainLabel}
      </button>
      {#if others.length}
        <button class="btn primary small more" disabled={busy || !text.trim()} aria-label="Other ways to send" title="Other ways to send" onclick={() => { confirming = false; menu = true; }}>
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
