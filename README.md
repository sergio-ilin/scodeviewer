# sCodeViewer

> **Fast, native, minimalist Git code reviewer and change tracker written in Go.**  
> Rendered directly via hardware acceleration (Direct3D). No browser, no web engine, zero heavy dependencies.

---

## Features

- **100% Native Desktop Application**:
  - Pure Go with hardware-accelerated 2D rendering (Direct3D 11).
  - No Chromium, no Electron, no WebView, no background console flashes.
  - Instant startup (<20ms) and minimal RAM usage (~20–40 MB).
  - Standalone binary size under 9 MB.

- **Commits & Merges Code Review**:
  - **Separated views**: quick filters for `All`, `Merges Only`, `Direct`, and `AI`.
  - **Detailed Merge inspection**: view the exact list of commits introduced by each merge commit, with individual author, message, and line stats.
  - High-performance unified diff viewer with line numbers and syntax contrast.
  - Search by commit message, author, or hash.

- **Branches & Nested Projects**:
  - **Branch switcher**: inspect and switch between all project branches.
  - **Nested repository discovery**: auto-scans project folders for nested Git repositories (submodules, microservices, monorepos) and lets you switch between them in 1 click.

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
