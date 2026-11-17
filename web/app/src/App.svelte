<script>
  import { route, go } from './lib/route.svelte.js';
  import { get, watch } from './lib/api.js';
  import { toasts } from './lib/toast.svelte.js';
  import { desk } from './lib/desk.svelte.js';
  import { sel, toggle, clear } from './lib/select.svelte.js';
  import { step } from './lib/nav.svelte.js';
  import Sidebar from './components/Sidebar.svelte';
  import ListPane from './components/ListPane.svelte';
  import Detail from './components/Detail.svelte';
  import NodePanel from './components/NodePanel.svelte';

  let view = $state(null);
  let globalView = $state(null);
  let error = $state('');
  let item = $state(null);
  let itemError = $state('');
  let live = $state('connecting');
  let drawer = $state(false);
  let seq = 0;
  let itemSeq = 0;

  async function loadView() {
    const my = ++seq;
    const params = { path: route.path, session: route.session, host: route.host, all: route.all ? '1' : '', q: route.q };
    try {
      const [v, g] = await Promise.all([get('view', params), get('view', { host: route.host })]);
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
  $effect(() => {
    route.item;
    loadItem();
  });

  let timer;
  $effect(() =>
    watch(
      () => {
        clearTimeout(timer);
        timer = setTimeout(refresh, 120);
      },
      (s) => (live = s),
    ),
  );

  $effect(() => {
    const onVisible = () => document.visibilityState === 'visible' && refresh();
    document.addEventListener('visibilitychange', onVisible);
    return () => document.removeEventListener('visibilitychange', onVisible);
  });

  // Close the drawer whenever the view changes.
  $effect(() => {
    route.path, route.session, route.host;
    drawer = false;
  });

  function onkey(e) {
    if (e.target.closest('input, textarea, select') || e.metaKey || e.ctrlKey || e.altKey) return;
    if (e.key === '/') {
      e.preventDefault();
      document.getElementById('search')?.focus();
    } else if (e.key === 'Escape' && (sel.ids.size || sel.mode)) {
      clear();
    } else if (e.key === 'Escape' && route.item) {
      go({ item: '' });
    } else if ((e.key === 'x' || e.key === 'X') && route.item && document.querySelector(`[data-item="${CSS.escape(route.item)}"]`)) {
      toggle(route.item, e.shiftKey);
    } else if (e.key === 'j' || e.key === 'k') {
      const id = step(e.key === 'j' ? 1 : -1, route.item);
      if (!id) return;
      go({ item: id }, { replace: true });
      document.querySelector(`[data-item="${CSS.escape(id)}"]`)?.scrollIntoView({ block: 'nearest' });
    } else if (e.key === 'r' || e.key === 'a') {
      const el = document.getElementById(e.key === 'a' ? 'answer-box' : 'reply-box');
      if (el) {
        e.preventDefault();
        el.focus();
      }
    }
  }
</script>

<svelte:window onkeydown={onkey} />

<div class="shell" class:has-item={!!route.item}>
  <div class="side" class:open={drawer}>
    <Sidebar view={globalView} {live} />
  </div>
  {#if drawer}
    <button class="scrim" aria-label="Close menu" onclick={() => (drawer = false)}></button>
  {/if}

  <main class="list">
    <ListPane {view} {error} onmenu={() => (drawer = true)} onchange={refresh} />
  </main>

  <aside class="detail">
    {#if route.item}
      <Detail {item} error={itemError} onchange={refresh} />
    {:else if view?.node}
      <NodePanel node={view.node} target={view.target} onchange={refresh} />
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
        <p class="faint keys"><kbd>j</kbd> <kbd>k</kbd> move · <kbd>/</kbd> search · <kbd>a</kbd> answer · <kbd>r</kbd> reply · <kbd>x</kbd> select · <kbd>esc</kbd> close</p>
      </div>
    {/if}
  </aside>
</div>

<div class="toasts" aria-live="polite">
  {#each toasts as t (t.id)}
    <div class="toast {t.kind}">{t.text}</div>
  {/each}
</div>

<style>
  .shell {
    display: grid;
    grid-template-columns: 248px minmax(380px, 1fr) minmax(420px, 1.15fr);
    height: 100dvh;
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
    bottom: max(16px, env(safe-area-inset-bottom));
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
