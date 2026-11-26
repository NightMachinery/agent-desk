<script>
  import { onMount, tick } from 'svelte';
  import { parseTip, withKey, ownLabel } from '../lib/tooltip.js';
  import { buttonKey } from '../lib/shortcuts.js';

  // The app's one tooltip card, in place of the browser's: lib/tooltip.js
  // says how a title splits into it. Any element with title, data-tip or
  // data-tip-img (an image to preview) gets one on hover, and a button or
  // link also on keyboard focus. So does a control a command clicks
  // (data-cmd): its card names the command's key, even with no title. The
  // title moves to data-tip the first time the element is reached, so the
  // browser never shows its own.
  //
  // Touch has no hover. There, holding an element shows its card, and so
  // does tapping a disabled control, which would otherwise do nothing; the
  // card then stays until the next touch. A link held keeps the phone's own
  // menu (open in a new tab, copy the link), so holding shows no card on it.

  const TITLED = '[title], [data-tip], [data-tip-img]';
  const SELECTOR = TITLED + ', [data-cmd]';
  const FOCUSABLE = 'button, a[href], summary, [role="button"]';
  const HOLD = 500; // ms: a touch held this long shows the card
  const SLOP = 10; // px: a touch moving further is a scroll, not a hold

  let el = /** @type {HTMLElement | null} */ (null); // the element shown
  let cand = /** @type {HTMLElement | null} */ (null); // the one waiting for its delay
  let card = $state(/** @type {HTMLElement | null} */ (null));
  let shown = $state(false);
  let placed = $state(false);
  let x = $state(0);
  let y = $state(0);
  let text = $state('');
  let what = $state('');
  let img = $state('');
  let disabled = $state(false);
  let keyOnly = false; // a card that only names a key: touch shows no keys, so not there
  let byPointer = true;
  let byTouch = $state(false); // shown by a touch: no key caps, and above the finger
  let pointerX = 0;
  /** @type {{t: HTMLElement, x: number, y: number, timer: number, held: boolean} | null} */
  let press = null; // the touch held on an element with a card
  let swallow = /** @type {HTMLElement | null} */ (null); // its click, which the hold used up
  let swallowUntil = 0;
  let timer = 0;
  let warmUntil = 0;

  // A disabled control says what it does, then why it cannot now.
  const parts = $derived(parseTip(disabled && what ? what : text));
  const reason = $derived(disabled && what ? text : '');

  /** Moves el's title aside, keeping it for screen readers. */
  function adopt(t) {
    const title = t.getAttribute('title');
    if (title == null) return;
    t.dataset.tip = title;
    t.removeAttribute('title');
    if (t.dataset.tipAria === 'label' || (!t.getAttribute('aria-label') && !t.textContent?.trim())) {
      t.setAttribute('aria-label', title);
      t.dataset.tipAria = 'label';
    } else t.setAttribute('aria-description', title);
  }

  const isDisabled = (t) => t.matches(':disabled') || t.getAttribute('aria-disabled') === 'true';

  function read(t) {
    text = t.dataset.tip ?? '';
    what = t.dataset.tipWhat ?? '';
    img = t.dataset.tipImg ?? '';
    disabled = isDisabled(t);
    keyOnly = false;
    const key = t.dataset.cmd ? buttonKey(t.dataset.cmd) : '';
    if (!key) return;
    const label = () => ownLabel(t.getAttribute('aria-label') ?? '', t.textContent ?? '', t.dataset.cmd ?? '');
    if (disabled && what) what = withKey(what, key, label());
    else {
      keyOnly = !text;
      text = withKey(text, key, label());
    }
  }

  function schedule(t, delay) {
    clearTimeout(timer);
    cand = t;
    timer = setTimeout(() => show(t), delay);
  }

  async function show(t) {
    cand = null;
    if (!t.isConnected) return hide();
    el = t;
    read(t);
    if ((byTouch && keyOnly) || (!parts.head && !parts.body && !reason && !img)) return hide();
    placed = false;
    shown = true;
    await tick();
    place();
  }

  function place() {
    if (!el || !card) return;
    const r = el.getBoundingClientRect();
    const w = card.offsetWidth;
    const h = card.offsetHeight;
    const cx = byPointer && r.width > 320 ? pointerX : r.left + r.width / 2;
    x = Math.round(Math.max(8, Math.min(innerWidth - w - 8, cx - w / 2)));
    const below = Math.min(r.bottom + 8, innerHeight - h - 8);
    const above = r.top - 8 - h;
    // A finger covers what is under it, so on touch the card goes above.
    const up = above >= 8 && (byTouch || r.bottom + 8 + h > innerHeight - 8);
    y = Math.round(up ? above : below);
    placed = true;
  }

  function hide() {
    clearTimeout(timer);
    cand = null;
    if (shown) warmUntil = Date.now() + 500;
    shown = false;
    el = null;
  }

  function over(e) {
    if (e.pointerType === 'touch') return;
    const t = /** @type {Element} */ (e.target).closest?.(SELECTOR);
    if (!(t instanceof HTMLElement) || t === el || t === cand) return;
    adopt(t);
    byPointer = true;
    byTouch = false;
    pointerX = e.clientX;
    // Sweeping along a toolbar: the next card follows at once.
    schedule(t, shown || Date.now() < warmUntil ? 40 : 450);
  }

  function out(e) {
    if (e.pointerType === 'touch') return; // a finger lifting: the card stays
    const cur = el ?? cand;
    if (!cur) return;
    const to = e.relatedTarget;
    if (to instanceof Node && cur.contains(to)) return;
    hide();
  }

  function focusin(e) {
    const t = e.target;
    if (!(t instanceof HTMLElement) || !t.matches(SELECTOR) || !t.matches(FOCUSABLE) || !t.matches(':focus-visible')) return;
    adopt(t);
    byPointer = false;
    byTouch = false;
    schedule(t, 600);
  }

  function focusout(e) {
    if (e.target === el || e.target === cand) hide();
  }

  function down(e) {
    hide();
    endPress();
    swallow = null;
    if (e.pointerType !== 'touch' || !e.isPrimary) return;
    const t = /** @type {Element} */ (e.target).closest?.(TITLED);
    if (!(t instanceof HTMLElement)) return;
    adopt(t);
    press = { t, x: e.clientX, y: e.clientY, held: false, timer: 0 };
    if (!t.matches('a[href]')) press.timer = setTimeout(hold, HOLD);
  }

  function hold() {
    if (!press || press.held) return;
    clearTimeout(press.timer);
    press.held = true;
    showByTouch(press);
    if (navigator.userActivation?.hasBeenActive) navigator.vibrate?.(8);
  }

  function showByTouch(p) {
    byPointer = true;
    byTouch = true;
    pointerX = p.x;
    clearTimeout(timer);
    show(p.t);
  }

  function move(e) {
    if (press && !press.held && e.isPrimary && Math.hypot(e.clientX - press.x, e.clientY - press.y) > SLOP) endPress();
  }

  function up(e) {
    if (!press || !e.isPrimary) return;
    const p = press;
    endPress();
    if (p.held) {
      // The browser may still send the tap's click: it is not one.
      swallow = p.t;
      swallowUntil = Date.now() + 800;
    } else if (isDisabled(p.t)) showByTouch(p);
  }

  function endPress() {
    if (press) clearTimeout(press.timer);
    press = null;
  }

  function click(e) {
    const s = swallow;
    swallow = null;
    if (s && Date.now() < swallowUntil && e.target instanceof Node && s.contains(e.target)) {
      e.preventDefault();
      e.stopImmediatePropagation();
    }
  }

  // Android's long press arrives as contextmenu: it shows the card, not the
  // menu. A link's press has no timer, so its menu stays.
  function contextmenu(e) {
    if (!press || !press.timer) return;
    e.preventDefault();
    hold();
  }

  onMount(() => {
    const opts = { capture: true, passive: true };
    const quiet = () => hide();
    window.addEventListener('pointerdown', down, opts);
    window.addEventListener('pointermove', move, opts);
    window.addEventListener('pointerup', up, opts);
    window.addEventListener('pointercancel', endPress, opts);
    window.addEventListener('click', click, true);
    window.addEventListener('contextmenu', contextmenu, true);
    window.addEventListener('keydown', quiet, opts);
    window.addEventListener('wheel', quiet, opts);
    document.addEventListener('scroll', quiet, opts);
    window.addEventListener('blur', quiet);
    window.addEventListener('resize', quiet);
    // Svelte sets a title again when its value changes: move it aside again,
    // and keep the card in step with the element it shows.
    const mo = new MutationObserver((list) => {
      for (const m of list) {
        const t = m.target;
        if (m.type === 'attributes' && t instanceof HTMLElement) {
          if (m.attributeName === 'title' && t.hasAttribute('title') && 'tip' in t.dataset) adopt(t);
          if (t === el) {
            read(t);
            tick().then(place);
          }
        }
      }
      if (el && !el.isConnected) hide();
    });
    mo.observe(document.body, { attributes: true, attributeFilter: ['title', 'disabled', 'data-tip-what', 'data-tip-img'], subtree: true, childList: true });
    return () => {
      window.removeEventListener('pointerdown', down, opts);
      window.removeEventListener('pointermove', move, opts);
      window.removeEventListener('pointerup', up, opts);
      window.removeEventListener('pointercancel', endPress, opts);
      window.removeEventListener('click', click, true);
      window.removeEventListener('contextmenu', contextmenu, true);
      window.removeEventListener('keydown', quiet, opts);
      window.removeEventListener('wheel', quiet, opts);
      document.removeEventListener('scroll', quiet, opts);
      window.removeEventListener('blur', quiet);
      window.removeEventListener('resize', quiet);
      mo.disconnect();
      clearTimeout(timer);
      endPress();
    };
  });
</script>

<svelte:document onpointerover={over} onpointerout={out} onfocusin={focusin} onfocusout={focusout} />

{#if shown}
  <div class="tooltip" class:placed class:figure={!!img} bind:this={card} style:left="{x}px" style:top="{y}px" role="tooltip">
    {#if img}<img src={img} alt="" onload={place} />{/if}
    {#if parts.head}<div class="head">{parts.head}</div>{/if}
    {#if parts.body}<div class="body">{parts.body}</div>{/if}
    {#if reason}
      <div class="why">
        <svg width="12" height="12" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="6.2" fill="none" stroke="currentColor" stroke-width="1.6" /><path d="M3.8 12.2l8.4-8.4" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" /></svg>
        <span>{reason}</span>
      </div>
    {/if}
    {#if parts.keys.length && !byTouch}
      <div class="keys">
        {#each parts.keys as chord, i (i)}
          {#if i}<span class="or">or</span>{/if}
          <span class="chord">{#each chord as k, j (j)}<kbd>{k}</kbd>{/each}</span>
        {/each}
      </div>
    {/if}
  </div>
{/if}

<style>
  .tooltip {
    position: fixed;
    z-index: 10000;
    max-width: 300px;
    padding: 9px 12px 10px;
    border-radius: 10px;
    border: 1px solid var(--line-strong);
    background: color-mix(in srgb, var(--panel) 94%, transparent);
    backdrop-filter: blur(10px) saturate(1.4);
    -webkit-backdrop-filter: blur(10px) saturate(1.4);
    box-shadow:
      0 1px 2px #0000000f,
      0 8px 28px #0000001f;
    color: var(--text);
    font: 12.5px/1.45 var(--font);
    pointer-events: none;
    visibility: hidden;
  }
  .tooltip.placed {
    visibility: visible;
    animation: tip-in 0.12s ease-out;
  }
  @keyframes tip-in {
    from {
      opacity: 0;
      transform: translateY(3px) scale(0.985);
    }
  }
  .figure {
    padding-top: 8px;
  }
  img {
    display: block;
    max-width: 276px;
    max-height: 220px;
    margin: 0 auto 8px;
    border-radius: 6px;
    background: var(--sunken);
  }
  .head {
    font-weight: 600;
    letter-spacing: -0.005em;
  }
  .body {
    color: var(--muted);
    overflow-wrap: anywhere;
  }
  .head + .body {
    margin-top: 2px;
  }
  .why {
    display: flex;
    gap: 6px;
    align-items: baseline;
    margin-top: 8px;
    padding-top: 7px;
    border-top: 1px solid var(--line);
    color: var(--deliverable);
  }
  .why svg {
    flex: none;
    transform: translateY(1px);
  }
  .why:first-child {
    margin-top: 0;
    padding-top: 0;
    border-top: 0;
  }
  .keys {
    display: flex;
    flex-wrap: wrap;
    gap: 4px 6px;
    align-items: center;
    margin-top: 8px;
  }
  .keys:first-child {
    margin-top: 0;
  }
  .chord {
    display: inline-flex;
    gap: 3px;
  }
  kbd {
    min-width: 18px;
    padding: 0 5px;
    border: 1px solid var(--line-strong);
    border-bottom-width: 2px;
    border-radius: 5px;
    background: var(--sunken);
    color: var(--text);
    font: 500 11px/17px var(--mono);
    text-align: center;
  }
  .or {
    color: var(--faint);
    font-size: 11px;
  }
</style>
