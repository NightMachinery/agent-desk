<script>
  // Push to this device: subscribes the browser to the desk's Web Push
  // messages (internal/cli/push.go), and lists the devices that did. The
  // notify.push setting decides which kinds of item are pushed.
  import { get, post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';

  let { stamp = '' } = $props();

  const supported =
    typeof window !== 'undefined' &&
    window.isSecureContext &&
    'serviceWorker' in navigator &&
    'PushManager' in window &&
    'Notification' in window;
  const ios = typeof navigator !== 'undefined' && /iPhone|iPad/.test(navigator.userAgent);

  let info = $state(null);
  let mine = $state(''); // this browser's subscription id, if it has one
  let busy = $state(false);
  let error = $state('');
  const subscribed = $derived(!!mine && !!info?.devices.some((d) => d.id === mine));
  const blocked = $derived(supported && typeof Notification !== 'undefined' && Notification.permission === 'denied');

  // The server names a device by the first 6 bytes of its endpoint's SHA-256.
  async function idOf(endpoint) {
    const b = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(endpoint));
    return [...new Uint8Array(b).slice(0, 6)].map((x) => x.toString(16).padStart(2, '0')).join('');
  }
  function keyBytes(b64) {
    const s = atob(b64.replace(/-/g, '+').replace(/_/g, '/'));
    return Uint8Array.from(s, (c) => c.charCodeAt(0));
  }
  function sameKey(buf, b64) {
    if (!buf) return false;
    const a = new Uint8Array(buf);
    const b = keyBytes(b64);
    return a.length === b.length && a.every((x, i) => x === b[i]);
  }
  async function current() {
    const reg = await navigator.serviceWorker.ready;
    return [reg, await reg.pushManager.getSubscription()];
  }

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

  function label() {
    const ua = navigator.userAgent;
    const os = /Android/.test(ua) ? 'Android' : ios ? 'iOS' : /Mac OS X/.test(ua) ? 'macOS' : /Windows/.test(ua) ? 'Windows' : /Linux/.test(ua) ? 'Linux' : 'a device';
    const br = /Firefox\//.test(ua) ? 'Firefox' : /Edg\//.test(ua) ? 'Edge' : /Chrome\//.test(ua) ? 'Chrome' : /Safari\//.test(ua) ? 'Safari' : 'a browser';
    const app = matchMedia('(display-mode: standalone)').matches ? ', installed app' : '';
    return `${br} on ${os}${app}`;
  }

  async function enable() {
    busy = true;
    try {
      const perm = await Notification.requestPermission();
      if (perm !== 'granted')
        throw new Error(perm === 'denied' ? 'Notifications are blocked for this site: allow them in the site settings, then try again.' : 'Notifications were not allowed.');
      const [reg, old] = await current();
      let sub = old;
      // A subscription made for another key cannot carry the desk's pushes.
      if (sub && !sameKey(sub.options.applicationServerKey, info.key)) {
        await sub.unsubscribe();
        sub = null;
      }
      if (!sub) sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: keyBytes(info.key) });
      await post('push', { action: 'subscribe', subscription: sub.toJSON(), label: label() });
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
      Notifications arrive even while the app is closed, and go away once the item is answered or read anywhere. Which kinds are
      pushed is the “Push to subscribed devices” setting above (built in: blocked questions). {info.off ? 'Pushing is turned off in the config ([push] off).' : ''}
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
