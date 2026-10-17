"""Enrollment, crash reconciliation and per-entry reversible configuration patches."""
from __future__ import annotations
import base64
import copy
import hashlib
import json
import os
from pathlib import Path
import secrets
import stat
import sys
import threading
import time
import uuid

from .discovery import read_bounded, parse, endpoint_summary
from .clients import Source
from .patching import get_at, replace_entry, span
from .storage import Vault, Conflict, atomic

BRIDGE = Path(__file__).with_name('bridge.py').absolute()


def source_key(path, keys):
    return hashlib.sha256(json.dumps([str(path), keys], ensure_ascii=False).encode()).hexdigest()

class Manager:
    def __init__(self, scanner, root: Path):
        self.scanner = scanner
        self.vault = Vault(root)
        self.root = root
        self.lock = threading.RLock()
        self.data = self.vault.get('registry.bin', {'version': 2, 'bindings': {}})
        if self.data.get('version') != 2: raise Conflict('不支持的管理清单版本')
        self.bindings = self.data['bindings']
        if 'fingerprintKey' not in self.data:
            self.data['fingerprintKey']=secrets.token_hex(32)
            self.vault.put('registry.bin',self.data)
        scanner.key=bytes.fromhex(self.data['fingerprintKey'])
        self.runtime = None
        self.recover()
        for r in self.bindings.values():
            selector='claude_project' if r['keys'][0]=='projects' else r['keys'][0]
            workspace=r.get('sourceWorkspace','')
            if not any(str(s.path)==r['path'] and s.selector==selector and s.workspace==workspace for s in scanner.sources):
                scanner.sources.append(Source(r['public']['client'],Path(r['path']),r['public']['scope'],workspace,selector,r['format']))

    def save(self): self.vault.put('registry.bin', self.data)

    def recover(self):
        changed = False
        for bid, r in list(self.bindings.items()):
            if r['state'] not in {'prepared', 'restoring'}: continue
            try:
                cfg = get_at(parse(read_bounded(Path(r['path'])), r['format']), r['keys'])
                if cfg == r['wrapper']:
                    r['state'] = 'active'
                elif cfg == r['original']:
                    del self.bindings[bid]
                else: r['state'] = 'conflict'
            except (OSError, ValueError, KeyError, TypeError, RuntimeError): r['state'] = 'conflict'
            changed = True
        if changed: self.save()

    def reason(self, ref):
        cfg, src = ref['cfg'], ref['source']
        if not cfg.get('command'): return '远程 MCP 暂不代理，保留原客户端直连'
        if cfg.get('disabled') is True or cfg.get('enabled') is False: return '先在原客户端启用此配置'
        cmd = cfg['command']
        argv = cmd if isinstance(cmd, list) else [cmd] + cfg.get('args', [])
        if not argv or any(not isinstance(a,str) for a in argv): return '启动参数格式不支持'
        if Path(argv[0].replace('\\','/')).stem.lower() in {'sh','bash','zsh','fish','cmd','powershell','pwsh'}:
            return '命令解释器入口需手工核对，本版不自动接管'
        if any(any(v in a for v in ['${','{env:','{file:']) for a in argv): return '启动参数含动态变量，暂不自动接管'
        if cfg.get('envFile') or cfg.get('env_file'): return '环境文件尚未适配，暂不改写'
        if cfg.get('cwd') and (not isinstance(cfg['cwd'],str) or not Path(cfg['cwd']).is_absolute()):
            return '相对工作目录需要先核对'
        if src.format == 'toml':
            try: import tomlkit
            except ImportError: return '接入 Codex 需安装 tomlkit'
        if len(self.bindings) >= 128: return '已达到 128 条接管上限'
        return ''

    def scan(self):
        with self.lock:
            snap = self.scanner.scan()
            registered = {r['origin']: (bid,r) for bid,r in self.bindings.items()}
            seen = set()
            for e in snap['entries']:
                ref = self.scanner.refs[e['id']]
                sk = source_key(ref['source'].path, ref['keys'])
                if sk in registered:
                    bid, r = registered[sk]; seen.add(bid)
                    live={k:e[k] for k in ['id','client','sourceId','scope','source','workspace']}
                    e.update(r['public']); e.update(live)
                    conflict = ref['cfg'] != r['wrapper']
                    e.update(managed=True, bindingId=bid, enabled=r['enabled'],
                        managementState='conflict' if conflict or r['state']=='conflict' else r['state'],
                        reason='原配置已变化，请核对后再还原' if conflict else '', canEnroll=False)
                else:
                    reason = self.reason(ref)
                    e.update(managed=False, canEnroll=not reason, reason=reason, managementState='available')
            for bid,r in self.bindings.items():
                if bid not in seen:
                    snap['entries'].append({**r['public'], 'id':'managed-'+bid, 'bindingId':bid, 'managed':True,
                        'enabled':r['enabled'], 'managementState':'conflict', 'canEnroll':False,
                        'reason':'源配置未发现或不可读；未自动覆盖'})
            snap['capabilities'].update(configWrite=True, executeMcp=True, executeCli=False, resourceEnforcement=False)
            snap['version']='0.2.0a1'
            snap['mode']='demo' if self.scanner.demo else 'local'
            snap['stats']['managedBindings']=len(self.bindings)
            snap['stats']['definitions']=len(snap['entries'])
            snap['stats']['available']=sum(not e['managed'] and e['canEnroll'] for e in snap['entries'])
            if self.runtime: snap['runtime'] = self.runtime.status()
            else: snap['runtime']={'instances':[], 'running':0, 'rssBytes':None}
            # Deployment semantics, not a pretend MCP-security certificate.
            snap['limits']={'maxConnections':8, 'maxFrameBytes':8*1024*1024, 'sharedProcesses':False,
                            'sandbox':False, 'cleanup':'owned-process-group' if os.name!='nt' else 'job-object'}
            return snap

    def enroll(self, ids: list[str]):
        with self.lock:
            results=[]
            for entry_id in ids:
                try:
                    ref=self.scanner.refs.get(entry_id)
                    if not ref: raise Conflict('检测结果已过期，请重新检测')
                    reason=self.reason(ref)
                    if reason: raise Conflict(reason)
                    src,keys=ref['source'],ref['keys']
                    origin=source_key(src.path,keys)
                    if any(r['origin']==origin for r in self.bindings.values()):
                        raise Conflict('同一配置已接入，无需重复接入')
                    raw=read_bounded(src.path)
                    if raw != ref['raw']: raise Conflict('检测后源文件已变化，请重新检测')
                    original=copy.deepcopy(ref['cfg'])
                    bid=uuid.uuid4().hex
                    args=['-I',str(BRIDGE),'--state-dir',str(self.root),'--binding',bid]
                    wrapper=copy.deepcopy(original)
                    if isinstance(original['command'],list): wrapper['command']=[sys.executable]+args
                    else: wrapper.update(command=sys.executable,args=args)
                    patched=replace_entry(raw,src.format,keys,original,wrapper)
                    before_text=raw.decode('utf-8-sig')
                    fragment=None
                    if src.format != 'toml':
                        a,b=span(before_text,keys);fragment=before_text[a:b]
                    record={'origin':origin,'path':str(src.path),'format':src.format,'keys':keys,
                        'sourceWorkspace':src.workspace,'original':original,'wrapper':wrapper,'before':base64.b64encode(raw).decode(),
                        'afterHash':hashlib.sha256(patched).hexdigest(), 'fragment':fragment,
                        'token':secrets.token_urlsafe(32),'state':'prepared','enabled':True,
                        'createdAt':time.time(),'public':copy.deepcopy(ref['entry'])}
                    # Durable prepare precedes source mutation. Recovery examines both.
                    self.bindings[bid]=record;self.save()
                    env=original.get('env',original.get('environment',{}))
                    names=list(env) if isinstance(env,dict) else []
                    names+=original.get('env_vars',[]) if isinstance(original.get('env_vars'),list) else []
                    self.vault.put('ticket-'+bid+'.bin',{'token':record['token'],'envNames':names,'scanHome':str(self.scanner.home)})
                    try:
                        atomic(src.path,patched,expected=raw,mode=stat.S_IMODE(src.path.stat().st_mode))
                    except Exception:
                        # Prepared record remains recoverable; do not lose the backup.
                        self.recover();raise
                    record['state']='active';self.save()
                    results.append({'id':entry_id,'ok':True,'bindingId':bid})
                    # Several selected services can live in the same source file.
                    for other in self.scanner.refs.values():
                        if other['source'].path==src.path and other['raw']==raw: other['raw']=patched
                except (Conflict,OSError,ValueError,KeyError,TypeError) as exc:
                    results.append({'id':entry_id,'ok':False,'error':str(exc) if isinstance(exc,Conflict) else '接入失败，备份仍保留；请检查文件权限'})
            return {'results':results,'restartClients':True}

    def verify(self,bid):
        r=self.bindings.get(bid)
        if not r or r['state']!='active' or not r['enabled']: raise Conflict('此 MCP 未接入或已暂停')
        try: cfg=get_at(parse(read_bounded(Path(r['path'])),r['format']),r['keys'])
        except (OSError,ValueError,KeyError,TypeError): raise Conflict('源配置不可读，拒绝启动') from None
        if cfg != r['wrapper']: raise Conflict('源配置变化，暂停启动以避免错误接管')
        return copy.deepcopy(r)

    def toggle(self,bid,enabled):
        with self.lock:
            if bid not in self.bindings: raise Conflict('接管记录不存在')
            self.bindings[bid]['enabled']=enabled;self.save()
            if not enabled and self.runtime: self.runtime.stop(bid)
        return {'ok':True,'enabled':enabled}

    def restore(self,bid):
        with self.lock:
            r=self.bindings.get(bid)
            if not r: raise Conflict('接管记录不存在')
            path=Path(r['path']);raw=read_bounded(path)
            # Whole original file is safe only when no other edits occurred.
            if hashlib.sha256(raw).hexdigest()==r['afterHash']:
                restored=base64.b64decode(r['before'])
            else:
                restored=replace_entry(raw,r['format'],r['keys'],r['wrapper'],r['original'],r.get('fragment'))
            r['state']='restoring';self.save()
            if self.runtime:self.runtime.stop(bid)
            atomic(path,restored,expected=raw,mode=stat.S_IMODE(path.stat().st_mode))
            # Keep an encrypted/private historical recovery snapshot.
            self.vault.put('restored-'+bid+'.bin',r)
            del self.bindings[bid];self.save()
            (self.root/('ticket-'+bid+'.bin')).unlink(missing_ok=True)
        return {'ok':True,'restartClients':True}
