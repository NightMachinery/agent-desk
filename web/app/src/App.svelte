<script>
  import LazyPanel from './components/LazyPanel.svelte';
  import { route, go, pageOpen } from './lib/route.svelte.js';
  import { get, watch } from './lib/api.js';
  import { sawBuild } from './lib/build.svelte.js';
  import { ui, loadSharedUI } from './lib/ui.svelte.js';
  import { hasDraft, onDraftChange, onItemDraftDropped } from './lib/drafts.js';
  import { act, change, onTaken, patchedPage, shown, ticket } from './lib/pending.svelte.js';
  import { startOutbox } from './lib/outbox.svelte.js';
  import { toasts } from './lib/toast.svelte.js';
  import { desk } from './lib/desk.svelte.js';
  import { untrack } from 'svelte';
  import { hold, lingerIds, moved, onLingerEnd } from './lib/linger.svelte.js';
  import { arrivalHooks, cancelArrival, routeMoved } from './lib/arrival.js';
  import { post } from './lib/api.js';
  import { keys, ctx, pane, onkeydown, loadUserKeys } from './lib/keys.svelte.js';
  import './lib/keymap.js';
  import Keys from './components/Keys.svelte';
  import ImageViewer from './components/ImageViewer.svelte';
  import Tooltip from './components/Tooltip.svelte';
  import Sidebar from './components/Sidebar.svelte';
  import ListPane from './components/ListPane.svelte';
  import ColdCacheDialog from './components/ColdCacheDialog.svelte';
  import ErrorSheet from './components/ErrorSheet.svelte';
  import { watchCache } from './lib/cache.svelte.js';
  import Settings from './components/Settings.svelte';
  import SetEditor from './components/SetEditor.svelte';
  import { setsUi } from './lib/sets.svelte.js';
  import { layout, screen, shows, columns, setHidden, setSide, setListWidth, saveLayout, HIDE_AT, SIDE_DEFAULT, SPLIT_DEFAULT } from './lib/layout.svelte.js';
  import { selecting } from './lib/select.svelte.js';
  import { tip } from './lib/shortcuts.js';

  let showRecordings = $state(false);
  // API responses are replaced as a unit, so rows can read plain objects.
  let view = $state.raw(null);
  let globalView = $state.raw(null);
  let error = $state('');
  let item = $state.raw(null);
  // The item page shows the user's changes the desk has not shown yet.
  const shownItem = $derived(patchedPage(item));
  let itemError = $state('');
  let live = $state('connecting');
  let drawer = $state(false);
  let seq = 0;
  let itemSeq = 0;

  async function loadView() {
    const my = ++seq;
    const params = { path: route.path, session: route.session, host: route.host, all: route.all ? '1' : '', show: route.hour ? 'hour' : '', q: route.q, deliverables: ui.deliverablesShow };
    // Rows just read stay listed for a while; reading the list of them must
    // not make every hold refetch the view.
    const asked = untrack(lingerIds);
    params.linger = asked.join(',');
    const t = ticket();
    try {
      const scoped = get('view', params);
      const filtered = params.path || params.session || params.all || params.q || params.linger;
      const global = filtered ? get('view', { host: params.host }) : scoped;
      const [v, g] = await Promise.all([scoped, global]);
      if (my !== seq) return;
      // A row held after this was asked for may be read already and left
      // out: ask again rather than drop it from the list for a moment.
      if (untrack(lingerIds).some((id) => !asked.includes(id))) return void loadView();
      shown('view', t);
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
    const t = ticket();
    try {
      const it = await get('item', { id });
      if (my !== itemSeq) return;
      shown('item', t);
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
    route.path, route.session, route.host, route.all, route.hour, route.q, ui.deliverablesShow;
    loadView();
  });
  let prefetchScheduled = false;
  $effect(() => {
    if (prefetchScheduled || !view || route.item || pageOpen()) return;
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
  // Leaving an item the page moved to by itself reads it (arrival.js).
  arrivalHooks({ poster: (id) => act([id], { unread: false }, [], () => post('read', { target: id })), onRead: () => refresh(), kept: hasDraft, hold });
  // A box emptied here (sent, discarded, cleared): its row stops saying
  // Draft at once, and stays so until the desk's answers agree.
  onItemDraftDropped((display, taken) => {
    const c = change([display], { draft: false });
    taken.then((ok) => (ok ? c.done() : c.failed()));
  });
  // A draft started or dropped changes what the list keeps (an item with a
  // draft stays listed): look again once it has reached the desk.
  $effect(() => onDraftChange(() => setTimeout(() => loadView(), 1600)));
  let lastItem = route.item;
  $effect(() => {
    const cur = route.item;
    untrack(() => {
      // Moving in visual mode selects; it is not moving on, so it reads nothing.
      if (keys.anchor) cancelArrival(lastItem);
      else routeMoved(lastItem, cur);
      lastItem = cur;
    });
  });
  onLingerEnd(() => loadView());

  $effect(() => untrack(() => startOutbox(refresh)));
  $effect(() => watchCache());

  let timer;
  // The desk took a change: look again soon, so the page stops laying it
  // over what the desk says (pending.svelte.js).
  onTaken(() => {
    clearTimeout(timer);
    timer = setTimeout(refresh, 120);
  });
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
      sawBuild,
      // The user's keys file: read on connecting, and again when it changes
      // (announced, so a saved edit is seen to apply).
      (stamp) => {
        if (stamp === keysStamp) return;
        loadUserKeys(keysStamp !== undefined);
        keysStamp = stamp;
      },
    ),
  );
  let keysStamp;

  $effect(() => untrack(() => void loadSharedUI()));
  $effect(() => {
    const onVisible = () => document.visibilityState === 'visible' && (refresh(), loadSharedUI());
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
  ctx.item = () => shownItem;
  ctx.view = () => view;
  ctx.runs = () => globalView?.view?.runs ?? [];
  // Pane focus shows once the keyboard has driven the page.
  const focused = $derived.by(() => {
    route.item;
    route.settings;
    route.verbatim;
    route.tasks;
    keys.pane;
    keys.mode;
    keys.tick;
    return keys.used ? pane() : '';
  });

  // Wide screens: the panes' layout (layout.svelte.js).
  const full = $derived(screen.wide && layout.full);
  // Dragging a grip: which one, and whether letting go now hides its pane.
  let dragging = $state('');
  let hiding = $state('');
  /** Drags the grip right of the sidebar (which = 'side') or of the list
   * ('list'); closer than HIDE_AT to the pane's left edge hides it. */
  function drag(e, which) {
    if (e.button !== 0) return;
    e.preventDefault();
    const grip = e.currentTarget;
    const shell = grip.parentElement;
    grip.setPointerCapture(e.pointerId);
    const list = shell.querySelector('.list').getBoundingClientRect();
    const right = shell.querySelector('.detail').getBoundingClientRect().right;
    const left = which === 'side' ? shell.getBoundingClientRect().left : list.left;
    // A pane dragged out of sight comes back at the width it had.
    const before = { side: layout.side, split: layout.split };
    dragging = which;
    const move = (ev) => {
      const x = ev.clientX - left;
      hiding = x < HIDE_AT[which] ? which : '';
      if (which === 'side') setSide(x);
      else setListWidth(x, right - left);
    };
    const end = () => {
      grip.removeEventListener('pointermove', move);
      grip.removeEventListener('pointerup', end);
      grip.removeEventListener('pointercancel', end);
      if (hiding) {
        Object.assign(layout, before);
        setHidden(hiding, true);
      } else saveLayout();
      dragging = hiding = '';
    };
    grip.addEventListener('pointermove', move);
    grip.addEventListener('pointerup', end);
    grip.addEventListener('pointercancel', end);
  }
  function resetWidth(which) {
    if (which === 'side') setSide(SIDE_DEFAULT);
    else layout.split = SPLIT_DEFAULT;
    saveLayout();
  }
  // In fullscreen a click that opens something shows the pane it opens in,
  // as on a phone: from the sidebar or the list, an item or a page opens in
  // the detail, a place in the list.
  $effect(() => {
    const follow = (e) => {
      if (!full || e.button !== 0 || e.shiftKey || e.metaKey || e.ctrlKey || e.altKey || selecting()) return;
      const t = e.target instanceof Element ? e.target : null;
      const from = t?.closest('[data-pane]')?.getAttribute('data-pane');
      const a = t?.closest('a[href^="#"]');
      if (!a || (from !== 'side' && from !== 'list')) return;
      const q = new URLSearchParams(a.getAttribute('href').split('?')[1] ?? '');
      keys.pane = ['item', 'settings', 'verbatim', 'tasks'].some((k) => q.has(k)) ? 'detail' : 'list';
    };
    document.addEventListener('click', follow);
    return () => document.removeEventListener('click', follow);
  });
</script>

<ColdCacheDialog />
<ErrorSheet />

<svelte:window {onkeydown} />

<div class="shell" class:has-item={!!route.item || pageOpen()} class:full class:dragging={!!dragging} style:--cols={columns()}>
  {#if layout.hidden.side}
    <button class="rail rail-side" data-cmd="toggle-sidebar" aria-label="Show the sidebar" title={tip('Show the sidebar', 'toggle-sidebar')} onclick={() => setHidden('side', false)}><span>Sidebar</span></button>
  {/if}
  <div class="side" class:open={drawer} class:gone={!shows('side')} data-pane="side" class:focused={focused === 'side'}>
    <Sidebar view={globalView} {live} onchange={refresh} />
  </div>
  {#if !layout.hidden.side}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="grip grip-side" class:active={dragging === 'side'} class:hiding={hiding === 'side'} aria-hidden="true"
      title="Drag to resize the sidebar, to the edge to hide it; double-click for its default width"
      onpointerdown={(e) => drag(e, 'side')} ondblclick={() => resetWidth('side')}></div>
  {/if}
  {#if drawer}
    <button class="scrim" aria-label="Close menu" onclick={() => (drawer = false)}></button>
  {/if}

  {#if layout.hidden.list}
    <button class="rail rail-list" data-cmd="toggle-list" aria-label="Show the list" title={tip('Show the list', 'toggle-list')} onclick={() => setHidden('list', false)}><span>List</span></button>
  {/if}
  <main class="list" class:gone={!shows('list')} data-pane="list" class:focused={focused === 'list'}>
    <ListPane {view} {error} onmenu={() => (drawer = true)} onchange={refresh} />
  </main>
  {#if !layout.hidden.list}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="grip grip-list" class:active={dragging === 'list'} class:hiding={hiding === 'list'} aria-hidden="true"
      title="Drag to resize the list and the detail, to the edge to hide the list; double-click for the default split"
      onpointerdown={(e) => drag(e, 'list')} ondblclick={() => resetWidth('list')}></div>
  {/if}

  <aside class="detail" class:gone={!shows('detail')} data-pane="detail" class:focused={focused === 'detail'}>
    {#if route.item}
      <LazyPanel view={import('./components/Detail.svelte')} item={shownItem} error={itemError} onchange={refresh} />
    {:else if route.settings}
      <section class="settings-page">
        <div class="settings-bar">
          <button class="btn ghost small" onclick={() => go({ settings: false })}>← Back</button>
        </div>
        <h1>Settings</h1>
        <p class="faint lede">How a message reaches a busy agent, and where you are notified. An override set here applies to everything under it, unless something nearer says otherwise.</p>
        <Settings path={route.path} stamp={view?.stamp} onchange={refresh} />
        <details id="recordings" ontoggle={(event) => (showRecordings = event.currentTarget.open)}>
          <summary>Recordings</summary>
          {#if showRecordings}<LazyPanel view={import('./components/Recordings.svelte')} />{/if}
        </details>
      </section>
    {:else if route.verbatim}
      <LazyPanel view={import('./components/Verbatim.svelte')} path={route.path} stamp={view?.stamp} />
    {:else if route.tasks}
      <LazyPanel view={import('./components/Tasks.svelte')} path={route.path} stamp={view?.stamp} runs={globalView?.view?.runs ?? []} />
    {:else if view?.node}
      <LazyPanel view={import('./components/NodePanel.svelte')} node={view.node} target={view.target} conversations={view.conversations ?? []} stamp={view.stamp} onchange={refresh} />
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
<ImageViewer />
<Tooltip />

<SetEditor sets={globalView?.sets ?? []} paths={globalView?.paths ?? []} onchange={refresh} />

<div class="toasts" aria-live="polite">
  {#each toasts as t (t.id)}
    <!-- A repeat of a keyed toast pulses it (bump) instead of stacking a copy. -->
    {#key t.bump}
      <div class="toast {t.kind}" class:bumped={t.bump > 0}>
        <span>{t.text}</span>
        {#if t.action}
          <button class="toast-act" onclick={() => { const i = toasts.findIndex((x) => x.id === t.id); if (i >= 0) toasts.splice(i, 1); t.action.run(); }}>{t.action.label}</button>
        {/if}
      </div>
    {/key}
  {/each}
</div>

<style>
  .shell {
    display: grid;
    grid-template-columns: var(--cols);
    height: calc(100dvh - var(--modeline-h));
  }
  /* The wide layout: sidebar, grip, list, grip, detail (layout.svelte.js's
     columns()); a hidden pane leaves a rail, and fullscreen shows only the
     focused pane. */
  @media (min-width: 901px) {
    .shell > * {
      grid-row: 1;
    }
    .side,
    .rail-side {
      grid-column: 1;
    }
    .grip-side {
      grid-column: 2;
    }
    .list,
    .rail-list {
      grid-column: 3;
    }
    .grip-list {
      grid-column: 4;
    }
    .detail {
      grid-column: 5;
    }
    .shell.full > * {
      grid-column: 1;
    }
    .gone,
    .shell.full :is(.rail, .grip) {
      display: none;
    }
    .shell.full .detail {
      border-left: 0;
    }
  }
  .grip {
    position: relative;
    z-index: 6;
    width: 9px;
    margin-left: -4px;
    cursor: col-resize;
    touch-action: none;
  }
  .grip::after {
    content: '';
    position: absolute;
    inset: 0 3px;
    background: var(--accent);
    opacity: 0;
    transition: opacity 0.12s;
  }
  .grip:hover::after,
  .grip.active::after {
    opacity: 0.55;
  }
  .grip.hiding::after {
    background: var(--blocked);
    opacity: 0.8;
  }
  .shell.dragging {
    cursor: col-resize;
    user-select: none;
  }
  .rail {
    display: flex;
    justify-content: center;
    padding: 12px 0;
    border: 0;
    border-right: 1px solid var(--line);
    background: var(--panel);
    color: var(--faint);
    font: inherit;
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    cursor: pointer;
  }
  .rail span {
    writing-mode: vertical-rl;
  }
  .rail:hover {
    background: var(--sunken);
    color: var(--text);
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
  .toast.bumped {
    animation: bump 0.35s ease-out;
  }
  @keyframes bump {
    40% {
      transform: scale(1.06);
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .toast.bumped {
      animation: none;
    }
  }
  @keyframes rise {
    from {
      transform: translateY(6px);
      opacity: 0;
    }
  }

  /* Phone and narrow windows: one column; the item opens over the list. */
  @media (max-width: 900px) {
    .shell {
      grid-template-columns: 1fr;
    }
    .rail,
    .grip {
      display: none;
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
