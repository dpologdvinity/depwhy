#!/usr/bin/env node
'use strict';
// Launcher: finds (or downloads) the native depwhy binary and runs it with the
// same arguments, forwarding its exit code.

const path = require('node:path');
const { spawnSync } = require('node:child_process');
const { ensureBinary } = require('../lib/install');
const { version } = require('../package.json');

async function main() {
  const bin = await ensureBinary({ version, dir: path.join(__dirname, '..', 'vendor') });
  const r = spawnSync(bin, process.argv.slice(2), { stdio: 'inherit' });
  if (r.error) throw r.error;
  if (r.signal) process.kill(process.pid, r.signal);
  process.exit(r.status === null ? 1 : r.status);
}

main().catch((err) => {
  console.error(`depwhy: ${err.message}`);
  process.exit(2);
});
