"""Tiny stdio client bridge. No protocol rewriting, no tool-call replay."""
from __future__ import annotations
import argparse
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import threading
import time

# Isolated Python launch (-I) still imports our package from an explicit location.
ROOT=Path(__file__).resolve().parents[1]
sys.path.insert(0,str(ROOT))
from mcpcell.storage import Vault
from mcpcell.runtime import BASE_ENV


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--state-dir',required=True,type=Path)
    parser.add_argument('--binding',required=True)
    args=parser.parse_args()
    if len(args.binding)!=32 or any(c not in '0123456789abcdef' for c in args.binding):return 1
    vault=Vault(args.state_dir)
    ticket=vault.get('ticket-'+args.binding+'.bin')
    if not ticket:
        print('MCPcell：此接入已被移除，请重启客户端。',file=sys.stderr);return 1
    sock=None;spawned=False
    # Reconnect only before any MCP bytes have been forwarded.
    for _ in range(40):
        run=vault.get('endpoint.bin',{})
        try:
            sock=socket.create_connection(('127.0.0.1',run['brokerPort']),timeout=2)
            break
        except (OSError,KeyError):
            if not spawned:
                kwargs={'stdin':subprocess.DEVNULL,'stdout':subprocess.DEVNULL,'stderr':subprocess.DEVNULL,'cwd':str(ROOT)}
                if os.name=='nt':kwargs['creationflags']=0x08000000
                else:kwargs['start_new_session']=True
                subprocess.Popen([sys.executable,'-I',str(ROOT/'run.py'),'--state-dir',str(args.state_dir),'--home',ticket.get('scanHome',str(Path.home())),'--port','0'],**kwargs)
                spawned=True
            time.sleep(.15)
    if not sock:
        print('MCPcell：管理器不可达；没有绕过接管直接启动工具。',file=sys.stderr);return 1
    allowed=BASE_ENV|set(ticket.get('envNames',[]))
    hello={'bindingId':args.binding,'token':ticket['token'],'cwd':os.getcwd(),
           'env':{k:v for k,v in os.environ.items() if k in allowed}}
    try:
        raw=json.dumps(hello).encode()+b'\n'
        if len(raw)>65536:raise ValueError('environment limit')
        sock.sendall(raw)
        reader=sock.makefile('rb',buffering=0)
        response=reader.readline(1024)
        if json.loads(response).get('ok') is not True:raise ValueError('denied')
        sock.settimeout(None)
        def send():
            try:
                # read1 avoids waiting for a full 64 KiB from interactive stdio.
                while data:=os.read(sys.stdin.fileno(),65536):sock.sendall(data)
            except (OSError,ValueError):pass
            finally:
                try:sock.shutdown(socket.SHUT_WR)
                except OSError:pass
        worker=threading.Thread(target=send,daemon=True);worker.start()
        while data:=reader.read(65536):
            sys.stdout.buffer.write(data);sys.stdout.buffer.flush()
        return 0
    except (OSError,ValueError):
        print('MCPcell：连接已停止或接入未授权。不会重试正在执行的调用。',file=sys.stderr);return 1
    finally:sock.close()

if __name__=='__main__':
    try:raise SystemExit(main())
    except Exception:
        print('MCPcell：无法读取本地接入信息。',file=sys.stderr);raise SystemExit(1)
