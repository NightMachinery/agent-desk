<script>
  // Push to this device: subscribes the browser to the desk's Web Push
  // messages (internal/cli/push.go), and lists the devices that did. The
  // notify.push setting decides which kinds of item are pushed.
  import { get, post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { current, idOf, ios, pushSupported, subscribeHere } from '../lib/push.js';

  let { stamp = '' } = $props();

  const supported = pushSupported;

  let info = $state(null);
  let mine = $state(''); // this browser's subscription id, if it has one
  let busy = $state(false);
  let error = $state('');
  const subscribed = $derived(!!mine && !!info?.devices.some((d) => d.id === mine));
  const blocked = $derived(supported && typeof Notification !== 'undefined' && Notification.permission === 'denied');

  async function load() {
    try {
      info = await get('push');
      if (supported) {
        const [, sub] = await current();
        mine = sub ? await idOf(sub.endpoint) : '';
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
      toast('Push is on for this device. Try Test.', 'ok');
      await load();
    } catch (e) {
      toast(e.message, 'error');
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
  const when = (ts) => (ts ? new Date(ts.split(' ')[0]).toLocaleString([], { dateStyle: 'medium', timeStyle: 'short' }) : '');
</script>

<div class="key">
  <div class="key-head">
    <span class="key-name">Push to this device</span>
    <span class="mono faint">this device</span>
  </div>
  {#if !supported}
    <div class="faint note">
      {ios ? 'On an iPhone or iPad only the installed app can receive push: open this page in Safari, Share, Add to Home Screen, then open it from there.' : 'This browser cannot receive push notifications here (it needs HTTPS and a browser with Web Push).'}
    </div>
  {:else if error}
    <div class="faint note">{error}</div>
  {:else if info}
    <div class="row">
      {#if subscribed}
        <button class="btn small" disabled={busy} onclick={() => act('test', mine)} data-cmd="push-test">Test</button>
        <button class="btn ghost small" disabled={busy} onclick={disable} data-cmd="push-off">Turn off</button>
      {:else}
        <button class="btn primary small" disabled={busy || blocked} onclick={enable} data-cmd="push-on">Turn on</button>
      {/if}
      <span class="faint">{subscribed ? 'On' : blocked ? 'Notifications are blocked for this site in the browser' : 'Off'}</span>
    </div>
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
</div>

<style>
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
