package git

import (
	"bufio"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"scodeviewer/pkg/models"
)

var hunkHeaderRegex = regexp.MustCompile(`^@@\s+-(\d+)(?:,(\d+))?\s+\+(\d+)(?:,(\d+))?\s+@@(.*)`)

// ParseUnifiedDiff parses a raw unified git diff string into structured DiffFile slices
func ParseUnifiedDiff(rawDiff string) []models.DiffFile {
	var files []models.DiffFile
	if strings.TrimSpace(rawDiff) == "" {
		return files
	}

	scanner := bufio.NewScanner(strings.NewReader(rawDiff))
	// Allow large lines
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	var currentFile *models.DiffFile
	var currentHunk *models.DiffHunk
	var oldLineNo, newLineNo int

	flushHunk := func() {
		if currentFile != nil && currentHunk != nil {
			currentFile.Hunks = append(currentFile.Hunks, *currentHunk)
			currentHunk = nil
		}
	}

	flushFile := func() {
		flushHunk()
		if currentFile != nil {
			files = append(files, *currentFile)
			currentFile = nil
		}
	}

	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")

		// Diff header start
		if strings.HasPrefix(line, "diff --git ") {
			flushFile()
			parts := strings.Split(line, " ")
			var oldP, newP string
			if len(parts) >= 4 {
				oldP = strings.TrimPrefix(parts[2], "a/")
				newP = strings.TrimPrefix(parts[3], "b/")
			}
			currentFile = &models.DiffFile{
				OldPath: oldP,
				NewPath: newP,
				Status:  "modified",
				Hunks:   make([]models.DiffHunk, 0),
			}
			continue
		}

		if currentFile == nil {
			continue
		}

		// Check statuses
		if strings.HasPrefix(line, "new file mode ") {
			currentFile.Status = "added"
			continue
		}
		if strings.HasPrefix(line, "deleted file mode ") {
			currentFile.Status = "deleted"
			continue
		}
		if strings.HasPrefix(line, "similarity index ") {
			currentFile.Status = "renamed"
			continue
		}
		if strings.HasPrefix(line, "Binary files ") || strings.Contains(line, "GIT binary patch") {
			currentFile.IsBinary = true
			continue
		}

		// Hunk header: @@ -A,B +C,D @@
		if strings.HasPrefix(line, "@@") {
			flushHunk()
			matches := hunkHeaderRegex.FindStringSubmatch(line)
			if len(matches) > 0 {
				oldStart, _ := strconv.Atoi(matches[1])
				newStart, _ := strconv.Atoi(matches[3])
				oldLineNo = oldStart
				newLineNo = newStart
			}
			currentHunk = &models.DiffHunk{
				Header: SanitizeCodeText(line),
				Lines:  make([]models.DiffLine, 0),
			}
			continue
		}

		// Process diff content lines if inside a hunk
		if currentHunk != nil {
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				currentFile.Additions++
				currentHunk.Lines = append(currentHunk.Lines, models.DiffLine{
					Type:      "add",
					NewLineNo: newLineNo,
					Content:   SanitizeCodeText(line[1:]),
				})
				newLineNo++
			} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
				currentFile.Deletions++
				currentHunk.Lines = append(currentHunk.Lines, models.DiffLine{
					Type:      "del",
					OldLineNo: oldLineNo,
					Content:   SanitizeCodeText(line[1:]),
				})
				oldLineNo++
			} else if strings.HasPrefix(line, " ") {
				currentHunk.Lines = append(currentHunk.Lines, models.DiffLine{
					Type:      "context",
					OldLineNo: oldLineNo,
					NewLineNo: newLineNo,
					Content:   SanitizeCodeText(line[1:]),
				})
				oldLineNo++
				newLineNo++
			} else if line == `\ No newline at end of file` {
				// marker line, omit or skip
				continue
			}
		}
	}

	flushFile()

	// Clean up paths for added/deleted
	for i := range files {
		if files[i].Status == "added" && files[i].OldPath == "" {
			files[i].OldPath = files[i].NewPath
		}
		if files[i].Status == "deleted" && files[i].NewPath == "" {
			files[i].NewPath = files[i].OldPath
		}
		if files[i].NewPath == "" && files[i].OldPath != "" {
			files[i].NewPath = files[i].OldPath
		}
		if files[i].OldPath == "" && files[i].NewPath != "" {
			files[i].OldPath = files[i].NewPath
		}
	}

	return files
}

// FormatCommitTitle extracts first line as subject
func FormatCommitTitle(msg string) string {
	lines := strings.Split(strings.TrimSpace(msg), "\n")
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return ""
}

// DiffFileSummary helper
func DiffFileSummary(files []models.DiffFile) (adds, dels int) {
	for _, f := range files {
		adds += f.Additions
		dels += f.Deletions
	}
	return
}

// SanitizeCodeText expands tab characters into 4 spaces and strips carriage returns/control chars
// so that GUI text renderers (e.g. Gio / Direct3D) do not render missing glyph tofu boxes (□).
func SanitizeCodeText(s string) string {
	if !strings.ContainsAny(s, "\t\r\v\f\x00") {
		return s
	}
	var sb strings.Builder
	col := 0
	for _, r := range s {
		switch r {
		case '\t':
			spaces := 4 - (col % 4)
			for i := 0; i < spaces; i++ {
				sb.WriteByte(' ')
			}
			col += spaces
		case '\r', '\v', '\f', 0:
			// Strip carriage return and control chars
		default:
			sb.WriteRune(r)
			col++
		}
	}
	return sb.String()
}

func _unused() {
	_ = fmt.Sprint()
}
