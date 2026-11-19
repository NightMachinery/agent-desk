<script>
  import LazyPanel from './components/LazyPanel.svelte';
  import { route, go } from './lib/route.svelte.js';
  import { get, watch } from './lib/api.js';
  import { startOutbox } from './lib/outbox.svelte.js';
  import { toasts } from './lib/toast.svelte.js';
  import { desk } from './lib/desk.svelte.js';
  import { untrack } from 'svelte';
  import { lingerIds, moved, onLingerEnd } from './lib/linger.svelte.js';
  import { keys, ctx, pane, onkeydown } from './lib/keys.svelte.js';
  import './lib/keymap.js';
  import Keys from './components/Keys.svelte';
  import Sidebar from './components/Sidebar.svelte';
  import ListPane from './components/ListPane.svelte';
  import ColdCacheDialog from './components/ColdCacheDialog.svelte';
  import { watchCache } from './lib/cache.svelte.js';
  import Settings from './components/Settings.svelte';
  import SetEditor from './components/SetEditor.svelte';
  import { setsUi } from './lib/sets.svelte.js';

  let showRecordings = $state(false);
  // API responses are replaced as a unit, so rows can read plain objects.
  let view = $state.raw(null);
  let globalView = $state.raw(null);
  let error = $state('');
  let item = $state.raw(null);
  let itemError = $state('');
  let live = $state('connecting');
  let drawer = $state(false);
  let seq = 0;
  let itemSeq = 0;

  async function loadView() {
    const my = ++seq;
    const params = { path: route.path, session: route.session, host: route.host, all: route.all ? '1' : '', q: route.q };
    // Rows just read stay listed for a while; reading the list of them must
    // not make every hold refetch the view.
    params.linger = untrack(lingerIds).join(',');
    try {
      const scoped = get('view', params);
      const filtered = params.path || params.session || params.all || params.q || params.linger;
      const global = filtered ? get('view', { host: params.host }) : scoped;
      const [v, g] = await Promise.all([scoped, global]);
      if (my !== seq) return;
      view = v;
      globalView = g;
      desk.host = v.host;
      error = '';
    } catch (e) {
      if (my === seq) error = e.message;
    }
  }

  async function loadItem() {
    const id = route.item;
    const my = ++itemSeq;
    if (!id) {
      item = null;
      return;
    }
    try {
      const it = await get('item', { id });
      if (my !== itemSeq) return;
      item = it;
      itemError = '';
    } catch (e) {
      if (my === itemSeq) {
        item = null;
        itemError = e.message;
      }
    }
  }

  function refresh() {
    loadView();
    loadItem();
  }

  $effect(() => {
    route.path, route.session, route.host, route.all, route.q;
    loadView();
  });
  let prefetchScheduled = false;
  $effect(() => {
    if (prefetchScheduled || !view || route.item || route.settings) return;
    prefetchScheduled = true;
    const prefetchPanels = () => {
      Promise.all([
        import('./components/Detail.svelte'),
        import('./components/NodePanel.svelte'),
      ]).catch(() => {});
    };
    // Leave a frame for the populated inbox to paint before scheduling any downloads.
    requestAnimationFrame(() => requestAnimationFrame(() => {
      if (window.requestIdleCallback) window.requestIdleCallback(prefetchPanels);
      else window.setTimeout(prefetchPanels, 0);
    }));
  });
  $effect(() => {
    route.item;
    loadItem();
  });
  $effect(() => {
    const cur = route.item;
    untrack(() => moved(cur));
  });
  onLingerEnd(() => loadView());

  $effect(() => untrack(() => startOutbox(refresh)));
  $effect(() => watchCache());

  let timer;
  $effect(() =>
    watch(
      () => {
        clearTimeout(timer);
        timer = setTimeout(refresh, 120);
      },
      (s) => {
        if (live === 'reconnecting' && s === 'live') refresh();
        live = s;
      },
    ),
  );

  $effect(() => {
    const onVisible = () => document.visibilityState === 'visible' && refresh();
    document.addEventListener('visibilitychange', onVisible);
    return () => document.removeEventListener('visibilitychange', onVisible);
  });

  // Close the drawer whenever the view changes, or the set editor opens
  // over it.
  $effect(() => {
    route.path, route.session, route.host, setsUi.open;
    drawer = false;
  });

  ctx.refresh = refresh;
  ctx.item = () => item;
  ctx.view = () => view;
  ctx.runs = () => globalView?.view?.runs ?? [];
  // Pane focus shows once the keyboard has driven the page.
  const focused = $derived.by(() => {
    route.item;
    route.settings;
    keys.pane;
    keys.mode;
    keys.tick;
    return keys.used ? pane() : '';
  });
</script>

<ColdCacheDialog />

<svelte:window {onkeydown} />

<div class="shell" class:has-item={!!route.item || route.settings}>
  <div class="side" class:open={drawer} data-pane="side" class:focused={focused === 'side'}>
    <Sidebar view={globalView} {live} onchange={refresh} />
  </div>
  {#if drawer}
    <button class="scrim" aria-label="Close menu" onclick={() => (drawer = false)}></button>
  {/if}

  <main class="list" data-pane="list" class:focused={focused === 'list'}>
    <ListPane {view} {error} onmenu={() => (drawer = true)} onchange={refresh} />
  </main>

  <aside class="detail" data-pane="detail" class:focused={focused === 'detail'}>
    {#if route.item}
      <LazyPanel view={import('./components/Detail.svelte')} {item} error={itemError} onchange={refresh} />
    {:else if route.settings}
      <section class="settings-page">
        <div class="settings-bar">
          <button class="btn ghost small" onclick={() => go({ settings: false })}>← Back</button>
        </div>
        <h1>Settings</h1>
        <p class="faint lede">How a message reaches a busy agent, and where you are notified. An override set here applies to everything under it, unless something nearer says otherwise.</p>
        <Settings path={route.path} stamp={view?.stamp} onchange={refresh} />
        <details ontoggle={(event) => (showRecordings = event.currentTarget.open)}>
          <summary>Recordings</summary>
          {#if showRecordings}<LazyPanel view={import('./components/Recordings.svelte')} />{/if}
        </details>
      </section>
    {:else if view?.node}
      <LazyPanel view={import('./components/NodePanel.svelte')} node={view.node} target={view.target} conversation={view.conversation} stamp={view.stamp} onchange={refresh} />
    {:else}
      <div class="placeholder">
        <svg viewBox="0 0 32 32" width="40" height="40" aria-hidden="true"
          ><path
            d="M7 17.5 9.6 9.4A2 2 0 0 1 11.5 8h9a2 2 0 0 1 1.9 1.4l2.6 8.1V22a2 2 0 0 1-2 2H9a2 2 0 0 1-2-2z M7.5 17.5h5l1.2 2.5h4.6l1.2-2.5h5"
            fill="none"
            stroke="currentColor"
            stroke-width="1.6"
            stroke-linejoin="round"
          /></svg
        >
        <p>Pick a question or a notice to read it, answer it, or message its agent.</p>
        <p class="faint keys"><kbd>j</kbd> <kbd>k</kbd> move · <kbd>/</kbd> search · <kbd>a</kbd> answer · <kbd>r</kbd> message · <kbd>f</kbd> click anything · <kbd>SPC</kbd> leader · <kbd>?</kbd> every key</p>
      </div>
    {/if}
  </aside>
</div>

<Keys />

<SetEditor sets={globalView?.sets ?? []} paths={globalView?.paths ?? []} onchange={refresh} />

<div class="toasts" aria-live="polite">
  {#each toasts as t (t.id)}
    <div class="toast {t.kind}">
      <span>{t.text}</span>
      {#if t.action}
        <button class="toast-act" onclick={() => { const i = toasts.findIndex((x) => x.id === t.id); if (i >= 0) toasts.splice(i, 1); t.action.run(); }}>{t.action.label}</button>
      {/if}
    </div>
  {/each}
</div>

<style>
  .shell {
    display: grid;
    grid-template-columns: 248px minmax(380px, 1fr) minmax(420px, 1.15fr);
    height: calc(100dvh - var(--modeline-h));
  }
  /* The pane keys act on, once the keyboard is in use. */
  .focused {
    box-shadow: inset 0 2px 0 var(--accent);
  }
  .side {
    border-right: 1px solid var(--line);
    background: var(--panel);
    overflow: auto;
  }
  .list {
    overflow: auto;
    min-width: 0;
  }
  .detail {
    border-left: 1px solid var(--line);
    background: var(--panel);
    overflow: auto;
    min-width: 0;
  }
  .scrim {
    display: none;
  }
  .settings-page {
    padding: max(12px, env(safe-area-inset-top)) 24px 40px;
    max-width: 640px;
  }
  .settings-page h1 {
    margin: 8px 0 4px;
    font-size: 21px;
    letter-spacing: -0.015em;
  }
  .lede {
    margin: 0 0 16px;
    font-size: 13px;
  }
  .placeholder {
    height: 100%;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 4px;
    padding: 40px;
    color: var(--muted);
    text-align: center;
  }
  .placeholder p {
    margin: 6px 0 0;
    max-width: 420px;
  }
  .keys {
    font-size: 12px;
  }
  kbd {
    font-family: var(--mono);
    font-size: 11px;
    padding: 1px 5px;
    border-radius: 4px;
    border: 1px solid var(--line-strong);
    background: var(--sunken);
  }
  .toasts {
    position: fixed;
    bottom: calc(max(16px, env(safe-area-inset-bottom)) + var(--modeline-h));
    left: 50%;
    transform: translateX(-50%);
    display: flex;
    flex-direction: column;
    gap: 8px;
    z-index: 50;
    width: min(520px, calc(100vw - 24px));
  }
  .toast {
    padding: 10px 14px;
    border-radius: 10px;
    background: var(--text);
    color: var(--panel);
    box-shadow: var(--shadow);
    font-size: 13px;
    white-space: pre-wrap;
    animation: rise 0.18s ease-out;
  }
  .toast {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .toast > span {
    flex: 1;
  }
  .toast-act {
    flex: none;
    border: 1px solid currentColor;
    border-radius: 7px;
    background: transparent;
    color: inherit;
    font: inherit;
    font-weight: 600;
    padding: 3px 10px;
    cursor: pointer;
  }
  .toast.error {
    background: var(--blocked);
    color: #fff;
  }
  .toast.ok {
    background: var(--ok);
    color: #fff;
  }
  @keyframes rise {
    from {
      transform: translateY(6px);
      opacity: 0;
    }
  }

  @media (max-width: 1180px) {
    .shell {
      grid-template-columns: 220px minmax(320px, 1fr) minmax(380px, 1fr);
    }
  }

  /* Phone and narrow windows: one column; the item opens over the list. */
  @media (max-width: 900px) {
    .shell {
      grid-template-columns: 1fr;
    }
    .side {
      position: fixed;
      inset: 0 auto 0 0;
      width: min(300px, 84vw);
      z-index: 30;
      transform: translateX(-100%);
      transition: transform 0.2s ease;
      box-shadow: var(--shadow);
    }
    .side.open {
      transform: none;
    }
    .scrim {
      display: block;
      position: fixed;
      inset: 0;
      z-index: 25;
      border: 0;
      background: #00000055;
    }
    .detail {
      display: none;
    }
    .shell.has-item .detail {
      display: block;
      position: fixed;
      inset: 0;
      z-index: 20;
      border: 0;
      animation: slide 0.18s ease-out;
    }
    .placeholder {
      display: none;
    }
  }
  @keyframes slide {
    from {
      transform: translateX(32px);
    }
  }
</style>
