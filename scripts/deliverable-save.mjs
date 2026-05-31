#!/usr/bin/env node
// deliverable-save — publish one team-authored deliverable (Deliverables page).
//
//   deliverable-save <projectId> <type> "<title>" <mdFile>
//
// Reads the markdown file and POSTs it to the office daemon's agent endpoint
// so it appears (versioned) on the Deliverables page for that project.
//
// Env (set in every office agent):
//   JARVIS_API_URL      — daemon base URL (default https://127.0.0.1:3555)
//   JARVIS_AGENT_TOKEN  — bearer token for the agent-upload endpoint

import { readFile } from 'node:fs/promises';

// The daemon serves HTTPS with a self-signed cert on localhost.
process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';

function die(msg, code = 1) {
  console.error('deliverable-save: ' + msg);
  process.exit(code);
}

const [projectId, type, title, mdFile] = process.argv.slice(2);
if (!projectId || !type || !title || !mdFile) {
  die('usage: deliverable-save <projectId> <type> "<title>" <mdFile>', 2);
}

const content = await readFile(mdFile, 'utf8').catch((e) => die('cannot read file: ' + e.message, 2));
if (!content.trim()) die('the markdown file is empty', 2);

const base = (process.env.JARVIS_API_URL || 'https://127.0.0.1:3555').replace(/\/+$/, '');
const token = process.env.JARVIS_AGENT_TOKEN || '';

const res = await fetch(`${base}/api/office/agent/deliverable-upload`, {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  },
  body: JSON.stringify({ projectId, type, title, content }),
});
if (!res.ok) die(`upload ${res.status}: ${(await res.text()).slice(0, 400)}`, 6);
const out = await res.json();
console.log(`deliverable saved: ${out.slug || 'ok'}`);
