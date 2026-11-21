<script>
  // The image viewer (lib/viewer.svelte.js): the image fit to the screen,
  // with its name; h and l (or the arrows) step through the item's other
  // images. Escape, q, C-g or a tap beside the image closes it.
  import { viewer, closeImage, stepImage } from '../lib/viewer.svelte.js';
  import { tip } from '../lib/shortcuts.js';

  let box = $state();
  const open = $derived(viewer.at >= 0);
  const img = $derived(open ? viewer.images[viewer.at] : null);
  // A served file is relative to the page; a local preview (blob:) is not.
  const src = (u) => (/^(blob|data|https?):/.test(u) ? u : './' + u);

  $effect(() => {
    if (!open) return;
    const prev = document.activeElement;
    queueMicrotask(() => box?.querySelector('[data-cmd="sheet-close"]')?.focus());
    const onkey = (e) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        e.preventDefault();
        closeImage();
      }
    };
    window.addEventListener('keydown', onkey, true);
    return () => {
      window.removeEventListener('keydown', onkey, true);
      if (prev instanceof HTMLElement) prev.focus();
    };
  });
</script>

{#if img}
  <div class="viewer" role="dialog" aria-modal="true" aria-label={img.name} bind:this={box}>
    <button data-keys="none" class="scrim" aria-label="Close" onclick={closeImage}></button>
    <img src={src(img.url)} alt={img.caption || img.name} />
    <div class="bar">
      {#if viewer.images.length > 1}
        <button data-cmd="image-prev" class="btn small" title={tip('Previous image', 'image-prev')} onclick={() => stepImage(-1)}>‹</button>
        <span class="n">{viewer.at + 1} / {viewer.images.length}</span>
        <button data-cmd="image-next" class="btn small" title={tip('Next image', 'image-next')} onclick={() => stepImage(1)}>›</button>
      {/if}
      <span class="name">{img.caption && img.caption !== img.name ? `${img.caption} · ` : ''}{img.name}</span>
      <a class="btn small" href={src(img.url)} target="_blank" rel="noopener">Open original</a>
      <button data-cmd="sheet-close" class="btn small" title={tip('Close', 'sheet-close')} onclick={closeImage}>Close</button>
    </div>
  </div>
{/if}

<style>
  .viewer {
    position: fixed;
    inset: 0;
    z-index: 60;
    display: grid;
    grid-template-rows: 1fr auto;
    place-items: center;
    padding: 16px 16px 0;
    gap: 8px;
  }
  .scrim {
    position: absolute;
    inset: 0;
    border: 0;
    padding: 0;
    background: #000c;
    cursor: zoom-out;
  }
  img {
    position: relative;
    max-width: 100%;
    max-height: calc(100vh - 80px);
    object-fit: contain;
    border-radius: 4px;
    background: repeating-conic-gradient(#8884 0 25%, transparent 0 50%) 0 0 / 16px 16px;
  }
  .bar {
    position: relative;
    display: flex;
    align-items: center;
    gap: 8px;
    max-width: 100%;
    margin-bottom: calc(12px + env(safe-area-inset-bottom));
    padding: 6px 8px;
    border-radius: 10px;
    background: #000b;
    font-size: 13px;
  }
  .name,
  .n {
    color: #eee;
  }
  .name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .n {
    font-variant-numeric: tabular-nums;
  }
</style>
