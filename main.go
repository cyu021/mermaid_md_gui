package main

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

func logMsg(msg string) {
	pc, file, line, ok := runtime.Caller(1)
	if !ok {
		fmt.Printf("[%s] %s\n", time.Now().Format("2006-01-02 15:04:05.000"), msg)
		return
	}
	fn := runtime.FuncForPC(pc).Name()
	if idx := strings.LastIndex(fn, "."); idx != -1 { fn = fn[idx+1:] }
	if idx := strings.LastIndex(file, "/"); idx != -1 { file = file[idx+1:] }
	fmt.Printf("[%s] %s (%s:%d) - %s\n", time.Now().Format("2006-01-02 15:04:05.000"), fn, file, line, msg)
}

type Node struct {
	Text      string
	Lines     []string
	Children  []*Node
	Level     int
	Shape     string
	LineIdx   int
	X, Y      float32
	Width     float32
	Height    float32
	Collapsed bool
	Color     color.Color
}

func (n *Node) WidthWithIcons() float32 {
	if len(n.Children) > 0 { return n.Width + 40 }
	return n.Width
}

type interactiveBackground struct {
	widget.BaseWidget
	app *editorApp
}

func newInteractiveBackground(a *editorApp) *interactiveBackground {
	b := &interactiveBackground{app: a}
	b.ExtendBaseWidget(b)
	return b
}

func (b *interactiveBackground) CreateRenderer() fyne.WidgetRenderer {
	rect := canvas.NewRectangle(color.NRGBA{R: 25, G: 25, B: 30, A: 255})
	return &backgroundRenderer{rect: rect}
}

type backgroundRenderer struct {
	rect *canvas.Rectangle
}
func (r *backgroundRenderer) Layout(s fyne.Size) { r.rect.Resize(s) }
func (r *backgroundRenderer) MinSize() fyne.Size { return fyne.NewSize(4000, 4000) }
func (r *backgroundRenderer) Refresh() { r.rect.Refresh() }
func (r *backgroundRenderer) Objects() []fyne.CanvasObject { return []fyne.CanvasObject{r.rect} }
func (r *backgroundRenderer) Destroy() {}

func (b *interactiveBackground) Dragged(e *fyne.DragEvent) {
	if b.app.scroll == nil { return }
	b.app.scroll.Offset.X -= e.Dragged.DX
	b.app.scroll.Offset.Y -= e.Dragged.DY
	b.app.scroll.Refresh()
	if b.app.miniMap != nil { b.app.miniMap.Refresh() }
}
func (b *interactiveBackground) DragEnd() {}
func (b *interactiveBackground) Tapped(e *fyne.PointEvent) {}
func (b *interactiveBackground) TappedSecondary(e *fyne.PointEvent) {
	txt := strings.TrimSpace(b.app.entry.Text)
	if txt == "" || !strings.Contains(txt, "mindmap") {
		b.app.entry.SetText("mindmap\n  New Root")
	}
}
func (b *interactiveBackground) Cursor() desktop.Cursor { return desktop.DefaultCursor }

type centeringTrigger struct {
	widget.BaseWidget
	app  *editorApp
	once sync.Once
}

func newCenteringTrigger(e *editorApp) *centeringTrigger {
	c := &centeringTrigger{app: e}
	c.ExtendBaseWidget(c)
	return c
}

func (c *centeringTrigger) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

func (c *centeringTrigger) Layout(size fyne.Size) {
	if c.app.isReady && size.Width > 100 {
		c.once.Do(func() {
			logMsg("GUI detected as visible and ready, triggering initial centering...")
			c.app.centerOnRoot()
		})
	}
}

type miniMapWidget struct {
	widget.BaseWidget
	app *editorApp
	bg  *canvas.Rectangle
	ind *canvas.Rectangle
	dots *fyne.Container
}

func newMiniMap(e *editorApp) *miniMapWidget {
	m := &miniMapWidget{app: e}
	m.bg = canvas.NewRectangle(color.NRGBA{R: 20, G: 20, B: 25, A: 220})
	m.bg.StrokeColor = theme.PrimaryColor()
	m.bg.StrokeWidth = 1
	m.ind = canvas.NewRectangle(color.NRGBA{R: 255, G: 255, B: 255, A: 40})
	m.ind.StrokeColor = color.NRGBA{R: 255, G: 255, B: 255, A: 180}
	m.ind.StrokeWidth = 1
	m.dots = container.NewWithoutLayout()
	m.ExtendBaseWidget(m)
	return m
}

func (m *miniMapWidget) CreateRenderer() fyne.WidgetRenderer {
	return &miniMapRenderer{m: m, objects: []fyne.CanvasObject{m.bg, m.dots, m.ind}}
}

func (m *miniMapWidget) Dragged(e *fyne.DragEvent) { m.panToMiniMapPos(e.Position) }
func (m *miniMapWidget) DragEnd() {}

func (m *miniMapWidget) panToMiniMapPos(pos fyne.Position) {
	if m.app.scroll == nil { return }
	curSize := m.Size()
	if curSize.Width <= 0 || curSize.Height <= 0 { return }
	ratioX, ratioY := 4000.0/curSize.Width, 4000.0/curSize.Height
	z := m.app.zoomScale
	vSize := m.app.scroll.Size()
	m.app.scroll.Offset = fyne.NewPos(pos.X*ratioX*z-vSize.Width/2, pos.Y*ratioY*z-vSize.Height/2)
	m.app.scroll.Refresh(); m.Refresh()
}

type miniMapRenderer struct {
	m       *miniMapWidget
	objects []fyne.CanvasObject
}

func (r *miniMapRenderer) Layout(size fyne.Size) { r.m.bg.Resize(size); r.m.updateIndicator() }
func (r *miniMapRenderer) MinSize() fyne.Size { return fyne.NewSize(100, 100) }
func (r *miniMapRenderer) Refresh() { r.m.updateIndicator(); canvas.Refresh(r.m.ind); canvas.Refresh(r.m.dots) }
func (r *miniMapRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *miniMapRenderer) Destroy()                     {}

func (m *miniMapWidget) updateIndicator() {
	if m.app.scroll == nil { return }
	curSize := m.Size()
	if curSize.Width <= 0 || curSize.Height <= 0 { return }
	ratioX, ratioY := curSize.Width/4000.0, curSize.Height/4000.0
	vSize, offset, z := m.app.scroll.Size(), m.app.scroll.Offset, m.app.zoomScale
	m.ind.Resize(fyne.NewSize((vSize.Width/z)*ratioX, (vSize.Height/z)*ratioY))
	m.ind.Move(fyne.NewPos((offset.X/z)*ratioX, (offset.Y/z)*ratioY))
}

func (m *miniMapWidget) updateDots(root *Node) {
	if root == nil { return }
	m.dots.Objects = nil
	m.addNodeDot(root)
}

func (m *miniMapWidget) addNodeDot(n *Node) {
	if n == nil { return }
	curSize := m.Size()
	if curSize.Width <= 0 { curSize = fyne.NewSize(120, 120) }
	ratioX, ratioY := curSize.Width/4000.0, curSize.Height/4000.0
	col := n.Color
	if col == nil { col = theme.PrimaryColor() }
	dot := canvas.NewRectangle(col)
	dot.Resize(fyne.NewSize(2, 2))
	dot.Move(fyne.NewPos(n.X*ratioX-1, n.Y*ratioY-1))
	m.dots.Add(dot)
	if !n.Collapsed {
		for _, child := range n.Children { m.addNodeDot(child) }
	}
}

type nodeWidget struct {
	widget.BaseWidget
	node     *Node
	app      *editorApp
	rect     *canvas.Rectangle
	texts    []*canvas.Text
	iconText *canvas.Text
	iconBox  *canvas.Rectangle
	scale    float32
}

func newNodeWidget(n *Node, e *editorApp, scale float32) *nodeWidget {
	w := &nodeWidget{node: n, app: e, scale: scale}
	w.rect = canvas.NewRectangle(theme.ButtonColor())
	w.rect.StrokeColor = n.Color
	if w.rect.StrokeColor == nil {
		w.rect.StrokeColor = theme.PrimaryColor()
	}
	w.rect.StrokeWidth = e.lineThickness * scale
	w.rect.CornerRadius = 8 * scale
	if n.Shape == "circle" { w.rect.CornerRadius = 20 * scale }
	if n.Shape == "square" { w.rect.CornerRadius = 0 }

	for _, txt := range n.Lines {
		t := canvas.NewText(txt, theme.ForegroundColor())
		t.Alignment = fyne.TextAlignCenter
		t.TextSize = 13 * scale
		if n.Level == 0 { t.TextStyle.Bold = true; t.TextSize = 15 * scale }
		w.texts = append(w.texts, t)
	}

	if len(n.Children) > 0 {
		iconChar := "-"
		iconColor := color.NRGBA{R: 200, G: 50, B: 50, A: 255}
		if n.Collapsed {
			iconChar = "+"
			iconColor = color.NRGBA{R: 50, G: 200, B: 50, A: 255}
		}
		w.iconText = canvas.NewText(iconChar, iconColor)
		w.iconText.TextSize = 16 * scale
		w.iconText.TextStyle.Bold = true
		w.iconBox = canvas.NewRectangle(color.Transparent)
	}

	w.ExtendBaseWidget(w)
	return w
}

func (w *nodeWidget) CreateRenderer() fyne.WidgetRenderer {
	objs := []fyne.CanvasObject{w.rect}
	for _, t := range w.texts { objs = append(objs, t) }
	if w.iconText != nil { objs = append(objs, w.iconBox, w.iconText) }
	return &nodeRenderer{widget: w, objects: objs}
}

func (w *nodeWidget) Tapped(e *fyne.PointEvent) {
	if w.iconText != nil {
		if e.Position.X > w.Size().Width-30*w.scale { w.app.toggleCollapse(w.node); return }
	}
	w.app.editNodeDialog(w.node)
}

func (w *nodeWidget) TappedSecondary(pe *fyne.PointEvent) {
	items := []*fyne.MenuItem{fyne.NewMenuItem("Add Child", func() { w.app.addChildNode(w.node) })}
	if !strings.EqualFold(strings.TrimSpace(w.node.Text), "mindmap") && w.node.LineIdx >= 0 {
		items = append(items, fyne.NewMenuItem("Add Sibling Before", func() { w.app.addSiblingNode(w.node, true) }))
		items = append(items, fyne.NewMenuItem("Add Sibling After", func() { w.app.addSiblingNode(w.node, false) }))
		items = append(items, fyne.NewMenuItemSeparator())
		items = append(items, fyne.NewMenuItem("Remove Node", func() { w.app.removeNode(w.node) }))
	}
	menu := fyne.NewMenu("", items...)
	widget.ShowPopUpMenuAtPosition(menu, w.app.window.Canvas(), pe.AbsolutePosition)
}

type nodeRenderer struct {
	widget  *nodeWidget
	objects []fyne.CanvasObject
}

func (r *nodeRenderer) Layout(size fyne.Size) {
	r.widget.rect.Resize(size)
	lineH := 20.0 * r.widget.scale
	startY := (size.Height - float32(len(r.widget.texts))*lineH) / 2
	for i, t := range r.widget.texts {
		t.Resize(fyne.NewSize(size.Width, lineH)); t.Move(fyne.NewPos(0, startY+float32(i)*lineH))
	}
	if r.widget.iconText != nil {
		iconAreaWidth := 30 * r.widget.scale
		r.widget.iconBox.Resize(fyne.NewSize(iconAreaWidth, size.Height))
		r.widget.iconBox.Move(fyne.NewPos(size.Width-iconAreaWidth, 0))
		r.widget.iconText.Move(fyne.NewPos(size.Width-iconAreaWidth*0.7, (size.Height-lineH)/2))
	}
}

func (r *nodeRenderer) MinSize() fyne.Size {
	return fyne.NewSize(r.widget.node.WidthWithIcons()*r.widget.scale, r.widget.node.Height*r.widget.scale)
}

func (r *nodeRenderer) Refresh() {
	r.widget.rect.FillColor = theme.ButtonColor(); r.widget.rect.StrokeWidth = r.widget.app.lineThickness * r.widget.scale
	for _, t := range r.widget.texts {
		t.Color = theme.ForegroundColor(); t.TextSize = 13 * r.widget.scale
		if r.widget.node.Level == 0 { t.TextSize = 15 * r.widget.scale }
	}
	canvas.Refresh(r.widget)
}
func (r *nodeRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *nodeRenderer) Destroy()                    {}

func parseMermaidMindmap(input string, e *editorApp) *Node {
	scanner := bufio.NewScanner(strings.NewReader(input))
	var root *Node
	var stack []*Node
	lineIdx := -1
	for scanner.Scan() {
		lineIdx++
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "mindmap" || strings.HasPrefix(trimmed, "```") { continue }
		indent := 0
		for _, r := range line {
			if r == ' ' { indent++ } else if r == '\t' { indent += 4 } else { break }
		}
		text := trimmed
		shape := "rounded"
		if strings.HasPrefix(text, "((") && strings.HasSuffix(text, "))") {
			text = text[2 : len(text)-2]; shape = "circle"
		} else if strings.HasPrefix(text, "(") && strings.HasSuffix(text, ")") {
			text = text[1 : len(text)-1]; shape = "rounded"
		} else if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
			text = text[1 : len(text)-1]; shape = "square"
		}

		node := &Node{Text: text, Level: indent, Shape: shape, LineIdx: lineIdx}
		if collapsed, ok := e.collapsedNodes[fmt.Sprintf("%d:%s", indent, text)]; ok {
			node.Collapsed = collapsed
		}

		const maxBoxWidth = 200.0
		const charWidth = 8.0
		const lineHeight = 20.0
		words := strings.Fields(node.Text)
		if len(words) == 0 {
			node.Width, node.Height = 60, 36; node.Lines = []string{""}
		} else {
			currLineW, maxSeenW := 0.0, 0.0
			var lines []string
			var currLine []string
			for _, w := range words {
				wW := float64(len(w)) * charWidth
				if currLineW > 0 && currLineW+charWidth+wW > maxBoxWidth {
					lines = append(lines, strings.Join(currLine, " ")); if currLineW > maxSeenW { maxSeenW = currLineW }
					currLine = []string{w}; currLineW = wW
				} else {
					if len(currLine) > 0 { currLineW += charWidth }
					currLine = append(currLine, w); currLineW += wW
				}
			}
			lines = append(lines, strings.Join(currLine, " ")); if currLineW > maxSeenW { maxSeenW = currLineW }
			node.Lines = lines; node.Width = float32(maxSeenW) + e.nodePadding*2; node.Height = float32(len(lines)*lineHeight) + e.nodePadding*1.5
			if node.Shape == "circle" && node.Width < node.Height { node.Width = node.Height }
		}
		if root == nil {
			root = node; stack = []*Node{node}
		} else {
			for len(stack) > 0 && stack[len(stack)-1].Level >= indent { stack = stack[:len(stack)-1] }
			if len(stack) > 0 {
				parent := stack[len(stack)-1]; parent.Children = append(parent.Children, node); stack = append(stack, node)
			} else {
				root.Children = append(root.Children, node); stack = append(stack, node)
			}
		}
	}
	return root
}

const verticalGap, horizontalGap = 40.0, 120.0

func calculateSubtreeHeight(n *Node) float32 {
	if n.Collapsed || len(n.Children) == 0 { return n.Height + verticalGap }
	var total float32
	for _, child := range n.Children { total += calculateSubtreeHeight(child) }
	if total < n.Height+verticalGap { return n.Height + verticalGap }
	return total
}

func layoutDirectional(n *Node, centerX, centerY float32, direction float32) float32 {
	subtreeHeight := calculateSubtreeHeight(n)
	n.X, n.Y = centerX, centerY
	if n.Collapsed || len(n.Children) == 0 { return subtreeHeight }
	var childrenHeight float32
	for _, child := range n.Children { childrenHeight += calculateSubtreeHeight(child) }
	childX := centerX + direction*(n.WidthWithIcons()/2+horizontalGap)
	childY := centerY - childrenHeight/2
	for _, child := range n.Children {
		sh := calculateSubtreeHeight(child)
		layoutDirectional(child, childX+direction*child.WidthWithIcons()/2, childY+sh/2, direction)
		childY += sh
	}
	return subtreeHeight
}

func layoutBalanced(root *Node, centerX, centerY float32) {
	root.X, root.Y = centerX, centerY
	if root.Collapsed || len(root.Children) == 0 { return }
	mid := (len(root.Children) + 1) / 2
	rightChildren, leftChildren := root.Children[:mid], root.Children[mid:]
	var rh float32; for _, c := range rightChildren { rh += calculateSubtreeHeight(c) }
	rx, ry := centerX+root.WidthWithIcons()/2+horizontalGap, centerY-rh/2
	for _, c := range rightChildren {
		sh := calculateSubtreeHeight(c); layoutDirectional(c, rx+c.WidthWithIcons()/2, ry+sh/2, 1); ry += sh
	}
	var lh float32; for _, c := range leftChildren { lh += calculateSubtreeHeight(c) }
	lx, ly := centerX-root.WidthWithIcons()/2-horizontalGap, centerY-lh/2
	for _, c := range leftChildren {
		sh := calculateSubtreeHeight(c); layoutDirectional(c, lx-c.WidthWithIcons()/2, ly+sh/2, -1); ly += sh
	}
}

func layoutFishbone(root *Node, centerX, centerY float32) {
	root.X, root.Y = centerX, centerY
	if root.Collapsed || len(root.Children) == 0 { return }
	mid := (len(root.Children) + 1) / 2
	rightChildren, leftChildren := root.Children[:mid], root.Children[mid:]
	for i, c := range rightChildren {
		dX, dY := float32(1.0), float32(1.0); if i%2 == 1 { dY = -1.0 }
		sJX := centerX + root.WidthWithIcons()/2 + horizontalGap + float32(i/2)*(horizontalGap*2.0)
		c.X, c.Y = sJX + 100*dX, centerY + 200*dY
		layoutDirectional(c, c.X+dX*(c.WidthWithIcons()/2+horizontalGap), c.Y, dX)
	}
	for i, c := range leftChildren {
		dX, dY := float32(-1.0), float32(1.0); if i%2 == 1 { dY = -1.0 }
		sJX := centerX - root.WidthWithIcons()/2 - horizontalGap - float32(i/2)*(horizontalGap*2.0)
		c.X, c.Y = sJX + 100*dX, centerY + 200*dY
		layoutDirectional(c, c.X+dX*(c.WidthWithIcons()/2+horizontalGap), c.Y, dX)
	}
}

type canvasLayout struct{ app *editorApp }
func (l *canvasLayout) Layout(objects []fyne.CanvasObject, _ fyne.Size) {
	size := fyne.NewSize(4000*l.app.zoomScale, 4000*l.app.zoomScale)
	for _, o := range objects { o.Resize(size); o.Move(fyne.NewPos(0, 0)) }
}
func (l *canvasLayout) MinSize(_ []fyne.CanvasObject) fyne.Size { return fyne.NewSize(4000*l.app.zoomScale, 4000*l.app.zoomScale) }

type editorApp struct {
	mainApp fyne.App; window fyne.Window; entry *widget.Entry; renderArea *fyne.Container; interactiveBG *interactiveBackground; scroll *container.Scroll; canvasContainer *fyne.Container; statusLabel *widget.Label; currentFile fyne.URI; zoomScale float32; rootNode *Node; collapsedNodes map[string]bool; isReady bool; miniMap *miniMapWidget; miniContainer *fyne.Container; toolbar *fyne.Container; layoutMode string; layoutSelect *widget.Select; routeStyle string; routeStyleSelect *widget.Select; lineThickness float32; nodePadding float32; detectedOSScale float64
}

func (e *editorApp) toggleCollapse(n *Node) {
	key := fmt.Sprintf("%d:%s", n.Level, n.Text)
	if e.collapsedNodes[key] {
		e.collapsedNodes[key] = false; for _, child := range n.Children { e.setCollapsedRecursive(child, true) }
	} else { e.setCollapsedRecursive(n, true) }
	e.updatePreview()
}
func (e *editorApp) setCollapsedRecursive(n *Node, collapsed bool) {
	if n == nil { return }; key := fmt.Sprintf("%d:%s", n.Level, n.Text); e.collapsedNodes[key] = collapsed; for _, child := range n.Children { e.setCollapsedRecursive(child, collapsed) }
}
func (e *editorApp) centerOnRoot() {
	if e.scroll == nil || e.rootNode == nil { return }
	vSize := e.scroll.Size(); if vSize.Width < 10 { winSize := e.window.Content().Size(); vSize = fyne.NewSize(winSize.Width*0.7, winSize.Height-80) }
	tx, ty := e.rootNode.X*e.zoomScale, e.rootNode.Y*e.zoomScale; e.scroll.Offset = fyne.NewPos(tx-vSize.Width/2, ty-vSize.Height/2); e.scroll.Refresh(); if e.miniMap != nil { e.miniMap.Refresh() }
}
func (e *editorApp) assignColors(n *Node) {
	if n == nil { return }; n.Color = theme.PrimaryColor()
	colors := []color.Color{color.NRGBA{255, 165, 0, 255}, color.NRGBA{50, 205, 50, 255}, color.NRGBA{147, 112, 219, 255}, color.NRGBA{30, 144, 255, 255}, color.NRGBA{255, 99, 71, 255}, color.NRGBA{0, 206, 209, 255}}
	for i, child := range n.Children { child.Color = colors[i%len(colors)]; e.propagateColor(child) }
}
func (e *editorApp) propagateColor(n *Node) { for _, child := range n.Children { child.Color = n.Color; e.propagateColor(child) } }
func (e *editorApp) setLayout(mode string) {
	switch mode { case "Mindmap (Balanced)": e.layoutMode = "balanced"; case "Mindmap (Fishbone)": e.layoutMode = "fishbone"; case "Logic (Left)": e.layoutMode = "left"; case "Logic (Right)": e.layoutMode = "right" }
	e.handleRefresh()
}
func (e *editorApp) updatePreview() {
	if e.renderArea == nil { return }; e.renderArea.Objects = nil; root := parseMermaidMindmap(e.entry.Text, e); e.rootNode = root
	if root != nil {
		e.assignColors(root)
		switch e.layoutMode { case "balanced": layoutBalanced(root, 2000, 2000); case "fishbone": layoutFishbone(root, 2000, 2000); case "left": layoutDirectional(root, 2000, 2000, -1); default: layoutDirectional(root, 2000, 2000, 1) }
		e.drawConnections(e.renderArea, root); e.drawNodes(e.renderArea, root); if e.miniMap != nil { e.miniMap.updateDots(root) }
	}
	e.canvasContainer.Refresh(); e.scroll.Refresh(); if e.miniMap != nil { e.miniMap.Refresh() }
}
func (e *editorApp) showSettings() {
	thick := widget.NewSlider(1, 10); thick.SetValue(float64(e.lineThickness))
	pad := widget.NewSlider(5, 50); pad.SetValue(float64(e.nodePadding))
	thick.OnChanged = func(v float64) { e.lineThickness = float32(v); e.updatePreview() }
	pad.OnChanged = func(v float64) { e.nodePadding = float32(v); e.updatePreview() }
	d := dialog.NewForm("Settings", "Close", "", []*widget.FormItem{{Text: "Line Thickness", Widget: thick}, {Text: "Node Padding", Widget: pad}}, func(bool) {}, e.window); d.Resize(fyne.NewSize(400, 200)); d.Show()
}
func (e *editorApp) drawConnections(c *fyne.Container, node *Node) {
	if node.Collapsed || len(node.Children) == 0 { return }
	for _, child := range node.Children {
		direction, col := float32(1.0), child.Color; if child.X < node.X { direction = -1.0 }; if col == nil { col = theme.PrimaryColor() }
		ps, pe, bgCol := fyne.NewPos((node.X+direction*node.WidthWithIcons()/2)*e.zoomScale, node.Y*e.zoomScale), fyne.NewPos((child.X-direction*child.WidthWithIcons()/2)*e.zoomScale, child.Y*e.zoomScale), color.NRGBA{R: 25, G: 25, B: 30, A: 255}
		th, halo := e.lineThickness*e.zoomScale, (e.lineThickness+3)*e.zoomScale
		if e.layoutMode == "fishbone" && node.Level == 0 {
			spineX := (child.X - direction*100) * e.zoomScale; jt := fyne.NewPos(spineX, node.Y*e.zoomScale)
			if child.Y < node.Y { pe = fyne.NewPos(child.X*e.zoomScale, (child.Y+child.Height/2)*e.zoomScale) } else { pe = fyne.NewPos(child.X*e.zoomScale, (child.Y-child.Height/2)*e.zoomScale) }
			l1h := canvas.NewLine(bgCol); l1h.Position1, l1h.Position2, l1h.StrokeWidth = ps, jt, halo; c.Add(l1h); l1 := canvas.NewLine(col); l1.Position1, l1.Position2, l1.StrokeWidth = ps, jt, th; c.Add(l1)
			if e.routeStyle == "Oval" { e.drawOvalRib(c, jt, pe, col) } else if e.routeStyle == "Orthogonal" { e.drawOrthogonalRib(c, jt, pe, col) } else { e.drawCurvedRib(c, jt, pe, col) }
		} else {
			if e.routeStyle == "Oval" { e.drawOvalSCurve(c, ps, pe, direction, col) } else if e.routeStyle == "Orthogonal" { e.drawOrthogonalSCurve(c, ps, pe, direction, col) } else { e.drawSCurve(c, ps, pe, direction, col) }
		}
		e.drawConnections(c, child)
	}
}
func (e *editorApp) drawOrthogonalRib(c *fyne.Container, start, end fyne.Position, col color.Color) {
	bgCol, mid := color.NRGBA{R: 25, G: 25, B: 30, A: 255}, fyne.NewPos(end.X, start.Y)
	th, halo := e.lineThickness*e.zoomScale, (e.lineThickness+3)*e.zoomScale
	l1h, l2h := canvas.NewLine(bgCol), canvas.NewLine(bgCol); l1h.Position1, l1h.Position2, l1h.StrokeWidth = start, mid, halo; l2h.Position1, l2h.Position2, l2h.StrokeWidth = mid, end, halo; c.Add(l1h); c.Add(l2h)
	l1, l2 := canvas.NewLine(col), canvas.NewLine(col); l1.Position1, l1.Position2, l1.StrokeWidth = start, mid, th; l2.Position1, l2.Position2, l2.StrokeWidth = mid, end, th; c.Add(l1); c.Add(l2)
}
func (e *editorApp) drawOrthogonalSCurve(c *fyne.Container, start, end fyne.Position, direction float32, col color.Color) {
	bgCol, jx := color.NRGBA{R: 25, G: 25, B: 30, A: 255}, start.X + direction*horizontalGap*0.5*e.zoomScale
	th, halo := e.lineThickness*e.zoomScale, (e.lineThickness+3)*e.zoomScale
	jt, pv := fyne.NewPos(jx, start.Y), fyne.NewPos(jx, end.Y)
	l1h, l2h, l3h := canvas.NewLine(bgCol), canvas.NewLine(bgCol), canvas.NewLine(bgCol); l1h.Position1, l1h.Position2, l1h.StrokeWidth = start, jt, halo; l2h.Position1, l2h.Position2, l2h.StrokeWidth = jt, pv, halo; l3h.Position1, l3h.Position2, l3h.StrokeWidth = pv, end, halo; c.Add(l1h); c.Add(l2h); c.Add(l3h)
	l1, l2, l3 := canvas.NewLine(col), canvas.NewLine(col), canvas.NewLine(col); l1.Position1, l1.Position2, l1.StrokeWidth = start, jt, th; l2.Position1, l2.Position2, l2.StrokeWidth = jt, pv, th; l3.Position1, l3.Position2, l3.StrokeWidth = pv, end, th; c.Add(l1); c.Add(l2); c.Add(l3)
}
func (e *editorApp) drawOvalRib(c *fyne.Container, start, end fyne.Position, col color.Color) {
	steps, px, py, dx, dy, bgCol := 20, start.X, start.Y, float64(end.X-start.X), float64(end.Y-start.Y), color.NRGBA{R: 25, G: 25, B: 30, A: 255}
	th, halo := e.lineThickness*e.zoomScale, (e.lineThickness+3)*e.zoomScale
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps); angle := t * math.Pi / 2.0; nx, ny := float32(float64(start.X)+dx*math.Sin(angle)), float32(float64(start.Y)+dy*(1.0-math.Cos(angle)))
		lh := canvas.NewLine(bgCol); lh.Position1, lh.Position2, lh.StrokeWidth = fyne.NewPos(px, py), fyne.NewPos(nx, ny), halo; c.Add(lh)
		l := canvas.NewLine(col); l.Position1, l.Position2, l.StrokeWidth = fyne.NewPos(px, py), fyne.NewPos(nx, ny), th; c.Add(l); px, py = nx, ny
	}
}
func (e *editorApp) drawOvalSCurve(c *fyne.Container, start, end fyne.Position, direction float32, col color.Color) {
	steps, px, py, midX, midY, bgCol := 20, start.X, start.Y, (start.X+end.X)/2, (start.Y+end.Y)/2, color.NRGBA{R: 25, G: 25, B: 30, A: 255}
	th, halo, dx1, dy1 := e.lineThickness*e.zoomScale, (e.lineThickness+3)*e.zoomScale, float64(midX-start.X), float64(midY-start.Y)
	for i := 1; i <= steps/2; i++ {
		t := float64(i) / (float64(steps) / 2.0); angle := t * math.Pi / 2.0; nx, ny := float32(float64(start.X)+dx1*math.Sin(angle)), float32(float64(start.Y)+dy1*(1.0-math.Cos(angle)))
		lh, l := canvas.NewLine(bgCol), canvas.NewLine(col); lh.Position1, lh.Position2, lh.StrokeWidth = fyne.NewPos(px, py), fyne.NewPos(nx, ny), halo; l.Position1, l.Position2, l.StrokeWidth = fyne.NewPos(px, py), fyne.NewPos(nx, ny), th; c.Add(lh); c.Add(l); px, py = nx, ny
	}
	dx2, dy2 := float64(end.X-midX), float64(end.Y-midY)
	for i := 1; i <= steps/2; i++ {
		t := float64(i) / (float64(steps) / 2.0); angle := t * math.Pi / 2.0; nx, ny := float32(float64(midX)+dx2*(1.0-math.Cos(angle))), float32(float64(midY)+dy2*math.Sin(angle))
		lh, l := canvas.NewLine(bgCol), canvas.NewLine(col); lh.Position1, lh.Position2, lh.StrokeWidth = fyne.NewPos(px, py), fyne.NewPos(nx, ny), halo; l.Position1, l.Position2, l.StrokeWidth = fyne.NewPos(px, py), fyne.NewPos(nx, ny), th; c.Add(lh); c.Add(l); px, py = nx, ny
	}
}
func (e *editorApp) drawCurvedRib(c *fyne.Container, start, end fyne.Position, col color.Color) {
	steps, px, py, cpx, cpy, bgCol := 20, start.X, start.Y, end.X, start.Y, color.NRGBA{R: 25, G: 25, B: 30, A: 255}
	th, halo := e.lineThickness*e.zoomScale, (e.lineThickness+3)*e.zoomScale
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps); it := 1.0 - t; nx, ny := float32(it*it*float64(start.X)+2*it*t*float64(cpx)+t*t*float64(end.X)), float32(it*it*float64(start.Y)+2*it*t*float64(cpy)+t*t*float64(end.Y))
		lh, l := canvas.NewLine(bgCol), canvas.NewLine(col); lh.Position1, lh.Position2, lh.StrokeWidth = fyne.NewPos(px, py), fyne.NewPos(nx, ny), halo; l.Position1, l.Position2, l.StrokeWidth = fyne.NewPos(px, py), fyne.NewPos(nx, ny), th; c.Add(lh); c.Add(l); px, py = nx, ny
	}
}
func (e *editorApp) drawSCurve(c *fyne.Container, start, end fyne.Position, direction float32, col color.Color) {
	steps, px, py, cp1x, cp2x, bgCol := 20, start.X, start.Y, start.X+direction*horizontalGap*0.5*e.zoomScale, end.X-direction*horizontalGap*0.5*e.zoomScale, color.NRGBA{R: 25, G: 25, B: 30, A: 255}
	th, halo := e.lineThickness*e.zoomScale, (e.lineThickness+3)*e.zoomScale
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps); it := 1.0 - t; nx, ny := float32(it*it*it*float64(start.X)+3*it*it*t*float64(cp1x)+3*it*t*t*float64(cp2x)+t*t*t*float64(end.X)), float32(it*it*it*float64(start.Y)+3*it*it*t*float64(start.Y)+3*it*t*t*float64(end.Y)+t*t*t*float64(end.Y))
		lh, l := canvas.NewLine(bgCol), canvas.NewLine(col); lh.Position1, lh.Position2, lh.StrokeWidth = fyne.NewPos(px, py), fyne.NewPos(nx, ny), halo; l.Position1, l.Position2, l.StrokeWidth = fyne.NewPos(px, py), fyne.NewPos(nx, ny), th; c.Add(lh); c.Add(l); px, py = nx, ny
	}
}
func (e *editorApp) drawNodes(c *fyne.Container, node *Node) {
	nw := newNodeWidget(node, e, e.zoomScale); nw.Resize(nw.MinSize()); nw.Move(fyne.NewPos((node.X-node.WidthWithIcons()/2)*e.zoomScale, (node.Y-node.Height/2)*e.zoomScale)); c.Add(nw)
	if !node.Collapsed { for _, child := range node.Children { e.drawNodes(c, child) } }
}
func (e *editorApp) editNodeDialog(n *Node) {
	en := widget.NewMultiLineEntry(); en.SetText(n.Text); d := dialog.NewForm("Edit Node", "Apply", "Cancel", []*widget.FormItem{{Text: "Text", Widget: en}}, func(c bool) { if c { e.updateMarkdownNode(n.LineIdx, en.Text, n.Shape) } }, e.window); d.Resize(fyne.NewSize(500, 300)); d.Show(); e.window.Canvas().Focus(en)
}
func (e *editorApp) addChildNode(parent *Node) {
	lines := strings.Split(e.entry.Text, "\n"); if parent.LineIdx < 0 || parent.LineIdx >= len(lines) { return }; indentStr := ""
	for _, r := range lines[parent.LineIdx] { if r == ' ' || r == '\t' { indentStr += string(r) } else { break } }
	childIndent, insertIdx := indentStr+"  ", parent.LineIdx+1
	for i := parent.LineIdx + 1; i < len(lines); i++ {
		line := lines[i]; if strings.TrimSpace(line) == "" { continue }; ind := 0; for _, r := range line { if r == ' ' { ind++ } else if r == '\t' { ind += 4 } else { break } }; if ind <= parent.Level { break }; insertIdx = i + 1
	}
	nl := append(lines[:insertIdx], append([]string{childIndent + "New Child"}, lines[insertIdx:]...)...); e.entry.SetText(strings.Join(nl, "\n"))
}
func (e *editorApp) addSiblingNode(n *Node, before bool) {
	lines := strings.Split(e.entry.Text, "\n"); if n.LineIdx < 0 || n.LineIdx >= len(lines) { return }; indentStr := ""
	for _, r := range lines[n.LineIdx] { if r == ' ' || r == '\t' { indentStr += string(r) } else { break } }; insertIdx := n.LineIdx
	if !before {
		insertIdx = n.LineIdx + 1; for i := n.LineIdx + 1; i < len(lines); i++ {
			line := lines[i]; if strings.TrimSpace(line) == "" { continue }; ind := 0; for _, r := range line { if r == ' ' { ind++ } else if r == '\t' { ind += 4 } else { break } }; if ind <= n.Level { break }; insertIdx = i + 1
		}
	}
	nl := append(lines[:insertIdx], append([]string{indentStr + "New Sibling"}, lines[insertIdx:]...)...); e.entry.SetText(strings.Join(nl, "\n"))
}
func (e *editorApp) removeNode(n *Node) {
	lines := strings.Split(e.entry.Text, "\n"); if n.LineIdx < 0 || n.LineIdx >= len(lines) { return }; endIdx := n.LineIdx + 1
	for i := n.LineIdx + 1; i < len(lines); i++ {
		line := lines[i]; if strings.TrimSpace(line) == "" { continue }; ind := 0; for _, r := range line { if r == ' ' { ind++ } else if r == '\t' { ind += 4 } else { break } }; if ind <= n.Level { break }; endIdx = i + 1
	}
	nl := append(lines[:n.LineIdx], lines[endIdx:]...); e.entry.SetText(strings.Join(nl, "\n"))
}
func (e *editorApp) updateMarkdownNode(idx int, nt string, shp string) {
	ls := strings.Split(e.entry.Text, "\n"); if idx < 0 || idx >= len(ls) { return }; ins := ""
	for _, r := range ls[idx] { if r == ' ' || r == '\t' { ins += string(r) } else { break } }
	cln := strings.ReplaceAll(nt, "\n", " "); fmtd := cln; switch shp { case "circle": fmtd = "((" + cln + "))"; case "rounded": fmtd = "(" + cln + ")"; case "square": fmtd = "[" + cln + "]" }
	ls[idx] = ins + fmtd; e.entry.SetText(strings.Join(ls, "\n"))
}
func (e *editorApp) zoomIn() { if e.zoomScale < 3.0 { e.zoomScale += 0.1; e.updatePreview() } }
func (e *editorApp) zoomOut() { if e.zoomScale > 0.3 { e.zoomScale -= 0.1; e.updatePreview() } }
func (e *editorApp) zoomReset() { e.zoomScale = 1.0; e.updatePreview() }
func (e *editorApp) handleRefresh() { e.updatePreview(); e.centerOnRoot() }
func (e *editorApp) showScalingInfo() {
	s := fmt.Sprintf("Cross-Platform Scaling - OS: %s\nDetected OS Scale: %.2f\nApplied Zoom Scale: %.2f\n\nFyne Canvas Raw Scale: %f\nFYNE_SCALE Env: %s", 
		runtime.GOOS, e.detectedOSScale, e.zoomScale, e.window.Canvas().Scale(), os.Getenv("FYNE_SCALE"))
	dialog.ShowInformation("UI Scaling Info", s, e.window)
}
func (e *editorApp) loadInitialContent() {
	e.entry.SetText("mindmap\n  root((UTF-8 Export Engine))\n    Chinese: 中文字符测试\n    Japanese: 日本語テスト\n    Korean: 한국어 테스트"); e.currentFile = nil; e.window.SetTitle("Mermaid Mindmap Editor"); e.updatePreview()
}
func getMindmapBounds(n *Node, minX, minY, maxX, maxY *float32) {
	if n == nil { return }; x1, y1, x2, y2 := n.X - n.WidthWithIcons()/2, n.Y - n.Height/2, n.X + n.WidthWithIcons()/2, n.Y + n.Height/2
	if x1 < *minX { *minX = x1 }; if y1 < *minY { *minY = y1 }; if x2 > *maxX { *maxX = x2 }; if y2 > *maxY { *maxY = y2 }
	if !n.Collapsed { for _, c := range n.Children { getMindmapBounds(c, minX, minY, maxX, maxY) } }
}
func colorToRGB(c color.Color) (int, int, int) { r, g, b, _ := c.RGBA(); return int(r >> 8), int(g >> 8), int(b >> 8) }
func getFallbackFontData() []byte {
	paths := []string{"/usr/share/fonts/truetype/droid/DroidSansFallbackFull.ttf", "/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc", "C:\\Windows\\Fonts\\msyh.ttc"}
	for _, p := range paths { if data, err := os.ReadFile(p); err == nil { return data } }
	return theme.DefaultTextFont().Content()
}
func getOpentypeFonts() (*opentype.Font, *opentype.Font) {
	fP, _ := opentype.Parse(theme.DefaultTextFont().Content()); dataF, fF := getFallbackFontData(), (*opentype.Font)(nil)
	if coll, err := opentype.ParseCollection(dataF); err == nil { fF, _ = coll.Font(0) } else { fF, _ = opentype.Parse(dataF) }
	if fF == nil { fF = fP }
	return fP, fF
}
func drawCompositeString(img draw.Image, txt string, x, y float64, fP, fF font.Face, col color.Color) {
	dP, dF := &font.Drawer{Dst: img, Src: image.NewUniform(col), Face: fP}, &font.Drawer{Dst: img, Src: image.NewUniform(col), Face: fF}
	dP.Dot = fixed.Point26_6{X: fixed.Int26_6(x * 64), Y: fixed.Int26_6(y * 64)}; dF.Dot = dP.Dot
	for _, r := range txt {
		if (r >= 0x2E80 && r <= 0x9FFF) || (r >= 0x3040 && r <= 0x30FF) || (r >= 0xAC00 && r <= 0xD7AF) || (r >= 0xFF00 && r <= 0xFFEF) { dF.DrawString(string(r)); dP.Dot = dF.Dot } else { dP.DrawString(string(r)); dF.Dot = dP.Dot }
	}
}
func measureCompositeString(txt string, fP, fF font.Face) float64 {
	dP, dF := &font.Drawer{Face: fP}, &font.Drawer{Face: fF}; var tW fixed.Int26_6
	for _, r := range txt {
		if (r >= 0x2E80 && r <= 0x9FFF) || (r >= 0x3040 && r <= 0x30FF) || (r >= 0xAC00 && r <= 0xD7AF) || (r >= 0xFF00 && r <= 0xFFEF) { tW += dF.MeasureString(string(r)) } else { tW += dP.MeasureString(string(r)) }
	}
	return float64(tW) / 64.0
}
func drawLine(img draw.Image, x1, y1, x2, y2, thickness int, col color.Color) {
	dx, dy := x2-x1, y2-y1; if dx < 0 { dx = -dx }; if dy < 0 { dy = -dy }; sx, sy := 1, 1; if x1 >= x2 { sx = -1 }; if y1 >= y2 { sy = -1 }; err := dx - dy
	for {
		for tx := -thickness/2; tx <= thickness/2; tx++ { for ty := -thickness/2; ty <= thickness/2; ty++ { img.Set(x1+tx, y1+ty, col) } }
		if x1 == x2 && y1 == y2 { break }; e2 := 2 * err; if e2 > -dy { err -= dy; x1 += sx }; if e2 < dx { err += dx; y1 += sy }
	}
}
func drawBezier(img draw.Image, x1, y1, x2, y2, cp1x, cp1y, cp2x, cp2y float64, thickness int, col color.Color) {
	p := func(t float64) (float64, float64) { it := 1.0 - t; return it*it*it*x1 + 3*it*it*t*cp1x + 3*it*t*t*cp2x + t*t*t*x2, it*it*it*y1 + 3*it*it*t*cp1y + 3*it*t*t*cp2y + t*t*t*y2 }
	px, py := x1, y1; for i := 1; i <= 50; i++ { t := float64(i) / 50.0; nx, ny := p(t); drawLine(img, int(px), int(py), int(nx), int(ny), thickness, col); px, py = nx, ny }
}
func drawOval(img draw.Image, x1, y1, x2, y2 float64, thickness int, col color.Color) {
	dx, dy, px, py := x2-x1, y2-y1, x1, y1; for i := 1; i <= 50; i++ { t := float64(i) / 50.0; a := t * math.Pi / 2.0; nx, ny := x1+dx*math.Sin(a), y1+dy*(1.0-math.Cos(a)); drawLine(img, int(px), int(py), int(nx), int(ny), thickness, col); px, py = nx, ny }
}
func (e *editorApp) exportPNG() {
	d := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) {
		if err != nil || w == nil { return }; go func() {
			defer w.Close(); var minX, minY, maxX, maxY float32 = 4000, 4000, 0, 0; getMindmapBounds(e.rootNode, &minX, &minY, &maxX, &maxY)
			z, margin := float64(e.zoomScale), 50.0; if minX > maxX { return }; outW, outH := int((float64(maxX-minX)+margin*2)*z), int((float64(maxY-minY)+margin*2)*z)
			img, bgCol := image.NewRGBA(image.Rect(0, 0, outW, outH)), color.White; draw.Draw(img, img.Bounds(), &image.Uniform{bgCol}, image.Point{}, draw.Src)
			fP, fF := getOpentypeFonts(); fPR, _ := opentype.NewFace(fP, &opentype.FaceOptions{Size: 13 * z, DPI: 72}); fFR, _ := opentype.NewFace(fF, &opentype.FaceOptions{Size: 13 * z, DPI: 72}); fPB, _ := opentype.NewFace(fP, &opentype.FaceOptions{Size: 15 * z, DPI: 72}); fFB, _ := opentype.NewFace(fF, &opentype.FaceOptions{Size: 15 * z, DPI: 72}); fPI, _ := opentype.NewFace(fP, &opentype.FaceOptions{Size: 16 * z, DPI: 72}); fFI, _ := opentype.NewFace(fF, &opentype.FaceOptions{Size: 16 * z, DPI: 72})
			var drawCons func(n *Node); drawCons = func(n *Node) {
				if n == nil || n.Collapsed { return }; for _, c := range n.Children {
					dir := 1.0; if c.X < n.X { dir = -1.0 }; psX, psY := (float64(n.X+float32(dir)*n.WidthWithIcons()/2)-float64(minX)+margin)*z, (float64(n.Y)-float64(minY)+margin)*z; peX, peY := (float64(c.X-float32(dir)*c.WidthWithIcons()/2)-float64(minX)+margin)*z, (float64(c.Y)-float64(minY)+margin)*z; rh := int(float64(e.lineThickness) * z); if rh < 1 { rh = 1 }; halo := rh + int(3*z)
					if e.layoutMode == "fishbone" && n.Level == 0 {
						sX := (float64(c.X-float32(dir)*100)-float64(minX)+margin)*z; peX = (float64(c.X)-float64(minX)+margin)*z; if c.Y < n.Y { peY = (float64(c.Y+c.Height/2)-float64(minY)+margin)*z } else { peY = (float64(c.Y-c.Height/2)-float64(minY)+margin)*z }
						rx, ry, sx := int(psX), int(psY), int(sX); sRH, sR := image.Rect(rx, ry-halo/2, sx, ry+halo/2), image.Rect(rx, ry-rh/2, sx, ry+rh/2); if dir < 0 { sRH, sR = image.Rect(sx, ry-halo/2, rx, ry+halo/2), image.Rect(sx, ry-rh/2, rx, ry+halo/2) }
						draw.Draw(img, sRH, &image.Uniform{bgCol}, image.Point{}, draw.Src); draw.Draw(img, sR, &image.Uniform{c.Color}, image.Point{}, draw.Src)
						if e.routeStyle == "Oval" { drawOval(img, sX, psY, peX, peY, halo, bgCol); drawOval(img, sX, psY, peX, peY, rh, c.Color) } else if e.routeStyle == "Orthogonal" { drawLine(img, int(sX), int(psY), int(peX), int(psY), halo, bgCol); drawLine(img, int(peX), int(psY), int(peX), int(peY), halo, bgCol); drawLine(img, int(sX), int(psY), int(peX), int(psY), rh, c.Color); drawLine(img, int(peX), int(psY), int(peX), int(peY), rh, c.Color) } else { drawBezier(img, sX, psY, peX, peY, peX, psY, peX, psY, halo, bgCol); drawBezier(img, sX, psY, peX, peY, peX, psY, peX, psY, rh, c.Color) }
					} else {
						if e.routeStyle == "Oval" {
							mX, mY := (psX+peX)/2, (psY+peY)/2; drawOval(img, psX, psY, mX, mY, halo, bgCol); drawOval(img, psX, psY, mX, mY, rh, c.Color); dx2, dy2, px2, py2 := peX-mX, peY-mY, mX, mY
							for i := 1; i <= 25; i++ { t := float64(i) / 25.0; a := t * math.Pi / 2.0; nx, ny := mX+dx2*(1.0-math.Cos(a)), mY+dy2*math.Sin(a); drawLine(img, int(px2), int(py2), int(nx), int(ny), halo, bgCol); drawLine(img, int(px2), int(py2), int(nx), int(ny), rh, c.Color); px2, py2 = nx, ny }
						} else if e.routeStyle == "Orthogonal" {
							jx := psX + dir*float64(horizontalGap)*0.5*z; drawLine(img, int(psX), int(psY), int(jx), int(psY), halo, bgCol); drawLine(img, int(jx), int(psY), int(jx), int(peY), halo, bgCol); drawLine(img, int(jx), int(peY), int(peX), int(peY), halo, bgCol); drawLine(img, int(psX), int(psY), int(jx), int(psY), rh, c.Color); drawLine(img, int(jx), int(psY), int(jx), int(peY), rh, c.Color); drawLine(img, int(jx), int(peY), int(peX), int(peY), rh, c.Color)
						} else { cp1x, cp2x := psX+dir*float64(horizontalGap)*0.5*z, peX-dir*float64(horizontalGap)*0.5*z; drawBezier(img, psX, psY, peX, peY, cp1x, psY, cp2x, peY, halo, bgCol); drawBezier(img, psX, psY, peX, peY, cp1x, psY, cp2x, peY, rh, c.Color) }
					}
					drawCons(c)
				}
			}; var drawNode func(n *Node); drawNode = func(n *Node) {
				if n == nil { return }; bX, bY, bW, bH := (float64(n.X-n.WidthWithIcons()/2-minX)+margin)*z, (float64(n.Y-n.Height/2-minY)+margin*z), float64(n.WidthWithIcons())*z, float64(n.Height)*z
				rect := image.Rect(int(bX), int(bY), int(bX+bW), int(bY+bH)); draw.Draw(img, rect, &image.Uniform{color.White}, image.Point{}, draw.Src)
				bw := int(float64(e.lineThickness) * z); if bw < 1 { bw = 1 }; nC := n.Color; if nC == nil { nC = theme.PrimaryColor() }
				draw.Draw(img, image.Rect(rect.Min.X, rect.Min.Y, rect.Max.X, rect.Min.Y+bw), &image.Uniform{nC}, image.Point{}, draw.Src); draw.Draw(img, image.Rect(rect.Min.X, rect.Max.Y-bw, rect.Max.X, rect.Max.Y), &image.Uniform{nC}, image.Point{}, draw.Src); draw.Draw(img, image.Rect(rect.Min.X, rect.Min.Y, rect.Min.X+bw, rect.Max.Y), &image.Uniform{nC}, image.Point{}, draw.Src); draw.Draw(img, image.Rect(rect.Max.X-bw, rect.Min.Y, rect.Max.X, rect.Max.Y), &image.Uniform{nC}, image.Point{}, draw.Src)
				lH, sY, fPF, fFF := 20.0*z, bY+(bH-float64(len(n.Lines))*20.0*z)/2+20.0*z*0.75, fPR, fFR; if n.Level == 0 { fPF, fFF = fPB, fFB }
				for i, txt := range n.Lines { tw := measureCompositeString(txt, fPF, fFF); drawCompositeString(img, txt, bX+float64(n.Width)*z/2-tw/2, sY+float64(i)*lH, fPF, fFF, color.Black) }
				if len(n.Children) > 0 { iC, iCol := "-", color.NRGBA{R: 200, G: 50, B: 50, A: 255}; if n.Collapsed { iC, iCol = "+", color.NRGBA{R: 50, G: 200, B: 50, A: 255} }; tw := measureCompositeString(iC, fPI, fFI); drawCompositeString(img, iC, bX+bW-30.0*z*0.7-tw/2, bY+bH/2+lH*0.25, fPI, fFI, iCol) }
				if !n.Collapsed { for _, c := range n.Children { drawNode(c) } }
			}
			drawCons(e.rootNode); drawNode(e.rootNode); png.Encode(w, img)
		}()
	}, e.window); d.SetFileName("mindmap.png"); d.Show()
}
func (e *editorApp) openFile() {
	d := dialog.NewFileOpen(func(r fyne.URIReadCloser, err error) { if err != nil || r == nil { return }; data, _ := io.ReadAll(r); e.entry.SetText(string(data)); e.currentFile = r.URI(); e.window.SetTitle("Mermaid Mindmap Editor - " + e.currentFile.Name()); e.handleRefresh(); r.Close() }, e.window); d.Show()
}
func (e *editorApp) saveFile() { if e.currentFile == nil { e.saveAsFile(); return }; w, _ := storage.Writer(e.currentFile); w.Write([]byte(e.entry.Text)); w.Close() }
func (e *editorApp) saveAsFile() {
	d := dialog.NewFileSave(func(w fyne.URIWriteCloser, err error) { if err != nil || w == nil { return }; w.Write([]byte(e.entry.Text)); e.currentFile = w.URI(); e.window.SetTitle("Mermaid Mindmap Editor - " + e.currentFile.Name()); w.Close() }, e.window); d.Show()
}
type tooltipButton struct { widget.Button; tip string; app *editorApp }
func (t *tooltipButton) Tooltip() string { return t.tip }
func (t *tooltipButton) MouseIn(e *desktop.MouseEvent) { if t.app.statusLabel != nil { t.app.statusLabel.SetText(t.tip) } }
func (t *tooltipButton) MouseOut() { if t.app.statusLabel != nil { t.app.statusLabel.SetText("Ready") } }
func newToolBtn(i fyne.Resource, tip string, e *editorApp, tap func()) *tooltipButton { b := &tooltipButton{tip: tip, app: e}; b.Icon, b.OnTapped, b.Importance = i, tap, widget.LowImportance; b.ExtendBaseWidget(b); return b }
type miniMapLayout struct{}
func (l *miniMapLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) { if len(objects) == 0 { return }; mW, mH := size.Width*0.10, size.Height*0.10; if mW < 100 { mW = 100 }; if mH < 100 { mH = 100 }; objects[0].Resize(fyne.NewSize(mW, mH)); objects[0].Move(fyne.NewPos(size.Width-mW-10, size.Height-mH-10)) }
func (l *miniMapLayout) MinSize(_ []fyne.CanvasObject) fyne.Size { return fyne.NewSize(100, 100) }
func main() {
	if os.Getenv("GOMAXPROCS") == "" {
		n := runtime.NumCPU() - 2
		if n < 1 { n = 1 }
		runtime.GOMAXPROCS(n)
	}
	logMsg(fmt.Sprintf("GOMAXPROCS: %d", runtime.GOMAXPROCS(0)))

	logMsg("Initializing GUI application..."); a := app.NewWithID("com.mermaid.md.gui"); w := a.NewWindow("Mermaid Mindmap Editor")
	e := &editorApp{mainApp: a, window: w, zoomScale: 1.0, collapsedNodes: make(map[string]bool), layoutMode: "balanced", lineThickness: 2, nodePadding: 10}
	e.entry, e.renderArea, e.interactiveBG, e.scroll = widget.NewMultiLineEntry(), container.NewWithoutLayout(), newInteractiveBackground(e), container.NewScroll(nil); e.canvasContainer = container.New(&canvasLayout{app: e}, e.interactiveBG, e.renderArea); e.scroll.Content = e.canvasContainer; e.statusLabel, e.miniMap = widget.NewLabel("Ready"), newMiniMap(e); e.miniContainer = container.New(&miniMapLayout{}, e.miniMap)
	e.layoutSelect = widget.NewSelect([]string{"Mindmap (Balanced)", "Mindmap (Fishbone)", "Logic (Left)", "Logic (Right)"}, func(s string) { e.setLayout(s) }); e.layoutSelect.SetSelected("Mindmap (Balanced)")
	e.routeStyle = "Bezier"; e.routeStyleSelect = widget.NewSelect([]string{"Bezier", "Oval", "Orthogonal"}, func(s string) { e.routeStyle = s; e.updatePreview() }); e.routeStyleSelect.SetSelected("Bezier")
	w.SetMainMenu(fyne.NewMainMenu(fyne.NewMenu("File", fyne.NewMenuItem("New", e.loadInitialContent), fyne.NewMenuItem("Open", e.openFile), fyne.NewMenuItem("Save", e.saveFile), fyne.NewMenuItem("Save As...", e.saveAsFile), fyne.NewMenuItemSeparator(), fyne.NewMenuItem("Export as PNG", e.exportPNG), fyne.NewMenuItemSeparator(), fyne.NewMenuItem("UI Scaling Info", e.showScalingInfo), fyne.NewMenuItemSeparator(), fyne.NewMenuItem("Exit", func() { os.Exit(0) }))))
	e.toolbar = container.NewHBox(newToolBtn(theme.DocumentCreateIcon(), "New", e, e.loadInitialContent), newToolBtn(theme.FolderOpenIcon(), "Open", e, e.openFile), newToolBtn(theme.DocumentSaveIcon(), "Save", e, e.showSettings), widget.NewSeparator(), container.NewHBox(widget.NewLabel("Layout:"), e.layoutSelect), widget.NewSeparator(), container.NewHBox(widget.NewLabel("Route:"), e.routeStyleSelect), widget.NewSeparator(), newToolBtn(theme.SettingsIcon(), "Settings", e, e.showSettings), widget.NewSeparator(), newToolBtn(theme.ZoomInIcon(), "Zoom In", e, e.zoomIn), newToolBtn(theme.ZoomOutIcon(), "Zoom Out", e, e.zoomOut), newToolBtn(theme.ViewRefreshIcon(), "Refresh & Center", e, e.handleRefresh))
	split := container.NewHSplit(e.entry, container.NewStack(e.scroll, newCenteringTrigger(e), e.miniContainer)); split.Offset = 0.3; w.SetContent(container.NewBorder(e.toolbar, e.statusLabel, nil, nil, split)); w.Resize(fyne.NewSize(1200, 800)); e.loadInitialContent(); e.entry.OnChanged = func(s string) { e.updatePreview() }
	
	w.Show()
	
	// Defer scaling adjustment to ensure window is mapped and scale is detected
	go func() {
		time.Sleep(1000 * time.Millisecond)
		if os.Getenv("FYNE_SCALE") == "" {
			var osScale float64 = 1.0
			
			if runtime.GOOS == "linux" {
				// In WSLg/Linux, OS scale is often reported incorrectly.
				// Use xrandr to calculate real scale relative to 1920x1080 base.
				out, err := exec.Command("xrandr", "--query").Output()
				if err == nil {
					outputStr := string(out)
					var curW, curH float64 = 1920, 1080
					if idx := strings.Index(outputStr, "current "); idx != -1 {
						line := outputStr[idx:]
						fmt.Sscanf(line, "current %f x %f", &curW, &curH)
					}
					if curW > 0 { osScale = 1920.0 / curW }
				} else {
					// Fallback for Linux without xrandr (Wayland/ChromeOS)
					osScale = float64(w.Canvas().Scale())
				}
			} else {
				// On Windows/macOS/Mobile, Fyne's built-in scale detection is reliable.
				osScale = float64(w.Canvas().Scale())
			}
			
			// Apply user's preferred formula: 2.0 * OS_SCALE - 1.5
			calculatedScale := 2.0*osScale - 1.5
			if calculatedScale < 0.1 { calculatedScale = 0.1 }
			
			e.detectedOSScale = osScale
			logMsg(fmt.Sprintf("Cross-Platform Scaling - OS: %s, Detected OS Scale: %.2f, Applying zoom: %.2f", runtime.GOOS, osScale, calculatedScale))
			
			// Fyne UI updates MUST happen on the main thread
			fyne.Do(func() {
				e.zoomScale = float32(calculatedScale)
				if e.window != nil && e.window.Content() != nil {
					e.window.Content().Refresh()
					e.handleRefresh()
				}
			})
		}
	}()

	e.isReady = true; logMsg("Starting GUI application..."); e.handleRefresh(); w.SetMaster(); w.SetOnClosed(func() { os.Exit(0) }); a.Run()
}
