<script>
  // The settings at one scope: one control per key, showing the resolved
  // value and where it comes from, for all kinds of event or for one.
  import { get, post } from '../lib/api.js';
  import { exportHref } from '../lib/exports.js';
  import { toast } from '../lib/toast.svelte.js';
  import { href } from '../lib/route.svelte.js';
  import { desktopText, remoteText, awayChoice, pushText, originText, behaviorLabel, autoContinueText } from '../lib/format.js';
  import { ui, saveUI, setNav } from '../lib/ui.svelte.js';
  import PushSettings from './PushSettings.svelte';
  import SectionOrder from './SectionOrder.svelte';
  import NavSettings from './NavSettings.svelte';
  import PromptEditor from './PromptEditor.svelte';
  import { off, BUSY } from '../lib/why.js';
  import { keyFor } from '../lib/keys.svelte.js';
  import { failed } from '../lib/errors.svelte.js';
  const lingerChoices = [0, 15, 30, 60, 120, 300];
  const lingerText = (n) => (n === 0 ? 'Drop it at once' : n < 60 ? `${n} seconds` : n === 60 ? '1 minute' : `${n / 60} minutes`);

  let { path = '', stamp = '', onchange = () => {} } = $props();

  // The org files (orgfiles.go): the server renders them once the desk has
  // been quiet for `debounce` seconds, when on. Shared by every device.
  const quietChoices = [1, 2, 5, 10, 30, 60, 300];
  const quietText = (n) => (n < 60 ? `${n} second${n === 1 ? '' : 's'}` : n === 60 ? '1 minute' : `${n / 60} minutes`);
  let org = $state(/** @type {any} */ (null));
  let orgBusy = $state(false);
  async function loadOrg() {
    try {
      org = await get('render');
    } catch {
      org = null;
    }
  }
  // Task changes (taskchanges.go): the server tells the agent working on a
  // task when the user changes its state, once no change has come for
  // `debounce` seconds. Shared by every device.
  const tellChoices = [3, 5, 10, 15, 30, 60, 120, 300, 600];
  let tc = $state(/** @type {any} */ (null));
  async function loadTc() {
    try {
      tc = await get('task-changes');
    } catch {
      tc = null;
    }
  }
  async function saveTc(over) {
    const data = { enabled: tc.enabled, debounce: tc.debounce, ...over };
    try {
      await post('dashboard-prefs', { key: 'task-changes', data });
      tc = { ...tc, ...data };
    } catch (err) {
      failed(err);
    }
  }
  // Reminders to agents (reminders.go): which are on, and on what; shared
  // by every device. Their templates, and every other prompt the desk
  // sends, are files on this machine (api/prompts).
  const hourChoices = [0, 1, 3, 6, 12, 24];
  const hourText = (n) => (n === 0 ? 'every time' : n === 1 ? '1 hour' : `${n} hours`);
  let rem = $state(/** @type {any} */ (null));
  let prompts = $state(/** @type {any[]} */ ([]));
  async function loadRem() {
    try {
      rem = await get('reminders');
    } catch {
      rem = null;
    }
    try {
      prompts = (await get('prompts')).prompts ?? [];
    } catch {
      prompts = [];
    }
  }
  async function saveRem(part, over) {
    const data = { ...rem, [part]: { ...rem[part], ...over } };
    try {
      await post('dashboard-prefs', { key: 'reminders', data });
      rem = data;
    } catch (err) {
      failed(err, 'save the reminders');
    }
  }
  const reminderNames = ['nag-no-task.md', 'nag-velocity.md'];
  const promptAt = (name) => prompts.findIndex((x) => x.name === name);
  $effect(() => {
    stamp;
    if (!path) loadOrg(), loadTc(), loadRem();
  });
  async function saveOrg(over) {
    const data = { enabled: org.enabled, debounce: org.debounce, ...over };
    try {
      await post('dashboard-prefs', { key: 'render', data });
      org = { ...org, ...data };
      setTimeout(loadOrg, 800); // turning it on renders at once
    } catch (err) {
      failed(err);
    }
  }
  async function renderOrg() {
    orgBusy = true;
    try {
      org = await post('render', {});
      toast(`Org files rendered in ${org.took_ms} ms.`, 'ok');
    } catch (err) {
      failed(err);
    } finally {
      orgBusy = false;
    }
  }
  const orgWhen = (ts) => new Date(ts).toLocaleString([], { hour: '2-digit', minute: '2-digit', second: '2-digit', day: 'numeric', month: 'short' });

  let cls = $state('*');
  let view = $state(null);
  let error = $state('');
  let busy = $state(false);
  let dest = $state('');

  const classNames = {
    '*': 'All kinds',
    'ask-blocked': 'Blocked questions',
    stopped: 'Agents stopped by an error',
    'ask-default': 'Questions with a default',
    response: 'Responses to your messages',
    deliverable: 'Deliverables (deployments, packages)',
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
    'auto-continue.main': 'Auto-continue for main agents',
    'auto-continue.subagents': 'Auto-continue for subagents',
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

  // choice: the text of an option under the setting's own heading.
  function valueText(key, v, choice = false) {
    if (key === 'deliver.behavior') return `${behaviorLabel[v] || v} (${v})`;
    if (key === 'notify.desktop') return desktopText(v);
    if (key === 'notify.remote') return choice ? awayChoice(v) : remoteText(v, view?.remote_label);
    if (key === 'notify.push') return choice ? awayChoice(v) : pushText(v);
    if (key.startsWith('auto-continue.')) return autoContinueText(v);
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
      failed(e);
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
            <button data-keys="none" class="btn small" {...off(undefined, [busy, BUSY], [!dest.trim(), 'Type where it sends first'], [dest === k.value, 'Unchanged'])} onclick={() => save(k, dest.trim())}>Save</button>
          </div>
        {:else}
          <select class="input" value={k.value} disabled={busy} onchange={(e) => save(k, e.currentTarget.value)}>
            {#each choices(k) as v (v)}<option value={v}>{valueText(k.key, v, true)}</option>{/each}
          </select>
        {/if}
        <div class="origin">
          <span class="faint">{originText(k.origin, path)}{k.classless && cls !== '*' ? ' · the same for every kind' : ''}{k.key.startsWith('auto-continue.') ? ' · an agent with its own choice keeps it' : ''}</span>
          {#if k.here}<button data-keys="none" class="btn ghost small" {...off(undefined, [busy, BUSY])} onclick={() => save(k, null)}>Reset to inherited</button>{/if}
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
    <div class="key" id="section-order">
      <div class="key-head">
        <span class="key-name">Order of the list's sections</span>
        <span class="mono faint">every device</span>
      </div>
      <SectionOrder />
    </div>
    <div class="key" id="nav-order">
      <div class="key-head">
        <span class="key-name">Auto-navigation: where n and moving on go</span>
        <span class="mono faint">every device</span>
      </div>
      <label class="check"><input type="checkbox" data-cmd="nav-views" checked={ui.nav.views} onchange={(e) => setNav({ views: e.currentTarget.checked })} /> Fewer views first: items opened less often come first</label>
      <div class="origin"><span class="faint">An open question stays listed after you open it; everything else is read on opening and drops out. Then by kind, and within a kind by age:</span></div>
      <NavSettings />
    </div>
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
      <div class="origin"><span class="faint">On a wide screen, to the next open item in the auto-navigation order; on a phone, back to the list. Off: stay on the item.</span></div>
    </div>
    <div class="key" id="org-files">
      <div class="key-head">
        <span class="key-name">Org files</span>
        <span class="mono faint">every device</span>
      </div>
      {#if org}
        <label class="check"><input type="checkbox" checked={org.enabled} onchange={(e) => saveOrg({ enabled: e.currentTarget.checked })} /> Keep them up to date, once the desk has been quiet for</label>
        <select class="input" aria-label="Quiet time before the org files are rendered" value={org.debounce} {...off(undefined, [!org.enabled, 'Turn on rendering the org files first'])} onchange={(e) => saveOrg({ debounce: +e.currentTarget.value })}>
          {#each quietChoices as n (n)}<option value={n}>{quietText(n)}</option>{/each}
        </select>
        <div class="org-acts">
          <button class="btn small" data-cmd="org-render" {...off(undefined, [orgBusy, 'Wait: rendering'])} onclick={renderOrg}>Render now</button>
          <a class="btn small" data-cmd="org-export" href={exportHref()} download title="The inbox, boards, tasks and ledger as top-level headings of one file">Export as one .org file</a>
          <a class="btn small ghost" data-cmd="org-export-inbox" href={exportHref({ scope: 'inbox' })} download>Inbox only</a>
          <a class="btn small ghost" data-cmd="org-export-zip" href={exportHref({ zip: '1' })} download title="The same files as the render folder">As a zip</a>
        </div>
        <div class="origin">
          <span class="faint">
            The inbox, a board per project and a ledger per month, as org, in <span class="mono">{org.dir}</span>: for Emacs (agent-desk.el) and <code>cat</code>. Export downloads them as one .org file (or the same files as a zip); a node or an item exports alone from its page (<span class="mono">{keyFor('org-export-node') || 'M-x org-export-node'}</span>, <span class="mono">{keyFor('org-export-item') || 'M-x org-export-item'}</span>); <code>agent-desk render</code> renders them from a shell.
            {#if org.last}Last rendered {orgWhen(org.last)}, in {org.took_ms} ms.{/if}
            {#if org.error}<span class="err">Last render failed: {org.error}</span>{/if}
          </span>
        </div>
      {:else}
        <span class="faint">Cannot read the org file settings from the desk.</span>
      {/if}
    </div>
    <div class="key" id="task-changes">
      <div class="key-head">
        <span class="key-name">Telling agents about task changes</span>
        <span class="mono faint">every device</span>
      </div>
      {#if tc}
        <label class="check"><input type="checkbox" checked={tc.enabled} onchange={(e) => saveTc({ enabled: e.currentTarget.checked })} /> When you mark a task done, drop, reopen, start or defer it, tell the agent working on it once you have made no change for</label>
        <select class="input" aria-label="Quiet time before agents are told of task changes" value={tc.debounce} {...off(undefined, [!tc.enabled, 'Turn on telling agents of task changes first'])} onchange={(e) => saveTc({ debounce: +e.currentTarget.value })}>
          {#each [...new Set([...tellChoices, tc.debounce])].sort((a, b) => a - b) as n (n)}<option value={n}>{quietText(n)}</option>{/each}
        </select>
        <div class="origin">
          <span class="faint">
            Each agent gets one message for all your changes in that time; a change you undo in it is not sent. Reopen asks what is still wrong (you can leave it empty) and passes that on. The wording is <span class="mono">internal/prompts/task-changes.md</span>; a copy in <span class="mono">~/.config/agent-desk/prompts/</span> replaces it.
          </span>
        </div>
      {:else}
        <span class="faint">Cannot read this setting from the desk.</span>
      {/if}
    </div>
    <div class="key" id="reminders">
      <div class="key-head">
        <span class="key-name">Reminders to agents</span>
        <span class="mono faint">every device</span>
      </div>
      {#if rem}
        <div class="sub">No started task</div>
        <label class="check"><input type="checkbox" data-cmd="reminders" checked={rem.no_task.on} onchange={(e) => saveRem('no_task', { on: e.currentTarget.checked })} /> When a message reaches an agent with no started task at its node, add one line asking it to file and start one. On:</label>
        <div class="occasions">
          {#each [['user', 'messages you type'], ['desk', 'your answers and messages from the desk'], ['heartbeat', 'Paseo heartbeats and schedules'], ['agent', 'messages from other agents']] as [k, label] (k)}
            <label class="check"><input type="checkbox" checked={rem.no_task[k]} disabled={!rem.no_task.on} onchange={(e) => saveRem('no_task', { [k]: e.currentTarget.checked })} /> {label}</label>
          {/each}
        </div>
        <div class="origin"><span class="faint">Never on cache keepalive pings, <code>/compact</code> and other slash commands, harness notices or the auto-continue ping. It comes from the prompt hook (<code>agent-desk hook prompt</code>), which adds its output to the message.</span></div>
        {#if promptAt('nag-no-task.md') >= 0}<PromptEditor bind:prompt={prompts[promptAt('nag-no-task.md')]} />{/if}
        <div class="sub">Velocity</div>
        <label class="check"><input type="checkbox" checked={rem.velocity.on} onchange={(e) => saveRem('velocity', { on: e.currentTarget.checked })} /> When a coordinator marks a task done, ask it to update its velocity slot when the slot is missing or older than</label>
        <select class="input" aria-label="How old the velocity slot may be before the reminder" value={rem.velocity.hours} {...off(undefined, [!rem.velocity.on, 'Turn on the velocity reminder first'])} onchange={(e) => saveRem('velocity', { hours: +e.currentTarget.value })}>
          {#each [...new Set([...hourChoices, rem.velocity.hours])].sort((a, b) => a - b) as n (n)}<option value={n}>{hourText(n)}</option>{/each}
        </select>
        <div class="origin"><span class="faint">A coordinator holds a run's top node, or a node with nodes under it. <code>agent-desk task done</code> prints the line to it.</span></div>
        {#if promptAt('nag-velocity.md') >= 0}<PromptEditor bind:prompt={prompts[promptAt('nag-velocity.md')]} />{/if}
      {:else}
        <span class="faint">Cannot read this setting from the desk.</span>
      {/if}
    </div>
    {#if prompts.length}
      <div class="key" id="prompts">
        <div class="key-head">
          <span class="key-name">Other prompts the desk sends</span>
          <span class="mono faint">this machine</span>
        </div>
        <div class="origin"><span class="faint">Go templates. Your copy of one, saved here or written by hand, replaces the built-in one on this machine.</span></div>
        {#each prompts as p, i (p.name)}
          {#if !reminderNames.includes(p.name)}
            <details class="more-prompt">
              <summary><span class="mono">{p.name}</span>{#if p.overridden} <span class="chip">your copy</span>{/if}</summary>
              <PromptEditor bind:prompt={prompts[i]} />
            </details>
          {/if}
        {/each}
      </div>
    {/if}
  {/if}
</div>

<style>
  .org-acts {
    display: flex;
    gap: 6px;
    flex-wrap: wrap;
  }
  .org-acts a {
    text-decoration: none;
  }
  .err {
    color: var(--blocked);
  }
  .sub {
    margin-top: 6px;
    font-size: 12px;
    font-weight: 700;
    color: var(--muted);
  }
  .occasions {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin-left: 20px;
  }
  .more-prompt summary {
    cursor: pointer;
    font-size: 13px;
  }
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
