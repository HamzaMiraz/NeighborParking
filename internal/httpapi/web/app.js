const app = document.querySelector('#app');
const nav = document.querySelector('#top-nav');
const modal = document.querySelector('#modal');
const modalBody = document.querySelector('#modal-body');
const state = { user: null, communities: [], community: null, members: [], socket: null, tool: 'PARKING', draft: null };
const cellTypes = ['PARKING','ROAD','ENTRY_GATE','EXIT_GATE','WALL','PILLAR','NO_PARKING','EMPTY'];

const esc = value => String(value ?? '').replace(/[&<>'"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;',"'":'&#39;','"':'&quot;'}[c]));
const initials = name => (name || 'U').split(/\s+/).slice(0,2).map(x=>x[0]).join('').toUpperCase();
const formJSON = form => Object.fromEntries(new FormData(form).entries());
const isAdmin = role => role === 'OWNER' || role === 'ADMIN';
document.querySelector('.modal-close').addEventListener('click', closeModal);

async function api(path, options={}) {
  const init = {credentials:'same-origin', ...options, headers:{...(options.body ? {'Content-Type':'application/json'} : {}), ...(options.headers || {})}};
  const response = await fetch('/api/v1' + path, init);
  if (response.status === 204) return null;
  const data = await response.json().catch(()=>({}));
  if (!response.ok) { const error = new Error(data.error?.message || 'Something went wrong.'); error.code=data.error?.code; error.status=response.status; throw error; }
  return data;
}

function toast(message, type='success') {
  const el=document.createElement('div'); el.className='toast '+type; el.textContent=message; document.querySelector('#toast-region').append(el); setTimeout(()=>el.remove(),4200);
}
function openModal(html){modalBody.innerHTML=html;modal.showModal();}
function closeModal(){modal.close();modalBody.innerHTML='';}
function busy(button,on=true){button.disabled=on;button.dataset.label ||= button.textContent;button.textContent=on?'Please wait…':button.dataset.label;}

async function boot(){
  try { const data=await api('/me'); state.user=data.user; showNav(); await showDashboard(); }
  catch(e){ showAuth(); }
}
function showNav(){nav.classList.remove('hidden');document.querySelector('#avatar').textContent=initials(state.user.fullName)}
function hideNav(){nav.classList.add('hidden')}

function showAuth(){
  disconnectSocket();hideNav();state.user=null;app.innerHTML=document.querySelector('#auth-template').innerHTML;
  document.querySelectorAll('[data-auth-tab]').forEach(btn=>btn.onclick=()=>{document.querySelectorAll('[data-auth-tab]').forEach(x=>x.classList.toggle('active',x===btn));document.querySelector('#login-form').classList.toggle('hidden',btn.dataset.authTab!=='login');document.querySelector('#register-form').classList.toggle('hidden',btn.dataset.authTab!=='register')});
  document.querySelector('#login-form').onsubmit=async e=>{e.preventDefault();const b=e.submitter;busy(b);try{const data=await api('/auth/login',{method:'POST',body:JSON.stringify(formJSON(e.target))});state.user=data.user;showNav();toast('Welcome back.');await showDashboard()}catch(x){toast(x.message,'error')}finally{busy(b,false)}};
  document.querySelector('#register-form').onsubmit=async e=>{e.preventDefault();const b=e.submitter;busy(b);try{const data=await api('/auth/register',{method:'POST',body:JSON.stringify(formJSON(e.target))});state.user=data.user;showNav();toast('Your account is ready.');await showDashboard()}catch(x){toast(x.message,'error')}finally{busy(b,false)}};
}

async function showDashboard(){
  disconnectSocket();app.innerHTML='<section class="loading-screen"><div class="spinner"></div><p>Loading communities…</p></section>';
  try { const data=await api('/dashboard/communities');state.communities=data.communities;app.innerHTML=document.querySelector('#dashboard-template').innerHTML;document.querySelector('#welcome-name').textContent=state.user.fullName.split(' ')[0];renderCommunityCards();bindDashboard();location.hash='#/'; }
  catch(e){if(e.status===401)return showAuth();toast(e.message,'error')}
}
function renderCommunityCards(){
  const list=document.querySelector('#community-grid');const total=state.communities.reduce((n,c)=>n+c.availableSpots,0);const managed=state.communities.filter(c=>isAdmin(c.role)).length;
  document.querySelector('#community-count').textContent=state.communities.length;document.querySelector('#available-count').textContent=total;document.querySelector('#managed-count').textContent=managed;
  if(!state.communities.length){list.innerHTML='<div class="empty-state"><span class="eyebrow">A blank canvas</span><h2>Your parking network starts here.</h2><p>Create a community or ask to join one nearby.</p></div>';return}
  list.innerHTML=state.communities.map(c=>`<button class="community-card" data-community="${esc(c.id)}"><header><span class="role-pill">${esc(c.role)}</span><span class="code">${esc(c.code)}</span></header><h3>${esc(c.name)}</h3><p>${esc(c.description||'A private community parking space.')}</p><div class="availability"><span><strong>${c.availableSpots}</strong> of ${c.totalSpots} spaces available</span><b>Open →</b></div></button>`).join('');
  list.querySelectorAll('[data-community]').forEach(x=>x.onclick=()=>openCommunity(x.dataset.community));
}
function bindDashboard(){document.querySelector('[data-action="create"]').onclick=createCommunityModal;document.querySelector('[data-action="join"]').onclick=joinCommunityModal}

function createCommunityModal(){
  openModal(`<span class="eyebrow">A new shared space</span><h2>Create community</h2><form id="create-community" class="stack"><label>Community name<input name="name" required minlength="2" placeholder="Lakeview Apartments"></label><label>Description<textarea name="description" rows="2" placeholder="A short introduction"></textarea></label><label>Private address<input name="address" placeholder="Visible only to approved members"></label><div class="form-grid"><label>Grid rows<input name="gridRows" type="number" min="1" max="50" value="8" required></label><label>Grid columns<input name="gridCols" type="number" min="1" max="50" value="10" required></label></div><button class="button primary">Create community</button></form>`);
  document.querySelector('#create-community').onsubmit=async e=>{e.preventDefault();const b=e.submitter;busy(b);const data=formJSON(e.target);data.gridRows=Number(data.gridRows);data.gridCols=Number(data.gridCols);try{const out=await api('/communities',{method:'POST',body:JSON.stringify(data)});closeModal();toast('Community created. Now design the parking grid.');await showDashboard();await openCommunity(out.community.id,'designer')}catch(x){toast(x.message,'error')}finally{busy(b,false)}};
}
function joinCommunityModal(){
  openModal(`<span class="eyebrow">Find your neighbors</span><h2>Join a community</h2><form id="search-community" class="stack"><label>Name or community code<input name="q" required minlength="2" autofocus placeholder="Search by name or code"></label><button class="button primary">Search</button></form><div id="search-results" class="request-grid" style="margin-top:1rem"></div>`);
  document.querySelector('#search-community').onsubmit=async e=>{e.preventDefault();const b=e.submitter;busy(b);try{const out=await api('/communities/search?q='+encodeURIComponent(new FormData(e.target).get('q')));const box=document.querySelector('#search-results');box.innerHTML=out.communities.length?out.communities.map(c=>`<article class="request-card"><span class="code">${esc(c.code)}</span><h3>${esc(c.name)}</h3><p>${esc(c.description||'Private community')}</p>${c.membershipStatus?`<span class="role-pill">${esc(c.membershipStatus)}</span>`:`<button class="button outline small" data-join="${esc(c.id)}">Request access</button>`}</article>`).join(''):'<p>No communities found.</p>';box.querySelectorAll('[data-join]').forEach(x=>x.onclick=()=>sendJoin(x))}catch(x){toast(x.message,'error')}finally{busy(b,false)}};
}
async function sendJoin(button){busy(button);try{await api(`/communities/${button.dataset.join}/join-requests`,{method:'POST',body:'{}'});button.textContent='Pending';button.disabled=true;toast('Join request sent.')}catch(e){toast(e.message,'error');busy(button,false)}}

async function openCommunity(id,initialTab='map'){
  app.innerHTML='<section class="loading-screen"><div class="spinner"></div><p>Opening the live map…</p></section>';
  try{const out=await api(`/communities/${id}/map`);state.community=out.community;app.innerHTML=document.querySelector('#community-template').innerHTML;document.querySelector('#community-name').textContent=state.community.name;document.querySelector('#community-role').textContent=state.community.role;document.querySelector('#community-address').textContent=state.community.address||`Community code · ${state.community.code}`;document.querySelectorAll('.admin-only').forEach(x=>x.classList.toggle('hidden',!isAdmin(state.community.role)));bindCommunity();renderMap();connectSocket();switchCommunityTab(initialTab);location.hash='#/community/'+id}catch(e){toast(e.message,'error');await showDashboard()}
}
function bindCommunity(){
  document.querySelectorAll('[data-community-tab]').forEach(x=>x.onclick=()=>switchCommunityTab(x.dataset.communityTab));
  document.querySelector('[data-action="refresh-map"]').onclick=refreshMap;document.querySelector('[data-action="leave"]').onclick=leaveCommunity;document.querySelector('[data-action="save-layout"]').onclick=saveLayout;document.querySelector('[data-action="reset-layout"]').onclick=initDesigner;
}
async function switchCommunityTab(tab){
  document.querySelectorAll('.tab-panel').forEach(x=>x.classList.add('hidden'));document.querySelector(`#tab-${tab}`).classList.remove('hidden');document.querySelectorAll('[data-community-tab]').forEach(x=>x.classList.toggle('active',x.dataset.communityTab===tab));
  if(tab==='members')await loadMembers();if(tab==='requests')await loadRequests();if(tab==='designer')initDesigner();
}
async function refreshMap(){try{const out=await api(`/communities/${state.community.id}/map`);state.community=out.community;renderMap()}catch(e){toast(e.message,'error')}}
function renderMap(){
  const grid=document.querySelector('#parking-grid');if(!grid)return;grid.style.gridTemplateColumns=`repeat(${state.community.gridCols},auto)`;const byPos=new Map(state.community.cells.map(c=>[`${c.row}:${c.col}`,c]));let free=0,busyCount=0,total=0;let html='';
  for(let r=0;r<state.community.gridRows;r++)for(let c=0;c<state.community.gridCols;c++){const cell=byPos.get(`${r}:${c}`)||{row:r,col:c,type:'EMPTY'};if(cell.type==='PARKING'){total++;cell.status==='AVAILABLE'?free++:busyCount++}html+=cellHTML(cell)}
  grid.innerHTML=html;grid.querySelectorAll('[data-slot]').forEach(x=>x.onclick=()=>spotModal(state.community.cells.find(c=>c.slotId===x.dataset.slot)));document.querySelector('#map-available').textContent=free;document.querySelector('#map-occupied').textContent=busyCount;document.querySelector('#map-total').textContent=total;
}
function cellHTML(c,designer=false){const label=c.type==='PARKING'?c.label:c.type==='EMPTY'?'':c.type.replaceAll('_',' ');return `<button class="grid-cell ${esc(c.type)} ${esc(c.status||'')}" ${c.slotId&&!designer?`data-slot="${esc(c.slotId)}"`:''} ${designer?`data-pos="${c.row}:${c.col}"`:''} aria-label="${esc(label||'Empty cell')}">${esc(label)}</button>`}
function spotModal(cell){
  const occupied=cell.status==='OCCUPIED',mine=cell.occupant?.userId===state.user.id,admin=isAdmin(state.community.role);let action='';
  if(!occupied) action=`<button class="button primary" data-spot-action="check-in">Check in here</button>${admin?'<button class="button outline" data-spot-action="force-in">Assign to member</button>':''}`;
  else if(mine) action=`<button class="button primary" data-spot-action="check-out">Check out</button>${admin?'<button class="button outline" data-spot-action="force-out">Force check-out</button>':''}`;
  else if(admin) action='<button class="button danger" data-spot-action="force-out">Force check-out</button>';
  openModal(`<span class="eyebrow">Parking space</span><h2>${esc(cell.label)}</h2><p><span class="role-pill">${esc(cell.status)}</span></p>${occupied?`<div class="request-card"><strong>${esc(cell.occupant?.fullName||'Member')}</strong><p>${esc(cell.occupant?.vehicleName||'Vehicle')}</p>${cell.occupant?.licensePlate?`<span class="code">${esc(cell.occupant.licensePlate)}</span>`:''}</div>`:'<p>This space is currently open.</p>'}<div class="actions" style="margin-top:1.2rem">${action}</div>`);
  modalBody.querySelectorAll('[data-spot-action]').forEach(x=>x.onclick=()=>spotAction(x.dataset.spotAction,cell));
}
async function spotAction(action,cell){
  if(action==='force-in'){await chooseForceMember(cell);return}const path=action==='check-in'?'check-in':action==='check-out'?'check-out':'force-check-out';const b=modalBody.querySelector(`[data-spot-action="${action}"]`);busy(b);try{await api(`/communities/${state.community.id}/slots/${cell.slotId}/${path}`,{method:'POST',body:'{}'});closeModal();toast(path.includes('out')?'Space released.':'You are checked in.');await refreshMap()}catch(e){toast(e.message,'error')}finally{busy(b,false)}}
async function chooseForceMember(cell){try{if(!state.members.length)await fetchMembers();const options=state.members.map(m=>`<option value="${esc(m.userId)}">${esc(m.user.fullName)} · ${esc(m.user.vehicleName)}</option>`).join('');openModal(`<span class="eyebrow">Administrator override</span><h2>Assign ${esc(cell.label)}</h2><form id="force-in-form" class="stack"><label>Approved member<select name="userId" required>${options}</select></label><button class="button primary">Force check-in</button></form>`);document.querySelector('#force-in-form').onsubmit=async e=>{e.preventDefault();const b=e.submitter;busy(b);try{await api(`/communities/${state.community.id}/slots/${cell.slotId}/force-check-in`,{method:'POST',body:JSON.stringify(formJSON(e.target))});closeModal();toast('Member assigned to the space.');await refreshMap()}catch(x){toast(x.message,'error')}finally{busy(b,false)}}}catch(e){toast(e.message,'error')}}

async function fetchMembers(){const out=await api(`/communities/${state.community.id}/members`);state.members=out.members;return out}
async function loadMembers(){
  const box=document.querySelector('#members-list');box.innerHTML='<div class="loading-screen"><div class="spinner"></div></div>';try{const out=await fetchMembers();box.innerHTML=out.members.map(m=>`<div class="member-row"><div class="person"><span class="person-mark">${esc(initials(m.user.fullName))}</span><div><strong>${esc(m.user.fullName)}</strong><small>${esc(m.user.email||m.role)}</small></div></div><span>${esc(m.user.vehicleName)}${m.user.licensePlate?` · ${esc(m.user.licensePlate)}`:''}</span><span class="role-pill">${esc(m.role)}</span>${isAdmin(state.community.role)&&m.role!=='OWNER'&&m.userId!==state.user.id?`<div class="actions"><button class="button ghost small" data-role="${esc(m.id)}" data-next="${m.role==='ADMIN'?'MEMBER':'ADMIN'}">${m.role==='ADMIN'?'Demote':'Promote'}</button><button class="button outline small" data-remove="${esc(m.id)}">Remove</button>${state.community.role==='OWNER'?`<button class="button ghost small" data-owner="${esc(m.userId)}">Make owner</button>`:''}</div>`:'<span></span>'}</div>`).join('')||'<div class="empty-state">No members yet.</div>';bindMemberActions()}catch(e){box.innerHTML='';toast(e.message,'error')}
}
function bindMemberActions(){document.querySelectorAll('[data-role]').forEach(x=>x.onclick=()=>changeRole(x));document.querySelectorAll('[data-remove]').forEach(x=>x.onclick=()=>removeMember(x));document.querySelectorAll('[data-owner]').forEach(x=>x.onclick=()=>transferOwner(x))}
async function changeRole(btn){if(!confirm(`Change this member to ${btn.dataset.next}?`))return;busy(btn);try{await api(`/communities/${state.community.id}/members/${btn.dataset.role}/role`,{method:'PATCH',body:JSON.stringify({role:btn.dataset.next})});toast('Role updated.');await loadMembers()}catch(e){toast(e.message,'error')}finally{busy(btn,false)}}
async function removeMember(btn){if(!confirm('Remove this member? An active parking space will be released.'))return;busy(btn);try{await api(`/communities/${state.community.id}/members/${btn.dataset.remove}/remove`,{method:'POST',body:'{}'});toast('Member removed.');await loadMembers();await refreshMap()}catch(e){toast(e.message,'error')}finally{busy(btn,false)}}
async function transferOwner(btn){if(!confirm('Transfer ownership? You will become an administrator.'))return;busy(btn);try{await api(`/communities/${state.community.id}/ownership/transfer`,{method:'POST',body:JSON.stringify({userId:btn.dataset.owner})});toast('Ownership transferred.');await openCommunity(state.community.id,'members')}catch(e){toast(e.message,'error')}finally{busy(btn,false)}}

async function loadRequests(){const box=document.querySelector('#requests-list');box.innerHTML='<div class="loading-screen"><div class="spinner"></div></div>';try{const out=await api(`/communities/${state.community.id}/join-requests`);box.innerHTML=out.requests.map(m=>`<article class="request-card"><div class="person"><span class="person-mark">${esc(initials(m.user.fullName))}</span><div><strong>${esc(m.user.fullName)}</strong><small>${esc(m.user.email)}</small></div></div><p>${esc(m.user.vehicleName)} · ${esc(m.user.licensePlate)}</p><div class="actions"><button class="button primary small" data-request="${esc(m.id)}" data-decision="approve">Approve</button><button class="button outline small" data-request="${esc(m.id)}" data-decision="reject">Reject</button></div></article>`).join('')||'<div class="empty-state"><h2>All caught up.</h2><p>There are no pending requests.</p></div>';box.querySelectorAll('[data-request]').forEach(x=>x.onclick=()=>decideRequest(x))}catch(e){box.innerHTML='';toast(e.message,'error')}}
async function decideRequest(btn){busy(btn);try{await api(`/communities/${state.community.id}/join-requests/${btn.dataset.request}/${btn.dataset.decision}`,{method:'POST',body:'{}'});toast(`Request ${btn.dataset.decision}d.`);await loadRequests()}catch(e){toast(e.message,'error')}finally{busy(btn,false)}}

function initDesigner(){
  state.draft={rows:state.community.gridRows,cols:state.community.gridCols,version:state.community.layoutVersion,cells:new Map(state.community.cells.map(c=>[`${c.row}:${c.col}`,{...c,occupant:c.occupant?{...c.occupant}:null}]))};document.querySelector('#grid-rows').value=state.draft.rows;document.querySelector('#grid-cols').value=state.draft.cols;renderPalette();renderDesigner();document.querySelector('#grid-rows').onchange=resizeDraft;document.querySelector('#grid-cols').onchange=resizeDraft;
}
function renderPalette(){const p=document.querySelector('#tool-palette');p.innerHTML=cellTypes.map(t=>`<button class="tool ${state.tool===t?'active':''}" data-tool="${t}">${t.replaceAll('_',' ')}</button>`).join('');p.querySelectorAll('[data-tool]').forEach(x=>x.onclick=()=>{state.tool=x.dataset.tool;renderPalette()})}
function resizeDraft(){const rows=Number(document.querySelector('#grid-rows').value),cols=Number(document.querySelector('#grid-cols').value);if(rows<1||rows>50||cols<1||cols>50)return toast('Grid dimensions must be between 1 and 50.','error');const removed=[...state.draft.cells.values()].filter(c=>c.row>=rows||c.col>=cols);if(removed.some(c=>c.status==='OCCUPIED')){toast('The new size would remove an occupied spot.','error');document.querySelector('#grid-rows').value=state.draft.rows;document.querySelector('#grid-cols').value=state.draft.cols;return}state.draft.rows=rows;state.draft.cols=cols;removed.forEach(c=>state.draft.cells.delete(`${c.row}:${c.col}`));renderDesigner()}
function renderDesigner(){const g=document.querySelector('#designer-grid');if(!g)return;g.style.gridTemplateColumns=`repeat(${state.draft.cols},auto)`;let html='';for(let r=0;r<state.draft.rows;r++)for(let c=0;c<state.draft.cols;c++)html+=cellHTML(state.draft.cells.get(`${r}:${c}`)||{row:r,col:c,type:'EMPTY'},true);g.innerHTML=html;g.querySelectorAll('[data-pos]').forEach(x=>x.onclick=()=>paintCell(x.dataset.pos))}
function paintCell(pos){const [row,col]=pos.split(':').map(Number),old=state.draft.cells.get(pos);if(old?.status==='OCCUPIED'&&(state.tool!=='PARKING'))return toast('Check out this occupied spot before changing it.','error');if(state.tool==='EMPTY'){state.draft.cells.delete(pos)}else if(state.tool==='PARKING'){const label=prompt('Parking spot name',old?.type==='PARKING'?old.label:'');if(!label?.trim())return;state.draft.cells.set(pos,{...old,row,col,type:'PARKING',label:label.trim(),status:old?.status||'AVAILABLE'})}else state.draft.cells.set(pos,{row,col,type:state.tool,label:''});renderDesigner()}
async function saveLayout(){const btn=document.querySelector('[data-action="save-layout"]');busy(btn);const cells=[...state.draft.cells.values()].map(({occupant,slotId,status,...cell})=>cell);try{const out=await api(`/communities/${state.community.id}/layout`,{method:'PUT',body:JSON.stringify({rows:state.draft.rows,cols:state.draft.cols,layoutVersion:state.draft.version,cells})});toast('Layout saved.');await refreshMap();state.community.layoutVersion=out.layoutVersion;initDesigner()}catch(e){toast(e.message,'error')}finally{busy(btn,false)}}

async function leaveCommunity(){if(state.community.role==='OWNER')return toast('Transfer ownership before leaving.','error');if(!confirm('Leave this community? You will lose access immediately.'))return;try{await api(`/communities/${state.community.id}/leave`,{method:'POST',body:'{}'});toast('You left the community.');await showDashboard()}catch(e){toast(e.message,'error')}}
function connectSocket(){disconnectSocket();const protocol=location.protocol==='https:'?'wss:':'ws:';const ws=new WebSocket(`${protocol}//${location.host}/api/v1/ws/communities/${state.community.id}`);state.socket=ws;const label=document.querySelector('#socket-state');ws.onopen=()=>{if(label)label.textContent='Updates connected'};ws.onclose=()=>{if(label)label.textContent='Reconnecting…';if(state.community)setTimeout(async()=>{if(!state.community||state.socket!==ws)return;try{await api(`/communities/${state.community.id}/map`);connectSocket()}catch(e){if(e.status===403||e.status===404){toast('Your community access has ended.','error');showDashboard()}}},1800)};ws.onmessage=async e=>{try{const event=JSON.parse(e.data);if(event.type==='ACCESS_REVOKED')return showDashboard();await refreshMap()}catch{}}}
function disconnectSocket(){if(state.socket){const ws=state.socket;state.socket=null;ws.onclose=null;ws.close()}}

function profileModal(){openModal(`<span class="eyebrow">Your details</span><h2>Profile</h2><form id="profile-form" class="stack"><label>Full name<input name="fullName" value="${esc(state.user.fullName)}" required></label><label>Phone<input name="phone" value="${esc(state.user.phone)}" required></label><label>Vehicle model<input name="vehicleName" value="${esc(state.user.vehicleName)}" required></label><label>License plate<input name="licensePlate" value="${esc(state.user.licensePlate)}" required></label><button class="button primary">Save profile</button></form>`);document.querySelector('#profile-form').onsubmit=async e=>{e.preventDefault();const b=e.submitter;busy(b);try{const out=await api('/me',{method:'PATCH',body:JSON.stringify(formJSON(e.target))});state.user=out.user;showNav();closeModal();toast('Profile updated.')}catch(x){toast(x.message,'error')}finally{busy(b,false)}}}

document.addEventListener('click',async e=>{const a=e.target.closest('[data-action]');if(!a)return;if(a.dataset.action==='dashboard')await showDashboard();if(a.dataset.action==='profile')profileModal();if(a.dataset.action==='logout'){await api('/auth/logout',{method:'POST',body:'{}'}).catch(()=>{});showAuth()}});
boot();
