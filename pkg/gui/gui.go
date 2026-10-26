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
	subRepos       []models.SubRepo
	branches       []models.BranchInfo
	activeBranch   string
	commits        []models.CommitSummary
	selectedHash   string
	selectedDetail *models.CommitDetail
	updatesData    *models.UpdatesResult

	// Popover / View toggles
	showBranchPicker  bool
	showSubRepoPicker bool

	// Filters
	searchEditor widget.Editor
	commitFilter string // "all", "merges", "direct", "ai"
	timePeriod   string // "1h", "24h", "today", "3d", "7d", "30d", "all"
	aiFilter     string // "all", "ai_only", "human_only"

	// UI Widgets
	btnOpenFolder widget.Clickable
	btnSubRepo    widget.Clickable
	btnBranch     widget.Clickable
	btnTabCommits widget.Clickable
	btnTabUpdates widget.Clickable

	btnFilterAll    widget.Clickable
	btnFilterMerges widget.Clickable
	btnFilterDirect widget.Clickable
	btnFilterAI     widget.Clickable

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

	commitsList      layout.List
	commitClicks     []widget.Clickable
	mergedClicks     []widget.Clickable
	branchClicks     []widget.Clickable
	subRepoClicks    []widget.Clickable
	diffList         layout.List
	updatesList      layout.List
	authorClicks     []widget.Clickable
	timelineClicks   []widget.Clickable
}

func NewApp(initialPath string) *App {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))

	cfgMgr, _ := config.NewManager()
	gitSvc := git.NewService()

	target := initialPath
	if target == "" && cfgMgr != nil {
		cfg := cfgMgr.GetConfig()
		if cfg.ActivePath != "" && gitSvc.IsGitRepo(cfg.ActivePath) {
			target = cfg.ActivePath
		}
	}

	a := &App{
		theme:        th,
		gitSvc:       gitSvc,
		cfgMgr:       cfgMgr,
		rootPath:     target,
		activeRepo:   target,
		activeTab:    "commits",
		commitFilter: "all",
		timePeriod:   "7d",
		aiFilter:     "all",
		commitsList:  layout.List{Axis: layout.Vertical},
		diffList:     layout.List{Axis: layout.Vertical},
		updatesList:  layout.List{Axis: layout.Vertical},
	}

	if target != "" {
		a.repoName = filepath.Base(target)
	}

	return a
}

func (a *App) Run() error {
	a.window = new(app.Window)
	a.window.Option(
		app.Title("sCodeViewer"),
		app.Size(unit.Dp(1180), unit.Dp(740)),
		app.MinSize(unit.Dp(800), unit.Dp(500)),
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

	// Discover nested repos if rootPath is set
	if a.rootPath != "" {
		subRepos, err := a.gitSvc.FindGitRepositories(a.rootPath)
		if err == nil && len(subRepos) > 0 {
			a.subRepos = subRepos
			a.subRepoClicks = make([]widget.Clickable, len(subRepos))
		}
	}

	branches, _ := a.gitSvc.GetBranches(a.activeRepo)
	a.branches = branches
	a.branchClicks = make([]widget.Clickable, len(branches))

	curr, _ := a.gitSvc.GetCurrentBranch(a.activeRepo)
	a.activeBranch = curr

	a.reloadCommitsLocked()
	a.reloadUpdatesLocked()
	a.isDataLoaded = true

	if a.selectedHash != "" {
		detail, err := a.gitSvc.GetCommitDetail(a.activeRepo, a.selectedHash)
		if err == nil {
			a.selectedDetail = detail
			a.mergedClicks = make([]widget.Clickable, len(detail.MergedCommits))
		}
	}
}

func (a *App) reloadCommitsLocked() {
	mergesOnly := a.commitFilter == "merges"
	filterAI := ""
	if a.commitFilter == "ai" {
		filterAI = "ai_only"
	}

	commits, err := a.gitSvc.GetCommits(a.activeRepo, a.activeBranch, "", filterAI, mergesOnly, 200, 0)
	if err == nil {
		// Filter direct commits if requested
		if a.commitFilter == "direct" {
			var direct []models.CommitSummary
			for _, c := range commits {
				if !c.IsMerge {
					direct = append(direct, c)
				}
			}
			a.commits = direct
		} else {
			a.commits = commits
		}

		a.commitClicks = make([]widget.Clickable, len(a.commits))
		if len(a.commits) > 0 && a.selectedHash == "" {
			a.selectedHash = a.commits[0].Hash
			detail, err := a.gitSvc.GetCommitDetail(a.activeRepo, a.commits[0].Hash)
			if err == nil {
				a.selectedDetail = detail
				a.mergedClicks = make([]widget.Clickable, len(detail.MergedCommits))
			}
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
	// Open Folder
	if a.btnOpenFolder.Clicked(gtx) {
		go func() {
			path := openFolderDialog()
			if path != "" && a.gitSvc.IsGitRepo(path) {
				a.mu.Lock()
				a.rootPath = path
				a.activeRepo = path
				a.repoName = filepath.Base(path)
				a.selectedHash = ""
				a.selectedDetail = nil
				a.showBranchPicker = false
				a.showSubRepoPicker = false
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

	// Toggle SubRepo Picker
	if a.btnSubRepo.Clicked(gtx) {
		a.showSubRepoPicker = !a.showSubRepoPicker
		a.showBranchPicker = false
	}

	// Toggle Branch Picker
	if a.btnBranch.Clicked(gtx) {
		a.showBranchPicker = !a.showBranchPicker
		a.showSubRepoPicker = false
	}

	// Tab switcher
	if a.btnTabCommits.Clicked(gtx) {
		a.activeTab = "commits"
		a.showBranchPicker = false
		a.showSubRepoPicker = false
	}
	if a.btnTabUpdates.Clicked(gtx) {
		a.activeTab = "updates"
		a.showBranchPicker = false
		a.showSubRepoPicker = false
		a.mu.Lock()
		a.reloadUpdatesLocked()
		a.mu.Unlock()
	}

	// SubRepo clicks
	a.mu.Lock()
	for i := range a.subRepoClicks {
		if i < len(a.subRepos) && a.subRepoClicks[i].Clicked(gtx) {
			a.activeRepo = a.subRepos[i].Path
			a.repoName = a.subRepos[i].Name
			a.selectedHash = ""
			a.selectedDetail = nil
			a.showSubRepoPicker = false
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
			a.showBranchPicker = false
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
	// Always use system theme automatically
	pal := GetPalette(ThemeAuto)

	// Clip & Fill window background
	paint.FillShape(gtx.Ops, pal.Bg, clip.Rect(image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Max.Y)).Op())

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header Bar
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return a.layoutHeader(gtx, pal)
		}),
		// Divider
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return drawDivider(gtx, pal.Border)
		}),
		// Dropdown panels if active
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if a.showSubRepoPicker {
				return a.layoutSubRepoPicker(gtx, pal)
			}
			if a.showBranchPicker {
				return a.layoutBranchPicker(gtx, pal)
			}
			return layout.Dimensions{}
		}),
		// Main Content
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if a.activeRepo == "" {
				return a.layoutEmptyState(gtx, pal)
			}
			if a.activeTab == "updates" {
				return a.layoutUpdatesTab(gtx, pal)
			}
			return a.layoutCommitsTab(gtx, pal)
		}),
	)
}

func (a *App) layoutHeader(gtx layout.Context, pal Palette) layout.Dimensions {
	return drawWithBg(gtx, pal.HeaderBg, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{
			Top:    unit.Dp(8),
			Bottom: unit.Dp(8),
			Left:   unit.Dp(16),
			Right:  unit.Dp(16),
		}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{
				Axis:      layout.Horizontal,
				Alignment: layout.Middle,
			}.Layout(gtx,
				// App title
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(a.theme, unit.Sp(13.5), "sCodeViewer")
					lbl.Color = pal.Text
					lbl.Font.Weight = font.Bold
					return lbl.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(14)}.Layout),

				// Project / Nested Sub-repo Selector
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					btnText := "Project: None"
					if a.repoName != "" {
						countStr := ""
						if len(a.subRepos) > 1 {
							countStr = fmt.Sprintf(" (%d repos)", len(a.subRepos))
						}
						btnText = "Repo: " + a.repoName + countStr
					}
					return a.drawPillButton(gtx, &a.btnSubRepo, btnText, a.showSubRepoPicker, pal)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),

				// Open Folder
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return a.drawPillButton(gtx, &a.btnOpenFolder, "Open Folder...", false, pal)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(14)}.Layout),

				// Branch Selector
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					bText := "Branch: " + a.activeBranch
					if a.activeBranch == "" {
						bText = "Branch: None"
					}
					return a.drawPillButton(gtx, &a.btnBranch, bText, a.showBranchPicker, pal)
				}),

				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layout.Dimensions{}
				}),

				// Tabs: Commits, Updates (NO THEME BUTTON)
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return a.drawPillButton(gtx, &a.btnTabCommits, "Commits & Merges", a.activeTab == "commits", pal)
				}),
				layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return a.drawPillButton(gtx, &a.btnTabUpdates, "Updates", a.activeTab == "updates", pal)
				}),
			)
		})
	})
}

// SubRepo Picker dropdown
func (a *App) layoutSubRepoPicker(gtx layout.Context, pal Palette) layout.Dimensions {
	a.mu.Lock()
	subRepos := a.subRepos
	a.mu.Unlock()

	return drawWithBg(gtx, pal.Surface, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(a.theme, unit.Sp(11), "Select Project / Nested Repository:")
					lbl.Color = pal.TextMuted
					return lbl.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						a.drawSubRepoList(gtx, subRepos, pal)...,
					)
				}),
			)
		})
	})
}

func (a *App) drawSubRepoList(gtx layout.Context, repos []models.SubRepo, pal Palette) []layout.FlexChild {
	var children []layout.FlexChild
	for i, r := range repos {
		idx := i
		repo := r
		isActive := repo.Path == a.activeRepo

		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			var click *widget.Clickable
			if idx < len(a.subRepoClicks) {
				click = &a.subRepoClicks[idx]
			} else {
				click = new(widget.Clickable)
			}
			label := repo.Name
			if repo.RelPath != "." {
				label += " [" + repo.RelPath + "]"
			}
			return a.drawPillButton(gtx, click, label, isActive, pal)
		}))
		children = append(children, layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout))
	}
	return children
}

// Branch Picker dropdown
func (a *App) layoutBranchPicker(gtx layout.Context, pal Palette) layout.Dimensions {
	a.mu.Lock()
	branches := a.branches
	a.mu.Unlock()

	return drawWithBg(gtx, pal.Surface, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(a.theme, unit.Sp(11), fmt.Sprintf("Branches (%d total):", len(branches)))
					lbl.Color = pal.TextMuted
					return lbl.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						a.drawBranchList(gtx, branches, pal)...,
					)
				}),
			)
		})
	})
}

func (a *App) drawBranchList(gtx layout.Context, branches []models.BranchInfo, pal Palette) []layout.FlexChild {
	var children []layout.FlexChild
	for i, b := range branches {
		idx := i
		branch := b
		isActive := branch.Name == a.activeBranch

		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			var click *widget.Clickable
			if idx < len(a.branchClicks) {
				click = &a.branchClicks[idx]
			} else {
				click = new(widget.Clickable)
			}
			name := branch.Name
			if branch.IsCurrent {
				name = "* " + name
			}
			return a.drawPillButton(gtx, click, name, isActive, pal)
		}))
		children = append(children, layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout))
	}
	return children
}

func (a *App) layoutCommitsTab(gtx layout.Context, pal Palette) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		// Left: Commits sidebar (360dp)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(unit.Dp(360))
			gtx.Constraints.Max.X = gtx.Dp(unit.Dp(360))
			return a.layoutCommitsSidebar(gtx, pal)
		}),
		// Vertical separator
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return drawVerticalDivider(gtx, pal.Border)
		}),
		// Right: Diff detail
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return a.layoutCommitDetail(gtx, pal)
		}),
	)
}

func (a *App) layoutCommitsSidebar(gtx layout.Context, pal Palette) layout.Dimensions {
	a.mu.Lock()
	commits := a.commits
	a.mu.Unlock()

	search := strings.ToLower(strings.TrimSpace(a.searchEditor.Text()))

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Search & Filter controls
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					// Search input
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						ed := material.Editor(a.theme, &a.searchEditor, "Search commits or hash...")
						ed.TextSize = unit.Sp(12)
						ed.Color = pal.Text
						ed.HintColor = pal.TextMuted
						return drawWithBg(gtx, pal.Surface, func(gtx layout.Context) layout.Dimensions {
							return layout.UniformInset(unit.Dp(6)).Layout(gtx, ed.Layout)
						})
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
					// Filter chips: All, Merges Only, Direct Commits, AI
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return a.drawPillButton(gtx, &a.btnFilterAll, "All", a.commitFilter == "all", pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return a.drawPillButton(gtx, &a.btnFilterMerges, "Merges Only", a.commitFilter == "merges", pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return a.drawPillButton(gtx, &a.btnFilterDirect, "Direct", a.commitFilter == "direct", pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return a.drawPillButton(gtx, &a.btnFilterAI, "AI", a.commitFilter == "ai", pal)
							}),
						)
					}),
				)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return drawDivider(gtx, pal.Border)
		}),
		// Commits list
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
					lbl := material.Label(a.theme, unit.Sp(12), "No commits found")
					lbl.Color = pal.TextMuted
					return lbl.Layout(gtx)
				})
			}

			return a.commitsList.Layout(gtx, len(filtered), func(gtx layout.Context, idx int) layout.Dimensions {
				c := filtered[idx]
				isSelected := c.Hash == a.selectedHash

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
							Top:    unit.Dp(8),
							Bottom: unit.Dp(8),
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
											lbl.Color = pal.Text
											return lbl.Layout(gtx)
										}),
										layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
										layout.Rigid(func(gtx layout.Context) layout.Dimensions {
											lbl := material.Label(a.theme, unit.Sp(10.5), formatRelativeTime(c.Date))
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
									return lbl.Layout(gtx)
								}),
								layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
								// Badges + Stats (Distinct MERGE badge)
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
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

func (a *App) layoutCommitDetail(gtx layout.Context, pal Palette) layout.Dimensions {
	a.mu.Lock()
	detail := a.selectedDetail
	a.mu.Unlock()

	if detail == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Label(a.theme, unit.Sp(12), "Select a commit")
			lbl.Color = pal.TextMuted
			return lbl.Layout(gtx)
		})
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header summary
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return drawWithBg(gtx, pal.Surface, func(gtx layout.Context) layout.Dimensions {
				return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						// Subject & Merge tag
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
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
							meta := fmt.Sprintf("%s  •  %s  •  %s  •  Files: %d  •  +%d -%d",
								detail.AuthorName,
								formatRelativeTime(detail.Date),
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

			return a.diffList.Layout(gtx, len(items), func(gtx layout.Context, idx int) layout.Dimensions {
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
		return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(a.theme, unit.Sp(11), fmt.Sprintf("Commits merged in this pull (%d):", len(merged)))
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

func (a *App) layoutUpdatesTab(gtx layout.Context, pal Palette) layout.Dimensions {
	a.mu.Lock()
	up := a.updatesData
	a.mu.Unlock()

	if up == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Label(a.theme, unit.Sp(12), "Loading updates...")
			lbl.Color = pal.TextMuted
			return lbl.Layout(gtx)
		})
	}

	return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// Filter Bar: Periods + AI Filter
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
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
						return a.drawPillButton(gtx, &a.btnSlice3d, "3d", a.timePeriod == "3d", pal)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return a.drawPillButton(gtx, &a.btnSlice7d, "7d", a.timePeriod == "7d", pal)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return a.drawPillButton(gtx, &a.btnSlice30d, "30d", a.timePeriod == "30d", pal)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return a.drawPillButton(gtx, &a.btnSliceAll, "All", a.timePeriod == "all", pal)
					}),

					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),

					// AI vs Devs filter
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
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),

			// Metric Cards Row
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return a.drawMetricCard(gtx, "Commits", fmt.Sprintf("%d", up.TotalCommits), fmt.Sprintf("%d merges", up.TotalMerges), pal)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return a.drawMetricCard(gtx, "AI vs Devs", fmt.Sprintf("%d / %d", up.AICommitsCount, up.HumanCommitsCount), "activity", pal)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return a.drawMetricCard(gtx, "Volume", fmt.Sprintf("+%d -%d", up.TotalAdditions, up.TotalDeletions), "lines changed", pal)
					}),
				)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(12)}.Layout),

			// Updates Content: Timeline Feed
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				if len(up.Commits) == 0 {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(a.theme, unit.Sp(12), "No commits in this timeframe")
						lbl.Color = pal.TextMuted
						return lbl.Layout(gtx)
					})
				}

				return a.updatesList.Layout(gtx, len(up.Commits), func(gtx layout.Context, idx int) layout.Dimensions {
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
							return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												lbl := material.Label(a.theme, unit.Sp(11.5), c.AuthorName)
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
									layout.Rigid(layout.Spacer{Height: unit.Dp(3)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										lbl := material.Label(a.theme, unit.Sp(12), c.Message)
										lbl.Color = pal.Text
										return lbl.Layout(gtx)
									}),
									layout.Rigid(layout.Spacer{Height: unit.Dp(3)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												lbl := material.Label(a.theme, unit.Sp(10.5), c.ShortHash)
												lbl.Color = pal.TextMuted
												return lbl.Layout(gtx)
											}),
											layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												lbl := material.Label(a.theme, unit.Sp(10.5), fmt.Sprintf("+%d -%d", c.Additions, c.Deletions))
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
	})
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
				return a.drawPillButton(gtx, &a.btnOpenFolder, "Open Folder...", true, pal)
			}),
		)
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
				lbl := material.Label(a.theme, unit.Sp(11), txt)
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
		return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(a.theme, unit.Sp(10.5), title)
					lbl.Color = pal.TextMuted
					return lbl.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(2)}.Layout),
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
