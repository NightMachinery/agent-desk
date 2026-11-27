<script>
  // A modal sheet: slides up from the bottom on a phone, a centred card on
  // a wide screen. Escape (once out of any field) or a tap outside closes
  // it. Every modal is one: the keymap acts on what has role="dialog"
  // (keys.svelte.js), and the key UI (hints, the guide) draws above it. A
  // native dialog element shown as a modal would sit in the browser's top
  // layer, over that UI and making it inert.
  // done: the label of the close button in its head; '' leaves it out, for a
  // sheet with its own Cancel (tag that data-cmd="sheet-close", so q and C-g
  // still close it). width: the card's width on a wide screen, in pixels.
  import { keyUiOpen, typing } from '../lib/keys.svelte.js';

  let { open = $bindable(false), title, done = 'Done', width = 420, children } = $props();
  let box = $state();

  // Rendered under <body>, not where it is declared: an ancestor that makes a
  // stacking context (the docked message box is sticky) would otherwise trap
  // its z-index, and the Previous/Next bar would cover its buttons.
  function portal(node) {
    document.body.append(node);
    return () => node.remove();
  }

  $effect(() => {
    if (!open) return;
    const prev = document.activeElement;
    queueMicrotask(() => box?.querySelector('button, select, input, textarea')?.focus());
    const onkey = (e) => {
      // Escape in a field leaves the field first, as everywhere (the keymap
      // does that); with key UI up it closes that. Then it closes the sheet.
      if (e.key === 'Escape' && !keyUiOpen() && !typing(document.activeElement)) {
        e.stopPropagation();
        open = false;
      }
    };
    window.addEventListener('keydown', onkey, true);
    return () => {
      window.removeEventListener('keydown', onkey, true);
      if (prev instanceof HTMLElement) prev.focus();
    };
  });
</script>

{#if open}
  <div class="sheet-wrap" {@attach portal}>
    <button data-keys="none" class="scrim" aria-label="Close" onclick={() => (open = false)}></button>
    <div class="sheet" role="dialog" aria-modal="true" aria-label={title} style:--sheet-width="{width}px" bind:this={box}>
      <div class="grab" aria-hidden="true"></div>
      <div class="head">
        <h3>{title}</h3>
        {#if done}<button data-cmd="sheet-close" class="btn ghost small" onclick={() => (open = false)}>{done}</button>{/if}
      </div>
      <div class="content">{@render children()}</div>
    </div>
  </div>
{/if}

<style>
  .sheet-wrap {
    position: fixed;
    inset: 0;
    z-index: 50;
    display: grid;
    place-items: center;
  }
  .scrim {
    position: absolute;
    inset: 0;
    border: 0;
    padding: 0;
    background: #0006;
    animation: fade 0.15s ease-out;
  }
  .sheet {
    position: relative;
    width: min(var(--sheet-width), calc(100vw - 32px));
    max-height: min(640px, 85vh);
    overflow: auto;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 16px;
    box-shadow: var(--shadow);
    padding: 6px 16px 16px;
    animation: pop 0.16s ease-out;
  }
  .grab {
    display: none;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 6px 0 8px;
  }
  h3 {
    margin: 0;
    font-size: 15px;
  }
  @media (max-width: 900px) {
    .sheet-wrap {
      place-items: end stretch;
    }
    .sheet {
      width: auto;
      max-height: 85vh;
      border-radius: 18px 18px 0 0;
      border-bottom: 0;
      padding-bottom: max(16px, env(safe-area-inset-bottom));
      animation: rise 0.2s ease-out;
    }
    .grab {
      display: block;
      width: 36px;
      height: 4px;
      border-radius: 2px;
      background: var(--line-strong);
      margin: 4px auto 2px;
    }
  }
  @keyframes fade {
    from {
      opacity: 0;
    }
  }
  @keyframes pop {
    from {
      opacity: 0;
      transform: scale(0.97);
    }
  }
  @keyframes rise {
    from {
      transform: translateY(40px);
      opacity: 0.6;
    }
  }
</style>
