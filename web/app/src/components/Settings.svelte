<script>
  // The settings at one scope: one control per key, showing the resolved
  // value and where it comes from, for all kinds of event or for one.
  import { get, post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { href } from '../lib/route.svelte.js';
  import { desktopText, remoteText, pushText, originText, behaviorLabel } from '../lib/format.js';
  import { ui, saveUI } from '../lib/ui.svelte.js';
  import PushSettings from './PushSettings.svelte';
  const lingerChoices = [0, 15, 30, 60, 120, 300];
  const lingerText = (n) => (n === 0 ? 'Drop it at once' : n < 60 ? `${n} seconds` : n === 60 ? '1 minute' : `${n / 60} minutes`);

  let { path = '', stamp = '', onchange = () => {} } = $props();

  let cls = $state('*');
  let view = $state(null);
  let error = $state('');
  let busy = $state(false);
  let dest = $state('');

  const classNames = {
    '*': 'All kinds',
    'ask-blocked': 'Blocked questions',
    'ask-default': 'Questions with a default',
    response: 'Responses to your messages',
    'tell-notify': 'Notices that ask to notify',
    tell: 'Other notices',
    done: 'Done reports',
  };
  const keyNames = {
    'deliver.behavior': 'Messages to a busy agent',
    'notify.desktop': 'Desktop notification',
    'notify.remote': 'Phone notification',
    'notify.remote.dest': 'Phone destination',
    'notify.push': 'When to push (to every device with push turned on)',
  };

  async function load() {
    try {
      view = await get('prefs', { path, class: cls === '*' ? '' : cls });
      dest = view.keys.find((k) => k.key === 'notify.remote.dest')?.value ?? '';
      error = '';
    } catch (e) {
      error = e.message;
    }
  }
  $effect(() => {
    void [path, cls, stamp];
    load();
  });

  function valueText(key, v) {
    if (key === 'deliver.behavior') return `${behaviorLabel[v] || v} (${v})`;
    if (key === 'notify.desktop') return desktopText(v);
    if (key === 'notify.remote') return remoteText(v, view?.remote_label);
    if (key === 'notify.push') return pushText(v);
    return v || 'not set';
  }
  function choices(k) {
    const out = [...(k.options ?? [])];
    if (k.key === 'notify.desktop') for (const h of view.hosts) if (!out.includes(h)) out.push(h);
    if (k.value && !out.includes(k.value)) out.unshift(k.value);
    return out;
  }
  async function save(k, value) {
    busy = true;
    try {
      const body = { scope: path, key: k.key, value };
      if (!k.classless && cls !== '*') body.class = cls;
      const res = await post('pref', body);
      toast(value === null ? 'Reset to the inherited value.' : res.note || 'Saved.', 'ok');
      await load();
      onchange();
    } catch (e) {
      toast(e.message, 'error');
    } finally {
      busy = false;
    }
  }
</script>

<div class="settings">
  <div class="scope-line">
    <span class="faint">Scope</span>
    <strong>{path || 'Everywhere'}</strong>
    {#if path}<a class="faint link" href={href({ path: '', item: '', settings: true })}>global settings</a>{/if}
  </div>
  <label class="field">
    <span class="field-name">For</span>
    <select class="input" bind:value={cls}>
      {#each Object.entries(classNames) as [v, name] (v)}<option value={v}>{name}</option>{/each}
    </select>
  </label>

  {#if error}
    <div class="empty">{error}</div>
  {:else if view}
    {#each view.keys as k (k.key)}
      <div class="key">
        <div class="key-head">
          <span class="key-name">{view.remote_label ? (keyNames[k.key] || k.key).replace('Phone', view.remote_label) : keyNames[k.key] || k.key}</span>
          <span class="mono faint">{k.key}</span>
        </div>
        {#if k.key === 'notify.remote.dest'}
          <div class="dest">
            <input class="input" bind:value={dest} placeholder="where your phone notifier sends" disabled={busy} />
            <button class="btn small" disabled={busy || !dest.trim() || dest === k.value} onclick={() => save(k, dest.trim())}>Save</button>
          </div>
        {:else}
          <select class="input" value={k.value} disabled={busy} onchange={(e) => save(k, e.currentTarget.value)}>
            {#each choices(k) as v (v)}<option value={v}>{valueText(k.key, v)}</option>{/each}
          </select>
        {/if}
        <div class="origin">
          <span class="faint">{originText(k.origin, path)}{k.classless && cls !== '*' ? ' · the same for every kind' : ''}</span>
          {#if k.here}<button class="btn ghost small" disabled={busy} onclick={() => save(k, null)}>Reset to inherited</button>{/if}
        </div>
        {#if k.differs?.length}
          <div class="differs faint">
            Differs for {k.differs.map((d) => `${(classNames[d.class] || d.class).toLowerCase()} (${valueText(k.key, d.value)})`).join(', ')}.
          </div>
        {/if}
      </div>
    {/each}
    {#if view.overrides.length}
      <details class="overrides">
        <summary>Every override on the desk ({view.overrides.length})</summary>
        <ul>
          {#each view.overrides as p (p.id)}
            <li>
              <span class="mono">{p.key}</span> = <strong>{p.value}</strong>
              <span class="faint">{p.scope || 'everywhere'}{p.class !== '*' ? ` · ${p.class}` : ''}</span>
            </li>
          {/each}
        </ul>
      </details>
    {/if}
  {:else}
    <div class="skeleton"></div>
  {/if}

  {#if !path}
    <PushSettings {stamp} />
    <div class="key">
      <div class="key-head">
        <span class="key-name">Keep a read item in the list for</span>
        <span class="mono faint">this device</span>
      </div>
      <select class="input" value={ui.lingerSeconds} onchange={(e) => ((ui.lingerSeconds = +e.currentTarget.value), saveUI())}>
        {#each lingerChoices as n (n)}<option value={n}>{lingerText(n)}</option>{/each}
      </select>
      <div class="origin"><span class="faint">Opening an item marks it read. It stays in place, dimmed once you move on, for this long.</span></div>
    </div>
    <div class="key">
      <div class="key-head">
        <span class="key-name">Move to the next item</span>
        <span class="mono faint">this device</span>
      </div>
      <label class="check"><input type="checkbox" checked={ui.nextAfterQuestion} onchange={(e) => ((ui.nextAfterQuestion = e.currentTarget.checked), saveUI())} /> after answering, approving, denying or dismissing a question</label>
      <label class="check"><input type="checkbox" checked={ui.nextAfterMessage} onchange={(e) => ((ui.nextAfterMessage = e.currentTarget.checked), saveUI())} /> after sending a message in a conversation</label>
      <div class="origin"><span class="faint">On a wide screen, to the next item that is not pinned; on a phone, back to the list. Off: stay on the item.</span></div>
    </div>
  {/if}
</div>

<style>
  .check {
    display: flex;
    gap: 6px;
    align-items: center;
    font-size: 13px;
  }
  .settings {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .scope-line {
    display: flex;
    align-items: baseline;
    gap: 8px;
    flex-wrap: wrap;
    font-size: 13px;
  }
  .link {
    text-decoration: underline;
    margin-left: auto;
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
  .key {
    display: flex;
    flex-direction: column;
    gap: 5px;
    padding: 10px 12px;
    border-radius: 10px;
    background: var(--sunken);
  }
  .key-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
    flex-wrap: wrap;
  }
  .key-name {
    font-weight: 600;
    font-size: 13.5px;
  }
  .key-head .mono {
    font-size: 11px;
  }
  .origin {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    min-height: 26px;
    font-size: 12px;
  }
  .differs {
    font-size: 12px;
  }
  .dest {
    display: flex;
    gap: 6px;
  }
  .overrides {
    font-size: 12.5px;
  }
  .overrides summary {
    cursor: pointer;
    color: var(--muted);
  }
  .overrides ul {
    margin: 6px 0 0;
    padding-left: 18px;
  }
  .skeleton {
    height: 200px;
    border-radius: 10px;
    background: var(--sunken);
  }
</style>
