package gui

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"scodeviewer/pkg/config"
	"scodeviewer/pkg/git"
	"scodeviewer/pkg/models"
)

type ProjectStatus struct {
	HasUncommitted bool
	HasRecent30d   bool
	LastCommitDate time.Time
}

type App struct {
	window       *app.Window
	theme        *material.Theme
	gitSvc       *git.Service
	cfgMgr       *config.Manager
	rootPath     string
	activeRepo   string
	repoName     string
	activeTab    string // "commits", "updates", "graph3d"
	isDataLoaded bool

	// State
	mu             sync.Mutex
	projects       []models.Project
	subRepos       []models.SubRepo
	branches       []models.BranchInfo
	activeBranch   string
	commits        []models.CommitSummary
	uncommitted    *models.CommitDetail
	selectedHash   string
	selectedDetail *models.CommitDetail
	updatesData    *models.UpdatesResult

	// Filter Counts
	countAll    int
	countMine   int
	countMerges int
	countDirect int
	countAI     int

	// User Profile & Personal Office ("Личный кабинет")
	gitUser               git.GitUser
	showProfileModal      bool
	btnOpenProfile        widget.Clickable
	btnCloseProfile       widget.Clickable
	btnBackdropProfile    widget.Clickable
	btnCardProfile        widget.Clickable
	profileNameEditor     widget.Editor
	profileEmailEditor    widget.Editor
	btnSaveProfileLocal   widget.Clickable
	btnSaveProfileGlobal  widget.Clickable
	btnProfilePush        widget.Clickable
	btnProfilePull        widget.Clickable
	btnProfileFetch       widget.Clickable
	btnProfileStash       widget.Clickable
	btnProfileStashPop    widget.Clickable
	gitOpStatus           string
	gitOpIsError          bool
	gitOpTime             time.Time

	// Commit & Working Copy Actions
	commitMsgEditor        widget.Editor
	commitAmendBool        widget.Bool
	btnDoCommit            widget.Clickable
	btnDoCommitAndPush     widget.Clickable
	btnDoStashUncommitted  widget.Clickable
	btnOpenProfileFromDiff widget.Clickable

	// Search Editors
	searchProjectsEditor widget.Editor
	searchBranchesEditor widget.Editor
	searchCommitsEditor  widget.Editor

	// Project Renaming
	renamingProjID string
	renameEditor   widget.Editor
	btnSaveRename  widget.Clickable
	btnCancelRename widget.Clickable

	// Filters
	commitFilter string // "all", "merges", "direct", "ai"
	timePeriod   string // "1h", "24h", "today", "3d", "7d", "30d", "all"
	aiFilter     string // "all", "ai_only", "human_only"

	// UI Widgets - Left Sidebar
	btnAddProject   widget.Clickable
	btnTabCommits   widget.Clickable
	btnTabUpdates   widget.Clickable
	btnTab3DGraph   widget.Clickable
	graph3dView     *Graph3DView
	graph3dData     *git.Graph3DData
	projStatuses    map[string]ProjectStatus
	projectClicks     []widget.Clickable
	projectRenameBtns []widget.Clickable
	projectDeleteBtns []widget.Clickable
	subRepoClicks     []widget.Clickable
	branchClicks      []widget.Clickable
	branchPinBtns     []widget.Clickable
	projectsList      widget.List
	branchesList      widget.List
	subReposList      widget.List

	// UI Widgets - Middle Panel
	btnFilterAll    widget.Clickable
	btnFilterMine   widget.Clickable
	btnFilterMerges widget.Clickable
	btnFilterDirect widget.Clickable
	btnFilterAI     widget.Clickable
	commitsList     widget.List
	commitClicks    []widget.Clickable
	commitPinBtns   []widget.Clickable
	commitCopyBtns  []widget.Clickable

	// UI Widgets - Right Panel
	icons             *Icons
	selectedFileIdx   int
	btnAllFiles       widget.Clickable
	fileItemClicks    []widget.Clickable
	filesListState    widget.List
	btnPinSelected    widget.Clickable
	btnCopyDetailHash widget.Clickable
	mergedClicks      []widget.Clickable
	mergedCopyBtns    []widget.Clickable
	copiedHash        string
	diffList          widget.List
	searchFilesEditor widget.Editor
	btnCollapseAll    widget.Clickable
	btnExpandAll      widget.Clickable
	collapsedFiles    map[string]bool
	fileHeaderClicks  []widget.Clickable
	fileChipClicks    []widget.Clickable
	fileChipsList     widget.List
	updatesList       widget.List
	authorClicks      []widget.Clickable
	timelineClicks    []widget.Clickable

	// Updates Slice Buttons
	btnSlice1h    widget.Clickable
	btnSlice24h   widget.Clickable
	btnSliceToday widget.Clickable
	btnSlice3d    widget.Clickable
	btnSlice7d    widget.Clickable
	btnSlice30d   widget.Clickable
	btnSliceAll   widget.Clickable

	btnUpFilterAll   widget.Clickable
	btnUpFilterAI    widget.Clickable
	btnUpFilterHuman widget.Clickable
}

func NewApp(initialPath string) *App {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))

	cfgMgr, _ := config.NewManager()
	gitSvc := git.NewService()

	target := initialPath
	var savedProjects []models.Project
	if cfgMgr != nil {
		cfg := cfgMgr.GetConfig()
		savedProjects = cfg.Projects
		if target == "" {
			if cfg.ActiveSubRepo != "" && gitSvc.IsGitRepo(cfg.ActiveSubRepo) {
				target = cfg.ActiveSubRepo
			} else if cfg.ActivePath != "" && gitSvc.IsGitRepo(cfg.ActivePath) {
				target = cfg.ActivePath
			}
		}
	}

	root := target
	if cfgMgr != nil {
		cfg := cfgMgr.GetConfig()
		if cfg.ActivePath != "" && gitSvc.IsGitRepo(cfg.ActivePath) {
			root = cfg.ActivePath
		}
	}

	a := &App{
		theme:         th,
		gitSvc:        gitSvc,
		cfgMgr:        cfgMgr,
		rootPath:      root,
		activeRepo:    target,
		projects:      savedProjects,
		activeTab:     "commits",
		commitFilter:  "all",
		timePeriod:    "7d",
		aiFilter:      "all",
		projectsList:  widget.List{List: layout.List{Axis: layout.Vertical}},
		branchesList:  widget.List{List: layout.List{Axis: layout.Vertical}},
		subReposList:  widget.List{List: layout.List{Axis: layout.Vertical}},
		commitsList:    widget.List{List: layout.List{Axis: layout.Vertical}},
		diffList:       widget.List{List: layout.List{Axis: layout.Vertical}},
		fileChipsList:  widget.List{List: layout.List{Axis: layout.Horizontal}},
		filesListState: widget.List{List: layout.List{Axis: layout.Vertical}},
		updatesList:    widget.List{List: layout.List{Axis: layout.Vertical}},
		collapsedFiles:  make(map[string]bool),
		icons:           NewIcons(),
		selectedFileIdx: -1,
		projStatuses:    make(map[string]ProjectStatus),
	}
	a.graph3dView = NewGraph3DView(a)

	if root != "" {
		a.repoName = filepath.Base(target)
		if cfgMgr != nil {
			cfgMgr.AddProject(root, true)
			if target != root {
				cfgMgr.SetActiveSubRepo(target)
			}
			a.projects = cfgMgr.GetConfig().Projects
		}
	}

	go a.updateProjectStatuses()

	return a
}

func (a *App) Run() error {
	a.window = new(app.Window)
	a.window.Option(
		app.Title("sCodeViewer"),
		app.Size(unit.Dp(1220), unit.Dp(780)),
		app.MinSize(unit.Dp(900), unit.Dp(550)),
	)

	if a.activeRepo != "" && a.gitSvc.IsGitRepo(a.activeRepo) {
		a.loadData()
	}

	var ops op.Ops
	for {
		switch e := a.window.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			if !a.isDataLoaded && a.activeRepo != "" && a.gitSvc.IsGitRepo(a.activeRepo) {
				a.loadData()
			}
			a.handleEvents(gtx)
			a.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (a *App) loadData() {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Load active git user profile
	if a.activeRepo != "" {
		u, _ := a.gitSvc.GetGitUser(a.activeRepo)
		a.gitUser = u
		if a.profileNameEditor.Text() == "" && u.Name != "" {
			a.profileNameEditor.SetText(u.Name)
		}
		if a.profileEmailEditor.Text() == "" && u.Email != "" {
			a.profileEmailEditor.SetText(u.Email)
		}
	}

	// Refresh projects list from config
	if a.cfgMgr != nil {
		a.projects = a.cfgMgr.GetConfig().Projects
		a.projectClicks = make([]widget.Clickable, len(a.projects))
		a.projectRenameBtns = make([]widget.Clickable, len(a.projects))
		a.projectDeleteBtns = make([]widget.Clickable, len(a.projects))
	}

	// Discover nested repos
	if a.rootPath != "" {
		subRepos, err := a.gitSvc.FindGitRepositories(a.rootPath)
		if err == nil && len(subRepos) > 0 {
			a.subRepos = subRepos
			a.subRepoClicks = make([]widget.Clickable, len(subRepos))
		}
	}

	// Load branches with last commit author and time
	branches, _ := a.gitSvc.GetBranches(a.activeRepo)
	var pinnedBranches, pinnedCommits []string
	if a.cfgMgr != nil {
		targetPath := a.rootPath
		if targetPath == "" {
			targetPath = a.activeRepo
		}
		pinnedBranches, pinnedCommits = a.cfgMgr.GetPinned(targetPath)
		if len(pinnedBranches) == 0 && len(pinnedCommits) == 0 && a.activeRepo != targetPath {
			pinnedBranches, pinnedCommits = a.cfgMgr.GetPinned(a.activeRepo)
		}
	}

	pinnedBranchMap := make(map[string]bool)
	for _, b := range pinnedBranches {
		pinnedBranchMap[b] = true
	}

	var pinnedBList, regularBList []models.BranchInfo
	for _, b := range branches {
		if pinnedBranchMap[b.Name] {
			b.IsPinned = true
			pinnedBList = append(pinnedBList, b)
		} else {
			b.IsPinned = false
			regularBList = append(regularBList, b)
		}
	}
	a.branches = append(pinnedBList, regularBList...)
	a.branchClicks = make([]widget.Clickable, len(a.branches))
	a.branchPinBtns = make([]widget.Clickable, len(a.branches))

	curr, _ := a.gitSvc.GetCurrentBranch(a.activeRepo)
	a.activeBranch = curr

	// Check uncommitted changes
	uncommitted, hasUncommitted := a.gitSvc.GetUncommittedChanges(a.activeRepo)
	if hasUncommitted {
		a.uncommitted = uncommitted
	} else {
		a.uncommitted = nil
	}

	a.reloadCommitsLocked()
	a.reloadUpdatesLocked()
	a.isDataLoaded = true

	if a.selectedHash != "" {
		if a.selectedHash == "UNCOMMITTED" && a.uncommitted != nil {
			a.selectedDetail = a.uncommitted
			a.mergedClicks = nil
			a.fileHeaderClicks = make([]widget.Clickable, len(a.uncommitted.Files))
			a.fileChipClicks = make([]widget.Clickable, len(a.uncommitted.Files))
			a.fileItemClicks = make([]widget.Clickable, len(a.uncommitted.Files))
		} else {
			detail, err := a.gitSvc.GetCommitDetail(a.activeRepo, a.selectedHash)
			if err == nil {
				a.selectedDetail = detail
				for _, pc := range pinnedCommits {
					if pc == detail.Hash || pc == detail.ShortHash {
						a.selectedDetail.IsPinned = true
						break
					}
				}
				a.mergedClicks = make([]widget.Clickable, len(detail.MergedCommits))
				a.mergedCopyBtns = make([]widget.Clickable, len(detail.MergedCommits))
				a.fileHeaderClicks = make([]widget.Clickable, len(detail.Files))
				a.fileChipClicks = make([]widget.Clickable, len(detail.Files))
				a.fileItemClicks = make([]widget.Clickable, len(detail.Files))
			}
		}
	}

	go a.updateProjectStatuses()
	if a.activeTab == "graph3d" {
		go a.loadGraph3D()
	}
}

func (a *App) updateProjectStatuses() {
	a.mu.Lock()
	projs := make([]models.Project, len(a.projects))
	copy(projs, a.projects)
	subRepos := make([]models.SubRepo, len(a.subRepos))
	copy(subRepos, a.subRepos)
	a.mu.Unlock()

	pathsToCheck := make(map[string]bool)
	for _, p := range projs {
		if p.Path != "" {
			pathsToCheck[p.Path] = true
		}
	}
	for _, r := range subRepos {
		if r.Path != "" {
			pathsToCheck[r.Path] = true
		}
	}

	for p := range pathsToCheck {
		hasUncommitted, hasRecent, lastDate, err := a.gitSvc.GetProjectStatus(p)
		if err == nil {
			a.mu.Lock()
			if a.projStatuses == nil {
				a.projStatuses = make(map[string]ProjectStatus)
			}
			a.projStatuses[p] = ProjectStatus{
				HasUncommitted: hasUncommitted,
				HasRecent30d:   hasRecent,
				LastCommitDate: lastDate,
			}
			a.mu.Unlock()
		}
	}

	if a.window != nil {
		a.window.Invalidate()
	}
}

func (a *App) loadGraph3D() {
	a.mu.Lock()
	repo := a.activeRepo
	a.mu.Unlock()
	if repo == "" {
		return
	}
	data, err := a.gitSvc.GetGraph3D(repo, 120)
	if err == nil {
		a.mu.Lock()
		a.graph3dData = data
		a.mu.Unlock()
		if a.window != nil {
			a.window.Invalidate()
		}
	}
}

func (a *App) reloadCommitsLocked() {
	// Re-check uncommitted changes
	uncommitted, hasUncommitted := a.gitSvc.GetUncommittedChanges(a.activeRepo)
	if hasUncommitted {
		a.uncommitted = uncommitted
	} else {
		a.uncommitted = nil
	}

	var pinnedCommits []string
	if a.cfgMgr != nil {
		targetPath := a.rootPath
		if targetPath == "" {
			targetPath = a.activeRepo
		}
		_, pinnedCommits = a.cfgMgr.GetPinned(targetPath)
		if len(pinnedCommits) == 0 && a.activeRepo != targetPath {
			_, pinnedCommits = a.cfgMgr.GetPinned(a.activeRepo)
		}
	}
	pinnedCommitMap := make(map[string]bool)
	for _, h := range pinnedCommits {
		pinnedCommitMap[h] = true
	}

	allCommits, err := a.gitSvc.GetCommits(a.activeRepo, a.activeBranch, "", "", false, 250, 0)
	if err == nil {
		a.countAll = len(allCommits)
		if a.uncommitted != nil {
			a.countAll++
		}
		a.countMerges = 0
		a.countDirect = 0
		a.countAI = 0
		a.countMine = 0

		myName := strings.ToLower(strings.TrimSpace(a.gitUser.Name))
		myEmail := strings.ToLower(strings.TrimSpace(a.gitUser.Email))

		var pinnedList, regularList []models.CommitSummary

		for _, c := range allCommits {
			cAuthor := strings.ToLower(c.AuthorName)
			cEmail := strings.ToLower(c.AuthorEmail)
			isMine := false
			if (myName != "" && (cAuthor == myName || strings.Contains(cAuthor, myName))) ||
				(myEmail != "" && (cEmail == myEmail || strings.Contains(cEmail, myEmail))) {
				isMine = true
				a.countMine++
			}

			if c.IsMerge {
				a.countMerges++
			} else {
				a.countDirect++
			}
			if c.IsAI {
				a.countAI++
			}

			if pinnedCommitMap[c.Hash] || pinnedCommitMap[c.ShortHash] {
				c.IsPinned = true
			}

			// Apply active filter
			match := false
			switch a.commitFilter {
			case "mine":
				match = isMine
			case "merges":
				match = c.IsMerge
			case "direct":
				match = !c.IsMerge
			case "ai":
				match = c.IsAI
			default:
				match = true
			}

			if match {
				if c.IsPinned {
					pinnedList = append(pinnedList, c)
				} else {
					regularList = append(regularList, c)
				}
			}
		}

		if a.uncommitted != nil {
			a.countMine++
		}

		var filtered []models.CommitSummary

		// Put uncommitted changes at top if matches filter
		if a.uncommitted != nil && (a.commitFilter == "all" || a.commitFilter == "direct" || a.commitFilter == "mine") {
			filtered = append(filtered, a.uncommitted.CommitSummary)
		}
		// Pinned commits at top right after uncommitted
		filtered = append(filtered, pinnedList...)
		// Regular commits
		filtered = append(filtered, regularList...)

		a.commits = filtered
		a.commitClicks = make([]widget.Clickable, len(filtered))
		a.commitPinBtns = make([]widget.Clickable, len(filtered))
		a.commitCopyBtns = make([]widget.Clickable, len(filtered))

		// Auto select first commit if none or current selected is not in filtered
		foundSelected := false
		for _, c := range filtered {
			if c.Hash == a.selectedHash {
				foundSelected = true
				break
			}
		}

		if (!foundSelected || a.selectedHash == "") && len(filtered) > 0 {
			a.selectedHash = filtered[0].Hash
			if filtered[0].Hash == "UNCOMMITTED" && a.uncommitted != nil {
				a.selectedDetail = a.uncommitted
				a.mergedClicks = nil
				a.fileHeaderClicks = make([]widget.Clickable, len(a.uncommitted.Files))
				a.fileChipClicks = make([]widget.Clickable, len(a.uncommitted.Files))
				a.fileItemClicks = make([]widget.Clickable, len(a.uncommitted.Files))
			} else {
				detail, errDetail := a.gitSvc.GetCommitDetail(a.activeRepo, filtered[0].Hash)
				if errDetail == nil {
					a.selectedDetail = detail
					a.selectedDetail.IsPinned = pinnedCommitMap[detail.Hash] || pinnedCommitMap[detail.ShortHash]
					a.mergedClicks = make([]widget.Clickable, len(detail.MergedCommits))
					a.mergedCopyBtns = make([]widget.Clickable, len(detail.MergedCommits))
					a.fileHeaderClicks = make([]widget.Clickable, len(detail.Files))
					a.fileChipClicks = make([]widget.Clickable, len(detail.Files))
					a.fileItemClicks = make([]widget.Clickable, len(detail.Files))
				}
			}
		} else if len(filtered) == 0 {
			a.selectedHash = ""
			a.selectedDetail = nil
		} else if a.selectedDetail != nil {
			a.selectedDetail.IsPinned = pinnedCommitMap[a.selectedDetail.Hash] || pinnedCommitMap[a.selectedDetail.ShortHash]
		}
	}
}

func (a *App) reloadUpdatesLocked() {
	res, err := a.gitSvc.GetUpdates(a.activeRepo, models.TimeSliceQuery{
		Period:   a.timePeriod,
		FilterAI: a.aiFilter,
		Branch:   a.activeBranch,
		Limit:    100,
	})
	if err == nil {
		a.updatesData = res
		a.authorClicks = make([]widget.Clickable, len(res.Authors))
		a.timelineClicks = make([]widget.Clickable, len(res.Commits))
	}
}

func (a *App) loadCommitDetail(hash string) {
	if hash == "UNCOMMITTED" {
		a.mu.Lock()
		if a.uncommitted != nil {
			a.selectedDetail = a.uncommitted
			a.mergedClicks = nil
			a.fileHeaderClicks = make([]widget.Clickable, len(a.uncommitted.Files))
			a.fileChipClicks = make([]widget.Clickable, len(a.uncommitted.Files))
		}
		a.mu.Unlock()
		if a.window != nil {
			a.window.Invalidate()
		}
		return
	}

	detail, err := a.gitSvc.GetCommitDetail(a.activeRepo, hash)
	if err == nil {
		a.mu.Lock()
		a.selectedDetail = detail
		if a.cfgMgr != nil {
			targetPath := a.rootPath
			if targetPath == "" {
				targetPath = a.activeRepo
			}
			_, pinnedCommits := a.cfgMgr.GetPinned(targetPath)
			if len(pinnedCommits) == 0 && a.activeRepo != targetPath {
				_, pinnedCommits = a.cfgMgr.GetPinned(a.activeRepo)
			}
			for _, pc := range pinnedCommits {
				if pc == detail.Hash || pc == detail.ShortHash {
					a.selectedDetail.IsPinned = true
					break
				}
			}
		}
		a.mergedClicks = make([]widget.Clickable, len(detail.MergedCommits))
		a.mergedCopyBtns = make([]widget.Clickable, len(detail.MergedCommits))
		a.fileHeaderClicks = make([]widget.Clickable, len(detail.Files))
		a.fileChipClicks = make([]widget.Clickable, len(detail.Files))
		a.fileItemClicks = make([]widget.Clickable, len(detail.Files))
		a.selectedFileIdx = -1 // Reset to all files
		a.mu.Unlock()
		if a.window != nil {
			a.window.Invalidate()
		}
	}
}

func (a *App) handleEvents(gtx layout.Context) {
	// Add Project / Open Folder
	if a.btnAddProject.Clicked(gtx) {
		go func() {
			path := openFolderDialog()
			if path != "" && a.gitSvc.IsGitRepo(path) {
				a.mu.Lock()
				a.rootPath = path
				a.activeRepo = path
				a.repoName = filepath.Base(path)
				a.selectedHash = ""
				a.selectedDetail = nil
				if a.cfgMgr != nil {
					a.cfgMgr.AddProject(path, true)
				}
				a.mu.Unlock()
				a.loadData()
				if a.window != nil {
					a.window.Invalidate()
				}
			}
		}()
	}

	// Rename save/cancel
	if a.btnSaveRename.Clicked(gtx) {
		newName := strings.TrimSpace(a.renameEditor.Text())
		if newName != "" && a.renamingProjID != "" && a.cfgMgr != nil {
			a.cfgMgr.RenameProject(a.renamingProjID, newName)
			a.projects = a.cfgMgr.GetConfig().Projects
		}
		a.renamingProjID = ""
	}
	if a.btnCancelRename.Clicked(gtx) {
		a.renamingProjID = ""
	}

	// Tab switcher
	if a.btnTabCommits.Clicked(gtx) {
		a.activeTab = "commits"
	}
	if a.btnTabUpdates.Clicked(gtx) {
		a.activeTab = "updates"
		a.mu.Lock()
		a.reloadUpdatesLocked()
		a.mu.Unlock()
	}
	if a.btnTab3DGraph.Clicked(gtx) {
		a.activeTab = "graph3d"
		go a.loadGraph3D()
	}

	// Switch Projects
	a.mu.Lock()
	for i := range a.projectClicks {
		if i < len(a.projects) && a.projectClicks[i].Clicked(gtx) {
			p := a.projects[i]
			a.rootPath = p.Path
			a.activeRepo = p.Path
			a.repoName = p.Name
			a.selectedHash = ""
			a.selectedDetail = nil
			if a.cfgMgr != nil {
				a.cfgMgr.SetActive(p.Path)
			}
			go func() {
				a.loadData()
				if a.window != nil {
					a.window.Invalidate()
				}
			}()
			break
		}
	}

	// Rename button click on project
	for i := range a.projectRenameBtns {
		if i < len(a.projects) && a.projectRenameBtns[i].Clicked(gtx) {
			p := a.projects[i]
			a.renamingProjID = p.ID
			a.renameEditor.SetText(p.Name)
			break
		}
	}

	// Delete project from list
	for i := range a.projectDeleteBtns {
		if i < len(a.projects) && a.projectDeleteBtns[i].Clicked(gtx) {
			p := a.projects[i]
			if a.cfgMgr != nil {
				a.cfgMgr.RemoveProject(p.ID)
				a.projects = a.cfgMgr.GetConfig().Projects
			}
			break
		}
	}

	// SubRepo clicks
	for i := range a.subRepoClicks {
		if i < len(a.subRepos) && a.subRepoClicks[i].Clicked(gtx) {
			a.activeRepo = a.subRepos[i].Path
			a.repoName = a.subRepos[i].Name
			a.selectedHash = ""
			a.selectedDetail = nil
			if a.cfgMgr != nil {
				a.cfgMgr.SetActiveSubRepo(a.subRepos[i].Path)
			}
			go func() {
				a.loadData()
				if a.window != nil {
					a.window.Invalidate()
				}
			}()
			break
		}
	}

	// Branch clicks
	for i := range a.branchClicks {
		if i < len(a.branches) && a.branchClicks[i].Clicked(gtx) {
			a.activeBranch = a.branches[i].Name
			a.reloadCommitsLocked()
			a.reloadUpdatesLocked()
			break
		}
	}

	// Branch Pin clicks
	for i := range a.branchPinBtns {
		if i < len(a.branches) && a.branchPinBtns[i].Clicked(gtx) {
			bName := a.branches[i].Name
			if a.cfgMgr != nil {
				targetPath := a.rootPath
				if targetPath == "" {
					targetPath = a.activeRepo
				}
				a.cfgMgr.TogglePinBranch(targetPath, bName)
			}
			go func() {
				a.loadData()
				if a.window != nil {
					a.window.Invalidate()
				}
			}()
			break
		}
	}

	// Commit filters
	if a.btnFilterAll.Clicked(gtx) {
		a.commitFilter = "all"
		a.reloadCommitsLocked()
	}
	if a.btnFilterMine.Clicked(gtx) {
		a.commitFilter = "mine"
		a.reloadCommitsLocked()
	}
	if a.btnFilterMerges.Clicked(gtx) {
		a.commitFilter = "merges"
		a.reloadCommitsLocked()
	}
	if a.btnFilterDirect.Clicked(gtx) {
		a.commitFilter = "direct"
		a.reloadCommitsLocked()
	}
	if a.btnFilterAI.Clicked(gtx) {
		a.commitFilter = "ai"
		a.reloadCommitsLocked()
	}

	// Updates slices
	slices := []struct {
		btn    *widget.Clickable
		period string
	}{
		{&a.btnSlice1h, "1h"},
		{&a.btnSlice24h, "24h"},
		{&a.btnSliceToday, "today"},
		{&a.btnSlice3d, "3d"},
		{&a.btnSlice7d, "7d"},
		{&a.btnSlice30d, "30d"},
		{&a.btnSliceAll, "all"},
	}
	for _, s := range slices {
		if s.btn.Clicked(gtx) {
			a.timePeriod = s.period
			a.reloadUpdatesLocked()
		}
	}

	// Updates AI filter
	if a.btnUpFilterAll.Clicked(gtx) {
		a.aiFilter = "all"
		a.reloadUpdatesLocked()
	}
	if a.btnUpFilterAI.Clicked(gtx) {
		a.aiFilter = "ai_only"
		a.reloadUpdatesLocked()
	}
	if a.btnUpFilterHuman.Clicked(gtx) {
		a.aiFilter = "human_only"
		a.reloadUpdatesLocked()
	}

	// Commit clicks
	for i := range a.commitClicks {
		if i < len(a.commits) && a.commitClicks[i].Clicked(gtx) {
			hash := a.commits[i].Hash
			a.selectedHash = hash
			go a.loadCommitDetail(hash)
			break
		}
	}

	// Commit Pin clicks
	for i := range a.commitPinBtns {
		if i < len(a.commits) && a.commitPinBtns[i].Clicked(gtx) {
			c := a.commits[i]
			if c.Hash != "UNCOMMITTED" && a.cfgMgr != nil {
				targetPath := a.rootPath
				if targetPath == "" {
					targetPath = a.activeRepo
				}
				a.cfgMgr.TogglePinCommit(targetPath, c.Hash)
				a.reloadCommitsLocked()
				if a.selectedDetail != nil && a.selectedDetail.Hash == c.Hash {
					a.selectedDetail.IsPinned = !a.selectedDetail.IsPinned
				}
			}
			break
		}
	}

	// Pin Selected button in commit detail header
	if a.btnPinSelected.Clicked(gtx) && a.selectedDetail != nil && a.selectedDetail.Hash != "UNCOMMITTED" {
		targetPath := a.rootPath
		if targetPath == "" {
			targetPath = a.activeRepo
		}
		if a.cfgMgr != nil {
			a.cfgMgr.TogglePinCommit(targetPath, a.selectedDetail.Hash)
			a.selectedDetail.IsPinned = !a.selectedDetail.IsPinned
			a.reloadCommitsLocked()
		}
	}

	// Merged child commits click
	if a.selectedDetail != nil {
		for i := range a.mergedClicks {
			if i < len(a.selectedDetail.MergedCommits) && a.mergedClicks[i].Clicked(gtx) {
				hash := a.selectedDetail.MergedCommits[i].Hash
				a.selectedHash = hash
				go a.loadCommitDetail(hash)
				break
			}
		}
		for i := range a.mergedCopyBtns {
			if i < len(a.selectedDetail.MergedCommits) && a.mergedCopyBtns[i].Clicked(gtx) {
				h := a.selectedDetail.MergedCommits[i].Hash
				gtx.Execute(clipboard.WriteCmd{
					Type: "application/text",
					Data: io.NopCloser(strings.NewReader(h)),
				})
				a.copiedHash = a.selectedDetail.MergedCommits[i].ShortHash
				break
			}
		}
	}

	// Commit hash copy clicks
	for i := range a.commitCopyBtns {
		if i < len(a.commits) && a.commitCopyBtns[i].Clicked(gtx) {
			c := a.commits[i]
			if c.Hash != "UNCOMMITTED" {
				gtx.Execute(clipboard.WriteCmd{
					Type: "application/text",
					Data: io.NopCloser(strings.NewReader(c.Hash)),
				})
				a.copiedHash = c.ShortHash
			}
			break
		}
	}

	// Copy detail commit hash click
	if a.btnCopyDetailHash.Clicked(gtx) && a.selectedDetail != nil && a.selectedDetail.Hash != "UNCOMMITTED" {
		gtx.Execute(clipboard.WriteCmd{
			Type: "application/text",
			Data: io.NopCloser(strings.NewReader(a.selectedDetail.Hash)),
		})
		a.copiedHash = a.selectedDetail.ShortHash
	}

	// All files button in files sidebar
	if a.btnAllFiles.Clicked(gtx) {
		a.selectedFileIdx = -1
	}

	// Individual file selection from files sidebar
	if a.selectedDetail != nil {
		for i := range a.fileItemClicks {
			if i < len(a.selectedDetail.Files) && a.fileItemClicks[i].Clicked(gtx) {
				if a.selectedFileIdx == i {
					a.selectedFileIdx = -1
				} else {
					a.selectedFileIdx = i
					delete(a.collapsedFiles, a.selectedDetail.Files[i].NewPath)
				}
				break
			}
		}
	}

	// Diff file collapse / expand all
	if a.btnCollapseAll.Clicked(gtx) && a.selectedDetail != nil {
		if a.collapsedFiles == nil {
			a.collapsedFiles = make(map[string]bool)
		}
		for _, f := range a.selectedDetail.Files {
			a.collapsedFiles[f.NewPath] = true
		}
	}
	if a.btnExpandAll.Clicked(gtx) && a.selectedDetail != nil {
		a.collapsedFiles = make(map[string]bool)
	}

	// Individual file header clicks & chip clicks
	if a.selectedDetail != nil {
		for i := range a.fileHeaderClicks {
			if i < len(a.selectedDetail.Files) && a.fileHeaderClicks[i].Clicked(gtx) {
				path := a.selectedDetail.Files[i].NewPath
				if a.collapsedFiles == nil {
					a.collapsedFiles = make(map[string]bool)
				}
				a.collapsedFiles[path] = !a.collapsedFiles[path]
				break
			}
		}
		for i := range a.fileChipClicks {
			if i < len(a.selectedDetail.Files) && a.fileChipClicks[i].Clicked(gtx) {
				path := a.selectedDetail.Files[i].NewPath
				if a.collapsedFiles == nil {
					a.collapsedFiles = make(map[string]bool)
				}
				a.collapsedFiles[path] = !a.collapsedFiles[path]
				break
			}
		}
	}

	// Timeline clicks (stay inside updates tab without jumping to commits tab)
	if a.updatesData != nil {
		for i := range a.timelineClicks {
			if i < len(a.updatesData.Commits) && a.timelineClicks[i].Clicked(gtx) {
				hash := a.updatesData.Commits[i].Hash
				a.selectedHash = hash
				go a.loadCommitDetail(hash)
				break
			}
		}
	}
	a.mu.Unlock()

	// Personal Office / Git Profile Modal open & close
	if a.btnOpenProfile.Clicked(gtx) || a.btnOpenProfileFromDiff.Clicked(gtx) {
		a.showProfileModal = !a.showProfileModal
		if a.showProfileModal && a.activeRepo != "" {
			u, _ := a.gitSvc.GetGitUser(a.activeRepo)
			a.mu.Lock()
			a.gitUser = u
			a.profileNameEditor.SetText(u.Name)
			a.profileEmailEditor.SetText(u.Email)
			a.mu.Unlock()
		}
	}
	if a.btnCloseProfile.Clicked(gtx) || a.btnBackdropProfile.Clicked(gtx) {
		a.showProfileModal = false
	}

	// Profile Author Save: Local vs Global
	if a.btnSaveProfileLocal.Clicked(gtx) {
		name := strings.TrimSpace(a.profileNameEditor.Text())
		email := strings.TrimSpace(a.profileEmailEditor.Text())
		if a.activeRepo != "" {
			if err := a.gitSvc.SetGitUser(a.activeRepo, name, email, false); err != nil {
				a.mu.Lock()
				a.gitOpStatus = "Error saving author: " + err.Error()
				a.gitOpIsError = true
				a.gitOpTime = time.Now()
				a.mu.Unlock()
			} else {
				a.mu.Lock()
				a.gitOpStatus = fmt.Sprintf("Author saved for this repo: %s <%s>", name, email)
				a.gitOpIsError = false
				a.gitUser.Name = name
				a.gitUser.Email = email
				a.gitOpTime = time.Now()
				a.reloadCommitsLocked()
				a.mu.Unlock()
			}
		}
	}
	if a.btnSaveProfileGlobal.Clicked(gtx) {
		name := strings.TrimSpace(a.profileNameEditor.Text())
		email := strings.TrimSpace(a.profileEmailEditor.Text())
		if a.activeRepo != "" {
			if err := a.gitSvc.SetGitUser(a.activeRepo, name, email, true); err != nil {
				a.mu.Lock()
				a.gitOpStatus = "Error saving global author: " + err.Error()
				a.gitOpIsError = true
				a.gitOpTime = time.Now()
				a.mu.Unlock()
			} else {
				a.mu.Lock()
				a.gitOpStatus = fmt.Sprintf("Author saved globally: %s <%s>", name, email)
				a.gitOpIsError = false
				a.gitUser.Name = name
				a.gitUser.Email = email
				a.gitOpTime = time.Now()
				a.reloadCommitsLocked()
				a.mu.Unlock()
			}
		}
	}

	// Remote Git Operations in Profile Modal
	if a.btnProfilePush.Clicked(gtx) {
		a.runGitPush()
	}
	if a.btnProfilePull.Clicked(gtx) {
		a.runGitPull()
	}
	if a.btnProfileFetch.Clicked(gtx) {
		a.runGitFetch()
	}
	if a.btnProfileStash.Clicked(gtx) {
		a.runGitStash()
	}
	if a.btnProfileStashPop.Clicked(gtx) {
		a.runGitStashPop()
	}

	// Commit & Push Actions from Uncommitted Changes Box
	if a.btnDoCommit.Clicked(gtx) {
		a.runGitCommit(a.commitMsgEditor.Text(), a.commitAmendBool.Value, false)
	}
	if a.btnDoCommitAndPush.Clicked(gtx) {
		a.runGitCommit(a.commitMsgEditor.Text(), a.commitAmendBool.Value, true)
	}
	if a.btnDoStashUncommitted.Clicked(gtx) {
		a.runGitStash()
	}
}

func (a *App) layout(gtx layout.Context) layout.Dimensions {
	pal := GetPalette(ThemeAuto)

	// Clip & Fill window background
	paint.FillShape(gtx.Ops, pal.Bg, clip.Rect(image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Max.Y)).Op())

	if a.activeRepo == "" {
		return a.layoutEmptyState(gtx, pal)
	}

	var content layout.Dimensions
	if a.activeTab == "graph3d" {
		a.mu.Lock()
		gData := a.graph3dData
		a.mu.Unlock()
		if gData == nil {
			go a.loadGraph3D()
		}
		content = layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			// Panel 1: Left Navigation Sidebar
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Dp(unit.Dp(290))
				gtx.Constraints.Max.X = gtx.Dp(unit.Dp(290))
				return a.layoutLeftSidebar(gtx, pal)
			}),
			// Panel 2: 3D Topology Canvas (Floating Island Card)
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(4), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min = gtx.Constraints.Max
					return drawWithBorder(gtx, pal.Bg, unit.Dp(8), unit.Dp(1), pal.Border, func(gtx layout.Context) layout.Dimensions {
						return a.graph3dView.Layout(gtx, gData, pal)
					})
				})
			}),
		)
	} else {
		// Professional 3-Panel Architecture (Floating Island Cards with Spacious Gaps):
		// Panel 1 (Left 290dp): Persistent Workspaces + Sub-repos + Branches
		// Panel 2 (Middle 380dp): Commits & Merges Feed
		// Panel 3 (Right flexed): Diff Viewer / Merged Commits / Analytics
		content = layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			// Panel 1: Left Navigation Sidebar
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Dp(unit.Dp(290))
				gtx.Constraints.Max.X = gtx.Dp(unit.Dp(290))
				return a.layoutLeftSidebar(gtx, pal)
			}),
			// Panel 2: Middle Panel (Commits List or Updates List)
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Dp(unit.Dp(380))
				gtx.Constraints.Max.X = gtx.Dp(unit.Dp(380))
				return a.layoutCenterPanel(gtx, pal)
			}),
			// Panel 3: Right Panel (Diff / Merge Breakdown / Analytics)
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return a.layoutRightPanel(gtx, pal)
			}),
		)
	}

	if a.showProfileModal {
		a.layoutProfileModalOverlay(gtx, pal)
	}

	return content
}

// -------------------------------------------------------------
// PANEL 1: Left Sidebar (Persistent Projects, Branches with Author & Time)
// -------------------------------------------------------------
func (a *App) layoutLeftSidebar(gtx layout.Context, pal Palette) layout.Dimensions {
	a.mu.Lock()
	projects := a.projects
	subRepos := a.subRepos
	branches := a.branches
	activeRepo := a.activeRepo
	activeBranch := a.activeBranch
	rootPath := a.rootPath
	renamingID := a.renamingProjID
	projStatuses := a.projStatuses
	a.mu.Unlock()

	searchProj := strings.ToLower(strings.TrimSpace(a.searchProjectsEditor.Text()))
	searchBranch := strings.ToLower(strings.TrimSpace(a.searchBranchesEditor.Text()))

	return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(10), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return drawWithBorder(gtx, pal.HeaderBg, unit.Dp(8), unit.Dp(1), pal.Border, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(14), Bottom: unit.Dp(14), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				// Top Bar: Brand + Group 1 (Add Repo) + Group 2 (View Switcher)
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						// App Brand
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Label(a.theme, unit.Sp(13), "sCodeViewer")
							lbl.Color = pal.Text
							lbl.Font.Weight = font.Bold
							return lbl.Layout(gtx)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),

						// GROUP 1: Project Actions (Add Repo)
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return a.drawIconButton(gtx, &a.btnAddProject, a.icons.AddFolder, false, pal)
						}),

						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							w := gtx.Dp(unit.Dp(1))
							h := gtx.Dp(unit.Dp(18))
							paint.FillShape(gtx.Ops, pal.Border, clip.Rect(image.Rect(0, 0, w, h)).Op())
							return layout.Dimensions{Size: image.Pt(w, h)}
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),

						// GROUP 2: View Switcher (Commits, Updates, 3D Graph)
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return drawWithRoundedBg(gtx, pal.Bg, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
								return layout.UniformInset(unit.Dp(2)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return a.drawIconButton(gtx, &a.btnTabCommits, a.icons.ViewList, a.activeTab == "commits", pal)
										}),
										layout.Rigid(layout.Spacer{Width: unit.Dp(2)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return a.drawIconButton(gtx, &a.btnTabUpdates, a.icons.Timeline, a.activeTab == "updates", pal)
										}),
										layout.Rigid(layout.Spacer{Width: unit.Dp(2)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return a.drawIconButton(gtx, &a.btnTab3DGraph, a.icons.Rotation3D, a.activeTab == "graph3d", pal)
										}),
									)
								})
							})
						}),

						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							w := gtx.Dp(unit.Dp(1))
							h := gtx.Dp(unit.Dp(18))
							paint.FillShape(gtx.Ops, pal.Border, clip.Rect(image.Rect(0, 0, w, h)).Op())
							return layout.Dimensions{Size: image.Pt(w, h)}
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),

						// GROUP 3: Personal Office / Git Profile & Sync
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return a.drawIconButton(gtx, &a.btnOpenProfile, a.icons.Account, a.showProfileModal, pal)
						}),
					)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return drawDivider(gtx, pal.Border)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),

				// PERSISTENT PROJECTS / WORKSPACES SECTION (Selected first!)
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return a.drawSectionHeader(gtx, fmt.Sprintf("PROJECTS (%d)", len(projects)), pal)
						}),
					)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

				// Search Projects Input (if multiple projects)
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if len(projects) <= 3 {
						return layout.Dimensions{}
					}
					ed := material.Editor(a.theme, &a.searchProjectsEditor, "Filter projects...")
					ed.TextSize = unit.Sp(10.5)
					ed.Color = pal.Text
					ed.HintColor = pal.TextMuted
					return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return drawWithRoundedBg(gtx, pal.Bg, unit.Dp(5), func(gtx layout.Context) layout.Dimensions {
							return layout.UniformInset(unit.Dp(7)).Layout(gtx, ed.Layout)
						})
					})
				}),

				// Projects List (with nested sub-repositories directly beneath the active project)
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return a.createListStyle(&a.projectsList, pal).Layout(gtx, len(projects), func(gtx layout.Context, idx int) layout.Dimensions {
						p := projects[idx]
						if searchProj != "" && !strings.Contains(strings.ToLower(p.Name), searchProj) && !strings.Contains(strings.ToLower(p.Path), searchProj) {
							return layout.Dimensions{}
						}

						isActive := p.Path == rootPath
						st := projStatuses[p.Path]

						// If currently renaming this project
						if p.ID == renamingID {
							return a.drawRenameEditor(gtx, pal)
						}

						var click *widget.Clickable
						if idx < len(a.projectClicks) {
							click = &a.projectClicks[idx]
						} else {
							click = new(widget.Clickable)
						}

						var btnRename *widget.Clickable
						if idx < len(a.projectRenameBtns) {
							btnRename = &a.projectRenameBtns[idx]
						} else {
							btnRename = new(widget.Clickable)
						}

						var btnDelete *widget.Clickable
						if idx < len(a.projectDeleteBtns) {
							btnDelete = &a.projectDeleteBtns[idx]
						} else {
							btnDelete = new(widget.Clickable)
						}

						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							// Project Header Row
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									bg := pal.HeaderBg
									if isActive {
										bg = pal.Selected
									} else if click.Hovered() {
										bg = pal.Hover
									}

									return drawWithRoundedBg(gtx, bg, unit.Dp(5), func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Top: unit.Dp(7), Bottom: unit.Dp(7), Left: unit.Dp(10), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													iconCol := pal.TextMuted
													if isActive {
														iconCol = pal.Accent
													}
													if st.HasUncommitted {
														iconCol = pal.AddText
													} else if st.HasRecent30d {
														if !isActive {
															iconCol = pal.Accent
														}
													} else {
														iconCol = pal.TextMuted
													}
													return a.icons.Layout(gtx, a.icons.Folder, iconCol, unit.Dp(13))
												}),
												layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
												layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
													lbl := material.Label(a.theme, unit.Sp(11), p.Name)
													if isActive {
														lbl.Color = pal.Accent
														lbl.Font.Weight = font.Bold
													} else if st.HasUncommitted {
														lbl.Color = pal.Text
														lbl.Font.Weight = font.SemiBold
													} else if st.HasRecent30d {
														lbl.Color = pal.Text
														lbl.Font.Weight = font.Medium
													} else {
														lbl.Color = pal.TextMuted
														lbl.Font.Weight = font.Normal
													}
													return lbl.Layout(gtx)
												}),
												// Uncommitted changes badge
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													if st.HasUncommitted {
														return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
															return a.drawTag(gtx, "MOD", pal.AddBg, pal.AddText)
														})
													}
													return layout.Dimensions{}
												}),
												// Rename button [Edit]
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													return btnRename.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														col := pal.TextMuted
														if btnRename.Hovered() {
															col = pal.Text
														}
														return layout.UniformInset(unit.Dp(4)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
															return a.icons.Layout(gtx, a.icons.Edit, col, unit.Dp(11))
														})
													})
												}),
												layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
												// Delete button [Delete]
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													return btnDelete.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														col := pal.TextMuted
														if btnDelete.Hovered() {
															col = pal.DelText
														}
														return layout.UniformInset(unit.Dp(4)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
															return a.icons.Layout(gtx, a.icons.Delete, col, unit.Dp(11))
														})
													})
												}),
											)
										})
									})
								})
							}),
							// Nested sub-repositories directly beneath their own parent project
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if !isActive || len(subRepos) <= 1 {
									return layout.Dimensions{}
								}
								return a.layoutProjectSubRepos(gtx, subRepos, activeRepo, projStatuses, pal)
							}),
							layout.Rigid(layout.Spacer{Height: unit.Dp(5)}.Layout),
						)
					})
				}),

				layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return drawDivider(gtx, pal.Border)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),

				// BRANCHES SECTION (Searchable, with Author and Last Update)
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return a.drawSectionHeader(gtx, fmt.Sprintf("BRANCHES (%d)", len(branches)), pal)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

				// Search Branches Input
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					ed := material.Editor(a.theme, &a.searchBranchesEditor, "Filter branches...")
					ed.TextSize = unit.Sp(10.5)
					ed.Color = pal.Text
					ed.HintColor = pal.TextMuted
					return layout.Inset{Bottom: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return drawWithRoundedBg(gtx, pal.Bg, unit.Dp(5), func(gtx layout.Context) layout.Dimensions {
							return layout.UniformInset(unit.Dp(7)).Layout(gtx, ed.Layout)
						})
					})
				}),

				// Scrollable Branches List with Author & Time!
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					var filteredBranches []int
					for i, b := range branches {
						if searchBranch != "" && !strings.Contains(strings.ToLower(b.Name), searchBranch) && !strings.Contains(strings.ToLower(b.LastAuthor), searchBranch) {
							continue
						}
						filteredBranches = append(filteredBranches, i)
					}

					if len(filteredBranches) == 0 {
						return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Label(a.theme, unit.Sp(11), "No matching branches")
							lbl.Color = pal.TextMuted
							return lbl.Layout(gtx)
						})
					}

					return a.createListStyle(&a.branchesList, pal).Layout(gtx, len(filteredBranches), func(gtx layout.Context, fIdx int) layout.Dimensions {
						origIdx := filteredBranches[fIdx]
						b := branches[origIdx]
						isActive := b.Name == activeBranch

						var click *widget.Clickable
						if origIdx < len(a.branchClicks) {
							click = &a.branchClicks[origIdx]
						} else {
							click = new(widget.Clickable)
						}

						var pinBtn *widget.Clickable
						if origIdx < len(a.branchPinBtns) {
							pinBtn = &a.branchPinBtns[origIdx]
						} else {
							pinBtn = new(widget.Clickable)
						}

						bg := pal.HeaderBg
						if isActive {
							bg = pal.Selected
						} else if click.Hovered() || pinBtn.Hovered() {
							bg = pal.Hover
						}

						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return drawWithRoundedBg(gtx, bg, unit.Dp(4), func(gtx layout.Context) layout.Dimensions {
									return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
											// Branch info (clickable to switch branch)
											layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
												return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
														// Row 1: Branch name with active indicator & pin indicator
														layout.Rigid(func(gtx layout.Context) layout.Dimensions {
															return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
																layout.Rigid(func(gtx layout.Context) layout.Dimensions {
																	if b.IsPinned {
																		return layout.Inset{Right: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
																			return a.icons.Layout(gtx, a.icons.Paperclip, pal.Accent, unit.Dp(11))
																		})
																	}
																	if b.IsCurrent {
																		return layout.Inset{Right: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
																			lbl := material.Label(a.theme, unit.Sp(10), "●")
																			lbl.Color = pal.Accent
																			return lbl.Layout(gtx)
																		})
																	}
																	return layout.Spacer{Width: unit.Dp(4)}.Layout(gtx)
																}),
																layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
																	lbl := material.Label(a.theme, unit.Sp(11), b.Name)
																	if isActive {
																		lbl.Font.Weight = font.Bold
																		lbl.Color = pal.Accent
																	} else {
																		lbl.Font.Weight = font.Medium
																		lbl.Color = pal.Text
																	}
																	return lbl.Layout(gtx)
																}),
															)
														}),
														// Row 2: Author and Last Update Time!
														layout.Rigid(func(gtx layout.Context) layout.Dimensions {
															if b.LastAuthor == "" && b.LastUpdate == "" {
																return layout.Dimensions{}
															}
															sub := b.LastAuthor
															if b.LastUpdate != "" {
																if sub != "" {
																	sub += "  •  "
																}
																sub += b.LastUpdate
															}
															return layout.Inset{Top: unit.Dp(2), Left: unit.Dp(15)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
																lbl := material.Label(a.theme, unit.Sp(9.5), sub)
																lbl.Color = pal.TextMuted
																return lbl.Layout(gtx)
															})
														}),
													)
												})
											}),
											// Pin button (paperclip icon)
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												pinCol := pal.TextMuted
												if b.IsPinned {
													pinCol = pal.Accent
												} else if pinBtn.Hovered() {
													pinCol = pal.Text
												}
												return pinBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
													return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(4), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
														return a.icons.Layout(gtx, a.icons.Paperclip, pinCol, unit.Dp(12))
													})
												})
											}),
										)
									})
								})
							}),
							layout.Rigid(layout.Spacer{Height: unit.Dp(5)}.Layout),
						)
					})
				}),
			)
		})
	})
})
}

func (a *App) layoutProjectSubRepos(gtx layout.Context, subRepos []models.SubRepo, activeRepo string, projStatuses map[string]ProjectStatus, pal Palette) layout.Dimensions {
	if len(subRepos) <= 1 {
		return layout.Dimensions{}
	}

	return layout.Inset{Left: unit.Dp(14), Top: unit.Dp(3), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		m := op.Record(gtx.Ops)
		dims := layout.Inset{Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			var children []layout.FlexChild
			for i := range subRepos {
				sIdx := i
				r := subRepos[sIdx]
				isActiveSub := r.Path == activeRepo
				stSub := projStatuses[r.Path]

				var click *widget.Clickable
				if sIdx < len(a.subRepoClicks) {
					click = &a.subRepoClicks[sIdx]
				} else {
					click = new(widget.Clickable)
				}

				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						bg := color.NRGBA{}
						if isActiveSub {
							bg = pal.Selected
						} else if click.Hovered() {
							bg = pal.Hover
						}

						return drawWithRoundedBg(gtx, bg, unit.Dp(4), func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(5), Bottom: unit.Dp(5), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										connector := "└ "
										if sIdx < len(subRepos)-1 {
											connector = "├ "
										}
										lbl := material.Label(a.theme, unit.Sp(10), connector)
										lbl.Color = pal.TextMuted
										return lbl.Layout(gtx)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										iconCol := pal.TextMuted
										if isActiveSub {
											iconCol = pal.Accent
										} else if stSub.HasUncommitted {
											iconCol = pal.AddText
										} else if stSub.HasRecent30d {
											iconCol = pal.Accent
										}
										return a.icons.Layout(gtx, a.icons.Folder, iconCol, unit.Dp(11.5))
									}),
									layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										name := r.RelPath
										if r.IsRoot || name == "" {
											name = "(root)"
										}
										lbl := material.Label(a.theme, unit.Sp(10.5), name)
										if isActiveSub {
											lbl.Color = pal.Accent
											lbl.Font.Weight = font.Bold
										} else if stSub.HasUncommitted {
											lbl.Color = pal.Text
											lbl.Font.Weight = font.SemiBold
										} else {
											lbl.Color = pal.TextMuted
											lbl.Font.Weight = font.Normal
										}
										return lbl.Layout(gtx)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										if stSub.HasUncommitted {
											return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return a.drawTag(gtx, "MOD", pal.AddBg, pal.AddText)
											})
										}
										return layout.Dimensions{}
									}),
								)
							})
						})
					})
				}))
				if sIdx < len(subRepos)-1 {
					children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(2)}.Layout))
				}
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
		call := m.Stop()

		// Draw subtle vertical tree guide line matching the actual height of the subrepos column
		guideCol := color.NRGBA{R: pal.Border.R, G: pal.Border.G, B: pal.Border.B, A: 160}
		lineW := gtx.Dp(unit.Dp(1))
		paint.FillShape(gtx.Ops, guideCol, clip.Rect(image.Rect(0, 0, lineW, dims.Size.Y)).Op())
		call.Add(gtx.Ops)
		return dims
	})
}

func (a *App) drawRenameEditor(gtx layout.Context, pal Palette) layout.Dimensions {
	return drawWithBg(gtx, pal.Surface, func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(unit.Dp(6)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					ed := material.Editor(a.theme, &a.renameEditor, "Project alias...")
					ed.TextSize = unit.Sp(11)
					ed.Color = pal.Text
					return ed.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return a.drawPillButton(gtx, &a.btnSaveRename, "Save", true, pal)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return a.drawPillButton(gtx, &a.btnCancelRename, "Cancel", false, pal)
						}),
					)
				}),
			)
		})
	})
}

// -------------------------------------------------------------
// PANEL 2: Middle Panel (Commits List with Uncommitted Changes + Counts)
// -------------------------------------------------------------
func (a *App) layoutCenterPanel(gtx layout.Context, pal Palette) layout.Dimensions {
	if a.activeTab == "updates" {
		return a.layoutUpdatesCenterPanel(gtx, pal)
	}
	return a.layoutCommitsCenterPanel(gtx, pal)
}

func (a *App) layoutCommitsCenterPanel(gtx layout.Context, pal Palette) layout.Dimensions {
	a.mu.Lock()
	commits := a.commits
	activeBranch := a.activeBranch
	cAll := a.countAll
	cMine := a.countMine
	cMerges := a.countMerges
	cDirect := a.countDirect
	cAI := a.countAI
	a.mu.Unlock()

	search := strings.ToLower(strings.TrimSpace(a.searchCommitsEditor.Text()))

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header info bar: Active Branch & Search (Floating Island Card)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return drawWithBorder(gtx, pal.Surface, unit.Dp(8), unit.Dp(1), pal.Border, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							// Current branch indicator
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										lbl := material.Label(a.theme, unit.Sp(10.5), "BRANCH:")
										lbl.Font.Weight = font.Bold
										lbl.Color = pal.TextMuted
										return lbl.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return a.drawTag(gtx, activeBranch, pal.BadgeBg, pal.Accent)
									}),
								)
							}),
							layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
							// Search input
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								ed := material.Editor(a.theme, &a.searchCommitsEditor, "Search commits, author, or hash...")
								ed.TextSize = unit.Sp(11)
								ed.Color = pal.Text
								ed.HintColor = pal.TextMuted
								return drawWithRoundedBg(gtx, pal.Bg, unit.Dp(4), func(gtx layout.Context) layout.Dimensions {
									return layout.UniformInset(unit.Dp(7)).Layout(gtx, ed.Layout)
								})
							}),
							layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
							// Filter chips with exact counts!
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return a.drawPillButton(gtx, &a.btnFilterAll, fmt.Sprintf("All (%d)", cAll), a.commitFilter == "all", pal)
									}),
									layout.Rigid(layout.Spacer{Width: unit.Dp(5)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return a.drawPillButton(gtx, &a.btnFilterMine, fmt.Sprintf("Mine (%d)", cMine), a.commitFilter == "mine", pal)
									}),
									layout.Rigid(layout.Spacer{Width: unit.Dp(5)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return a.drawPillButton(gtx, &a.btnFilterMerges, fmt.Sprintf("Merges (%d)", cMerges), a.commitFilter == "merges", pal)
									}),
									layout.Rigid(layout.Spacer{Width: unit.Dp(5)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return a.drawPillButton(gtx, &a.btnFilterDirect, fmt.Sprintf("Direct (%d)", cDirect), a.commitFilter == "direct", pal)
									}),
									layout.Rigid(layout.Spacer{Width: unit.Dp(5)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return a.drawPillButton(gtx, &a.btnFilterAI, fmt.Sprintf("AI (%d)", cAI), a.commitFilter == "ai", pal)
									}),
								)
							}),
						)
					})
				})
			})
		}),
		// Commits List (Includes Uncommitted Changes at top if present)
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			var filtered []models.CommitSummary
			for _, c := range commits {
				if search != "" {
					if !strings.Contains(strings.ToLower(c.Message), search) &&
						!strings.Contains(strings.ToLower(c.AuthorName), search) &&
						!strings.Contains(strings.ToLower(c.ShortHash), search) {
						continue
					}
				}
				filtered = append(filtered, c)
			}

			if len(filtered) == 0 {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(a.theme, unit.Sp(12), "No commits found for this filter")
					lbl.Color = pal.TextMuted
					return lbl.Layout(gtx)
				})
			}

			return a.createListStyle(&a.commitsList, pal).Layout(gtx, len(filtered), func(gtx layout.Context, idx int) layout.Dimensions {
				c := filtered[idx]
				isSelected := c.Hash == a.selectedHash
				isUncommitted := c.Hash == "UNCOMMITTED"

				var click *widget.Clickable
				for origIdx, origC := range commits {
					if origC.Hash == c.Hash && origIdx < len(a.commitClicks) {
						click = &a.commitClicks[origIdx]
						break
					}
				}
				if click == nil {
					click = new(widget.Clickable)
				}

				var pinBtn *widget.Clickable
				for origIdx, origC := range commits {
					if origC.Hash == c.Hash && origIdx < len(a.commitPinBtns) {
						pinBtn = &a.commitPinBtns[origIdx]
						break
					}
				}
				if pinBtn == nil {
					pinBtn = new(widget.Clickable)
				}

				borderColor := pal.Border
				if isSelected {
					borderColor = pal.Accent
				}
				cardBg := pal.Surface
				if isSelected {
					cardBg = pal.Selected
				} else if click.Hovered() || pinBtn.Hovered() {
					cardBg = pal.Hover
				}

				return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return drawWithBorder(gtx, cardBg, unit.Dp(8), unit.Dp(1), borderColor, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{
							Top:    unit.Dp(10),
							Bottom: unit.Dp(10),
							Left:   unit.Dp(12),
							Right:  unit.Dp(12),
						}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
											// Author + Time
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
													layout.Rigid(func(gtx layout.Context) layout.Dimensions {
														return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
															layout.Rigid(func(gtx layout.Context) layout.Dimensions {
																if c.IsPinned {
																	return layout.Inset{Right: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
																		return a.icons.Layout(gtx, a.icons.Paperclip, pal.Accent, unit.Dp(11))
																	})
																}
																return layout.Dimensions{}
															}),
															layout.Rigid(func(gtx layout.Context) layout.Dimensions {
																lbl := material.Label(a.theme, unit.Sp(11), c.AuthorName)
																lbl.Font.Weight = font.SemiBold
																if isUncommitted {
																	lbl.Color = pal.AddText
																} else {
																	lbl.Color = pal.Text
																}
																return lbl.Layout(gtx)
															}),
														)
													}),
													layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
													layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
													layout.Rigid(func(gtx layout.Context) layout.Dimensions {
														timeText := formatRelativeTime(c.Date)
														if isUncommitted {
															timeText = "Now"
														}
														lbl := material.Label(a.theme, unit.Sp(10.5), timeText)
														lbl.Color = pal.TextMuted
														return lbl.Layout(gtx)
													}),
												)
											}),
											layout.Rigid(layout.Spacer{Height: unit.Dp(5)}.Layout),
											// Message
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												msg := c.Message
												if len(msg) > 75 {
													msg = msg[:75] + "..."
												}
												lbl := material.Label(a.theme, unit.Sp(12), msg)
												lbl.Color = pal.Text
												if isUncommitted {
													lbl.Font.Weight = font.Bold
												}
												return lbl.Layout(gtx)
											}),
											layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
											// Badges + Stats
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
													layout.Rigid(func(gtx layout.Context) layout.Dimensions {
														if isUncommitted {
															return a.drawTag(gtx, "UNCOMMITTED", pal.AddBg, pal.AddText)
														}
														if c.IsPinned {
															return layout.Inset{Right: unit.Dp(5)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
																return a.drawTag(gtx, "PINNED", pal.Selected, pal.Accent)
															})
														}
														return layout.Dimensions{}
													}),
													layout.Rigid(func(gtx layout.Context) layout.Dimensions {
														if c.IsMerge {
															return a.drawTag(gtx, "MERGE", pal.Accent, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
														}
														if c.IsAI {
															tag := "AI"
															if c.AITag != "" {
																tag = c.AITag
															}
															return a.drawTag(gtx, tag, pal.BadgeBg, pal.BadgeText)
														}
														return layout.Dimensions{}
													}),
													layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
													layout.Rigid(func(gtx layout.Context) layout.Dimensions {
														stats := formatDiffStats(c.Additions, c.Deletions)
														if stats == "" {
															return layout.Dimensions{}
														}
														lbl := material.Label(a.theme, unit.Sp(10.5), stats)
														lbl.Color = pal.TextMuted
														return lbl.Layout(gtx)
													}),
													layout.Rigid(func(gtx layout.Context) layout.Dimensions {
														if isUncommitted {
															return layout.Dimensions{}
														}
														var copyBtn *widget.Clickable
														for origIdx, origC := range commits {
															if origC.Hash == c.Hash && origIdx < len(a.commitCopyBtns) {
																copyBtn = &a.commitCopyBtns[origIdx]
																break
															}
														}
														if copyBtn == nil {
															copyBtn = new(widget.Clickable)
														}
														isCopied := a.copiedHash == c.ShortHash
														return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
															return a.drawCopyBadge(gtx, copyBtn, c.Hash, c.ShortHash, isCopied, pal)
														})
													}),
												)
											}),
										)
									})
								}),
								// Pin button (paperclip vector icon)
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if isUncommitted {
										return layout.Dimensions{}
									}
									pinCol := pal.TextMuted
									if c.IsPinned {
										pinCol = pal.Accent
									} else if pinBtn.Hovered() {
										pinCol = pal.Text
									}
									return pinBtn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Left: unit.Dp(8), Right: unit.Dp(2), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return a.icons.Layout(gtx, a.icons.Paperclip, pinCol, unit.Dp(13))
										})
									})
								}),
							)
						})
					})
				})
			})
		}),
	)
}

func (a *App) layoutUpdatesCenterPanel(gtx layout.Context, pal Palette) layout.Dimensions {
	a.mu.Lock()
	up := a.updatesData
	a.mu.Unlock()

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Time Slices Header (Floating Island Card)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return drawWithBorder(gtx, pal.Surface, unit.Dp(8), unit.Dp(1), pal.Border, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return a.drawSectionHeader(gtx, "TIME RANGE", pal)
							}),
							layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return a.drawPillButton(gtx, &a.btnSlice1h, "1h", a.timePeriod == "1h", pal)
									}),
									layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return a.drawPillButton(gtx, &a.btnSlice24h, "24h", a.timePeriod == "24h", pal)
									}),
									layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return a.drawPillButton(gtx, &a.btnSliceToday, "Today", a.timePeriod == "today", pal)
									}),
									layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return a.drawPillButton(gtx, &a.btnSlice7d, "7d", a.timePeriod == "7d", pal)
									}),
									layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return a.drawPillButton(gtx, &a.btnSliceAll, "All", a.timePeriod == "all", pal)
									}),
								)
							}),
							layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
							// AI Filter
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return a.drawPillButton(gtx, &a.btnUpFilterAll, "All", a.aiFilter == "all", pal)
									}),
									layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return a.drawPillButton(gtx, &a.btnUpFilterAI, "AI Only", a.aiFilter == "ai_only", pal)
									}),
									layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return a.drawPillButton(gtx, &a.btnUpFilterHuman, "Devs", a.aiFilter == "human_only", pal)
									}),
								)
							}),
						)
					})
				})
			})
		}),
		// Feed
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if up == nil || len(up.Commits) == 0 {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(a.theme, unit.Sp(12), "No commits in this timeframe")
					lbl.Color = pal.TextMuted
					return lbl.Layout(gtx)
				})
			}

			return a.createListStyle(&a.updatesList, pal).Layout(gtx, len(up.Commits), func(gtx layout.Context, idx int) layout.Dimensions {
				c := up.Commits[idx]
				var click *widget.Clickable
				if idx < len(a.timelineClicks) {
					click = &a.timelineClicks[idx]
				} else {
					click = new(widget.Clickable)
				}

				return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					bg := pal.Surface
					if click.Hovered() {
						bg = pal.Hover
					}

					return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return drawWithBorder(gtx, bg, unit.Dp(8), unit.Dp(1), pal.Border, func(gtx layout.Context) layout.Dimensions {
							return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												lbl := material.Label(a.theme, unit.Sp(11), c.AuthorName)
												lbl.Font.Weight = font.SemiBold
												lbl.Color = pal.Text
												return lbl.Layout(gtx)
											}),
											layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												if c.IsMerge {
													return a.drawTag(gtx, "MERGE", pal.Accent, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
												}
												if c.IsAI {
													tag := "AI"
													if c.AITag != "" {
														tag = c.AITag
													}
													return a.drawTag(gtx, tag, pal.BadgeBg, pal.BadgeText)
												}
												return layout.Dimensions{}
											}),
											layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												lbl := material.Label(a.theme, unit.Sp(10.5), formatRelativeTime(c.Date))
												lbl.Color = pal.TextMuted
												return lbl.Layout(gtx)
											}),
										)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										lbl := material.Label(a.theme, unit.Sp(11.5), c.Message)
										lbl.Color = pal.Text
										return lbl.Layout(gtx)
									}),
								)
							})
						})
					})
				})
			})
		}),
	)
}

// -------------------------------------------------------------
// PANEL 3: Right Panel (Diff Viewer / Merged Commits / Metrics)
// -------------------------------------------------------------
func (a *App) layoutRightPanel(gtx layout.Context, pal Palette) layout.Dimensions {
	if a.activeTab == "updates" {
		return a.layoutUpdatesAnalytics(gtx, pal)
	}
	return a.layoutCommitDetail(gtx, pal)
}

func (a *App) layoutCommitDetail(gtx layout.Context, pal Palette) layout.Dimensions {
	a.mu.Lock()
	detail := a.selectedDetail
	a.mu.Unlock()

	if detail == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Label(a.theme, unit.Sp(12), "Select a commit from the list to view diffs")
			lbl.Color = pal.TextMuted
			return lbl.Layout(gtx)
		})
	}

	isUncommitted := detail.Hash == "UNCOMMITTED"

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header summary (Floating Island Card)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return drawWithBorder(gtx, pal.Surface, unit.Dp(8), unit.Dp(1), pal.Border, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							// Subject & Tag & Pin button
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										if isUncommitted {
											return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return a.drawTag(gtx, "UNCOMMITTED CHANGES", pal.AddBg, pal.AddText)
											})
										}
										if detail.IsPinned {
											return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return a.drawTag(gtx, "PINNED", pal.Selected, pal.Accent)
											})
										}
										if detail.IsMerge {
											return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												return a.drawTag(gtx, "MERGE COMMIT", pal.Accent, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
											})
										}
										return layout.Dimensions{}
									}),
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										lbl := material.Label(a.theme, unit.Sp(14), detail.Message)
										lbl.Font.Weight = font.Bold
										lbl.Color = pal.Text
										return lbl.Layout(gtx)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										if isUncommitted {
											return layout.Dimensions{}
										}
										pinText := "Pin"
										if detail.IsPinned {
											pinText = "Pinned"
										}
										return a.drawIconPillButton(gtx, &a.btnPinSelected, a.icons.Paperclip, pinText, detail.IsPinned, pal)
									}),
								)
							}),
							layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
							// Metadata row
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								dateStr := formatRelativeTime(detail.Date)
								if isUncommitted {
									dateStr = "Current working directory"
								}
								return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										meta := fmt.Sprintf("%s  •  %s", detail.AuthorName, dateStr)
										if len(detail.Files) > 0 {
											meta += fmt.Sprintf("  •  Files: %d", len(detail.Files))
										}
										stats := formatDiffStats(detail.Additions, detail.Deletions)
										if stats != "" {
											meta += "  •  " + stats
										}
										lbl := material.Label(a.theme, unit.Sp(11), meta)
										lbl.Color = pal.TextMuted
										return lbl.Layout(gtx)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										if isUncommitted {
											return layout.Dimensions{}
										}
										isCopied := a.copiedHash == detail.ShortHash
										return layout.Inset{Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return a.drawCopyBadge(gtx, &a.btnCopyDetailHash, detail.Hash, detail.ShortHash, isCopied, pal)
										})
									}),
								)
							}),
						)
					})
				})
			})
		}),
		// IF UNCOMMITTED: Show Commit & Push interactive action box!
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if isUncommitted {
				return a.layoutCommitActionBox(gtx, pal)
			}
			return layout.Dimensions{}
		}),
		// IF MERGE: Show contained commits section!
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if detail.IsMerge && len(detail.MergedCommits) > 0 {
				return a.layoutMergedCommitsSection(gtx, detail.MergedCommits, pal)
			}
			return layout.Dimensions{}
		}),
		// MAIN REVIEW WORKSPACE: Files sidebar + Diff document cards
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return a.layoutReviewWorkspace(gtx, detail, pal)
		}),
	)
}

// Review workspace containing changed files sidebar on the left and diff documents on the right
func (a *App) layoutReviewWorkspace(gtx layout.Context, detail *models.CommitDetail, pal Palette) layout.Dimensions {
	if len(detail.Files) == 0 {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Label(a.theme, unit.Sp(12), "No changed files in this commit")
			lbl.Color = pal.TextMuted
			return lbl.Layout(gtx)
		})
	}

	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		// Left: Dedicated Changed Files Sidebar (Floating Island Card)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			w := gtx.Dp(unit.Dp(260))
			gtx.Constraints.Min.X = w
			gtx.Constraints.Max.X = w
			return layout.Inset{
				Top:    unit.Dp(2),
				Bottom: unit.Dp(10),
				Left:   unit.Dp(6),
				Right:  unit.Dp(4),
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return drawWithBorder(gtx, pal.Surface, unit.Dp(8), unit.Dp(1), pal.Border, func(gtx layout.Context) layout.Dimensions {
					return a.layoutChangedFilesSidebar(gtx, detail, pal)
				})
			})
		}),
		// Right: Spacious Diff Document Cards (Floating with comfortable outer padding)
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{
				Top:    unit.Dp(2),
				Bottom: unit.Dp(10),
				Left:   unit.Dp(4),
				Right:  unit.Dp(10),
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return a.layoutDiffDocumentCards(gtx, detail, pal)
			})
		}),
	)
}

func (a *App) layoutChangedFilesSidebar(gtx layout.Context, detail *models.CommitDetail, pal Palette) layout.Dimensions {
	if len(a.fileItemClicks) < len(detail.Files) {
		a.fileItemClicks = make([]widget.Clickable, len(detail.Files))
	}

	searchFile := strings.ToLower(strings.TrimSpace(a.searchFilesEditor.Text()))
	var matched []int
	for i, f := range detail.Files {
		if searchFile != "" && !strings.Contains(strings.ToLower(f.NewPath), searchFile) && !strings.Contains(strings.ToLower(f.OldPath), searchFile) {
			continue
		}
		matched = append(matched, i)
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Section Header & Quick Collapse/Expand Controls
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						title := fmt.Sprintf("FILES (%d)", len(detail.Files))
						lbl := material.Label(a.theme, unit.Sp(10.5), title)
						lbl.Font.Weight = font.Bold
						lbl.Color = pal.TextMuted
						return lbl.Layout(gtx)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return a.drawIconPillButton(gtx, &a.btnCollapseAll, a.icons.ExpandLess, "", false, pal)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return a.drawIconPillButton(gtx, &a.btnExpandAll, a.icons.ExpandMore, "", false, pal)
					}),
				)
			})
		}),
		// File Search Filter Input
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				ed := material.Editor(a.theme, &a.searchFilesEditor, "Filter files (path or name)...")
				ed.TextSize = unit.Sp(10.5)
				ed.Color = pal.Text
				ed.HintColor = pal.TextMuted
				return drawWithRoundedBg(gtx, pal.Bg, unit.Dp(5), func(gtx layout.Context) layout.Dimensions {
					return layout.UniformInset(unit.Dp(6)).Layout(gtx, ed.Layout)
				})
			})
		}),
		// "View All Files" Shortcut Button
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(10), Right: unit.Dp(10), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				allActive := (a.selectedFileIdx == -1)
				txt := fmt.Sprintf("All Files (%d)", len(detail.Files))
				return a.drawIconPillButton(gtx, &a.btnAllFiles, a.icons.Folder, txt, allActive, pal)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return drawDivider(gtx, pal.Border)
		}),
		// Scrollable Files List
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if len(matched) == 0 {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(a.theme, unit.Sp(11), "No matching files")
					lbl.Color = pal.TextMuted
					return lbl.Layout(gtx)
				})
			}

			return a.createListStyle(&a.filesListState, pal).Layout(gtx, len(matched), func(gtx layout.Context, mIdx int) layout.Dimensions {
				origIdx := matched[mIdx]
				f := detail.Files[origIdx]
				isSelected := (a.selectedFileIdx == origIdx)

				filePath := filepath.ToSlash(f.NewPath)
				dir := filepath.Dir(filePath)
				if dir == "." || dir == "/" {
					dir = ""
				} else {
					dir = dir + "/"
				}
				fileName := filepath.Base(filePath)

				var click *widget.Clickable
				if origIdx < len(a.fileItemClicks) {
					click = &a.fileItemClicks[origIdx]
				} else {
					click = new(widget.Clickable)
				}

				return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					bg := pal.Surface
					if isSelected {
						bg = pal.Selected
					} else if click.Hovered() {
						bg = pal.Hover
					}

					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return drawWithBg(gtx, bg, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
									// Active indicator bar
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										if !isSelected {
											return layout.Dimensions{}
										}
										return drawWithBg(gtx, pal.Accent, func(gtx layout.Context) layout.Dimensions {
											return layout.Dimensions{Size: image.Pt(gtx.Dp(unit.Dp(3)), gtx.Dp(unit.Dp(38)))}
										})
									}),
									layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
										return layout.Inset{Top: unit.Dp(7.5), Bottom: unit.Dp(7.5), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
												// Directory path line (if present)
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													if dir == "" {
														return layout.Dimensions{}
													}
													lbl := material.Label(a.theme, unit.Sp(9.5), dir)
													lbl.Color = pal.TextMuted
													return lbl.Layout(gtx)
												}),
												// File name row with vector icon, status badge, and diff stats
												layout.Rigid(func(gtx layout.Context) layout.Dimensions {
													return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
														layout.Rigid(func(gtx layout.Context) layout.Dimensions {
															iconCol := pal.TextMuted
															if isSelected {
																iconCol = pal.Accent
															}
															return a.icons.Layout(gtx, a.icons.File, iconCol, unit.Dp(12))
														}),
														layout.Rigid(layout.Spacer{Width: unit.Dp(5)}.Layout),
														layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
															lbl := material.Label(a.theme, unit.Sp(11), fileName)
															if isSelected {
																lbl.Font.Weight = font.Bold
																lbl.Color = pal.Text
															} else {
																lbl.Font.Weight = font.Medium
																lbl.Color = pal.Text
															}
															return lbl.Layout(gtx)
														}),
														layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
														layout.Rigid(func(gtx layout.Context) layout.Dimensions {
															tagBg := pal.BadgeBg
															tagFg := pal.BadgeText
															tagText := "M"
															switch strings.ToLower(f.Status) {
															case "added":
																tagBg = pal.AddBg
																tagFg = pal.AddText
																tagText = "A"
															case "deleted":
																tagBg = pal.DelBg
																tagFg = pal.DelText
																tagText = "D"
															}
															return a.drawTag(gtx, tagText, tagBg, tagFg)
														}),
														layout.Rigid(func(gtx layout.Context) layout.Dimensions {
															stats := formatDiffStats(f.Additions, f.Deletions)
															if stats == "" {
																return layout.Dimensions{}
															}
															return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
																layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
																layout.Rigid(func(gtx layout.Context) layout.Dimensions {
																	lbl := material.Label(a.theme, unit.Sp(9.5), stats)
																	lbl.Color = pal.TextMuted
																	return lbl.Layout(gtx)
																}),
															)
														}),
													)
												}),
											)
										})
									}),
								)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return drawDivider(gtx, pal.Border)
						}),
					)
				})
			})
		}),
	)
}

func (a *App) layoutDiffDocumentCards(gtx layout.Context, detail *models.CommitDetail, pal Palette) layout.Dimensions {
	if len(a.fileHeaderClicks) < len(detail.Files) {
		a.fileHeaderClicks = make([]widget.Clickable, len(detail.Files))
	}
	if a.collapsedFiles == nil {
		a.collapsedFiles = make(map[string]bool)
	}

	type diffItem struct {
		isFileHeader bool
		isHunk       bool
		isSpacer     bool
		isCollapsed  bool
		isLastInFile bool
		fileOrigIdx  int
		fileDir      string
		fileName     string
		status       string
		adds, dels   int
		hunkHeader   string
		line         models.DiffLine
	}

	searchFile := strings.ToLower(strings.TrimSpace(a.searchFilesEditor.Text()))

	// Determine which files to render: single selected file or all matching files
	var targetIndices []int
	if a.selectedFileIdx >= 0 && a.selectedFileIdx < len(detail.Files) {
		targetIndices = append(targetIndices, a.selectedFileIdx)
	} else {
		for i, f := range detail.Files {
			if searchFile != "" && !strings.Contains(strings.ToLower(f.NewPath), searchFile) && !strings.Contains(strings.ToLower(f.OldPath), searchFile) {
				continue
			}
			targetIndices = append(targetIndices, i)
		}
	}

	var items []diffItem
	for _, origIdx := range targetIndices {
		f := detail.Files[origIdx]
		isCollapsed := a.collapsedFiles[f.NewPath]

		filePath := filepath.ToSlash(f.NewPath)
		dir := filepath.Dir(filePath)
		if dir == "." || dir == "/" {
			dir = ""
		} else {
			dir = dir + "/"
		}
		fileName := filepath.Base(filePath)

		items = append(items, diffItem{
			isFileHeader: true,
			isCollapsed:  isCollapsed,
			fileOrigIdx:  origIdx,
			fileDir:      dir,
			fileName:     fileName,
			status:       f.Status,
			adds:         f.Additions,
			dels:         f.Deletions,
		})

		hasContent := false
		if !isCollapsed {
			for _, h := range f.Hunks {
				items = append(items, diffItem{
					isHunk:     true,
					hunkHeader: h.Header,
				})
				hasContent = true
				for _, l := range h.Lines {
					items = append(items, diffItem{
						line: l,
					})
				}
			}
		}

		if hasContent && len(items) > 0 {
			items[len(items)-1].isLastInFile = true
		} else {
			items[len(items)-1].isCollapsed = true
		}

		// Separate file documents with a spacious gap
		items = append(items, diffItem{isSpacer: true})
	}

	if len(items) == 0 {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Label(a.theme, unit.Sp(12), "No files matching filter")
			lbl.Color = pal.TextMuted
			return lbl.Layout(gtx)
		})
	}

	return a.createListStyle(&a.diffList, pal).Layout(gtx, len(items), func(gtx layout.Context, idx int) layout.Dimensions {
		it := items[idx]

		if it.isSpacer {
			return layout.Spacer{Height: unit.Dp(16)}.Layout(gtx)
		}

		if it.isFileHeader {
			var click *widget.Clickable
			if it.fileOrigIdx < len(a.fileHeaderClicks) {
				click = &a.fileHeaderClicks[it.fileOrigIdx]
			} else {
				click = new(widget.Clickable)
			}

			return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				bg := pal.Surface
				if click.Hovered() {
					bg = pal.Hover
				}

				cardFn := func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(14), Right: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							// Chevron vector icon
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								ic := a.icons.ExpandMore
								if it.isCollapsed {
									ic = a.icons.ChevronRight
								}
								return a.icons.Layout(gtx, ic, pal.Accent, unit.Dp(13))
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
							// File vector icon
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return a.icons.Layout(gtx, a.icons.File, pal.TextMuted, unit.Dp(13))
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
							// Directory path (muted, comfortable to read)
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if it.fileDir == "" {
									return layout.Dimensions{}
								}
								lbl := material.Label(a.theme, unit.Sp(11), it.fileDir)
								lbl.Color = pal.TextMuted
								return lbl.Layout(gtx)
							}),
							// File name (bold, prominent)
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Label(a.theme, unit.Sp(12), it.fileName)
								lbl.Font.Weight = font.Bold
								lbl.Color = pal.Text
								return lbl.Layout(gtx)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
							// Status tag
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								tagColor := pal.BadgeBg
								tagText := pal.BadgeText
								switch strings.ToLower(it.status) {
								case "added":
									tagColor = pal.AddBg
									tagText = pal.AddText
								case "deleted":
									tagColor = pal.DelBg
									tagText = pal.DelText
								}
								return a.drawTag(gtx, strings.ToUpper(it.status), tagColor, tagText)
							}),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								if !it.isCollapsed {
									return layout.Dimensions{}
								}
								lbl := material.Label(a.theme, unit.Sp(10), "collapsed")
								lbl.Color = pal.TextMuted
								return lbl.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								stats := formatDiffStats(it.adds, it.dels)
								if stats == "" {
									return layout.Dimensions{}
								}
								return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
									layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										lbl := material.Label(a.theme, unit.Sp(10.5), stats)
										lbl.Color = pal.TextMuted
										return lbl.Layout(gtx)
									}),
								)
							}),
						)
					})
				}

				if it.isCollapsed {
					return drawWithBorder(gtx, bg, unit.Dp(8), unit.Dp(1), pal.Border, cardFn)
				}
				return drawDiffCardTopHeader(gtx, bg, pal.Border, unit.Dp(8), cardFn)
			})
		}

		if it.isHunk {
			return drawDiffCardLine(gtx, pal.BadgeBg, pal.Border, it.isLastInFile, unit.Dp(8), func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6.5), Bottom: unit.Dp(6.5), Left: unit.Dp(14), Right: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(a.theme, unit.Sp(10.5), git.SanitizeCodeText(it.hunkHeader))
					lbl.Color = pal.TextMuted
					return lbl.Layout(gtx)
				})
			})
		}

		// Content line
		bgColor := pal.Bg
		textColor := pal.Text
		prefix := " "
		lineNo := ""

		if it.line.Type == "add" {
			bgColor = pal.AddBg
			textColor = pal.AddText
			prefix = "+"
			lineNo = fmt.Sprintf("%d", it.line.NewLineNo)
		} else if it.line.Type == "del" {
			bgColor = pal.DelBg
			textColor = pal.DelText
			prefix = "-"
			lineNo = fmt.Sprintf("%d", it.line.OldLineNo)
		} else {
			lineNo = fmt.Sprintf("%d", it.line.NewLineNo)
		}

		return drawDiffCardLine(gtx, bgColor, pal.Border, it.isLastInFile, unit.Dp(8), func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{
				Top:    unit.Dp(4.5),
				Bottom: unit.Dp(4.5),
				Left:   unit.Dp(12),
				Right:  unit.Dp(12),
			}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					// Line number (right-aligned, spacious 48dp)
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Dp(unit.Dp(48))
						gtx.Constraints.Max.X = gtx.Dp(unit.Dp(48))
						lbl := material.Label(a.theme, unit.Sp(10.5), lineNo)
						lbl.Color = pal.TextMuted
						lbl.Alignment = text.End
						return lbl.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
					// Gutter prefix (+, -, space)
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Dp(unit.Dp(14))
						gtx.Constraints.Max.X = gtx.Dp(unit.Dp(14))
						lbl := material.Label(a.theme, unit.Sp(11), prefix)
						lbl.Font.Weight = font.Bold
						if it.line.Type == "add" {
							lbl.Color = pal.AddText
						} else if it.line.Type == "del" {
							lbl.Color = pal.DelText
						} else {
							lbl.Color = pal.TextMuted
						}
						return lbl.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					// Code text (comfortable size, sanitized text)
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(a.theme, unit.Sp(11.5), git.SanitizeCodeText(it.line.Content))
						lbl.Color = textColor
						return lbl.Layout(gtx)
					}),
				)
			})
		})
	})
}

// Merged commits list inside a merge commit
func (a *App) layoutMergedCommitsSection(gtx layout.Context, merged []models.CommitSummary, pal Palette) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(6), Left: unit.Dp(6), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return drawWithBorder(gtx, pal.Surface, unit.Dp(8), unit.Dp(1), pal.Border, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(a.theme, unit.Sp(11), fmt.Sprintf("COMMITS MERGED IN THIS PULL (%d):", len(merged)))
						lbl.Font.Weight = font.Bold
						lbl.Color = pal.Accent
						return lbl.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							a.drawMergedCommitItems(gtx, merged, pal)...,
						)
					}),
				)
			})
		})
	})
}

func (a *App) drawMergedCommitItems(gtx layout.Context, merged []models.CommitSummary, pal Palette) []layout.FlexChild {
	var children []layout.FlexChild
	for i, c := range merged {
		idx := i
		commit := c

		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			var click *widget.Clickable
			if idx < len(a.mergedClicks) {
				click = &a.mergedClicks[idx]
			} else {
				click = new(widget.Clickable)
			}

			return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				bg := pal.Surface
				if click.Hovered() {
					bg = pal.Hover
				}

				return drawWithBg(gtx, bg, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								var copyBtn *widget.Clickable
								if idx < len(a.mergedCopyBtns) {
									copyBtn = &a.mergedCopyBtns[idx]
								} else {
									copyBtn = new(widget.Clickable)
								}
								isCopied := a.copiedHash == commit.ShortHash
								return a.drawCopyBadge(gtx, copyBtn, commit.Hash, commit.ShortHash, isCopied, pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Label(a.theme, unit.Sp(11), commit.AuthorName+":")
								lbl.Color = pal.TextMuted
								return lbl.Layout(gtx)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Label(a.theme, unit.Sp(11), commit.Message)
								lbl.Color = pal.Text
								return lbl.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								stats := formatDiffStats(commit.Additions, commit.Deletions)
								if stats == "" {
									return layout.Dimensions{}
								}
								lbl := material.Label(a.theme, unit.Sp(10), stats)
								lbl.Color = pal.TextMuted
								return lbl.Layout(gtx)
							}),
						)
					})
				})
			})
		}))
	}
	return children
}

func (a *App) layoutUpdatesAnalytics(gtx layout.Context, pal Palette) layout.Dimensions {
	a.mu.Lock()
	up := a.updatesData
	a.mu.Unlock()

	if up == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Label(a.theme, unit.Sp(12), "Loading analytics...")
			lbl.Color = pal.TextMuted
			return lbl.Layout(gtx)
		})
	}

	return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(4), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return drawWithBorder(gtx, pal.Surface, unit.Dp(8), unit.Dp(1), pal.Border, func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(a.theme, unit.Sp(14), "Activity Overview")
						lbl.Font.Weight = font.Bold
						lbl.Color = pal.Text
						return lbl.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
					// Metric Cards Row
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return a.drawMetricCard(gtx, "COMMITS", fmt.Sprintf("%d", up.TotalCommits), fmt.Sprintf("%d merges", up.TotalMerges), pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return a.drawMetricCard(gtx, "AI VS DEVS", fmt.Sprintf("%d / %d", up.AICommitsCount, up.HumanCommitsCount), "commits", pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								return a.drawMetricCard(gtx, "LINES", fmt.Sprintf("+%d -%d", up.TotalAdditions, up.TotalDeletions), "volume", pal)
							}),
						)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return drawDivider(gtx, pal.Border)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return a.drawSectionHeader(gtx, "ACTIVE CONTRIBUTORS", pal)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
					// Authors List
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return a.drawAuthorsList(gtx, up.Authors, pal)
					}),
				)
			})
		})
	})
}

func (a *App) drawAuthorsList(gtx layout.Context, authors []models.AuthorStat, pal Palette) layout.Dimensions {
	if len(authors) == 0 {
		return layout.Dimensions{}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		a.drawAuthorItems(gtx, authors, pal)...,
	)
}

func (a *App) drawAuthorItems(gtx layout.Context, authors []models.AuthorStat, pal Palette) []layout.FlexChild {
	var children []layout.FlexChild
	for _, aut := range authors {
		author := aut
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return drawWithBg(gtx, pal.Surface, func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Label(a.theme, unit.Sp(11.5), author.Name)
							lbl.Font.Weight = font.SemiBold
							lbl.Color = pal.Text
							return lbl.Layout(gtx)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if author.IsAI {
								return a.drawTag(gtx, "AI", pal.BadgeBg, pal.BadgeText)
							}
							return layout.Dimensions{}
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Label(a.theme, unit.Sp(11), fmt.Sprintf("%d commits  (+%d -%d)", author.CommitCount, author.Additions, author.Deletions))
							lbl.Color = pal.TextMuted
							return lbl.Layout(gtx)
						}),
					)
				})
			})
		}))
		children = append(children, layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout))
	}
	return children
}

func (a *App) layoutEmptyState(gtx layout.Context, pal Palette) layout.Dimensions {
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Label(a.theme, unit.Sp(14), "No repository opened")
				lbl.Color = pal.TextMuted
				return lbl.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return a.drawPillButton(gtx, &a.btnAddProject, "Open Folder...", true, pal)
			}),
		)
	})
}

// -------------------------------------------------------------
// UI Helpers
// -------------------------------------------------------------
func (a *App) createListStyle(state *widget.List, pal Palette) material.ListStyle {
	ls := material.List(a.theme, state)

	if state.Axis == layout.Horizontal {
		ls.AnchorStrategy = material.Overlay
		ls.Indicator.MinorWidth = unit.Dp(4)
		ls.Indicator.CornerRadius = unit.Dp(2)
		ls.Indicator.Color = pal.TextMuted
		ls.Indicator.HoverColor = pal.Accent
		ls.Track.Color = color.NRGBA{0, 0, 0, 0}
		return ls
	}

	// Vertical lists: Occupy strategy with dedicated gutter track and distinct visible thumb
	ls.AnchorStrategy = material.Occupy
	ls.Indicator.MinorWidth = unit.Dp(8)
	ls.Indicator.CornerRadius = unit.Dp(4)
	ls.Indicator.MajorMinLen = unit.Dp(36)
	ls.Track.MinorPadding = unit.Dp(2)
	ls.Track.MajorPadding = unit.Dp(2)

	if pal.IsDark {
		ls.Track.Color = color.NRGBA{R: 16, G: 20, B: 26, A: 255}
		ls.Indicator.Color = color.NRGBA{R: 90, G: 105, B: 125, A: 255}
		ls.Indicator.HoverColor = pal.Accent
	} else {
		ls.Track.Color = color.NRGBA{R: 238, G: 240, B: 244, A: 255}
		ls.Indicator.Color = color.NRGBA{R: 160, G: 170, B: 185, A: 255}
		ls.Indicator.HoverColor = pal.Accent
	}
	return ls
}

func formatDiffStats(adds, dels int) string {
	if adds == 0 && dels == 0 {
		return ""
	}
	if adds > 0 && dels == 0 {
		return fmt.Sprintf("+%d", adds)
	}
	if adds == 0 && dels > 0 {
		return fmt.Sprintf("-%d", dels)
	}
	return fmt.Sprintf("+%d -%d", adds, dels)
}

func (a *App) drawCopyBadge(gtx layout.Context, btn *widget.Clickable, hashToCopy string, displayLabel string, isCopied bool, pal Palette) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := pal.Surface
		fg := pal.TextMuted
		if isCopied {
			bg = pal.Selected
			fg = pal.Accent
		} else if btn.Hovered() {
			bg = pal.Hover
			fg = pal.Text
		}

		return drawWithRoundedBg(gtx, bg, unit.Dp(4), func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(7), Right: unit.Dp(7)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						text := displayLabel
						if isCopied {
							text = "Copied!"
						}
						lbl := material.Label(a.theme, unit.Sp(10), text)
						if isCopied {
							lbl.Font.Weight = font.Bold
							lbl.Color = pal.Accent
						} else {
							lbl.Color = fg
						}
						return lbl.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if isCopied {
							return a.icons.Layout(gtx, a.icons.Check, pal.Accent, unit.Dp(11))
						}
						return a.icons.Layout(gtx, a.icons.Copy, fg, unit.Dp(11))
					}),
				)
			})
		})
	})
}

func (a *App) drawSectionHeader(gtx layout.Context, title string, pal Palette) layout.Dimensions {
	lbl := material.Label(a.theme, unit.Sp(10), title)
	lbl.Font.Weight = font.Bold
	lbl.Color = pal.TextMuted
	return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2)}.Layout(gtx, lbl.Layout)
}

func (a *App) drawNavRow(gtx layout.Context, btn *widget.Clickable, title string, active bool, pal Palette) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := pal.HeaderBg
		fg := pal.TextMuted
		if active {
			bg = pal.Selected
			fg = pal.Accent
		} else if btn.Hovered() {
			bg = pal.Hover
			fg = pal.Text
		}

		return drawWithRoundedBg(gtx, bg, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Label(a.theme, unit.Sp(11), title)
				lbl.Color = fg
				if active {
					lbl.Font.Weight = font.Bold
				} else {
					lbl.Font.Weight = font.Medium
				}
				return lbl.Layout(gtx)
			})
		})
	})
}

func (a *App) drawPillButton(gtx layout.Context, btn *widget.Clickable, txt string, active bool, pal Palette) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := pal.Surface
		fg := pal.TextMuted
		if active {
			bg = pal.Accent
			fg = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
		} else if btn.Hovered() {
			bg = pal.Hover
			fg = pal.Text
		}

		return drawWithRoundedBg(gtx, bg, unit.Dp(5), func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(5), Bottom: unit.Dp(5), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Label(a.theme, unit.Sp(10.5), txt)
				lbl.Color = fg
				lbl.Font.Weight = font.Medium
				if active {
					lbl.Font.Weight = font.Bold
				}
				return lbl.Layout(gtx)
			})
		})
	})
}

func (a *App) drawIconPillButton(gtx layout.Context, btn *widget.Clickable, ic *widget.Icon, txt string, active bool, pal Palette) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := pal.Surface
		fg := pal.TextMuted
		if active {
			bg = pal.Accent
			fg = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
		} else if btn.Hovered() {
			bg = pal.Hover
			fg = pal.Text
		}

		return drawWithRoundedBg(gtx, bg, unit.Dp(5), func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(5), Bottom: unit.Dp(5), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if ic == nil {
							return layout.Dimensions{}
						}
						return a.icons.Layout(gtx, ic, fg, unit.Dp(11))
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if ic == nil || txt == "" {
							return layout.Dimensions{}
						}
						return layout.Spacer{Width: unit.Dp(6)}.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if txt == "" {
							return layout.Dimensions{}
						}
						lbl := material.Label(a.theme, unit.Sp(10.5), txt)
						lbl.Color = fg
						lbl.Font.Weight = font.Medium
						if active {
							lbl.Font.Weight = font.Bold
						}
						return lbl.Layout(gtx)
					}),
				)
			})
		})
	})
}

func (a *App) drawIconButton(gtx layout.Context, btn *widget.Clickable, ic *widget.Icon, active bool, pal Palette) layout.Dimensions {
	return btn.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := color.NRGBA{}
		fg := pal.TextMuted
		if active {
			bg = pal.Selected
			fg = pal.Accent
		} else if btn.Hovered() {
			bg = pal.Hover
			fg = pal.Text
		}

		return drawWithRoundedBg(gtx, bg, unit.Dp(5), func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(6)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return a.icons.Layout(gtx, ic, fg, unit.Dp(15))
			})
		})
	})
}

func (a *App) drawTag(gtx layout.Context, txt string, bg, fg color.NRGBA) layout.Dimensions {
	return drawWithRoundedBg(gtx, bg, unit.Dp(4), func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Label(a.theme, unit.Sp(9.5), txt)
			lbl.Color = fg
			lbl.Font.Weight = font.Bold
			return lbl.Layout(gtx)
		})
	})
}

func (a *App) drawMetricCard(gtx layout.Context, title, val, sub string, pal Palette) layout.Dimensions {
	return drawWithRoundedBg(gtx, pal.Surface, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(unit.Dp(14)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(a.theme, unit.Sp(10), title)
					lbl.Color = pal.TextMuted
					lbl.Font.Weight = font.Bold
					return lbl.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(3)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(a.theme, unit.Sp(16), val)
					lbl.Font.Weight = font.Bold
					lbl.Color = pal.Text
					return lbl.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(1)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(a.theme, unit.Sp(10), sub)
					lbl.Color = pal.TextMuted
					return lbl.Layout(gtx)
				}),
			)
		})
	})
}

func drawWithBg(gtx layout.Context, bg color.NRGBA, w layout.Widget) layout.Dimensions {
	m := op.Record(gtx.Ops)
	dims := w(gtx)
	call := m.Stop()

	paint.FillShape(gtx.Ops, bg, clip.Rect(image.Rect(0, 0, dims.Size.X, dims.Size.Y)).Op())
	call.Add(gtx.Ops)
	return dims
}

func drawWithRoundedBg(gtx layout.Context, bg color.NRGBA, radius unit.Dp, w layout.Widget) layout.Dimensions {
	m := op.Record(gtx.Ops)
	dims := w(gtx)
	call := m.Stop()

	r := gtx.Dp(radius)
	paint.FillShape(gtx.Ops, bg, clip.RRect{
		Rect: image.Rect(0, 0, dims.Size.X, dims.Size.Y),
		SE:   r, SW: r, NW: r, NE: r,
	}.Op(gtx.Ops))
	call.Add(gtx.Ops)
	return dims
}

func drawWithBgAndLeftBorder(gtx layout.Context, bg color.NRGBA, borderCol color.NRGBA, borderW int, w layout.Widget) layout.Dimensions {
	m := op.Record(gtx.Ops)
	dims := w(gtx)
	call := m.Stop()

	paint.FillShape(gtx.Ops, bg, clip.Rect(image.Rect(0, 0, dims.Size.X, dims.Size.Y)).Op())
	if borderW > 0 {
		paint.FillShape(gtx.Ops, borderCol, clip.Rect(image.Rect(0, 0, borderW, dims.Size.Y)).Op())
	}
	call.Add(gtx.Ops)
	return dims
}

func drawWithRoundedBgAndLeftBorder(gtx layout.Context, bg color.NRGBA, borderCol color.NRGBA, borderW int, radius unit.Dp, w layout.Widget) layout.Dimensions {
	m := op.Record(gtx.Ops)
	dims := w(gtx)
	call := m.Stop()

	r := gtx.Dp(radius)
	paint.FillShape(gtx.Ops, bg, clip.RRect{
		Rect: image.Rect(0, 0, dims.Size.X, dims.Size.Y),
		SE:   r, SW: r, NW: r, NE: r,
	}.Op(gtx.Ops))
	if borderW > 0 {
		paint.FillShape(gtx.Ops, borderCol, clip.RRect{
			Rect: image.Rect(0, 0, borderW, dims.Size.Y),
			NW:   r, SW: r,
		}.Op(gtx.Ops))
	}
	call.Add(gtx.Ops)
	return dims
}

func drawWithBorder(gtx layout.Context, bg color.NRGBA, radius unit.Dp, borderW unit.Dp, borderCol color.NRGBA, w layout.Widget) layout.Dimensions {
	m := op.Record(gtx.Ops)
	dims := w(gtx)
	call := m.Stop()

	r := gtx.Dp(radius)
	rr := clip.RRect{
		Rect: image.Rect(0, 0, dims.Size.X, dims.Size.Y),
		SE:   r, SW: r, NW: r, NE: r,
	}
	paint.FillShape(gtx.Ops, bg, rr.Op(gtx.Ops))
	bw := float32(gtx.Dp(borderW))
	if bw > 0 {
		paint.FillShape(gtx.Ops, borderCol, clip.Stroke{Path: rr.Path(gtx.Ops), Width: bw}.Op())
	}
	call.Add(gtx.Ops)
	return dims
}

func drawDiffCardTopHeader(gtx layout.Context, bg, borderCol color.NRGBA, radius unit.Dp, w layout.Widget) layout.Dimensions {
	m := op.Record(gtx.Ops)
	dims := w(gtx)
	call := m.Stop()

	if dims.Size.X > 0 && dims.Size.Y > 0 {
		r := gtx.Dp(radius)
		rr := clip.RRect{
			Rect: image.Rect(0, 0, dims.Size.X, dims.Size.Y),
			NW:   r, NE: r,
			SW:   0, SE: 0,
		}
		paint.FillShape(gtx.Ops, bg, rr.Op(gtx.Ops))
		bw := float32(gtx.Dp(unit.Dp(1)))
		if bw < 1 {
			bw = 1
		}
		paint.FillShape(gtx.Ops, borderCol, clip.Stroke{Path: rr.Path(gtx.Ops), Width: bw}.Op())
	}
	call.Add(gtx.Ops)
	return dims
}

func drawDiffCardLine(gtx layout.Context, bg, borderCol color.NRGBA, isLastLine bool, radius unit.Dp, w layout.Widget) layout.Dimensions {
	m := op.Record(gtx.Ops)
	dims := w(gtx)
	call := m.Stop()

	if dims.Size.X > 0 && dims.Size.Y > 0 {
		if isLastLine {
			r := gtx.Dp(radius)
			rr := clip.RRect{
				Rect: image.Rect(0, 0, dims.Size.X, dims.Size.Y),
				NW:   0, NE: 0,
				SW:   r, SE: r,
			}
			paint.FillShape(gtx.Ops, bg, rr.Op(gtx.Ops))
			bw := float32(gtx.Dp(unit.Dp(1)))
			if bw < 1 {
				bw = 1
			}
			paint.FillShape(gtx.Ops, borderCol, clip.Stroke{Path: rr.Path(gtx.Ops), Width: bw}.Op())
		} else {
			paint.FillShape(gtx.Ops, bg, clip.Rect(image.Rect(0, 0, dims.Size.X, dims.Size.Y)).Op())
			lineW := gtx.Dp(unit.Dp(1))
			if lineW < 1 {
				lineW = 1
			}
			// Left border
			paint.FillShape(gtx.Ops, borderCol, clip.Rect(image.Rect(0, 0, lineW, dims.Size.Y)).Op())
			// Right border
			paint.FillShape(gtx.Ops, borderCol, clip.Rect(image.Rect(dims.Size.X-lineW, 0, dims.Size.X, dims.Size.Y)).Op())
		}
	}
	call.Add(gtx.Ops)
	return dims
}

func drawDivider(gtx layout.Context, c color.NRGBA) layout.Dimensions {
	h := gtx.Dp(unit.Dp(1))
	w := gtx.Constraints.Max.X
	paint.FillShape(gtx.Ops, c, clip.Rect(image.Rect(0, 0, w, h)).Op())
	return layout.Dimensions{Size: image.Pt(w, h)}
}

func drawVerticalDivider(gtx layout.Context, c color.NRGBA) layout.Dimensions {
	w := gtx.Dp(unit.Dp(1))
	h := gtx.Constraints.Max.Y
	paint.FillShape(gtx.Ops, c, clip.Rect(image.Rect(0, 0, w, h)).Op())
	return layout.Dimensions{Size: image.Pt(w, h)}
}

func formatRelativeTime(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "yesterday"
	}
	if days < 30 {
		return fmt.Sprintf("%dd ago", days)
	}
	return t.Format("02 Jan 2006")
}

func openFolderDialog() string {
	cmd := exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command",
		`[System.Reflection.Assembly]::LoadWithPartialName("System.windows.forms") | Out-Null; $d = New-Object System.Windows.Forms.FolderBrowserDialog; $d.Description = "Select Git Repository Folder"; if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { Write-Host -NoNewline $d.SelectedPath }`)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// -------------------------------------------------------------
// Git Operations & Personal Office Helpers
// -------------------------------------------------------------

func (a *App) runGitCommit(msg string, amend bool, andPush bool) {
	if a.activeRepo == "" {
		return
	}
	msg = strings.TrimSpace(msg)
	if msg == "" {
		a.mu.Lock()
		a.gitOpStatus = "Error: Commit message cannot be empty."
		a.gitOpIsError = true
		a.gitOpTime = time.Now()
		a.mu.Unlock()
		if a.window != nil {
			a.window.Invalidate()
		}
		return
	}

	a.mu.Lock()
	a.gitOpStatus = "Creating commit..."
	a.gitOpIsError = false
	a.gitOpTime = time.Now()
	a.mu.Unlock()
	if a.window != nil {
		a.window.Invalidate()
	}

	go func() {
		out, err := a.gitSvc.CreateCommit(a.activeRepo, msg, amend)
		a.mu.Lock()
		if err != nil {
			a.gitOpStatus = "Commit failed: " + err.Error() + "\n" + out
			a.gitOpIsError = true
		} else {
			a.gitOpStatus = "Committed successfully!\n" + out
			a.gitOpIsError = false
			a.commitMsgEditor.SetText("")
		}
		a.gitOpTime = time.Now()
		a.mu.Unlock()

		if err == nil && andPush {
			a.runGitPush()
			return
		}

		a.loadData()
		if a.window != nil {
			a.window.Invalidate()
		}
	}()
}

func (a *App) runGitPush() {
	if a.activeRepo == "" {
		return
	}
	a.mu.Lock()
	a.gitOpStatus = "Pushing changes to remote..."
	a.gitOpIsError = false
	a.gitOpTime = time.Now()
	a.mu.Unlock()
	if a.window != nil {
		a.window.Invalidate()
	}

	go func() {
		out, err := a.gitSvc.Push(a.activeRepo, "origin", a.activeBranch, true)
		a.mu.Lock()
		if err != nil {
			a.gitOpStatus = "Push failed: " + err.Error() + "\n" + out
			a.gitOpIsError = true
		} else {
			a.gitOpStatus = "Pushed to remote successfully!\n" + out
			a.gitOpIsError = false
		}
		a.gitOpTime = time.Now()
		a.mu.Unlock()
		a.loadData()
		if a.window != nil {
			a.window.Invalidate()
		}
	}()
}

func (a *App) runGitPull() {
	if a.activeRepo == "" {
		return
	}
	a.mu.Lock()
	a.gitOpStatus = "Pulling latest updates from remote..."
	a.gitOpIsError = false
	a.gitOpTime = time.Now()
	a.mu.Unlock()
	if a.window != nil {
		a.window.Invalidate()
	}

	go func() {
		out, err := a.gitSvc.Pull(a.activeRepo)
		a.mu.Lock()
		if err != nil {
			a.gitOpStatus = "Pull failed: " + err.Error() + "\n" + out
			a.gitOpIsError = true
		} else {
			a.gitOpStatus = "Pulled successfully!\n" + out
			a.gitOpIsError = false
		}
		a.gitOpTime = time.Now()
		a.mu.Unlock()
		a.loadData()
		if a.window != nil {
			a.window.Invalidate()
		}
	}()
}

func (a *App) runGitFetch() {
	if a.activeRepo == "" {
		return
	}
	a.mu.Lock()
	a.gitOpStatus = "Fetching all remotes & branches..."
	a.gitOpIsError = false
	a.gitOpTime = time.Now()
	a.mu.Unlock()
	if a.window != nil {
		a.window.Invalidate()
	}

	go func() {
		out, err := a.gitSvc.Fetch(a.activeRepo)
		a.mu.Lock()
		if err != nil {
			a.gitOpStatus = "Fetch failed: " + err.Error() + "\n" + out
			a.gitOpIsError = true
		} else {
			a.gitOpStatus = "Fetch completed!\n" + out
			a.gitOpIsError = false
		}
		a.gitOpTime = time.Now()
		a.mu.Unlock()
		a.loadData()
		if a.window != nil {
			a.window.Invalidate()
		}
	}()
}

func (a *App) runGitStash() {
	if a.activeRepo == "" {
		return
	}
	a.mu.Lock()
	a.gitOpStatus = "Stashing changes..."
	a.gitOpIsError = false
	a.gitOpTime = time.Now()
	a.mu.Unlock()
	if a.window != nil {
		a.window.Invalidate()
	}

	go func() {
		tag := "sCodeViewer " + time.Now().Format("2006-01-02 15:04:05")
		out, err := a.gitSvc.Stash(a.activeRepo, tag)
		a.mu.Lock()
		if err != nil {
			a.gitOpStatus = "Stash failed: " + err.Error() + "\n" + out
			a.gitOpIsError = true
		} else {
			a.gitOpStatus = "Stashed changes successfully!\n" + out
			a.gitOpIsError = false
		}
		a.gitOpTime = time.Now()
		a.mu.Unlock()
		a.loadData()
		if a.window != nil {
			a.window.Invalidate()
		}
	}()
}

func (a *App) runGitStashPop() {
	if a.activeRepo == "" {
		return
	}
	a.mu.Lock()
	a.gitOpStatus = "Applying latest stash..."
	a.gitOpIsError = false
	a.gitOpTime = time.Now()
	a.mu.Unlock()
	if a.window != nil {
		a.window.Invalidate()
	}

	go func() {
		out, err := a.gitSvc.StashPop(a.activeRepo)
		a.mu.Lock()
		if err != nil {
			a.gitOpStatus = "Stash pop failed: " + err.Error() + "\n" + out
			a.gitOpIsError = true
		} else {
			a.gitOpStatus = "Stash popped successfully!\n" + out
			a.gitOpIsError = false
		}
		a.gitOpTime = time.Now()
		a.mu.Unlock()
		a.loadData()
		if a.window != nil {
			a.window.Invalidate()
		}
	}()
}

func (a *App) layoutCommitActionBox(gtx layout.Context, pal Palette) layout.Dimensions {
	a.mu.Lock()
	authorName := a.gitUser.Name
	authorEmail := a.gitUser.Email
	statusText := a.gitOpStatus
	statusIsErr := a.gitOpIsError
	a.mu.Unlock()

	return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(8), Left: unit.Dp(6), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return drawWithBorder(gtx, pal.Surface, unit.Dp(8), unit.Dp(1), pal.Border, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(14), Right: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					// Row 1: Header + Author identity tag
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Label(a.theme, unit.Sp(12), "NEW COMMIT")
								lbl.Font.Weight = font.Bold
								lbl.Color = pal.Accent
								return lbl.Layout(gtx)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								authorStr := "Author: not configured"
								if authorName != "" {
									authorStr = fmt.Sprintf("Author: %s <%s>", authorName, authorEmail)
								}
								lbl := material.Label(a.theme, unit.Sp(10.5), authorStr)
								lbl.Color = pal.TextMuted
								return lbl.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return a.drawIconPillButton(gtx, &a.btnOpenProfileFromDiff, a.icons.Account, "Profile / Switch", false, pal)
							}),
						)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

					// Row 2: Commit message input box
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						ed := material.Editor(a.theme, &a.commitMsgEditor, "Commit message summary (e.g. feat: add new 3D graph viewer)...")
						ed.TextSize = unit.Sp(11.5)
						ed.Color = pal.Text
						ed.HintColor = pal.TextMuted

						return drawWithRoundedBg(gtx, pal.Bg, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
							return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(48))
								return ed.Layout(gtx)
							})
						})
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

					// Row 3: Actions row (Amend checkbox + Stash + Commit + Commit & Push)
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								cb := material.CheckBox(a.theme, &a.commitAmendBool, "Amend (--amend)")
								cb.Color = pal.Text
								cb.IconColor = pal.Accent
								cb.TextSize = unit.Sp(11)
								return cb.Layout(gtx)
							}),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return a.drawIconPillButton(gtx, &a.btnDoStashUncommitted, a.icons.Folder, "Stash", false, pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return a.drawIconPillButton(gtx, &a.btnDoCommit, a.icons.Check, "Commit", false, pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return a.drawIconPillButton(gtx, &a.btnDoCommitAndPush, a.icons.Push, "Commit & Push", true, pal)
							}),
						)
					}),

					// Status Banner if any
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if statusText == "" {
							return layout.Dimensions{}
						}
						bg := pal.Selected
						fg := pal.Accent
						if statusIsErr {
							bg = pal.DelBg
							fg = pal.DelText
						}
						return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return drawWithRoundedBg(gtx, bg, unit.Dp(4), func(gtx layout.Context) layout.Dimensions {
								return layout.UniformInset(unit.Dp(6)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(a.theme, unit.Sp(10.5), statusText)
									lbl.Color = fg
									return lbl.Layout(gtx)
								})
							})
						})
					}),
				)
			})
		})
	})
}

func (a *App) layoutProfileModalOverlay(gtx layout.Context, pal Palette) layout.Dimensions {
	gtx.Constraints.Min = gtx.Constraints.Max
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			dims := layout.Dimensions{Size: gtx.Constraints.Max}
			backdropCol := color.NRGBA{R: 0, G: 0, B: 0, A: 175}
			paint.FillShape(gtx.Ops, backdropCol, clip.Rect(image.Rect(0, 0, dims.Size.X, dims.Size.Y)).Op())
			return a.btnBackdropProfile.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return dims
			})
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min = gtx.Constraints.Max
			return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return a.btnCardProfile.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return a.layoutProfileModalCard(gtx, pal)
				})
			})
		}),
	)
}

func (a *App) layoutProfileModalCard(gtx layout.Context, pal Palette) layout.Dimensions {
	a.mu.Lock()
	repoName := a.repoName
	activeBranch := a.activeBranch
	statusText := a.gitOpStatus
	statusIsErr := a.gitOpIsError
	a.mu.Unlock()

	gtx.Constraints.Min.X = gtx.Dp(unit.Dp(520))
	gtx.Constraints.Max.X = gtx.Dp(unit.Dp(520))

	return drawWithBorder(gtx, pal.Surface, unit.Dp(10), unit.Dp(1), pal.Border, func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(unit.Dp(20)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				// Header
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return a.icons.Layout(gtx, a.icons.Account, pal.Accent, unit.Dp(22))
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(a.theme, unit.Sp(14), "Git Commit Author / Автор коммитов")
									lbl.Font.Weight = font.Bold
									lbl.Color = pal.Text
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(a.theme, unit.Sp(10.5), fmt.Sprintf("Repo: %s  •  Branch: %s", repoName, activeBranch))
									lbl.Color = pal.TextMuted
									return lbl.Layout(gtx)
								}),
							)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return a.drawIconButton(gtx, &a.btnCloseProfile, a.icons.Close, false, pal)
						}),
					)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return drawDivider(gtx, pal.Border)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(14)}.Layout),

				// Section: Commit Author Identity
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return a.drawSectionHeader(gtx, "AUTHOR IDENTITY (user.name & user.email)", pal)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						// Name field
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(a.theme, unit.Sp(10), "NAME (user.name)")
									lbl.Font.Weight = font.SemiBold
									lbl.Color = pal.TextMuted
									return lbl.Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									ed := material.Editor(a.theme, &a.profileNameEditor, "Author Name")
									ed.TextSize = unit.Sp(11)
									ed.Color = pal.Text
									return drawWithRoundedBg(gtx, pal.Bg, unit.Dp(4), func(gtx layout.Context) layout.Dimensions {
										return layout.UniformInset(unit.Dp(7)).Layout(gtx, ed.Layout)
									})
								}),
							)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
						// Email field
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(a.theme, unit.Sp(10), "EMAIL (user.email)")
									lbl.Font.Weight = font.SemiBold
									lbl.Color = pal.TextMuted
									return lbl.Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									ed := material.Editor(a.theme, &a.profileEmailEditor, "Author Email")
									ed.TextSize = unit.Sp(11)
									ed.Color = pal.Text
									return drawWithRoundedBg(gtx, pal.Bg, unit.Dp(4), func(gtx layout.Context) layout.Dimensions {
										return layout.UniformInset(unit.Dp(7)).Layout(gtx, ed.Layout)
									})
								}),
							)
						}),
					)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),
				// Save Buttons Row
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return a.drawIconPillButton(gtx, &a.btnSaveProfileLocal, a.icons.Save, "Save for Current Repository", true, pal)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return a.drawIconPillButton(gtx, &a.btnSaveProfileGlobal, a.icons.Save, "Save Globally (~/.gitconfig)", false, pal)
						}),
					)
				}),

				// Status output banner
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if statusText == "" {
						return layout.Dimensions{}
					}
					bg := pal.Selected
					fg := pal.Accent
					if statusIsErr {
						bg = pal.DelBg
						fg = pal.DelText
					}
					return layout.Inset{Top: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return drawWithRoundedBg(gtx, bg, unit.Dp(6), func(gtx layout.Context) layout.Dimensions {
							return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Label(a.theme, unit.Sp(11), statusText)
								lbl.Color = fg
								return lbl.Layout(gtx)
							})
						})
					})
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(16)}.Layout),
				// Done / Close button
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return a.drawPillButton(gtx, &a.btnCloseProfile, "Done", true, pal)
						}),
					)
				}),
			)
		})
	})
}
