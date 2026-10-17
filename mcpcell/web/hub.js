'use strict';
(() => {
const d=document,$=s=>d.querySelector(s),M=window.mcpcell;
const esc=v=>String(v??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const offline=Boolean(window.__MCPCELL_PREVIEW__);
const state={page:'detection',hub:null,kind:'agents',query:'',agentQuery:'',agentFilter:'all',selected:new Set(),busy:false,active:null,terminals:new Map(),polling:false};
window.mcpcellPage='detection';
const api=(path,body)=>M.api(path,body);
const cancel='<button class="button" data-close>取消</button>';
const uuid=()=>Array.from(crypto.getRandomValues(new Uint8Array(16)),b=>b.toString(16).padStart(2,'0')).join('');
const agent=id=>state.hub?.agents.find(a=>a.id===id);
const session=id=>state.hub?.sessions.find(s=>s.id===id);
const logo=a=>`<span class="agent-logo ${esc(a.id)}">${esc(a.short)}</span>`;
let pending=null;
function modal(title,content,actions='',subtitle=''){M.show(title,content,actions,subtitle);}
function clickButton(label,action,cls='primary'){return `<button class="button ${cls}" data-hub-action="${action}">${label}</button>`;}
function switchPage(page){
  if(!['agents','detection','mcp','session'].includes(page))return;
  state.page=page;window.mcpcellPage=page;
  ['agents','detection','mcp'].forEach(p=>{$(`#page-${p}`).hidden=p!==page;$(`#nav-${p}`).setAttribute('aria-selected',String(p===page));});
  $('#session-panels').hidden=page!=='session';
  if(page==='agents')renderAgents();if(page==='detection')renderDetection();
  renderTabs();if(page==='session')activateTerminal();
}
function foundRows(){
 const q=state.query.toLowerCase();
 if(state.kind==='agents')return state.hub.agents.filter(a=>`${a.name} ${a.command}`.toLowerCase().includes(q));
 return (M.snapshot?.entries||[]).filter(e=>`${e.name} ${e.client} ${e.source}`.toLowerCase().includes(q));
}
function canCollect(e){return state.kind==='agents'?(e.launchable&&!e.enrolled):(!e.managed&&e.canEnroll!==false&&e.transport==='stdio');}
function renderDetection(){
 if(!state.hub||!M.snapshot)return;
 const hub=state.hub,data=M.snapshot,found=hub.agents.filter(a=>a.found).length;
 $('#detect-summary').innerHTML=`<span>✓ ${offline||hub.mode==='demo'?'样例检测':'最近检测'}：</span><strong>${found}</strong> 个 Agent 程序<span> · </span><strong>${data.entries.length}</strong> 条 MCP 配置<span> · 程序发现不代表已登录</span>`;
 $('#detected-agent-count').textContent=hub.agents.length;$('#detected-mcp-count').textContent=data.entries.length;
 d.querySelectorAll('[data-detect-kind]').forEach(b=>b.setAttribute('aria-selected',String(b.dataset.detectKind===state.kind)));
 const rows=foundRows();
 $('#detection-results').innerHTML=rows.length?rows.map(e=>{
  const isAgent=state.kind==='agents',eligible=canCollect(e);
  const name=isAgent?e.name:e.name;
  const sub=isAgent?`${e.command} · ${e.found?e.detectionSource||'手动路径':'PATH 与已知目录未找到'}`:`${data.clients.find(c=>c.id===e.client)?.name||e.client} · ${e.source}`;
  const text=isAgent?(e.enrolled?'已收纳':e.launchable?'可收纳':e.found?'入口待核对':'未找到'):(e.managed?'已接入':eligible?'可接入':'仅检测');
  return `<div class="detected-row"><div class="detected-item"><input type="checkbox" data-detection-select="${esc(e.id)}" aria-label="选择 ${esc(name)}" ${eligible?'':'disabled'} ${state.selected.has(e.id)?'checked':''}>${isAgent?logo(e):'<span class="agent-logo">◇</span>'}<div><strong>${esc(name)}</strong><small title="${esc(sub)}">${esc(sub)}</small></div></div><span class="state ${e.enrolled||e.managed?'ready':''}">${text}</span>${eligible?`<button class="detected-action" data-collect="${esc(e.id)}">${isAgent?'收纳':'接入'}</button>`:`<button class="view-link" ${isAgent?`data-agent-info="${esc(e.id)}"`:`data-detection-info="${esc(e.id)}"`}>详情</button>`}</div>`;
 }).join(''):'<div class="panel-empty"><h2>没有匹配结果</h2><p>换个关键词，或添加明确的项目目录。</p></div>';
 updateDetectionSelection();
}
function updateDetectionSelection(){
 const available=foundRows().filter(canCollect),selected=available.filter(e=>state.selected.has(e.id));
 $('#detect-select-all').checked=Boolean(available.length)&&selected.length===available.length;
 $('#detect-select-all').indeterminate=selected.length>0&&selected.length<available.length;
 $('#detect-select-all').disabled=!available.length;
 $('#collect-selected').disabled=!selected.length||state.busy;
 $('#collect-selected').textContent=`${state.kind==='agents'?'收纳':'接入'}所选${selected.length?' '+selected.length:''} →`;
 $('#detection-note').textContent=selected.length?`已选择 ${selected.length} 项 · 操作前需要确认`:'仅检查已知位置与选择的目录，不运行程序';
}
function renderAgents(){
 if(!state.hub)return;
 const list=state.hub.agents.filter(a=>a.enrolled&&(state.agentFilter!=='pinned'||a.pinned)&&`${a.name} ${a.command}`.toLowerCase().includes(state.agentQuery.toLowerCase())).sort((a,b)=>Number(b.pinned)-Number(a.pinned));
 $('#agent-cards').innerHTML=list.length?list.map(a=>{
 const running=state.hub.sessions.filter(s=>s.agentId===a.id&&s.state==='running').length;
 const sources=a.client?(M.snapshot?.entries||[]).filter(e=>e.client===a.client).length:0;
 return `<article class="agent-card"><div class="agent-card-top">${logo(a)}<div class="agent-title"><h2>${esc(a.name)} ${a.pinned?'<span title="已置顶">·</span>':''}</h2><small>${esc(a.command)}${sources?` · ${sources} 条 MCP 来源`:a.id==='pi'?' · 原生终端':' · 本机启动入口'}</small></div><button class="more-button" data-agent-info="${esc(a.id)}" aria-label="管理 ${esc(a.name)}">···</button></div><p class="agent-description">${esc(a.description)}</p><div class="agent-card-bottom"><span class="agent-status"><i></i>${running?`${running} 个会话运行中`:!a.enabled?'已停用 · 不接受新会话':a.launchable?'就绪 · 按需启动':'程序入口需要核对'}</span><button class="button ${a.id==='claude-code'||a.id==='pi'?'primary':''}" data-launch-agent="${esc(a.id)}" ${!a.enabled||!a.launchable?'disabled':''}>新会话 ↗</button></div></article>`;
 }).join(''):`<div class="panel-empty"><h2>${state.agentFilter==='pinned'?'还没有置顶的 Agent':'把你的 Agent 放到这里'}</h2><p>在检测页收纳 Claude Code、Pi、Codex 等工具。<br>收纳不会安装、启动或修改它们的账号。</p><button class="button primary" data-page="detection">去检测 →</button></div>`;
 const total=state.hub.agents.filter(a=>a.enrolled).length,open=state.hub.sessions.filter(s=>s.state==='running').length;
 $('#agent-library-note').textContent=`已收纳 ${total} 个 · ${open} 个运行会话 · ${offline||state.hub.mode==='demo'?'演示模式':'沿用 Agent 本机配置'}`;
 d.querySelectorAll('[data-agent-filter]').forEach(b=>b.setAttribute('aria-pressed',String(b.dataset.agentFilter===state.agentFilter)));
}
function renderTabs(){
 if(!state.hub)return;
 const sessions=state.hub.sessions;
 $('#session-strip').hidden=!sessions.length;
 $('#session-tabs').innerHTML=sessions.map(s=>`<div class="session-chip ${state.page==='session'&&state.active===s.id?'active':''}"><button role="tab" aria-selected="${state.page==='session'&&state.active===s.id}" data-session-tab="${s.id}" title="${esc(s.title)}"><i class="${s.state==='exited'?'exited':''}"></i><span>${esc(s.title)}</span></button><button class="close-session" data-session-close="${s.id}" aria-label="关闭 ${esc(s.title)}">×</button></div>`).join('');
}
function refreshHub(hub){
 state.hub=hub;$('#agent-total').textContent=hub.agents.filter(a=>a.enrolled).length;
 $('#mcp-total').textContent=M.groups().length;
 $('#agent-running').textContent=`Agent ${hub.sessions.filter(s=>s.state==='running').length}`;
 renderTabs();if(state.page==='agents')renderAgents();if(state.page==='detection')renderDetection();
 for(const s of hub.sessions){const title=$(`[data-panel="${s.id}"] .terminal-title strong`);if(title)title.textContent=s.title;}
}
function onSnapshot(data){if(data.hub)refreshHub(data.hub);else if(state.hub){$('#mcp-total').textContent=M.groups().length;if(state.page==='detection')renderDetection();}}
d.addEventListener('mcpcell:snapshot',e=>onSnapshot(e.detail));
if(M.snapshot)onSnapshot(M.snapshot);
async function busy(task){if(state.busy)return;state.busy=true;d.querySelectorAll('#dialog button').forEach(b=>b.disabled=true);updateDetectionSelection();try{await task();}catch(e){M.toast(e.message);}finally{state.busy=false;d.querySelectorAll('#dialog button').forEach(b=>b.disabled=false);updateDetectionSelection();}}
function collect(ids){
 if(state.kind==='mcp'){M.confirmEnroll(ids);return;}
 pending={ids};
 modal(`收纳 ${ids.length} 个 Agent？`,`<p class="explain">${ids.map(id=>esc(agent(id)?.name)).join('、')}</p><div class="notice">只保存启动入口，不安装、不运行、不改账号。开启会话时会再次确认工作目录。</div>`,cancel+clickButton('确认收纳','enroll'));
}
function agentInfo(id){
 const a=agent(id);if(!a)return;pending={agentId:id};
 const sources=a.client?(M.snapshot?.entries||[]).filter(e=>e.client===a.client):[];
 const sourceBlock=sources.length?`<div class="detail-item"><div class="detail-label">MCP 配置来源 · ${sources.length} 条</div><div class="detail-value">${esc([...new Set(sources.map(e=>e.name))].join('、'))}</div><button class="text-action" data-link-mcp="${esc(a.client)}">去 MCP 管理 →</button></div>`:'';
 modal(a.name,`<div class="agent-preview-row">${logo(a)}<div><strong>${a.enrolled?'已收纳':'尚未收纳'} · ${a.found?'找到入口':'未找到入口'}</strong><p>程序发现与登录状态分开，当前未检测账号。</p></div></div><div class="detail-item"><div class="detail-label">启动程序</div><div class="file-path">${esc(a.executable||'未找到。可在“手动添加”中指定绝对路径。')}</div>${a.reason?`<p class="dialog-status">${esc(a.reason)}</p>`:''}</div>${sourceBlock}<div class="notice">${a.id==='pi'?'Pi 没有内置权限审批，默认具有启动用户的权限。MCPcell 终端不是沙箱。':'保留此 Agent 的原生交互与本机配置，不添加跳过批准参数。'}${a.enrolled?'<br>停用仅阻止新会话，正在运行的会话需单独结束。':''}</div>`,a.enrolled?`<button class="button" data-hub-action="pin">${a.pinned?'取消置顶':'置顶'}</button><button class="button" data-hub-action="enable">${a.enabled?'停用':'启用'}</button><button class="button danger" data-hub-action="remove-confirm">移出收纳</button>`:a.launchable?clickButton('收纳 Agent','enroll-one'):'');
}
function launchDialog(id){
 if(!window.MCPcellTerminal){modal('先准备终端组件','<div class="notice">当前缺少本地终端资源，尚未启动 Agent。<br>运行 <code>python scripts/setup_terminal.py</code> 后刷新，或使用附带资源的源码 ZIP。</div>');return;}
 const available=state.hub.agents.filter(a=>a.enrolled&&a.enabled&&a.launchable);
 if(!available.length){M.toast('先在检测页收纳一个可启动的 Agent。');switchPage('detection');return;}
 const a=available.find(a=>a.id===id)||available[0];
 pending={requestId:uuid()};
 modal('开启 Agent 会话',`<label class="form-field"><span>Agent</span><select id="launch-agent">${available.map(x=>`<option value="${esc(x.id)}" ${x.id===a.id?'selected':''}>${esc(x.name)}</option>`).join('')}</select></label><label class="form-field"><span>工作目录</span><input id="launch-cwd" value="${esc(a.lastCwd||state.hub.defaultCwd)}" placeholder="绝对路径，如 D:\\work\\project" autocomplete="off" spellcheck="false"><small>使用独立进程与终端；不会自动创建 Git worktree。</small></label><div class="notice warn">将运行本机 Agent。它可能访问工作目录、使用已有账号并产生模型费用；本窗口不是执行沙箱。${offline||state.hub.mode==='demo'?'<br><strong>当前为演示模式，不会真正启动或发送请求。</strong>':''}</div>`,cancel+clickButton('启动并打开页签','launch'));
 $('#launch-agent').addEventListener('change',e=>{const a=agent(e.target.value);$('#launch-cwd').value=a.lastCwd||state.hub.defaultCwd;});
}
function registerDialog(){
 modal('手动添加 Agent',`<label class="form-field"><span>名称</span><input id="custom-name" maxlength="60" placeholder="例如：我的 Pi 工作配置"></label><label class="form-field"><span>可执行文件的绝对路径</span><input id="custom-exe" placeholder="例如 C:\\tools\\agent.exe" spellcheck="false"></label><label class="form-field"><span>附加参数 · JSON 数组</span><textarea id="custom-args" spellcheck="false">[]</textarea><small>不接受拼接 shell 命令；不要在参数中填写 API 密钥。</small></label><div class="notice">登记前只检查文件。启动时才运行；账号登录仍由原 Agent 完成。</div>`,cancel+clickButton('保存到收纳','register'));
}
function confirmClose(id){const s=session(id);if(!s)return;pending={sessionId:id};if(s.state==='exited'){busy(()=>closeSession(id));return;}modal('结束会话并关闭页签？',`<p class="explain">${esc(s.title)}</p><div class="notice warn">会终止此页签所属的 Agent。进行中的操作可能已产生副作用，不会自动撤销。其他页签不受影响。</div>`,cancel+clickButton('结束并关闭','close','danger'));}
async function closeSession(id){if(offline)state.hub.sessions=state.hub.sessions.filter(s=>s.id!==id);else state.hub=await api('/api/agents/close',{sessionId:id,confirm:true});const terminal=state.terminals.get(id);terminal?.observer?.disconnect();terminal?.term?.dispose();state.terminals.delete(id);$(`[data-panel="${id}"]`)?.remove();$('#dialog').close();refreshHub(state.hub);if(state.active===id){state.active=state.hub.sessions.at(-1)?.id||null;switchPage(state.active?'session':'agents');}}
function makeTerminal(s){
 let item=state.terminals.get(s.id);if(item)return item;
 const pane=d.createElement('section');pane.className='session-panel';pane.dataset.panel=s.id;pane.setAttribute('role','tabpanel');
 pane.innerHTML=`<div class="terminal-toolbar"><div class="terminal-title"><strong>${esc(s.title)}</strong><small>${esc(s.cwd)} · ${offline||state.hub.mode==='demo'?'演示终端':`PID ${s.pid}`}</small></div><div class="terminal-tools"><button data-session-rename="${s.id}">重命名</button><button data-session-interrupt="${s.id}" aria-label="发送 Ctrl C">Ctrl C</button><button class="stop-button" data-session-stop="${s.id}">结束</button></div></div><div class="terminal-wrap"><div class="terminal-host" id="terminal-${s.id}"></div></div><div class="terminal-note"><span>${offline||state.hub.mode==='demo'?'演示 · 输入不会执行':'原生交互 · 沿用 Agent 的登录和权限机制'}</span><span data-terminal-state>连接中</span></div>`;
 $('#session-panels').append(pane);
 item={offset:0,term:null,fit:null,pane,queue:[],sending:false,observer:null,connected:true,readBusy:false,demoLine:''};state.terminals.set(s.id,item);
 if(!window.MCPcellTerminal){pane.querySelector('.terminal-host').innerHTML='<div class="terminal-fallback">缺少终端前端资源。\n请运行 python scripts/setup_terminal.py，再刷新页面。\n\n会话不会因此被自动结束；可以点击“结束”停止。</div>';return item;}
 const {Terminal,FitAddon}=window.MCPcellTerminal;
 item.term=new Terminal({fontFamily:'ui-monospace, "Cascadia Mono", "SFMono-Regular", Consolas, monospace',fontSize:12.5,lineHeight:1.22,scrollback:1000,cursorBlink:true,convertEol:false,allowProposedApi:false,
  theme:{background:'#192019',foreground:'#dbe5d1',cursor:'#d0ef98',selectionBackground:'#637a4466',black:'#20251d',red:'#df8d78',green:'#bcdf84',yellow:'#d9c687',blue:'#9ab9d3',magenta:'#bba6cd',cyan:'#94c8b1',white:'#e3ebdc'},
  linkHandler:{activate:()=>M.toast('终端链接不会自动打开；请核对后自行访问。')}});
 item.fit=new FitAddon();item.term.loadAddon(item.fit);item.term.open(pane.querySelector('.terminal-host'));
 item.term.onData(text=>enqueueInput(s.id,text));
 item.term.onResize(({cols,rows})=>{if(!offline&&cols>=20&&rows>=5)api('/api/agents/resize',{sessionId:s.id,cols:Math.min(cols,300),rows:Math.min(rows,100)}).catch(()=>{});});
 item.observer=new ResizeObserver(()=>{if(state.page==='session'&&state.active===s.id)try{item.fit.fit();}catch{}});item.observer.observe(pane.querySelector('.terminal-wrap'));
 if(offline){item.term.writeln(`\x1b[1;32m${agent(s.agentId)?.name||'Agent'}\x1b[0m  · MCPcell 演示终端`);item.term.writeln('');item.term.writeln('这是独立会话页签的交互演示，不运行真实 Agent。');item.term.writeln('输入不会执行，也不会发送给模型。');item.term.writeln('');item.term.writeln(`工作目录  ${s.cwd}`);item.term.writeln('');item.term.write('\x1b[32m> \x1b[0m');}
 if(offline)item.pane.querySelector('[data-terminal-state]').textContent='演示 · 就绪';
 return item;
}
function activateTerminal(){const s=session(state.active);if(!s){switchPage('agents');return;}const item=makeTerminal(s);d.querySelectorAll('.session-panel').forEach(p=>p.hidden=p.dataset.panel!==s.id);item.term?.focus();requestAnimationFrame(()=>{try{item.fit?.fit();item.term?.focus();}catch{}readActive();});}
function enqueueInput(id,text){
 const item=state.terminals.get(id),s=session(id);if(!item||s?.state!=='running')return;
 if(new TextEncoder().encode(text).length>8192||item.queue.reduce((n,t)=>n+t.length,0)+text.length>16384){M.toast('粘贴内容过长，未发送；请分段输入。');return;}
 if(offline){for(const c of text){if(c==='\r'){item.term.write('\r\n\x1b[90m[演示] 已收到输入；未执行任何操作。\x1b[0m\r\n\r\n> ');item.demoLine='';}else if(c==='\x03'){item.term.write('^C\r\n> ');item.demoLine='';}else if(c==='\x7f'){if(item.demoLine){item.demoLine=item.demoLine.slice(0,-1);item.term.write('\b \b');}}else if(c>=' '){item.demoLine+=c;item.term.write(c);}}return;}
 item.queue.push(text);sendInput(id);
}
async function sendInput(id){const item=state.terminals.get(id);if(!item||item.sending)return;item.sending=true;try{while(item.queue.length){const text=item.queue.shift();await api('/api/agents/input',{sessionId:id,data:text});}}catch(e){item.queue=[];M.toast(e.message+' 输入未自动重试。');}finally{item.sending=false;}}
async function readActive(){
 if(offline||state.page!=='session'||d.hidden)return;
 const sid=state.active,item=state.terminals.get(sid);if(!item||!item.term||item.readBusy)return;item.readBusy=true;
 try{const r=await api(`/api/agents/output?sessionId=${sid}&offset=${item.offset}`);if(r.droppedBytes){item.term.reset();M.toast('终端输出超过保留上限，较早内容已丢弃；不代表任务重启。');}if(r.data){const raw=Uint8Array.from(atob(r.data),c=>c.charCodeAt(0));await new Promise(resolve=>item.term.write(raw,resolve));}item.offset=r.next;item.connected=true;const original=session(sid);if(original)Object.assign(original,r.session);const status=item.pane.querySelector('[data-terminal-state]');status.textContent=r.session.state==='running'?'运行中':`已结束 · ${r.session.exitCode??'退出码未知'}`;status.classList.remove('terminal-disconnected');renderTabs();}
 catch(e){item.connected=false;const status=item.pane.querySelector('[data-terminal-state]');status.textContent='连接中断 · 未重启';status.classList.add('terminal-disconnected');}
 finally{item.readBusy=false;}
}
async function launchNow(){
 const key=$('#launch-agent').value,cwd=$('#launch-cwd').value.trim(),requestId=pending.requestId;
 if(!cwd)throw new Error('请选择工作目录。');
 let s;
 if(offline){s={id:uuid(),agentId:key,title:`${agent(key).name} · ${cwd.split(/[\\/]/).filter(Boolean).at(-1)}`,cwd,pid:null,state:'running',exitCode:null,createdAt:Date.now()/1000,rssBytes:null,backend:'demo'};state.hub.sessions.push(s);agent(key).lastCwd=cwd;}
 else{const result=await api('/api/agents/launch',{agentId:key,cwd,requestId,confirm:true});s=result.session;state.hub=result.hub;}
 $('#dialog').close();$('#toast').classList.remove('visible');state.active=s.id;refreshHub(state.hub);switchPage('session');
}
d.addEventListener('click',event=>{
 const b=event.target.closest('button');if(!b)return;
 if(b.dataset.page){switchPage(b.dataset.page);return;}
 if(b.dataset.detectKind){state.kind=b.dataset.detectKind;state.selected.clear();renderDetection();return;}
 if(b.dataset.agentFilter){state.agentFilter=b.dataset.agentFilter;renderAgents();return;}
 if(b.dataset.collect){collect([b.dataset.collect]);return;}
 if(b.dataset.agentInfo){agentInfo(b.dataset.agentInfo);return;}
 if(b.dataset.detectionInfo){const e=M.snapshot.entries.find(e=>e.id===b.dataset.detectionInfo);const group=M.groups().find(g=>g.members.some(x=>x.id===e.id));if(group)M.detail(group.key);return;}
 if(b.dataset.launchAgent){launchDialog(b.dataset.launchAgent);return;}
 if(b.dataset.linkMcp){$('#dialog').close();switchPage('mcp');$('#client').value=b.dataset.linkMcp;$('#client').dispatchEvent(new Event('change'));return;}
 if(b.dataset.sessionTab){state.active=b.dataset.sessionTab;switchPage('session');return;}
 if(b.dataset.sessionClose){confirmClose(b.dataset.sessionClose);return;}
 if(b.dataset.sessionInterrupt){enqueueInput(b.dataset.sessionInterrupt,'\x03');return;}
 if(b.dataset.sessionStop){pending={sessionId:b.dataset.sessionStop};modal('结束此 Agent 会话？','<div class="notice warn">停止所属进程，不撤销已发生的操作。页签保留以便查看输出，其他会话不受影响。</div>',cancel+clickButton('结束会话','stop','danger'));return;}
 if(b.dataset.sessionRename){pending={sessionId:b.dataset.sessionRename};modal('重命名会话',`<label class="form-field"><span>名称</span><input id="session-title" maxlength="80" value="${esc(session(pending.sessionId)?.title)}"></label>`,cancel+clickButton('保存','rename'));return;}
 const action=b.dataset.hubAction;if(!action||state.busy)return;
 if(action==='remove-confirm'){const id=pending.agentId;modal('移出 Agent 收纳？','<p class="explain">不会卸载 Agent 或删除它的账号和历史。仍有运行会话时会拒绝移出。</p>',cancel+clickButton('移出收纳','remove','danger'));pending={agentId:id};return;}
 busy(async()=>{
  if(action==='enroll'||action==='enroll-one'){const ids=action==='enroll'?pending.ids:[pending.agentId];if(offline)ids.forEach(id=>Object.assign(agent(id),{enrolled:true,enabled:true}));else state.hub=await api('/api/agents/enroll',{ids,confirm:true});state.selected.clear();$('#dialog').close();refreshHub(state.hub);M.toast('已收纳，尚未启动。');switchPage('agents');}
  else if(action==='launch')await launchNow();
  else if(action==='register'){let args;try{args=JSON.parse($('#custom-args').value);}catch{throw new Error('附加参数须为 JSON 数组，例如 []。');}if(offline)throw new Error('离线演示不登记本机程序；实际添加请运行源码。');state.hub=await api('/api/agents/register',{name:$('#custom-name').value,executable:$('#custom-exe').value,args,confirm:true});$('#dialog').close();refreshHub(state.hub);}
  else if(action==='pin'||action==='enable'){const a=agent(pending.agentId);const enabled=action==='enable'?!a.enabled:a.enabled,pinned=action==='pin'?!a.pinned:a.pinned;if(offline)Object.assign(a,{enabled,pinned});else state.hub=await api('/api/agents/update',{agentId:a.id,enabled,pinned});refreshHub(state.hub);$('#dialog').close();}
  else if(action==='remove'){const id=pending.agentId;if(offline){if(state.hub.sessions.some(s=>s.agentId===id&&s.state==='running'))throw new Error('请先结束此 Agent 的运行会话。');agent(id).enrolled=false;}else state.hub=await api('/api/agents/remove',{agentId:id,confirm:true});$('#dialog').close();refreshHub(state.hub);}
  else if(action==='close')await closeSession(pending.sessionId);
  else if(action==='stop'){const s=session(pending.sessionId);if(offline){s.state='exited';s.exitCode=0;}else state.hub=await api('/api/agents/stop',{sessionId:s.id,confirm:true});$('#dialog').close();refreshHub(state.hub);const item=state.terminals.get(s.id);if(item)item.pane.querySelector('[data-terminal-state]').textContent='已结束';}
  else if(action==='rename'){const title=$('#session-title').value.trim();if(!title)throw new Error('请填写会话名称。');if(offline)session(pending.sessionId).title=title;else state.hub=await api('/api/agents/rename',{sessionId:pending.sessionId,title});$('#dialog').close();refreshHub(state.hub);}
  else if(action==='scope'){const workspace=$('#scan-workspace').value.trim();if(offline)throw new Error('离线演示不读取目录；请在本机运行源码。');const result=await api('/api/detection/scope',{workspace,confirm:true});M.setSnapshot(result);$('#dialog').close();M.toast('已纳入本次扫描范围；没有执行项目内程序。');}
 });
});
$('#new-session').onclick=()=>launchDialog();$('#session-add').onclick=()=>launchDialog();$('#register-agent').onclick=registerDialog;
$('#agent-search').oninput=e=>{state.agentQuery=e.target.value;renderAgents();};
$('#detection-search').oninput=e=>{state.query=e.target.value;state.selected.clear();renderDetection();};
$('#collect-selected').onclick=()=>collect(foundRows().filter(e=>canCollect(e)&&state.selected.has(e.id)).map(e=>e.id));
$('#detect-select-all').onchange=e=>{for(const x of foundRows().filter(canCollect)){if(e.target.checked)state.selected.add(x.id);else state.selected.delete(x.id);}renderDetection();};
d.addEventListener('change',e=>{if(e.target.dataset.detectionSelect){if(e.target.checked)state.selected.add(e.target.dataset.detectionSelect);else state.selected.delete(e.target.dataset.detectionSelect);updateDetectionSelection();}});
$('#detect-all').onclick=()=>busy(async()=>{const b=$('#detect-all');b.textContent='检测中…';b.setAttribute('aria-busy','true');try{if(offline){state.hub.detectedAt=Date.now()/1000;M.toast('样例检测完成，不读取本机。');}else{const result=await api('/api/rescan',{});M.setSnapshot(result);M.toast('检测完成。未运行程序，也没有自动收纳或接入。');}renderDetection();}finally{b.textContent='↻ 扫描本机';b.removeAttribute('aria-busy');}});
$('#add-scope').onclick=()=>modal('添加检测目录','<label class="form-field"><span>项目绝对路径</span><input id="scan-workspace" placeholder="如 D:\\work\\project" autocomplete="off"><small>只检查这个目录下已知的 MCP 配置位置，不递归查找磁盘。</small></label>',cancel+clickButton('确认并检测','scope'));
d.addEventListener('keydown',e=>{if((e.ctrlKey||e.metaKey)&&e.key.toLowerCase()==='k'&&state.page!=='mcp'){e.preventDefault();$('#dialog').close();if(state.page==='session')switchPage('agents');$(state.page==='agents'?'#agent-search':'#detection-search').focus();}if(e.altKey&&['1','2','3'].includes(e.key)){e.preventDefault();switchPage(['detection','agents','mcp'][Number(e.key)-1]);}const tab=e.target.closest('[role="tab"]');if(tab&&['ArrowLeft','ArrowRight'].includes(e.key)){const all=[...tab.closest('[role="tablist"]').querySelectorAll('[role="tab"]')];let i=(all.indexOf(tab)+(e.key==='ArrowRight'?1:-1)+all.length)%all.length;e.preventDefault();all[i].focus();all[i].click();}});
setInterval(readActive,250);
if(!offline)setInterval(async()=>{if(d.hidden||state.busy||$('#dialog').open)return;try{refreshHub(await api('/api/agents'));}catch{/* Keep last known state; do not infer zero running agents. */}},3500);
// No auto-launch on load or reload. Server-owned sessions are listed for explicit reattachment.
window.mcpcellHub={switchPage,get hub(){return state.hub;}};
})();
