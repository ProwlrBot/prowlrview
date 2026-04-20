package ui

import (
	"fmt"
	"sort"

	"github.com/ProwlrBot/prowlrview/internal/graph"
	"github.com/ProwlrBot/prowlrview/internal/theme"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type graphView struct {
	*tview.Box
	g     *graph.Graph
	theme graphTheme
}

type graphTheme struct {
	critical tcell.Color
	high     tcell.Color
	medium   tcell.Color
	low      tcell.Color
	info     tcell.Color
	edge     tcell.Color
	label    tcell.Color
	bg       tcell.Color
}

func newGraphView(g *graph.Graph, th *theme.Theme) *graphView {
	gv := &graphView{
		Box: tview.NewBox(),
		g:   g,
		theme: graphTheme{
			critical: th.SevCritical,
			high:     th.SevHigh,
			medium:   th.SevMedium,
			low:      th.SevLow,
			info:     th.SevInfo,
			edge:     tcell.NewRGBColor(80, 80, 80),
			label:    tcell.NewRGBColor(220, 220, 220),
			bg:       th.Background,
		},
	}
	gv.SetBorder(true).SetTitle(" ⬡ graph ").SetBorderColor(th.Border).SetTitleColor(th.Title)
	gv.SetDrawFunc(gv.draw)
	return gv
}

func (gv *graphView) draw(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
	nodes := gv.g.Nodes()
	if len(nodes) == 0 {
		tview.Print(screen, "[gray]no nodes[-]", x+2, y+height/2, width-4, tview.AlignLeft, tcell.ColorGray)
		return x, y, width, height
	}

	// build ID→node map and depth map
	nodeMap := make(map[string]*graph.Node, len(nodes))
	for _, n := range nodes {
		nodeMap[n.ID] = n
	}
	depth := make(map[string]int)

	// BFS from roots
	queue := []*graph.Node{}
	for _, n := range nodes {
		if n.Parent == "" || nodeMap[n.Parent] == nil {
			depth[n.ID] = 0
			queue = append(queue, n)
		}
	}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, n := range nodes {
			if n.Parent == cur.ID {
				if _, seen := depth[n.ID]; !seen {
					depth[n.ID] = depth[cur.ID] + 1
					queue = append(queue, n)
				}
			}
		}
	}

	// group by depth
	maxDepth := 0
	byDepth := make(map[int][]*graph.Node)
	for _, n := range nodes {
		d := depth[n.ID]
		byDepth[d] = append(byDepth[d], n)
		if d > maxDepth {
			maxDepth = d
		}
	}

	// sort within each column by severity desc
	for d := range byDepth {
		sort.Slice(byDepth[d], func(i, j int) bool {
			return byDepth[d][i].Severity > byDepth[d][j].Severity
		})
	}

	// column positions: spread evenly across width
	colWidth := (width - 4) / (maxDepth + 1)
	if colWidth < 12 {
		colWidth = 12
	}

	// node screen positions
	type pos struct{ x, y int }
	nodePos := make(map[string]pos)

	innerHeight := height - 2
	for d := 0; d <= maxDepth; d++ {
		col := byDepth[d]
		if len(col) == 0 {
			continue
		}
		colX := x + 2 + d*colWidth
		for i, n := range col {
			var rowY int
			if len(col) == 1 {
				rowY = y + innerHeight/2
			} else {
				rowY = y + 1 + (i*(innerHeight-1))/(len(col))
			}
			nodePos[n.ID] = pos{colX, rowY}
		}
	}

	// draw edges first (behind nodes)
	edgeStyle := tcell.StyleDefault.Foreground(gv.theme.edge)
	for _, n := range nodes {
		if n.Parent == "" {
			continue
		}
		from, fok := nodePos[n.Parent]
		to, tok := nodePos[n.ID]
		if !fok || !tok {
			continue
		}
		// horizontal line from parent right edge to child left edge
		labelLen := len([]rune(truncate(n.Parent, 14))) + 4
		startX := from.x + labelLen
		endX := to.x - 1
		midY := from.y
		for cx := startX; cx <= endX && cx < x+width-1; cx++ {
			if cx >= x {
				screen.SetContent(cx, midY, '─', nil, edgeStyle)
			}
		}
		// vertical segment if Y differs
		if to.y != from.y {
			for cy := gvMin(from.y, to.y); cy <= gvMax(from.y, to.y); cy++ {
				if endX >= x && endX < x+width {
					screen.SetContent(endX, cy, '│', nil, edgeStyle)
				}
			}
			// corner at destination row
			corner := '╰'
			if to.y < from.y {
				corner = '╭'
			}
			if endX >= x && endX < x+width {
				screen.SetContent(endX, to.y, corner, nil, edgeStyle)
			}
		}
	}

	// draw nodes
	for _, n := range nodes {
		p, ok := nodePos[n.ID]
		if !ok {
			continue
		}
		label := truncate(n.ID, 14)
		box := fmt.Sprintf("[%s]", label)
		color := gv.sevColor(n.Severity)
		style := tcell.StyleDefault.Foreground(color)
		for i, ch := range box {
			cx := p.x + i
			if cx >= x+width-1 {
				break
			}
			screen.SetContent(cx, p.y, ch, nil, style)
		}
	}

	return x, y, width, height
}

func (gv *graphView) sevColor(s graph.Severity) tcell.Color {
	switch s {
	case graph.SevCritical:
		return gv.theme.critical
	case graph.SevHigh:
		return gv.theme.high
	case graph.SevMedium:
		return gv.theme.medium
	case graph.SevLow:
		return gv.theme.low
	default:
		return gv.theme.info
	}
}

func gvMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func gvMax(a, b int) int {
	if a > b {
		return a
	}
	return b
}
