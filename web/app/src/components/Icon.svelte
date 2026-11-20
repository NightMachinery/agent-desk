<script>
  // Small line icons, drawn at 16 units and scaled.
  let { name, size = 16 } = $props();
  const paths = {
    pin: 'M10 2.5 13.5 6l-2.2.9-2.6 2.6-.4 2.9L3.6 7.7l2.9-.4 2.6-2.6zM6 10l-3.5 3.5',
    unread: 'M2.5 4.5h8M2.5 8h6M2.5 11.5h8M13 4.5a1.6 1.6 0 1 0 0 .01',
    read: 'M2.5 8.5 6 12l7.5-8',
    idtag: 'M2.5 4h11v8h-11zM5.5 9.5a1.5 1.5 0 1 1 0-3 1.5 1.5 0 0 1 0 3M9 7h3M9 9.5h2',
    terminal: 'M2.5 3.5h11v9h-11zM5 6.5l2 1.5-2 1.5M8.5 10h2.5',
    clip: 'M11.5 7.5 7 12a2.8 2.8 0 0 1-4-4l5-5a1.9 1.9 0 0 1 2.7 2.7L5.9 10.5a.9.9 0 0 1-1.3-1.3L9 4.8',
    prev: 'M10 3 5 8l5 5',
    next: 'M6 3l5 5-5 5',
    message: 'M2.5 3.5h11v7.5h-6l-3 2.5v-2.5h-2z',
    readall: 'M1.5 8.5l3 3 6-7M7.5 11.5l.5.5 6.5-8',
    link: 'M9 2.5h4.5V7M13.5 2.5l-6 6M12 9.5v4H2.5V4h4',
    download: 'M8 2.5v8M4.5 7 8 10.5 11.5 7M2.5 13.5h11',
  };
</script>

<svg width={size} height={size} viewBox="0 0 16 16" aria-hidden="true"
  ><path d={paths[name]} fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg
>
