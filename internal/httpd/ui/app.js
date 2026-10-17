'use strict';

const state = {
  data: null,
  open: new Set(),        // expanded service names
  tab: new Map(),         // service -> client tab
  busy: new Set(),        // services with an action in flight
  rendered: new Map(),    // service -> last rendered html, to avoid needless DOM churn
};

const CLIENTS = ['Claude Code', 'Codex', 'Cursor'];
const $ = (id) => document.getElementById(id);

function esc(v) {
  return String(v ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function fmtBytes(n) {
  if (!n) return '—';
  const mb = n / 1048576;
  if (mb >= 1024) return (mb / 1024).toFixed(1) + ' GB';
  return (mb < 10 ? mb.toFixed(1) : Math.round(mb)) + ' MB';
}

function span(ms) {
  const s = Math.max(0, Math.round(ms / 1000));
  if (s < 60) return s + ' 秒';
  const m = Math.round(s / 60);
  if (m < 60) return m + ' 分钟';
  const h = Math.round(m / 60);
  if (h < 48) return h + ' 小时';
  return Math.round(h / 24) + ' 天';
}
const since = (t) => span(Date.now() - new Date(t).getTime());
const until = (t) => span(new Date(t).getTime() - Date.now());

function clock(t) {
  const d = new Date(t);
  return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false });
}

function stateOf(s) {
  switch (s.state) {
    case 'ready': return { dot: 'green', text: '运行中' };
    case 'starting': return { dot: 'amber', text: '启动中' };
    case 'failed': return { dot: 'red', text: '启动失败' };
    default: return { dot: 'gray', text: '未启动' };
  }
}

function activity(s) {
  const inst = s.instance;
  if (inst) return inst.inflight ? `处理中 ${inst.inflight} 个请求` : `空闲 ${since(inst.lastActiveAt)}`;
  if (s.state === 'failed' && s.retryAt) return `${until(s.retryAt)}后可重试`;
  if (s.state === 'cold') return '首次连接时启动';
  return '—';
}

function snippet(client, name, url, token) {
  const shown = token.slice(0, 6) + '…';
  const make = (tok) => {
    switch (client) {
      case 'Codex':
        return `[mcp_servers.${name}]\nurl = "${url}"\nhttp_headers = { Authorization = "Bearer ${tok}" }`;
      case 'Cursor':
        return `"${name}": {\n  "url": "${url}",\n  "headers": { "Authorization": "Bearer ${tok}" }\n}`;
      default:
        return `"${name}": {\n  "type": "http",\n  "url": "${url}",\n  "headers": { "Authorization": "Bearer ${tok}" }\n}`;
    }
  };
  const where = {
    'Claude Code': '放进 ~/.claude.json 或项目 .mcp.json 的 mcpServers，替换原来的同名条目；改完重启会话。',
    'Codex': '放进 ~/.codex/config.toml，替换原来的同名表；改完重启 Codex。',
    'Cursor': '放进 ~/.cursor/mcp.json 的 mcpServers，替换原来的同名条目。',
  }[client];
  return { shown: make(shown), full: make(token), where };
}

function rowHTML(s, d) {
  const st = stateOf(s);
  const inst = s.instance;
  const sessions = d.sessions.filter((x) => x.service === s.name);
  const open = state.open.has(s.name);
  const busy = state.busy.has(s.name);
  let action = '';
  if (s.state === 'ready') action = `<button class="btn" data-act="stop" data-name="${esc(s.name)}" ${busy ? 'disabled' : ''}>回收</button>`;
  else if (s.state !== 'starting') action = `<button class="btn" data-act="start" data-name="${esc(s.name)}" ${busy ? 'disabled' : ''}>启动</button>`;

  let html = `<div class="row${open ? ' open' : ''}">
  <div class="row-main">
    <div class="name">
      <button data-act="toggle" data-name="${esc(s.name)}" aria-expanded="${open}"><span class="chev">▶</span>${esc(s.name)}</button>
      <div class="cmd mono" title="${esc(s.command)}">${esc(s.command)}</div>
    </div>
    <div class="state"><span class="dot dot-${st.dot}"></span>${st.text}</div>
    <div class="num">${sessions.length}</div>
    <div class="num">${inst ? fmtBytes(inst.usage.privateBytes) : '—'}</div>
    <div class="activity">${esc(activity(s))}</div>
    <div class="actions">${action}<button class="btn btn-primary" data-act="toggle" data-name="${esc(s.name)}">${open ? '收起' : '接入'}</button></div>
  </div>`;
  if (open) html += detailHTML(s, d, sessions);
  return html + '</div>';
}

function detailHTML(s, d, sessions) {
  const url = `${d.baseUrl}/s/${s.name}`;
  const tab = state.tab.get(s.name) || CLIENTS[0];
  const sn = snippet(tab, s.name, url, d.token);
  const inst = s.instance;
  let html = `<div class="detail">
  <div>
    <h3>接入地址</h3>
    <div class="url"><code>${esc(url)}</code><button class="btn" data-act="copy" data-text="${esc(url)}">复制</button></div>
  </div>
  <div>
    <h3>客户端配置</h3>
    <div class="tabs" role="tablist">${CLIENTS.map((c) =>
      `<button role="tab" aria-selected="${c === tab}" data-act="tab" data-name="${esc(s.name)}" data-tab="${esc(c)}">${c}</button>`).join('')}</div>
    <div class="snippet"><pre>${esc(sn.shown)}</pre><button class="btn" data-act="copy" data-text="${esc(sn.full)}">复制</button></div>
    <div class="where">${esc(sn.where)} 复制的内容包含完整 token。</div>
  </div>`;

  if (inst) {
    const u = inst.usage;
    html += `<div>
    <h3>进程</h3>
    <div class="kv">
      <span><b>PID</b>${inst.pid}</span>
      <span><b>进程数</b>${u.processes}</span>
      <span><b>私有提交</b>${fmtBytes(u.privateBytes)}</span>
      <span><b>工作集</b>${fmtBytes(u.workingSetBytes)}</span>
      <span><b>协议</b>${esc(inst.protocolVersion)}</span>
      ${inst.serverName ? `<span><b>上游</b>${esc(inst.serverName)} ${esc(inst.serverVersion || '')}</span>` : ''}
      <span><b>已运行</b>${since(inst.startedAt)}</span>
      <span><b>调用</b>${inst.calls} 次${inst.failures ? `，失败 ${inst.failures}` : ''}</span>
    </div>
  </div>`;
  }
  html += `<div><h3>设置</h3><div class="kv">
      <span><b>模式</b>${s.mode === 'shared' ? '共享（所有会话共用一个进程）' : esc(s.mode)}</span>
      <span><b>空闲回收</b>${s.idleTimeoutSec ? span(s.idleTimeoutSec * 1000) + '后' : '不回收'}</span>
      <span><b>累计启动</b>${s.starts} 次</span>
      ${s.envNames && s.envNames.length ? `<span><b>环境变量</b>${esc(s.envNames.join(', '))}</span>` : ''}
    </div></div>`;

  if (sessions.length) {
    html += `<div><h3>会话</h3><table class="sessions"><thead><tr><th>客户端</th><th>协议</th><th>连接于</th><th>最近活动</th><th>进行中</th></tr></thead><tbody>${
      sessions.map((x) => `<tr><td>${esc(x.client || '未知客户端')}</td><td class="mono">${esc(x.protocolVersion)}</td><td>${clock(x.createdAt)}</td><td>${since(x.lastSeenAt)}前</td><td class="num">${x.inflight}</td></tr>`).join('')
    }</tbody></table></div>`;
  }
  if (s.lastError) {
    const when = s.lastErrorAt ? `（${since(s.lastErrorAt)}前）` : '';
    html += `<div class="error"><h3>最近错误${when}</h3><pre>${esc(s.lastError)}${s.stderrTail ? '\n\n' + esc(s.stderrTail) : ''}</pre></div>`;
  }
  return html + '</div>';
}

function render() {
  const d = state.data;
  if (!d) return;
  const running = d.services.filter((s) => s.instance);
  const mcpBytes = running.reduce((a, s) => a + (s.instance.usage.privateBytes || 0), 0);
  const procs = running.reduce((a, s) => a + (s.instance.usage.processes || 0), 0);

  $('daemon').innerHTML = `<span class="dot dot-green"></span>运行中 · 127.0.0.1:${d.port} · 已运行 ${since(d.startedAt)} · v${esc(d.version)}`;
  $('stats').innerHTML = [
    ['服务', d.services.length, `运行中 ${running.length}`],
    ['会话', d.sessions.length, '来自所有客户端'],
    ['MCP 内存', fmtBytes(mcpBytes), `${procs} 个进程`],
    ['守护进程', fmtBytes(d.daemon.privateBytes), `PID ${d.pid}`],
  ].map(([k, v, sub]) => `<div class="stat"><div class="stat-label">${k}</div><div class="stat-value">${v}</div><div class="stat-sub">${sub}</div></div>`).join('');
  $('config-path').textContent = d.configPath ? `配置：${d.configPath}` : '';

  const box = $('services');
  if (!d.services.length) {
    state.rendered.clear();
    box.innerHTML = `<div class="empty"><p>还没有服务。在 <code>${esc(d.configPath)}</code> 的 services 里添加，写法与客户端的 mcpServers 条目相同，然后重启守护进程：</p><pre>${esc(
      '{\n  "services": {\n    "context7": { "command": "npx", "args": ["-y", "@upstash/context7-mcp"] }\n  }\n}')}</pre></div>`;
    return;
  }
  // Re-render only rows whose content changed, so text selection survives polling.
  const names = d.services.map((s) => s.name);
  if (box.querySelector('.empty') || box.children.length !== names.length ||
      [...box.children].some((el, i) => el.dataset.name !== names[i])) {
    box.innerHTML = names.map((n) => `<div data-name="${esc(n)}"></div>`).join('');
    state.rendered.clear();
  }
  d.services.forEach((s, i) => {
    const html = rowHTML(s, d);
    if (state.rendered.get(s.name) !== html) {
      box.children[i].innerHTML = html;
      state.rendered.set(s.name, html);
    }
  });
}

async function refresh() {
  try {
    const r = await fetch('/api/status', { cache: 'no-store' });
    if (r.status === 401) { location.reload(); return; }
    state.data = await r.json();
    render();
  } catch {
    $('daemon').innerHTML = '<span class="dot dot-red"></span>无法连接守护进程，正在重试…';
  }
}

let toastTimer;
function toast(text) {
  let el = document.querySelector('.toast');
  if (!el) { el = document.createElement('div'); el.className = 'toast'; document.body.append(el); }
  el.textContent = text;
  el.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => el.classList.remove('show'), 1600);
}

async function act(kind, name) {
  state.busy.add(name);
  render();
  try {
    const r = await fetch(`/api/services/${encodeURIComponent(name)}/${kind}`, { method: 'POST', headers: { 'X-MCPcell': '1' } });
    const body = await r.json().catch(() => ({}));
    if (!r.ok) toast(body.error || '操作失败');
    else toast(kind === 'stop' ? `已回收 ${name}` : `${name} 已启动`);
  } finally {
    state.busy.delete(name);
    await refresh();
  }
}

document.addEventListener('click', async (e) => {
  const el = e.target.closest('[data-act]');
  if (!el) return;
  const name = el.dataset.name;
  switch (el.dataset.act) {
    case 'toggle':
      state.open.has(name) ? state.open.delete(name) : state.open.add(name);
      render();
      break;
    case 'tab':
      state.tab.set(name, el.dataset.tab);
      render();
      break;
    case 'copy':
      try { await navigator.clipboard.writeText(el.dataset.text); toast('已复制'); } catch { toast('复制失败，请手动选择'); }
      break;
    case 'start':
    case 'stop':
      act(el.dataset.act, name);
      break;
  }
});

// #name in the address opens that service, e.g. a link shared from the CLI.
const initial = decodeURIComponent(location.hash.slice(1));
if (initial) state.open.add(initial);

refresh();
setInterval(() => { if (!document.hidden) refresh(); }, 2000);
document.addEventListener('visibilitychange', () => { if (!document.hidden) refresh(); });
