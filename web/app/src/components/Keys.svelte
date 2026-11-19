<script>
  import { cheatsheet } from '../lib/shortcuts.js';
  // What the keyboard layer draws: the mode line, the which-key popup, the
  // list of every binding, the M-x palette and the link hints.
  import { keys, pane, continuations, activeBindings, matches, groups, description, describeCommand } from '../lib/keys.svelte.js';
  import { route } from '../lib/route.svelte.js';
  import { position } from '../lib/nav.svelte.js';
  import { sel } from '../lib/select.svelte.js';

  const which = $derived(keys.which && keys.pending.length ? continuations(keys.pending) : []);
  const pos = $derived(position(route.item));
  const paneNow = $derived.by(() => {
    route.item;
    route.settings;
    route.verbatim;
    keys.pane;
    keys.mode;
    keys.tick;
    return pane();
  });
  const paneName = { side: 'sidebar', list: 'list', detail: 'detail', dialog: 'sheet' };

  // The palette.
  const shown = $derived(keys.palette ? matches(keys.palette.items, keys.palette.query).slice(0, 200) : []);
  let input = $state();
  let listEl = $state();
  $effect(() => {
    if (keys.palette) queueMicrotask(() => input?.focus());
  });
  $effect(() => {
    keys.palette?.index;
    listEl?.querySelector('.on')?.scrollIntoView({ block: 'nearest' });
  });
  function close() {
    keys.palette = null;
  }
  function pick(it) {
    close();
    queueMicrotask(() => it?.run());
  }
  function onpalettekey(e) {
    const p = keys.palette;
    if (!p) return;
    const k = (e.ctrlKey ? 'C-' : '') + e.key;
    const n = shown.length;
    if (['C-n', 'C-j', 'ArrowDown', 'Tab'].includes(k)) p.index = n ? (p.index + 1) % n : 0;
    else if (['C-p', 'C-k', 'ArrowUp'].includes(k) || (e.shiftKey && e.key === 'Tab')) p.index = n ? (p.index - 1 + n) % n : 0;
    else if (k === 'Enter') pick(shown[p.index]);
    else if (k === 'Escape' || k === 'C-g') close();
    else return;
    e.preventDefault();
    e.stopPropagation();
  }

  // The describe sheet (SPC h k, SPC h f).
  const desc = $derived.by(() => {
    keys.tick;
    return keys.help === 'describe' ? description(keys.describe) : null;
  });
  const where = (p) => (p ? `in the ${paneName[p]}` : 'everywhere');

  // The help sheet: every binding that applies now, by context.
  const helpGroups = $derived.by(() => {
    if (keys.help !== 'all') return [];
    const by = new Map();
    for (const b of activeBindings()) {
      const where = b.pane ? `In the ${paneName[b.pane]}` : b.seq[0] === 'SPC' ? `Leader: ${groups[b.seq.slice(0, 2).join(' ')] ?? 'SPC'}` : 'Everywhere';
      if (!by.has(where)) by.set(where, new Map());
      const m = by.get(where);
      m.set(b.desc, [...(m.get(b.desc) ?? []), b.keys]);
    }
    return [...by.entries()].map(([where, m]) => ({ where, rows: [...m.entries()] }));
  });
</script>

{#if keys.which && which.length}
  <div class="keys-overlay whichkey" role="status">
    <div class="wk-head"><kbd>{keys.pending.join(' ')}</kbd><span class="faint">{groups[keys.pending.join(' ')] ?? ''}</span></div>
    <div class="wk-grid">
      {#each which as w (w.key)}
        <div class="wk"><kbd>{w.key}</kbd><span class="arrow">→</span><span class:grp={w.group}>{w.desc}</span></div>
      {/each}
    </div>
  </div>
{/if}

{#if keys.help === 'sheet'}
  <div class="keys-overlay scrimmed" role="presentation" onpointerdown={(e) => e.target === e.currentTarget && (keys.help = false)}>
    <div class="sheet keys-ui" role="dialog" aria-label="Keyboard cheatsheet">
      <div class="kh-head">
        <h3>Keyboard</h3>
        <span class="faint"><kbd>TAB</kbd> every key for here · <kbd>q</kbd> close</span>
      </div>
      <div class="cards">
        {#each cheatsheet as c (c.title)}
          <section class="card">
            <h4>{c.title}</h4>
            {#each c.rows as [ks, desc] (desc)}
              <div class="cs-row">
                <span class="cs-keys">{#each ks as k, i (k)}{#if i}<span class="or">/</span>{/if}<kbd>{k}</kbd>{/each}</span>
                <span>{desc}</span>
              </div>
            {/each}
          </section>
        {/each}
      </div>
      <p class="faint kh-note">
        Keys are commands until a text box has focus. The which-key popup after <kbd>SPC</kbd> or <kbd>g</kbd> lists what can follow; every button's tooltip names its key.
      </p>
    </div>
  </div>
{:else if keys.help === 'describe'}
  <div class="keys-overlay scrimmed" role="presentation" onpointerdown={(e) => e.target === e.currentTarget && (keys.help = false)}>
    <div class="keyhelp describe keys-ui" role="dialog" aria-label="Describe">
      <div class="kh-head">
        <h3>{desc?.keys ? 'Describe key' : 'Describe command'}</h3>
        <span class="faint"><kbd>q</kbd> close</span>
      </div>
      {#if !desc}
        <p>Nothing to describe.</p>
      {:else if desc.keys}
        {#if desc.cmd}
          <p><kbd>{desc.keys}</kbd> runs <b>{desc.cmd.desc}</b> <span class="faint mono">{desc.cmd.name}</span> here.</p>
          <p><button class="btn ghost small" onclick={() => desc.cmd && describeCommand(desc.cmd.name)}>More about this command</button></p>
        {:else}
          <p><kbd>{desc.keys}</kbd> does nothing here.</p>
        {/if}
        {#if desc.elsewhere.length}
          <p class="faint">Elsewhere:</p>
          <ul>
            {#each desc.elsewhere as o, i (i)}<li>{o.desc} <span class="faint">({where(o.pane)}, <span class="mono">{o.cmd}</span>)</span></li>{/each}
          </ul>
        {/if}
      {:else}
        <p><b>{desc.cmd.desc}</b> <span class="faint mono">{desc.cmd.name}</span></p>
        <p>
          {#if desc.applies}Available here{desc.here.length ? ':' : ', with no key here; run it from SPC SPC or M-x.'}
            {#each desc.here as k (k)}<kbd>{k}</kbd> {/each}
          {:else}Not available here{desc.cmd.need ? `: it needs ${desc.cmd.need}` : ''}.{/if}
        </p>
        {#if desc.all.length}
          <p class="faint">Every key for it:</p>
          <ul>
            {#each desc.all as b, i (i)}<li><kbd>{b.keys}</kbd> <span class="faint">{where(b.pane)}</span></li>{/each}
          </ul>
        {:else}
          <p class="faint">It has no key; SPC SPC or M-x runs it by name.</p>
        {/if}
      {/if}
    </div>
  </div>
{:else if keys.help}
  <div class="keys-overlay scrimmed">
    <div class="keyhelp keys-ui" role="dialog" aria-label="Key bindings">
      <div class="kh-head">
        <h3>Key bindings</h3>
        <span class="faint">for the {paneName[paneNow]} and everywhere · <kbd>j</kbd>/<kbd>k</kbd> scroll · <kbd>TAB</kbd> cheatsheet · <kbd>q</kbd> close</span>
      </div>
      <p class="faint kh-note">
        Normal mode: keys are commands. A text box puts you in insert mode; <kbd>Esc</kbd> or <kbd>C-g</kbd> leaves it, and
        <kbd>⌘ RET</kbd> (<kbd>C-RET</kbd>) sends. A number before a motion repeats it (<kbd>5j</kbd>).
      </p>
      {#each helpGroups as g (g.where)}
        <h4>{g.where}</h4>
        <div class="kh-grid">
          {#each g.rows as [desc, ks] (desc)}
            <div class="kh-keys">{#each ks as k (k)}<kbd>{k}</kbd>{/each}</div>
            <div>{desc}</div>
          {/each}
        </div>
      {/each}
    </div>
  </div>
{/if}

{#if keys.palette}
  <div class="keys-overlay scrimmed" role="presentation" onpointerdown={(e) => e.target === e.currentTarget && close()}>
    <div class="palette keys-ui" role="dialog" aria-label={keys.palette.title}>
      <div class="pal-head">
        <span class="pal-title">{keys.palette.title}</span>
        <input
          bind:this={input}
          bind:value={keys.palette.query}
          oninput={() => keys.palette && (keys.palette.index = 0)}
          onkeydown={onpalettekey}
          onblur={() => setTimeout(() => keys.palette && document.activeElement !== input && close(), 150)}
          placeholder="Type to narrow; words match in any order"
          autocomplete="off"
          spellcheck="false"
        />
        <span class="faint count">{shown.length}/{keys.palette.items.length}</span>
      </div>
      <div class="pal-list" bind:this={listEl}>
        {#each shown as it, i (it.label + i)}
          <button class="pal-row" class:on={i === keys.palette.index} onpointerdown={(e) => e.preventDefault()} onclick={() => pick(it)}>
            <span class="lbl">{it.label}</span>
            {#if it.detail}<span class="faint">{it.detail}</span>{/if}
            {#if it.key}<kbd>{it.key}</kbd>{/if}
          </button>
        {:else}
          <div class="faint none">No match.</div>
        {/each}
      </div>
    </div>
  </div>
{/if}

{#if keys.hints}
  <div class="keys-overlay hints" aria-hidden="true">
    {#each keys.hints.marks as m (m.label)}
      {#if m.label.startsWith(keys.hints.typed)}
        <span class="hint" style="left: {m.x}px; top: {m.y}px"
          ><b>{m.label.slice(0, keys.hints.typed.length)}</b>{m.label.slice(keys.hints.typed.length)}</span
        >
      {/if}
    {/each}
  </div>
{/if}

<div class="modeline" aria-hidden="true">
  <span class="mode {keys.hints ? 'hints' : keys.palette ? 'mx' : keys.mode}"
    >{keys.hints ? 'HINTS' : keys.capture ? 'KEY?' : keys.palette ? (keys.palette.title === 'M-x' ? 'M-x' : 'PICK') : keys.mode === 'insert' ? 'INSERT' : keys.mode === 'visual' ? 'VISUAL' : 'NORMAL'}</span
  >
  <span class="pane">{paneName[paneNow]}</span>
  {#if keys.count || keys.pending.length}<span class="pending">{keys.count}{keys.pending.join(' ')}-</span>{/if}
  {#if keys.message}<span class="msg">{keys.message}</span>{/if}
  <span class="spacer"></span>
  {#if sel.ids.size}<span>{sel.ids.size} selected</span>{/if}
  {#if pos.at}<span class="mono">{pos.at}/{pos.of}</span>{/if}
  <span class="mono path">{route.path || 'everything'}{route.q ? ` /${route.q}` : ''}</span>
  <span class="faint"><kbd>SPC</kbd> leader · <kbd>?</kbd> keys</span>
</div>

<style>
  kbd {
    font-family: var(--mono);
    font-size: 11px;
    padding: 0 5px;
    border-radius: 4px;
    border: 1px solid var(--line-strong);
    background: var(--sunken);
    white-space: nowrap;
  }
  .keys-overlay {
    position: fixed;
    z-index: 80;
  }
  .scrimmed {
    inset: 0;
    background: #0005;
    display: grid;
    place-items: start center;
    padding-top: 10vh;
  }

  /* which-key: a panel across the bottom, above the mode line. */
  .whichkey {
    left: 0;
    right: 0;
    bottom: var(--modeline-h, 0px);
    background: var(--panel);
    border-top: 1px solid var(--line-strong);
    box-shadow: var(--shadow);
    padding: 8px 16px 10px;
    font-size: 12.5px;
  }
  .wk-head {
    display: flex;
    gap: 8px;
    align-items: center;
    margin-bottom: 6px;
  }
  .wk-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(230px, 1fr));
    gap: 3px 18px;
  }
  .wk {
    display: flex;
    gap: 6px;
    align-items: baseline;
    min-width: 0;
  }
  .wk span:last-child {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .arrow {
    color: var(--muted);
  }
  .grp {
    color: var(--accent, #7c5cff);
    font-weight: 600;
  }

  .sheet {
    width: min(1040px, calc(100vw - 32px));
    max-height: 86vh;
    overflow: auto;
    background: var(--panel);
    border-radius: 16px;
    box-shadow: var(--shadow);
    padding: 18px 22px 16px;
    font-size: 13px;
  }
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(290px, 1fr));
    gap: 12px;
    margin-top: 12px;
  }
  .card {
    border: 1px solid var(--line);
    border-radius: 10px;
    padding: 10px 12px 8px;
  }
  .card h4 {
    margin: 0 0 6px;
    color: var(--accent);
  }
  .cs-row {
    display: grid;
    grid-template-columns: 112px 1fr;
    gap: 10px;
    align-items: baseline;
    padding: 3px 0;
  }
  .cs-keys {
    display: flex;
    flex-wrap: wrap;
    gap: 3px;
    align-items: baseline;
  }
  .or {
    color: var(--muted);
    font-size: 11px;
  }
  .keyhelp {
    width: min(760px, calc(100vw - 32px));
    max-height: 80vh;
    overflow: auto;
    background: var(--panel);
    border-radius: 14px;
    box-shadow: var(--shadow);
    padding: 16px 20px 20px;
    font-size: 13px;
  }
  .keyhelp.describe {
    width: min(520px, calc(100vw - 32px));
  }
  .describe ul {
    margin: 4px 0 8px;
    padding-left: 18px;
  }
  .describe p {
    margin: 8px 0;
  }
  .kh-head {
    display: flex;
    gap: 12px;
    align-items: baseline;
    flex-wrap: wrap;
  }
  .kh-head h3 {
    margin: 0;
    font-size: 16px;
  }
  .kh-note {
    margin: 8px 0 4px;
    font-size: 12px;
  }
  h4 {
    margin: 14px 0 6px;
    font-size: 12px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: var(--muted);
  }
  .kh-grid {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 3px 14px;
  }
  .kh-keys {
    display: flex;
    gap: 4px;
    flex-wrap: wrap;
  }

  .palette {
    width: min(640px, calc(100vw - 32px));
    background: var(--panel);
    border-radius: 12px;
    box-shadow: var(--shadow);
    overflow: hidden;
  }
  .pal-head {
    display: flex;
    gap: 10px;
    align-items: center;
    padding: 10px 12px;
    border-bottom: 1px solid var(--line);
  }
  .pal-title {
    font-family: var(--mono);
    font-size: 12px;
    color: var(--muted);
    white-space: nowrap;
  }
  .pal-head input {
    flex: 1;
    border: 0;
    outline: 0;
    background: transparent;
    color: var(--text);
    font: inherit;
    font-size: 15px;
    min-width: 0;
  }
  .count {
    font-size: 11px;
    font-family: var(--mono);
  }
  .pal-list {
    max-height: 50vh;
    overflow: auto;
    padding: 4px;
  }
  .pal-row {
    display: flex;
    gap: 10px;
    align-items: baseline;
    width: 100%;
    text-align: left;
    border: 0;
    background: transparent;
    color: var(--text);
    padding: 6px 10px;
    border-radius: 7px;
    font: inherit;
    font-size: 13.5px;
    cursor: pointer;
  }
  .pal-row .lbl {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .pal-row.on {
    background: var(--sunken);
    box-shadow: inset 3px 0 0 var(--accent, #7c5cff);
  }
  .none {
    padding: 10px;
  }

  .hints {
    inset: 0;
    pointer-events: none;
  }
  .hint {
    position: fixed;
    transform: translate(-2px, -2px);
    background: #ffd84a;
    color: #222;
    border: 1px solid #c9a400;
    border-radius: 4px;
    padding: 0 4px;
    font: 700 11px/1.5 var(--mono);
    text-transform: uppercase;
    box-shadow: 0 1px 3px #0004;
  }
  .hint b {
    color: #b02a00;
  }

  /* The mode line shows only where a keyboard and a mouse are likely. */
  .modeline {
    display: none;
  }
  @media (hover: hover) and (pointer: fine) {
    .modeline {
      position: fixed;
      left: 0;
      right: 0;
      bottom: 0;
      height: var(--modeline-h);
      z-index: 60;
      display: flex;
      align-items: center;
      gap: 12px;
      padding: 0 10px 0 0;
      background: var(--sunken);
      border-top: 1px solid var(--line);
      font-size: 11.5px;
      color: var(--muted);
      white-space: nowrap;
      overflow: hidden;
    }
  }
  .mode {
    align-self: stretch;
    display: grid;
    place-items: center;
    padding: 0 10px;
    font: 700 11px var(--mono);
    letter-spacing: 0.04em;
    color: #fff;
    background: #4f8a3c;
  }
  .mode.insert {
    background: #3b6fd4;
  }
  .mode.visual {
    background: #c7731d;
  }
  .mode.hints,
  .mode.mx {
    background: #8a4fd4;
  }
  .pane {
    font-weight: 600;
    color: var(--text);
  }
  .pending {
    font-family: var(--mono);
    color: var(--text);
  }
  .msg {
    color: var(--text);
  }
  .spacer {
    flex: 1;
  }
  .path {
    max-width: 40vw;
    overflow: hidden;
    text-overflow: ellipsis;
  }
</style>
