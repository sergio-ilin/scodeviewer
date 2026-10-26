package gui

import (
	"fmt"
	"image"
	"image/color"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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

	"scodeviewer/pkg/git"
	"scodeviewer/pkg/models"
)

type App struct {
	window     *app.Window
	theme      *material.Theme
	gitSvc     *git.Service
	repoPath   string
	repoName   string
	themeMode  ThemeMode
	activeTab  string // "commits", "updates"

	// State
	mu           sync.Mutex
	commits      []models.CommitSummary
	branches     []models.BranchInfo
	activeBranch string
	selectedHash string
	selectedDetail *models.CommitDetail
	updatesData  *models.UpdatesResult

	// Filters
	searchEditor widget.Editor
	commitFilter string // "all", "merges", "ai"
	timePeriod   string // "1h", "24h", "today", "3d", "7d", "30d", "all"
	aiFilter     string // "all", "ai_only", "human_only"

	// UI Widgets
	btnOpenRepo   widget.Clickable
	btnTabCommits widget.Clickable
	btnTabUpdates widget.Clickable
	btnTheme      widget.Clickable

	btnFilterAll    widget.Clickable
	btnFilterMerges widget.Clickable
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

	commitsList layout.List
	commitClicks []widget.Clickable

	diffList layout.List
	updatesList layout.List
	authorClicks []widget.Clickable
	timelineClicks []widget.Clickable
}

func NewApp(initialPath string) *App {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))

	a := &App{
		theme:        th,
		gitSvc:       git.NewService(),
		repoPath:     initialPath,
		themeMode:    ThemeAuto,
		activeTab:    "commits",
		commitFilter: "all",
		timePeriod:   "7d",
		aiFilter:     "all",
		commitsList:  layout.List{Axis: layout.Vertical},
		diffList:     layout.List{Axis: layout.Vertical},
		updatesList:  layout.List{Axis: layout.Vertical},
	}

	if initialPath != "" {
		a.repoName = filepath.Base(initialPath)
	}

	return a
}

func (a *App) Run() error {
	a.window = new(app.Window)
	a.window.Option(
		app.Title("sCodeViewer"),
		app.Size(unit.Dp(1150), unit.Dp(720)),
		app.MinSize(unit.Dp(800), unit.Dp(500)),
	)

	if a.repoPath != "" && a.gitSvc.IsGitRepo(a.repoPath) {
		a.loadData()
	}

	var ops op.Ops
	for {
		switch e := a.window.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			a.handleEvents(gtx)
			a.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (a *App) loadData() {
	a.mu.Lock()
	defer a.mu.Unlock()

	branches, _ := a.gitSvc.GetBranches(a.repoPath)
	a.branches = branches
	curr, _ := a.gitSvc.GetCurrentBranch(a.repoPath)
	a.activeBranch = curr

	a.reloadCommitsLocked()
	a.reloadUpdatesLocked()
}

func (a *App) reloadCommitsLocked() {
	mergesOnly := a.commitFilter == "merges"
	filterAI := ""
	if a.commitFilter == "ai" {
		filterAI = "ai_only"
	}
	commits, err := a.gitSvc.GetCommits(a.repoPath, a.activeBranch, "", filterAI, mergesOnly, 150, 0)
	if err == nil {
		a.commits = commits
		a.commitClicks = make([]widget.Clickable, len(commits))
		if len(commits) > 0 && a.selectedHash == "" {
			a.selectedHash = commits[0].Hash
			go a.loadCommitDetail(commits[0].Hash)
		}
	}
}

func (a *App) reloadUpdatesLocked() {
	res, err := a.gitSvc.GetUpdates(a.repoPath, models.TimeSliceQuery{
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
	detail, err := a.gitSvc.GetCommitDetail(a.repoPath, hash)
	if err == nil {
		a.mu.Lock()
		a.selectedDetail = detail
		a.mu.Unlock()
		if a.window != nil {
			a.window.Invalidate()
		}
	}
}

func (a *App) handleEvents(gtx layout.Context) {
	if a.btnOpenRepo.Clicked(gtx) {
		go func() {
			path := openFolderDialog()
			if path != "" && a.gitSvc.IsGitRepo(path) {
				a.mu.Lock()
				a.repoPath = path
				a.repoName = filepath.Base(path)
				a.selectedHash = ""
				a.selectedDetail = nil
				a.mu.Unlock()
				a.loadData()
				if a.window != nil {
					a.window.Invalidate()
				}
			}
		}()
	}

	if a.btnTheme.Clicked(gtx) {
		a.themeMode = (a.themeMode + 1) % 3
	}

	if a.btnTabCommits.Clicked(gtx) {
		a.activeTab = "commits"
	}
	if a.btnTabUpdates.Clicked(gtx) {
		a.activeTab = "updates"
		a.mu.Lock()
		a.reloadUpdatesLocked()
		a.mu.Unlock()
	}

	// Commit filters
	if a.btnFilterAll.Clicked(gtx) {
		a.commitFilter = "all"
		a.mu.Lock()
		a.reloadCommitsLocked()
		a.mu.Unlock()
	}
	if a.btnFilterMerges.Clicked(gtx) {
		a.commitFilter = "merges"
		a.mu.Lock()
		a.reloadCommitsLocked()
		a.mu.Unlock()
	}
	if a.btnFilterAI.Clicked(gtx) {
		a.commitFilter = "ai"
		a.mu.Lock()
		a.reloadCommitsLocked()
		a.mu.Unlock()
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
			a.mu.Lock()
			a.reloadUpdatesLocked()
			a.mu.Unlock()
		}
	}

	// Updates AI filter
	if a.btnUpFilterAll.Clicked(gtx) {
		a.aiFilter = "all"
		a.mu.Lock()
		a.reloadUpdatesLocked()
		a.mu.Unlock()
	}
	if a.btnUpFilterAI.Clicked(gtx) {
		a.aiFilter = "ai_only"
		a.mu.Lock()
		a.reloadUpdatesLocked()
		a.mu.Unlock()
	}
	if a.btnUpFilterHuman.Clicked(gtx) {
		a.aiFilter = "human_only"
		a.mu.Lock()
		a.reloadUpdatesLocked()
		a.mu.Unlock()
	}

	// Commit clicks
	a.mu.Lock()
	for i := range a.commitClicks {
		if i < len(a.commits) && a.commitClicks[i].Clicked(gtx) {
			hash := a.commits[i].Hash
			a.selectedHash = hash
			go a.loadCommitDetail(hash)
			break
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
	pal := GetPalette(a.themeMode)

	// Background fill
	paint.Fill(gtx.Ops, pal.Bg)

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header Bar
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return a.layoutHeader(gtx, pal)
		}),
		// Divider
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return drawDivider(gtx, pal.Border)
		}),
		// Main Content
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			if a.repoPath == "" {
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
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			paint.Fill(gtx.Ops, pal.HeaderBg)
			return layout.Dimensions{Size: gtx.Constraints.Min}
		},
		func(gtx layout.Context) layout.Dimensions {
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
						lbl := material.Label(a.theme, unit.Sp(14), "sCodeViewer")
						lbl.Color = pal.Text
						lbl.Font.Weight = font.Bold
						return lbl.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),

					// Open repo button
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						btnText := "Open..."
						if a.repoName != "" {
							btnText = a.repoName + " (Open...)"
						}
						return a.drawPillButton(gtx, &a.btnOpenRepo, btnText, false, pal)
					}),

					// Branch indicator
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						if a.activeBranch == "" {
							return layout.Dimensions{}
						}
						return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Label(a.theme, unit.Sp(12), a.activeBranch)
							lbl.Color = pal.TextMuted
							return lbl.Layout(gtx)
						})
					}),

					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Dimensions{}
					}),

					// Tabs: Commits, Updates
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return a.drawPillButton(gtx, &a.btnTabCommits, "Commits", a.activeTab == "commits", pal)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return a.drawPillButton(gtx, &a.btnTabUpdates, "Updates", a.activeTab == "updates", pal)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),

					// Theme toggle
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						label := "Auto"
						if a.themeMode == ThemeDark {
							label = "Dark"
						} else if a.themeMode == ThemeLight {
							label = "Light"
						}
						return a.drawPillButton(gtx, &a.btnTheme, label, false, pal)
					}),
				)
			})
		},
	)
}

func (a *App) layoutCommitsTab(gtx layout.Context, pal Palette) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		// Left: Commits sidebar (340dp)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(unit.Dp(340))
			gtx.Constraints.Max.X = gtx.Dp(unit.Dp(340))
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
						ed := material.Editor(a.theme, &a.searchEditor, "Search...")
						ed.TextSize = unit.Sp(12)
						ed.Color = pal.Text
						ed.HintColor = pal.TextMuted
						return layout.Background{}.Layout(gtx,
							func(gtx layout.Context) layout.Dimensions {
								paint.FillShape(gtx.Ops, pal.Surface, clip.UniformRRect(image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y), gtx.Dp(4)).Op(gtx.Ops))
								return layout.Dimensions{Size: gtx.Constraints.Min}
							},
							func(gtx layout.Context) layout.Dimensions {
								return layout.UniformInset(unit.Dp(6)).Layout(gtx, ed.Layout)
							},
						)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
					// Filter chips
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return a.drawPillButton(gtx, &a.btnFilterAll, "All", a.commitFilter == "all", pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return a.drawPillButton(gtx, &a.btnFilterMerges, "Merges", a.commitFilter == "merges", pal)
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

				// Find click index
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
					return layout.Background{}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							if isSelected {
								paint.Fill(gtx.Ops, pal.Selected)
							} else if click.Hovered() {
								paint.Fill(gtx.Ops, pal.Hover)
							}
							return layout.Dimensions{Size: gtx.Constraints.Min}
						},
						func(gtx layout.Context) layout.Dimensions {
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
									// Badges + Stats
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
											layout.Rigid(func(gtx layout.Context) layout.Dimensions {
												if c.IsMerge {
													return a.drawTag(gtx, "Merge", pal.BadgeBg, pal.BadgeText)
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
						},
					)
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
			return layout.Background{}.Layout(gtx,
				func(gtx layout.Context) layout.Dimensions {
					paint.Fill(gtx.Ops, pal.Surface)
					return layout.Dimensions{Size: gtx.Constraints.Min}
				},
				func(gtx layout.Context) layout.Dimensions {
					return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							// Subject
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Label(a.theme, unit.Sp(13.5), detail.Message)
								lbl.Font.Weight = font.Bold
								lbl.Color = pal.Text
								return lbl.Layout(gtx)
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
				},
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return drawDivider(gtx, pal.Border)
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

			// Flatten hunks/lines into list items
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
					return layout.Background{}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							paint.Fill(gtx.Ops, pal.Surface)
							return layout.Dimensions{Size: gtx.Constraints.Min}
						},
						func(gtx layout.Context) layout.Dimensions {
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
						},
					)
				}

				if it.isHunk {
					return layout.Background{}.Layout(gtx,
						func(gtx layout.Context) layout.Dimensions {
							paint.Fill(gtx.Ops, pal.BadgeBg)
							return layout.Dimensions{Size: gtx.Constraints.Min}
						},
						func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								lbl := material.Label(a.theme, unit.Sp(10.5), it.hunkHeader)
								lbl.Color = pal.TextMuted
								return lbl.Layout(gtx)
							})
						},
					)
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

				return layout.Background{}.Layout(gtx,
					func(gtx layout.Context) layout.Dimensions {
						paint.Fill(gtx.Ops, bgColor)
						return layout.Dimensions{Size: gtx.Constraints.Min}
					},
					func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(1), Bottom: unit.Dp(1), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
								// Line number
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									gtx.Constraints.Min.X = gtx.Dp(unit.Dp(35))
									gtx.Constraints.Max.X = gtx.Dp(unit.Dp(35))
									lbl := material.Label(a.theme, unit.Sp(10), lineNo)
									lbl.Color = pal.TextMuted
									return lbl.Layout(gtx)
								}),
								// Prefix
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(a.theme, unit.Sp(10.5), prefix+" ")
									lbl.Color = textColor
									return lbl.Layout(gtx)
								}),
								// Content
								layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(a.theme, unit.Sp(10.5), it.line.Content)
									lbl.Color = textColor
									return lbl.Layout(gtx)
								}),
							)
						})
					},
				)
			})
		}),
	)
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
						return layout.Background{}.Layout(gtx,
							func(gtx layout.Context) layout.Dimensions {
								bg := pal.Surface
								if click.Hovered() {
									bg = pal.Hover
								}
								paint.FillShape(gtx.Ops, bg, clip.UniformRRect(image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y), gtx.Dp(6)).Op(gtx.Ops))
								return layout.Dimensions{Size: gtx.Constraints.Min}
							},
							func(gtx layout.Context) layout.Dimensions {
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
														return a.drawTag(gtx, "Merge", pal.BadgeBg, pal.BadgeText)
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
							},
						)
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
				return a.drawPillButton(gtx, &a.btnOpenRepo, "Open Folder...", true, pal)
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

		return layout.Background{}.Layout(gtx,
			func(gtx layout.Context) layout.Dimensions {
				paint.FillShape(gtx.Ops, bg, clip.UniformRRect(image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y), gtx.Dp(4)).Op(gtx.Ops))
				return layout.Dimensions{Size: gtx.Constraints.Min}
			},
			func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(4), Left: unit.Dp(8), Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(a.theme, unit.Sp(11), txt)
					lbl.Color = fg
					lbl.Font.Weight = font.Medium
					return lbl.Layout(gtx)
				})
			},
		)
	})
}

func (a *App) drawTag(gtx layout.Context, txt string, bg, fg color.NRGBA) layout.Dimensions {
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			paint.FillShape(gtx.Ops, bg, clip.UniformRRect(image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y), gtx.Dp(3)).Op(gtx.Ops))
			return layout.Dimensions{Size: gtx.Constraints.Min}
		},
		func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(1), Bottom: unit.Dp(1), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lbl := material.Label(a.theme, unit.Sp(9.5), txt)
				lbl.Color = fg
				lbl.Font.Weight = font.Bold
				return lbl.Layout(gtx)
			})
		},
	)
}

func (a *App) drawMetricCard(gtx layout.Context, title, val, sub string, pal Palette) layout.Dimensions {
	return layout.Background{}.Layout(gtx,
		func(gtx layout.Context) layout.Dimensions {
			paint.FillShape(gtx.Ops, pal.Surface, clip.UniformRRect(image.Rect(0, 0, gtx.Constraints.Min.X, gtx.Constraints.Min.Y), gtx.Dp(6)).Op(gtx.Ops))
			return layout.Dimensions{Size: gtx.Constraints.Min}
		},
		func(gtx layout.Context) layout.Dimensions {
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
		},
	)
}

func drawDivider(gtx layout.Context, c color.NRGBA) layout.Dimensions {
	gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(1))
	gtx.Constraints.Max.Y = gtx.Dp(unit.Dp(1))
	paint.Fill(gtx.Ops, c)
	return layout.Dimensions{Size: gtx.Constraints.Min}
}

func drawVerticalDivider(gtx layout.Context, c color.NRGBA) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Dp(unit.Dp(1))
	gtx.Constraints.Max.X = gtx.Dp(unit.Dp(1))
	paint.Fill(gtx.Ops, c)
	return layout.Dimensions{Size: gtx.Constraints.Min}
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
	cmd := exec.Command("powershell", "-NoProfile", "-Command",
		`Add-Type -AssemblyName System.Windows.Forms; $d = New-Object System.Windows.Forms.FolderBrowserDialog; if ($d.ShowDialog() -eq 'OK') { Write-Host -NoNewline $d.SelectedPath }`)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
