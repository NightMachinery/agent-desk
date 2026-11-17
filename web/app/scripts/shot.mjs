#!/usr/bin/env node
// Screenshots the dashboard in headless Chrome, optionally after running some
// JavaScript in the page (to select rows, open a menu, click a button), so a
// UI change can be checked in one command. It speaks the DevTools protocol
// directly: no puppeteer, nothing to install beyond Node 22+ and Chrome.
//
//   node scripts/shot.mjs URL OUT.png [--width 1360] [--height 860] [--wait 1500] [--timeout 30000]
//                         [--scale 2] [--transparent] [--js 'code'] [--js-file F]
//
// The code runs as the body of an async function; its return value is printed
// as JSON, so a check can both show and assert. --timeout bounds the whole
// run after --wait (default 30 s); raise it for a check that waits on a timer. --width under 600 emulates a
// phone (touch, mobile viewport). --scale is the device pixel ratio (default
// 2), so --width 512 --scale 1 makes a 512-pixel image; --transparent keeps
// what the page does not paint transparent. CHROME names the browser binary.
//
// Example: select two rows and show the bulk bar on a phone.
//   node scripts/shot.mjs 'http://127.0.0.1:7792/#/p/' /tmp/bar.png --width 520 --height 900 \
//     --js 'const c = document.querySelectorAll("button.check"); c[0].click(); c[1].click();
//           return document.querySelector(".bulk")?.innerText'

import { spawn } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

function usage(msg) {
  if (msg) console.error(`shot: ${msg}`);
  console.error('usage: shot.mjs URL OUT.png [--width N] [--height N] [--wait MS] [--timeout MS] [--scale N] [--transparent] [--js CODE] [--js-file F]');
  process.exit(2);
}

const args = process.argv.slice(2);
const pos = [];
const opt = { width: 1360, height: 860, wait: 1500, timeout: 30000, scale: 2, js: '', transparent: false };
for (let i = 0; i < args.length; i++) {
  const a = args[i];
  if (!a.startsWith('--')) {
    pos.push(a);
    continue;
  }
  if (a === '--transparent') {
    opt.transparent = true;
    continue;
  }
  const v = args[++i];
  if (v === undefined) usage(`${a} needs a value`);
  if (a === '--js') opt.js = v;
  else if (a === '--js-file') opt.js = readFileSync(v, 'utf8');
  else if (['--width', '--height', '--wait', '--timeout', '--scale'].includes(a)) {
    opt[a.slice(2)] = Number(v);
    if (!Number.isFinite(opt[a.slice(2)]) || opt[a.slice(2)] < 0) usage(`${a} takes a number`);
  } else usage(`unknown option ${a}`);
}
if (pos.length !== 2) usage();
const [url, out] = pos;

function chromePath() {
  if (process.env.CHROME) return process.env.CHROME;
  const candidates = [
    '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome',
    '/Applications/Chromium.app/Contents/MacOS/Chromium',
    '/usr/bin/google-chrome',
    '/usr/bin/chromium',
    '/usr/bin/chromium-browser',
  ];
  const found = candidates.find((p) => existsSync(p));
  if (!found) usage('no Chrome found; set CHROME to its binary');
  return found;
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const profile = mkdtempSync(join(tmpdir(), 'agent-desk-shot-'));
// Port 0: Chrome picks a free port and writes it to DevToolsActivePort.
const chrome = spawn(
  chromePath(),
  ['--headless=new', '--remote-debugging-port=0', `--user-data-dir=${profile}`, '--hide-scrollbars', '--no-first-run', 'about:blank'],
  { stdio: 'ignore' },
);
let done = false;
function finish(code) {
  if (done) return;
  done = true;
  chrome.kill();
  // Chrome may still hold files for a moment after the kill.
  setTimeout(() => {
    rmSync(profile, { recursive: true, force: true });
    process.exit(code);
  }, 300);
}
const deadline = setTimeout(() => {
  console.error('shot: timed out');
  finish(1);
}, opt.timeout + opt.wait);

try {
  let port = '';
  for (let i = 0; i < 100 && !port; i++) {
    await sleep(100);
    try {
      port = readFileSync(join(profile, 'DevToolsActivePort'), 'utf8').split('\n')[0];
    } catch {
      /* not yet */
    }
  }
  if (!port) throw new Error('Chrome did not start');
  const tabs = await (await fetch(`http://127.0.0.1:${port}/json`)).json();
  const page = tabs.find((t) => t.type === 'page');
  const ws = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => {
    ws.onopen = resolve;
    ws.onerror = () => reject(new Error('cannot connect to Chrome'));
  });
  let id = 0;
  const pending = new Map();
  ws.onmessage = (m) => {
    const d = JSON.parse(m.data);
    if (d.id && pending.has(d.id)) {
      pending.get(d.id)(d);
      pending.delete(d.id);
    }
  };
  const call = (method, params = {}) =>
    new Promise((resolve, reject) => {
      const n = ++id;
      pending.set(n, (d) => (d.error ? reject(new Error(`${method}: ${d.error.message}`)) : resolve(d.result)));
      ws.send(JSON.stringify({ id: n, method, params }));
    });

  const mobile = opt.width < 600;
  await call('Emulation.setDeviceMetricsOverride', { width: opt.width, height: opt.height, deviceScaleFactor: opt.scale, mobile });
  if (mobile) await call('Emulation.setTouchEmulationEnabled', { enabled: true });
  if (opt.transparent) await call('Emulation.setDefaultBackgroundColorOverride', { color: { r: 0, g: 0, b: 0, a: 0 } });
  await call('Page.enable');
  await call('Page.navigate', { url });
  await sleep(opt.wait);
  if (opt.js) {
    const r = await call('Runtime.evaluate', {
      expression: `(async () => {\n${opt.js}\n})()`,
      awaitPromise: true,
      returnByValue: true,
    });
    if (r.exceptionDetails) throw new Error(`page script: ${r.exceptionDetails.exception?.description ?? r.exceptionDetails.text}`);
    if (r.result.value !== undefined) console.log(JSON.stringify(r.result.value));
    await sleep(400);
  }
  const shot = await call('Page.captureScreenshot', { format: 'png' });
  writeFileSync(out, Buffer.from(shot.data, 'base64'));
  console.error(`shot: wrote ${out}`);
  ws.close();
  clearTimeout(deadline);
  finish(0);
} catch (err) {
  console.error(`shot: ${err.message}`);
  clearTimeout(deadline);
  finish(1);
}
