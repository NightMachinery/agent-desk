// Replace dialog: which provider, model and effort to offer first for the
// agent that replaces one out of quota, and the profile string the launcher
// takes (provider[/model][@thinking]).
//
// A provider here is one entry of GET api/providers: {id, label, models:
// [{id, label, thinking: [...], default_thinking}], account, verdict,
// available_at}. The agent is a card of GET api/quota: {provider, model,
// thinking}. remembered maps a provider id to the {model, thinking} the
// user last started on it.

const blocked = (p) => p.verdict === 'blocked';
const offers = (p, model) => !!model && (p.models ?? []).some((m) => m.id === model);

/**
 * The provider to select first: one with quota that offers the agent's own
 * model (the same family on another account: Claude to Claude (Work)), else
 * the one the user last replaced with, if it has quota, else the first with
 * quota, else the agent's own provider, else the first.
 */
export function pickProvider(providers, agent, lastProvider = '') {
  if (!providers?.length) return '';
  const free = providers.filter((p) => !blocked(p));
  const sameModel = free.filter((p) => offers(p, agent?.model));
  // Of those, another account first: the agent's own provider is the one
  // that ran out, unless the keeper says otherwise.
  const other = sameModel.find((p) => p.id !== agent?.provider);
  if (other) return other.id;
  if (sameModel.length) return sameModel[0].id;
  const last = free.find((p) => p.id === lastProvider);
  if (last) return last.id;
  const firstFree = free.find((p) => p.id !== agent?.provider) ?? free[0];
  if (firstFree) return firstFree.id;
  return (providers.find((p) => p.id === agent?.provider) ?? providers[0]).id;
}

/** The effort to keep for model m: want if it offers it, else its default. */
export function pickThinking(m, want = '') {
  const opts = m?.thinking ?? [];
  if (!opts.length) return '';
  if (want && opts.includes(want)) return want;
  if (m.default_thinking && opts.includes(m.default_thinking)) return m.default_thinking;
  return opts[0];
}

/**
 * The model and effort for provider p: the agent's own when p offers its
 * model, else what the user last started on p, else p's first model (Paseo
 * lists the provider's preferred first) at its default effort.
 */
export function pickModel(p, agent, remembered = {}) {
  const models = p?.models ?? [];
  if (!models.length) return { model: '', thinking: '' };
  let m = models.find((x) => x.id === agent?.model);
  if (m) return { model: m.id, thinking: pickThinking(m, agent?.thinking) };
  const r = remembered[p.id];
  m = r && models.find((x) => x.id === r.model);
  if (m) return { model: m.id, thinking: pickThinking(m, r.thinking) };
  return { model: models[0].id, thinking: pickThinking(models[0]) };
}

/** The launcher's profile: provider[/model][@thinking]. */
export function profileOf(provider, model = '', thinking = '') {
  if (!provider) return '';
  return provider + (model ? '/' + model : '') + (thinking ? '@' + thinking : '');
}

/**
 * Parses a profile back (a preset from the config's [quota] profiles) into
 * {provider, model, thinking}.
 */
export function parseProfile(s = '') {
  const at = s.indexOf('@');
  const head = at < 0 ? s : s.slice(0, at);
  const thinking = at < 0 ? '' : s.slice(at + 1);
  const slash = head.indexOf('/');
  return slash < 0 ? { provider: head, model: '', thinking } : { provider: head.slice(0, slash), model: head.slice(slash + 1), thinking };
}
