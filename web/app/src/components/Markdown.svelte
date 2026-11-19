<script>
  import { markdown } from '../lib/md.js';
  import { refs } from '../lib/refs.svelte.js';

  let { text = '', links = true, class: cls = '' } = $props();
  const html = $derived(markdown(text, { links, refs }));
</script>

<!-- Safe: markdown-it runs with html off, so the source cannot add markup. -->
<div class="md {cls}">{@html html}</div>
