<script>
  // A modal sheet: slides up from the bottom on a phone, a centred card on
  // a wide screen. Escape or a tap outside closes it.
  let { open = $bindable(false), title, children } = $props();
  let box = $state();

  $effect(() => {
    if (!open) return;
    const prev = document.activeElement;
    queueMicrotask(() => box?.querySelector('button, select, input, textarea')?.focus());
    const onkey = (e) => {
      if (e.key === 'Escape') {
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
  <div class="sheet-wrap">
    <button data-keys="none" class="scrim" aria-label="Close" onclick={() => (open = false)}></button>
    <div class="sheet" role="dialog" aria-modal="true" aria-label={title} bind:this={box}>
      <div class="grab" aria-hidden="true"></div>
      <div class="head">
        <h3>{title}</h3>
        <button data-cmd="sheet-close" class="btn ghost small" onclick={() => (open = false)}>Done</button>
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
    width: min(420px, calc(100vw - 32px));
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
