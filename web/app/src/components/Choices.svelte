<script>
  // A question's multiple-choice part. Each question has options and one
  // text field: with a pick it is a note on the pick, with none it is your
  // own answer. Clicking a picked option again unpicks it. `lines` is the
  // answer so far, one line per question ("1. B: label. Note: ...", or
  // "(no answer)"); the answer box below sends it with any general message.
  import Markdown from './Markdown.svelte';
  import { tip } from '../lib/shortcuts.js';
  import { onDestroy } from 'svelte';
  import { loadDraft, saveDraft, watchDraft } from '../lib/drafts.js';

  // draft names the question ("choices:q-7f3a"): picks and notes are saved
  // until sent, and another question starts from its own.
  let { choices, draft = '', lines = $bindable(''), count = $bindable(0) } = $props();

  const qs = $derived(choices?.questions ?? []);
  // Per question: the picked keys and the text field.
  let ans = $state([]);
  let ansOf = null; // the question whose picks are in ans
  let stopWatch = () => {};
  onDestroy(() => stopWatch());
  const fresh = (d) => (Array.isArray(d) && d.length === qs.length ? d : qs.map(() => ({ picked: [], text: '' })));
  $effect(() => {
    const k = draft + '#' + qs.length;
    const now = $state.snapshot(ans);
    if (k !== ansOf) {
      ansOf = k;
      ans = fresh(loadDraft(draft));
      stopWatch();
      // Picks and notes made on another device, newer than these.
      stopWatch = watchDraft(draft, (v) => {
        if (ansOf === k) ans = fresh(v);
      });
      return;
    }
    saveDraft(draft, now, now.every((a) => !a.picked.length && !a.text.trim()));
  });

  function pick(i, key) {
    const s = ans[i];
    if (s.picked.includes(key)) s.picked = s.picked.filter((k) => k !== key);
    else s.picked = qs[i].multi ? [...s.picked, key] : [key];
  }
  const answered = (i) => ans[i] && (ans[i].picked.length > 0 || ans[i].text.trim() !== '');

  function line(i) {
    const q = qs[i];
    const s = ans[i];
    const text = s?.text.trim() ?? '';
    if (!s?.picked.length) return `${i + 1}. ${text ? 'Other: ' + text : '(no answer)'}`;
    const os = q.options.filter((o) => s.picked.includes(o.key));
    let out = `${i + 1}. ${os.map((o) => o.key).join(', ')}: ${os.map((o) => o.label).join('; ')}`;
    return text ? `${out}. Note: ${text}` : out;
  }
  $effect(() => {
    count = qs.filter((_, i) => answered(i)).length;
    lines = qs.map((_, i) => line(i)).join('\n');
  });
  function takeAll() {
    qs.forEach((q, i) => q.recommended && (ans[i].picked = [q.recommended]));
  }
  const anyRec = $derived(qs.some((q) => q.recommended));
</script>

<div class="choices">
  {#each qs as q, i (i)}
    {@const s = ans[i]}
    {#if s}
      <fieldset class="cq" class:done={answered(i)}>
        <legend><span class="num">{i + 1}</span><Markdown text={q.text} /></legend>
        {#each q.options as o (o.key)}
          <label class="opt" class:on={s.picked.includes(o.key)}>
            <input
              type={q.multi ? 'checkbox' : 'radio'}
              name={'cq' + i}
              checked={s.picked.includes(o.key)}
              onclick={() => pick(i, o.key)}
            />
            <span class="key">{o.key}</span>
            <span class="lbl">
              {o.label}
              {#if o.key === q.recommended}<span class="chip ok rec">recommended</span>{/if}
              {#if o.detail}<span class="detail">{o.detail}</span>{/if}
            </span>
          </label>
        {/each}
        <textarea
          class="input"
          rows="1"
          bind:value={s.text}
          placeholder={s.picked.length ? 'A note on your pick (optional)' : 'Or your own answer, with nothing picked'}
        ></textarea>
      </fieldset>
    {/if}
  {/each}
  <div class="bar">
    {#if anyRec}<button class="btn small" data-cmd="quick" title={tip('Pick every recommendation; you can still change them', 'quick')} onclick={takeAll}>Pick the recommendations</button>{/if}
    <span class="faint">{count} of {qs.length} answered; unanswered ones are sent as "(no answer)"</span>
  </div>
</div>

<style>
  .choices {
    display: flex;
    flex-direction: column;
    gap: 12px;
    margin: 6px 0 14px;
  }
  .cq {
    border: 1px solid var(--line);
    border-radius: var(--radius);
    padding: 10px 12px 12px;
    margin: 0;
    background: var(--panel);
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .cq.done {
    border-color: var(--line-strong);
  }
  legend {
    display: flex;
    gap: 8px;
    align-items: baseline;
    padding: 0 4px;
    font-weight: 600;
  }
  legend :global(p) {
    margin: 0;
  }
  .num {
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }
  .opt {
    display: grid;
    grid-template-columns: auto minmax(22px, max-content) 1fr;
    gap: 8px;
    align-items: baseline;
    padding: 6px 8px;
    border-radius: 8px;
    cursor: pointer;
  }
  .opt:hover {
    background: var(--hover, rgba(127, 127, 127, 0.08));
  }
  .opt.on {
    background: var(--accent-soft);
  }
  .key {
    font-family: var(--mono);
    color: var(--muted);
  }
  .detail {
    display: block;
    color: var(--muted);
    font-size: 13px;
    margin-top: 2px;
  }
  .rec {
    margin-left: 6px;
    font-size: 11px;
  }
  textarea {
    margin: 4px 0 0;
    min-height: 0;
    field-sizing: content;
  }
  .bar {
    display: flex;
    gap: 10px;
    align-items: center;
    justify-content: flex-end;
  }
  .bar .faint {
    margin-right: auto;
  }
</style>
