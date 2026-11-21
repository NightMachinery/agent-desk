<script>
  // On a phone or an installed app that does not receive the desk's pushes,
  // a strip above the list offers to turn them on. Allowing notifications in
  // the phone's settings is not enough: the page has to subscribe. "Not now"
  // hides it on this device for 30 days. A subscription the browser already
  // holds is registered again silently instead (ensureRegistered).
  import { get } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { copyPushLogs, ensureRegistered, pushSupported, subscribeHere } from '../lib/push.js';

  const KEY = 'agent-desk:push-nudge-off';
  const QUIET = 30 * 24 * 3600 * 1000;
  const phoneLike =
    typeof window !== 'undefined' &&
    (/Android|iPhone|iPad/.test(navigator.userAgent) || matchMedia('(display-mode: standalone)').matches);

  let show = $state(false);
  let busy = $state(false);
  let info = null;

  function quiet() {
    try {
      return Date.now() - Number(localStorage.getItem(KEY) || 0) < QUIET;
    } catch {
      return false;
    }
  }
  $effect(() => {
    if (!pushSupported || !phoneLike || quiet() || Notification.permission === 'denied') return;
    get('push')
      .then(async (i) => {
        info = i;
        // A subscription the browser holds but the desk lost is re-sent
        // quietly; the strip shows only when there is none to send.
        show = !(await ensureRegistered(i).catch(() => ''));
      })
      .catch(() => {});
  });

  async function on() {
    busy = true;
    try {
      await subscribeHere(info);
      show = false;
      toast('Push is on for this device. Settings has a Test button.', 'ok');
    } catch (e) {
      toast(e.message, 'error', 12000, { label: 'Copy logs', run: copyPushLogs });
    } finally {
      busy = false;
    }
  }
  function later() {
    try {
      localStorage.setItem(KEY, String(Date.now()));
    } catch {}
    show = false;
  }
</script>

{#if show}
  <div class="nudge">
    <span>This device does not get the desk's notifications yet.</span>
    <button class="btn small primary" disabled={busy} onclick={on} data-cmd="push-nudge-on">Turn on</button>
    <button class="btn small ghost" onclick={later}>Not now</button>
  </div>
{/if}

<style>
  .nudge {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
    margin: 0 0 10px;
    padding: 8px 10px;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--accent-soft);
  }
  .nudge span {
    flex: 1 1 200px;
  }
</style>
