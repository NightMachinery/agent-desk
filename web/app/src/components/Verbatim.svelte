<script>
  // The verbatim log: everything the user typed to an agent, word for word,
  // as the prompt hook recorded it, newest first. Scoped to the current
  // path; the search keeps entries holding every word.
  import { get } from '../lib/api.js';
  import { href, go } from '../lib/route.svelte.js';
  import { copy } from '../lib/format.js';
  import { toast } from '../lib/toast.svelte.js';
  import { off } from '../lib/why.js';

  let { path = '', stamp = '' } = $props();
  const PAGE = 100;

  let q = $state('');
  let items = $state(/** @type {any[]} */ ([]));
  let total = $state(0);
  let loading = $state(false);
  let error = $state('');

  let seq = 0;
  async function load(more = false) {
    const mine = ++seq;
    loading = true;
    try {
      // A refresh keeps the older pages already shown (up to the server's 500).
      const limit = more ? PAGE : Math.min(500, Math.max(PAGE, items.length));
      const res = await get('verbatim', { prefix: path, q: q.trim(), limit, offset: more ? items.length : 0 });
      if (mine !== seq) return;
      items = more ? [...items, ...res.items] : res.items;
      total = res.total;
      error = '';
    } catch (err) {
      if (mine === seq) error = err.message;
    } finally {
      if (mine === seq) loading = false;
    }
  }

  // Reload on a new scope, a new search (after a pause in typing) or a
  // change on the desk.
  let timer;
  let lastKey = '';
  $effect(() => {
    stamp;
    const query = q;
    const key = path + '\n' + query;
    if (key !== lastKey) {
      lastKey = key;
      items = [];
    }
    clearTimeout(timer);
    timer = setTimeout(() => load(), query ? 250 : 0);
    return () => clearTimeout(timer);
  });

  const day = (ts) => new Date(ts).toLocaleDateString([], { weekday: 'short', year: 'numeric', month: 'short', day: 'numeric' });
  const clock = (ts) => new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  const groups = $derived.by(() => {
    const out = [];
    for (const it of items) {
      const d = day(it.ts);
      if (!out.length || out[out.length - 1].day !== d) out.push({ day: d, items: [] });
      out[out.length - 1].items.push(it);
    }
    return out;
  });

  // The text cut into plain and matched parts, for highlighting.
  function parts(text) {
    const words = q.trim().toLowerCase().split(/\s+/).filter(Boolean);
    if (!words.length) return [{ t: text, hit: false }];
    const re = new RegExp('(' + words.map((w) => w.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('|') + ')', 'gi');
    return text.split(re).filter((t) => t !== '').map((t) => ({ t, hit: words.includes(t.toLowerCase()) }));
  }

  async function copyText(text) {
    toast((await copy(text)) ? 'Copied.' : text, 'ok');
  }
</script>

<section class="verbatim">
  <div class="bar">
    <button data-cmd="close" class="btn ghost small" onclick={() => go({ verbatim: false })}>← Back</button>
  </div>
  <h1>Verbatim log</h1>
  <p class="faint lede">
    Everything you typed to an agent, word for word, as each session recorded it.
    {#if path}Showing <a class="mono" href={href({ path, verbatim: false })}>{path}</a> only (<a href={href({ path: '', verbatim: true })}>show everywhere</a>).{:else}Showing every project.{/if}
    Agents read the same with <code>agent-desk instructions</code>.
  </p>
  <input id="verbatim-search" class="input search" type="search" placeholder="Search: every word must appear  (/)" bind:value={q} aria-label="Search the verbatim log" />
  <div class="tally faint">
    {#if error}<span class="err">{error}</span>
    {:else if loading && !items.length}Loading…
    {:else}{total} {total === 1 ? 'message' : 'messages'}{q.trim() ? ' matching' : ''}{#if items.length < total}, newest {items.length} shown{/if}{/if}
  </div>

  {#each groups as g (g.day)}
    <h2 class="day">{g.day}</h2>
    <ol class="entries">
      {#each g.items as it (it.id)}
        <li class="entry">
          <div class="head faint">
            <span>{clock(it.ts)}</span>
            {#if it.agent}<span class="agent" title={it.paseo ? 'Paseo ' + it.paseo : it.session}>{it.agent}</span>{/if}
            <a class="mono path" href={href({ path: it.path, verbatim: false })}>{it.path}</a>
            {#if it.host}<span>@{it.host}</span>{/if}
            <button data-keys="none" class="btn ghost small copy" onclick={() => copyText(it.text)} title="Copy the message">Copy</button>
          </div>
          <div class="plain-text">{#each parts(it.text) as p, i (i)}{#if p.hit}<mark>{p.t}</mark>{:else}{p.t}{/if}{/each}</div>
        </li>
      {/each}
    </ol>
  {/each}
  {#if items.length < total}
    <button class="btn small more" data-cmd="verbatim-more" {...off(undefined, [loading, 'Wait: loading'])} onclick={() => load(true)}>{loading ? 'Loading…' : 'Show older'}</button>
  {/if}
</section>

<style>
  .verbatim {
    padding: 16px 20px 40px;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  h1 {
    margin: 4px 0 0;
  }
  .lede {
    margin: 0;
    font-size: 13px;
  }
  .search {
    width: 100%;
  }
  .tally {
    font-size: 12.5px;
  }
  .err {
    color: var(--blocked);
  }
  .day {
    font-size: 12px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--muted);
    margin: 14px 0 0;
  }
  .entries {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .entry {
    border: 1px solid var(--line);
    border-radius: 8px;
    padding: 8px 10px;
    background: var(--panel);
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0 8px;
    font-size: 12px;
  }
  .agent {
    font-weight: 600;
    color: var(--text);
  }
  .path {
    overflow-wrap: anywhere;
  }
  .copy {
    margin-left: auto;
  }
  .plain-text {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    margin-top: 4px;
    font-size: 14px;
  }
  mark {
    background: var(--default-soft);
    color: inherit;
    border-radius: 2px;
  }
  .more {
    align-self: center;
  }
</style>
