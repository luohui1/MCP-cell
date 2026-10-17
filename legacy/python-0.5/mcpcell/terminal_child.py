"""POSIX exec trampoline: controlling TTY without preexec_fn in threaded code."""
import fcntl
import os
import sys
import termios

# Popen starts a new session before this script runs. No shell or eval.
fcntl.ioctl(0, termios.TIOCSCTTY, 0)
os.execvpe(sys.argv[1], sys.argv[1:], os.environ)
