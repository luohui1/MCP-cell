import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from mcpcell.clients import CLIENTS, Source, candidates
from mcpcell.discovery import ConfigError, Discovery, MAX_FILE, jsonc, parse, plan, read_bounded
from mcpcell.demo import demo_discovery


class ParserTests(unittest.TestCase):
    def test_jsonc_preserves_urls_and_comments_in_strings(self):
        result = parse(b'{// comment\n"url":"https://a.example/a//b?x=/*literal*/", /* block */ "n": [1,2,],}', 'jsonc')
        self.assertEqual(result['url'], 'https://a.example/a//b?x=/*literal*/')
        self.assertEqual(result['n'], [1,2])

    def test_jsonc_preserves_escapes(self):
        value = {'str':'quote " comma,} backslash \\ //'}
        self.assertEqual(parse(json.dumps(value).encode(), 'jsonc'), value)

    def test_jsonc_unclosed_comment(self):
        with self.assertRaises(ConfigError): parse(b'{/* broken', 'jsonc')

    def test_json_rejects_comments(self):
        with self.assertRaises(ConfigError): parse(b'{/*no*/}', 'json')

    def test_duplicate_keys_rejected(self):
        with self.assertRaises(ConfigError): parse(b'{"a":1,"a":2}', 'json')

    def test_non_finite_rejected(self):
        for x in [b'{"x":NaN}', b'{"x":Infinity}', b'{"x":1e9999}']:
            with self.assertRaises(ConfigError): parse(x, 'json')

    def test_bom_supported(self):
        self.assertEqual(parse(b'\xef\xbb\xbf{}','json'),{})

    def test_toml(self):
        x=parse(b'[mcp_servers.a]\ncommand="node"\nargs=["server.js"]','toml')
        self.assertEqual(x['mcp_servers']['a']['args'],['server.js'])

    def test_sanitized_parser_error(self):
        with self.assertRaises(ConfigError) as ctx: parse(b'{"MY_SECRET_VALUE":broken}', 'json')
        self.assertNotIn('MY_SECRET_VALUE', str(ctx.exception))

    def test_non_object_rejected(self):
        for text in [b'[]', b'null', b'1']:
            with self.assertRaises(ConfigError): parse(text,'json')

    def test_deep_json_rejected_safely(self):
        with self.assertRaises(ConfigError): parse(('{'+'"a":['+'['*1500+'0'+']'*1501+'}').encode(),'json')


class DiscoveryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.home=Path(self.temp.name).resolve()

    def write(self,rel,obj):
        p=self.home/rel;p.parent.mkdir(parents=True,exist_ok=True)
        p.write_text(json.dumps(obj),encoding='utf-8')
        return p

    def scanner(self,**kwargs):
        return Discovery(self.home,platform='linux',environ={},**kwargs)

    def test_empty_home_no_fake_metrics(self):
        x=self.scanner().scan()
        self.assertEqual(x['stats']['definitions'],0)
        self.assertIsNone(x['stats']['memorySavedBytes'])
        self.assertIsNone(x['stats']['processCount'])
        self.assertFalse(x['capabilities']['executeCli'])

    def test_all_client_families_registered(self):
        self.assertEqual(len(CLIENTS),11)
        for platform in ['win32','darwin','linux']:
            ids={s.client for s in candidates(self.home,[],platform=platform,environ={})}
            self.assertEqual(ids,set(CLIENTS)-({'claude-desktop'} if platform=='linux' else set()))

    def test_windows_appdata(self):
        src=candidates(self.home,[],platform='win32',environ={'APPDATA':str(self.home/'roaming')})
        self.assertTrue(any(s.client=='claude-desktop' and s.path==self.home/'roaming/Claude/claude_desktop_config.json' for s in src))

    def test_environment_overrides(self):
        src=candidates(self.home,[],platform='linux',environ={'CODEX_HOME':str(self.home/'alt'),'COPILOT_HOME':str(self.home/'copilot')})
        self.assertTrue(any(s.path==self.home/'alt/config.toml' for s in src))
        self.assertTrue(any(s.path==self.home/'copilot/mcp-config.json' for s in src))

    def test_safe_identity_does_not_echo_secrets(self):
        self.write('.cursor/mcp.json',{'mcpServers':{'remote':{'type':'http','url':'https://user:URL_SECRET@a.example/PRIVATE_PATH?token=QUERY_SECRET#FRAGMENT_SECRET','headers':{'Authorization':'HEADER_SECRET'},'env':{'KEY':'ENV_SECRET'},'oauth':{'clientSecret':'OAUTH_SECRET'}}}})
        self.write('.claude.json',{'mcpServers':{'local':{'command':'node','args':['ARG_SECRET']}}})
        text=json.dumps(self.scanner().scan())
        for secret in ['URL_SECRET','PRIVATE_PATH','QUERY_SECRET','FRAGMENT_SECRET','HEADER_SECRET','ENV_SECRET','OAUTH_SECRET','ARG_SECRET']:
            self.assertNotIn(secret,text)

    def test_custom_command_hidden(self):
        self.write('.cursor/mcp.json',{'mcpServers':{'local':{'command':'/a/COMMAND_SECRET'}}})
        self.assertNotIn('COMMAND_SECRET',json.dumps(self.scanner().scan()))

    def test_candidate_not_sharing_approval(self):
        value={'mcpServers':{'a':{'command':'node','args':['a.js']}}}
        self.write('.cursor/mcp.json',value);self.write('.claude.json',value)
        x=self.scanner().scan()
        self.assertEqual(x['stats']['candidateGroups'],1)
        for e in x['entries']:
            self.assertEqual(e['sharing'],'exclusive');self.assertFalse(e['managed'])

    def test_different_secrets_not_grouped(self):
        for file,key in [('.cursor/mcp.json','one'),('.claude.json','two')]:
            self.write(file,{'mcpServers':{'a':{'command':'node','args':['a.js'],'env':{'TOKEN':key}}}})
        self.assertEqual(self.scanner().scan()['stats']['candidateGroups'],0)

    def test_same_name_different_commands_not_grouped(self):
        for file,command in [('.cursor/mcp.json','node'),('.claude.json','python')]:
            self.write(file,{'mcpServers':{'a':{'command':command}}})
        self.assertEqual(self.scanner().scan()['stats']['candidateGroups'],0)

    def test_shared_physical_file_not_duplicate_group(self):
        root=self.home/'project';root.mkdir()
        self.write('project/.mcp.json',{'mcpServers':{'a':{'command':'node','args':['a.js']}}})
        x=self.scanner(workspaces=[root]).scan()
        self.assertEqual(x['stats']['definitions'],2) # two client bindings, one origin
        self.assertEqual(x['stats']['candidateGroups'],0)

    def test_workspace_not_scanned_implicitly(self):
        self.write('project/.cursor/mcp.json',{'mcpServers':{'a':{'command':'node'}}})
        self.assertEqual(self.scanner().scan()['stats']['definitions'],0)

    def test_only_selected_claude_project(self):
        root=self.home/'project';root.mkdir()
        self.write('.claude.json',{'projects':{str(root):{'mcpServers':{'chosen':{'command':'node'}}},'/unselected':{'mcpServers':{'private':{'command':'node'}}}}})
        x=self.scanner(workspaces=[root]).scan()
        self.assertEqual([e['name'] for e in x['entries']],['chosen'])

    def test_explicit_custom_source(self):
        p=self.write('custom.json',{'mcpServers':{'a':{'command':'node'}}})
        x=self.scanner(extra=[Source('cursor',p,'custom')]).scan()
        self.assertEqual(x['entries'][0]['scope'],'custom')

    def test_invalid_entry_does_not_stop_other_entries(self):
        self.write('.cursor/mcp.json',{'mcpServers':{'bad':{'command':'node','args':'wrong'},'ok':{'command':'node'}}})
        x=self.scanner().scan()
        self.assertEqual(x['stats']['definitions'],1)
        self.assertEqual(len(x['errors']),1)

    def test_unknown_fields_counted_not_copied(self):
        self.write('.cursor/mcp.json',{'mcpServers':{'a':{'command':'node','futureExtension':{'sensitive':'HIDDEN_EXTRA'}}}})
        x=self.scanner().scan()
        self.assertEqual(x['entries'][0]['extraFieldCount'],1)
        self.assertNotIn('HIDDEN_EXTRA',json.dumps(x))

    def test_disabled_override_not_execution_candidate(self):
        self.write('.kiro/settings/mcp.json',{'mcpServers':{'a':{'disabled':True}}})
        e=self.scanner().scan()['entries'][0]
        self.assertEqual(e['configuredState'],'disabled')
        self.assertNotEqual(e['severity'],'high')

    def test_does_not_execute(self):
        self.write('.cursor/mcp.json',{'mcpServers':{'a':{'command':'bash','args':['-c','touch /should-not-exist']}}})
        with patch('subprocess.Popen',side_effect=AssertionError('must not execute')):
            self.assertEqual(self.scanner().scan()['stats']['definitions'],1)

    def test_ref_not_read_or_expanded(self):
        self.write('.cursor/mcp.json',{'mcpServers':{'a':{'command':'node','env':{'X':'${file:~/NEVER_READ}'}}}})
        x=self.scanner().scan()
        self.assertIn('unresolved-variables',[i['code'] for i in x['entries'][0]['issues']])
        self.assertFalse((self.home/'NEVER_READ').exists())

    def test_source_file_unchanged(self):
        p=self.write('.cursor/mcp.json',{'mcpServers':{'a':{'command':'node'}}})
        before=p.read_bytes();self.scanner().scan();self.assertEqual(before,p.read_bytes())

    def test_plan_cannot_apply_and_preserves_source_credentials(self):
        self.write('.cursor/mcp.json',{'mcpServers':{'a':{'command':'node'}}})
        p=plan(self.scanner().scan())
        self.assertFalse(p['applicable'])
        self.assertEqual(p['items'][0]['credentialAction'],'keep-in-source')
        self.assertEqual(p['items'][0]['proposedSharing'],'exclusive')

    def test_keyed_identity_stable_only_within_process(self):
        self.write('.cursor/mcp.json',{'mcpServers':{'a':{'command':'node'}}})
        scanner=self.scanner();a=scanner.scan()['entries'][0]['candidateKey']
        self.assertEqual(a,scanner.scan()['entries'][0]['candidateKey'])
        self.assertNotEqual(a,self.scanner().scan()['entries'][0]['candidateKey'])

    def test_oversized_file_skipped(self):
        p=self.home/'.claude.json';p.write_bytes(b' '*(MAX_FILE+1))
        x=self.scanner().scan();self.assertEqual(len(x['errors']),1)
        self.assertEqual(x['stats']['definitions'],0)

    @unittest.skipUnless(hasattr(os,'symlink'),'symlink support required')
    def test_symlink_skipped(self):
        p=self.write('real.json',{})
        try: (self.home/'.claude.json').symlink_to(p)
        except OSError: self.skipTest('symlink privileges unavailable')
        self.assertEqual(len(self.scanner().scan()['errors']),1)

    @unittest.skipUnless(hasattr(os,'mkfifo'),'POSIX only')
    def test_fifo_not_opened(self):
        p=self.home/'.claude.json';os.mkfifo(p)
        with self.assertRaises(ConfigError):read_bounded(p)

    def test_resource_limits(self):
        self.write('.claude.json',{'mcpServers':{str(i):{'command':'node'} for i in range(1026)}})
        x=self.scanner().scan()
        self.assertEqual(len(x['entries']),1024)
        self.assertTrue(x['errors'])

    def test_workspace_limit(self):
        with self.assertRaises(ValueError): self.scanner(workspaces=[self.home/str(i) for i in range(17)])

    def test_demo_has_clear_provenance(self):
        temp,scanner=demo_discovery()
        try:
            x=scanner.scan();self.assertEqual(x['mode'],'demo');self.assertEqual(x['stats']['definitions'],12)
        finally:temp.cleanup()

if __name__=='__main__': unittest.main()
