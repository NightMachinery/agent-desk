<script>
  import { cancelArrival } from '../lib/arrival.js';
  import LazyPanel from './LazyPanel.svelte';
  import CopyIds from './CopyIds.svelte';
  import { tip } from '../lib/shortcuts.js';
  import { route, href, go, showMode, sortMode, showOver } from '../lib/route.svelte.js';
  import { agentName, copy, short } from '../lib/format.js';
  import { toast } from '../lib/toast.svelte.js';
  import { post } from '../lib/api.js';
  import ItemRow from './ItemRow.svelte';
  import TreeNode from './TreeNode.svelte';
  import Deferred from './Deferred.svelte';
  import ScreenStrip from './ScreenStrip.svelte';
  import PushNudge from './PushNudge.svelte';
  import { build } from '../lib/build.svelte.js';
  import QuotaHit from './QuotaHit.svelte';
  import TaskRows from './TaskRows.svelte';
  const NOPICK = 'Select items first (x)';
  let taskCount = $state(0);
  let taskIds = $state(/** @type {string[]} */ ([]));
  let quotaCount = $state(0);
  let quotaHidden = $state(0);
  let stoppedCount = $state(0);
  let stoppedHidden = $state(0);
  import { sel, toggleAll, clear, prune, markSelected, pinSelected, selecting, deferSelected } from '../lib/select.svelte.js';
  import { snapshot } from '../lib/nav.svelte.js';
  import { ui, toggleSection, laterOpen, toggleLater, deliverableModes, deliverableModeLabel, showDeliverables, orderedSections, taskModes, taskModeLabel, taskSpans, showTasks } from '../lib/ui.svelte.js';
  import { act, left, patched, patchedGroups, READ_LEAVES } from '../lib/pending.svelte.js';
  import { hasDraft } from '../lib/drafts.js';
  import { runCommand, keymap } from '../lib/keys.svelte.js';
  import { off } from '../lib/why.js';
  import { failed } from '../lib/errors.svelte.js';


  let { view, error, onmenu, onchange } = $props();

  // The server lists oldest first. Recent first (the default) puts each
  // group's newest item on top, and the groups of the project with the newest
  // item first; in Needs you, blocked questions still come before defaults.
  // Read order (sort=read) does the same by read time, the unread on top.
  // Keys compare as plain strings (ids and RFC 3339 times sort by time;
  // '~' sorts after both), never with localeCompare, which puts '~' first.
  const keyOf = (it) => (sortMode() === 'read' ? (it.unread || !it.read_at ? '~' : it.read_at) : it.event.id);
  const cmp = (x, y) => (x < y ? -1 : x > y ? 1 : 0);
  function recent(groups, blockedFirst) {
    const byProject = new Map();
    const rank = (it) => (blockedFirst && it.event.level === 'blocked' ? 0 : 1);
    const ranked = groups.map((group) => {
      const newest = group.items.reduce((m, it) => (keyOf(it) > m ? keyOf(it) : m), '');
      if (!byProject.has(group.project) || newest > byProject.get(group.project)) byProject.set(group.project, newest);
      const items = [...group.items].sort((a, b) => rank(a) - rank(b) || cmp(keyOf(b), keyOf(a)));
      return { group: { ...group, items }, newest };
    });
    return ranked
      .sort((a, b) => cmp(byProject.get(b.group.project), byProject.get(a.group.project)) || cmp(b.newest, a.newest))
      .map(({ group }) => group);
  }
  const sorted = (gs, blockedFirst) => (sortMode() === 'old' ? (gs ?? []) : recent(gs ?? [], blockedFirst));
  // Deferred items sit under a folded "Deferred" at the end of their
  // section, read or not: out of its badge, its mark-all and auto-navigation.
  const part = (gs, deferred) => gs.map((g) => ({ ...g, items: g.items.filter((it) => !!it.deferred === deferred) })).filter((g) => g.items.length);
  // Each section shows the user's changes the desk has not shown yet
  // (pending.svelte.js): an item answered, read, deferred or pinned looks so
  // at once, and an answered question leaves Needs you at once.
  const section = (gs, name, blockedFirst) => patchedGroups(sorted(gs, blockedFirst), name);
  const needsAll = $derived(section(view?.view?.needs_you, 'needs', true));
  const freshAll = $derived(section(view?.view?.new, 'new', false));
  const responsesAll = $derived(section(view?.view?.responses, 'responses', false));
  const deliverablesAll = $derived(section(view?.view?.deliverables, 'deliverables', false));
  const needs = $derived(part(needsAll, false));
  const fresh = $derived(part(freshAll, false));
  const responses = $derived(part(responsesAll, false));
  const deliverables = $derived(part(deliverablesAll, false));
  const later = $derived({
    needs: part(needsAll, true),
    new: part(freshAll, true),
    responses: part(responsesAll, true),
    deliverables: part(deliverablesAll, true),
  });
  // Pinned: an item pinned here joins it from its section at once, one
  // unpinned leaves it.
  const pinned = $derived.by(() => {
    const own = (view?.view?.pinned ?? []).filter((it) => !left(it.display_id, 'pinned')).map((it) => patched(it)).filter((it) => it.pinned !== false);
    const ids = new Set(own.map((it) => it.display_id));
    const joined = [needsAll, freshAll, responsesAll, deliverablesAll].flat().flatMap((g) => g.items).filter((it) => it.pinned && !ids.has(it.display_id) && ids.add(it.display_id));
    return [...own, ...joined];
  });
  const deliverablesAny = $derived(view?.view?.deliverables_any ?? 0);
  const sectionOrder = $derived(orderedSections(ui.sectionOrder));
  const runs = $derived(view?.view?.runs ?? []);
  const count = (gs) => gs.reduce((n, g) => n + g.items.length, 0);
  // A section's badge counts what is unread: an item read a moment ago
  // stays listed (until you move on, or with Show: All) but no longer counts.
  const unreadCount = (gs) => gs.reduce((n, g) => n + g.items.filter((it) => it.unread).length, 0);
  const idsOf = (gs) => gs.flatMap((g) => g.items.map((it) => it.display_id));
  const needIds = $derived(idsOf(needs));
  const freshIds = $derived(idsOf(fresh));
  const responseIds = $derived(idsOf(responses));
  const deliverableIds = $derived(idsOf(deliverables));
  const pinnedIds = $derived(pinned.map((it) => it.display_id));
  const unreadOf = (gs) => gs.flatMap((g) => g.items.filter((it) => it.unread).map((it) => it.display_id));
  const freshUnread = $derived(unreadOf(fresh));
  const responseUnread = $derived(unreadOf(responses));
  const deliverableUnread = $derived(unreadOf(deliverables));

  // Marks a whole section read, with Undo. Not offered on Needs you: reading
  // a question there dismisses it, too much for one unguarded click.
  // An item with a draft is left unread (and listed) until the draft is sent
  // or discarded.
  const byId = $derived(new Map([...needs, ...deliverables, ...responses, ...fresh, ...Object.values(later).flat()].flatMap((g) => g.items).concat(pinned).map((it) => [it.display_id, it])));
  const drafted = (id) => !!byId.get(id)?.draft || hasDraft(id);
  async function markAllRead(all) {
    const kept = all.filter(drafted);
    const targets = all.filter((t) => !kept.includes(t));
    const keptNote = kept.length ? ` ${kept.length} with a draft stayed unread.` : '';
    if (!targets.length) return toast(`${kept.length === 1 ? 'It has a draft' : 'They have drafts'}: send or discard first.`, 'info');
    targets.forEach((t) => cancelArrival(t));
    // All and Last hour keep read items listed; Unread drops them at once.
    const leave = route.all || route.hour ? [] : READ_LEAVES;
    try {
      await act(targets, { unread: false }, leave, () => post('read', { targets }));
      toast(`${targets.length} ${targets.length === 1 ? 'item' : 'items'} marked read.${keptNote}`, 'ok', 7000, {
        label: 'Undo',
        run: async () => {
          try {
            await act(targets, { unread: true }, [], () => post('unread', { targets }));
            onchange?.();
          } catch (err) {
            failed(err);
          }
        },
      });
      onchange?.();
    } catch (err) {
      failed(err);
    }
  }
  // Folded sections drop out of j/k and previous/next, and so do the
  // Deferred under a section while folded (as they are until opened).
  const shown = (name, ids) => (ui.collapsed[name] ? [] : [...ids, ...(laterOpen(name) ? idsOf(later[name] ?? []) : [])]);
  const allIds = $derived([
    ...new Set(sectionOrder.flatMap((n) => shown(n, { needs: needIds, deliverables: deliverableIds, pinned: pinnedIds, responses: responseIds, new: freshIds, tasks: taskIds }[n] ?? []))),
  ]);
  // Pinned items in any section, including a pinned question under Needs.
  const pinnedAll = $derived(
    new Set([...pinnedIds, ...[...needs, ...deliverables, ...responses, ...fresh].flatMap((g) => g.items.filter((it) => it.pinned).map((it) => it.display_id))]),
  );
  // Open: what is still waiting (Needs you, and unread items elsewhere).
  const openAll = $derived(new Set([...needIds, ...freshUnread, ...responseUnread, ...deliverableUnread, ...pinned.filter((it) => it.unread).map((it) => it.display_id)]));
  // What auto-navigation ranks each item by (moveon.js): its kind, from its
  // section (a question by its level), its time and the desk's view count.
  const infoAll = $derived.by(() => {
    const m = new Map();
    const add = (groups, kindOf) => {
      for (const g of groups)
        for (const it of g.items)
          if (!m.has(it.display_id)) m.set(it.display_id, { kind: kindOf(it), ts: Date.parse(it.event.ts), views: it.views ?? 0 });
    };
    add(needs, (it) => (it.event.level === 'blocked' ? 'blocked' : 'default'));
    add(deliverables, () => 'deliverable');
    add(responses, () => 'response');
    add(fresh, () => 'notice');
    return m;
  });
  // Previous and next walk the list in the order shown.
  $effect(() => snapshot(allIds, route.item, pinnedAll, openAll, infoAll));
  const pickedPinned = $derived(pinnedIds.filter((x) => sel.ids.has(x)).length);
  const pickedDeferred = $derived([...sel.ids].filter((x) => byId.get(x)?.deferred).length);
  const allOn = (ids) => ids.length > 0 && ids.every((x) => sel.ids.has(x));
  $effect(() => prune(allIds));
  const picked = $derived(sel.ids.size);
  const pickedQuestions = $derived(needs.flatMap((g) => g.items).filter((it) => !it.dismissed && sel.ids.has(it.display_id)).length);
  const crumbs = $derived(route.path ? route.path.split('/').map((name, i, all) => ({ name, path: all.slice(0, i + 1).join('/') })) : []);
  // Under a run or deeper, group headers repeat the scope; drop them.
  const deep = $derived(crumbs.length >= 2);
  // What a row's lineage leaves out: its group heading, or the scope when
  // headings are dropped.
  const baseOf = (g) => (deep ? route.path : `${g.project}/${g.run}`);

  let q = $state(route.q);
  $effect(() => {
    q = route.q;
  });
  let typing;
  function onsearch() {
    clearTimeout(typing);
    typing = setTimeout(() => go({ q: q.trim(), item: '' }, { replace: true }), 220);
  }
</script>

<header>
  <div class="top">
    <button data-keys="none" class="btn ghost menu" aria-label="Menu" onclick={onmenu}>
      <svg width="18" height="18" viewBox="0 0 18 18"><path d="M2 4.5h14M2 9h14M2 13.5h14" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" /></svg>
    </button>
    <nav class="crumbs" aria-label="Scope">
      <a href={href({ path: '', item: '' })} class:here={!crumbs.length}>Everything</a>
      {#each crumbs as c, i (c.path)}
        <span class="sep">/</span>
        <a href={href({ path: c.path, item: '' })} class:here={i === crumbs.length - 1}>{c.name}</a>
      {/each}
    </nav>
    <!-- The Tasks page here, and a new task, from any list. -->
    <div class="tasks-seg" role="group" aria-label="Tasks">
      <a data-cmd="tasks" href={href({ item: '', tasks: true })} class:on={route.tasks} title={tip('The Tasks page: the checklist here, finished tasks too', 'tasks')}>Tasks</a>
      <button data-cmd="task-new" aria-label="New task" title={tip('File a new task, bug or idea', 'task-add')} onclick={() => runCommand('task-add')}
        ><svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><path d="M6 1.5v9M1.5 6h9" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" /></svg></button
      >
    </div>
  </div>
  <div class="filters">
    <label class="search">
      <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true"><circle cx="7" cy="7" r="5" fill="none" stroke="currentColor" stroke-width="1.6" /><path d="m11 11 3.5 3.5" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" /></svg>
      <input id="search" type="search" placeholder="Search items and runs  (/)" bind:value={q} oninput={onsearch} autocomplete="off" />
    </label>
    <!-- One pill, two menus: which items the list shows, and their order. -->
    <div class="listpick" class:wide={showMode() !== 'unread'} role="group" aria-label="What the list shows, and in what order">
      <label class="seg" title={tip('Which items to list: the unread; the unread and those you read in the last hour; or every one, finished runs too', 'toggle-all')}>
        <select data-cmd="list-show" aria-label="Which items to list" value={showMode()} onchange={(e) => go({ ...showOver(e.currentTarget.value), item: '' })}>
          <option value="unread">Unread</option>
          <option value="hour">+ Last hour</option>
          <option value="all">All</option>
        </select>
        <svg class="chev-down" width="9" height="9" viewBox="0 0 10 10" aria-hidden="true"><path d="m2 3.5 3 3 3-3" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
      </label>
      <span class="seg-line" aria-hidden="true"></span>
      <label class="seg" title={tip(showMode() === 'unread' ? 'Order: newest or oldest first. Sorting by read time needs read items listed (+ Last hour or All)' : 'Order: newest or oldest first, or most recently read first (the unread on top)', 'toggle-old')}>
        <svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true"
          ><path d={sortMode() === 'old' ? 'M5 13V3M2.5 5.5 5 3l2.5 2.5' : 'M5 3v10M2.5 10.5 5 13l2.5-2.5'} fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /><path
            d="M9.5 4.5h4M9.5 8h3M9.5 11.5h2"
            stroke="currentColor"
            stroke-width="1.6"
            stroke-linecap="round"
          /></svg
        >
        <select data-cmd="list-sort" aria-label="Order of the list" value={sortMode()} onchange={(e) => go({ sort: e.currentTarget.value }, { replace: true })}>
          <option value="">Newest</option>
          <option value="old">Oldest</option>
          <option value="read" disabled={showMode() === 'unread'}>Last read</option>
        </select>
        <svg class="chev-down" width="9" height="9" viewBox="0 0 10 10" aria-hidden="true"><path d="m2 3.5 3 3 3-3" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
      </label>
    </div>
    <button data-cmd="select-mode" title={tip('Select items', 'select-mode')} class="btn small" class:on={selecting()} onclick={() => (selecting() ? clear() : (sel.mode = true))}>{selecting() ? 'Done' : 'Select'}</button>
  </div>
  {#if route.session}
    {@const s = view?.session}
    <div class="session">
      <div>
        <div class="who">
          {agentName(s?.agent)} session <span class="mono">{short(route.session)}</span>
        </div>
        {#if s?.node}<div class="faint">{s.node}{s.last_age ? ` · last wrote ${s.last_age} ago` : ''}{s.host && s.host !== view?.host ? ` · on ${s.host}` : ''}</div>{/if}
      </div>
      <div class="actions">
        <CopyIds paseo={s?.paseo} resume={s?.resume} keys={false} />
        <a class="btn small" href={href({ session: '', item: '' })}>Clear</a>
      </div>
    </div>
  {/if}
</header>

<div class="body" class:selecting={selecting()}>
  {#if error}
    <div class="empty error">Cannot reach the desk: {error}</div>
  {:else if !view}
    <div class="skeleton"></div>
    <div class="skeleton short"></div>
  {:else}
    {#if view.node}
      <div class="phone-node"><LazyPanel view={import('./NodePanel.svelte')} node={view.node} target={view.target} conversations={view.conversations ?? []} stamp={view.stamp} {onchange} compact /></div>
    {/if}

    {#if build.stale}
      <div class="update" role="status">
        <span>The dashboard has been updated; this page still runs the old version.</span>
        <button class="btn small primary" data-cmd="reload-page" onclick={() => location.reload()} title={tip('Reload the page; drafts are kept', 'reload-page')}>Reload</button>
      </div>
    {/if}
    {#if keymap.error && keymap.dismissed !== keymap.error}
      <div class="update keys-bad" role="alert">
        <span>Your keys file <span class="mono">{keymap.path}</span> was not applied, so the built-in keys are in use. {keymap.error}</span>
        <button class="btn small ghost" data-keys="none" onclick={() => (keymap.dismissed = keymap.error)}>Dismiss</button>
      </div>
    {/if}
    <PushNudge />
    {#if view.screens?.length}<ScreenStrip screens={view.screens} host={view.host} {onchange} />{/if}

    {#each sectionOrder as name (name)}
      {#if name === 'needs'}
    <h2 class="section-title" data-section="needs">
      {@render fold('needs', 'Needs you')}
      {#if count(needs)}<span class="count hot">{count(needs)}</span>{/if}
      {#if needIds.length > 1 && !ui.collapsed.needs}<button data-keys="none" class="pick" onclick={() => toggleAll(needIds)}>{allOn(needIds) ? 'Deselect' : 'Select all'}</button>{/if}
    </h2>
    {#if !ui.collapsed.needs}
      {#each needs as g (g.project + '/' + g.run)}
        {@render groupHead(g)}
        <div class="rows">
          {#each g.items as it (it.event.id)}<ItemRow item={it} base={baseOf(g)} />{/each}
        </div>
      {:else}
        <div class="empty">Nothing is waiting on you{route.q ? ' that matches' : ''}.</div>
      {/each}
      {@render deferredPart('needs')}
    {/if}
      {:else if name === 'stopped'}
    <div class:hidden={!stoppedCount && !stoppedHidden}>
      <h2 class="section-title" data-section="stopped">
        {@render fold('stopped', 'Stopped by an error')}
        <span class="count stopped">{stoppedCount}</span>
      </h2>
      <QuotaHit why="error" stamp={view.stamp} path={route.path} collapsed={!!ui.collapsed.stopped} bind:count={stoppedCount} bind:hiddenCount={stoppedHidden} />
    </div>
      {:else if name === 'quota'}
    <div class:hidden={!quotaCount && !quotaHidden}>
      <h2 class="section-title" data-section="quota">
        {@render fold('quota', 'Out of quota')}
        <span class="count">{quotaCount}</span>
      </h2>
      <QuotaHit stamp={view.stamp} path={route.path} collapsed={!!ui.collapsed.quota} bind:count={quotaCount} bind:hiddenCount={quotaHidden} />
    </div>
      {:else if name === 'deliverables'}
    {#if deliverables.length || deliverablesAny || later.deliverables.length}
      <h2 class="section-title" data-section="deliverables">
        {@render fold('deliverables', 'Deliverables')}
        {#if unreadCount(deliverables)}<span class="count deliverable">{unreadCount(deliverables)}</span>{/if}
        <label class="show-pick" title={tip('Which deliverables to list: the newest of each, unread; the newest of each; or every one, replaced ones too', 'deliverables-show')}>
          <select data-cmd="deliverables-show" aria-label="Which deliverables to list" value={ui.deliverablesShow} onchange={(e) => showDeliverables(e.currentTarget.value)}>
            {#each deliverableModes as m (m)}<option value={m}>{deliverableModeLabel[m]}</option>{/each}
          </select>
          <svg width="9" height="9" viewBox="0 0 10 10" aria-hidden="true"><path d="m2 3.5 3 3 3-3" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
        </label>
        {#if deliverableIds.length > 1 && !ui.collapsed.deliverables}<button data-keys="none" class="pick" onclick={() => toggleAll(deliverableIds)}>{allOn(deliverableIds) ? 'Deselect' : 'Select all'}</button>{/if}
        {#if deliverableUnread.length}{@render markAll(deliverableUnread)}{/if}
      </h2>
      {#if !ui.collapsed.deliverables}
        {#each deliverables as g (g.project + '/' + g.run)}
          {@render groupHead(g, true)}
          <div class="rows">
            {#each g.items as it (it.event.id)}<ItemRow item={it} base={baseOf(g)} />{/each}
          </div>
        {:else}
          <div class="empty">
            {#if route.q}No deliverable matches.{:else if ui.deliverablesShow === 'unread'}Nothing new. <button data-cmd="deliverables-latest" class="linkish" onclick={() => showDeliverables('latest')}>Latest</button> lists the ones you have seen.{:else}No deliverables here.{/if}
          </div>
        {/each}
        {@render deferredPart('deliverables')}
      {/if}
    {/if}
      {:else if name === 'pinned'}
    {#if pinned.length}
      <h2 class="section-title" data-section="pinned">
        {@render fold('pinned', 'Pinned')}
        <span class="count">{pinned.length}</span>
      </h2>
      {#if !ui.collapsed.pinned}
        <div class="rows">
          {#each pinned as it (it.event.id)}<ItemRow item={it} base={route.path} />{/each}
        </div>
      {/if}
    {/if}
      {:else if name === 'responses'}
    {#if responses.length || later.responses.length}
      <h2 class="section-title" data-section="responses">
        {@render fold('responses', 'Responses to you')}
        {#if unreadCount(responses)}<span class="count response">{unreadCount(responses)}</span>{/if}
        {#if responseIds.length > 1 && !ui.collapsed.responses}<button data-keys="none" class="pick" onclick={() => toggleAll(responseIds)}>{allOn(responseIds) ? 'Deselect' : 'Select all'}</button>{/if}
        {#if responseUnread.length}{@render markAll(responseUnread)}{/if}
      </h2>
      {#if !ui.collapsed.responses}
        {#each responses as g (g.project + '/' + g.run)}
          {@render groupHead(g, true)}
          <div class="rows">
            {#each g.items as it (it.event.id)}<ItemRow item={it} base={baseOf(g)} />{/each}
          </div>
        {/each}
        {@render deferredPart('responses')}
      {/if}
    {/if}
      {:else if name === 'new'}
    <h2 class="section-title" data-section="new">
      {@render fold('new', route.all ? 'Notices and slots' : 'New')}
      {#if unreadCount(fresh)}<span class="count">{unreadCount(fresh)}</span>{/if}
      {#if freshIds.length > 1 && !ui.collapsed.new}<button data-keys="none" class="pick" onclick={() => toggleAll(freshIds)}>{allOn(freshIds) ? 'Deselect' : 'Select all'}</button>{/if}
      {#if freshUnread.length}{@render markAll(freshUnread)}{/if}
    </h2>
    {#if !ui.collapsed.new}
      {#each fresh as g (g.project + '/' + g.run)}
        {@render groupHead(g, true)}
        <div class="rows">
          {#each g.items as it (it.event.id)}<ItemRow item={it} base={baseOf(g)} />{/each}
        </div>
      {:else}
        <div class="empty">{route.all ? 'No notices here.' : 'All read.'}</div>
      {/each}
      {@render deferredPart('new')}
    {/if}
      {:else if name === 'tasks'}
    <h2 class="section-title" data-section="tasks">
      {@render fold('tasks', 'Tasks')}
      {#if taskCount}<span class="count">{taskCount}</span>{/if}
      <label class="show-pick tasks-pick" title={tip('Which tasks to list: the open ones always, and with a span also those finished or dropped in it', 'tasks-show')}>
        <select data-cmd="tasks-show" aria-label="Which tasks to list" value={ui.tasksShow} onchange={(e) => showTasks(e.currentTarget.value)}>
          {#each taskModes as m (m)}<option value={m}>{taskModeLabel[m]}</option>{/each}
        </select>
        <svg width="9" height="9" viewBox="0 0 10 10" aria-hidden="true"><path d="m2 3.5 3 3 3-3" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
      </label>
      <span class="heads">
        <button data-cmd="task-new" class="pick" title={tip('File a new task, bug or idea', 'task-add')} onclick={() => runCommand('task-add')}>New</button>
        <a class="pick" data-cmd="tasks" href={href({ item: '', tasks: true })} title={tip('The Tasks page: finished tasks too, filters, subtasks', 'tasks')}>All tasks</a>
      </span>
    </h2>
    <TaskRows path={route.path} stamp={view.stamp} base={route.path} collapsed={!!ui.collapsed.tasks} span={taskSpans[ui.tasksShow]} bind:count={taskCount} bind:ids={taskIds} {onchange} />
      {:else if name === 'runs'}
    <h2 class="section-title" data-section="runs">{@render fold('runs', 'Runs')}</h2>
    {#if ui.collapsed.runs}
      <!-- folded -->
    {:else if runs.length}
      <div class="tree">
        {#each runs as n (n.path)}<TreeNode node={n} />{/each}
      </div>
    {:else if route.q}
      <div class="empty">No project, run or agent matches{route.all ? '' : '; Show: All includes finished runs'}.</div>
    {:else}
      <div class="empty">No runs{route.all ? '' : ' with anything to show; Show: All includes finished ones'}.</div>
    {/if}
      {:else if name === 'deferred'}
    {#if view.deferred?.length}
      <h2 class="section-title" data-section="deferred">
        {@render fold('deferred', 'Deferred for quota')}
        <span class="count">{view.deferred.length}</span>
      </h2>
      {#if !ui.collapsed.deferred}
        <Deferred tasks={view.deferred} launch={view.launch} base={route.path} {onchange} />
      {/if}
    {/if}
      {/if}
    {/each}
  {/if}
</div>

{#if selecting()}
  <div class="bulk" role="toolbar" aria-label="Selected items">
    <button data-cmd="select-clear" class="btn small ghost x" aria-label="Clear the selection" title="Clear (Esc)" onclick={clear}>✕</button>
    <span class="n">{#if !picked}Tap items to select them{:else}<strong>{picked}</strong> selected{/if}{pickedQuestions ? ` · ${pickedQuestions} ${pickedQuestions === 1 ? 'question' : 'questions'} to dismiss` : ''}</span>
    {#if !allOn(allIds)}<button data-cmd="select-all" class="btn small ghost" onclick={() => toggleAll(allIds)}>All {allIds.length}</button>{/if}
    <button data-cmd="bulk-pin" class="btn small" {...off(undefined, [!picked, NOPICK])} onclick={() => pinSelected(pickedPinned < picked, onchange)}
      >{picked && pickedPinned === picked ? 'Unpin' : 'Pin'}</button
    >
    <button data-cmd="bulk-defer" class="btn small" {...off(undefined, [!picked, NOPICK])} onclick={() => deferSelected(pickedDeferred < picked, onchange)}
      >{picked && pickedDeferred === picked ? 'Undefer' : 'Defer'}</button
    >
    <button data-cmd="bulk-unread" class="btn small" {...off(undefined, [!picked, NOPICK])} onclick={() => markSelected(false, onchange)}>Mark unread</button>
    <button
      data-cmd="bulk-read"
      class="btn small primary"
      {...off(pickedQuestions ? 'Questions leave Needs you but stay open, so you can still answer them from Show: All' : '', [!picked, NOPICK])}
      onclick={() => markSelected(true, onchange, pickedQuestions)}>Mark read</button
    >
  </div>
{/if}

{#snippet markAll(ids, under = '')}
  <button class="mark-all" class:sub={!!under} data-cmd={under ? undefined : 'mark-all-read'}
    title={under ? `Mark the ${ids.length} unread in ${under} read` : `Mark all ${ids.length} read`}
    aria-label={under ? `Mark the ${ids.length} unread in ${under} read` : `Mark all ${ids.length} read`}
    onclick={(e) => { e.stopPropagation(); markAllRead(ids); }}>
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true"><path d="M1.5 8.5l3 3 6-7M7.5 11.5l.5.5 6.5-8" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
  </button>
{/snippet}

{#snippet deferredPart(name)}
  {@const gs = later[name] ?? []}
  {#if gs.length}
    {@const open = laterOpen(name)}
    <h3 class="later-title" data-later={name}>
      <button data-keys="none" class="fold" aria-expanded={open} onclick={() => toggleLater(name)} title={tip(open ? 'Fold the deferred items' : 'Show the deferred items: kept here, read or not, until you undefer them', 'deferred-fold')}>
        <svg class="chev" class:shut={!open} width="10" height="10" viewBox="0 0 12 12" aria-hidden="true"><path d="m3 4.5 3 3 3-3" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" /></svg>
        Deferred
      </button>
      <span class="count">{count(gs)}</span>
    </h3>
    {#if open}
      {#each gs as g (g.project + '/' + g.run)}
        {@render groupHead(g)}
        <div class="rows">
          {#each g.items as it (it.event.id)}<ItemRow item={it} base={baseOf(g)} />{/each}
        </div>
      {/each}
    {/if}
  {/if}
{/snippet}

{#snippet fold(name, label)}
  <button class="fold" data-cmd={'fold-' + name} aria-expanded={!ui.collapsed[name]} onclick={() => toggleSection(name)} title={tip(ui.collapsed[name] ? 'Show this section' : 'Fold this section', 'fold-' + name)}>
    <svg class="chev" class:shut={ui.collapsed[name]} width="11" height="11" viewBox="0 0 12 12" aria-hidden="true"><path d="m3 4.5 3 3 3-3" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" /></svg>
    {label}
  </button>
{/snippet}

{#snippet groupHead(g, readable = false)}
  {@const ids = g.items.map((it) => it.display_id)}
  {@const unread = readable && !deep ? g.items.filter((it) => it.unread).map((it) => it.display_id) : []}
  <div class="group-line">
    {#if !deep}<a class="group" href={href({ path: `${g.project}/${g.run}`, item: '' })}>{g.project} <span class="faint">/</span> {g.run}</a>{:else}<span></span>{/if}
    {#if ids.length > 1 && (!deep || selecting())}<button data-keys="none" class="pick" onclick={() => toggleAll(ids)}>{allOn(ids) ? 'Deselect' : 'Select'} {ids.length}</button>{/if}
    {#if unread.length}{@render markAll(unread, `${g.project}/${g.run}`)}{/if}
  </div>
{/snippet}

<style>
  .hidden {
    display: none;
  }
  .later-title {
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 14px 0 6px;
    font-size: 12px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: var(--muted);
  }
  .later-title .fold {
    font: inherit;
    color: inherit;
    text-transform: inherit;
    letter-spacing: inherit;
  }
  header {
    position: sticky;
    top: 0;
    z-index: 5;
    background: color-mix(in srgb, var(--bg) 88%, transparent);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    border-bottom: 1px solid var(--line);
    padding: max(10px, env(safe-area-inset-top)) 18px 10px;
  }
  .top {
    display: flex;
    align-items: center;
    gap: 6px;
    min-height: 32px;
  }
  .tasks-seg {
    display: inline-flex;
    flex: none;
    margin-left: auto;
    height: 28px;
    border: 1px solid var(--line-strong);
    border-radius: 999px;
    background: var(--panel);
    overflow: hidden;
  }
  .tasks-seg a,
  .tasks-seg button {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: 0;
    background: none;
    color: var(--text);
    font-size: 13px;
    font-weight: 500;
    text-decoration: none;
    cursor: pointer;
  }
  .tasks-seg a {
    padding: 0 10px 0 12px;
  }
  .tasks-seg button {
    width: 32px;
    border-left: 1px solid var(--line-strong);
  }
  .tasks-seg a:hover:not(:disabled),
  .tasks-seg button:hover:not(:disabled) {
    background: var(--sunken);
  }
  .tasks-seg a.on {
    background: var(--text);
    color: var(--panel);
  }
  .menu {
    display: none;
    padding: 0 7px;
    margin-left: -7px;
  }
  .crumbs {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
    font-size: 15px;
    font-weight: 600;
  }
  .crumbs a {
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .crumbs a:hover:not(:disabled),
  .crumbs a.here {
    color: var(--text);
  }
  .sep {
    color: var(--faint);
  }
  .filters {
    display: flex;
    gap: 8px;
    margin-top: 8px;
  }
  .search {
    flex: 1;
    display: flex;
    align-items: center;
    gap: 7px;
    height: 32px;
    padding: 0 10px;
    border-radius: 8px;
    border: 1px solid var(--line-strong);
    background: var(--panel);
    color: var(--faint);
  }
  .search:focus-within {
    border-color: var(--default);
    box-shadow: 0 0 0 3px var(--focus);
  }
  .search input {
    flex: 1;
    min-width: 0;
    border: 0;
    outline: 0;
    background: transparent;
    color: var(--text);
  }
  /* The list's Show and Sort menus: one pill, two native selects (the
     phone's own picker), each with its chevron. */
  .listpick {
    display: inline-flex;
    align-items: stretch;
    height: 32px;
    border: 1px solid var(--line-strong);
    border-radius: 999px;
    background: var(--panel);
    flex: none;
  }
  .listpick.wide {
    border-color: var(--text);
  }
  .listpick .seg {
    position: relative;
    display: inline-flex;
    align-items: center;
    gap: 5px;
    padding: 0 10px 0 12px;
    border-radius: 999px;
    cursor: pointer;
  }
  .listpick .seg:hover:not(:disabled) {
    background: var(--sunken);
  }
  .listpick .seg:focus-within {
    box-shadow: 0 0 0 3px var(--focus);
  }
  .listpick select {
    appearance: none;
    -webkit-appearance: none;
    field-sizing: content;
    border: 0;
    background: none;
    color: var(--text);
    font: inherit;
    font-size: 13px;
    font-weight: 500;
    padding: 0 14px 0 0;
    height: 100%;
    cursor: pointer;
    outline: 0;
  }
  .listpick .chev-down {
    position: absolute;
    right: 10px;
    pointer-events: none;
    color: var(--muted);
  }
  .listpick.wide .seg:first-child select {
    font-weight: 650;
  }
  .seg-line {
    width: 1px;
    margin: 7px 0;
    background: var(--line-strong);
  }
  .session {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 10px;
    margin-top: 10px;
    padding: 9px 12px;
    border-radius: 10px;
    background: var(--default-soft);
    font-size: 13px;
  }
  .who {
    font-weight: 600;
  }
  .body {
    padding: 4px 18px 60px;
  }
  .fold {
    all: unset;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    gap: 6px;
    border-radius: 4px;
  }
  .fold:hover:not(:disabled) {
    color: var(--text);
  }
  .fold:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .chev {
    transition: transform 0.14s;
  }
  .chev.shut {
    transform: rotate(-90deg);
  }
  .group-line {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }
  /* The Deliverables section's "show" menu: a quiet pill that reads as part
     of the heading until hovered. */
  .show-pick {
    --tone: var(--deliverable);
    --tone-soft: var(--deliverable-soft);
    position: relative;
    display: inline-flex;
    align-items: center;
    text-transform: none;
    letter-spacing: 0;
  }
  .show-pick select {
    appearance: none;
    -webkit-appearance: none;
    field-sizing: content;
    height: 22px;
    padding: 0 20px 0 9px;
    border: 1px solid color-mix(in srgb, var(--tone) 35%, var(--line));
    border-radius: 999px;
    background: var(--tone-soft);
    color: var(--tone);
    font: inherit;
    font-size: 11.5px;
    font-weight: 600;
    cursor: pointer;
  }
  .show-pick select:hover:not(:disabled),
  .show-pick select:focus-visible {
    border-color: var(--tone);
    outline: none;
  }
  .show-pick svg {
    position: absolute;
    right: 7px;
    pointer-events: none;
    color: var(--tone);
  }
  .tasks-pick {
    --tone: var(--ok);
    --tone-soft: var(--ok-soft);
  }
  .linkish {
    padding: 0;
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    text-decoration: underline;
    cursor: pointer;
  }
  .group-line .pick {
    opacity: 0;
    margin-left: auto;
  }
  /* A group's mark-read sits under its section's, a step quieter. */
  .mark-all.sub {
    color: var(--faint);
  }
  .group-line:hover:not(:disabled) .pick,
  .group-line .pick:focus-visible {
    opacity: 1;
  }
  .mark-all {
    margin-left: auto;
    display: inline-grid;
    place-items: center;
    width: 26px;
    height: 24px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--muted);
    cursor: pointer;
  }
  .pick + .mark-all {
    margin-left: 2px;
  }
  .mark-all:hover:not(:disabled) {
    background: var(--panel);
    color: var(--text);
  }
  .heads {
    display: inline-flex;
    gap: 2px;
    margin-left: auto;
  }
  .heads .pick {
    margin-left: 0;
    text-decoration: none;
  }
  .pick {
    margin-left: auto;
    padding: 2px 6px;
    border: 0;
    border-radius: 6px;
    background: transparent;
    color: var(--muted);
    font-size: 11.5px;
    font-weight: 600;
    letter-spacing: 0;
    text-transform: none;
    transition: opacity 0.12s, background 0.12s;
  }
  .pick:hover:not(:disabled) {
    background: var(--sunken);
    color: var(--text);
  }
  .bulk {
    position: sticky;
    bottom: max(12px, env(safe-area-inset-bottom));
    z-index: 6;
    display: flex;
    align-items: center;
    gap: 6px;
    margin: 0 14px;
    padding: 8px;
    border-radius: 12px;
    background: var(--panel);
    border: 1px solid var(--line-strong);
    box-shadow: var(--shadow);
    animation: rise 0.16s ease-out;
  }
  .bulk .x {
    padding: 0 8px;
  }
  .bulk .n {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 13px;
    color: var(--muted);
  }
  .bulk .n strong {
    color: var(--text);
  }
  @keyframes rise {
    from {
      transform: translateY(8px);
    }
  }
  .selecting .group-line .pick {
    opacity: 1;
  }
  .btn.on {
    background: var(--text);
    border-color: var(--text);
    color: var(--panel);
  }
  .icon {
    padding: 0 9px;
    height: 32px;
  }
  .filters > .btn {
    height: 32px;
  }
  @media (max-width: 600px) {
    .filters {
      flex-wrap: wrap;
      justify-content: flex-end;
    }
    .search {
      flex-basis: 100%;
    }
  }
  .group {
    display: block;
    margin: 12px 2px 6px;
    font-size: 12.5px;
    font-weight: 600;
    color: var(--muted);
  }
  .group:hover:not(:disabled) {
    color: var(--text);
  }
  .rows {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .tree {
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    padding: 4px;
  }
  .phone-node {
    display: none;
  }
  .error {
    color: var(--blocked);
    border-color: var(--blocked);
    margin-top: 16px;
  }
  .skeleton {
    height: 64px;
    margin-top: 16px;
    border-radius: var(--radius);
    background: linear-gradient(90deg, var(--sunken), var(--line), var(--sunken));
    background-size: 200% 100%;
    animation: shimmer 1.2s infinite;
  }
  .skeleton.short {
    width: 70%;
  }
  @keyframes shimmer {
    to {
      background-position: -200% 0;
    }
  }
  @media (max-width: 900px) {
    .menu {
      display: inline-flex;
    }
    header {
      padding-left: 14px;
      padding-right: 14px;
    }
    .body {
      padding: 4px 14px 60px;
    }
    .phone-node {
      display: block;
      margin-top: 12px;
    }
  }
  .update {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
    margin: 0 0 10px;
    padding: 8px 10px;
    border: 1px solid var(--line-strong);
    border-radius: var(--radius);
    background: var(--accent-soft);
    font-size: 13px;
  }
  .keys-bad {
    background: var(--blocked-soft);
  }
</style>
