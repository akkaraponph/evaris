#!/usr/bin/env node
'use strict';
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const { spawnSync } = require('node:child_process');
const root = path.resolve(__dirname, '..');
const [version, input = path.join(root, 'dist')] = process.argv.slice(2);
try {
  if (!/^v(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(version || '')) throw new Error('expected release version vX.Y.Z');
  const dir = path.resolve(input);
  const targets = ['linux-amd64', 'linux-arm64', 'darwin-amd64', 'darwin-arm64'];
  const sums = fs.readFileSync(path.join(dir, 'SHA256SUMS'), 'utf8').trim().split(/\r?\n/);
  const checksums = new Map();
  for (const line of sums) {
    const match = /^([a-fA-F0-9]{64})\s+\*?(\S+)$/.exec(line);
    if (!match || checksums.has(match[2])) throw new Error('invalid or duplicate checksum entry');
    checksums.set(match[2], match[1].toLowerCase());
  }
  const files = targets.map(target => `evaris-${version}-${target}`);
  if (checksums.size !== files.length || files.some(name => !checksums.has(name))) throw new Error('checksum list must contain exactly the four release binaries');
  for (const name of files) {
    const actual = crypto.createHash('sha256').update(fs.readFileSync(path.join(dir, name))).digest('hex');
    if (actual !== checksums.get(name)) throw new Error(`checksum mismatch: ${name}`);
  }
  const staging = path.join(dir, 'npm-package');
  fs.rmSync(staging, { recursive: true, force: true });
  fs.mkdirSync(path.join(staging, 'bin'), { recursive: true });
  fs.mkdirSync(path.join(staging, 'binaries'));
  const metadata = JSON.parse(fs.readFileSync(path.join(root, 'npm/evaris/package.json')));
  metadata.version = version.slice(1);
  fs.writeFileSync(path.join(staging, 'package.json'), JSON.stringify(metadata, null, 2) + '\n');
  fs.copyFileSync(path.join(root, 'npm/evaris/bin/evaris.js'), path.join(staging, 'bin/evaris.js'));
  fs.copyFileSync(path.join(root, 'npm/evaris/README.md'), path.join(staging, 'README.md'));
  fs.copyFileSync(path.join(root, 'LICENSE'), path.join(staging, 'LICENSE'));
  for (let i = 0; i < files.length; i++) {
    const dest = path.join(staging, 'binaries', `evaris-${targets[i]}`);
    fs.copyFileSync(path.join(dir, files[i]), dest);
    fs.chmodSync(dest, 0o755);
  }
  const packed = spawnSync('npm', ['pack', '--pack-destination', dir], { cwd: staging, stdio: 'inherit' });
  if (packed.error || packed.status !== 0) throw packed.error || new Error(`npm pack failed: ${packed.status}`);
} catch (err) {
  console.error(`package-npm: ${err.message}`);
  process.exitCode = 1;
}
