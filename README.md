# sCodeViewer

> **Fast, native, minimalist Git code reviewer and change tracker written in Go.**  
> Rendered directly via hardware acceleration (Direct3D). No browser, no web engine, zero heavy dependencies.

---

## Features

- **100% Native Desktop Application**:
  - Pure Go with hardware-accelerated 2D rendering (Direct3D 11).
  - No Chromium, no Electron, no WebView, no background console flashes.
  - Instant startup (<20ms) and minimal RAM usage (~25–45 MB).
  - Standalone binary size under 9.5 MB.

- **Persistent Workspaces & Projects**:
  - Multiple added projects/workspaces saved in persistent storage.
  - Project search & filtering.
  - Ability to rename/alias projects directly in the UI.
  - Auto-discovery and navigation of nested repositories (submodules, microservices).

- **Rich Branch Navigation**:
  - Searchable branch list with real-time filtering.
  - Relative update times (`2h ago`, `3d ago`) and last commit authors for every branch.
  - Instant 1-click branch switching.

- **Uncommitted Changes (Working Tree)**:
  - Working tree changes (modified, added, unstaged, untracked) appear at the top of the commits list.
  - Immediate diff review of uncommitted work before committing.

- **Commits & Merges Code Review**:
  - **Separated views**: quick filters with exact counts: `All (N)`, `Merges (M)`, `Direct (D)`, `AI (K)`.
  - **Detailed Merge inspection**: inspect the exact list of commits introduced by each merge commit, with individual author, message, and line stats.
  - High-performance unified diff viewer with line numbers and syntax contrast.
  - Search by commit message, author, or hash.

- **Updates & Activity Timeline**:
  - Time slices: `1h`, `24h`, `Today`, `3d`, `7d`, `30d`, `All`.
  - Automatic detection of AI-assisted commits (`Claude`, `Cursor`, `Copilot`, `Aider`, `Devin`, `Windsurf`, etc.).
  - Activity breakdown: AI vs Developer commits, net lines added/deleted.
  - Timeline feed with instant jump to diff.

- **Minimalist Design & Themes**:
  - Automatic system dark/light theme detection from Windows registry.
  - Clean, distraction-free interface without emoji clutter or unnecessary buttons.

---

## Installation & Running

### Run directly:
```bash
./scodeviewer.exe
```
Or open a specific repository:
```bash
./scodeviewer.exe -path "C:\path\to\repo"
```

### Build from source:
Requires Go 1.22+:
```bash
go build -ldflags="-H windowsgui -s -w" -o scodeviewer.exe .
```

---

## License
MIT
