"""Stable entry point; suitable for isolated interpreter launches."""
from pathlib import Path
import sys
sys.path.insert(0,str(Path(__file__).resolve().parent))
from mcpcell.__main__ import main
main()
