<script>
  // Auto-navigation's order (lib/moveon.js): where n goes, and where a wide
  // screen moves on to after an answer. Shared by every device.
  import OrderCards from './OrderCards.svelte';
  import { ui, setNav } from '../lib/ui.svelte.js';
  import { navKinds, navKindNames } from '../lib/moveon.js';

  const about = {
    blocked: 'The agent waits for your answer',
    default: 'The agent goes on with its default if you do not answer',
    deliverable: 'Deployments and packages to try',
    response: "Agents' answers to your messages",
    notice: 'Notices, reports and slots (New)',
  };
  const tone = {
    blocked: 'var(--blocked)',
    default: 'var(--default)',
    deliverable: 'var(--deliverable)',
    response: 'var(--response)',
    notice: 'var(--notice)',
  };
</script>

<div class="key" id="nav-order">
  <div class="key-head">
    <span class="key-name">Auto-navigation: where n and moving on go</span>
    <span class="mono faint">every device</span>
  </div>
  <label class="check">
    <input type="checkbox" data-cmd="nav-views" checked={ui.nav.views} onchange={(e) => setNav({ views: e.currentTarget.checked })} />
    Fewer views first: items opened less often come first
  </label>
  <div class="origin">
    <span class="faint">An open question stays listed after you open it; everything else is read on opening and drops out. Then by kind:</span>
  </div>
  <OrderCards
    value={ui.nav.kinds}
    defaults={navKinds}
    names={navKindNames}
    {about}
    {tone}
    onsave={(kinds) => setNav({ kinds: kinds ?? navKinds })}
    cmd="nav-order"
    resetCmd="nav-order-reset"
    foot="Drag a card by its handle, or use the arrows."
  />
  <label class="age">
    <span>Within a kind</span>
    <select class="input" data-cmd="nav-age" value={ui.nav.age} onchange={(e) => setNav({ age: e.currentTarget.value === 'new' ? 'new' : 'old' })}>
      <option value="old">Oldest first</option>
      <option value="new">Newest first</option>
    </select>
  </label>
</div>

<style>
  .age {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 8px;
    font-size: 13px;
  }
  .age select {
    width: auto;
  }
</style>
