<script>
  // The files attached to an item or a message, each with its caption:
  // images, audio and video play here, anything else is a download. A file
  // that is not on this host (collected, or not merged yet) says why.
  let { media = [] } = $props();
</script>

{#if media.length}
  <div class="attachments">
    {#each media as m (m.sha256 + m.caption)}
      <figure class="att {m.kind}" class:gone={!m.url}>
        {#if !m.url}
          <div class="missing">{m.name}: {m.gone}</div>
        {:else if m.kind === 'image'}
          <a href={'./' + m.url} target="_blank" rel="noopener" title="Open full size">
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
</style>
