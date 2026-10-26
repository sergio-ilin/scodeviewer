package gui

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"sort"
	"strings"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"scodeviewer/pkg/git"
)

type Graph3DView struct {
	app *App

	// 3D Camera
	yaw        float64
	pitch      float64
	zoom       float64
	panX, panY float64
	autoRotate bool
	navMode    string // "orbit" or "pan"
	spaceHeld  bool

	// Pointer interaction
	lastPos    f32.Point
	dragStart  f32.Point
	isDragging bool

	// Selected node
	selectedHash string

	// Navigation Mode & Preset Buttons
	btnModeOrbit     widget.Clickable
	btnModePan       widget.Clickable
	btnIso           widget.Clickable
	btnTop           widget.Clickable
	btnFront         widget.Clickable
	btnReset         widget.Clickable
	btnFocusSelected widget.Clickable
	btnAutoRotate    widget.Clickable
	btnZoomIn        widget.Clickable
	btnZoomOut       widget.Clickable
	btnCopyHash      widget.Clickable
	btnCloseCard     widget.Clickable

	// Screen projected positions for hit testing & rendering
	projectedNodes []projectedNode
}

type projectedNode struct {
	node      git.Graph3DNode
	screenPos f32.Point
	depth     float64
	visible   bool
	col       color.NRGBA
	isMerge   bool
	radius    float32
}

type projectedEdge struct {
	from    f32.Point
	to      f32.Point
	depth   float64
	isMerge bool
	col     color.NRGBA
}

type mergeCluster struct {
	mergeIdx    int
	mergeHash   string
	author      string
	branch      string
	commitCount int
	nodeIndices []int
	col         color.NRGBA
}

func NewGraph3DView(app *App) *Graph3DView {
	return &Graph3DView{
		app:     app,
		navMode: "orbit",
		yaw:     0.38,
		pitch:   0.24,
		zoom:    1.0,
		panX:    0,
		panY:    -30,
	}
}

func (g *Graph3DView) Layout(gtx layout.Context, data *git.Graph3DData, pal Palette) layout.Dimensions {
	if data == nil || len(data.Nodes) == 0 {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			lbl := material.Label(g.app.theme, unit.Sp(13), "Loading 3D commit topology...")
			lbl.Color = pal.TextMuted
			return lbl.Layout(gtx)
		})
	}

	// Ensure the 3D viewport fills the full allocated area
	gtx.Constraints.Min = gtx.Constraints.Max

	// Clip everything strictly to 3D canvas so nothing ever draws over sidebar or window bounds!
	defer clip.Rect(image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Max.Y)).Push(gtx.Ops).Pop()

	// 1. Process Pointer & Keyboard Events
	g.handlePointer(gtx)

	// Process Keyboard Events (Space for Pan, O for Orbit, P for Pan, F for Focus, Esc to deselect)
	for {
		ev, ok := gtx.Event(key.Filter{Focus: g})
		if !ok {
			break
		}
		if ke, ok := ev.(key.Event); ok {
			if ke.Name == key.NameSpace {
				if ke.State == key.Press {
					g.spaceHeld = true
				} else if ke.State == key.Release {
					g.spaceHeld = false
				}
			} else if ke.State == key.Press {
				switch ke.Name {
				case "O", "o":
					g.navMode = "orbit"
				case "P", "p", "H", "h":
					g.navMode = "pan"
				case "F", "f":
					g.focusSelectedOrCenter()
				case key.NameEscape:
					g.selectedHash = ""
				}
			}
			gtx.Execute(op.InvalidateCmd{})
		}
	}

	// 2. Handle Auto-Rotation
	if g.autoRotate {
		g.yaw += 0.005
		if g.yaw > math.Pi*2 {
			g.yaw -= math.Pi * 2
		}
		gtx.Execute(op.InvalidateCmd{})
	}

	mergeCount := 0
	for _, n := range data.Nodes {
		if n.IsMerge {
			mergeCount++
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// 1. Top Controls Toolbar (Mode, Presets, Reset, Zoom)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(6), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return drawWithBorder(gtx, pal.HeaderBg, unit.Dp(8), unit.Dp(1), pal.Border, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							// App / View Title
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										return g.app.icons.Layout(gtx, g.app.icons.Merge, pal.Accent, unit.Dp(14))
									}),
									layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										lbl := material.Label(g.app.theme, unit.Sp(12), "3D TOPOLOGY")
										lbl.Font.Weight = font.Bold
										lbl.Color = pal.Text
										return lbl.Layout(gtx)
									}),
								)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(12)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								w := gtx.Dp(unit.Dp(1))
								h := gtx.Dp(unit.Dp(18))
								paint.FillShape(gtx.Ops, pal.Border, clip.Rect(image.Rect(0, 0, w, h)).Op())
								return layout.Dimensions{Size: image.Pt(w, h)}
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),

							// NAVIGATION MODE TOGGLE: Orbit (Rotate) vs Pan (Move)
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return g.app.drawPillButton(gtx, &g.btnModeOrbit, "Orbit (Rotate)", g.navMode == "orbit", pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return g.app.drawPillButton(gtx, &g.btnModePan, "Pan (Space)", g.navMode == "pan" || g.spaceHeld, pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								w := gtx.Dp(unit.Dp(1))
								h := gtx.Dp(unit.Dp(18))
								paint.FillShape(gtx.Ops, pal.Border, clip.Rect(image.Rect(0, 0, w, h)).Op())
								return layout.Dimensions{Size: image.Pt(w, h)}
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),

							// CAMERA PRESETS
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return g.app.drawPillButton(gtx, &g.btnIso, "Isometric (3D)", false, pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return g.app.drawPillButton(gtx, &g.btnTop, "Top (2D DAG)", false, pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return g.app.drawPillButton(gtx, &g.btnFront, "Front (Timeline)", false, pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),

							// VIEW CONTROLS
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return g.app.drawPillButton(gtx, &g.btnReset, "Center View", false, pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								rotText := "Auto-Rotate"
								if g.autoRotate {
									rotText = "Stop Rotate"
								}
								return g.app.drawPillButton(gtx, &g.btnAutoRotate, rotText, g.autoRotate, pal)
							}),

							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),

							// ZOOM CONTROLS
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								zoomPct := fmt.Sprintf("%d%%", int(g.zoom*100))
								lbl := material.Label(g.app.theme, unit.Sp(11), zoomPct)
								lbl.Color = pal.TextMuted
								lbl.Font.Weight = font.SemiBold
								return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, lbl.Layout)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return g.app.drawPillButton(gtx, &g.btnZoomIn, "+", false, pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return g.app.drawPillButton(gtx, &g.btnZoomOut, "-", false, pal)
							}),
						)
					})
				})
			})
		}),

		// 2. Middle: Interactive 3D Viewport
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Stack{}.Layout(gtx,
				layout.Expanded(func(gtx layout.Context) layout.Dimensions {
					// Canvas background
					paint.FillShape(gtx.Ops, pal.Bg, clip.Rect(image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Max.Y)).Op())

					// Register pointer hit area
					area := clip.Rect(image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Max.Y)).Push(gtx.Ops)
					event.Op(gtx.Ops, g)

					// Render the 3D Scene with Merge Zones, Diamonds & Spheres
					g.render3DScene(gtx, data, pal)
					area.Pop()
					return layout.Dimensions{Size: gtx.Constraints.Max}
				}),
				// Floating Inspector Card docked on the right side if node selected
				layout.Stacked(func(gtx layout.Context) layout.Dimensions {
					if g.selectedHash == "" {
						return layout.Dimensions{}
					}
					var selNode *git.Graph3DNode
					for i := range data.Nodes {
						if data.Nodes[i].Hash == g.selectedHash {
							selNode = &data.Nodes[i]
							break
						}
					}
					if selNode == nil {
						return layout.Dimensions{}
					}
					return layout.NE.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(10), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							gtx.Constraints.Max.X = gtx.Dp(unit.Dp(360))
							return g.renderInspectorCard(gtx, selNode, pal)
						})
					})
				}),
			)
		}),

		// 3. Bottom Status Bar with controls hint and node count (NEVER overlaps canvas or toolbar!)
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(4), Bottom: unit.Dp(10), Left: unit.Dp(10), Right: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return drawWithBorder(gtx, pal.HeaderBg, unit.Dp(6), unit.Dp(1), pal.Border, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Label(g.app.theme, unit.Sp(10), "Navigation: ")
								lbl.Font.Weight = font.Bold
								lbl.Color = pal.Accent
								return lbl.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Label(g.app.theme, unit.Sp(10), "Space+Drag / Right Drag: Pan  •  Left Drag: Rotate  •  Mouse wheel: Zoom  •  Click node to inspect")
								lbl.Color = pal.TextMuted
								return lbl.Layout(gtx)
							}),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								stats := fmt.Sprintf("%d Commits  •  %d Merges  •  %d Connections", len(data.Nodes), mergeCount, len(data.Edges))
								lbl := material.Label(g.app.theme, unit.Sp(10), stats)
								lbl.Color = pal.TextMuted
								lbl.Font.Weight = font.Medium
								return lbl.Layout(gtx)
							}),
						)
					})
				})
			})
		}),
	)
}

func (g *Graph3DView) focusSelectedOrCenter() {
	if g.selectedHash != "" {
		for _, pn := range g.projectedNodes {
			if pn.node.Hash == g.selectedHash {
				g.panX = 0
				g.panY = -float64(pn.node.Order)*30.0 + 120.0
				g.zoom = 1.0
				return
			}
		}
	}
	g.yaw = 0.38
	g.pitch = 0.24
	g.zoom = 1.0
	g.panX = 0
	g.panY = -30
	g.autoRotate = false
}

func (g *Graph3DView) handlePointer(gtx layout.Context) {
	// Mode switcher buttons
	if g.btnModeOrbit.Clicked(gtx) {
		g.navMode = "orbit"
	}
	if g.btnModePan.Clicked(gtx) {
		g.navMode = "pan"
	}

	// Camera Presets
	if g.btnIso.Clicked(gtx) {
		g.yaw = 0.42
		g.pitch = 0.28
		g.zoom = 1.0
		g.panX = 0
		g.panY = -30
	}
	if g.btnTop.Clicked(gtx) {
		g.yaw = 0
		g.pitch = 0
		g.zoom = 1.0
		g.panX = 0
		g.panY = -30
	}
	if g.btnFront.Clicked(gtx) {
		g.yaw = 1.57
		g.pitch = 0
		g.zoom = 1.0
		g.panX = 0
		g.panY = -30
	}
	if g.btnReset.Clicked(gtx) {
		g.focusSelectedOrCenter()
		gtx.Execute(op.InvalidateCmd{})
	}
	if g.btnAutoRotate.Clicked(gtx) {
		g.autoRotate = !g.autoRotate
	}
	if g.btnZoomIn.Clicked(gtx) {
		g.zoom = math.Min(6.0, g.zoom*1.2)
		gtx.Execute(op.InvalidateCmd{})
	}
	if g.btnZoomOut.Clicked(gtx) {
		g.zoom = math.Max(0.15, g.zoom*0.8)
		gtx.Execute(op.InvalidateCmd{})
	}

	for {
		ev, ok := gtx.Event(pointer.Filter{
			Target:  g,
			Kinds:   pointer.Press | pointer.Drag | pointer.Release | pointer.Scroll,
			ScrollY: pointer.ScrollRange{Min: -1000000, Max: 1000000},
			ScrollX: pointer.ScrollRange{Min: -1000000, Max: 1000000},
		})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok {
			switch e.Kind {
			case pointer.Press:
				gtx.Execute(key.FocusCmd{Tag: g})
				g.lastPos = e.Position
				g.dragStart = e.Position
				g.isDragging = true
			case pointer.Drag:
				deltaX := float64(e.Position.X - g.lastPos.X)
				deltaY := float64(e.Position.Y - g.lastPos.Y)
				g.lastPos = e.Position

				isPan := g.navMode == "pan" ||
					g.spaceHeld ||
					e.Buttons.Contain(pointer.ButtonSecondary) ||
					e.Buttons.Contain(pointer.ButtonTertiary) ||
					e.Modifiers.Contain(key.ModShift)

				if isPan {
					g.panX += deltaX
					g.panY += deltaY
				} else {
					g.yaw += deltaX * 0.006
					g.pitch += deltaY * 0.006
					if g.pitch < -1.45 {
						g.pitch = -1.45
					}
					if g.pitch > 1.45 {
						g.pitch = 1.45
					}
				}
				gtx.Execute(op.InvalidateCmd{})
			case pointer.Release:
				g.isDragging = false
				dx := e.Position.X - g.dragStart.X
				dy := e.Position.Y - g.dragStart.Y
				if dx*dx+dy*dy < 36 {
					g.hitTestNode(e.Position)
				}
				gtx.Execute(op.InvalidateCmd{})
			case pointer.Scroll:
				if e.Modifiers.Contain(key.ModShift) {
					g.panX -= float64(e.Scroll.Y) * 0.5
				} else if e.Scroll.Y != 0 {
					if e.Scroll.Y > 0 {
						g.zoom = math.Max(0.12, g.zoom*0.86)
					} else {
						g.zoom = math.Min(6.0, g.zoom*1.16)
					}
				} else if e.Scroll.X != 0 {
					g.panX -= float64(e.Scroll.X) * 0.5
				}
				gtx.Execute(op.InvalidateCmd{})
			}
		}
	}
}

func (g *Graph3DView) hitTestNode(pos f32.Point) {
	var bestHash string
	bestDist := float32(1000.0)

	for _, pn := range g.projectedNodes {
		if !pn.visible {
			continue
		}
		dx := pos.X - pn.screenPos.X
		dy := pos.Y - pn.screenPos.Y
		dist := float32(math.Sqrt(float64(dx*dx + dy*dy)))
		hitRadius := pn.radius + 12
		if dist <= hitRadius && dist < bestDist {
			bestDist = dist
			bestHash = pn.node.Hash
		}
	}

	if bestHash != "" {
		if g.selectedHash == bestHash {
			g.selectedHash = ""
		} else {
			g.selectedHash = bestHash
		}
	}
}

func computeMergeClusters(data *git.Graph3DData) []mergeCluster {
	if data == nil || len(data.Nodes) == 0 {
		return nil
	}

	var clusters []mergeCluster
	visited := make(map[string]bool)

	for i, n := range data.Nodes {
		if !n.IsMerge || len(n.ParentHashes) < 2 {
			continue
		}

		cluster := mergeCluster{
			mergeIdx:  i,
			mergeHash: n.Hash,
			author:    n.AuthorName,
			branch:    n.Branch,
			col:       getAuthorColor(n.AuthorName),
		}

		var queue []string
		for _, pHash := range n.ParentHashes[1:] {
			queue = append(queue, pHash)
		}

		for len(queue) > 0 && len(cluster.nodeIndices) < 12 {
			currHash := queue[0]
			queue = queue[1:]

			if visited[currHash] || currHash == n.ParentHashes[0] {
				continue
			}
			visited[currHash] = true

			idx, exists := data.NodeByHash[currHash]
			if !exists {
				continue
			}

			cNode := data.Nodes[idx]
			cluster.nodeIndices = append(cluster.nodeIndices, idx)
			if cluster.branch == "" && cNode.Branch != "" {
				cluster.branch = cNode.Branch
			}
			if cluster.author == "" && cNode.AuthorName != "" {
				cluster.author = cNode.AuthorName
				cluster.col = getAuthorColor(cNode.AuthorName)
			}

			if len(cNode.ParentHashes) > 0 && cNode.ParentHashes[0] != n.ParentHashes[0] {
				queue = append(queue, cNode.ParentHashes[0])
			}
		}

		cluster.commitCount = len(cluster.nodeIndices)
		if cluster.commitCount > 0 {
			clusters = append(clusters, cluster)
		}
	}

	return clusters
}

func (g *Graph3DView) render3DScene(gtx layout.Context, data *git.Graph3DData, pal Palette) {
	if data == nil || len(data.Nodes) == 0 {
		return
	}

	screenW := float64(gtx.Constraints.Max.X)
	screenH := float64(gtx.Constraints.Max.Y)

	maxLane := 0
	for _, n := range data.Nodes {
		if n.Lane > maxLane {
			maxLane = n.Lane
		}
	}
	cx := float64(maxLane) * 50.0
	cy := 180.0
	cz := 0.0

	// 0. Render subtle 3D floor runway lines for lanes
	gridCol := color.NRGBA{R: pal.Border.R, G: pal.Border.G, B: pal.Border.B, A: 45}
	for l := 0; l <= maxLane+1; l++ {
		lx := float64(l) * 110.0
		sx1, sy1, _, v1 := project3D(lx, -60.0, -10.0, cx, cy, cz, g.yaw, g.pitch, g.zoom, screenW, screenH, g.panX, g.panY)
		sx2, sy2, _, v2 := project3D(lx, math.Min(float64(len(data.Nodes))*60.0, 2400.0), -10.0, cx, cy, cz, g.yaw, g.pitch, g.zoom, screenW, screenH, g.panX, g.panY)
		if v1 && v2 {
			var gp clip.Path
			gp.Begin(gtx.Ops)
			gp.MoveTo(f32.Pt(float32(sx1), float32(sy1)))
			gp.LineTo(f32.Pt(float32(sx2), float32(sy2)))
			paint.FillShape(gtx.Ops, gridCol, clip.Stroke{Path: gp.End(), Width: 1.0}.Op())
		}
	}

	// 1. Project all nodes into screen coordinates
	g.projectedNodes = make([]projectedNode, len(data.Nodes))
	for i, n := range data.Nodes {
		worldX := float64(n.Lane) * 110.0
		worldY := float64(n.Order) * 60.0
		worldZ := 0.0
		if n.IsMerge {
			worldZ = 40.0 // Elevated in 3D
		}

		sx, sy, depth, visible := project3D(worldX, worldY, worldZ, cx, cy, cz, g.yaw, g.pitch, g.zoom, screenW, screenH, g.panX, g.panY)

		col := getAuthorColor(n.AuthorName)

		rad := float32(9.0 * g.zoom)
		if n.IsMerge {
			rad = float32(16.0 * g.zoom)
			if rad < 8 {
				rad = 8
			}
			if rad > 32 {
				rad = 32
			}
		} else {
			if rad < 4 {
				rad = 4
			}
			if rad > 18 {
				rad = 18
			}
		}

		g.projectedNodes[i] = projectedNode{
			node:      n,
			screenPos: f32.Pt(float32(sx), float32(sy)),
			depth:     depth,
			visible:   visible,
			col:       col,
			isMerge:   n.IsMerge,
			radius:    rad,
		}
	}

	// 2. Render Merge Zones / Feature Hulls (Behind edges and nodes)
	clusters := computeMergeClusters(data)
	for _, c := range clusters {
		pMerge := g.projectedNodes[c.mergeIdx]
		if !pMerge.visible {
			continue
		}

		minX := pMerge.screenPos.X
		maxX := pMerge.screenPos.X
		minY := pMerge.screenPos.Y
		maxY := pMerge.screenPos.Y
		hasVis := false

		for _, nIdx := range c.nodeIndices {
			pn := g.projectedNodes[nIdx]
			if pn.visible {
				hasVis = true
				if pn.screenPos.X < minX {
					minX = pn.screenPos.X
				}
				if pn.screenPos.X > maxX {
					maxX = pn.screenPos.X
				}
				if pn.screenPos.Y < minY {
					minY = pn.screenPos.Y
				}
				if pn.screenPos.Y > maxY {
					maxY = pn.screenPos.Y
				}
			}
		}

		if hasVis {
			g.drawMergeZone(gtx, c, f32.Pt(minX, minY), f32.Pt(maxX, maxY), pal)
		}
	}

	// 3. Project Edges
	var projEdges []projectedEdge
	for _, e := range data.Edges {
		fromIdx, hasFrom := data.NodeByHash[e.FromHash]
		toIdx, hasTo := data.NodeByHash[e.ToHash]
		if !hasFrom || !hasTo {
			continue
		}
		pFrom := g.projectedNodes[fromIdx]
		pTo := g.projectedNodes[toIdx]
		if !pFrom.visible || !pTo.visible {
			continue
		}

		edgeCol := pFrom.col
		edgeCol.A = 170
		if e.IsMerge {
			edgeCol = pTo.col
			edgeCol.A = 220
		}

		projEdges = append(projEdges, projectedEdge{
			from:    pFrom.screenPos,
			to:      pTo.screenPos,
			depth:   (pFrom.depth + pTo.depth) / 2,
			isMerge: e.IsMerge,
			col:     edgeCol,
		})
	}

	// Depth sort edges
	sort.Slice(projEdges, func(i, j int) bool {
		return projEdges[i].depth < projEdges[j].depth
	})

	for _, e := range projEdges {
		var path clip.Path
		path.Begin(gtx.Ops)
		path.MoveTo(e.from)
		if e.isMerge {
			// 3D Arc between branches into merge commit
			midX := (e.from.X + e.to.X) / 2
			midY := (e.from.Y+e.to.Y)/2 - float32(28*g.zoom)
			path.QuadTo(f32.Pt(midX, midY), e.to)
			strokeW := float32(2.8 * g.zoom)
			if strokeW < 1.6 {
				strokeW = 1.6
			}
			paint.FillShape(gtx.Ops, e.col, clip.Stroke{Path: path.End(), Width: strokeW}.Op())
		} else {
			path.LineTo(e.to)
			strokeW := float32(2.0 * g.zoom)
			if strokeW < 1.0 {
				strokeW = 1.0
			}
			paint.FillShape(gtx.Ops, e.col, clip.Stroke{Path: path.End(), Width: strokeW}.Op())
		}
	}

	// 4. Render Nodes (Depth Sorted: back to front)
	type sortableNode struct {
		idx   int
		depth float64
	}
	var sortedNodes []sortableNode
	for i, pn := range g.projectedNodes {
		if pn.visible {
			sortedNodes = append(sortedNodes, sortableNode{idx: i, depth: pn.depth})
		}
	}
	sort.Slice(sortedNodes, func(i, j int) bool {
		return sortedNodes[i].depth < sortedNodes[j].depth
	})

	for _, sn := range sortedNodes {
		pn := g.projectedNodes[sn.idx]
		isSelected := pn.node.Hash == g.selectedHash

		// RENDER SHAPE: Diamond for Merge Commits vs Sphere for Regular Commits!
		if pn.isMerge {
			g.draw3DDiamond(gtx, pn.screenPos, pn.radius, pn.col, isSelected, pal)

			// Floating "MERGE" badge above diamond
			if g.zoom >= 0.28 {
				mBadgeY := pn.screenPos.Y - pn.radius*1.4 - 12
				macroM := op.Record(gtx.Ops)
				mBg := color.NRGBA{R: pal.HeaderBg.R, G: pal.HeaderBg.G, B: pal.HeaderBg.B, A: 245}
				drawWithRoundedBg(gtx, mBg, unit.Dp(3), func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(1), Bottom: unit.Dp(1), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								lbl := material.Label(g.app.theme, unit.Sp(8), "MERGE")
								lbl.Color = pn.col
								lbl.Font.Weight = font.Bold
								return lbl.Layout(gtx)
							}),
						)
					})
				})
				callM := macroM.Stop()
				mStack := op.Offset(image.Pt(int(pn.screenPos.X-16), int(mBadgeY))).Push(gtx.Ops)
				callM.Add(gtx.Ops)
				mStack.Pop()
			}
		} else {
			g.draw3DSphere(gtx, pn.screenPos, pn.radius, pn.col, isSelected, pal)
		}

		// Text Labels (ShortHash + Author Name)
		if g.zoom >= 0.45 || (pn.isMerge && g.zoom >= 0.32) {
			labelX := pn.screenPos.X + pn.radius + 8
			labelY := pn.screenPos.Y - 6

			macro := op.Record(gtx.Ops)
			lblText := pn.node.ShortHash
			if pn.isMerge {
				lblText = fmt.Sprintf("%s (%s)", pn.node.ShortHash, pn.node.AuthorName)
			}
			lbl := material.Label(g.app.theme, unit.Sp(9.5), lblText)
			if isSelected {
				lbl.Font.Weight = font.Bold
				lbl.Color = pal.Accent
			} else if pn.isMerge {
				lbl.Font.Weight = font.SemiBold
				lbl.Color = pal.Text
			} else {
				lbl.Color = pal.TextMuted
			}
			dims := lbl.Layout(gtx)
			call := macro.Stop()

			tStack := op.Offset(image.Pt(int(labelX), int(labelY))).Push(gtx.Ops)
			call.Add(gtx.Ops)
			tStack.Pop()

			// Branch ref badge
			if len(pn.node.Refs) > 0 {
				refText := pn.node.Refs[0]
				macro2 := op.Record(gtx.Ops)
				tagCol := pal.HeaderBg
				if isSelected {
					tagCol = pal.Selected
				}
				drawWithRoundedBg(gtx, tagCol, unit.Dp(3), func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(1), Bottom: unit.Dp(1), Left: unit.Dp(4), Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						tLbl := material.Label(g.app.theme, unit.Sp(8.5), refText)
						tLbl.Color = pn.col
						tLbl.Font.Weight = font.Bold
						return tLbl.Layout(gtx)
					})
				})
				call2 := macro2.Stop()

				refX := labelX + float32(dims.Size.X) + 6
				tStack2 := op.Offset(image.Pt(int(refX), int(labelY-2))).Push(gtx.Ops)
				call2.Add(gtx.Ops)
				tStack2.Pop()
			}
		}
	}
}

// Draw a Merge Zone / Feature Hull: translucent capsule enclosing commits of a branch merging into master
func (g *Graph3DView) drawMergeZone(gtx layout.Context, c mergeCluster, minPt, maxPt f32.Point, pal Palette) {
	padX := float32(24.0)
	padY := float32(16.0)
	x0 := minPt.X - padX
	y0 := minPt.Y - padY
	x1 := maxPt.X + padX
	y1 := maxPt.Y + padY

	w := int(x1 - x0)
	h := int(y1 - y0)
	if w < 50 || h < 35 {
		return
	}

	bgCol := color.NRGBA{R: c.col.R, G: c.col.G, B: c.col.B, A: 24}
	borderCol := color.NRGBA{R: c.col.R, G: c.col.G, B: c.col.B, A: 110}

	offset := op.Offset(image.Pt(int(x0), int(y0))).Push(gtx.Ops)
	r := gtx.Dp(unit.Dp(12))
	rr := clip.RRect{
		Rect: image.Rect(0, 0, w, h),
		SE:   r, SW: r, NW: r, NE: r,
	}
	paint.FillShape(gtx.Ops, bgCol, rr.Op(gtx.Ops))
	paint.FillShape(gtx.Ops, borderCol, clip.Stroke{Path: rr.Path(gtx.Ops), Width: 1.5}.Op())

	// Floating Author & Feature Banner at top of the Merge Zone
	if g.zoom >= 0.28 {
		badgeMacro := op.Record(gtx.Ops)
		badgeBg := color.NRGBA{R: pal.HeaderBg.R, G: pal.HeaderBg.G, B: pal.HeaderBg.B, A: 245}
		drawWithRoundedBg(gtx, badgeBg, unit.Dp(4), func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(3), Bottom: unit.Dp(3), Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						sz := gtx.Dp(unit.Dp(7))
						paint.FillShape(gtx.Ops, c.col, clip.Ellipse{Min: image.Pt(0, 0), Max: image.Pt(sz, sz)}.Op(gtx.Ops))
						return layout.Dimensions{Size: image.Pt(sz, sz)}
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(5)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(g.app.theme, unit.Sp(9.5), c.author)
						lbl.Font.Weight = font.Bold
						lbl.Color = pal.Text
						return lbl.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(4)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						desc := fmt.Sprintf("• %d commits", c.commitCount)
						if c.branch != "" {
							desc = fmt.Sprintf("• %s (%d)", c.branch, c.commitCount)
						}
						lbl := material.Label(g.app.theme, unit.Sp(9), desc)
						lbl.Color = pal.TextMuted
						return lbl.Layout(gtx)
					}),
				)
			})
		})
		badgeCall := badgeMacro.Stop()
		bStack := op.Offset(image.Pt(10, 8)).Push(gtx.Ops)
		badgeCall.Add(gtx.Ops)
		bStack.Pop()
	}
	offset.Pop()
}

// Regular Commit: 3D Spherical bead with specular lighting
func (g *Graph3DView) draw3DSphere(gtx layout.Context, center f32.Point, rad float32, baseCol color.NRGBA, isSelected bool, pal Palette) {
	if isSelected {
		haloR := rad + 6
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(center.X-haloR, center.Y))
		p.Arc(f32.Pt(center.X-haloR, center.Y), f32.Pt(center.X+haloR, center.Y), math.Pi)
		p.Arc(f32.Pt(center.X+haloR, center.Y), f32.Pt(center.X-haloR, center.Y), math.Pi)
		p.Close()
		paint.FillShape(gtx.Ops, color.NRGBA{R: pal.Accent.R, G: pal.Accent.G, B: pal.Accent.B, A: 180}, clip.Stroke{Path: p.End(), Width: 3.5}.Op())
	}

	// Base circle
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(center.X-rad, center.Y))
	p.Arc(f32.Pt(center.X-rad, center.Y), f32.Pt(center.X+rad, center.Y), math.Pi)
	p.Arc(f32.Pt(center.X+rad, center.Y), f32.Pt(center.X-rad, center.Y), math.Pi)
	p.Close()
	paint.FillShape(gtx.Ops, baseCol, clip.Outline{Path: p.End()}.Op())

	// Specular highlight on top-left for 3D illusion
	hiRad := rad * 0.45
	hiX := center.X - rad*0.3
	hiY := center.Y - rad*0.3
	var hiPath clip.Path
	hiPath.Begin(gtx.Ops)
	hiPath.MoveTo(f32.Pt(hiX-hiRad, hiY))
	hiPath.Arc(f32.Pt(hiX-hiRad, hiY), f32.Pt(hiX+hiRad, hiY), math.Pi)
	hiPath.Arc(f32.Pt(hiX+hiRad, hiY), f32.Pt(hiX-hiRad, hiY), math.Pi)
	hiPath.Close()
	paint.FillShape(gtx.Ops, shadeColor(baseCol, 1.55), clip.Outline{Path: hiPath.End()}.Op())

	// Subtle border
	var borderPath clip.Path
	borderPath.Begin(gtx.Ops)
	borderPath.MoveTo(f32.Pt(center.X-rad, center.Y))
	borderPath.Arc(f32.Pt(center.X-rad, center.Y), f32.Pt(center.X+rad, center.Y), math.Pi)
	borderPath.Arc(f32.Pt(center.X+rad, center.Y), f32.Pt(center.X-rad, center.Y), math.Pi)
	borderPath.Close()
	paint.FillShape(gtx.Ops, shadeColor(baseCol, 0.6), clip.Stroke{Path: borderPath.End(), Width: 1.0}.Op())
}

// Merge Commit: 3D Diamond / Octahedral Hub with glowing halo and facets
func (g *Graph3DView) draw3DDiamond(gtx layout.Context, center f32.Point, rad float32, baseCol color.NRGBA, isSelected bool, pal Palette) {
	// Glowing halo ring around Merge Commit
	haloR := rad + 7
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(f32.Pt(center.X-haloR, center.Y))
	p.Arc(f32.Pt(center.X-haloR, center.Y), f32.Pt(center.X+haloR, center.Y), math.Pi)
	p.Arc(f32.Pt(center.X+haloR, center.Y), f32.Pt(center.X-haloR, center.Y), math.Pi)
	p.Close()
	glowCol := color.NRGBA{R: baseCol.R, G: baseCol.G, B: baseCol.B, A: 90}
	if isSelected {
		glowCol = color.NRGBA{R: pal.Accent.R, G: pal.Accent.G, B: pal.Accent.B, A: 200}
	}
	paint.FillShape(gtx.Ops, glowCol, clip.Stroke{Path: p.End(), Width: 2.8}.Op())

	w := rad * 1.25
	h := rad * 1.45

	pTop := f32.Pt(center.X, center.Y-h)
	pBottom := f32.Pt(center.X, center.Y+h)
	pLeft := f32.Pt(center.X-w, center.Y)
	pRight := f32.Pt(center.X+w, center.Y)
	pCenter := f32.Pt(center.X, center.Y)

	topCol := shadeColor(baseCol, 1.45)
	leftCol := shadeColor(baseCol, 1.1)
	rightCol := shadeColor(baseCol, 0.85)
	bottomCol := shadeColor(baseCol, 0.65)

	// 4 facets for 3D diamond
	drawPolygon(gtx, []f32.Point{pTop, pLeft, pCenter}, topCol)
	drawPolygon(gtx, []f32.Point{pTop, pRight, pCenter}, leftCol)
	drawPolygon(gtx, []f32.Point{pBottom, pLeft, pCenter}, rightCol)
	drawPolygon(gtx, []f32.Point{pBottom, pRight, pCenter}, bottomCol)

	// Crisp border
	var bPath clip.Path
	bPath.Begin(gtx.Ops)
	bPath.MoveTo(pTop)
	bPath.LineTo(pRight)
	bPath.LineTo(pBottom)
	bPath.LineTo(pLeft)
	bPath.Close()
	paint.FillShape(gtx.Ops, color.NRGBA{R: 255, G: 255, B: 255, A: 210}, clip.Stroke{Path: bPath.End(), Width: 1.8}.Op())
}

func drawPolygon(gtx layout.Context, pts []f32.Point, col color.NRGBA) {
	if len(pts) < 3 {
		return
	}
	var p clip.Path
	p.Begin(gtx.Ops)
	p.MoveTo(pts[0])
	for i := 1; i < len(pts); i++ {
		p.LineTo(pts[i])
	}
	p.Close()
	paint.FillShape(gtx.Ops, col, clip.Outline{Path: p.End()}.Op())
}

func shadeColor(c color.NRGBA, factor float64) color.NRGBA {
	r := float64(c.R) * factor
	g := float64(c.G) * factor
	b := float64(c.B) * factor
	if r > 255 {
		r = 255
	}
	if g > 255 {
		g = 255
	}
	if b > 255 {
		b = 255
	}
	return color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: c.A}
}

func getAuthorColor(author string) color.NRGBA {
	authorColors := []color.NRGBA{
		{R: 56, G: 139, B: 253, A: 255},  // Blue
		{R: 163, G: 113, B: 247, A: 255}, // Purple / Violet
		{R: 63, G: 185, B: 80, A: 255},   // Green
		{R: 240, G: 136, B: 62, A: 255},  // Orange
		{R: 247, G: 120, B: 186, A: 255}, // Pink
		{R: 57, G: 197, B: 207, A: 255},  // Cyan
		{R: 227, G: 179, B: 65, A: 255},  // Gold / Yellow
		{R: 255, G: 107, B: 129, A: 255}, // Coral
	}
	name := strings.ToLower(strings.TrimSpace(author))
	if name == "" {
		return authorColors[0]
	}
	hash := 0
	for _, c := range name {
		hash = (hash*31 + int(c)) & 0x7FFFFFFF
	}
	return authorColors[hash%len(authorColors)]
}

func project3D(x, y, z, cx, cy, cz, yaw, pitch, zoom, screenW, screenH, panX, panY float64) (screenX, screenY, depth float64, visible bool) {
	dx := x - cx
	dy := y - cy
	dz := z - cz

	cosY, sinY := math.Cos(yaw), math.Sin(yaw)
	x1 := dx*cosY - dz*sinY
	z1 := dx*sinY + dz*cosY

	cosP, sinP := math.Cos(pitch), math.Sin(pitch)
	y2 := dy*cosP - z1*sinP
	z2 := dy*sinP + z1*cosP

	fov := 700.0
	dist := fov + z2
	if dist <= 80.0 {
		return 0, 0, -99999, false
	}
	scale := (fov / dist) * zoom

	screenX = (screenW / 2) + x1*scale + panX
	screenY = (screenH / 2) + y2*scale + panY
	depth = z2
	visible = true
	return
}

func (g *Graph3DView) renderInspectorCard(gtx layout.Context, n *git.Graph3DNode, pal Palette) layout.Dimensions {
	if g.btnCopyHash.Clicked(gtx) {
		gtx.Execute(clipboard.WriteCmd{
			Type: "application/text",
			Data: io.NopCloser(strings.NewReader(n.Hash)),
		})
		g.app.copiedHash = n.ShortHash
	}
	if g.btnCloseCard.Clicked(gtx) {
		g.selectedHash = ""
	}

	return drawWithBorder(gtx, pal.HeaderBg, unit.Dp(8), unit.Dp(1), pal.Border, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(14), Right: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				// Header: Type badge + Hash copy badge + Close button
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if n.IsMerge {
								return g.app.drawTag(gtx, "MERGE COMMIT", color.NRGBA{R: 163, G: 113, B: 247, A: 255}, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
							}
							return g.app.drawTag(gtx, "COMMIT", pal.BadgeBg, pal.Accent)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
							isCopied := g.app.copiedHash == n.ShortHash
							return g.app.drawCopyBadge(gtx, &g.btnCopyHash, n.Hash, n.ShortHash, isCopied, pal)
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return g.app.drawIconButton(gtx, &g.btnCloseCard, g.app.icons.Close, false, pal)
						}),
					)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),

				// Message
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(g.app.theme, unit.Sp(11.5), n.Message)
					lbl.Font.Weight = font.Bold
					lbl.Color = pal.Text
					return lbl.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),

				// Author & Date
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					sub := fmt.Sprintf("%s  •  %s", n.AuthorName, formatRelativeTime(n.Date))
					lbl := material.Label(g.app.theme, unit.Sp(10), sub)
					lbl.Color = pal.TextMuted
					return lbl.Layout(gtx)
				}),

				// Parents info (if merge)
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if len(n.ParentHashes) > 1 {
						pShorts := make([]string, len(n.ParentHashes))
						for idx, ph := range n.ParentHashes {
							if len(ph) > 7 {
								pShorts[idx] = ph[:7]
							} else {
								pShorts[idx] = ph
							}
						}
						info := "Parents: " + strings.Join(pShorts, ", ")
						return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							lbl := material.Label(g.app.theme, unit.Sp(9.5), info)
							lbl.Color = pal.Accent
							return lbl.Layout(gtx)
						})
					}
					return layout.Dimensions{}
				}),
			)
		})
	})
}
