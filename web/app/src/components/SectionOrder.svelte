<script>
  // The order of the list's sections, set by dragging a card by its handle
  // (mouse or touch), with the arrow buttons, or with the arrow keys on a
  // focused handle. Every device shows the order saved here.
  import { flip } from 'svelte/animate';
  import { tick } from 'svelte';
  import { ui, sectionNames, defaultSections, setSectionOrder } from '../lib/ui.svelte.js';
  import { off } from '../lib/why.js';

  const about = {
    needs: 'Questions waiting on you',
    quota: 'Agents out of usage quota',
    deliverables: 'Deployments and packages to try',
    pinned: 'What you pinned',
    responses: "Agents' answers to your messages",
    tasks: 'Open and started tasks here',
    new: 'Notices, reports and slots',
    runs: 'Projects, runs and agents',
    deferred: 'Tasks waiting for quota',
  };
  const tone = {
    needs: 'var(--accent)',
    quota: 'var(--blocked)',
    deliverables: 'var(--deliverable)',
    pinned: 'var(--default)',
    responses: 'var(--response)',
    tasks: 'var(--ok)',
    new: 'var(--notice)',
    runs: 'var(--muted)',
    deferred: 'var(--faint)',
  };

  let order = $state([...ui.sectionOrder]);
  // The card being dragged: its name, where the pointer holds it (px from
  // its top), and how far it is drawn from its place in the list.
  let drag = $state(null);
  let list = $state();
  const els = {};
  $effect(() => {
    const shared = ui.sectionOrder;
    if (!drag) order = [...shared];
  });
  const isDefault = $derived(order.join() === defaultSections.join());

  function save() {
    if (order.join() === ui.sectionOrder.join()) return;
    setSectionOrder(isDefault ? null : [...order]);
  }
  function shift(from, to) {
    const next = [...order];
    next.splice(to, 0, ...next.splice(from, 1));
    order = next;
  }
  function nudge(name, by) {
    const i = order.indexOf(name);
    const j = i + by;
    if (j < 0 || j >= order.length) return;
    shift(i, j);
    save();
  }

  function down(e, name) {
    if (e.button > 0) return;
    e.preventDefault();
    try {
      e.currentTarget.setPointerCapture(e.pointerId);
    } catch {
      // a pointer the browser no longer tracks: moves still reach the handle
    }
    const top = els[name].getBoundingClientRect().top;
    drag = { name, grab: e.clientY - top, y: 0 };
  }
  async function move(e) {
    if (!drag) return;
    const el = els[drag.name];
    const top = e.clientY - drag.grab - list.getBoundingClientRect().top;
    const mid = top + el.offsetHeight / 2;
    const i = order.indexOf(drag.name);
    const prev = els[order[i - 1]];
    const next = els[order[i + 1]];
    if (prev && mid < prev.offsetTop + prev.offsetHeight / 2) shift(i, i - 1);
    else if (next && mid > next.offsetTop + next.offsetHeight / 2) shift(i, i + 1);
    await tick();
    if (drag) drag.y = top - el.offsetTop;
  }
  function up() {
    if (!drag) return;
    save();
    drag = null;
  }
  function keys(e, name) {
    const by = e.key === 'ArrowUp' ? -1 : e.key === 'ArrowDown' ? 1 : 0;
    if (!by) return;
    e.preventDefault();
    e.stopPropagation();
    nudge(name, by);
  }
</script>

<div class="order" class:dragging={!!drag}>
  <ol bind:this={list}>
    {#each order as s, i (s)}
      <li
        bind:this={els[s]}
        class="card"
        class:lifted={drag?.name === s}
        style:--tone={tone[s]}
        style:transform={drag?.name === s ? `translateY(${drag.y}px) scale(1.02)` : null}
        animate:flip={{ duration: drag?.name === s ? 0 : 180 }}
      >
        <button
          class="grip"
          data-cmd={i === 0 ? 'section-order' : undefined}
          aria-label={`Move ${sectionNames[s]}: drag, or press the up and down arrows`}
          title="Drag to move; ↑ ↓ on the keyboard"
          onpointerdown={(e) => down(e, s)}
          onpointermove={move}
          onpointerup={up}
          onpointercancel={up}
          onkeydown={(e) => keys(e, s)}
        >
          <svg width="12" height="18" viewBox="0 0 12 18" aria-hidden="true">
            {#each [3, 9, 15] as y (y)}<circle cx="3.5" cy={y} r="1.5" /><circle cx="8.5" cy={y} r="1.5" />{/each}
          </svg>
        </button>
        <span class="pos">{i + 1}</span>
        <span class="what">
          <span class="name">{sectionNames[s]}</span>
          <span class="about">{about[s]}</span>
        </span>
        <span class="moves">
          <button class="nudge" aria-label={`Move ${sectionNames[s]} up`} {...off(undefined, [i === 0, 'Already first'])} onclick={() => nudge(s, -1)}>
            <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="m3 7.5 3-3 3 3" /></svg>
          </button>
          <button class="nudge" aria-label={`Move ${sectionNames[s]} down`} {...off(undefined, [i === order.length - 1, 'Already last'])} onclick={() => nudge(s, 1)}>
            <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="m3 4.5 3 3 3-3" /></svg>
          </button>
        </span>
      </li>
    {/each}
  </ol>
  <div class="foot">
    <span class="faint">Drag a card by its handle, or use the arrows. Every device shows this order.</span>
    <button data-cmd="section-order-reset" class="btn ghost small" {...off(undefined, [isDefault, 'Already in the default order'])} onclick={() => ((order = [...defaultSections]), save())}>Reset to default</button>
  </div>
</div>

<style>
  .order {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .order.dragging {
    user-select: none;
    -webkit-user-select: none;
    cursor: grabbing;
  }
  ol {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .card {
    position: relative;
    display: flex;
    align-items: center;
    gap: 10px;
    min-height: 48px;
    padding: 6px 8px 6px 4px;
    border: 1px solid var(--line);
    border-left: 4px solid var(--tone);
    border-radius: 10px;
    background: linear-gradient(90deg, color-mix(in srgb, var(--tone) 9%, var(--panel)), var(--panel) 45%);
    transition: box-shadow 0.15s, transform 0.15s;
  }
  .card:hover:not(:disabled) {
    box-shadow: 0 1px 2px #0000000d, 0 2px 8px #0000000a;
  }
  .card.lifted {
    z-index: 2;
    box-shadow: var(--shadow), 0 10px 28px #0000001f;
    transition: box-shadow 0.15s;
  }
  .grip {
    display: grid;
    place-items: center;
    width: 30px;
    height: 36px;
    padding: 0;
    border: 0;
    border-radius: 7px;
    background: none;
    color: var(--faint);
    cursor: grab;
    touch-action: none;
  }
  .grip svg {
    fill: currentColor;
  }
  .grip:hover:not(:disabled),
  .grip:focus-visible,
  .lifted .grip {
    color: var(--tone);
    background: color-mix(in srgb, var(--tone) 12%, transparent);
    outline: none;
  }
  .lifted .grip {
    cursor: grabbing;
  }
  .pos {
    display: grid;
    place-items: center;
    flex: none;
    width: 22px;
    height: 22px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--tone) 16%, var(--panel));
    color: var(--tone);
    font-size: 11.5px;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
  }
  .what {
    display: flex;
    flex-direction: column;
    min-width: 0;
    flex: 1;
  }
  .name {
    font-weight: 600;
    font-size: 13.5px;
  }
  .about {
    color: var(--muted);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .moves {
    display: flex;
    gap: 2px;
  }
  .nudge {
    display: grid;
    place-items: center;
    width: 28px;
    height: 28px;
    padding: 0;
    border: 1px solid transparent;
    border-radius: 7px;
    background: none;
    color: var(--muted);
    cursor: pointer;
  }
  .nudge svg {
    fill: none;
    stroke: currentColor;
    stroke-width: 1.7;
    stroke-linecap: round;
    stroke-linejoin: round;
  }
  .nudge:hover:not(:disabled) {
    border-color: var(--line);
    background: var(--panel);
    color: var(--text);
  }
  .nudge:disabled {
    opacity: 0.3;
    cursor: default;
  }
  .foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    font-size: 12px;
  }
</style>
