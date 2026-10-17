import http.client
import json
import threading
import time
import unittest
from mcpcell.demo import demo_discovery
from mcpcell.server import Workbench

class ServerTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp,scanner=demo_discovery();cls.server=Workbench(scanner,0)
        cls.thread=threading.Thread(target=cls.server.serve_forever,daemon=True);cls.thread.start()

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown();cls.thread.join();cls.server.server_close();cls.temp.cleanup()

    def request(self,path='/',method='GET',body=None,headers=None,auth=False):
        h=dict(headers or {})
        if auth:h['Authorization']='Bearer '+self.server.token
        conn=http.client.HTTPConnection('127.0.0.1',self.server.server_port,timeout=5)
        conn.request(method,path,body=body,headers=h)
        response=conn.getresponse();result=(response.status,dict(response.getheaders()),response.read());conn.close()
        return result

    def test_static_page_no_config_or_token(self):
        status,headers,body=self.request()
        self.assertEqual(status,200);self.assertNotIn(self.server.token.encode(),body)
        self.assertNotIn(b'DEMO_NOT_A_CREDENTIAL',body)
        self.assertIn('frame-ancestors',headers['Content-Security-Policy'])

    def test_api_requires_auth(self):self.assertEqual(self.request('/api/snapshot')[0],401)
    def test_snapshot_authenticated(self):
        status,headers,body=self.request('/api/snapshot',auth=True)
        self.assertEqual(status,200);self.assertEqual(json.loads(body)['mode'],'demo')
        self.assertEqual(headers['Cache-Control'],'no-store')
        self.assertNotIn('Access-Control-Allow-Origin',headers)
    def test_host_protected(self):self.assertEqual(self.request(headers={'Host':'evil.example'},auth=True)[0],403)
    def test_origin_protected(self):self.assertEqual(self.request('/api/snapshot',headers={'Origin':'https://evil.example'},auth=True)[0],403)
    def test_fetch_site_protected(self):self.assertEqual(self.request('/api/snapshot',headers={'Sec-Fetch-Site':'cross-site'},auth=True)[0],403)
    def test_wrong_bearer(self):self.assertEqual(self.request('/api/snapshot',headers={'Authorization':'Bearer wrong'})[0],401)
    def test_bad_unicode_bearer_is_401(self):self.assertEqual(self.request('/api/snapshot',headers={'Authorization':'Bearer é'})[0],401)
    def test_path_traversal(self):self.assertEqual(self.request('/../discovery.py')[0],404)
    def test_no_execute_endpoint(self):self.assertEqual(self.request('/api/execute','POST','{}',{'Content-Type':'application/json'},True)[0],404)
    def test_session_cookie(self):
        status,h,b=self.request('/api/session','POST','{}',{'Content-Type':'application/json'},True)
        self.assertEqual(status,200);self.assertIn('HttpOnly',h['Set-Cookie']);self.assertIn('SameSite=Strict',h['Set-Cookie'])
        self.assertEqual(self.request('/api/snapshot',headers={'Cookie':h['Set-Cookie'].split(';')[0]})[0],200)
    def test_non_json_rejected(self):self.assertEqual(self.request('/api/rescan','POST','{}',{'Content-Type':'text/plain'},True)[0],415)
    def test_path_argument_not_accepted(self):self.assertEqual(self.request('/api/rescan','POST','{"path":"/etc/passwd"}',{'Content-Type':'application/json'},True)[0],400)
    def test_payload_limit(self):self.assertEqual(self.request('/api/rescan','POST',' '*1025,{'Content-Type':'application/json'},True)[0],413)
    def test_proposal_only(self):
        status,headers,body=self.request('/api/plan',auth=True)
        self.assertEqual(status,200);self.assertFalse(json.loads(body)['applicable'])
    def test_rescan_cooldown(self):
        self.server.last_scan=time.monotonic()
        self.assertEqual(self.request('/api/rescan','POST','{}',{'Content-Type':'application/json'},True)[0],429)
    def test_allowed_rescan(self):
        self.server.last_scan=0
        status,h,b=self.request('/api/rescan','POST','{}',{'Content-Type':'application/json'},True)
        self.assertEqual(status,200);self.assertEqual(json.loads(b)['stats']['definitions'],12)

    def test_z_real_http_enroll_pause_restore(self):
        status,_,data=self.request('/api/snapshot',auth=True)
        entry=next(e for e in json.loads(data)['entries'] if e['canEnroll'])
        status,_,data=self.request('/api/enroll','POST',json.dumps({'ids':[entry['id']],'confirm':True}),{'Content-Type':'application/json'},True)
        self.assertEqual(status,200)
        result=json.loads(data);self.assertTrue(result['results'][0]['ok'],result)
        bid=result['results'][0]['bindingId']
        self.assertTrue(any(e.get('bindingId')==bid for e in result['snapshot']['entries']))
        try:
            status,_,data=self.request('/api/toggle','POST',json.dumps({'bindingId':bid,'enabled':False}),{'Content-Type':'application/json'},True)
            self.assertEqual(status,200);self.assertFalse(json.loads(data)['enabled'])
        finally:
            status,_,data=self.request('/api/restore','POST',json.dumps({'bindingId':bid}),{'Content-Type':'application/json'},True)
            self.assertEqual(status,200);self.assertTrue(json.loads(data)['ok'])
    def test_enroll_requires_explicit_confirmation(self):
        entry=next(e for e in self.server.snapshot['entries'] if e['canEnroll'])
        status,_,_=self.request('/api/enroll','POST',json.dumps({'ids':[entry['id']],'confirm':False}),{'Content-Type':'application/json'},True)
        self.assertEqual(status,409)

if __name__=='__main__':unittest.main()
