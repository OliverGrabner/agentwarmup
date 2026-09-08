#!/usr/bin/env node
import { readFile } from 'node:fs/promises';
import { runLauncher } from '../lib/launcher.js';

try {
  const manifest = JSON.parse(await readFile(new URL('../manifest.json', import.meta.url), 'utf8'));
  const metadata = JSON.parse(await readFile(new URL('../package.json', import.meta.url), 'utf8'));
  if (metadata.version !== manifest.version) throw new Error('package and executable versions differ');
  process.exitCode = await runLauncher(manifest, process.argv.slice(2));
} catch (error) {
  console.error(`AgentWarmup: ${error.message}`);
  process.exitCode = 1;
}
