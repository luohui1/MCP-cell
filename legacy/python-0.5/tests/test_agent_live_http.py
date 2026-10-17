"""Actual loopback HTTP -> owned POSIX PTY -> harmless fixture, not demo."""
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

from mcpcell.discovery import Discovery
from mcpcell.server import Workbench

@unittest.skipIf(os.name=='nt','POSIX native integration; Windows requires a native runner')
class LiveAgentHTTPTests(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.home=Path(self.tmp.name).resolve()
        self.server=Workbench(Discovery(self.home,environ={}),0,agent_environ={'PATH':os.defpath,'HOME':str(self.home)})
        self.thread=threading.Thread(target=self.server.serve_forever,daemon=True);self.thread.start()
    def tearDown(self):
        self.server.shutdown();self.thread.join();self.server.server_close();self.tmp.cleanup()
    def call(self,path,body=None):
        conn=http.client.HTTPConnection('127.0.0.1',self.server.server_port,timeout=5)
        headers={'Authorization':'Bearer '+self.server.token,'Content-Type':'application/json'}
        conn.request('POST' if body is not None else 'GET',path,body=json.dumps(body) if body is not None else None,headers=headers)
        response=conn.getresponse();code=response.status;data=json.loads(response.read());conn.close()
        self.assertEqual(code,200,data);return data
    def wait_output(self,sid,needle):
        deadline=time.monotonic()+4
        while time.monotonic()<deadline:
            out=self.call('/api/agents/output?sessionId='+sid+'&offset=0')
            text=base64.b64decode(out['data']).decode(errors='replace')
            if needle in text:return text
            time.sleep(.03)
        self.fail('Expected fixture output not received')
    def test_live_register_launch_input_resize_and_close(self):
        fixture=str(Path(__file__).with_name('fixture_agent.py').resolve())
        hub=self.call('/api/agents/register',{'name':'HTTP fixture','executable':sys.executable,'args':['-u',fixture],'confirm':True})
        key=next(a['id'] for a in hub['agents'] if a['name']=='HTTP fixture')
        body={'agentId':key,'cwd':str(self.home),'requestId':os.urandom(16).hex(),'confirm':True}
        session=self.call('/api/agents/launch',body)['session'];sid=session['id']
        self.assertEqual(session['backend'],'posix-pty');self.assertIsInstance(session['pid'],int)
        self.assertIn('FIXTURE_READY True',self.wait_output(sid,'FIXTURE_READY'))
        self.assertEqual(self.call('/api/agents/launch',body)['session']['id'],sid)
        self.call('/api/agents/input',{'sessionId':sid,'data':'HTTP 中文\r'})
        self.assertIn('REPLY HTTP 中文',self.wait_output(sid,'REPLY HTTP 中文'))
        self.call('/api/agents/resize',{'sessionId':sid,'cols':91,'rows':27})
        self.wait_output(sid,'RESIZED')
        obj=self.server.agent_hub.get_session(sid)
        self.call('/api/agents/close',{'sessionId':sid,'confirm':True})
        self.assertTrue(obj.closed);self.assertIsNotNone(obj.process.poll())
        self.assertEqual(self.call('/api/agents')['sessions'],[])
