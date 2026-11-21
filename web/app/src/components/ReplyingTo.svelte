<script>
  // The chip above a message box that replies to one thread entry, with a
  // button (reply-to-cancel) that goes back to the box's own target.
  import Icon from './Icon.svelte';

  let { to, back = 'the open item', oncancel } = $props();
  function jump(ev) {
    ev.preventDefault();
    document.getElementById('e-' + to.display)?.scrollIntoView({ block: 'center', behavior: 'smooth' });
  }
</script>

<div class="replying">
  <Icon name="reply" size={13} />
  <span>Replying to <a class="mono" href={'#e-' + to.display} onclick={jump}>{to.display}</a> ({to.who}){to.snippet ? ':' : ''}</span>
  <span class="faint snippet">{to.snippet}</span>
  <button class="btn ghost small" data-cmd="reply-to-cancel" title={`Stop replying to ${to.display}: the message goes to ${back} again`} aria-label="Stop replying to it" onclick={oncancel}>✕</button>
</div>

<style>
  .replying {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 6px;
    padding: 5px 8px;
    border-left: 3px solid var(--default);
    border-radius: 6px;
    background: var(--sunken);
    font-size: 12.5px;
    min-width: 0;
  }
  .snippet {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  a {
    text-decoration: underline;
  }
</style>
