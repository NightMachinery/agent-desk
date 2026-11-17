<script>
  // The images and audio an agent attached, each with its caption. A file
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
        {:else}
          <audio controls preload="metadata" src={'./' + m.url}></audio>
        {/if}
        <figcaption>
          <span class="caption">{m.caption}</span>
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
  .att audio {
    display: block;
    width: 100%;
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
