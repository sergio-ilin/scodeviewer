package git

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"scodeviewer/pkg/models"
)

const sep = "\x1f"
const commitDelim = "@@__COMMIT_START__@@"

// FindGitRepositories scans rootPath and subdirectories for git repositories
func (s *Service) FindGitRepositories(rootPath string) ([]models.SubRepo, error) {
	var repos []models.SubRepo
	cleanRoot := filepath.Clean(rootPath)

	// Check root
	if s.IsGitRepo(cleanRoot) {
		branch, _ := s.GetCurrentBranch(cleanRoot)
		repos = append(repos, models.SubRepo{
			Name:          filepath.Base(cleanRoot) + " (root)",
			Path:          cleanRoot,
			RelPath:       ".",
			IsRoot:        true,
			CurrentBranch: branch,
		})
	}

	// Walk up to 4 levels deep to find nested repos (submodules, microservices, etc.)
	_ = filepath.WalkDir(cleanRoot, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if p == cleanRoot {
			return nil
		}

		name := d.Name()
		// Skip known non-repo vendor/cache directories
		if name == ".git" || name == "node_modules" || name == "vendor" || name == ".idea" ||
			name == ".vscode" || name == "dist" || name == "bin" || name == "obj" ||
			name == ".next" || name == "target" || name == ".venv" {
			return filepath.SkipDir
		}

		// Calculate depth relative to cleanRoot
		rel, errRel := filepath.Rel(cleanRoot, p)
		if errRel != nil {
			return nil
		}
		depth := strings.Count(rel, string(filepath.Separator))
		if depth > 4 {
			return filepath.SkipDir
		}

		// Check if this subdirectory is a git repo
		gitDir := filepath.Join(p, ".git")
		if _, statErr := os.Stat(gitDir); statErr == nil {
			branch, _ := s.GetCurrentBranch(p)
			repos = append(repos, models.SubRepo{
				Name:          filepath.Base(p),
				Path:          p,
				RelPath:       filepath.ToSlash(rel),
				IsRoot:        false,
				CurrentBranch: branch,
			})
			return filepath.SkipDir // Don't look inside nested .git
		}

		return nil
	})

	return repos, nil
}

// Service wraps git CLI interactions
type Service struct{}

func NewService() *Service {
	return &Service{}
}

func hideWindowAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}

func (s *Service) runGit(ctx context.Context, repoPath string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoPath
	cmd.SysProcAttr = hideWindowAttr()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("git error: %v, stderr: %s", err, stderr.String())
	}
	return stdout.String(), nil
}

// IsGitRepo checks if given path is a valid git repository
func (s *Service) IsGitRepo(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = path
	cmd.SysProcAttr = hideWindowAttr()
	err := cmd.Run()
	return err == nil
}

// GetBranches returns list of local and remote branches with last update and author
func (s *Service) GetBranches(repoPath string) ([]models.BranchInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	currentBranch, _ := s.GetCurrentBranch(repoPath)

	out, err := s.runGit(ctx, repoPath, "for-each-ref",
		`--format=%(refname:short)|%(committerdate:relative)|%(authorname)`,
		"--sort=-committerdate",
		"refs/heads/", "refs/remotes/",
	)
	if err != nil {
		// Fallback to basic branch -a
		out, err = s.runGit(ctx, repoPath, "branch", "-a", "--no-color")
		if err != nil {
			return nil, err
		}
	}

	var branches []models.BranchInfo
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.Contains(line, "->") || strings.HasSuffix(line, "/HEAD") {
			continue
		}

		parts := strings.Split(line, "|")
		name := strings.TrimSpace(parts[0])
		name = strings.TrimPrefix(name, "* ")
		name = strings.TrimPrefix(name, "remotes/")

		if seen[name] {
			continue
		}
		seen[name] = true

		isRemote := strings.HasPrefix(name, "origin/")
		isCurrent := name == currentBranch

		lastUpdate := ""
		lastAuthor := ""
		if len(parts) >= 3 {
			lastUpdate = strings.TrimSpace(parts[1])
			lastAuthor = strings.TrimSpace(parts[2])
		}

		branches = append(branches, models.BranchInfo{
			Name:       name,
			IsCurrent:  isCurrent,
			IsRemote:   isRemote,
			LastUpdate: lastUpdate,
			LastAuthor: lastAuthor,
		})
	}
	return branches, nil
}

// GetProjectStatus inspects repoPath to determine if it has uncommitted changes and recent commits (within 30 days)
func (s *Service) GetProjectStatus(repoPath string) (hasUncommitted bool, hasRecent30d bool, lastDate time.Time, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	// Check uncommitted changes
	statusOut, sErr := s.runGit(ctx, repoPath, "status", "--porcelain")
	if sErr == nil && strings.TrimSpace(statusOut) != "" {
		hasUncommitted = true
	}

	// Check last commit date
	logOut, lErr := s.runGit(ctx, repoPath, "log", "-1", "--format=%ct")
	if lErr == nil && strings.TrimSpace(logOut) != "" {
		epoch, pErr := strconv.ParseInt(strings.TrimSpace(logOut), 10, 64)
		if pErr == nil && epoch > 0 {
			lastDate = time.Unix(epoch, 0)
			if time.Since(lastDate) < 30*24*time.Hour {
				hasRecent30d = true
			}
		}
	}

	return hasUncommitted, hasRecent30d, lastDate, nil
}

// GetUncommittedChanges checks working directory for unstaged/staged changes
func (s *Service) GetUncommittedChanges(repoPath string) (*models.CommitDetail, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	statusOut, err := s.runGit(ctx, repoPath, "status", "--porcelain")
	if err != nil || strings.TrimSpace(statusOut) == "" {
		return nil, false
	}

	diffOut, err := s.runGit(ctx, repoPath, "diff", "HEAD", "--unified=3")
	if err != nil {
		diffOut, _ = s.runGit(ctx, repoPath, "diff", "--unified=3")
	}

	files := ParseUnifiedDiff(diffOut)
	adds, dels := DiffFileSummary(files)

	// Also check for untracked files
	scanner := bufio.NewScanner(strings.NewReader(statusOut))
	existingFiles := make(map[string]bool)
	for _, f := range files {
		existingFiles[f.NewPath] = true
		existingFiles[f.OldPath] = true
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "?? ") {
			filePath := strings.TrimPrefix(line, "?? ")
			if !existingFiles[filePath] {
				files = append(files, models.DiffFile{
					NewPath: filePath,
					OldPath: filePath,
					Status:  "untracked",
				})
			}
		}
	}

	return &models.CommitDetail{
		CommitSummary: models.CommitSummary{
			Hash:         "UNCOMMITTED",
			ShortHash:    "WORKING",
			AuthorName:   "Working Directory",
			Date:         time.Now(),
			Message:      "Uncommitted Changes",
			FilesChanged: len(files),
			Additions:    adds,
			Deletions:    dels,
		},
		FullMessage: "Uncommitted changes in working tree",
		Files:       files,
	}, true
}

// GetCurrentBranch returns active branch name
func (s *Service) GetCurrentBranch(repoPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, err := s.runGit(ctx, repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// GetCommits retrieves list of commits with numstat summary
func (s *Service) GetCommits(repoPath string, branch string, author string, filterAI string, isMergeOnly bool, limit int, offset int) ([]models.CommitSummary, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	args := []string{
		"log",
		fmt.Sprintf("--format=%s%%H%s%%h%s%%P%s%%an%s%%ae%s%%at%s%%s", commitDelim, sep, sep, sep, sep, sep, sep),
		"--numstat",
	}

	if isMergeOnly {
		args = append(args, "--merges")
	}

	if author != "" {
		args = append(args, fmt.Sprintf("--author=%s", author))
	}

	fetchLimit := limit + offset
	args = append(args, fmt.Sprintf("-n%d", fetchLimit))

	if branch != "" {
		args = append(args, branch)
	} else {
		args = append(args, "HEAD")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := s.runGit(ctx, repoPath, args...)
	if err != nil {
		return nil, err
	}

	allCommits := parseCommitsWithNumstat(out)

	// Filter AI if requested
	filtered := make([]models.CommitSummary, 0, len(allCommits))
	for _, c := range allCommits {
		if filterAI == "ai_only" && !c.IsAI {
			continue
		}
		if filterAI == "human_only" && c.IsAI {
			continue
		}
		filtered = append(filtered, c)
	}

	if offset >= len(filtered) {
		return []models.CommitSummary{}, nil
	}

	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}

	return filtered[offset:end], nil
}

// GetCommitDetail retrieves detailed info and diffs for a specific commit
func (s *Service) GetCommitDetail(repoPath string, hash string) (*models.CommitDetail, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1. Fetch metadata & full message
	metaOut, err := s.runGit(ctx, repoPath, "show", "-s",
		fmt.Sprintf("--format=%%H%s%%h%s%%P%s%%an%s%%ae%s%%at%s%%B", sep, sep, sep, sep, sep, sep),
		hash,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get commit metadata: %w", err)
	}

	parts := strings.Split(metaOut, sep)
	if len(parts) < 7 {
		return nil, fmt.Errorf("malformed commit metadata output")
	}

	h := parts[0]
	sh := parts[1]
	parentsRaw := strings.TrimSpace(parts[2])
	var parents []string
	if parentsRaw != "" {
		parents = strings.Fields(parentsRaw)
	}
	an := parts[3]
	ae := parts[4]
	ts, _ := strconv.ParseInt(strings.TrimSpace(parts[5]), 10, 64)
	body := strings.TrimRight(parts[6], "\r\n")

	title := FormatCommitTitle(body)
	isAI, aiTag := DetectAI(an, ae, body)

	// 2. Fetch patch diff
	diffOut, err := s.runGit(ctx, repoPath, "show", "--format=", "--patch", "-m", "--first-parent", "--unified=3", hash)
	if err != nil {
		// try regular show if -m first parent fails (e.g. root commit)
		diffOut, err = s.runGit(ctx, repoPath, "show", "--format=", "--patch", "--unified=3", hash)
		if err != nil {
			return nil, fmt.Errorf("failed to get commit diff: %w", err)
		}
	}

	files := ParseUnifiedDiff(diffOut)
	adds, dels := DiffFileSummary(files)

	var mergedCommits []models.CommitSummary
	if len(parents) > 1 {
		// Log the commits merged by this merge commit (parent[0]..hash)
		rangeSpec := fmt.Sprintf("%s..%s", parents[0], hash)
		logArgs := []string{
			"log",
			fmt.Sprintf("--format=%s%%H%s%%h%s%%P%s%%an%s%%ae%s%%at%s%%s", commitDelim, sep, sep, sep, sep, sep, sep),
			"--numstat",
			"-n50",
			rangeSpec,
		}
		mergedOut, errLog := s.runGit(ctx, repoPath, logArgs...)
		if errLog == nil {
			mergedCommits = parseCommitsWithNumstat(mergedOut)
		}
	}

	return &models.CommitDetail{
		CommitSummary: models.CommitSummary{
			Hash:         h,
			ShortHash:    sh,
			ParentHashes: parents,
			IsMerge:      len(parents) > 1,
			AuthorName:   an,
			AuthorEmail:  ae,
			Date:         time.Unix(ts, 0),
			Message:      title,
			IsAI:         isAI,
			AITag:        aiTag,
			FilesChanged: len(files),
			Additions:    adds,
			Deletions:    dels,
		},
		FullMessage:   body,
		Files:         files,
		MergedCommits: mergedCommits,
	}, nil
}

// GetUpdates analyzes commits and changes in a time slice
func (s *Service) GetUpdates(repoPath string, query models.TimeSliceQuery) (*models.UpdatesResult, error) {
	args := []string{
		"log",
		fmt.Sprintf("--format=%s%%H%s%%h%s%%P%s%%an%s%%ae%s%%at%s%%s", commitDelim, sep, sep, sep, sep, sep, sep),
		"--numstat",
	}

	periodLabel := "Все время"
	now := time.Now()

	switch query.Period {
	case "1h":
		args = append(args, "--since=1 hour ago")
		periodLabel = "Последний 1 час"
	case "24h":
		args = append(args, "--since=24 hours ago")
		periodLabel = "Последние 24 часа"
	case "today":
		args = append(args, "--since=midnight")
		periodLabel = "Сегодня"
	case "3d":
		args = append(args, "--since=3 days ago")
		periodLabel = "Последние 3 дня"
	case "7d":
		args = append(args, "--since=7 days ago")
		periodLabel = "Последняя неделя"
	case "30d":
		args = append(args, "--since=30 days ago")
		periodLabel = "Последние 30 дней"
	case "custom":
		if !query.Since.IsZero() {
			args = append(args, fmt.Sprintf("--since=%s", query.Since.Format(time.RFC3339)))
		}
		if !query.Until.IsZero() {
			args = append(args, fmt.Sprintf("--until=%s", query.Until.Format(time.RFC3339)))
		}
		periodLabel = fmt.Sprintf("%s — %s", query.Since.Format("02.01.2006"), query.Until.Format("02.01.2006"))
	default:
		// Default to 7 days if not specified
		if query.Period == "" {
			args = append(args, "--since=7 days ago")
			periodLabel = "Последняя неделя"
		}
	}

	if query.Author != "" {
		args = append(args, fmt.Sprintf("--author=%s", query.Author))
	}

	if query.Branch != "" {
		args = append(args, query.Branch)
	} else {
		args = append(args, "HEAD")
	}

	// Limit to reasonable number for fast rendering
	limit := query.Limit
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	args = append(args, fmt.Sprintf("-n%d", limit))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	out, err := s.runGit(ctx, repoPath, args...)
	if err != nil {
		return nil, err
	}

	commits := parseCommitsWithNumstat(out)

	var filteredCommits []models.CommitSummary
	authorsMap := make(map[string]*models.AuthorStat)
	totalAdds := 0
	totalDels := 0
	totalMerges := 0
	aiCount := 0
	humanCount := 0

	for _, c := range commits {
		if c.IsAI {
			aiCount++
		} else {
			humanCount++
		}

		if c.IsMerge {
			totalMerges++
		}
		totalAdds += c.Additions
		totalDels += c.Deletions

		// Group author stats
		key := strings.ToLower(c.AuthorEmail)
		if key == "" {
			key = strings.ToLower(c.AuthorName)
		}

		stat, exists := authorsMap[key]
		if !exists {
			stat = &models.AuthorStat{
				Name:        c.AuthorName,
				Email:       c.AuthorEmail,
				IsAI:        c.IsAI,
				AITag:       c.AITag,
				CommitCount: 0,
				MergeCount:  0,
				Additions:   0,
				Deletions:   0,
			}
			authorsMap[key] = stat
		}
		stat.CommitCount++
		if c.IsMerge {
			stat.MergeCount++
		}
		stat.Additions += c.Additions
		stat.Deletions += c.Deletions

		// AI filter apply for commits list
		if query.FilterAI == "ai_only" && !c.IsAI {
			continue
		}
		if query.FilterAI == "human_only" && c.IsAI {
			continue
		}

		filteredCommits = append(filteredCommits, c)
	}

	var authors []models.AuthorStat
	for _, a := range authorsMap {
		authors = append(authors, *a)
	}

	currentBranch, _ := s.GetCurrentBranch(repoPath)

	_ = now // used in period calc
	return &models.UpdatesResult{
		Commits:           filteredCommits,
		TotalCommits:      len(commits),
		TotalMerges:       totalMerges,
		TotalAdditions:    totalAdds,
		TotalDeletions:    totalDels,
		AICommitsCount:    aiCount,
		HumanCommitsCount: humanCount,
		Authors:           authors,
		Branch:            currentBranch,
		PeriodLabel:       periodLabel,
	}, nil
}

// parseCommitsWithNumstat parses git log output with custom delimiter and numstat lines
func parseCommitsWithNumstat(raw string) []models.CommitSummary {
	var commits []models.CommitSummary
	if strings.TrimSpace(raw) == "" {
		return commits
	}

	blocks := strings.Split(raw, commitDelim)
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}

		lines := strings.Split(block, "\n")
		if len(lines) == 0 {
			continue
		}

		metaLine := strings.TrimSpace(lines[0])
		parts := strings.Split(metaLine, sep)
		if len(parts) < 7 {
			continue
		}

		h := parts[0]
		sh := parts[1]
		parentsRaw := strings.TrimSpace(parts[2])
		var parents []string
		if parentsRaw != "" {
			parents = strings.Fields(parentsRaw)
		}
		an := parts[3]
		ae := parts[4]
		ts, _ := strconv.ParseInt(strings.TrimSpace(parts[5]), 10, 64)
		msg := parts[6]

		isAI, aiTag := DetectAI(an, ae, msg)

		adds := 0
		dels := 0
		filesCount := 0

		// Parse numstat lines
		for _, numLine := range lines[1:] {
			numLine = strings.TrimSpace(numLine)
			if numLine == "" {
				continue
			}
			col := strings.Fields(numLine)
			if len(col) >= 3 {
				filesCount++
				a, errA := strconv.Atoi(col[0])
				d, errD := strconv.Atoi(col[1])
				if errA == nil {
					adds += a
				}
				if errD == nil {
					dels += d
				}
			}
		}

		commits = append(commits, models.CommitSummary{
			Hash:         h,
			ShortHash:    sh,
			ParentHashes: parents,
			IsMerge:      len(parents) > 1,
			AuthorName:   an,
			AuthorEmail:  ae,
			Date:         time.Unix(ts, 0),
			Message:      msg,
			IsAI:         isAI,
			AITag:        aiTag,
			FilesChanged: filesCount,
			Additions:    adds,
			Deletions:    dels,
		})
	}

	return commits
}
