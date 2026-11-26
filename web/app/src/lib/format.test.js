import { test } from 'node:test';
import assert from 'node:assert/strict';
import { deliverableURL, deliverableWhere, isWebURL, openLabel, openTarget } from './format.js';

const ev = (url) => ({ meta: { url } });

test('a deliverable opens a web address or an app link', () => {
  assert.equal(deliverableURL(ev('https://staging.example.org/')), 'https://staging.example.org/');
  assert.equal(deliverableURL(ev('http://example.org/a.apk')), 'http://example.org/a.apk');
  assert.equal(deliverableURL(ev('myapp://open?path=~/x.pdf&page=3')), 'myapp://open?path=~/x.pdf&page=3');
});

test('an unsafe or relative link is never a click target', () => {
  for (const url of ['javascript:alert(1)', 'JavaScript:alert(1)', ' javascript:alert(1)', 'vbscript:x', 'file:///etc/hosts', 'data:text/html,x', 'docs/x.pdf', '/x.pdf', '', undefined, 42])
    assert.equal(deliverableURL(ev(url)), '', String(url));
});

test('only a web address opens in a new tab', () => {
  assert.deepEqual(openTarget('https://example.org/'), { target: '_blank', rel: 'noopener noreferrer' });
  assert.deepEqual(openTarget('myapp://open?x=1'), {});
  assert.equal(isWebURL('myapp://open?x=1'), false);
});

test('the Open label names the host or the app scheme', () => {
  assert.equal(openLabel('https://staging.example.org/x'), 'staging.example.org');
  assert.equal(openLabel('myapp://open?x=1'), 'in myapp');
  assert.equal(deliverableWhere(ev('myapp://open?x=1')), 'myapp://open?x=1');
  assert.equal(deliverableWhere(ev('https://example.org/a/')), 'example.org/a');
});
