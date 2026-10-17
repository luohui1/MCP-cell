"""Explicitly prepare local terminal assets from fixed npm versions.

No npm/Node process or package lifecycle scripts are executed. Downloads are
checked against npm's version metadata integrity (registry trust, not publisher
attestation). ZIP deliverables already contain ready-to-use local assets.
"""
import base64
import hashlib
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import urllib.parse
import urllib.request

ROOT=Path(__file__).resolve().parents[1]
REGISTRY='https://registry.npmjs.org/'
PACKAGES=[('@xterm/xterm','5.5.0'),('@xterm/addon-fit','0.10.0')]
MAX_DOWNLOAD=8*1024*1024


def download(url,limit=MAX_DOWNLOAD):
    request=urllib.request.Request(url,headers={'User-Agent':'MCPcell-terminal-build/0.3'})
    with urllib.request.urlopen(request,timeout=30) as response:
        if urllib.parse.urlsplit(response.url).scheme!='https':
            raise ValueError('拒绝非 HTTPS 下载')
        data=response.read(limit+1)
        if len(data)>limit:raise ValueError('依赖包超过下载限额')
        return data


def member(archive,path):
    entry=archive.getmember(path)
    if not entry.isfile() or entry.size>MAX_DOWNLOAD:raise ValueError('无效依赖文件')
    stream=archive.extractfile(entry)
    if stream is None:raise ValueError('依赖文件不可读')
    return stream.read()


def fetch_package(name,version):
    metadata=json.loads(download(REGISTRY+urllib.parse.quote(name,safe='')+'/'+version,512*1024))
    if metadata.get('name')!=name or metadata.get('version')!=version:raise ValueError('包版本不匹配')
    dist=metadata['dist'];url=dist['tarball'];u=urllib.parse.urlsplit(url)
    if u.scheme!='https' or u.hostname!='registry.npmjs.org':raise ValueError('包来源不受支持')
    raw=download(url)
    integrity=dist['integrity'].split()[0]
    algorithm,encoded=integrity.split('-',1)
    if algorithm!='sha512' or hashlib.sha512(raw).digest()!=base64.b64decode(encoded,validate=True):
        raise ValueError('依赖完整性验证失败')
    return tarfile.open(fileobj=io.BytesIO(raw),mode='r:gz'),integrity


def build():
    fetched={};receipts=[]
    try:
        for name,version in PACKAGES:
            archive,integrity=fetch_package(name,version);fetched[name]=archive
            receipts.append({'name':name,'version':version,'integrity':integrity})
        xterm=fetched['@xterm/xterm'];fit=fetched['@xterm/addon-fit']
        js=member(xterm,'package/lib/xterm.js')+b'\n'+member(fit,'package/lib/addon-fit.js')
        js+=b'\nwindow.MCPcellTerminal={Terminal:window.Terminal,FitAddon:window.FitAddon.FitAddon};\n'
        files={'terminal.js':js,'terminal.css':member(xterm,'package/css/xterm.css'),
               'LICENSE-xterm.txt':member(xterm,'package/LICENSE'),
               'LICENSE-fit.txt':member(fit,'package/LICENSE')}
        receipts.append({'files':{name:hashlib.sha256(data).hexdigest() for name,data in files.items()}})
        files['receipt.json']=json.dumps(receipts,indent=2).encode()
        target=ROOT/'mcpcell/web/vendor';target.mkdir(parents=True,exist_ok=True)
        with tempfile.TemporaryDirectory(prefix='.terminal-build-',dir=target) as directory:
            temp=Path(directory)
            for name,data in files.items():(temp/name).write_bytes(data)
            for name in files:os.replace(temp/name,target/name)
        print('终端资源已就绪。不会在运行时访问 CDN。')
    finally:
        for archive in fetched.values():archive.close()

if __name__=='__main__':
    print('从 npm registry 下载固定版本的 xterm 和 FitAddon，仅提取静态文件。')
    try:build()
    except (OSError,ValueError,KeyError,tarfile.TarError) as exc:
        raise SystemExit('终端资源准备失败（'+type(exc).__name__+'）。未运行包内脚本；请检查网络或使用附带资源的源码 ZIP。') from None
