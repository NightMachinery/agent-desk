<script>
  // The mode line's layout buttons on a wide screen: hide or show the sidebar
  // and the list, and fullscreen; in fullscreen, a tab per pane instead. They
  // run the pane commands (keymap.js), so a click and a key do the same. Out
  // of the tab order, like the rest of the mode line: the keys reach them all
  // by name.
  import { layout, screen } from '../lib/layout.svelte.js';
  import { keys, runCommand } from '../lib/keys.svelte.js';
  import { tip } from '../lib/shortcuts.js';

  // A press must not take focus from the pane, or the keys would follow it.
  const keep = (e) => e.preventDefault();
</script>

{#if screen.wide}
  <span class="panes" role="group" aria-label="Panes">
    {#if layout.full}
      <button data-cmd="pane-side" tabindex="-1" class="tab" class:on={keys.pane === 'side'} title={tip('Show the sidebar', 'pane-side')} onpointerdown={keep} onclick={() => runCommand('pane-side')}>Sidebar</button>
      <button data-cmd="pane-list" tabindex="-1" class="tab" class:on={keys.pane === 'list'} title={tip('Show the list', 'pane-list')} onpointerdown={keep} onclick={() => runCommand('pane-list')}>List</button>
      <button data-cmd="pane-detail" tabindex="-1" class="tab" class:on={keys.pane === 'detail'} title={tip('Show the detail', 'pane-detail')} onpointerdown={keep} onclick={() => runCommand('pane-detail')}>Detail</button>
    {:else}
      <button data-cmd="toggle-sidebar" tabindex="-1" class="icon" class:on={!layout.hidden.side} aria-pressed={!layout.hidden.side} title={tip(layout.hidden.side ? 'Show the sidebar' : 'Hide the sidebar', 'toggle-sidebar')} onpointerdown={keep} onclick={() => runCommand('toggle-sidebar')}
        ><svg width="15" height="12" viewBox="0 0 15 12" aria-hidden="true"><rect x="0.75" y="0.75" width="13.5" height="10.5" rx="2" fill="none" stroke="currentColor" stroke-width="1.3" /><rect x="1.5" y="1.5" width="3.5" height="9" fill="currentColor" /></svg></button
      >
      <button data-cmd="toggle-list" tabindex="-1" class="icon" class:on={!layout.hidden.list} aria-pressed={!layout.hidden.list} title={tip(layout.hidden.list ? 'Show the list' : 'Hide the list', 'toggle-list')} onpointerdown={keep} onclick={() => runCommand('toggle-list')}
        ><svg width="15" height="12" viewBox="0 0 15 12" aria-hidden="true"><rect x="0.75" y="0.75" width="13.5" height="10.5" rx="2" fill="none" stroke="currentColor" stroke-width="1.3" /><rect x="5" y="1.5" width="4" height="9" fill="currentColor" /></svg></button
      >
    {/if}
    <button data-cmd="pane-fullscreen" tabindex="-1" class="icon" class:on={layout.full} aria-pressed={layout.full} title={tip(layout.full ? 'Leave fullscreen: every pane again' : 'Fullscreen: only the focused pane; the pane keys switch it', 'pane-fullscreen')} onpointerdown={keep} onclick={() => runCommand('pane-fullscreen')}
      ><svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"
        ><path d={layout.full ? 'M4.5 1v3.5H1M7.5 1v3.5H11M4.5 11V7.5H1M7.5 11V7.5H11' : 'M1 4.5V1h3.5M11 4.5V1H7.5M1 7.5V11h3.5M11 7.5V11H7.5'} fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round" /></svg
      ></button
    >
  </span>
{/if}

<style>
  .panes {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    align-self: stretch;
  }
  button {
    display: inline-grid;
    place-items: center;
    height: 18px;
    padding: 0 6px;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: var(--faint);
    font: inherit;
    font-size: 11px;
    cursor: pointer;
  }
  button.icon {
    padding: 0 4px;
  }
  button:hover {
    background: var(--line);
    color: var(--text);
  }
  button.on {
    color: var(--text);
  }
  button.tab.on {
    background: var(--panel);
    box-shadow: inset 0 0 0 1px var(--line-strong);
  }
</style>
