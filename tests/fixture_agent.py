"""Harmless interactive fixture. Never invokes a model or executes input."""
import json
import os
import signal
import sys

print('FIXTURE_READY ' + str(os.isatty(0)) + ' ' + os.getcwd(), flush=True)
if hasattr(signal, 'SIGWINCH'):
    signal.signal(signal.SIGWINCH, lambda *_: print('RESIZED', flush=True))
for line in sys.stdin:
    line=line.rstrip('\r\n')
    if line=='quit':break
    if line=='environment':
        print(json.dumps({k:os.environ.get(k) for k in ['HOME','TERM','OPENAI_API_KEY','ANTHROPIC_API_KEY','SECRET_SENTINEL']}),flush=True)
    else:print('REPLY '+line,flush=True)
print('FIXTURE_EXIT',flush=True)
