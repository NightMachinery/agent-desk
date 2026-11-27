<script>
  // Replace an agent out of quota: pick the provider for the new agent from
  // every one Paseo offers (with its account's quota), then its model and
  // effort, filled in for that provider and still editable. The new agent
  // recovers the old one's partial work, takes over its role and desk nodes,
  // and archives it (prompt: replace-agent.md).
  import { get, post } from '../lib/api.js';
  import { toast } from '../lib/toast.svelte.js';
  import { availableText } from '../lib/quotatime.js';
  import { pickProvider, pickModel, pickThinking, profileOf, parseProfile } from '../lib/replace.js';
  import { short } from '../lib/format.js';
  import { off } from '../lib/why.js';
  import { onMount } from 'svelte';
  import Sheet from './Sheet.svelte';

  /** agent: a card of api/quota; presets: the config's [quota] profiles. */
  let { agent = null, presets = [], onclose = () => {}, ondone = () => {} } = $props();

  const KEY = 'agent-desk:replace';
  const saved = (() => {
    try {
      return JSON.parse(localStorage.getItem(KEY) ?? '{}');
    } catch {
      return {};
    }
  })();
  const remembered = saved.models ?? {};

  let form = $state();
  let providers = $state(/** @type {any[]} */ ([]));
  let loading = $state(true);
  let error = $state('');
  let provider = $state('');
  let model = $state('');
  let thinking = $state('');
  let starting = $state(false);
  let failed = $state('');
  let now = $state(Date.now());

  const current = $derived(providers.find((p) => p.id === provider));
  const models = $derived(current?.models ?? []);
  const currentModel = $derived(models.find((m) => m.id === model));
  const efforts = $derived(currentModel?.thinking ?? []);
  const profile = $derived(profileOf(provider, model, thinking));
  const own = (p) => p.id === agent?.provider;

  async function load(fresh = false) {
    loading = true;
    error = '';
    try {
      const res = await get('providers', { fresh: fresh ? 1 : '' });
      providers = res.providers ?? [];
      now = Date.now();
      if (!providers.some((p) => p.id === provider)) choose(pickProvider(providers, agent, saved.provider));
      if (!providers.length) error = 'Paseo lists no enabled, available provider.';
    } catch (err) {
      error = err.message;
    } finally {
      loading = false;
    }
  }

  // Selecting a provider fills in its model and effort.
  function choose(id) {
    provider = id;
    const p = providers.find((x) => x.id === id);
    ({ model, thinking } = pickModel(p, agent, remembered));
    failed = '';
  }
  // A new model keeps the effort if it offers it.
  function chooseModel(id) {
    model = id;
    thinking = pickThinking(models.find((m) => m.id === id), thinking);
  }
  function applyPreset(s) {
    const pr = parseProfile(s);
    const p = providers.find((x) => x.id === pr.provider);
    if (!p) {
      failed = `Preset ${s}: Paseo has no enabled provider ${pr.provider}.`;
      return;
    }
    provider = p.id;
    const m = (p.models ?? []).find((x) => x.id === pr.model) ?? null;
    if (m) {
      model = m.id;
      thinking = pickThinking(m, pr.thinking);
    } else ({ model, thinking } = pickModel(p, agent, remembered));
    failed = '';
  }

  async function start(e) {
    e?.preventDefault();
    if (!profile || starting) return;
    starting = true;
    failed = '';
    try {
      const res = await post('quota', { action: 'replace', id: agent.paseo, profile });
      try {
        localStorage.setItem(KEY, JSON.stringify({ provider, models: { ...remembered, [provider]: { model, thinking } } }));
      } catch {
        // Private mode: the choice is not remembered.
      }
      toast(res.note || `Started a replacement on ${current?.label ?? provider}.`, 'ok', 8000);
      ondone();
      close();
    } catch (err) {
      failed = err.message;
    } finally {
      starting = false;
    }
  }

  const close = () => onclose();

  onMount(() => void load());
  // Focus the selected provider once the list is there, so the arrow keys
  // move between providers and Enter starts.
  $effect(() => {
    if (loading || !provider || !form) return;
    queueMicrotask(() => form?.querySelector(`input[name="provider"][value="${CSS.escape(provider)}"]`)?.focus());
  });

  const quotaText = (p) => {
    if (!p.verdict) return 'quota unknown';
    if (p.verdict === 'usable') return 'quota left';
    if (p.verdict === 'blocked') {
      const at = availableText(p, now).split(', at ')[1];
      return at ? `out of quota · back ${at}` : 'out of quota';
    }
    return 'quota unknown';
  };
  const modelLabel = (id) => models.find((m) => m.id === id)?.label ?? id;
</script>

<!-- A Sheet, so the keys work in it; Escape, a tap outside, q and Cancel close it. -->
<Sheet title={`Replace “${agent?.title || short(agent?.paseo ?? '')}”`} done="" width={540}
  bind:open={() => true, (v) => { if (!v) close(); }}>
  <form bind:this={form} onsubmit={start}>
    <header>
      <p class="lead">A new agent recovers its partial work, takes over its role and desk nodes, and archives it.
        {#if agent?.model}It ran <span class="mono">{agent.provider}/{agent.model}{agent.thinking ? '@' + agent.thinking : ''}</span>.{/if}</p>
    </header>

    <fieldset class="providers" disabled={starting}>
      <legend>Provider</legend>
      {#if loading && !providers.length}
        <p class="faint">Asking Paseo for its providers…</p>
      {:else if error}
        <p class="err">{error}</p>
        <button data-keys="none" type="button" class="btn small" onclick={() => load(true)}>Try again</button>
      {:else}
        {#each providers as p (p.id)}
          <label class="prov" class:on={p.id === provider} class:blocked={p.verdict === 'blocked'}>
            <input type="radio" name="provider" value={p.id} checked={p.id === provider} onchange={() => choose(p.id)} />
            <span class="name">{p.label}
              {#if own(p)}<span class="tag">this agent’s</span>{/if}</span>
            <span class="quota" class:ok={p.verdict === 'usable'} class:bad={p.verdict === 'blocked'}
              title={[p.account && 'Account ' + p.account, availableText(p, now), ...(p.reasons ?? [])].filter(Boolean).join('\n') || 'The quota keeper has not seen an agent on this provider'}>
              {p.account_label ? p.account_label + ' · ' : ''}{quotaText(p)}</span>
          </label>
        {/each}
      {/if}
    </fieldset>

    {#if current}
      <div class="pickers">
        <label>
          <span class="lbl">Model</span>
          {#if models.length}
            <select value={model} disabled={starting} onchange={(e) => chooseModel(e.currentTarget.value)}>
              {#each models as m (m.id)}
                <option value={m.id} title={m.description}>{m.label}{m.id === agent?.model ? ' (as before)' : ''}</option>
              {/each}
            </select>
          {:else}
            <select disabled><option>Provider default</option></select>
          {/if}
        </label>
        <label>
          <span class="lbl">Effort</span>
          {#if efforts.length}
            <select bind:value={thinking} disabled={starting}>
              {#each efforts as t (t)}
                <option value={t}>{t}{t === currentModel?.default_thinking ? ' (default)' : ''}</option>
              {/each}
            </select>
          {:else}
            <select disabled><option>Provider default</option></select>
          {/if}
        </label>
      </div>
      {#if currentModel?.description}<p class="desc faint">{currentModel.description}</p>{/if}
      {#if current.verdict === 'blocked'}
        <p class="warn">{current.label} is out of quota too; the new agent would stop at once.</p>
      {/if}
    {/if}

    {#if presets.length && providers.length}
      <div class="presets">
        <span class="faint">Presets</span>
        {#each presets as s (s)}
          <button data-keys="none" type="button" class="chip mono" class:on={s === profile} onclick={() => applyPreset(s)}>{s}</button>
        {/each}
      </div>
    {/if}

    {#if failed}<p class="err">{failed}</p>{/if}

    <footer>
      <span class="profile faint" title="The profile handed to launch_command">{profile ? `${current?.label ?? provider} · ${modelLabel(model) || 'default model'}${thinking ? ' · ' + thinking : ''}` : ''}</span>
      <button data-cmd="sheet-close" type="button" class="btn" onclick={close}>Cancel</button>
      <button type="submit" class="btn primary" data-keys="none" {...off(undefined, [starting, 'Wait: starting the new agent'], [loading && !providers.length, 'Wait: loading the providers'], [!profile, 'Pick a provider first'])}>
        {starting ? 'Starting…' : 'Start replacement'}
      </button>
    </footer>
    <p class="keys faint">↑↓ provider · Tab to model and effort · Enter starts · Esc cancels</p>
  </form>
</Sheet>

<style>
  form {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .lead {
    margin: 0;
    color: var(--muted);
    font-size: 13px;
    line-height: 1.45;
  }
  .mono {
    font-family: var(--mono);
    font-size: 12px;
  }
  fieldset {
    border: 0;
    margin: 0;
    padding: 0;
    min-width: 0;
  }
  legend,
  .lbl {
    font-size: 11.5px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--muted);
    margin-bottom: 6px;
    padding: 0;
  }
  .providers {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .prov {
    display: grid;
    grid-template-columns: auto 1fr auto;
    align-items: center;
    gap: 10px;
    padding: 9px 10px;
    border: 1px solid var(--line);
    border-radius: 9px;
    cursor: pointer;
    font-size: 14px;
  }
  .prov:hover:not(:disabled) {
    background: var(--sunken);
  }
  .prov.on {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  .prov.blocked:not(.on) .name {
    color: var(--muted);
  }
  .prov input {
    margin: 0;
    accent-color: var(--accent);
  }
  .tag {
    white-space: nowrap;
    margin-left: 6px;
    padding: 1px 6px;
    border-radius: 999px;
    background: var(--sunken);
    color: var(--muted);
    font-size: 11px;
  }
  .quota {
    font-size: 12px;
    color: var(--faint);
    text-align: right;
  }
  .quota.ok {
    color: var(--ok);
  }
  .quota.bad {
    color: var(--blocked);
  }
  .pickers {
    display: grid;
    grid-template-columns: 2fr 1fr;
    gap: 10px;
  }
  .pickers label {
    display: flex;
    flex-direction: column;
  }
  select {
    font: inherit;
    font-size: 14px;
    padding: 7px 8px;
    border: 1px solid var(--line-strong);
    border-radius: 8px;
    background: var(--panel);
    color: var(--text);
  }
  .desc {
    margin: -6px 0 0;
    font-size: 12.5px;
  }
  .warn,
  .err {
    margin: 0;
    padding: 8px 10px;
    border-radius: 8px;
    font-size: 13px;
    background: var(--blocked-soft);
    color: var(--blocked);
  }
  .presets {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    align-items: center;
    font-size: 12px;
  }
  .chip {
    border: 1px solid var(--line);
    background: transparent;
    color: inherit;
    border-radius: 999px;
    padding: 3px 9px;
    cursor: pointer;
  }
  .chip.on {
    border-color: var(--accent);
    background: var(--accent-soft);
  }
  footer {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .profile {
    font-size: 12.5px;
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .keys {
    margin: -6px 0 0;
    font-size: 11.5px;
    text-align: right;
  }
  @media (pointer: coarse) {
    .keys {
      display: none;
    }
  }
  @media (max-width: 480px) {
    .pickers {
      grid-template-columns: 1fr;
    }
    footer {
      flex-wrap: wrap;
    }
    .profile {
      flex-basis: 100%;
    }
  }
</style>
