const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');

const launcher = path.resolve(__dirname, '../bin/evaris.js');
test('passes arguments, streams and exit status to the native binary', () => {
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'evaris-launch-'));
  try {
    fs.mkdirSync(path.join(tmp, 'bin'));
    fs.mkdirSync(path.join(tmp, 'binaries'));
    fs.copyFileSync(launcher, path.join(tmp, 'bin/evaris.js'));
    const arch = { x64: 'amd64', arm64: 'arm64' }[process.arch];
    const binary = path.join(tmp, `binaries/evaris-${process.platform}-${arch}`);
    fs.writeFileSync(binary, '#!/usr/bin/env node\nprocess.stdin.pipe(process.stdout); console.error(JSON.stringify(process.argv.slice(2))); process.on("exit", () => {}); process.exitCode = 7;\n', { mode: 0o755 });
    const result = spawnSync(process.execPath, [path.join(tmp, 'bin/evaris.js'), 'a b', '$(bad)'], { input: 'input', encoding: 'utf8' });
    assert.equal(result.status, 7);
    assert.equal(result.stdout, 'input');
    assert.deepEqual(JSON.parse(result.stderr), ['a b', '$(bad)']);
  } finally { fs.rmSync(tmp, { recursive: true, force: true }); }
});

test('reports a missing binary', () => {
  const result = spawnSync(process.execPath, [launcher], { encoding: 'utf8' });
  assert.equal(result.status, 2);
  assert.match(result.stderr, /unable to start/i);
});
