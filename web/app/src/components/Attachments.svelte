<script>
  // The files attached to an item or a message, each with its caption:
  // images, audio and video play here, anything else is a download. A file
  // that is not on this host (collected, or not merged yet) says why. An
  // image opens in the viewer (ImageViewer); compact (the Tasks list) shows
  // images as thumbnails and other files as one line each.
  import { openImage } from '../lib/viewer.svelte.js';

  let { media = [], compact = false } = $props();
  const images = $derived(media.filter((m) => m.kind === 'image' && m.url));
  /** Open the image in the viewer; a modified click still opens a tab. */
  function view(e, m) {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    openImage(images, images.indexOf(m));
  }
</script>

{#if media.length && compact}
  <div class="attachments compact">
    {#each media as m (m.sha256 + m.caption)}
      {#if m.kind === 'image' && m.url}
        <a class="thumb" href={'./' + m.url} target="_blank" rel="noopener" data-tip-img={'./' + m.url} title={m.caption !== m.name ? `${m.caption} (${m.name})` : m.name} onclick={(e) => view(e, m)}>
          <img src={'./' + m.url} alt={m.caption} loading="lazy" />
        </a>
      {:else if m.url}
        <a class="line" href={'./' + m.url} download={m.name}>{m.name} <span class="file">{m.size_text}</span></a>
      {:else}
        <span class="line missing">{m.name}: {m.gone}</span>
      {/if}
    {/each}
  </div>
{:else if media.length}
  <div class="attachments">
    {#each media as m (m.sha256 + m.caption)}
      <figure class="att {m.kind}" class:gone={!m.url}>
        {#if !m.url}
          <div class="missing">{m.name}: {m.gone}</div>
        {:else if m.kind === 'image'}
          <a href={'./' + m.url} target="_blank" rel="noopener" title="View it larger" onclick={(e) => view(e, m)}>
            <img src={'./' + m.url} alt={m.caption} loading="lazy" />
          </a>
        {:else if m.kind === 'audio'}
          <audio controls preload="metadata" src={'./' + m.url}></audio>
        {:else if m.kind === 'video'}
          <!-- svelte-ignore a11y_media_has_caption -->
          <video controls preload="metadata" src={'./' + m.url}></video>
        {:else}
          <a class="download" href={'./' + m.url} download={m.name}>
            <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><path d="M8 2v8m0 0-3-3m3 3 3-3M3 13h10" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" /></svg>
            Download
          </a>
        {/if}
        <figcaption>
          {#if m.caption !== m.name}<span class="caption">{m.caption}</span>{/if}
          <span class="file">{m.name} · {m.size_text}</span>
        </figcaption>
      </figure>
    {/each}
  </div>
{/if}

<style>
  .attachments {
    display: flex;
    flex-direction: column;
    gap: 12px;
    margin: 12px 0;
  }
  .att {
    margin: 0;
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 8px;
    background: var(--panel);
  }
  .att img {
    display: block;
    cursor: zoom-in;
    max-width: 100%;
    max-height: 60vh;
    border-radius: 4px;
    margin: 0 auto;
    background: repeating-conic-gradient(var(--line) 0 25%, transparent 0 50%) 0 0 / 16px 16px;
  }
  .att audio,
  .att video {
    display: block;
    width: 100%;
  }
  .att video {
    max-height: 60vh;
    background: #000;
    border-radius: 4px;
  }
  .download {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-weight: 600;
    font-size: 13.5px;
    word-break: break-all;
  }
  figcaption {
    margin-top: 6px;
    display: flex;
    flex-wrap: wrap;
    gap: 4px 10px;
    justify-content: space-between;
    font-size: 13px;
  }
  .file {
    color: var(--muted);
    font-size: 12px;
  }
  .gone {
    border-style: dashed;
  }
  .missing {
    color: var(--muted);
    font-size: 13px;
  }
  .compact {
    flex-direction: row;
    flex-wrap: wrap;
    align-items: flex-start;
    gap: 8px;
    margin: 8px 0;
  }
  .thumb {
    display: block;
    border: 1px solid var(--line);
    border-radius: 6px;
    overflow: hidden;
    cursor: zoom-in;
  }
  .thumb img {
    display: block;
    height: 120px;
    max-width: 240px;
    object-fit: cover;
    background: repeating-conic-gradient(var(--line) 0 25%, transparent 0 50%) 0 0 / 16px 16px;
  }
  .line {
    align-self: center;
    font-size: 13px;
    word-break: break-all;
  }
</style>
