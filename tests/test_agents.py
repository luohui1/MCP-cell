import base64
import http.client
import json
import os
from pathlib import Path
import sys
import tempfile
import threading
import time
import unittest
from unittest.mock import patch

from mcpcell.agent_terminal import OutputBuffer, TerminalSession
from mcpcell.agents import AgentHub, CATALOG
from mcpcell.storage import Vault, Conflict
from mcpcell.discovery import Discovery
from mcpcell.server import Workbench

FIXTURE=Path(__file__).with_name('fixture_agent.py').resolve()

class BufferTests(unittest.TestCase):
    def test_bounded_output_and_loss_marker(self):
        b=OutputBuffer(8);b.append(b'1234567890')
        r=b.read(0);self.assertEqual(base64.b64decode(r['data']),b'34567890')
        self.assertEqual(r['droppedBytes'],2);self.assertEqual(r['next'],10)
    def test_cursor_does_not_replay(self):
        b=OutputBuffer();b.append('中文'.encode());r=b.read(0)
        self.assertEqual(base64.b64decode(r['data']).decode(),'中文')
        self.assertEqual(b.read(r['next'])['data'],'')
    def test_invalid_offsets_rejected(self):
        for n in [-1,True,4]:
            with self.assertRaises(Conflict):OutputBuffer().read(n)

class AgentTests(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.home=Path(self.tmp.name).resolve();self.bin=self.home/'bin';self.bin.mkdir()
        self.env={'PATH':str(self.bin),'HOME':str(self.home),'SECRET_SENTINEL':'DO_NOT_PASS',
                  'OPENAI_API_KEY':'FAKE_OPENAI','ANTHROPIC_API_KEY':'FAKE_ANTHROPIC'}
        self.hub=AgentHub(self.home,Vault(self.home/'state'),environ=self.env)
        self.addCleanup(self.hub.close)
    def executable(self,name):
        p=self.bin/name
        p.write_text('#!'+sys.executable+'\n'+FIXTURE.read_text(),encoding='utf-8');p.chmod(0o700);return p
    def custom(self):
        x=self.hub.register('Fixture',sys.executable,['-I',str(FIXTURE)])
        return next(a['id'] for a in x['agents'] if a['kind']=='custom')
    def launch(self,key,request=None):
        return self.hub.launch(key,str(self.home),request or os.urandom(16).hex())
    def output(self,sid,contains='',timeout=4):
        deadline=time.time()+timeout;value=''
        while time.time()<deadline:
            s=self.hub.get_session(sid)
            value=base64.b64decode(s.output.read(0)['data']).decode(errors='replace')
            if contains in value:return value
            time.sleep(.03)
        self.fail('Expected fixture output not observed: '+contains+' in '+value)
    def test_catalog_includes_cc_pi_and_other_runtimes(self):
        self.assertEqual(len(CATALOG),10);self.assertIn('pi',[a['id'] for a in CATALOG])
    def test_cc_compiler_not_detected_as_claude(self):
        self.executable('cc');self.hub.detect()
        self.assertFalse(next(a for a in self.hub.snapshot()['agents'] if a['id']=='claude-code')['found'])
    def test_detection_never_runs_a_program(self):
        self.executable('claude')
        with patch('subprocess.Popen',side_effect=AssertionError('no execution')):
            result=self.hub.detect()
        self.assertTrue(result['agents'][0]['found']);self.assertFalse(result['agents'][0]['enrolled'])
    def test_enrollment_is_persisted_without_starting(self):
        self.executable('claude');self.hub.detect();self.hub.enroll(['claude-code'])
        self.assertFalse(self.hub.sessions)
        restored=AgentHub(self.home,Vault(self.home/'state'),environ=self.env);self.addCleanup(restored.close)
        self.assertTrue(restored.snapshot()['agents'][0]['enrolled'])
    def test_missing_agent_cannot_be_enrolled(self):
        with self.assertRaises(Conflict):self.hub.enroll(['pi'])
    def test_custom_requires_absolute_executable(self):
        with self.assertRaises(Conflict):self.hub.register('a','relative',[])
    def test_shell_not_a_registered_agent(self):
        p=self.executable('bash')
        with self.assertRaises(Conflict):self.hub.register('a',str(p),[])
    def test_custom_argv_shape(self):
        with self.assertRaises(Conflict):self.hub.register('a',sys.executable,'not a list')
    def test_pin_disable_and_remove(self):
        key=self.custom();self.hub.update(key,False,True)
        item=next(a for a in self.hub.snapshot()['agents'] if a['id']==key)
        self.assertTrue(item['pinned']);self.assertFalse(item['enabled'])
        with self.assertRaises(Conflict):self.launch(key)
        self.hub.remove(key);self.assertNotIn(key,self.hub.records)
    def test_empty_name_rejected(self):
        with self.assertRaises(Conflict):self.hub.register('',sys.executable,[])
    def test_boolean_state_validation(self):
        key=self.custom()
        with self.assertRaises(Conflict):self.hub.update(key,'true',False)
    def test_no_auth_values_in_catalog_snapshot(self):
        self.assertNotIn('FAKE_OPENAI',json.dumps(self.hub.snapshot()))
    def test_program_change_requires_review(self):
        p=self.executable('claude');self.hub.detect();self.hub.enroll(['claude-code'])
        p.write_text(p.read_text()+'\n# changed\n')
        with self.assertRaises(Conflict):self.launch('claude-code')
    def test_demo_does_not_execute_or_read_host_programs(self):
        with patch('subprocess.Popen',side_effect=AssertionError('no process')):
            demo=AgentHub(self.home,Vault(self.home/'demo-state'),demo=True,environ={})
            self.addCleanup(demo.close);s=demo.launch('pi',str(self.home),'b'*32)
            demo.get_session(s['id']).write('not executed\r')
        self.assertEqual(s['backend'],'demo')
    @unittest.skipIf(os.name=='nt','POSIX PTY native test')
    def test_actual_interactive_pty(self):
        key=self.custom();s=self.launch(key)
        text=self.output(s['id'],'FIXTURE_READY');self.assertIn('True',text)
        self.hub.get_session(s['id']).write('hello 中文\r')
        self.output(s['id'],'REPLY hello 中文')
    @unittest.skipIf(os.name=='nt','POSIX PTY native test')
    def test_launch_retry_is_idempotent(self):
        key=self.custom();first=self.launch(key,'c'*32);again=self.launch(key,'c'*32)
        self.assertEqual(first['id'],again['id']);self.assertEqual(first['pid'],again['pid'])
        self.assertEqual(len(self.hub.sessions),1)
    @unittest.skipIf(os.name=='nt','POSIX PTY native test')
    def test_closed_request_never_restarts(self):
        key=self.custom();s=self.launch(key,'d'*32);self.hub.close_session(s['id'])
        with self.assertRaises(Conflict):self.launch(key,'d'*32)
    @unittest.skipIf(os.name=='nt','POSIX PTY native test')
    def test_workspaces_and_sessions_do_not_mix(self):
        key=self.custom();other=self.home/'other';other.mkdir()
        a=self.launch(key);b=self.hub.launch(key,str(other),'e'*32)
        self.assertNotEqual(a['pid'],b['pid'])
        self.output(a['id'],'FIXTURE_READY');self.output(b['id'],'FIXTURE_READY')
        self.hub.get_session(a['id']).write('ONLY_A\r');self.output(a['id'],'REPLY ONLY_A')
        self.assertNotIn('ONLY_A',self.output(b['id']))
        self.hub.close_session(a['id']);self.hub.get_session(b['id']).write('B_STILL_RUNNING\r')
        self.output(b['id'],'REPLY B_STILL_RUNNING')
    @unittest.skipIf(os.name=='nt','POSIX PTY native test')
    def test_running_agent_cannot_be_removed(self):
        key=self.custom();self.launch(key)
        with self.assertRaises(Conflict):self.hub.remove(key)
    @unittest.skipIf(os.name=='nt','POSIX PTY native test')
    def test_resize_updates_pty_and_dimensions(self):
        key=self.custom();s=self.launch(key);self.output(s['id'],'FIXTURE_READY')
        t=self.hub.get_session(s['id']);t.resize(81,26)
        self.assertEqual(t.snapshot()['cols'],81);self.output(s['id'],'RESIZED')
        with self.assertRaises(Conflict):t.resize(10000,20)
    @unittest.skipIf(os.name=='nt','POSIX PTY native test')
    def test_environment_scoped_to_agent(self):
        self.executable('codex');self.hub.detect();self.hub.enroll(['codex']);s=self.launch('codex')
        self.output(s['id'],'FIXTURE_READY');self.hub.get_session(s['id']).write('environment\r')
        text=self.output(s['id'],'SECRET_SENTINEL')
        self.assertIn('FAKE_OPENAI',text);self.assertNotIn('FAKE_ANTHROPIC',text);self.assertNotIn('DO_NOT_PASS',text)
    @unittest.skipIf(os.name=='nt','POSIX PTY native test')
    def test_input_is_literal_not_shell(self):
        key=self.custom();s=self.launch(key);self.output(s['id'],'FIXTURE_READY')
        self.hub.get_session(s['id']).write('$(touch NEVER)\r')
        self.output(s['id'],'REPLY $(touch NEVER)');self.assertFalse((self.home/'NEVER').exists())
    @unittest.skipIf(os.name=='nt','POSIX PTY native test')
    def test_active_session_limit(self):
        key=self.custom()
        with patch('mcpcell.agents.MAX_RUNNING',1):
            self.launch(key)
            with self.assertRaises(Conflict):self.launch(key)
    @unittest.skipIf(os.name=='nt','POSIX PTY native test')
    def test_stop_and_manager_shutdown_reap_owned_process(self):
        key=self.custom();s=self.launch(key);self.output(s['id'],'FIXTURE_READY')
        obj=self.hub.get_session(s['id']);self.hub.close()
        self.assertIsNotNone(obj.process.poll());self.assertTrue(obj.closed)
    @unittest.skipIf(os.name=='nt','POSIX PTY native test')
    def test_natural_exit_is_reported(self):
        s=self.launch(self.custom());self.output(s['id'],'FIXTURE_READY')
        obj=self.hub.get_session(s['id']);obj.write('quit\r')
        deadline=time.time()+3
        while not obj.closed and time.time()<deadline:time.sleep(.02)
        self.assertEqual(obj.state,'exited');self.assertEqual(obj.exit_code,0)
    def test_invalid_workdir(self):
        key=self.custom()
        with self.assertRaises(Conflict):self.hub.launch(key,'relative','a'*32)

class AgentAPITests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        from mcpcell.demo import demo_discovery
        cls.tmp,scanner=demo_discovery();cls.server=Workbench(scanner,0)
        cls.thread=threading.Thread(target=cls.server.serve_forever,daemon=True);cls.thread.start()
    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown();cls.thread.join();cls.server.server_close();cls.tmp.cleanup()
    def call(self,path,body=None,auth=True,origin=None):
        conn=http.client.HTTPConnection('127.0.0.1',self.server.server_port,timeout=5)
        headers={}
        if auth:headers['Authorization']='Bearer '+self.server.token
        if origin:headers['Origin']=origin
        if body is not None:headers['Content-Type']='application/json'
        conn.request('POST' if body is not None else 'GET',path,body=json.dumps(body) if body is not None else None,headers=headers)
        r=conn.getresponse();data=json.loads(r.read());code=r.status;conn.close();return code,data
    def test_catalog_requires_auth(self):self.assertEqual(self.call('/api/agents',auth=False)[0],401)
    def test_launch_rejects_foreign_origin(self):
        self.assertEqual(self.call('/api/agents/launch',{},origin='https://foreign.example')[0],403)
    def test_launch_requires_confirmation(self):
        self.assertEqual(self.call('/api/agents/launch',{'agentId':'pi','cwd':str(self.server.scanner.home),'requestId':'a'*32,'confirm':False})[0],409)
    def test_full_demo_session_api(self):
        c,x=self.call('/api/agents/launch',{'agentId':'pi','cwd':str(self.server.scanner.home),'requestId':os.urandom(16).hex(),'confirm':True})
        self.assertEqual(c,200);sid=x['session']['id']
        self.assertEqual(self.call('/api/agents/input',{'sessionId':sid,'data':'hello\r'})[0],200)
        c,out=self.call('/api/agents/output?sessionId='+sid+'&offset=0');self.assertEqual(c,200)
        self.assertIn('演示',base64.b64decode(out['data']).decode())
        self.assertEqual(self.call('/api/agents/resize',{'sessionId':sid,'cols':80,'rows':25})[0],200)
        self.assertEqual(self.call('/api/agents/rename',{'sessionId':sid,'title':'One'})[0],200)
        self.assertEqual(self.call('/api/agents/close',{'sessionId':sid,'confirm':True})[0],200)
    def test_unknown_session_does_not_execute(self):
        self.assertEqual(self.call('/api/agents/input',{'sessionId':'unknown','data':'hello'})[0],409)
    def test_unknown_body_field_rejected(self):
        self.assertEqual(self.call('/api/agents/launch',{'shell':'whoami'})[0],400)
    def test_scope_requires_confirmation_and_existing_absolute_path(self):
        self.assertEqual(self.call('/api/detection/scope',{'workspace':'relative','confirm':True})[0],409)
    def test_explicit_scope_scan_is_available(self):
        path=self.server.scanner.home/'new-project';path.mkdir(exist_ok=True)
        c,x=self.call('/api/detection/scope',{'workspace':str(path),'confirm':True});self.assertEqual(c,200)
        self.assertIn(path,self.server.scanner.workspaces)

if __name__=='__main__':unittest.main()
