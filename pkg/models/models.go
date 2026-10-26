package models

import "time"

// Project represents a tracked git repository folder
type Project struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Path       string    `json:"path"`
	LastOpened time.Time `json:"lastOpened"`
	AddedAt    time.Time `json:"addedAt"`
	IsGit      bool      `json:"isGit"`
}

// Config stores global configuration and list of projects
type Config struct {
	Projects       []Project `json:"projects"`
	ActivePath     string    `json:"activePath"`
	ThemePreference string   `json:"themePreference"` // "system", "dark", "light"
}

// CommitSummary represents commit info in log/timeline
type CommitSummary struct {
	Hash           string    `json:"hash"`
	ShortHash      string    `json:"shortHash"`
	ParentHashes   []string  `json:"parentHashes"`
	IsMerge        bool      `json:"isMerge"`
	AuthorName     string    `json:"authorName"`
	AuthorEmail    string    `json:"authorEmail"`
	Date           time.Time `json:"date"`
	Message        string    `json:"message"`
	IsAI           bool      `json:"isAI"`
	AITag          string    `json:"aiTag,omitempty"` // e.g. "Copilot", "Cursor", "Claude", etc.
	FilesChanged   int       `json:"filesChanged"`
	Additions      int       `json:"additions"`
	Deletions      int       `json:"deletions"`
	Branches       []string  `json:"branches,omitempty"`
}

// DiffLine represents a line in unified/split diff
type DiffLine struct {
	Type       string `json:"type"` // "add", "del", "context", "hunk"
	OldLineNo  int    `json:"oldLineNo,omitempty"`
	NewLineNo  int    `json:"newLineNo,omitempty"`
	Content    string `json:"content"`
}

// DiffHunk represents a diff hunk (@@ ... @@)
type DiffHunk struct {
	Header string     `json:"header"`
	Lines  []DiffLine `json:"lines"`
}

// DiffFile represents changed file in a commit
type DiffFile struct {
	OldPath   string     `json:"oldPath"`
	NewPath   string     `json:"newPath"`
	Status    string     `json:"status"` // "modified", "added", "deleted", "renamed"
	Additions int        `json:"additions"`
	Deletions int        `json:"deletions"`
	Hunks     []DiffHunk `json:"hunks"`
	IsBinary  bool       `json:"isBinary"`
}

// CommitDetail contains full commit data with parsed diffs
type CommitDetail struct {
	CommitSummary
	FullMessage   string          `json:"fullMessage"`
	Files         []DiffFile      `json:"files"`
	MergedCommits []CommitSummary `json:"mergedCommits,omitempty"`
}

// SubRepo represents a discovered root or nested git repository
type SubRepo struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	RelPath       string `json:"relPath"`
	IsRoot        bool   `json:"isRoot"`
	CurrentBranch string `json:"currentBranch"`
}

// TimeSliceQuery represents filtering parameters for updates
type TimeSliceQuery struct {
	Period    string    `json:"period"` // "1h", "24h", "today", "3d", "7d", "30d", "custom", "all"
	Since     time.Time `json:"since"`
	Until     time.Time `json:"until"`
	Author    string    `json:"author"`
	FilterAI  string    `json:"filterAI"` // "all", "ai_only", "human_only"
	Branch    string    `json:"branch"`
	Limit     int       `json:"limit"`
}

// AuthorStat represents statistics for a single author/AI
type AuthorStat struct {
	Name        string `json:"name"`
	Email       string `json:"email"`
	IsAI        bool   `json:"isAI"`
	AITag       string `json:"aiTag,omitempty"`
	CommitCount int    `json:"commitCount"`
	MergeCount  int    `json:"mergeCount"`
	Additions   int    `json:"additions"`
	Deletions   int    `json:"deletions"`
}

// UpdatesResult represents the time-slice updates response
type UpdatesResult struct {
	Commits     []CommitSummary `json:"commits"`
	TotalCommits int            `json:"totalCommits"`
	TotalMerges  int            `json:"totalMerges"`
	TotalAdditions int          `json:"totalAdditions"`
	TotalDeletions int          `json:"totalDeletions"`
	AICommitsCount int          `json:"aiCommitsCount"`
	HumanCommitsCount int       `json:"humanCommitsCount"`
	Authors      []AuthorStat   `json:"authors"`
	Branch       string         `json:"branch"`
	PeriodLabel  string         `json:"periodLabel"`
}

// BranchInfo represents a git branch
type BranchInfo struct {
	Name      string `json:"name"`
	IsCurrent bool   `json:"isCurrent"`
	IsRemote  bool   `json:"isRemote"`
}
