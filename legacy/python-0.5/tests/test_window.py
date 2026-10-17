"""Opening the UI must not force an app window or a screen size."""
import unittest
from unittest.mock import patch
from mcpcell.window import open_window

class WindowTests(unittest.TestCase):
    def test_uses_default_browser_without_subprocess_or_geometry(self):
        with patch('mcpcell.window.webbrowser.open',return_value=True) as browser, \
             patch('subprocess.Popen',side_effect=AssertionError('no forced app window')):
            self.assertTrue(open_window('http://127.0.0.1:1234/'))
            browser.assert_called_once_with('http://127.0.0.1:1234/',new=0,autoraise=True)

    def test_reports_browser_open_failure(self):
        with patch('mcpcell.window.webbrowser.open',return_value=False):
            self.assertFalse(open_window('http://127.0.0.1:1234/'))
