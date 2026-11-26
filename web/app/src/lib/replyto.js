// Replying to one entry of a conversation (its reply button): the message
// box then sends about that entry, so the message stays in its thread and
// goes to whoever wrote it. Detail and the node panel share this.

/** What the "Replying to" chip shows for thread entry t. */
export function replyRef(t) {
  const fromAgent = t.kind === 'response' || t.kind === 'question';
  return {
    id: t.event.id,
    display: t.display_id,
    who: fromAgent ? t.by || 'its agent' : t.kind === 'answer' ? 'your answer' : 'your message',
    snippet: (t.event.title || t.event.body || '').split('\n')[0].slice(0, 100),
  };
}

/** Focuses the message box inside el (or the page's reply box) once drawn. */
export function focusBoxIn(el, after) {
  setTimeout(() => {
    const box = el?.querySelector('textarea') ?? document.getElementById('reply-box');
    box?.focus({ preventScroll: true }); // the item page docks the box in view
    box?.scrollIntoView({ block: 'nearest' });
    after?.();
  });
}
