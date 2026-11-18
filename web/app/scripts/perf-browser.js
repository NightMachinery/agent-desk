// Install before navigation on the synthetic fixture using shot.mjs --init-file.
window.fixtureTiming = (async () => {
  const paint = () => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  const until = predicate => new Promise(resolve => {
    const observer = new MutationObserver(check);
    function check() {
      if (predicate()) {
        observer.disconnect();
        resolve();
      }
    }
    observer.observe(document, { childList: true, subtree: true, attributes: true });
    check();
  });
  const resource = name => performance.getEntriesByType('resource').find(e => e.name.includes('/api/' + name));
  await until(() => document.querySelector('a.row'));
  await paint();
  const list = performance.now();
  const rows = [...document.querySelectorAll('a.row')];
  const thread = rows.find(row => row.textContent.includes('Review the long thread'));
  if (!thread || thread.classList.contains('unread')) throw Error('Expected the pinned, read fixture thread.');
  const assets = performance.getEntriesByType('resource').filter(e => e.name.includes('/assets/'));
  const start = performance.now();
  thread.click();
  await until(() => document.querySelectorAll('li.entry').length === 180 && document.querySelector('.detail textarea'));
  await paint();
  const detail = performance.now();
  const input = document.querySelector('.detail textarea');
  input.value = 'Fixture draft';
  input.dispatchEvent(new Event('input', { bubbles: true }));
  await until(() => !document.querySelector('.detail [data-cmd="send"]').disabled);
  await paint();
  return {
    rows: rows.length,
    assetBytes: assets.reduce((sum, entry) => sum + entry.encodedBodySize, 0),
    navigationToListMs: list,
    htmlToListMs: list - performance.getEntriesByType('navigation')[0].responseEnd,
    viewToListMs: list - resource('view').responseEnd,
    clickToThreadMs: detail - start,
    itemToComposerMs: detail - resource('item').responseEnd,
    editToPaintMs: performance.now() - detail,
  };
})();
