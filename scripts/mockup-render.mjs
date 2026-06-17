#!/usr/bin/env node
// mockup-render — render one mockup image and upload it (Mockups page).
//
//   UI (image-to-image restyle of a screenshot):
//     mockup-render ui <projectId> <index> "<concept>" <inputPng>
//   Presentation (text-to-image slide design):
//     mockup-render presentation <projectId> <index> "<slide concept + style>"
//
// Renders via Google Gemini ("Nano Banana" image model) -- image+text edit for
// ui, text-to-image for presentation -- then uploads the result to the office
// daemon so it appears on the Mockups page tile for that project + kind.
//
// Env (set in every office agent):
//   GEMINI_API_KEY      — image generation (Nano Banana)
//   GEMINI_IMAGE_MODEL  — optional model override (default gemini-2.5-flash-image-preview)
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

const apiKey = process.env.GEMINI_API_KEY;
if (!apiKey) die('GEMINI_API_KEY is not set', 3);
const model = process.env.GEMINI_IMAGE_MODEL || 'gemini-2.5-flash-image';
const base = (process.env.JARVIS_API_URL || 'https://127.0.0.1:3555').replace(/\/+$/, '');
const token = process.env.JARVIS_AGENT_TOKEN || '';

// Assemble the Gemini generateContent request parts: a text instruction plus
// (for ui) the inline base screenshot to restyle.
const parts = [];
if (kind === 'ui') {
  if (!inputPng) die('ui mode requires an inputPng (the UI screenshot)', 2);
  const png = await readFile(inputPng).catch((e) => die('cannot read input image: ' + e.message, 2));
  parts.push({
    text:
      'Redesign this web UI as a polished, production-quality full-page mockup. ' +
      prompt +
      ' Keep it a believable, high-fidelity app/landing screen with realistic, legible content.',
  });
  parts.push({ inline_data: { mime_type: 'image/png', data: png.toString('base64') } });
} else {
  parts.push({
    text:
      'Generate a polished 16:9 presentation slide mockup. ' +
      prompt +
      ' Clean typographic hierarchy, real legible text, professional layout.',
  });
}

const url = `https://generativelanguage.googleapis.com/v1beta/models/${model}:generateContent?key=${apiKey}`;
const r = await fetch(url, {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    contents: [{ parts }],
    generationConfig: { responseModalities: ['IMAGE'] },
  }),
});
if (!r.ok) die(`gemini ${r.status}: ${(await r.text()).slice(0, 400)}`, 4);

// Pull the first inline image out of the candidate parts (REST returns
// camelCase inlineData; tolerate snake_case too).
const candParts = r && (await r.json())?.candidates?.[0]?.content?.parts;
let b64;
for (const p of candParts || []) {
  const inl = p.inlineData || p.inline_data;
  if (inl && inl.data) {
    b64 = inl.data;
    break;
  }
}
if (!b64) die('no image in gemini response', 5);

// Upload to the daemon so it lands on the Mockups page. The mockup-upload
// route lives on the /claude/hooks mount (agent-token auth), not /office/agent.
const up = await fetch(`${base}/api/claude/hooks/mockup-upload`, {
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
