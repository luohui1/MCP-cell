"""Local agent catalog, explicit enrollment, and bounded interactive sessions.

Discovery never runs executables, loads agent plugins, or reads auth files.
'claude' is detected for Claude Code: the Unix 'cc' C compiler is NOT an agent.
"""
from __future__ import annotations

import copy
import hashlib
import json
import os
from pathlib import Path
import shutil
import sys
import threading
import time
import uuid

from .agent_terminal import TerminalSession, OutputBuffer
from .storage import Conflict, Vault
from .runtime import BASE_ENV

CATALOG = [
    {'id':'claude-code', 'name':'Claude Code', 'short':'CC', 'command':'claude', 'client':'claude-code',
     'description':'Anthropic · 原生交互与审批', 'packages':['@anthropic-ai/claude-code'],
     'env':['ANTHROPIC_API_KEY','ANTHROPIC_AUTH_TOKEN','ANTHROPIC_BASE_URL','CLAUDE_CONFIG_DIR'], 'docs':'https://code.claude.com/docs/en/cli-reference'},
    {'id':'pi','name':'Pi','short':'π','command':'pi','client':None,
     'description':'可扩展的轻量 Agent · 无内置权限审批', 'packages':['@earendil-works/pi-coding-agent','@mariozechner/pi-coding-agent'],
     'env':['PI_CODING_AGENT_DIR','ANTHROPIC_API_KEY','OPENAI_API_KEY','GEMINI_API_KEY','GOOGLE_API_KEY','OPENROUTER_API_KEY'], 'docs':'https://github.com/earendil-works/pi'},
    {'id':'codex','name':'Codex','short':'CX','command':'codex','client':'codex',
     'description':'OpenAI · 原生终端与项目工作流', 'packages':['@openai/codex'],
     'env':['CODEX_HOME','OPENAI_API_KEY','OPENAI_BASE_URL'], 'docs':'https://developers.openai.com/codex/cli'},
    {'id':'gemini','name':'Gemini CLI','short':'GE','command':'gemini','client':'gemini',
     'description':'Google · 交互式编程助手', 'packages':['@google/gemini-cli'],
     'env':['GEMINI_API_KEY','GOOGLE_API_KEY','GOOGLE_CLOUD_PROJECT','GOOGLE_CLOUD_LOCATION','GOOGLE_APPLICATION_CREDENTIALS'], 'docs':'https://geminicli.com/docs/'},
    {'id':'opencode','name':'OpenCode','short':'OC','command':'opencode','client':'opencode',
     'description':'多模型 · 保留本机配置', 'packages':['opencode-ai'],
     'env':['OPENCODE_CONFIG','OPENAI_API_KEY','ANTHROPIC_API_KEY','OPENROUTER_API_KEY'], 'docs':'https://opencode.ai/docs/'},
    {'id':'droid','name':'Factory Droid','short':'FD','command':'droid','client':None,
     'description':'Factory · 原生终端', 'packages':[], 'env':['FACTORY_API_KEY'], 'docs':'https://docs.factory.ai/'},
    {'id':'copilot','name':'Copilot CLI','short':'GH','command':'copilot','client':'vscode',
     'description':'GitHub · 原生终端', 'packages':['@github/copilot'],
     'env':['GH_TOKEN','GITHUB_TOKEN','COPILOT_HOME'], 'docs':'https://docs.github.com/en/copilot/concepts/agents/about-copilot-cli'},
    {'id':'aider','name':'Aider','short':'AI','command':'aider','client':None,
     'description':'终端结对编程', 'packages':[],
     'env':['OPENAI_API_KEY','ANTHROPIC_API_KEY','OPENROUTER_API_KEY'], 'docs':'https://aider.chat/docs/'},
    {'id':'goose','name':'Goose','short':'GO','command':'goose','client':None,
     'description':'可扩展 Agent · 原生终端', 'packages':[], 'args':['session'],
     'env':['GOOSE_PROVIDER','GOOSE_MODEL','OPENAI_API_KEY','ANTHROPIC_API_KEY'], 'docs':'https://block.github.io/goose/docs/'},
    {'id':'ccs','name':'CCS','short':'CS','command':'ccs','client':None,
     'description':'本机已有的 Agent / 模型配置入口', 'packages':['@kaitranntt/ccs'],
     'env':['ANTHROPIC_API_KEY','ANTHROPIC_AUTH_TOKEN','ANTHROPIC_BASE_URL'], 'docs':'https://github.com/kaitranntt/ccs'},
]
INDEX = {a['id']: a for a in CATALOG}
FORBIDDEN = {'sh','bash','zsh','fish','cmd','powershell','pwsh'}
MAX_AGENTS = 48
MAX_SESSIONS = 24
MAX_RUNNING = 8


def executable_signature(argv):
    """Change detector, not a supply-chain authenticity certificate."""
    material=[]
    for part in argv[:2]:
        p=Path(part)
        if p.is_absolute() and p.is_file():
            s=p.stat()
            material.append([str(p.resolve()),s.st_dev,s.st_ino,s.st_size,s.st_mtime_ns])
    return hashlib.sha256(json.dumps(material).encode()).hexdigest()


def resolve_executable(path: Path, catalog: dict, env: dict) -> list[str]:
    if not path.is_absolute() or not path.is_file():
        raise Conflict('启动程序不存在，请重新检测或指定绝对路径')
    if path.stem.lower() in FORBIDDEN:
        raise Conflict('不能把命令解释器直接登记为 Agent')
    if os.name != 'nt':
        if not os.access(path, os.X_OK):
            raise Conflict('启动文件没有执行权限')
        return [str(path)]
    if path.suffix.lower() not in {'.cmd','.bat','.ps1'}:
        return [str(path)]
    # Standard npm shim -> declared package bin -> node.exe, never cmd /c.
    node = shutil.which('node',path=env.get('PATH',os.defpath))
    if node:
        for package in catalog.get('packages',[]):
            root=path.parent/'node_modules'/package
            manifest=root/'package.json'
            try:
                if manifest.stat().st_size > 256*1024:
                    continue
                value=json.loads(manifest.read_text(encoding='utf-8'))
                target=value.get('bin')
                if isinstance(target,dict):
                    target=target.get(catalog['command'])
                if not isinstance(target,str):
                    continue
                script=(root/target).resolve()
                script.relative_to(root.resolve())
                if script.is_file():
                    return [str(Path(node).absolute()),str(script)]
            except (OSError,ValueError,TypeError):
                continue
    raise Conflict('此脚本入口尚未解析为原生程序；请指定可执行文件或受支持的 npm 安装')


class DemoSession:
    def __init__(self,sid,agent_id,title,cwd):
        self.id,self.agent_id,self.title,self.cwd=sid,agent_id,title,cwd
        self.closed=False;self.state='running';self.output=OutputBuffer();self.created=time.time()
        self.line='';self.cols=100;self.rows=28
        self.output.append((f'\x1b[1;32m{title}\x1b[0m  · MCPcell 演示终端\r\n\r\n'
            '这是交互样例，不运行 Agent、不发送模型请求、不执行输入。\r\n'
            f'工作目录  {cwd}\r\n\r\n> ').encode())
    def write(self,text):
        if self.closed:raise Conflict('会话已结束')
        if not isinstance(text,str) or len(text.encode())>8192:raise Conflict('输入过长')
        for c in text:
            if c=='\r':
                self.output.append(('\r\n[演示] 已收到输入；未执行任何操作。\r\n\r\n> ').encode());self.line=''
            elif c=='\x03':self.output.append(b'^C\r\n> ');self.line=''
            elif c=='\x7f':
                if self.line:self.line=self.line[:-1];self.output.append(b'\b \b')
            elif c.isprintable():self.line+=c;self.output.append(c.encode())
    def resize(self,cols,rows):TerminalSession._validate_size(cols,rows);self.cols,self.rows=cols,rows
    def stop(self):self.closed=True;self.state='exited'
    def snapshot(self):
        return {'id':self.id,'agentId':self.agent_id,'title':self.title,'cwd':self.cwd,'pid':None,
                'state':self.state,'exitCode':0 if self.closed else None,'createdAt':self.created,
                'rssBytes':None,'error':None,'cols':self.cols,'rows':self.rows,'cleanup':'demo', 'backend':'demo'}


class AgentHub:
    def __init__(self, home: Path, vault: Vault, *, demo=False, environ=None):
        self.home,self.vault,self.demo=home,vault,demo
        self.environ=dict(os.environ if environ is None else environ)
        self.lock=threading.RLock();self.sessions={};self.launch_requests={};self.closed=False
        self.records=vault.get('agents.bin',{'entries':{}})['entries']
        self.detected={};self.last_detect=0
        self.detect()
        if demo and not self.records:
            for key in ['claude-code','pi','codex']:
                self._enroll(key)

    def _save(self):self.vault.put('agents.bin',{'entries':self.records})

    def detect(self):
        with self.lock:
            found={}
            for spec in CATALOG:
                key,cmd=spec['id'],spec['command']
                if self.demo:
                    path=str(self.home/'.local/bin'/cmd) if key in {'claude-code','pi','codex','gemini','opencode','ccs'} else None
                    found[key]={'executable':path,'argv':[path] if path else [],'signature':'demo-'+key,
                                'reason':'' if path else '未找到启动程序','origin':'演示样例'}
                    continue
                # Empty/relative PATH entries could resolve executables in an untrusted CWD.
                search_path=os.pathsep.join(p for p in self.environ.get('PATH',os.defpath).split(os.pathsep) if p and Path(p).is_absolute())
                located=shutil.which(cmd,path=search_path)
                origin='PATH'
                if not located:
                    roots=[self.home/'.local/bin',self.home/'.npm-global/bin',self.home/'.bun/bin']
                    if os.name=='nt':roots+=[Path(self.environ.get('APPDATA',str(self.home/'AppData/Roaming')))/'npm']
                    names=[cmd+'.exe',cmd+'.cmd',cmd] if os.name=='nt' else [cmd]
                    located=next((str(root/name) for root in roots for name in names if (root/name).is_file()),None)
                    origin='已知安装目录'
                argv=[];reason='未找到启动程序';signature=None
                if located:
                    try:
                        argv=resolve_executable(Path(located).absolute(),spec,self.environ)
                        signature=executable_signature(argv)
                        reason=''
                    except Conflict as exc:reason=str(exc)
                found[key]={'executable':located,'argv':argv,'signature':signature,'reason':reason,'origin':origin}
            self.detected=found;self.last_detect=time.time()
            return self.snapshot()

    def _enroll(self,key):
        if key not in INDEX or not self.detected[key]['argv']:
            raise Conflict('未找到可启动的 Agent；可手动登记绝对路径')
        if key in self.records:return
        if len(self.records)>=MAX_AGENTS:raise Conflict('最多收纳 48 个 Agent')
        spec,det=INDEX[key],self.detected[key]
        self.records[key]={'id':key,'catalogId':key,'name':spec['name'],'short':spec['short'],
                           'argv':list(det['argv'])+spec.get('args',[]),'signature':det['signature'],
                           'enabled':True,'pinned':False,'lastCwd':'','createdAt':time.time()}
        self._save()

    def enroll(self,ids):
        if not isinstance(ids,list) or not 1<=len(ids)<=16 or any(not isinstance(x,str) for x in ids):
            raise Conflict('请选择 1–16 个 Agent')
        with self.lock:
            for key in ids:
                if key not in INDEX or not self.detected[key]['argv']:raise Conflict('所选程序未找到或入口不受支持')
            for key in ids:self._enroll(key)
        return self.snapshot()

    def register(self,name,executable,args):
        if not isinstance(name,str) or not name.strip() or len(name)>60 or not name.isprintable():raise Conflict('名称需为 1–60 个可见字符')
        if not isinstance(executable,str) or len(executable)>1024:raise Conflict('程序路径无效')
        if not isinstance(args,list) or len(args)>24 or any(not isinstance(x,str) or len(x)>1024 or '\x00' in x for x in args):raise Conflict('参数需为最多 24 个字符串的 JSON 数组')
        if self.demo:raise Conflict('演示模式不登记本机程序')
        with self.lock:
            if len(self.records)>=MAX_AGENTS:raise Conflict('最多收纳 48 个 Agent')
            argv=resolve_executable(Path(executable),{},self.environ)
            key='custom-'+uuid.uuid4().hex
            self.records[key]={'id':key,'catalogId':None,'name':name.strip(),'short':name.strip()[:2],
                              'argv':argv+args,'signature':executable_signature(argv+args),'enabled':True,
                              'pinned':False,'lastCwd':'','createdAt':time.time()}
            self._save()
        return self.snapshot()

    def update(self,key,enabled,pinned):
        if type(enabled) is not bool or type(pinned) is not bool:raise Conflict('状态字段须为布尔值')
        with self.lock:
            record=self.records.get(key)
            if not record:raise Conflict('Agent 尚未收纳')
            record.update(enabled=enabled,pinned=pinned);self._save()
        return self.snapshot()

    def remove(self,key):
        with self.lock:
            if any(s.agent_id==key and not s.closed for s in self.sessions.values()):raise Conflict('请先结束此 Agent 的运行会话')
            if key not in self.records:raise Conflict('Agent 尚未收纳')
            del self.records[key];self._save()
        return self.snapshot()

    def _environment(self,key):
        names=set(BASE_ENV)|{'TERM','COLORTERM','SHELL','LOGNAME','LC_CTYPE','XDG_DATA_HOME','XDG_STATE_HOME'}
        spec=INDEX.get(self.records[key].get('catalogId'),{})
        names.update(spec.get('env',[]))
        # Explicitly excludes the manager's other provider credentials and management tokens.
        env={k:v for k,v in self.environ.items() if k in names}
        env.update(TERM='xterm-256color',COLORTERM='truecolor')
        return env

    def launch(self,key,cwd,request_id):
        if not isinstance(cwd,str) or not Path(cwd).is_absolute() or len(cwd)>1024:raise Conflict('请选择绝对路径的工作目录')
        if not self.demo and not Path(cwd).is_dir():raise Conflict('工作目录不存在')
        if not isinstance(request_id,str) or len(request_id)!=32 or any(c not in '0123456789abcdef' for c in request_id):raise Conflict('启动请求标识无效')
        with self.lock:
            if self.closed:raise Conflict('管理器正在退出')
            if request_id in self.launch_requests:
                sid=self.launch_requests[request_id]
                if sid not in self.sessions:raise Conflict('此启动请求已结束，不会重复启动')
                old=self.sessions[sid]
                if old.agent_id!=key or old.cwd!=cwd:raise Conflict('启动请求标识不能用于不同任务')
                return old.snapshot()
            record=self.records.get(key)
            if not record or not record['enabled']:raise Conflict('请先收纳并启用此 Agent')
            if len(self.sessions)>=MAX_SESSIONS:raise Conflict('最多保留 24 个会话，请关闭已结束页签')
            if sum(not s.closed for s in self.sessions.values())>=MAX_RUNNING:raise Conflict('最多同时运行 8 个 Agent 会话')
            if len(self.launch_requests)>=4096:raise Conflict('本次管理器的启动请求额度已满，请结束会话并重启管理器')
            if not self.demo and executable_signature(record['argv'])!=record['signature']:raise Conflict('启动程序或入口脚本已变化，请移除后重新检测收纳')
            sid=uuid.uuid4().hex
            title=record['name']+' · '+(Path(cwd).name or cwd)
            if self.demo:s=DemoSession(sid,key,title,cwd)
            else:
                try:s=TerminalSession(sid,key,title,record['argv'],cwd,self._environment(key))
                except Conflict:raise
                except Exception:raise Conflict('Agent 启动失败，请检查程序入口和终端组件；未重试') from None
            self.sessions[sid]=s;self.launch_requests[request_id]=sid
            record['lastCwd']=cwd
            try:self._save()
            except Exception:
                s.stop();del self.sessions[sid]
                raise Conflict('保存启动记录失败，会话已停止') from None
            return s.snapshot()

    def get_session(self,sid):
        if not isinstance(sid,str):raise Conflict('会话标识无效')
        with self.lock:
            s=self.sessions.get(sid)
            if not s:raise Conflict('会话不存在或管理器已经重启')
            return s

    def close_session(self,sid):
        s=self.get_session(sid);s.stop()
        with self.lock:self.sessions.pop(sid,None)

    def snapshot(self):
        with self.lock:
            entries=[]
            for spec in CATALOG:
                key=spec['id'];det=self.detected.get(key,{})
                record=self.records.get(key)
                available=bool(det.get('argv'))
                entries.append({'id':key,'name':record['name'] if record else spec['name'],'short':spec['short'],
                    'description':spec['description'],'command':spec['command'],'executable':det.get('executable'),
                    'found':bool(det.get('executable')),'launchable':available,'enrolled':record is not None,
                    'enabled':record['enabled'] if record else False,'pinned':record.get('pinned',False) if record else False,
                    'lastCwd':record.get('lastCwd','') if record else '', 'client':spec['client'],
                    'reason':det.get('reason',''),'detectionSource':det.get('origin',''),'docs':spec['docs'],
                    'kind':'builtin','version':None})
            for key,record in self.records.items():
                if key in INDEX:continue
                exists=Path(record['argv'][0]).is_file()
                entries.append({'id':key,'name':record['name'],'short':record['short'],'description':'手动登记的本机 Agent',
                    'command':Path(record['argv'][0]).name,'executable':record['argv'][0],'found':exists,
                    'launchable':exists,'enrolled':True,'enabled':record['enabled'],'pinned':record.get('pinned',False),
                    'lastCwd':record.get('lastCwd',''),'client':None,'reason':'' if exists else '程序不再存在',
                    'kind':'custom','version':None})
            sessions=[s.snapshot() for s in self.sessions.values()]
            return {'agents':entries,'sessions':sessions,'detectedAt':self.last_detect,
                    'mode':'demo' if self.demo else 'local','defaultCwd':str(self.home),
                    'limits':{'maxRunning':MAX_RUNNING,'maxSessions':MAX_SESSIONS,'outputBytesPerSession':1024*1024,
                              'sandbox':False,'historyPersistence':False},
                    'terminalBackend':'conpty-preview' if os.name=='nt' else 'posix-pty'}

    def close(self):
        with self.lock:self.closed=True;sessions=list(self.sessions.values())
        for s in sessions:s.stop()
