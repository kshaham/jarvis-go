#!/usr/bin/env node
// mockup-render — render one mockup image and upload it (Mockups page).
//
//   UI (image-to-image restyle of a screenshot):
//     mockup-render ui <projectId> <index> "<concept>" <inputPng>
//   Presentation (text-to-image slide design):
//     mockup-render presentation <projectId> <index> "<slide concept + style>"
//
// Renders via OpenAI gpt-image-1 (edits for ui, generations for presentation)
// then uploads the result to the office daemon so it appears on the Mockups
// page tile for that project + kind.
//
// Env (set in every office agent):
//   OPENAI_API_KEY      — image generation
//   JARVIS_API_URL      — daemon base URL (default https://127.0.0.1:3555)
//   JARVIS_AGENT_TOKEN  — bearer token for the agent-upload endpoint

import { readFile } from 'node:fs/promises';

// The daemon serves HTTPS with a self-signed cert on localhost.
process.env.NODE_TLS_REJECT_UNAUTHORIZED = '0';

function die(msg, code = 1) {
  console.error('mockup-render: ' + msg);
  process.exit(code);
}

const [kind, projectId, indexStr, prompt, inputPng] = process.argv.slice(2);
if (!kind || !projectId || indexStr === undefined || !prompt) {
  die('usage: mockup-render <ui|presentation> <projectId> <index> "<prompt>" [inputPng]', 2);
}
if (kind !== 'ui' && kind !== 'presentation') die('kind must be ui or presentation', 2);
const index = Number(indexStr);
if (!Number.isInteger(index) || index < 0 || index > 5) die('index must be 0..5', 2);

const apiKey = process.env.OPENAI_API_KEY;
if (!apiKey) die('OPENAI_API_KEY is not set', 3);
const base = (process.env.JARVIS_API_URL || 'https://127.0.0.1:3555').replace(/\/+$/, '');
const token = process.env.JARVIS_AGENT_TOKEN || '';

let b64;
if (kind === 'ui') {
  // Image-to-image edit of the UI screenshot.
  if (!inputPng) die('ui mode requires an inputPng (the UI screenshot)', 2);
  const png = await readFile(inputPng).catch((e) => die('cannot read input image: ' + e.message, 2));
  const form = new FormData();
  form.append('model', 'gpt-image-1');
  form.append(
    'prompt',
    'Redesign this app UI as a polished, production-quality mockup. ' +
      prompt +
      ' Keep it a believable app screen; high fidelity, realistic content.',
  );
  form.append('size', '1024x1024');
  form.append('moderation', 'low');
  form.append('image', new Blob([png], { type: 'image/png' }), 'base.png');
  const r = await fetch('https://api.openai.com/v1/images/edits', {
    method: 'POST',
    headers: { Authorization: `Bearer ${apiKey}` },
    body: form,
  });
  if (!r.ok) die(`openai ${r.status}: ${(await r.text()).slice(0, 400)}`, 4);
  b64 = (await r.json())?.data?.[0]?.b64_json;
} else {
  // Text-to-image generation of a presentation slide (16:9).
  const r = await fetch('https://api.openai.com/v1/images/generations', {
    method: 'POST',
    headers: { Authorization: `Bearer ${apiKey}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({
      model: 'gpt-image-1',
      prompt:
        'A polished 16:9 presentation slide mockup. ' +
        prompt +
        ' Clean typographic hierarchy, real legible text, professional layout.',
      size: '1536x1024',
      moderation: 'low',
    }),
  });
  if (!r.ok) die(`openai ${r.status}: ${(await r.text()).slice(0, 400)}`, 4);
  b64 = (await r.json())?.data?.[0]?.b64_json;
}
if (!b64) die('no image in openai response', 5);

// Upload to the daemon so it lands on the Mockups page.
const up = await fetch(`${base}/api/office/agent/mockup-upload`, {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  },
  body: JSON.stringify({ projectId, kind, index, imageBase64: b64 }),
});
if (!up.ok) die(`upload ${up.status}: ${(await up.text()).slice(0, 400)}`, 6);
const out = await up.json();
console.log(`mockup ${kind} ${index} uploaded: ${out.url || 'ok'}`);
