// A disabled control says why it is disabled. off(title, [condition,
// reason]...) gives the attributes to spread on it: disabled while any
// condition holds, with that reason as its tooltip, else enabled with its
// usual title. One expression decides both, so the reason cannot drift from
// the condition.

/**
 * @param {string | undefined} title the tooltip while enabled
 * @param {...[unknown, string]} why condition and reason pairs, first match wins
 */
export function off(title, ...why) {
  for (const [on, reason] of why) if (on) return { disabled: true, title: reason };
  return { disabled: false, title };
}

/** The reason while a request this control started is still running. */
export const BUSY = 'Wait: the last request is still running';
