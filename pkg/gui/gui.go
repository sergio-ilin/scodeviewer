package gui

import (
	"fmt"
	"image"
	"image/color"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/font/gofont"
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

type App struct {
	window       *app.Window
	theme        *material.Theme
	gitSvc       *git.Service
	cfgMgr       *config.Manager
	rootPath     string
	activeRepo   string
	repoName     string
	activeTab    string // "commits", "updates"
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
	countMerges int
	countDirect int
	countAI     int

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
	projectClicks   []widget.Clickable
	projectRenameBtns []widget.Clickable
	projectDeleteBtns []widget.Clickable
	subRepoClicks   []widget.Clickable
	branchClicks    []widget.Clickable
	projectsList    widget.List
	branchesList    widget.List
	subReposList    widget.List

	// UI Widgets - Middle Panel
	btnFilterAll    widget.Clickable
	btnFilterMerges widget.Clickable
	btnFilterDirect widget.Clickable
	btnFilterAI     widget.Clickable
	commitsList     widget.List
	commitClicks    []widget.Clickable

	// UI Widgets - Right Panel
	mergedClicks   []widget.Clickable
	diffList       widget.List
	updatesList    widget.List
	authorClicks   []widget.Clickable
	timelineClicks []widget.Clickable

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
		if target == "" && cfg.ActivePath != "" && gitSvc.IsGitRepo(cfg.ActivePath) {
			target = cfg.ActivePath
		}
	}

	a := &App{
		theme:         th,
		gitSvc:        gitSvc,
		cfgMgr:        cfgMgr,
		rootPath:      target,
		activeRepo:    target,
		projects:      savedProjects,
		activeTab:     "commits",
		commitFilter:  "all",
		timePeriod:    "7d",
		aiFilter:      "all",
		projectsList:  widget.List{List: layout.List{Axis: layout.Vertical}},
		branchesList:  widget.List{List: layout.List{Axis: layout.Vertical}},
		subReposList:  widget.List{List: layout.List{Axis: layout.Vertical}},
		commitsList:   widget.List{List: layout.List{Axis: layout.Vertical}},
		diffList:      widget.List{List: layout.List{Axis: layout.Vertical}},
		updatesList:   widget.List{List: layout.List{Axis: layout.Vertical}},
	}

	if target != "" {
		a.repoName = filepath.Base(target)
		if cfgMgr != nil {
			cfgMgr.AddProject(target, true)
			a.projects = cfgMgr.GetConfig().Projects
		}
	}

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
	a.branches = branches
	a.branchClicks = make([]widget.Clickable, len(branches))

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
		} else {
			detail, err := a.gitSvc.GetCommitDetail(a.activeRepo, a.selectedHash)
			if err == nil {
				a.selectedDetail = detail
				a.mergedClicks = make([]widget.Clickable, len(detail.MergedCommits))
			}
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

	allCommits, err := a.gitSvc.GetCommits(a.activeRepo, a.activeBranch, "", "", false, 250, 0)
	if err == nil {
		a.countAll = len(allCommits)
		if a.uncommitted != nil {
			a.countAll++
		}
		a.countMerges = 0
		a.countDirect = 0
		a.countAI = 0

		var filtered []models.CommitSummary

		// Put uncommitted changes at top if matches filter
		if a.uncommitted != nil && (a.commitFilter == "all" || a.commitFilter == "direct") {
			filtered = append(filtered, a.uncommitted.CommitSummary)
		}

		for _, c := range allCommits {
			if c.IsMerge {
				a.countMerges++
			} else {
				a.countDirect++
			}
			if c.IsAI {
				a.countAI++
			}

			// Apply active filter
			switch a.commitFilter {
			case "merges":
				if c.IsMerge {
					filtered = append(filtered, c)
				}
			case "direct":
				if !c.IsMerge {
					filtered = append(filtered, c)
				}
			case "ai":
				if c.IsAI {
					filtered = append(filtered, c)
				}
			default:
				filtered = append(filtered, c)
			}
		}

		a.commits = filtered
		a.commitClicks = make([]widget.Clickable, len(filtered))

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
			} else {
				detail, errDetail := a.gitSvc.GetCommitDetail(a.activeRepo, filtered[0].Hash)
				if errDetail == nil {
					a.selectedDetail = detail
					a.mergedClicks = make([]widget.Clickable, len(detail.MergedCommits))
				}
			}
		} else if len(filtered) == 0 {
			a.selectedHash = ""
			a.selectedDetail = nil
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
		a.mergedClicks = make([]widget.Clickable, len(detail.MergedCommits))
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

	// Commit filters
	if a.btnFilterAll.Clicked(gtx) {
		a.commitFilter = "all"
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
	}

	// Timeline clicks
	if a.updatesData != nil {
		for i := range a.timelineClicks {
			if i < len(a.updatesData.Commits) && a.timelineClicks[i].Clicked(gtx) {
				hash := a.updatesData.Commits[i].Hash
				a.selectedHash = hash
				a.activeTab = "commits"
				go a.loadCommitDetail(hash)
				break
			}
		}
	}
	a.mu.Unlock()
}

func (a *App) layout(gtx layout.Context) layout.Dimensions {
	pal := GetPalette(ThemeAuto)

	// Clip & Fill window background
	paint.FillShape(gtx.Ops, pal.Bg, clip.Rect(image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Max.Y)).Op())

	if a.activeRepo == "" {
		return a.layoutEmptyState(gtx, pal)
	}

	// Professional 3-Panel Architecture:
	// Panel 1 (Left 250dp): Persistent Workspaces + Sub-repos + Branches + Views
	// Panel 2 (Middle 360dp): Commits & Merges with Uncommitted Changes + Counts
	// Panel 3 (Right flexed): Diff Viewer / Merged Commits / Analytics
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		// Panel 1: Left Navigation Sidebar
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(unit.Dp(250))
			gtx.Constraints.Max.X = gtx.Dp(unit.Dp(250))
			return a.layoutLeftSidebar(gtx, pal)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return drawVerticalDivider(gtx, pal.Border)
		}),
		// Panel 2: Middle Panel (Commits List or Updates List)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(unit.Dp(360))
			gtx.Constraints.Max.X = gtx.Dp(unit.Dp(360))
			return a.layoutCenterPanel(gtx, pal)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return drawVerticalDivider(gtx, pal.Border)
		}),
		// Panel 3: Right Panel (Diff / Merge Breakdown / Analytics)
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return a.layoutRightPanel(gtx, pal)
		}),
	)
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
	a.mu.Unlock()

	searchProj := strings.ToLower(strings.TrimSpace(a.searchProjectsEditor.Text()))
	searchBranch := strings.ToLower(strings.TrimSpace(a.searchBranchesEditor.Text()))

	return drawWithBg(gtx, pal.HeaderBg, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(10), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				// App Brand
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							lbl := material.Label(a.theme, unit.Sp(13.5), "sCodeViewer")
							lbl.Color = pal.Text
							lbl.Font.Weight = font.Bold
							return lbl.Layout(gtx)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return a.drawPillButton(gtx, &a.btnAddProject, "+ Add Repo", false, pal)
						}),
					)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return drawDivider(gtx, pal.Border)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

				// VIEW SWITCHER
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return a.drawSectionHeader(gtx, "VIEWS", pal)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return a.drawNavRow(gtx, &a.btnTabCommits, "Commits & Merges", a.activeTab == "commits", pal)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(2)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return a.drawNavRow(gtx, &a.btnTabUpdates, "Activity & Updates", a.activeTab == "updates", pal)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return drawDivider(gtx, pal.Border)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

				// PERSISTENT PROJECTS / WORKSPACES SECTION
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return a.drawSectionHeader(gtx, fmt.Sprintf("PROJECTS (%d)", len(projects)), pal)
						}),
					)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),

				// Search Projects Input (if multiple projects)
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if len(projects) <= 3 {
						return layout.Dimensions{}
					}
					ed := material.Editor(a.theme, &a.searchProjectsEditor, "Filter projects...")
					ed.TextSize = unit.Sp(10.5)
					ed.Color = pal.Text
					ed.HintColor = pal.TextMuted
					return drawWithBg(gtx, pal.Surface, func(gtx layout.Context) layout.Dimensions {
						return layout.UniformInset(unit.Dp(4)).Layout(gtx, ed.Layout)
					})
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),

				// Projects List (with active highlight, rename, delete)
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return a.createListStyle(&a.projectsList, pal).Layout(gtx, len(projects), func(gtx layout.Context, idx int) layout.Dimensions {
						p := projects[idx]
						if searchProj != "" && !strings.Contains(strings.ToLower(p.Name), searchProj) && !strings.Contains(strings.ToLower(p.Path), searchProj) {
							return layout.Dimensions{}
						}

						isActive := p.Path == rootPath

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

						return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							bg := pal.HeaderBg
							fg := pal.Text
							if isActive {
								bg = pal.Selected
								fg = pal.Accent
							} else if click.Hovered() {
								bg = pal.Hover
							}

							return drawWithBg(gtx, bg, func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											lbl := material.Label(a.theme, unit.Sp(11), p.Name)
											lbl.Color = fg
											if isActive {
												lbl.Font.Weight = font.Bold
											} else {
												lbl.Font.Weight = font.Medium
											}
											return lbl.Layout(gtx)
										}),
										layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
										// Rename button [Edit]
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return btnRename.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												lbl := material.Label(a.theme, unit.Sp(9), "edit")
												lbl.Color = pal.TextMuted
												return lbl.Layout(gtx)
											})
										}),
										layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
										// Delete button [x]
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											return btnDelete.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
												lbl := material.Label(a.theme, unit.Sp(10), "✕")
												lbl.Color = pal.TextMuted
												return lbl.Layout(gtx)
											})
										}),
									)
								})
							})
						})
					})
				}),

				// NESTED SUB-REPOSITORIES (if active project has nested repos)
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if len(subRepos) <= 1 {
						return layout.Dimensions{}
					}
					return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return a.drawSectionHeader(gtx, fmt.Sprintf("SUB-REPOSITORIES (%d)", len(subRepos)), pal)
							}),
							layout.Rigid(layout.Spacer{Height: unit.Dp(3)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return a.createListStyle(&a.subReposList, pal).Layout(gtx, len(subRepos), func(gtx layout.Context, idx int) layout.Dimensions {
									r := subRepos[idx]
									isActive := r.Path == activeRepo
									var click *widget.Clickable
									if idx < len(a.subRepoClicks) {
										click = &a.subRepoClicks[idx]
									} else {
										click = new(widget.Clickable)
									}

									name := r.Name
									if !r.IsRoot && r.RelPath != "" {
										name = "└ " + r.RelPath
									}

									return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										return a.drawNavRow(gtx, click, name, isActive, pal)
									})
								})
							}),
						)
					})
				}),

				layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return drawDivider(gtx, pal.Border)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

				// BRANCHES SECTION (Searchable, with Author and Last Update)
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return a.drawSectionHeader(gtx, fmt.Sprintf("BRANCHES (%d)", len(branches)), pal)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),

				// Search Branches Input
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					ed := material.Editor(a.theme, &a.searchBranchesEditor, "Filter branches...")
					ed.TextSize = unit.Sp(10.5)
					ed.Color = pal.Text
					ed.HintColor = pal.TextMuted
					return drawWithBg(gtx, pal.Surface, func(gtx layout.Context) layout.Dimensions {
						return layout.UniformInset(unit.Dp(4)).Layout(gtx, ed.Layout)
					})
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),

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
						return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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

						return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							bg := pal.HeaderBg
							if isActive {
								bg = pal.Selected
							} else if click.Hovered() {
								bg = pal.Hover
							}

							return drawWithBg(gtx, bg, func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
										// Row 1: Branch name with active indicator
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											prefix := "  "
											if b.IsCurrent {
												prefix = "● "
											}
											lbl := material.Label(a.theme, unit.Sp(11), prefix+b.Name)
											if isActive {
												lbl.Font.Weight = font.Bold
												lbl.Color = pal.Accent
											} else {
												lbl.Font.Weight = font.Medium
												lbl.Color = pal.Text
											}
											return lbl.Layout(gtx)
										}),
										// Row 2: Author and Last Update Time!
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											if b.LastAuthor == "" && b.LastUpdate == "" {
												return layout.Dimensions{}
											}
											sub := b.LastAuthor
											if b.LastUpdate != "" {
												if sub != "" {
													sub += " • "
												}
												sub += b.LastUpdate
											}
											lbl := material.Label(a.theme, unit.Sp(9.5), "    "+sub)
											lbl.Color = pal.TextMuted
											return lbl.Layout(gtx)
										}),
									)
								})
							})
						})
					})
				}),
			)
		})
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
	cMerges := a.countMerges
	cDirect := a.countDirect
	cAI := a.countAI
	a.mu.Unlock()

	search := strings.ToLower(strings.TrimSpace(a.searchCommitsEditor.Text()))

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header info bar: Active Branch & Search
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return drawWithBg(gtx, pal.Surface, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						// Current branch indicator
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(a.theme, unit.Sp(11), "Branch:")
									lbl.Color = pal.TextMuted
									return lbl.Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(a.theme, unit.Sp(12), activeBranch)
									lbl.Font.Weight = font.Bold
									lbl.Color = pal.Text
									return lbl.Layout(gtx)
								}),
							)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
						// Search input
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							ed := material.Editor(a.theme, &a.searchCommitsEditor, "Search commits or hash...")
							ed.TextSize = unit.Sp(11.5)
							ed.Color = pal.Text
							ed.HintColor = pal.TextMuted
							return drawWithBg(gtx, pal.Bg, func(gtx layout.Context) layout.Dimensions {
								return layout.UniformInset(unit.Dp(6)).Layout(gtx, ed.Layout)
							})
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
						// Filter chips with exact counts!
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return a.drawPillButton(gtx, &a.btnFilterAll, fmt.Sprintf("All (%d)", cAll), a.commitFilter == "all", pal)
								}),
								layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return a.drawPillButton(gtx, &a.btnFilterMerges, fmt.Sprintf("Merges (%d)", cMerges), a.commitFilter == "merges", pal)
								}),
								layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return a.drawPillButton(gtx, &a.btnFilterDirect, fmt.Sprintf("Direct (%d)", cDirect), a.commitFilter == "direct", pal)
								}),
								layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return a.drawPillButton(gtx, &a.btnFilterAI, fmt.Sprintf("AI (%d)", cAI), a.commitFilter == "ai", pal)
								}),
							)
						}),
					)
				})
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return drawDivider(gtx, pal.Border)
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

				return click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					bg := pal.Bg
					if isSelected {
						bg = pal.Selected
					} else if click.Hovered() {
						bg = pal.Hover
					}

					return drawWithBg(gtx, bg, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{
							Top:    unit.Dp(7),
							Bottom: unit.Dp(7),
							Left:   unit.Dp(10),
							Right:  unit.Dp(10),
						}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								// Author + Time
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
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
								layout.Rigid(layout.Spacer{Height: unit.Dp(3)}.Layout),
								// Message
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									msg := c.Message
									if len(msg) > 75 {
										msg = msg[:75] + "..."
									}
									lbl := material.Label(a.theme, unit.Sp(11.5), msg)
									lbl.Color = pal.Text
									if isUncommitted {
										lbl.Font.Weight = font.Bold
									}
									return lbl.Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
								// Badges + Stats
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											if isUncommitted {
												return a.drawTag(gtx, "UNCOMMITTED", pal.AddBg, pal.AddText)
											}
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
											lbl := material.Label(a.theme, unit.Sp(10.5), fmt.Sprintf("+%d -%d", c.Additions, c.Deletions))
											lbl.Color = pal.TextMuted
											return lbl.Layout(gtx)
										}),
										layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											lbl := material.Label(a.theme, unit.Sp(10.5), c.ShortHash)
											lbl.Color = pal.TextMuted
											return lbl.Layout(gtx)
										}),
									)
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
		// Time Slices Header
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return drawWithBg(gtx, pal.Surface, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return drawDivider(gtx, pal.Border)
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

					return drawWithBg(gtx, bg, func(gtx layout.Context) layout.Dimensions {
						return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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
								layout.Rigid(layout.Spacer{Height: unit.Dp(2)}.Layout),
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
		// Header summary
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return drawWithBg(gtx, pal.Surface, func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						// Subject & Tag
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									if isUncommitted {
										return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return a.drawTag(gtx, "UNCOMMITTED CHANGES", pal.AddBg, pal.AddText)
										})
									}
									if detail.IsMerge {
										return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
											return a.drawTag(gtx, "MERGE COMMIT", pal.Accent, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
										})
									}
									return layout.Dimensions{}
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(a.theme, unit.Sp(13.5), detail.Message)
									lbl.Font.Weight = font.Bold
									lbl.Color = pal.Text
									return lbl.Layout(gtx)
								}),
							)
						}),
						layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
						// Metadata row
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							dateStr := formatRelativeTime(detail.Date)
							if isUncommitted {
								dateStr = "Current working directory"
							}
							meta := fmt.Sprintf("%s  •  %s  •  %s  •  Files: %d  •  +%d -%d",
								detail.AuthorName,
								dateStr,
								detail.ShortHash,
								len(detail.Files),
								detail.Additions,
								detail.Deletions,
							)
							lbl := material.Label(a.theme, unit.Sp(11), meta)
							lbl.Color = pal.TextMuted
							return lbl.Layout(gtx)
						}),
					)
				})
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return drawDivider(gtx, pal.Border)
		}),
		// IF MERGE: Show contained commits section!
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if detail.IsMerge && len(detail.MergedCommits) > 0 {
				return a.layoutMergedCommitsSection(gtx, detail.MergedCommits, pal)
			}
			return layout.Dimensions{}
		}),
		// Diff files list
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if len(detail.Files) == 0 {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(a.theme, unit.Sp(12), "No changed files in this commit")
					lbl.Color = pal.TextMuted
					return lbl.Layout(gtx)
				})
			}

			type diffItem struct {
				isFileHeader bool
				isHunk       bool
				filePath     string
				status       string
				adds, dels   int
				hunkHeader   string
				line         models.DiffLine
			}

			var items []diffItem
			for _, f := range detail.Files {
				items = append(items, diffItem{
					isFileHeader: true,
					filePath:     f.NewPath,
					status:       f.Status,
					adds:         f.Additions,
					dels:         f.Deletions,
				})
				for _, h := range f.Hunks {
					items = append(items, diffItem{
						isHunk:     true,
						hunkHeader: h.Header,
					})
					for _, l := range h.Lines {
						items = append(items, diffItem{
							line: l,
						})
					}
				}
			}

			return a.createListStyle(&a.diffList, pal).Layout(gtx, len(items), func(gtx layout.Context, idx int) layout.Dimensions {
				it := items[idx]

				if it.isFileHeader {
					return drawWithBg(gtx, pal.Surface, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(a.theme, unit.Sp(11.5), it.filePath)
									lbl.Font.Weight = font.SemiBold
									lbl.Color = pal.Text
									return lbl.Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return a.drawTag(gtx, strings.ToUpper(it.status), pal.BadgeBg, pal.BadgeText)
								}),
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(a.theme, unit.Sp(10.5), fmt.Sprintf("+%d -%d", it.adds, it.dels))
									lbl.Color = pal.TextMuted
									return lbl.Layout(gtx)
								}),
							)
						})
					})
				}

				if it.isHunk {
					return drawWithBg(gtx, pal.BadgeBg, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Label(a.theme, unit.Sp(10.5), it.hunkHeader)
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

				return drawWithBg(gtx, bgColor, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(1), Bottom: unit.Dp(1), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.X = gtx.Dp(unit.Dp(35))
								gtx.Constraints.Max.X = gtx.Dp(unit.Dp(35))
								lbl := material.Label(a.theme, unit.Sp(10), lineNo)
								lbl.Color = pal.TextMuted
								return lbl.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Label(a.theme, unit.Sp(10.5), prefix+" ")
								lbl.Color = textColor
								return lbl.Layout(gtx)
							}),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Label(a.theme, unit.Sp(10.5), it.line.Content)
								lbl.Color = textColor
								return lbl.Layout(gtx)
							}),
						)
					})
				})
			})
		}),
	)
}

// Merged commits list inside a merge commit
func (a *App) layoutMergedCommitsSection(gtx layout.Context, merged []models.CommitSummary, pal Palette) layout.Dimensions {
	return drawWithBg(gtx, pal.Surface, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(a.theme, unit.Sp(11), fmt.Sprintf("COMMITS MERGED IN THIS PULL (%d):", len(merged)))
					lbl.Font.Weight = font.Bold
					lbl.Color = pal.Accent
					return lbl.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						a.drawMergedCommitItems(gtx, merged, pal)...,
					)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return drawDivider(gtx, pal.Border)
				}),
			)
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
								lbl := material.Label(a.theme, unit.Sp(10.5), commit.ShortHash)
								lbl.Color = pal.Accent
								lbl.Font.Weight = font.SemiBold
								return lbl.Layout(gtx)
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
								lbl := material.Label(a.theme, unit.Sp(10), fmt.Sprintf("+%d -%d", commit.Additions, commit.Deletions))
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
	ls.AnchorStrategy = material.Overlay
	ls.Indicator.MinorWidth = unit.Dp(6)
	ls.Indicator.CornerRadius = unit.Dp(3)
	if pal.IsDark {
		ls.Indicator.Color = color.NRGBA{R: 110, G: 118, B: 129, A: 160}
		ls.Indicator.HoverColor = pal.Accent
	} else {
		ls.Indicator.Color = color.NRGBA{R: 140, G: 149, B: 159, A: 180}
		ls.Indicator.HoverColor = pal.Accent
	}
	ls.Track.Color = color.NRGBA{0, 0, 0, 0}
	return ls
}

func (a *App) drawSectionHeader(gtx layout.Context, title string, pal Palette) layout.Dimensions {
	lbl := material.Label(a.theme, unit.Sp(9.5), title)
	lbl.Font.Weight = font.Bold
	lbl.Color = pal.TextMuted
	return lbl.Layout(gtx)
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

		return drawWithBg(gtx, bg, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(5), Bottom: unit.Dp(5), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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

		return drawWithBg(gtx, bg, func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Label(a.theme, unit.Sp(10.5), txt)
				lbl.Color = fg
				lbl.Font.Weight = font.Medium
				return lbl.Layout(gtx)
			})
		})
	})
}

func (a *App) drawTag(gtx layout.Context, txt string, bg, fg color.NRGBA) layout.Dimensions {
	return drawWithBg(gtx, bg, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(1), Bottom: unit.Dp(1), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Label(a.theme, unit.Sp(9.5), txt)
			lbl.Color = fg
			lbl.Font.Weight = font.Bold
			return lbl.Layout(gtx)
		})
	})
}

func (a *App) drawMetricCard(gtx layout.Context, title, val, sub string, pal Palette) layout.Dimensions {
	return drawWithBg(gtx, pal.Surface, func(gtx layout.Context) layout.Dimensions {
		return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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
