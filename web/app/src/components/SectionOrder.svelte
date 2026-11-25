<script>
  // The order of the list's sections. Every device shows the order saved here.
  import OrderCards from './OrderCards.svelte';
  import { ui, sectionNames, defaultSections, setSectionOrder } from '../lib/ui.svelte.js';

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
</script>

<OrderCards
  value={ui.sectionOrder}
  defaults={defaultSections}
  names={sectionNames}
  {about}
  {tone}
  onsave={setSectionOrder}
  cmd="section-order"
  resetCmd="section-order-reset"
  foot="Drag a card by its handle, or use the arrows. Every device shows this order."
/>
