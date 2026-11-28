<script>
  // One prompt the desk sends agents (internal/prompts): the text in use,
  // whether it is the built-in one or the user's copy and where that copy
  // lives, and an editor that saves the copy (POST api/prompt) or removes it
  // to go back to the built-in one. The server refuses a template that does
  // not parse, or for a reminder, names a field it does not have.
  import { post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { failed } from '../lib/errors.svelte.js';
  import { off, BUSY } from '../lib/why.js';

  let { prompt = $bindable() } = $props();
  let editing = $state(false);
  let draft = $state('');
  let busy = $state(false);
  const text = $derived(prompt.overridden ? prompt.override : prompt.builtin);
  const changed = $derived(draft !== text);

  function edit() {
    draft = text;
    editing = true;
  }
  async function send(body, said) {
    busy = true;
    try {
      prompt = await post('prompt', { name: prompt.name, ...body });
      editing = false;
      toast(said, 'ok');
    } catch (err) {
      failed(err, `save ${prompt.name}`);
    } finally {
      busy = false;
    }
  }
  const save = () => send({ text: draft }, `Saved your copy of ${prompt.name}.`);
  const reset = () => send({ reset: true }, `${prompt.name} is the built-in one again.`);
</script>

<div class="prompt">
  <div class="head">
    <span class="mono name">{prompt.name}</span>
    <span class="faint where" title={prompt.path}>{prompt.overridden ? `your copy: ${prompt.path}` : `built-in; a copy at ${prompt.path} replaces it`}</span>
  </div>
  {#if editing}
    <textarea class="input mono" rows={Math.min(18, Math.max(6, draft.split('\n').length + 1))} bind:value={draft} aria-label="Template {prompt.name}" spellcheck="false"></textarea>
    <div class="acts">
      <button data-keys="none" class="btn small primary" {...off(undefined, [busy, BUSY], [!changed, 'Nothing changed'])} onclick={save}>Save my copy</button>
      <button data-keys="none" class="btn small ghost" onclick={() => (editing = false)}>Cancel</button>
    </div>
  {:else}
    <pre class="text">{text}</pre>
    <div class="acts">
      <button data-keys="none" class="btn small" onclick={edit}>Edit</button>
      {#if prompt.overridden}<button data-keys="none" class="btn small ghost" {...off(undefined, [busy, BUSY])} onclick={reset}>Back to built-in</button>{/if}
    </div>
  {/if}
</div>

<style>
  .prompt {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-top: 4px;
  }
  .head {
    display: flex;
    gap: 8px;
    align-items: baseline;
    flex-wrap: wrap;
    font-size: 12px;
  }
  .name {
    font-weight: 600;
  }
  .where {
    overflow-wrap: anywhere;
  }
  .text {
    margin: 0;
    padding: 8px 10px;
    max-height: 220px;
    overflow: auto;
    border-radius: 8px;
    background: var(--sunken);
    font-family: var(--mono);
    font-size: 12px;
    line-height: 1.45;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  textarea {
    width: 100%;
    box-sizing: border-box;
    font-size: 12px;
    line-height: 1.45;
    resize: vertical;
  }
  .acts {
    display: flex;
    gap: 6px;
  }
</style>
