<script>
  // "stale?" beside a started task that may no longer be worked on: its
  // agent has finished (archived, or another agent holds its node now), or
  // nothing touched it for a day. why is the server's reason (Task.stale).
  let { why = '' } = $props();
</script>

{#if why}
  <span class="chip stale" title={`Maybe stale: ${why}. Ask where it stands, start it again, or finish it.`}>stale?</span>
{/if}

<style>
  .stale {
    background: var(--accent-soft);
    color: var(--accent);
  }
</style>
