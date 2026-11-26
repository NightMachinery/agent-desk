<script module>
  let renderFormula = $state(null);
  let loading;

  function loadMath() {
    loading ??= import('../lib/math-render.js').then((module) => {
      renderFormula = module.renderFormula;
    }, () => {
      // The source stays readable if the assets cannot load.
      loading = null;
    });
  }
</script>

<script>
  import { parseMarkdown } from '../lib/md.js';
  import { refs } from '../lib/refs.svelte.js';

  let { text = '', links = true, literalText = false, class: cls = '' } = $props();
  const parsed = $derived(parseMarkdown(text, { links, refs, literalText }));
  const html = $derived(parsed.render(parsed.hasMath ? renderFormula : null));
  $effect(() => {
    if (parsed.hasMath) loadMath();
  });
</script>

<!-- Markdown escapes raw HTML. The only additional markup comes from KaTeX
     with trust-requiring commands blocked, and the math rule escapes its source and errors. -->
<svelte:element this={literalText ? 'span' : 'div'} class="md {cls}" class:md-literal={literalText}>{@html html}</svelte:element>
