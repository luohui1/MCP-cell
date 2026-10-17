"""Open a compact browser-app window, without installing another browser engine."""
import os
from pathlib import Path
import shutil
import subprocess
import webbrowser

def open_window(url):
    programs=[]
    if os.name=='nt':
        for base in ['PROGRAMFILES(X86)','PROGRAMFILES','LOCALAPPDATA']:
            root=Path(os.environ.get(base,''))
            programs += [root/'Microsoft/Edge/Application/msedge.exe',root/'Google/Chrome/Application/chrome.exe']
    else:
        programs += [Path(p) for n in ['google-chrome','chromium','chromium-browser'] if (p:=shutil.which(n))]
    for p in programs:
        if p.is_file():
            try:
                subprocess.Popen([str(p),f'--app={url}','--window-size=1000,710'],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
                return
            except OSError:pass
    webbrowser.open(url)
