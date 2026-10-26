# sCodeViewer

> **Fast, native, minimalist Git code reviewer and change tracker written in Go.**  
> Rendered directly via hardware acceleration (Direct3D). No browser, no web engine, zero heavy dependencies.

---

## Features

- **100% Native Desktop Application**:
  - Pure Go with hardware-accelerated 2D rendering.
  - No Chromium, no Electron, no WebView, no browser popups.
  - Instant startup (<20ms) and minimal RAM usage (~15–30 MB).
  - Standalone binary size under 9 MB.

- **Commits & Merges Code Review**:
  - Fast branch detection and commit inspection.
  - Clear distinction of merge commits with parent hashes.
  - High-performance unified diff viewer with line numbers and syntax contrast.
  - Search by commit message, author, or hash.
  - Quick filters: `All`, `Merges`, `AI`.

- **Updates & Activity Timeline**:
  - Time slices: `1h`, `24h`, `Today`, `3d`, `7d`, `30d`, `All`.
  - Automatic detection of AI-assisted commits (`Claude`, `Cursor`, `Copilot`, `Aider`, `Devin`, `Windsurf`, etc.).
  - Activity breakdown: AI vs Developer commits, net lines added/deleted.
  - Timeline feed with instant jump to diff.

- **Minimalist Design & Themes**:
  - Native Windows system dark/light theme auto-detection.
  - 1-click theme switch: `Auto`, `Dark`, `Light`.
  - Clean, distraction-free interface without emoji clutter.

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
go build -ldflags="-s -w" -o scodeviewer.exe .
```

---

## License
MIT
