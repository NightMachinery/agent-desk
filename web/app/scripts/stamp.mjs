// Writes ../dist/.source-hash: a hash of every file the build reads, so the
// Go test in ../ can tell a stale dist/ from a current one without Node.
// Keep in step with sourceHash in ../embed_test.go.
import { createHash } from 'node:crypto';
import { readdirSync, readFileSync, statSync, writeFileSync, existsSync } from 'node:fs';
import { join } from 'node:path';

const root = new URL('..', import.meta.url).pathname;
const files = ['index.html', 'package.json', 'package-lock.json', 'svelte.config.js', 'vite.config.js'];
function walk(dir) {
  for (const name of readdirSync(join(root, dir)).sort()) {
    const rel = `${dir}/${name}`;
    if (statSync(join(root, rel)).isDirectory()) walk(rel);
    else files.push(rel);
  }
}
for (const dir of ['src', 'public']) if (existsSync(join(root, dir))) walk(dir);
files.sort();
const h = createHash('sha256');
for (const f of files) {
  h.update(f + '\0');
  h.update(readFileSync(join(root, f)));
  h.update('\0');
}
writeFileSync(join(root, '../dist/.source-hash'), h.digest('hex') + '\n');
