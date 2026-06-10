#!/bin/sh
set -e
echo "=== Registering Anthropic Providers ==="

bun -e "
const API='http://9router:3456/api';
const DS=process.env.DEEPSEEK_API_KEY||'sk-placeholder';
let C='';

async function post(url,body) {
  const h={'Content-Type':'application/json'};
  if(C) h.Cookie=C;
  const r=await fetch(url,{method:'POST',headers:h,body:body?JSON.stringify(body):undefined});
  const sc=r.headers.get('set-cookie'); if(sc) C=sc.split(';')[0];
  const txt=await r.text().catch(()=>'');
  const data=r.ok ? JSON.parse(txt) : {error:txt,status:r.status};
  return {ok:r.ok,data};
}
async function del(url) {
  const h={'Content-Type':'application/json'}; if(C) h.Cookie=C;
  const r=await fetch(url,{method:'DELETE',headers:h});
  return r.ok;
}

// Login
await post(API+'/auth/login',{password:'123456'});

// Delete old nodes (will fail if none exist, that's ok)
const old=await post(API+'/provider-nodes',null);
// Need GET for listing
async function get(url) {
  const h={'Content-Type':'application/json'}; if(C) h.Cookie=C;
  const r=await fetch(url,{method:'GET',headers:h});
  return r.ok ? await r.json() : null;
}
const nodes=await get(API+'/provider-nodes');
if(nodes?.nodes) {
  for(const n of nodes.nodes) {
    if(n.prefix==='ds' || n.prefix==='cp') {
      await del(API+'/provider-nodes/'+n.id);
      console.log('Deleted old node:', n.prefix);
    }
  }
}

// Create new nodes
const ds=await post(API+'/provider-nodes',{name:'DeepSeek',prefix:'ds',baseUrl:'https://api.deepseek.com/anthropic',type:'anthropic-compatible'});
console.log('DS node:', JSON.stringify(ds.data));

const cp=await post(API+'/provider-nodes',{name:'Copilot Claude',prefix:'cp',baseUrl:'http://copilot-anthropic:4142',type:'anthropic-compatible'});
console.log('CP node:', JSON.stringify(cp.data));

// Create connections
const dsId=ds.data?.node?.id;
if(dsId){
  const c=await post(API+'/providers',{provider:dsId,name:'DeepSeek',apiKey:DS,label:'DeepSeek'});
  console.log('DS conn:', JSON.stringify(c.data));
}
const cpId=cp.data?.node?.id;
if(cpId){
  const c=await post(API+'/providers',{provider:cpId,name:'Copilot Claude',apiKey:'not-needed',label:'Copilot Claude'});
  console.log('CP conn:', JSON.stringify(c.data));
}
console.log('=== Done ===');
"
