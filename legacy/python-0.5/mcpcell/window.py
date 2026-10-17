"""Open the workbench in the user's normal browser; never impose window size."""
import webbrowser


def open_window(url: str) -> bool:
    """Reuse the user's browser preference. No app-window or geometry flags."""
    return webbrowser.open(url, new=0, autoraise=True)
