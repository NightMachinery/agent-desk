<script>
  // Push to this device: subscribes the browser to the desk's Web Push
  // messages (internal/cli/push.go), and lists the devices that did. The
  // notify.push setting decides which kinds of item are pushed. A failed
  // Turn on stays shown, saying what to do; Copy logs copies a report of
  // this browser's push state and the desk's push log (push.js).
  import { get, post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { copy } from '../lib/format.js';
  import { brave, braveSetting, copyPushLogs, current, ensureRegistered, idOf, ios, pushSupported, subscribeHere } from '../lib/push.js';

  let { stamp = '' } = $props();

  const supported = pushSupported;

  let info = $state(null);
  let mine = $state(''); // this browser's subscription id, if it has one
  let busy = $state(false);
  let error = $state('');
  let failure = $state(/** @type {{ message: string, raw: string } | null} */ (null));
  let report = $state(''); // shown when the browser would not copy it
  const subscribed = $derived(!!mine && !!info?.devices.some((d) => d.id === mine));
  const blocked = $derived(supported && typeof Notification !== 'undefined' && Notification.permission === 'denied');

  async function load() {
    try {
      info = await get('push');
      if (supported) {
        const [, sub] = await current();
        mine = sub ? await idOf(sub.endpoint) : '';
        // Held here but not listed by the desk: register it again quietly.
        if (mine && !info.devices.some((d) => d.id === mine) && (await ensureRegistered(info).catch(() => ''))) info = await get('push');
      }
      error = '';
    } catch (e) {
      error = e.message;
    }
  }
  $effect(() => {
    void stamp;
    load();
  });

  async function enable() {
    busy = true;
    try {
      mine = await subscribeHere(info);
      failure = null;
      toast('Push is on for this device. Try Test.', 'ok');
      await load();
    } catch (e) {
      failure = { message: e.message, raw: e.raw ?? '' };
      toast(e.message, 'error', 12000, { label: 'Copy logs', run: copyLogs });
    } finally {
      busy = false;
    }
  }
  async function disable() {
    busy = true;
    try {
      const [, sub] = await current();
      if (sub) {
        await post('push', { action: 'unsubscribe', subscription: { endpoint: sub.endpoint } });
        await sub.unsubscribe();
      }
      toast('Push is off for this device.', 'ok');
      await load();
    } catch (e) {
      toast(e.message, 'error');
    } finally {
      busy = false;
    }
  }
  async function act(action, id) {
    busy = true;
    try {
      await post('push', { action, id });
      toast(action === 'test' ? 'Sent. It should appear within a few seconds.' : 'Removed.', 'ok');
      await load();
    } catch (e) {
      toast(e.message, 'error');
      await load();
    } finally {
      busy = false;
    }
  }
  async function copyLogs() {
    const { ok, text } = await copyPushLogs();
    report = ok ? '' : text;
  }
  async function copySettings() {
    const ok = await copy(braveSetting);
    toast(ok ? 'Copied: paste it into the address bar.' : 'The browser would not copy it.', ok ? 'ok' : 'error');
  }
  const when = (ts) => (ts ? new Date(ts.split(' ')[0]).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' }) : '');
</script>

<div class="key">
  <div class="key-head">
    <span class="key-name">Push to this device</span>
    <span class="head-acts">
      <button class="btn ghost small" onclick={copyLogs} data-cmd="push-logs" title="Copy a report of this browser's push state and the desk's push log, to send with a problem">Copy logs</button>
      <span class="mono faint">this device</span>
    </span>
  </div>
  {#if !supported}
    <div class="faint note">
      {ios ? 'On an iPhone or iPad only the installed app can receive push: open this page in Safari, Share, Add to Home Screen, then open it from there.' : 'This browser cannot receive push notifications here (it needs HTTPS and a browser with Web Push).'}
    </div>
  {:else if error}
    <div class="faint note">{error}</div>
  {:else if info}
    {#if brave && !subscribed}
      <div class="note brave">
        Brave sends push only through Google's push service, which it leaves off. Before Turn on, switch on “Use Google services
        for push messaging”: copy its address and paste it into the address bar (a page cannot open it), or find it in Brave's
        Settings under Privacy and security. If Turn on still fails, relaunch Brave.
        <button class="btn ghost small" onclick={copySettings} data-cmd="push-brave-setting">Copy its address</button>
      </div>
    {/if}
    <div class="row">
      {#if subscribed}
        <button class="btn small" disabled={busy} onclick={() => act('test', mine)} data-cmd="push-test">Test</button>
        <button class="btn ghost small" disabled={busy} onclick={disable} data-cmd="push-off">Turn off</button>
      {:else}
        <button class="btn primary small" disabled={busy || blocked} onclick={enable} data-cmd="push-on">Turn on</button>
      {/if}
      <span class="faint">{subscribed ? 'On' : blocked ? 'Notifications are blocked for this site in the browser' : 'Off'}</span>
    </div>
    {#if failure && !subscribed}
      <div class="failure" role="alert">
        <div><strong>Push could not be turned on here.</strong> {failure.message}</div>
        {#if failure.raw && failure.raw !== failure.message}<div class="faint">The browser said: <span class="mono">{failure.raw}</span></div>{/if}
        <div class="row">
          <button class="btn small" onclick={copyLogs}>Copy logs</button>
          {#if brave && /brave:\/\/settings/.test(failure.message)}<button class="btn ghost small" onclick={copySettings}>Copy the settings address</button>{/if}
          <button class="btn ghost small" onclick={() => (failure = null)}>Dismiss</button>
        </div>
      </div>
    {/if}
    <div class="faint note">
      Turn this on once on each phone or browser that should get the desk's notifications; allowing notifications in the phone's
      settings alone is not enough. Which items are pushed is the “When to push” setting above, the same for every device (built
      in: blocked questions). Notifications arrive even while the app is closed, and go away once the item is answered, read or
      dismissed anywhere. {info.off ? 'Pushing is turned off in the config ([push] off).' : ''}
    </div>
    {#if info.devices.length}
      <ul class="devices">
        {#each info.devices as d (d.id)}
          <li>
            <span>{d.label}{d.id === mine ? ' (this device)' : ''}</span>
            <span class="faint">
              {d.last_error ? `failed ${when(d.last_error)}` : d.last_ok ? `last push ${when(d.last_ok)}` : `added ${when(d.created)}`}
            </span>
            {#if d.id !== mine}
              <button class="btn ghost small" disabled={busy} onclick={() => act('test', d.id)}>Test</button>
              <button class="btn ghost small" disabled={busy} onclick={() => act('remove', d.id)}>Remove</button>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  {:else}
    <div class="faint note">Reading…</div>
  {/if}
  {#if report}
    <div class="report">
      <div class="faint">The browser would not copy the logs; select them here instead.</div>
      <textarea class="input mono" readonly rows="10" onfocus={(e) => e.currentTarget.select()}>{report}</textarea>
      <button class="btn ghost small" onclick={() => (report = '')}>Hide</button>
    </div>
  {/if}
</div>

<style>
  .head-acts {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .failure {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 8px 0;
    padding: 10px 12px;
    border: 1px solid var(--blocked);
    border-radius: var(--radius);
    background: var(--blocked-soft);
    overflow-wrap: anywhere;
  }
  .brave {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
  }
  .report {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 6px;
    margin-top: 8px;
  }
  .report textarea {
    width: 100%;
    font-size: 11.5px;
  }
  .key {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 10px 12px;
    border-radius: 10px;
    background: var(--sunken);
  }
  .key-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
  }
  .key-name {
    font-weight: 600;
    font-size: 13.5px;
  }
  .key-head .mono {
    font-size: 11px;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12.5px;
  }
  .note {
    font-size: 12px;
  }
  .devices {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 12.5px;
  }
  .devices li {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
</style>
