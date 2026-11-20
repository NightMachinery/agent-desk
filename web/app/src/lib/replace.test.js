import { test } from 'node:test';
import assert from 'node:assert/strict';
import { pickProvider, pickModel, pickThinking, profileOf, parseProfile } from './replace.js';

const opus = { id: 'claude-opus-5-5', thinking: ['low', 'medium', 'high'], default_thinking: 'medium' };
const fable = { id: 'claude-fable-5-2', thinking: ['low', 'high'], default_thinking: 'high' };
const sol = { id: 'gpt-6.2-sol', thinking: ['low', 'high', 'xhigh'], default_thinking: 'high' };
const claude = { id: 'claude', models: [opus, fable], verdict: 'blocked' };
const work = { id: 'work', models: [opus, fable], verdict: 'usable' };
const codex = { id: 'codex', models: [sol], verdict: 'usable' };
const agy = { id: 'agy', models: [] };
const agent = { provider: 'claude', model: 'claude-opus-5-5', thinking: 'high' };

test('the same family on another account with quota comes first', () => {
  assert.equal(pickProvider([claude, codex, work], agent), 'work');
});

test("the agent's own provider when it is the only one with its model and has quota", () => {
  assert.equal(pickProvider([{ ...claude, verdict: 'usable' }, codex], agent), 'claude');
});

test('then the last used provider with quota, then the first with quota', () => {
  const blockedWork = { ...work, verdict: 'blocked' };
  assert.equal(pickProvider([claude, blockedWork, agy, codex], agent, 'codex'), 'codex');
  assert.equal(pickProvider([claude, blockedWork, agy, codex], agent, 'gone'), 'agy');
});

test('every provider blocked: the agent own provider', () => {
  assert.equal(pickProvider([{ ...work, verdict: 'blocked' }, claude], agent), 'claude');
  assert.equal(pickProvider([], agent), '');
});

test("model: the agent's own, keeping its effort", () => {
  assert.deepEqual(pickModel(work, agent), { model: 'claude-opus-5-5', thinking: 'high' });
});

test('model: remembered, then the first at its default', () => {
  assert.deepEqual(pickModel(codex, agent, { codex: { model: 'gpt-6.2-sol', thinking: 'xhigh' } }), { model: 'gpt-6.2-sol', thinking: 'xhigh' });
  assert.deepEqual(pickModel(codex, agent, { codex: { model: 'retired', thinking: 'xhigh' } }), { model: 'gpt-6.2-sol', thinking: 'high' });
  assert.deepEqual(pickModel(agy, agent), { model: '', thinking: '' });
});

test('thinking falls back to the default when unsupported', () => {
  assert.equal(pickThinking(fable, 'medium'), 'high');
  assert.equal(pickThinking({ id: 'x', thinking: [] }, 'high'), '');
});

test('profile strings round-trip', () => {
  assert.equal(profileOf('codex', 'gpt-6.2-sol', 'high'), 'codex/gpt-6.2-sol@high');
  assert.equal(profileOf('agy'), 'agy');
  assert.deepEqual(parseProfile('codex/gpt-6.2-sol@high'), { provider: 'codex', model: 'gpt-6.2-sol', thinking: 'high' });
  assert.deepEqual(parseProfile('claude@low'), { provider: 'claude', model: '', thinking: 'low' });
});
