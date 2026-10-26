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

	// Pointer interaction
	lastPos    f32.Point
	dragStart  f32.Point
	isDragging bool

	// Selected & Hovered state
	selectedHash string

	// Camera Preset Buttons
	btnIso        widget.Clickable
	btnTop        widget.Clickable
	btnFront      widget.Clickable
	btnReset      widget.Clickable
	btnAutoRotate widget.Clickable
	btnZoomIn     widget.Clickable
	btnZoomOut    widget.Clickable
	btnCopyHash   widget.Clickable

	// Screen projected positions for hit testing
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

func NewGraph3DView(app *App) *Graph3DView {
	return &Graph3DView{
		app:   app,
		yaw:   0.38,
		pitch: 0.24,
		zoom:  1.0,
		panX:  0,
		panY:  -30,
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

	// 1. Process Pointer Events on the 3D Canvas
	g.handlePointer(gtx)

	// 2. Handle Auto-Rotation
	if g.autoRotate {
		g.yaw += 0.005
		if g.yaw > math.Pi*2 {
			g.yaw -= math.Pi * 2
		}
		gtx.Execute(op.InvalidateCmd{})
	}

	return layout.Stack{}.Layout(gtx,
		// Background & 3D Render Canvas
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			// Canvas background
			paint.FillShape(gtx.Ops, pal.Bg, clip.Rect(image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Max.Y)).Op())

			// Register pointer hit area (entire canvas area)
			area := clip.Rect(image.Rect(0, 0, gtx.Constraints.Max.X, gtx.Constraints.Max.Y)).Push(gtx.Ops)
			event.Op(gtx.Ops, g)

			// Render the 3D Scene
			g.render3DScene(gtx, data, pal)
			area.Pop()
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}),

		// Top Controls Toolbar (Presets, Camera, Zoom)
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(14), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				return drawWithBorder(gtx, pal.HeaderBg, unit.Dp(8), unit.Dp(1), pal.Border, func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(14), Right: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
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
							layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								w := gtx.Dp(unit.Dp(1))
								h := gtx.Dp(unit.Dp(18))
								paint.FillShape(gtx.Ops, pal.Border, clip.Rect(image.Rect(0, 0, w, h)).Op())
								return layout.Dimensions{Size: image.Pt(w, h)}
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(14)}.Layout),
							// Presets
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return g.app.drawPillButton(gtx, &g.btnIso, "Isometric (3D)", false, pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return g.app.drawPillButton(gtx, &g.btnTop, "Top (2D DAG)", false, pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return g.app.drawPillButton(gtx, &g.btnFront, "Front (Timeline)", false, pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return g.app.drawPillButton(gtx, &g.btnReset, "Reset View", false, pal)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								rotText := "Auto-Rotate"
								if g.autoRotate {
									rotText = "Stop Rotate"
								}
								return g.app.drawPillButton(gtx, &g.btnAutoRotate, rotText, g.autoRotate, pal)
							}),
							layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
							// Zoom level indicator and buttons
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

		// Instructions badge at bottom left
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			if g.selectedHash != "" {
				return layout.Dimensions{}
			}
			return layout.SW.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(16), Left: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return drawWithBorder(gtx, pal.HeaderBg, unit.Dp(6), unit.Dp(1), pal.Border, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(g.app.theme, unit.Sp(10.5), "Controls: ")
									lbl.Font.Weight = font.Bold
									lbl.Color = pal.Accent
									return lbl.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									lbl := material.Label(g.app.theme, unit.Sp(10.5), "Left drag: Rotate  •  Right drag / Shift: Pan  •  Wheel: Zoom in/out  •  Click node to inspect")
									lbl.Color = pal.Text
									return lbl.Layout(gtx)
								}),
							)
						})
					})
				})
			})
		}),

		// Floating 3D Commit Inspector Card (when node selected)
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

			return layout.SE.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Max.X = gtx.Dp(unit.Dp(360))
					return g.renderInspectorCard(gtx, selNode, pal)
				})
			})
		}),
	)
}

func (g *Graph3DView) handlePointer(gtx layout.Context) {
	// Handle button clicks
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
		g.yaw = 0.38
		g.pitch = 0.24
		g.zoom = 1.0
		g.panX = 0
		g.panY = -30
		g.autoRotate = false
	}
	if g.btnAutoRotate.Clicked(gtx) {
		g.autoRotate = !g.autoRotate
	}
	if g.btnZoomIn.Clicked(gtx) {
		g.zoom = math.Min(5.0, g.zoom*1.2)
	}
	if g.btnZoomOut.Clicked(gtx) {
		g.zoom = math.Max(0.20, g.zoom*0.8)
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
				g.lastPos = e.Position
				g.dragStart = e.Position
				g.isDragging = true
			case pointer.Drag:
				deltaX := float64(e.Position.X - g.lastPos.X)
				deltaY := float64(e.Position.Y - g.lastPos.Y)
				g.lastPos = e.Position

				if e.Buttons.Contain(pointer.ButtonSecondary) || e.Modifiers.Contain(key.ModShift) {
					g.panX += deltaX
					g.panY += deltaY
				} else {
					g.yaw += deltaX * 0.006
					g.pitch += deltaY * 0.006
					if g.pitch < -1.4 {
						g.pitch = -1.4
					}
					if g.pitch > 1.4 {
						g.pitch = 1.4
					}
				}
				gtx.Execute(op.InvalidateCmd{})
			case pointer.Release:
				g.isDragging = false
				dx := e.Position.X - g.dragStart.X
				dy := e.Position.Y - g.dragStart.Y
				if dx*dx+dy*dy < 36 {
					// Hit test nodes
					g.hitTestNode(e.Position)
				}
				gtx.Execute(op.InvalidateCmd{})
			case pointer.Scroll:
				if e.Modifiers.Contain(key.ModShift) {
					// Shift+Wheel: horizontal pan
					g.panX -= float64(e.Scroll.Y) * 0.5
				} else if e.Scroll.Y != 0 {
					// Mouse wheel zoom
					if e.Scroll.Y > 0 {
						g.zoom = math.Max(0.15, g.zoom*0.88)
					} else {
						g.zoom = math.Min(6.0, g.zoom*1.14)
					}
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
		if dist <= pn.radius+10 && dist < bestDist {
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

func (g *Graph3DView) render3DScene(gtx layout.Context, data *git.Graph3DData, pal Palette) {
	if data == nil || len(data.Nodes) == 0 {
		return
	}

	screenW := float64(gtx.Constraints.Max.X)
	screenH := float64(gtx.Constraints.Max.Y)

	// Calculate center of graph in 3D
	maxLane := 0
	for _, n := range data.Nodes {
		if n.Lane > maxLane {
			maxLane = n.Lane
		}
	}
	cx := float64(maxLane) * 50.0
	// Centered on recent commits (HEAD is at Order 0)
	cy := 180.0
	cz := 0.0

	laneColors := []color.NRGBA{
		{R: 56, G: 139, B: 253, A: 255},  // Lane 0: Master (Blue)
		{R: 63, G: 185, B: 80, A: 255},   // Lane 1: Feature 1 (Green)
		{R: 163, G: 113, B: 247, A: 255}, // Lane 2: Feature 2 (Purple)
		{R: 240, G: 136, B: 62, A: 255},  // Lane 3: Feature 3 (Orange)
		{R: 247, G: 120, B: 186, A: 255}, // Lane 4: Feature 4 (Pink)
		{R: 57, G: 197, B: 207, A: 255},  // Lane 5: Feature 5 (Teal)
	}

	getLaneColor := func(lane int) color.NRGBA {
		if lane < 0 {
			lane = -lane
		}
		return laneColors[lane%len(laneColors)]
	}

	// 0. Render subtle 3D floor runway lines for lanes
	gridCol := color.NRGBA{R: pal.Border.R, G: pal.Border.G, B: pal.Border.B, A: 50}
	for l := 0; l <= maxLane+1; l++ {
		lx := float64(l) * 100.0
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
		worldX := float64(n.Lane) * 100.0
		worldY := float64(n.Order) * 60.0
		worldZ := 0.0
		if n.IsMerge {
			worldZ = 35.0 // Elevated in 3D
		}

		sx, sy, depth, visible := project3D(worldX, worldY, worldZ, cx, cy, cz, g.yaw, g.pitch, g.zoom, screenW, screenH, g.panX, g.panY)

		col := getLaneColor(n.Lane)
		if n.IsMerge {
			col = color.NRGBA{R: 220, G: 100, B: 255, A: 255} // Purple/Magenta for Merges
		}

		rad := float32(11.0 * g.zoom)
		if rad < 5 {
			rad = 5
		}
		if rad > 24 {
			rad = 24
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

	// 2. Project Edges
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
		edgeCol.A = 180
		if e.IsMerge {
			edgeCol = color.NRGBA{R: 230, G: 120, B: 255, A: 220}
		}

		projEdges = append(projEdges, projectedEdge{
			from:    pFrom.screenPos,
			to:      pTo.screenPos,
			depth:   (pFrom.depth + pTo.depth) / 2,
			isMerge: e.IsMerge,
			col:     edgeCol,
		})
	}

	// 3. Render Edges (Depth Sorted: back to front)
	sort.Slice(projEdges, func(i, j int) bool {
		return projEdges[i].depth < projEdges[j].depth
	})

	for _, e := range projEdges {
		var path clip.Path
		path.Begin(gtx.Ops)
		path.MoveTo(e.from)
		if e.isMerge {
			// 3D Arc between branches
			midX := (e.from.X + e.to.X) / 2
			midY := (e.from.Y+e.to.Y)/2 - float32(25*g.zoom)
			path.QuadTo(f32.Pt(midX, midY), e.to)
			strokeW := float32(2.5 * g.zoom)
			if strokeW < 1.5 {
				strokeW = 1.5
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

		// 3D Volumetric Block / Prism
		g.draw3DBlock(gtx, pn.screenPos, pn.radius, pn.col, pn.isMerge, isSelected, pal)

		// Label (ShortHash + Branch tag)
		if g.zoom >= 0.60 {
			labelX := pn.screenPos.X + pn.radius + 8
			labelY := pn.screenPos.Y - 6

			macro := op.Record(gtx.Ops)
			lbl := material.Label(g.app.theme, unit.Sp(9.5), pn.node.ShortHash)
			if isSelected {
				lbl.Font.Weight = font.Bold
				lbl.Color = pal.Accent
			} else {
				lbl.Color = pal.Text
			}
			dims := lbl.Layout(gtx)
			call := macro.Stop()

			tStack := op.Offset(image.Pt(int(labelX), int(labelY))).Push(gtx.Ops)
			call.Add(gtx.Ops)
			tStack.Pop()

			// If has branch ref
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

// Draw a stylized 3D volumetric block with isometric top, front, and side faces
func (g *Graph3DView) draw3DBlock(gtx layout.Context, center f32.Point, rad float32, baseCol color.NRGBA, isMerge, isSelected bool, pal Palette) {
	// Selection Halo
	if isSelected {
		haloR := rad + 6
		var p clip.Path
		p.Begin(gtx.Ops)
		p.MoveTo(f32.Pt(center.X-haloR, center.Y))
		p.Arc(f32.Pt(center.X-haloR, center.Y), f32.Pt(center.X+haloR, center.Y), math.Pi)
		p.Arc(f32.Pt(center.X+haloR, center.Y), f32.Pt(center.X-haloR, center.Y), math.Pi)
		p.Close()
		paint.FillShape(gtx.Ops, color.NRGBA{R: pal.Accent.R, G: pal.Accent.G, B: pal.Accent.B, A: 160}, clip.Stroke{Path: p.End(), Width: 3.5}.Op())
	}

	w := rad * 1.5
	h := rad * 0.9
	d := rad * 0.7

	// Colors for 3D shading
	topCol := shadeColor(baseCol, 1.35)  // Illuminated top
	frontCol := baseCol                 // Base front
	sideCol := shadeColor(baseCol, 0.7) // Shadow side

	// Vertices for 3D block
	p0 := f32.Pt(center.X, center.Y-h-d)
	p1 := f32.Pt(center.X+w, center.Y-d)
	p2 := f32.Pt(center.X, center.Y+h-d)
	p3 := f32.Pt(center.X-w, center.Y-d)

	p4 := f32.Pt(center.X, center.Y+h)
	p5 := f32.Pt(center.X-w, center.Y)
	p6 := f32.Pt(center.X+w, center.Y)

	// 1. Front-Left Face
	drawPolygon(gtx, []f32.Point{p3, p2, p4, p5}, frontCol)

	// 2. Front-Right (Side) Face
	drawPolygon(gtx, []f32.Point{p2, p1, p6, p4}, sideCol)

	// 3. Top Face (Illuminated)
	drawPolygon(gtx, []f32.Point{p0, p1, p2, p3}, topCol)

	// 4. Edges stroke
	edgeCol := shadeColor(baseCol, 0.5)
	var edgePath clip.Path
	edgePath.Begin(gtx.Ops)
	edgePath.MoveTo(p0)
	edgePath.LineTo(p1)
	edgePath.LineTo(p6)
	edgePath.LineTo(p4)
	edgePath.LineTo(p5)
	edgePath.LineTo(p3)
	edgePath.Close()
	paint.FillShape(gtx.Ops, edgeCol, clip.Stroke{Path: edgePath.End(), Width: 1.0}.Op())

	// If merge, draw central elevated glowing core
	if isMerge {
		coreR := rad * 0.55
		var cp clip.Path
		cp.Begin(gtx.Ops)
		cp.MoveTo(f32.Pt(center.X-coreR, center.Y-d))
		cp.LineTo(f32.Pt(center.X, center.Y-coreR-d))
		cp.LineTo(f32.Pt(center.X+coreR, center.Y-d))
		cp.LineTo(f32.Pt(center.X, center.Y+coreR-d))
		cp.Close()
		paint.FillShape(gtx.Ops, color.NRGBA{R: 255, G: 255, B: 255, A: 230}, clip.Outline{Path: cp.End()}.Op())
	}
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

	return drawWithBorder(gtx, pal.HeaderBg, unit.Dp(8), unit.Dp(1), pal.Border, func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12), Left: unit.Dp(14), Right: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				// Header: Type badge + Hash copy badge
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							if n.IsMerge {
								return g.app.drawTag(gtx, "MERGE COMMIT", color.NRGBA{R: 163, G: 113, B: 247, A: 255}, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
							}
							return g.app.drawTag(gtx, "COMMIT", pal.BadgeBg, pal.Accent)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							isCopied := g.app.copiedHash == n.ShortHash
							return g.app.drawCopyBadge(gtx, &g.btnCopyHash, n.Hash, n.ShortHash, isCopied, pal)
						}),
					)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
				// Commit message
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					lbl := material.Label(g.app.theme, unit.Sp(12), n.Message)
					lbl.Font.Weight = font.Bold
					lbl.Color = pal.Text
					return lbl.Layout(gtx)
				}),
				layout.Rigid(layout.Spacer{Height: unit.Dp(6)}.Layout),
				// Author + Date
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					meta := fmt.Sprintf("%s  •  %s", n.AuthorName, formatRelativeTime(n.Date))
					lbl := material.Label(g.app.theme, unit.Sp(10.5), meta)
					lbl.Color = pal.TextMuted
					return lbl.Layout(gtx)
				}),
				// Parents row
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if len(n.ParentHashes) == 0 {
						return layout.Dimensions{}
					}
					var parentShorts []string
					for _, p := range n.ParentHashes {
						if len(p) > 7 {
							parentShorts = append(parentShorts, p[:7])
						} else {
							parentShorts = append(parentShorts, p)
						}
					}
					return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(g.app.theme, unit.Sp(10), fmt.Sprintf("Parents: %s", strings.Join(parentShorts, ", ")))
						lbl.Color = pal.TextMuted
						return lbl.Layout(gtx)
					})
				}),
			)
		})
	})
}
