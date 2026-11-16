const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const crypto = require('node:crypto');
const { spawnSync } = require('node:child_process');
const script = path.resolve(__dirname, '../../../scripts/package-npm.js');

test('packages only complete checksum-verified release binaries', () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'evaris-pack-'));
  try {
    const names = ['linux-amd64', 'linux-arm64', 'darwin-amd64', 'darwin-arm64'].map(t => `evaris-v1.2.3-${t}`);
    for (const name of names) fs.writeFileSync(path.join(dir, name), name);
    fs.writeFileSync(path.join(dir, 'SHA256SUMS'), names.map(name => `${crypto.createHash('sha256').update(name).digest('hex')}  ${name}\n`).join(''));
    const run = () => spawnSync(process.execPath, [script, 'v1.2.3', dir], { encoding: 'utf8' });
    assert.equal(run().status, 0);
    assert.equal(JSON.parse(fs.readFileSync(path.join(dir, 'npm-package/package.json'))).version, '1.2.3');
    fs.writeFileSync(path.join(dir, names[0]), 'tampered');
    assert.notEqual(run().status, 0);
    fs.writeFileSync(path.join(dir, names[0]), names[0]);
    fs.unlinkSync(path.join(dir, names[1]));
    assert.notEqual(run().status, 0);
  } finally { fs.rmSync(dir, { recursive: true, force: true }); }
});
