import { spawn } from 'node:child_process';
import { mkdir, rm, writeFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const root = path.dirname(scriptDir);
const edge = 'C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe';
const profile = path.join(root, '.tools', 'edge-verify', `${process.pid}-${Date.now()}`);
const screenshots = path.join(root, 'docs', 'screenshots');
const port = 9300 + Math.floor(Math.random() * 500);
await mkdir(profile, { recursive: true });
await mkdir(screenshots, { recursive: true });

const browser = spawn(edge, [
  '--headless=new', '--disable-gpu', '--disable-gpu-sandbox', '--no-sandbox', '--use-angle=swiftshader',
  '--no-first-run', '--no-default-browser-check',
  `--remote-debugging-port=${port}`, '--remote-allow-origins=*', `--user-data-dir=${profile}`,
  '--window-size=1440,1000', 'about:blank'
], { stdio: ['ignore', 'ignore', 'pipe'] });
let browserExit;
let browserError = '';
browser.on('exit', code => { browserExit = code ?? 0; });
browser.stderr.on('data', chunk => { browserError += chunk.toString(); });

const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));
async function json(url, options) { const response = await fetch(url, options); if (!response.ok) throw new Error(`${url}: ${response.status}`); return response.json(); }
async function waitEndpoint() { for (let i=0;i<80;i++){if(browserExit!==undefined)throw new Error(`Edge exited before startup (${browserExit}): ${browserError.trim()}`);try{return await json(`http://127.0.0.1:${port}/json/list`)}catch{await sleep(100)}}throw new Error(`Edge debugging endpoint did not start: ${browserError.trim()}`) }

class CDP {
  constructor(url) { this.id=0;this.pending=new Map();this.errors=[];this.ws=new WebSocket(url);this.ready=new Promise((resolve,reject)=>{this.ws.onopen=resolve;this.ws.onerror=reject;this.ws.onclose=()=>reject(new Error('Edge debugging WebSocket closed before startup'))});this.ws.onmessage=e=>{const message=JSON.parse(e.data);if(message.id){const p=this.pending.get(message.id);if(!p)return;this.pending.delete(message.id);message.error?p.reject(new Error(message.error.message)):p.resolve(message.result)}else if(message.method==='Runtime.exceptionThrown'){this.errors.push(message.params.exceptionDetails?.text||'Uncaught browser exception')}} }
  async send(method,params={}){await Promise.race([this.ready,timeout('connecting to Edge debugging WebSocket')]);const id=++this.id;const promise=new Promise((resolve,reject)=>this.pending.set(id,{resolve,reject}));this.ws.send(JSON.stringify({id,method,params}));return Promise.race([promise,timeout(`waiting for ${method}`)])}
  async eval(expression){const out=await this.send('Runtime.evaluate',{expression,awaitPromise:true,returnByValue:true});if(out.exceptionDetails)throw new Error(out.exceptionDetails.text);return out.result.value}
}

let cdp;
try {
  const pages = await waitEndpoint();
  const page = pages.find(x=>x.type==='page');
  if (!page) throw new Error('No browser page target found');
  const debuggerUrl = page.webSocketDebuggerUrl.replace('://localhost:', '://127.0.0.1:');
  cdp = new CDP(debuggerUrl);
  await cdp.send('Page.enable'); await cdp.send('Runtime.enable'); await cdp.send('Log.enable');
  await cdp.send('Page.navigate',{url:'http://localhost:8080/'});
  await waitFor(`document.querySelector('#login-form') && !document.querySelector('#login-form').classList.contains('hidden')`);
  await sleep(300);
  assert(await cdp.eval(`document.title==='NeighborParking'`),'document title');
  assert(await cdp.eval(`document.querySelector('meta[name="description"]').content.length>20`),'meta description');

  await cdp.eval(`(()=>{const f=document.querySelector('#login-form');f.elements.email.value='owner@demo.local';f.elements.password.value='DemoPass123!';f.querySelector('button[type="submit"],button').click();return true})()`);
  await waitFor(`document.querySelectorAll('.community-card').length>=2`);
  assert(await cdp.eval(`!document.querySelector('#top-nav').classList.contains('hidden')`),'authenticated navigation');
  await capture('dashboard.png');

  await cdp.eval(`(()=>{const cards=[...document.querySelectorAll('.community-card')];const c=cards.find(x=>x.querySelector('h3')?.textContent==='Lakeview Residency');if(!c)return false;c.click();return true})()`);
  await waitFor(`document.querySelectorAll('#parking-grid .grid-cell').length>10`);
  assert(await cdp.eval(`document.querySelector('#community-role').textContent==='OWNER'`),'owner role badge');
  assert(await cdp.eval(`document.querySelectorAll('#parking-grid .PARKING.AVAILABLE').length>0 && document.querySelectorAll('#parking-grid .PARKING.OCCUPIED').length>0`),'live parking colors');
  assert(await cdp.eval(`document.querySelector('#socket-state').textContent==='Updates connected'`),'WebSocket UI connection');
  await capture('live-map.png');

  await cdp.eval(`document.querySelector('[data-community-tab="members"]').click()`);
  await waitFor(`document.querySelectorAll('#members-list .member-row').length>=3`);
  assert(await cdp.eval(`!document.querySelector('#tab-members').classList.contains('hidden')`),'members tab');
  await cdp.eval(`document.querySelector('[data-community-tab="requests"]').click()`);
  await waitFor(`!document.querySelector('#tab-requests').classList.contains('hidden') && document.querySelector('#requests-list').children.length>0`);
  await cdp.eval(`document.querySelector('[data-community-tab="designer"]').click()`);
  await waitFor(`document.querySelectorAll('#designer-grid .grid-cell').length>10`);
  assert(await cdp.eval(`document.querySelectorAll('#tool-palette .tool').length===8`),'designer tools');

  await cdp.eval(`document.querySelector('[data-action="profile"]').click()`);
  await waitFor(`document.querySelector('#modal').open && document.querySelector('#profile-form')`);
  assert(await cdp.eval(`document.querySelector('#profile-form').elements.fullName.value==='Amina Rahman'`),'profile modal values');
  await cdp.eval(`document.querySelector('.modal-close').click()`);
  await cdp.eval(`document.querySelector('[data-action="dashboard"]').click()`);
  await waitFor(`document.querySelectorAll('.community-card').length>=2`);
  await cdp.eval(`document.querySelector('[data-action="logout"]').click()`);
  await waitFor(`document.querySelector('#login-form') && !document.querySelector('#login-form').classList.contains('hidden')`);

  await cdp.send('Emulation.setDeviceMetricsOverride',{width:390,height:844,deviceScaleFactor:1,mobile:true});
  await cdp.send('Page.reload',{ignoreCache:true});
  await waitFor(`document.querySelector('#login-form')`);
  assert(await cdp.eval(`document.documentElement.scrollWidth<=window.innerWidth+1`),'mobile horizontal overflow');
  await capture('mobile-login.png');
  if(cdp.errors.length)throw new Error('Browser console errors: '+cdp.errors.join(' | '));
  console.log('PASS: browser login, dashboard, live map, tabs, designer, profile, logout, WebSocket UI, and mobile layout');
} catch (error) {
  const diagnostics = browserError.trim();
  if (diagnostics) error.message += `\nEdge diagnostics:\n${diagnostics}`;
  throw error;
} finally {
  if(cdp){try{await cdp.send('Browser.close')}catch{}}
  browser.kill();
  await sleep(200);
  await rm(profile,{recursive:true,force:true}).catch(()=>{});
}

async function waitFor(expression,timeout=8000){const started=Date.now();while(Date.now()-started<timeout){try{if(await cdp.eval(`Boolean(${expression})`))return}catch{}await sleep(100)}throw new Error('Timed out waiting for: '+expression)}
async function capture(name){const out=await cdp.send('Page.captureScreenshot',{format:'png',captureBeyondViewport:false});await writeFile(path.join(screenshots,name),Buffer.from(out.data,'base64'))}
function assert(value,label){if(!value)throw new Error('Assertion failed: '+label)}
function timeout(label,ms=5000){return new Promise((_,reject)=>setTimeout(()=>reject(new Error(`Timed out ${label}`)),ms))}
