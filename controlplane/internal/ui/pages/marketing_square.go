package pages

import "strconv"

// The square wall is hero A (/a), next to the long containers of hero B
// (/b, marketing_harbor.go) until one wins: a still wall of square
// containers across the whole width, stepped at the top like a skyline,
// low on the left, rising to the right. Every container is an app. Units are SVG user units; the
// wall is anchored at the bottom and cut at the sides (slice), so a phone
// shows the middle columns, big.
const (
	squareW    = 2400
	squareH    = 400
	squareStep = 100 // a column, a tier
	squareBox  = 96  // a container, the rest is the gap
)

// squareHeights is how many containers stand in each column.
var squareHeights = []int{1, 1, 2, 1, 2, 2, 1, 2, 2, 3, 2, 3, 3, 2, 3, 3, 4, 3, 4, 4, 3, 4, 4, 4}

// squareLabels are the apps stencilled on containers, by column and tier
// from the ground up. Each labelled container is pulled out, so no other
// container covers its name. They sit in the middle, where every screen
// shows them. your-app is drawn in ink.
var squareLabels = map[[2]int]string{
	{7, 1}:  "ruby",
	{9, 1}:  "postgres",
	{11, 0}: "node",
	{12, 2}: "your-app",
	{13, 1}: "python",
	{15, 2}: "golang",
	{17, 1}: "n8n",
}

// squareCrate is one container in the wall.
type squareCrate struct {
	X, Y  int
	Ref   string // empty when nothing is stencilled on it
	Tone  string // a, b or c: three grays
	Ink   bool   // your-app, in the foreground color
	Depth int    // how far it is pulled out of the wall, 0 when flush
}

func squareX(col int) int  { return col*squareStep + (squareStep-squareBox)/2 }
func squareY(tier int) int { return squareH - (tier+1)*squareStep + (squareStep - squareBox) }

// squareFront are the unlabelled containers pulled out of the wall, by
// column and tier, with their depth step (1 to 3). Every labelled container
// is pulled out too, by squareLabelDepth.
var squareFront = map[[2]int]int{
	{1, 0}: 1, {4, 1}: 2, {18, 3}: 1, {20, 1}: 3, {23, 2}: 2,
}

// squareLabelDepth is the depth step of each labelled container.
var squareLabelDepth = map[string]int{
	"ruby": 1, "postgres": 3, "node": 2, "your-app": 3, "python": 1, "golang": 2, "n8n": 3,
}

// The three steps a container can be pulled out by, and the shadow it
// throws per unit of depth. Light comes from the top right: the front face
// moves up and right by the depth, the left and bottom sides show in shade,
// and the shadow falls from where it leaves the wall down to the left,
// longer the further it stands out.
var squareDepths = []int{0, 3, 6, 9}

const squareShadowPerDepth = 8

// squareBoxes is the wall, flush containers first and then the pulled out
// ones from the shallowest to the deepest, the order they are drawn in.
var squareBoxes = func() []squareCrate {
	tones := []string{"a", "b", "c", "b", "a", "c", "b"}
	var flush []squareCrate
	front := make([][]squareCrate, len(squareDepths))
	for col, h := range squareHeights {
		for k := 0; k < h; k++ {
			at := [2]int{col, k}
			ref := squareLabels[at]
			step := squareFront[at]
			if ref != "" {
				step = squareLabelDepth[ref]
			}
			c := squareCrate{
				X:     squareX(col),
				Y:     squareY(k),
				Ref:   ref,
				Tone:  tones[(col*5+k*3)%len(tones)],
				Ink:   ref == "your-app",
				Depth: squareDepths[step],
			}
			if step == 0 {
				flush = append(flush, c)
			} else {
				front[step] = append(front[step], c)
			}
		}
	}
	for _, f := range front {
		flush = append(flush, f...)
	}
	return flush
}()

func squarePoints(xy ...int) string {
	s := ""
	for i := 0; i+1 < len(xy); i += 2 {
		s += squareN(xy[i]) + "," + squareN(xy[i+1]) + " "
	}
	return s
}

// squareShadow is the shadow a pulled out container throws on the wall:
// the square where it leaves the wall, swept down and to the left.
func squareShadow(x, y, depth int) string {
	w, l := squareBox, depth*squareShadowPerDepth
	return squarePoints(x, y, x+w, y, x+w, y+w, x+w-l, y+w+l, x-l, y+w+l, x-l, y+l)
}

// squareSideLeft and squareSideBottom are the two sides of a pulled out
// container that show, between the wall and its front face.
func squareSideLeft(x, y, d int) string {
	w := squareBox
	return squarePoints(x, y, x+d, y-d, x+d, y-d+w, x, y+w)
}

func squareSideBottom(x, y, d int) string {
	w := squareBox
	return squarePoints(x, y+w, x+d, y+w-d, x+d+w, y+w-d, x+w, y+w)
}

// squareRibs are the x positions of the corrugation ribs of a container.
func squareRibs(x int) []int {
	var ribs []int
	for r := x + 8; r < x+squareBox-6; r += 8 {
		ribs = append(ribs, r)
	}
	return ribs
}

func squareTone(tone string) string {
	if tone == "a" {
		return "fill-card"
	}
	return "fill-muted"
}

func squareN(n int) string { return strconv.Itoa(n) }
