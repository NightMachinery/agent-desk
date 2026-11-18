<script>
  let { view, ...props } = $props();
</script>

{#await view}
  <span class="faint">Loading…</span>
{:then { default: Panel }}
  <Panel {...props} />
{:catch}
  <span role="alert">Could not load this panel. Reload the page to try again.</span>
{/await}
