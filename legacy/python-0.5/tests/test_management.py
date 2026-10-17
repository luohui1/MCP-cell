import base64
import copy
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unittest

from mcpcell.clients import Source
from mcpcell.discovery import Discovery,parse
from mcpcell.management import Manager
from mcpcell.patching import replace_entry
from mcpcell.runtime import Runtime
from mcpcell.storage import Conflict,InstanceLock,Vault

FIXTURE=str(Path(__file__).with_name('fixture_mcp.py').absolute())

class ManagedTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.home=Path(self.temp.name).resolve()
        self.path=self.home/'.cursor/mcp.json';self.path.parent.mkdir()
        self.cfg={'command':sys.executable,'args':[FIXTURE],'env':{'FIXTURE_TOKEN':'SECRET_TEST_VALUE'},'autoApprove':[]}
        self.path.write_text(json.dumps({'mcpServers':{'echo':self.cfg},'other':{'keep':True}}))
        self.original=self.path.read_bytes()
        self.scanner=Discovery(self.home,platform='linux',environ={})
        self.manager=Manager(self.scanner,self.home/'.mcpcell')
        self.runtime=None;self.streams=[]
    def tearDown(self):
        for s in self.streams:
            try:s.close()
            except OSError:pass
        if self.runtime:self.runtime.close()
        self.temp.cleanup()
    def enroll(self):
        snap=self.manager.scan();e=next(e for e in snap['entries'] if e['name']=='echo')
        response=self.manager.enroll([e['id']]);self.assertTrue(response['results'][0]['ok'],response)
        return response['results'][0]['bindingId']
    def start_runtime(self):
        self.runtime=Runtime(self.manager);self.manager.runtime=self.runtime
        self.manager.vault.put('endpoint.bin',{'brokerPort':self.runtime.server_address[1]})
    def connect(self,bid):
        s=socket.create_connection(self.runtime.server_address,timeout=3);self.streams.append(s)
        f=s.makefile('rwb',buffering=0);self.streams.append(f)
        r=self.manager.bindings[bid]
        f.write(json.dumps({'bindingId':bid,'token':r['token'],'cwd':str(self.home),
                'env':{'PATH':os.environ.get('PATH',''),'FIXTURE_TOKEN':'SECRET_TEST_VALUE','UNRELATED_SECRET':'never-forward'}}).encode()+b'\n')
        self.assertTrue(json.loads(f.readline())['ok'])
        return s,f
    def rpc(self,f,method='tools/call',id=7,args=None):
        obj={'jsonrpc':'2.0','id':id,'method':method,'params':{'name':'echo','arguments':args or {'hello':'world'}}}
        f.write(json.dumps(obj).encode()+b'\n');return json.loads(f.readline())
    def wait_empty(self):
        deadline=time.monotonic()+4
        while self.runtime.status()['running'] and time.monotonic()<deadline:time.sleep(.04)
        self.assertEqual(self.runtime.status()['running'],0)

    def test_real_enroll_changes_launch_not_other_fields(self):
        bid=self.enroll();obj=json.loads(self.path.read_text());e=obj['mcpServers']['echo']
        self.assertIn('bridge.py',' '.join(e['args']));self.assertIn(bid,e['args'])
        self.assertEqual(e['env'],self.cfg['env']);self.assertEqual(obj['other'],{'keep':True})
    def test_enroll_does_not_start_program(self):
        self.enroll();self.assertIsNone(self.manager.runtime)
    def test_byte_exact_restore(self):
        bid=self.enroll();self.manager.restore(bid);self.assertEqual(self.path.read_bytes(),self.original)
    def test_restore_keeps_external_unrelated_edits(self):
        bid=self.enroll();obj=json.loads(self.path.read_text());obj['other']['new']='keep-me'
        self.path.write_text(json.dumps(obj));self.manager.restore(bid)
        obj=json.loads(self.path.read_text());self.assertEqual(obj['other']['new'],'keep-me');self.assertEqual(obj['mcpServers']['echo'],self.cfg)
    def test_restore_refuses_changed_managed_entry(self):
        bid=self.enroll();obj=json.loads(self.path.read_text());obj['mcpServers']['echo']['args']=['changed'];self.path.write_text(json.dumps(obj))
        changed=self.path.read_bytes()
        with self.assertRaises(Conflict):self.manager.restore(bid)
        self.assertEqual(changed,self.path.read_bytes())
    def test_stale_scan_refuses_to_write(self):
        e=self.manager.scan()['entries'][0];self.path.write_text('{}')
        r=self.manager.enroll([e['id']]);self.assertFalse(r['results'][0]['ok']);self.assertEqual(self.path.read_text(),'{}')
    def test_multiple_selected_in_one_file(self):
        obj=json.loads(self.path.read_text());obj['mcpServers']['second']=dict(self.cfg)
        self.path.write_text(json.dumps(obj));original=self.path.read_bytes()
        snap=self.manager.scan();r=self.manager.enroll([e['id'] for e in snap['entries']]);self.assertTrue(all(i['ok'] for i in r['results']),r)
        for i in reversed(r['results']):self.manager.restore(i['bindingId'])
        self.assertEqual(original,self.path.read_bytes())
    def test_forward_order_restore_keeps_other_binding(self):
        obj=json.loads(self.path.read_text());obj['mcpServers']['second']=dict(self.cfg);self.path.write_text(json.dumps(obj))
        snap=self.manager.scan();r=self.manager.enroll([e['id'] for e in snap['entries']])
        self.manager.restore(r['results'][0]['bindingId'])
        self.manager.verify(r['results'][1]['bindingId'])
        self.manager.restore(r['results'][1]['bindingId'])
        self.assertEqual(parse(self.path.read_bytes(),'json'),obj)
    def test_codex_actual_enroll_restore(self):
        path=self.home/'.codex/config.toml';path.parent.mkdir()
        import tomlkit
        raw=tomlkit.dumps({'model':'keep','mcp_servers':{'codexecho':self.cfg}}).encode()
        path.write_bytes(raw)
        e=next(e for e in self.manager.scan()['entries'] if e['name']=='codexecho')
        result=self.manager.enroll([e['id']]);self.assertTrue(result['results'][0]['ok'],result)
        self.assertIn('bridge.py',path.read_text());self.manager.restore(result['results'][0]['bindingId'])
        self.assertEqual(raw,path.read_bytes())
    def test_opencode_array_actual_enroll(self):
        p=self.home/'.config/opencode/opencode.json';p.parent.mkdir(parents=True)
        raw=json.dumps({'mcp':{'openecho':{'type':'local','command':[sys.executable,FIXTURE],'environment':{'X':'value'}}}}).encode()
        p.write_bytes(raw)
        e=next(e for e in self.manager.scan()['entries'] if e['name']=='openecho')
        result=self.manager.enroll([e['id']]);self.assertTrue(result['results'][0]['ok'],result)
        cfg=json.loads(p.read_bytes())['mcp']['openecho'];self.assertIsInstance(cfg['command'],list)
        self.assertEqual(cfg['environment'],{'X':'value'});self.manager.restore(result['results'][0]['bindingId']);self.assertEqual(raw,p.read_bytes())
    def test_registry_reopen_stable_inventory(self):
        bid=self.enroll();key=self.manager.scan()['entries'][0]['candidateKey']
        manager=Manager(Discovery(self.home,platform='linux',environ={}),self.home/'.mcpcell')
        e=manager.scan()['entries'][0];self.assertTrue(e['managed']);self.assertEqual(e['candidateKey'],key);self.assertEqual(e['bindingId'],bid)
    def test_prepared_after_write_reconciles(self):
        bid=self.enroll();self.manager.bindings[bid]['state']='prepared';self.manager.save()
        reopened=Manager(self.scanner,self.home/'.mcpcell');self.assertEqual(reopened.bindings[bid]['state'],'active')
    def test_restore_interrupted_after_write_reconciles(self):
        bid=self.enroll();self.manager.bindings[bid]['state']='restoring';self.manager.save();self.path.write_bytes(self.original)
        reopened=Manager(self.scanner,self.home/'.mcpcell');self.assertNotIn(bid,reopened.bindings)
    def test_secrets_not_in_public_snapshot(self):
        self.enroll();self.assertNotIn('SECRET_TEST_VALUE',json.dumps(self.manager.scan()))
    def test_pause_persists(self):
        bid=self.enroll();self.manager.toggle(bid,False)
        reopened=Manager(self.scanner,self.home/'.mcpcell')
        with self.assertRaises(Conflict):reopened.verify(bid)
    def test_remote_not_rewritten(self):
        self.path.write_text(json.dumps({'mcpServers':{'remote':{'url':'https://example.org/mcp'}}}));before=self.path.read_bytes()
        e=self.manager.scan()['entries'][0];self.assertFalse(e['canEnroll'])
        self.assertFalse(self.manager.enroll([e['id']])['results'][0]['ok']);self.assertEqual(before,self.path.read_bytes())
    def test_single_instance_lock(self):
        lock=InstanceLock(self.home/'locktest')
        try:
            with self.assertRaises(Conflict):InstanceLock(self.home/'locktest')
        finally:lock.close()
        InstanceLock(self.home/'locktest').close()
    def test_private_file_modes(self):
        if os.name=='nt':self.skipTest('POSIX permissions only')
        self.enroll();self.assertEqual((self.home/'.mcpcell').stat().st_mode&0o777,0o700)
        self.assertEqual((self.home/'.mcpcell/registry.bin').stat().st_mode&0o777,0o600)
    def test_runtime_waits_for_protocol_input(self):
        bid=self.enroll();self.start_runtime();s,f=self.connect(bid)
        self.assertEqual(self.runtime.status()['running'],0)
        result=self.rpc(f,'initialize');self.assertEqual(result['id'],7)
        self.assertEqual(self.runtime.status()['running'],1)
    def test_protocol_roundtrip_real_process(self):
        bid=self.enroll();self.start_runtime();s,f=self.connect(bid)
        r=self.rpc(f,args={'中文':'ok'},id='same-id');self.assertEqual(r['id'],'same-id');self.assertIn('content',r['result'])
    def test_environment_whitelist(self):
        bid=self.enroll();self.start_runtime();s,f=self.connect(bid);r=self.rpc(f,'env')
        self.assertEqual(r['result']['present'],'SECRET_TEST_VALUE');self.assertIsNone(r['result']['unexpected'])
    def test_two_sessions_isolated(self):
        bid=self.enroll();self.start_runtime();s1,f1=self.connect(bid);s2,f2=self.connect(bid)
        self.assertEqual(self.rpc(f1,id=1)['id'],1);self.assertEqual(self.rpc(f2,id=1)['id'],1)
        pids=[r['pid'] for r in self.runtime.status()['instances']];self.assertEqual(len(set(pids)),2)
    def test_pause_stops_and_denies_connections(self):
        bid=self.enroll();self.start_runtime();s,f=self.connect(bid);self.rpc(f)
        self.manager.toggle(bid,False);self.wait_empty()
        with self.assertRaises(Conflict):self.manager.verify(bid)
    def test_eof_reaps_process(self):
        bid=self.enroll();self.start_runtime();s,f=self.connect(bid);self.rpc(f)
        s.shutdown(socket.SHUT_RDWR);self.wait_empty()
    def test_wrong_token_no_process(self):
        bid=self.enroll();self.start_runtime();s=socket.create_connection(self.runtime.server_address,timeout=2)
        try:
            s.sendall(json.dumps({'bindingId':bid,'token':'bad','cwd':str(self.home),'env':{}}).encode()+b'\n')
            self.assertEqual(s.recv(1000),b'');self.assertEqual(self.runtime.status()['running'],0)
        finally:s.close()
    def test_real_bridge_executable_path_roundtrip(self):
        bid=self.enroll();self.start_runtime();wrapper=json.loads(self.path.read_text())['mcpServers']['echo']
        env={**os.environ,'FIXTURE_TOKEN':'SECRET_TEST_VALUE'}
        p=subprocess.Popen([wrapper['command']]+wrapper['args'],cwd=self.home,env=env,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
        try:
            p.stdin.write(json.dumps({'jsonrpc':'2.0','id':99,'method':'tools/list'})+'\n');p.stdin.flush()
            result=json.loads(p.stdout.readline());self.assertEqual(result['id'],99);self.assertEqual(result['result']['tools'][0]['name'],'echo')
        finally:
            p.stdin.close();p.wait(timeout=5);p.stdout.close();p.stderr.close()
        self.wait_empty()

class PatchTests(unittest.TestCase):
    def test_jsonc_untouched_comments_and_fields(self):
        raw=b'{\n// top\n"mcpServers":{"a":{/* inner */"command":"node", "args":[], "custom":3}}, "other":7,\n}'
        expected={'command':'node','args':[],'custom':3};new={**expected,'command':'python','args':['bridge.py']}
        out=replace_entry(raw,'jsonc',['mcpServers','a'],expected,new)
        self.assertIn(b'// top',out);self.assertIn(b'/* inner */',out);self.assertIn(b'"other":7',out)
    def test_jsonc_insert_missing_args_trailing_comma(self):
        raw=b'{"mcpServers":{"a":{"command":"node", // comment\n}}}'
        out=replace_entry(raw,'jsonc',['mcpServers','a'],{'command':'node'},{'command':'python','args':['b.py']})
        self.assertEqual(parse(out,'jsonc')['mcpServers']['a']['args'],['b.py'])
    def test_codex_toml_comments_and_unrelated_fields(self):
        raw=b'# header\nmodel = "original"\n[mcp_servers.echo]\ncommand = "node" # keep\nargs = ["server.js"]\nstartup_timeout_sec = 30\n\n[other]\nvalue = 1\n'
        expected=parse(raw,'toml')['mcp_servers']['echo'];new={**expected,'command':'python','args':['bridge.py']}
        out=replace_entry(raw,'toml',['mcp_servers','echo'],expected,new)
        self.assertIn(b'# header',out);self.assertIn(b'# keep',out);self.assertIn(b'[other]\nvalue = 1',out)
    def test_nested_claude_project(self):
        keys=['projects','/work/a','mcpServers','echo'];raw=json.dumps({'projects':{'/work/a':{'mcpServers':{'echo':{'command':'node'}}}}}).encode()
        out=replace_entry(raw,'json',keys,{'command':'node'},{'command':'python','args':[]})
        self.assertIn(b'python',out)

if __name__=='__main__':unittest.main()
