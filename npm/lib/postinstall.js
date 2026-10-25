'use strict';
// npm runs this after install. A failure here must not break `npm install`
// (npm runs installs offline in some setups), so it only warns; bin/depwhy.js
// tries again on first use.

const path = require('node:path');
const { ensureBinary } = require('./install');
const { version } = require('../package.json');

ensureBinary({ version, dir: path.join(__dirname, '..', 'vendor') }).catch((err) => {
  console.warn(`depwhy: could not download the binary now (${err.message}); it will be fetched on first run.`);
});
