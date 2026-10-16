'use strict';
(() => {
  const paths = {
    grid:'<rect x="3" y="3" width="7" height="7" rx="2"/><rect x="14" y="3" width="7" height="7" rx="2"/><rect x="3" y="14" width="7" height="7" rx="2"/><rect x="14" y="14" width="7" height="7" rx="2"/>',
    layers:'<path d="m12 3 9 5-9 5-9-5 9-5Z"/><path d="m3 12 9 5 9-5M3 16l9 5 9-5"/>',
    devices:'<rect x="3" y="4" width="14" height="11" rx="2"/><path d="M7 20h7M10 15v5"/><rect x="17" y="10" width="5" height="10" rx="1"/>',
    shield:'<path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Z"/><path d="m8 12 3 3 5-6"/>',
    terminal:'<rect x="3" y="4" width="18" height="16" rx="3"/><path d="m7 9 3 3-3 3m6 0h4"/>',
    lock:'<rect x="5" y="10" width="14" height="11" rx="3"/><path d="M8 10V7a4 4 0 0 1 8 0v3m-4 5v2"/>',
    settings:'<path d="M4 6h16M4 12h16M4 18h16"/><circle cx="8" cy="6" r="2" fill="currentColor"/><circle cx="16" cy="12" r="2" fill="currentColor"/><circle cx="10" cy="18" r="2" fill="currentColor"/>',
    search:'<circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/>',
    refresh:'<path d="M20 7v5h-5M4 17v-5h5"/><path d="M6 6a8 8 0 0 1 13 3M5 15a8 8 0 0 0 13 3"/>',
    arrow:'<path d="M4 12h15m-5-5 5 5-5 5"/>',
    chevron:'<path d="m9 5 7 7-7 7"/>',
    download:'<path d="M12 3v12m-5-5 5 5 5-5M4 16v4h16v-4"/>',
    leaf:'<path d="M20 3s0 12-6 15c-5 3-10-2-8-7 2-5 14-8 14-8Z"/><path d="M4 21 15 10"/>',
    check:'<path d="m5 12 4 4L19 6"/>',
    alert:'<path d="m12 3 10 18H2L12 3Z"/><path d="M12 9v5m0 3v.01"/>',
    link:'<path d="m9 15 6-6m-7 8-2 2a4 4 0 0 1-6-6l4-4a4 4 0 0 1 6 0m4 0 2-2a4 4 0 0 1 6 6l-4 4a4 4 0 0 1-6 0" transform="translate(1 -1)"/>',
    globe:'<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a18 18 0 0 1 0 18 18 18 0 0 1 0-18Z"/>',
    close:'<path d="m6 6 12 12M6 18 18 6"/>',
    clock:'<circle cx="12" cy="12" r="9"/><path d="M12 7v5l3 2"/>',
    file:'<path d="M5 3h9l5 5v13H5V3Zm9 0v6h5M8 13h8m-8 4h6"/>',
    sliders:'<path d="M5 3v18M12 3v18M19 3v18"/><path d="M2 8h6m1 9h6m1-10h6"/>'
  };
  const icon = n => `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${paths[n] || paths.layers}</svg>`;
  const esc = v => String(v ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  document.querySelectorAll('[data-icon]').forEach(el => { el.innerHTML = icon(el.dataset.icon); });
  const titles = {overview:'概览', services:'服务清单', clients:'AI 客户端', review:'审查与迁移', cli:'CLI 接入', about:'能力与边界'};
  const state = {snapshot:null, view:'overview', query:'', filter:'all', client:'all', selected:new Set()};
  const main = document.querySelector('#main');
  const offline = Boolean(window.__MCPCELL_PREVIEW__);
  let toastTimer;
  function toast(text) { const el=document.querySelector('#toast'); el.textContent=text; el.classList.add('visible'); clearTimeout(toastTimer); toastTimer=setTimeout(()=>el.classList.remove('visible'),4200); }
  async function api(path, method='GET', token='') {
    const headers = method === 'POST' ? {'Content-Type':'application/json'} : {};
    if(token) headers.Authorization=`Bearer ${token}`;
    const response = await fetch(path, {method, credentials:'same-origin', headers, ...(method==='POST'?{body:'{}'}:{})});
    if(!response.ok) throw new Error(response.status===401?'请使用终端打印的含临时凭据的启动链接。':response.status===429?'扫描间隔过短，请稍后重试。':`本地接口返回 ${response.status}；操作未完成。`);
    return response.json();
  }
  function clientName(id) { return state.snapshot.clients.find(c=>c.id===id)?.name || id; }
  function scopeName(s) {return ({user:'用户级', workspace:'项目级',local:'本地项目',custom:'自定义'})[s] || s;}
  const tag=(text,kind='neutral')=>`<span class="tag ${kind}">${esc(text)}</span>`;
  const button=(label,action,ico='arrow',primary=false)=>`<button class="button ${primary?'primary':''}" data-action="${action}">${icon(ico)}${esc(label)}</button>`;
  function heading(title, subtitle, action='rescan') {
    return `<div class="page-heading"><div><p class="eyebrow">YOUR TOOLS. ONE QUIET PLACE.</p><h1>${esc(title)}</h1><p class="subtitle">${esc(subtitle)}</p></div><div class="heading-actions">${button('重新扫描','rescan','refresh')}${action==='plan'?button('导出审查提案','export-plan','download',true):button('审查迁移建议','review','sliders',true)}</div></div>`;
  }
  function stats() {
    const s=state.snapshot.stats;
    return `<div class="stats-grid">${[
      ['发现配置的客户端',s.clientsFound,'/ 11','已知位置发现，不代表运行中','devices'],
      ['配置绑定',s.definitions,'条','按客户端及配置来源分别保留','layers'],
      ['同配置候选组',s.candidateGroups,'组','配置相同 ≠ 可以共享进程','link'],
      ['需要核对',s.reviewCount,'条','静态线索，不是安全认证','shield']
    ].map(([title,value,unit,note,ico])=>`<article class="glass stat-card"><div class="stat-title">${esc(title)}${icon(ico)}</div><div class="stat-value"><strong>${value}</strong><span>${unit}</span></div><p class="stat-note">${esc(note)}</p></article>`).join('')}</div>`;
  }
  function serviceRows(entries, compact=false) {
    return entries.map(e=>`<tr>${compact?'':`<td><input type="checkbox" data-select="${esc(e.id)}" aria-label="选择 ${esc(e.name)}" ${state.selected.has(e.id)?'checked':''}></td>`}<td><div class="service-label"><span class="service-icon">${icon(e.transport==='remote'?'globe':'layers')}</span><div><button class="service-name" data-detail="${esc(e.id)}">${esc(e.name)}</button><p class="service-sub">${esc(e.endpoint)}</p></div></div></td><td><span class="client-chip">${esc(clientName(e.client))}</span></td>${compact?'':`<td>${esc(scopeName(e.scope))}</td>`}<td>${tag(e.configuredState==='disabled'?'配置已禁用':e.candidateCount>1?'复用待评估':'尚未托管',e.configuredState==='disabled'?'neutral':e.candidateCount>1?'mint':'outline')}</td><td class="numeric"><button class="icon-button" data-detail="${esc(e.id)}" aria-label="查看 ${esc(e.name)}">${icon('chevron')}</button></td></tr>`).join('');
  }
  function table(entries,compact=false) {
    if(!entries.length) return `<div class="empty-state"><h2>没有匹配的配置</h2><p>试试其他筛选条件，或显式指定需要扫描的工作区。</p></div>`;
    return `<div class="table-wrap"><table><thead><tr>${compact?'':'<th><input id="select-all" type="checkbox" aria-label="选择当前筛选下所有条目"></th>'}<th>服务</th><th>来源客户端</th>${compact?'':'<th>作用域</th>'}<th>治理状态</th><th class="numeric">详情</th></tr></thead><tbody>${serviceRows(entries,compact)}</tbody></table></div>`;
  }
  function overview() {
    const x=state.snapshot,s=x.stats;
    const seen=x.clients.filter(c=>c.found>0);
    return heading('工具有序，工作轻盈。','发现分散的 MCP 配置，在统一接入前看清每一个边界。')+
      `<section class="glass hero"><div class="hero-copy"><p class="eyebrow">A LIGHTER WAY TO WORK</p><h2>让每一份工具资源，都有归属。</h2><p>连接你的 AI 工作流，而不是再启动一整套服务。<br>先发现、再审查，逐步进入受控运行。</p><div class="hero-links"><button class="text-button" data-view="clients">查看客户端接入 ${icon('arrow')}</button><span>当前阶段 · 只读发现</span></div></div><div class="hero-art" aria-label="配置清单示意，不是资源监控"><span class="orbit-label left">MCP</span><div class="liquid-orb"><div class="orb-content"><strong>${s.definitions}</strong><span>配置绑定</span></div></div><span class="orbit-label right">CLI · NEXT</span></div></section>`+
      stats()+`<div class="main-grid"><section class="glass section-card"><div class="section-heading"><div><h2>服务清单 <span class="count">${s.definitions}</span></h2><p class="subtitle">保留来源，不直接合并</p></div><button class="text-button" data-view="services">查看全部 ${icon('arrow')}</button></div>${table(x.entries.slice(0,5),true)}<p class="table-note">这里展示配置声明；当前尚未观测进程，也没有接管运行。</p></section><aside class="aside-column"><section class="glass review-card"><div class="section-heading"><h2>接管之前</h2>${tag('建议','amber')}</div><p>先处理边界，再决定如何共享。</p><div class="review-item"><span class="review-icon">${icon('link')}</span><div><strong>${s.candidateGroups} 组同配置候选</strong><p>需验证工作区、凭据和状态隔离，<br>不会自动合并成一个进程。</p></div></div><div class="review-item"><span class="review-icon">${icon('shield')}</span><div><strong>${s.reviewCount} 条配置需要核对</strong><p>检查凭据、启动方式和批准设置，<br>保留原客户端的授权边界。</p></div></div><div class="review-divider"></div><button class="button block" data-view="review">打开审查工作台 ${icon('arrow')}</button></section><section class="compact-note"><div class="note-head">${icon('leaf')}不把愿景当成已实现</div><p>运行实例：未观测 · 内存节省：未测量<br>当前不会运行工具或修改配置。</p></section></aside></div>`+
      `<section class="clients-strip"><div class="section-heading"><h2>你的 AI 客户端</h2><button class="text-button" data-view="clients">全部适配器 ${icon('arrow')}</button></div><div class="client-mini-grid">${(seen.length?seen:x.clients).slice(0,4).map(c=>`<button class="glass client-mini" data-client-detail="${c.id}"><span class="monogram">${c.monogram}</span><div><strong>${c.name}</strong><p>${c.found?`${c.entryCount} 条配置绑定`:'未发现配置'}</p></div><span class="tiny-dot"></span></button>`).join('')}</div></section>`;
  }
  function filtered() {
    const q=state.query.toLowerCase().trim();
    return state.snapshot.entries.filter(e=>(state.client==='all'||e.client===state.client)&&(!q||`${e.name} ${clientName(e.client)} ${e.endpoint} ${e.source}`.toLowerCase().includes(q))&&(state.filter==='all'||(state.filter==='candidates'&&e.candidateCount>1)||(state.filter==='review'&&e.issues.length>0)||(state.filter==='disabled'&&e.configuredState==='disabled')));
  }
  function services() {
    return heading('服务清单','每条配置有来源，每次接入有边界。这里只显示声明，不推断客户端是否启用。','plan')+
      `<div class="toolbar"><label class="search-box">${icon('search')}<input id="service-search" type="search" placeholder="搜索服务、客户端或来源…" value="${esc(state.query)}" aria-label="搜索服务"><kbd>⌘ K</kbd></label><div class="segmented" role="group" aria-label="配置筛选">${[['all','全部'],['candidates','重复候选'],['review','需核对'],['disabled','已禁用']].map(([id,t])=>`<button data-filter="${id}" class="${state.filter===id?'active':''}" aria-pressed="${state.filter===id}">${t}</button>`).join('')}</div><select id="client-filter" class="select" aria-label="按客户端筛选"><option value="all">全部客户端</option>${state.snapshot.clients.map(c=>`<option value="${c.id}" ${state.client===c.id?'selected':''}>${c.name}</option>`).join('')}</select></div><section class="glass full-table"><div id="table-region">${table(filtered())}</div><p class="table-note" id="selection-note">已选 ${state.selected.size} 条 · 导出的是审查提案，不是可直接应用的客户端配置。</p></section>`;
  }
  function clients() {
    return heading('AI 客户端','不同配置格式，同一个发现入口。保留客户端原有的配置与权限语义。')+
      `<div class="section-note">自动检查已知用户级路径。项目级配置需在启动时通过 <code>--workspace</code> 显式指定；自定义路径使用 <code>--config CLIENT=PATH</code>。未发现配置，不等于未安装客户端。</div><div class="client-grid">${state.snapshot.clients.map(c=>`<article class="glass client-card"><div class="card-top"><span class="monogram">${c.monogram}</span><h3>${c.name}</h3>${tag(c.found?'发现配置':'未发现',c.found?'mint':'outline')}</div><p>${c.format} 只读解析<br>${c.found?`发现 ${c.found} 个配置来源，未改变原有设置。`:'已知位置未发现配置，可检查自定义路径。'}</p><div class="source-count"><span><strong>${c.entryCount}</strong> 条绑定</span><button class="text-button" data-client-detail="${c.id}">查看来源 ${icon('arrow')}</button></div></article>`).join('')}</div>`;
  }
  function findings() {
    return state.snapshot.entries.flatMap(e=>e.issues.map(i=>({...i, entry:e}))).sort((a,b)=>(b.severity==='high')-(a.severity==='high'));
  }
  function review() {
    const list=findings();
    return heading('审查与迁移','发现不是信任。先核对配置，再谈资源复用与自动接管。','plan')+
      `<div class="inline-warning">当前仅生成审查提案。配置回写、事务备份、回滚与运行接管尚未实现，界面不会执行这些操作。</div><div class="review-layout"><section class="glass section-card"><div class="section-heading"><h2>配置审查线索 <span class="count">${list.length}</span></h2>${tag('静态检查','outline')}</div>${list.length?list.map(f=>`<div class="finding">${tag(f.severity==='high'?'优先核对':'需核对',f.severity==='high'?'red':'amber')}<div><h3>${esc(f.title)}</h3><p><button class="text-button" data-detail="${f.entry.id}">${esc(f.entry.name)} · ${esc(clientName(f.entry.client))} ${icon('arrow')}</button></p></div></div>`).join(''):'<div class="empty-state"><h2>未发现预设风险线索</h2><p>这不代表配置或服务已通过安全验证。</p></div>'}</section><aside><section class="glass section-card"><h2>渐进接入，不打断工作</h2><ol class="steps"><li><strong>发现与归一化 · 已实现</strong><p>只读解析，保留来源、作用域和配置差异。</p></li><li><strong>审查提案 · 已实现</strong><p>默认独占；凭据留在源配置；不会执行。</p></li><li><strong>事务迁移 · 待实现</strong><p>备份、对比、原子替换、校验与可逆回滚。</p></li><li><strong>运行治理 · 待实现</strong><p>进程归属、资源限制、租约与安全复用。</p></li></ol><button class="button primary block" data-action="export-plan">${icon('download')}导出审查提案</button><p class="subtitle">所有限制和阻断项都会写入提案。</p></section><section class="compact-note"><div class="note-head">${icon('lock')}敏感值留在配置文件</div><p>浏览器不接收参数值、环境变量值、认证头或 OAuth 原文。自定义命令路径默认隐藏。</p></section></aside></div>`;
  }
  function cli() {
    return heading('CLI，也在同一套秩序里。','统一执行契约，而不是再造一个任意命令终端。')+
      `<section class="glass prose"><p class="eyebrow">ONE CONTRACT. MULTIPLE ADAPTERS.</p>${tag('设计预留 · 尚未执行','amber')}<h2>前端不依赖底层调用协议</h2><p>MCP 和 CLI 将使用同一份服务身份、工作区、权限、资源预算与审计记录。CLI 接入通过显式注册的参数模板，而不是把任意文本交给 shell。</p><div class="contract-box">ToolDefinition
  ├─ identity / source / workspace
  ├─ authorization / approval
  ├─ resourceBudget / lifecycle
  └─ adapter
      ├─ mcp-stdio
      ├─ mcp-http
      └─ cli-argv  [planned]</div><p>当前程序只提供扫描与审查命令，没有 CLI 执行 API，也不会通过 <code>--help</code> 或 <code>--version</code> 运行发现到的程序。</p></section><div class="capability-grid"><article class="glass capability">${icon('terminal')}<h2>结构化参数</h2><p>注册可执行制品、允许的子命令和参数模式。固定工作目录与环境白名单，禁止隐式 shell 拼接。</p></article><article class="glass capability">${icon('shield')}<h2>同样的治理边界</h2><p>超时、输出预算、并发、批准和取消语义将与 MCP 共用。没有安全执行后端时，拒绝执行。</p></article></div>`;
  }
  function about() {
    const x=state.snapshot;
    return heading('能力与边界','能做到什么、还没做到什么，都应该看得清楚。','plan')+
      `<div class="capability-grid"><article class="glass capability">${tag('本版已实现','mint')}<h2>本地只读工作台</h2><p>11 类配置适配器；JSON / JSONC / TOML 解析；显式工作区与自定义路径；来源追踪；同配置候选；静态审查；隐藏敏感值；审查提案导出。</p><button class="text-button" data-action="export-inventory">导出当前清单 ${icon('download')}</button></article><article class="glass capability">${tag('尚未实现','amber')}<h2>运行治理与迁移</h2><p>MCP/CLI 执行、进程共享、Job Objects/cgroups、凭据托管、自动配置回写、事务备份及回滚。当前没有经过测量的内存节省数据。</p></article><article class="glass capability"><h2>扫描范围</h2><p>${x.limitations.map(esc).join('<br>')}</p></article><article class="glass capability"><h2>访问与隐私</h2><p>仅监听 127.0.0.1，API 需要随机访问凭据；限制 Host 与 Origin。资源全部本地加载。扫描限额：单文件 1 MiB，256 个来源，1024 条绑定。</p><p>不防御已完全控制同一系统账号的恶意进程；本版也不是工具执行沙箱。</p></article></div>`+
      `<section class="glass section-card clients-strip"><h2>扫描诊断 · ${x.errors.length} 条</h2>${x.errors.length?x.errors.map(e=>`<div class="finding"><div><h3>${esc(e.message)}</h3><p>${esc(e.source)}</p></div></div>`).join(''):'<p class="subtitle">本次已检查来源未返回读取或解析错误；未检查范围不在此结论内。</p>'}</section>`;
  }
  function render() {
    if(!state.snapshot) return;
    document.querySelectorAll('[data-view]').forEach(el=>{if(el.classList.contains('nav-item')){el.classList.toggle('active',el.dataset.view===state.view); if(el.dataset.view===state.view) el.setAttribute('aria-current','page'); else el.removeAttribute('aria-current');}});
    document.querySelector('#crumb').textContent=titles[state.view];
    document.querySelector('#service-count').textContent=state.snapshot.stats.definitions;
    const badge=document.querySelector('#mode-badge');
    badge.className=`tag ${state.snapshot.mode==='demo'?'amber':'mint'}`;
    badge.textContent=state.snapshot.mode==='demo'?(offline?'演示数据 · 离线预览':'演示数据 · 样例配置'):'本机配置 · 只读';
    document.querySelector('#footer-status').textContent=`最近扫描 ${state.snapshot.scannedAt.slice(11,19)} · ${state.snapshot.durationMs} ms · 无运行时测量`;
    main.innerHTML=({overview,services,clients,review,cli,about})[state.view]();
    main.classList.remove('view-enter'); requestAnimationFrame(()=>main.classList.add('view-enter'));
    document.body.dataset.ready='true';
  }
  function navigate(view) {if(!titles[view])return; state.view=view;render();window.scrollTo({top:0});}
  function showDialog(html) {const dialog=document.querySelector('#detail');document.querySelector('#detail-body').innerHTML=html;dialog.showModal();}
  const closeButton=()=>`<button class="icon-button" data-action="close-dialog" aria-label="关闭详情">${icon('close')}</button>`;
  function detail(id) {
    const e=state.snapshot.entries.find(v=>v.id===id);if(!e)return;
    const line=(key,value)=>`<div class="detail-line"><dt>${esc(key)}</dt><dd>${esc(value)}</dd></div>`;
    showDialog(`<div class="detail-header"><div><p class="eyebrow">SERVICE DEFINITION</p><h2 id="detail-title">${esc(e.name)}</h2><small>${esc(clientName(e.client))} · ${esc(scopeName(e.scope))}</small></div>${closeButton()}</div>${tag('发现配置 · 未托管','neutral')}<div class="detail-section"><h3>来源与身份</h3><dl>${line('配置文件',e.source)}${line('工作区',e.workspace)}${line('连接方式',e.transport==='remote'?'远程声明（未连接）':e.transport==='stdio'?'本地 stdio（未启动）':'未确定')}${line('地址 / 程序',e.endpoint)}${line('配置状态',e.configuredState==='disabled'?'显式禁用声明':'存在声明；未确认实际启用')}${line('同配置候选',`${e.candidateCount} 条；不是共享资格`)}${line('默认策略','独占 · 尚未执行')}</dl></div><div class="detail-section"><h3>敏感值不进入界面</h3><dl>${line('参数',`${e.argumentCount} 个，所有值隐藏`)}${line('环境变量',`${e.environmentCount} 个，所有值隐藏`)}${line('请求头',`${e.headerCount} 个，所有值隐藏`)}${line('扩展字段',`${e.extraFieldCount} 个，保留原文件，不自动转换`)}</dl></div><div class="detail-section"><h3>审查线索</h3>${e.issues.length?e.issues.map(i=>`<div class="finding">${tag(i.severity==='high'?'优先':'核对',i.severity==='high'?'red':'amber')}<div><h3>${esc(i.title)}</h3><p>${esc(i.code)}</p></div></div>`).join(''):'<p class="detail-note">未匹配预设线索，不代表服务已通过安全验证。</p>'}</div><div class="detail-section"><p class="detail-note">当前不执行服务、不修改客户端配置，也不展示推测的内存节省。原始凭据仍保留在源配置中。</p></div>`);
  }
  function clientDetail(id) {
    const c=state.snapshot.clients.find(v=>v.id===id);if(!c)return;
    showDialog(`<div class="detail-header"><div><p class="eyebrow">CLIENT ADAPTER</p><h2 id="detail-title">${esc(c.name)}</h2><small>${esc(c.format)} · 只读配置发现</small></div>${closeButton()}</div><p class="detail-note">文件存在不代表客户端正在使用。当前不合并覆盖顺序，不读取密钥库，不检测进程。</p><div class="detail-section"><h3>检查过的来源</h3>${c.sources.map(s=>`<div class="source-record">${tag(s.status==='found'?'发现配置':s.status==='missing'?'未发现':'读取受限',s.status==='found'?'mint':s.status==='missing'?'outline':'amber')}<p class="source-file">${esc(s.path)}</p><small>${esc(scopeName(s.scope))} · ${s.entries} 条绑定${s.note?' · '+esc(s.note):''}</small></div>`).join('')||'<p class="subtitle">此平台未登记默认位置，请使用自定义配置路径。</p>'}</div><div class="detail-section"><button class="button block" data-client-filter="${c.id}">查看此客户端的服务 ${icon('arrow')}</button></div>`);
  }
  function saveJSON(data,name) {const blob=new Blob([JSON.stringify(data,null,2)],{type:'application/json;charset=utf-8'});const url=URL.createObjectURL(blob);const a=document.createElement('a');a.href=url;a.download=name;a.click();setTimeout(()=>URL.revokeObjectURL(url),1500);}
  function offlinePlan(){return {schemaVersion:'mcpcell.review-plan/v0alpha1',mode:'proposal_only',applicable:false,demo:true,reason:'离线演示审查提案；不是客户端配置，不能执行。',items:state.snapshot.entries.map(e=>({definitionId:e.id,name:e.name,client:e.client,source:e.source,scope:e.scope,operation:'review-before-enrollment',proposedSharing:'exclusive',credentialAction:'keep-in-source',blockers:['runtime-not-implemented','client-semantics-unverified','operator-approval-required'],checks:e.issues.map(i=>i.code)}))};}
  document.addEventListener('click',async event=>{
    const el=event.target.closest('button,a');if(!el)return;
    if(el.dataset.view){navigate(el.dataset.view);return;}
    if(el.dataset.detail){detail(el.dataset.detail);return;}
    if(el.dataset.clientDetail){clientDetail(el.dataset.clientDetail);return;}
    if(el.dataset.clientFilter){document.querySelector('#detail').close();state.client=el.dataset.clientFilter;navigate('services');return;}
    if(el.dataset.filter){state.filter=el.dataset.filter;render();return;}
    const action=el.dataset.action;
    if(action==='close-dialog'){document.querySelector('#detail').close();return;}
    if(action==='review'){navigate('review');return;}
    if(action==='rescan'){
      if(offline){toast('离线预览不会访问本机。运行 python -m mcpcell 后可重新扫描。');return;}
      el.disabled=true;
      try{state.snapshot=await api('/api/rescan','POST');state.selected.clear();render();toast('扫描完成。没有修改配置或启动工具。');}catch(error){toast(error.message);}finally{el.disabled=false;}
    }
    if(action==='export-inventory'){saveJSON(state.snapshot,'mcpcell-inventory.json');toast('已导出隐藏敏感值的清单；分享前仍请检查服务名与文件路径。');}
    if(action==='export-plan'){
      el.disabled=true;
      try{const proposal=offline?offlinePlan():await api('/api/plan');if(state.selected.size)proposal.items=proposal.items.filter(i=>state.selected.has(i.definitionId));saveJSON(proposal,'mcpcell-review-plan.json');toast('已导出审查提案。此文件不可直接应用，原配置未改变。');}catch(error){toast(error.message);}finally{el.disabled=false;}
    }
  });
  document.addEventListener('input',event=>{if(event.target.id==='service-search'){state.query=event.target.value;document.querySelector('#table-region').innerHTML=table(filtered());}});
  document.addEventListener('change',event=>{
    const el=event.target;
    if(el.id==='client-filter'){state.client=el.value;document.querySelector('#table-region').innerHTML=table(filtered());}
    if(el.dataset.select){if(el.checked)state.selected.add(el.dataset.select);else state.selected.delete(el.dataset.select);}
    if(el.id==='select-all'){filtered().forEach(e=>{if(el.checked)state.selected.add(e.id);else state.selected.delete(e.id);});document.querySelector('#table-region').innerHTML=table(filtered());document.querySelector('#select-all').checked=el.checked;}
    const note=document.querySelector('#selection-note');if(note)note.textContent=`已选 ${state.selected.size} 条 · 导出的是审查提案，不是可直接应用的客户端配置。`;
  });
  function focusSearch(){navigate('services');document.querySelector('#service-search')?.focus();}
  document.querySelector('#search-shortcut').addEventListener('click',focusSearch);
  document.addEventListener('keydown',event=>{if((event.metaKey||event.ctrlKey)&&event.key.toLowerCase()==='k'){event.preventDefault();if(document.querySelector('#detail').open)document.querySelector('#detail').close();focusSearch();}});
  document.querySelector('#detail').addEventListener('click',event=>{if(event.target===event.currentTarget){const r=event.currentTarget.getBoundingClientRect();if(event.clientX<r.left||event.clientX>r.right||event.clientY<r.top||event.clientY>r.bottom)event.currentTarget.close();}});
  document.querySelector('.brand').addEventListener('click',e=>{e.preventDefault();navigate('overview');});
  (async()=>{
    try{
      if(offline)state.snapshot=window.__MCPCELL_PREVIEW__;
      else{const params=new URLSearchParams(location.hash.slice(1));let token=params.get('token')||'';if(token){history.replaceState(null,'',location.pathname);await api('/api/session','POST',token);token='';}state.snapshot=await api('/api/snapshot');}
      render();
    }catch(error){document.querySelector('#mode-badge').textContent='连接未完成';main.innerHTML=`<div class="loading-state"><h1>还没有连接到工作台</h1><p>${esc(error.message)}</p><p>在项目目录运行 <code>python -m mcpcell --open</code>。<br>仅浏览样式可运行 <code>python -m mcpcell --demo --open</code>。</p></div>`;}
  })();
})();
