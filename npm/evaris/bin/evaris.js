#!/usr/bin/env node
'use strict';

const path = require('node:path');
const { spawn } = require('node:child_process');

const arch = { x64: 'amd64', arm64: 'arm64' }[process.arch];
if (!['linux', 'darwin'].includes(process.platform) || !arch) {
  console.error(`evaris: unsupported platform ${process.platform}/${process.arch}`);
  process.exit(2);
}
const binary = path.join(__dirname, '..', 'binaries', `evaris-${process.platform}-${arch}`);
const child = spawn(binary, process.argv.slice(2), { stdio: 'inherit' });
for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => { if (child.pid) child.kill(signal); });
}
child.on('error', (err) => {
  console.error(`evaris: unable to start native binary: ${err.message}`);
  process.exitCode = 2;
});
child.on('exit', (code, signal) => {
  if (signal) {
    process.removeAllListeners(signal);
    process.kill(process.pid, signal);
  } else if (code !== null) {
    process.exitCode = code;
  }
});
