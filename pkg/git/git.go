package git

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"scodeviewer/pkg/models"
)

const sep = "\x1f"
const commitDelim = "@@__COMMIT_START__@@"

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

// GetBranches returns list of local and remote branches
func (s *Service) GetBranches(repoPath string) ([]models.BranchInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	out, err := s.runGit(ctx, repoPath, "branch", "-a", "--no-color")
	if err != nil {
		return nil, err
	}

	var branches []models.BranchInfo
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.Contains(line, "->") {
			continue
		}
		isCurrent := strings.HasPrefix(line, "*")
		name := strings.TrimSpace(strings.TrimPrefix(line, "*"))
		isRemote := strings.HasPrefix(name, "remotes/")

		if isRemote {
			name = strings.TrimPrefix(name, "remotes/")
		}

		branches = append(branches, models.BranchInfo{
			Name:      name,
			IsCurrent: isCurrent,
			IsRemote:  isRemote,
		})
	}
	return branches, nil
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
		FullMessage: body,
		Files:       files,
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
