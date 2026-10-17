# Local terminal assets

Generated assets are not stored in Git. ZIP deliverables include them and their license/provenance notices. For a fresh checkout, explicitly run:

```sh
python scripts/setup_terminal.py
```

This downloads fixed official npm packages, checks registry-provided SHA-512 integrity, extracts only the static terminal files and licenses, and runs no package scripts. No Node service, npm install, Electron, font download, or runtime CDN is required.

The UI refuses to start a new Agent when terminal assets are missing. Existing sessions can still be stopped through the authenticated API.
