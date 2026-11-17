<script>
  // Icon buttons that copy how to reach an agent: its Paseo id (what other
  // agents and the paseo CLI take) and its session's resume command.
  import Icon from './Icon.svelte';
  import { copy, short } from '../lib/format.js';
  import { toast } from '../lib/toast.svelte.js';
  import { tip } from '../lib/shortcuts.js';

  let { paseo = '', resume = '', keys = true } = $props();

  async function yank(text, what) {
    toast((await copy(text)) ? `${what} copied.` : text, 'ok');
  }
  const t = (label, cmd) => (keys ? tip(label, cmd) : label);
</script>

{#if paseo}
  <button data-cmd={keys ? 'copy-paseo' : undefined} class="btn ghost small iconbtn" title={t(`Copy the Paseo id ${short(paseo)}…`, 'copy-paseo')} aria-label="Copy the Paseo id" onclick={() => yank(paseo, 'Paseo id')}
    ><Icon name="idtag" /></button
  >
{/if}
{#if resume}
  <button data-cmd={keys ? 'copy-resume' : undefined} class="btn ghost small iconbtn" title={t(`Copy the resume command: ${resume}`, 'copy-resume')} aria-label="Copy the resume command" onclick={() => yank(resume, 'Resume command')}
    ><Icon name="terminal" /></button
  >
{/if}
