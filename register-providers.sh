#!/bin/sh
set -e

echo "=== Registering Providers ==="

bun -e "
import { writeFileSync } from 'fs';

const API = 'http://9router:3456/api';

// Parse PROVIDER_<N>_<FIELD> from env
const providers = [];
const prefix = 'PROVIDER_';
for (const [k, v] of Object.entries(process.env)) {
  if (!k.startsWith(prefix)) continue;
  const m = k.match(/^PROVIDER_(\d+)_(.+)$/);
  if (!m) continue;
  const idx = parseInt(m[1]) - 1;
  const field = m[2].toLowerCase();
  if (!providers[idx]) providers[idx] = {};
  providers[idx][field] = v;
}

// Filter empty, parse models
const list = providers.filter(Boolean).map(p => ({
  ...p,
  models: (p.models || '').split(',').map(s => s.trim()).filter(Boolean)
}));

if (list.length === 0) {
  console.error('No providers found in env. Expected PROVIDER_1_NAME etc.');
  process.exit(1);
}

let cookie = '';
async function api(method, url, body) {
  const headers = { 'Content-Type': 'application/json' };
  if (cookie) headers['Cookie'] = cookie;
  const opts = { method, headers };
  if (body) opts.body = JSON.stringify(body);
  const res = await fetch(url, opts);
  const sc = res.headers.get('set-cookie');
  if (sc) cookie = sc.split(';')[0];
  const text = await res.text().catch(() => '');
  try { return { ok: res.ok, data: JSON.parse(text) }; }
  catch { return { ok: res.ok, data: text, status: res.status }; }
}
const post = (url, body) => api('POST', url, body);
const get = (url) => api('GET', url);
const del = (url) => api('DELETE', url);

// Login
await post(API + '/auth/login', { password: '123456' });

// Delete old nodes
const existing = await get(API + '/provider-nodes');
const knownPrefixes = list.map(p => p.prefix);
if (existing?.nodes) {
  for (const n of existing.nodes) {
    if (knownPrefixes.includes(n.prefix)) {
      await del(API + '/provider-nodes/' + n.id);
      console.log('Deleted old node:', n.prefix);
    }
  }
}

// Register each provider
const models = [];
for (const p of list) {
  const r = await post(API + '/provider-nodes', {
    name: p.name,
    prefix: p.prefix,
    baseUrl: p.base_url,
    type: 'anthropic-compatible'
  });
  console.log(p.prefix + ' node:', JSON.stringify(r.data));

  const nodeId = r.data?.node?.id;
  if (nodeId) {
    const c = await post(API + '/providers', {
      provider: nodeId,
      name: p.name,
      apiKey: p.api_key || '',
      label: p.name
    });
    console.log(p.prefix + ' conn:', c.ok ? 'OK' : 'FAIL');
  }

  for (const m of p.models) {
    models.push({ id: p.prefix + '/' + m, name: p.name + ' ' + m });
  }
}

// Generate Claude Code settings
const settings = {
  apiKey: 'x',
  baseUrl: 'http://localhost:3456/v1',
  model: models[0]?.id || '',
  _availableModels: models
};
writeFileSync('/out/claude-code-settings.json', JSON.stringify(settings, null, 2) + '\n');
console.log('\nGenerated claude-code-settings.json with', models.length, 'models');

console.log('=== Done ===');
"
