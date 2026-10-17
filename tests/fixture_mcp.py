"""Harmless local JSON-RPC test server. No network, filesystem tools, or commands."""
import json
import os
import sys
for line in sys.stdin:
    obj=json.loads(line)
    if 'id' not in obj:continue
    method=obj.get('method')
    if method=='initialize':result={'protocolVersion':'2025-11-25','capabilities':{'tools':{}},'serverInfo':{'name':'mcpcell-fixture','version':'1'}}
    elif method=='tools/list':result={'tools':[{'name':'echo','description':'Echo test input','inputSchema':{'type':'object'}}]}
    elif method=='tools/call':result={'content':[{'type':'text','text':json.dumps(obj.get('params',{}).get('arguments',{}))}],'isError':False}
    elif method=='env':result={'present':os.environ.get('FIXTURE_TOKEN'),'unexpected':os.environ.get('UNRELATED_SECRET')}
    else:result={}
    print(json.dumps({'jsonrpc':'2.0','id':obj['id'],'result':result}),flush=True)
