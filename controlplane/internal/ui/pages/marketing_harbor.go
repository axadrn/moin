package pages

import "strconv"

// The harbor of the hero is a still wall of shipping containers across the
// whole width, stepped at the top like a skyline: low on the left, rising
// to the right. Every container is an app. Units are SVG user units; the
// wall is anchored at the bottom and cut at the sides (slice), so a phone
// shows the middle columns, big.
const (
	harborW    = 2400
	harborH    = 400
	harborStep = 100 // a column, a tier
	harborBox  = 96  // a container, the rest is the gap
)

// harborHeights is how many containers stand in each column.
var harborHeights = []int{1, 1, 2, 1, 2, 2, 1, 2, 2, 3, 2, 3, 3, 2, 3, 3, 4, 3, 4, 4, 3, 4, 4, 4}

// harborLabels are the apps stencilled on containers, by column and tier
// from the ground up. Each labelled container is pulled out, so no other
// container covers its name. They sit in the middle, where every screen
// shows them. your-app is drawn in ink.
var harborLabels = map[[2]int]string{
	{7, 1}:  "ruby",
	{9, 1}:  "postgres",
	{11, 0}: "node",
	{12, 2}: "your-app",
	{13, 1}: "python",
	{15, 2}: "golang",
	{17, 1}: "n8n",
}

// harborCrate is one container in the wall.
type harborCrate struct {
	X, Y  int
	Ref   string // empty when nothing is stencilled on it
	Tone  string // a, b or c: three grays
	Ink   bool   // your-app, in the foreground color
	Depth int    // how far it is pulled out of the wall, 0 when flush
}

func harborX(col int) int  { return col*harborStep + (harborStep-harborBox)/2 }
func harborY(tier int) int { return harborH - (tier+1)*harborStep + (harborStep - harborBox) }

// harborFront are the unlabelled containers pulled out of the wall, by
// column and tier, with their depth step (1 to 3). Every labelled container
// is pulled out too, by harborLabelDepth.
var harborFront = map[[2]int]int{
	{1, 0}: 1, {4, 1}: 2, {18, 3}: 1, {20, 1}: 3, {23, 2}: 2,
}

// harborLabelDepth is the depth step of each labelled container.
var harborLabelDepth = map[string]int{
	"ruby": 1, "postgres": 3, "node": 2, "your-app": 3, "python": 1, "golang": 2, "n8n": 3,
}

// The three steps a container can be pulled out by, and the shadow it
// throws per unit of depth. Light comes from the top right: the front face
// moves up and right by the depth, the left and bottom sides show in shade,
// and the shadow falls from where it leaves the wall down to the left,
// longer the further it stands out.
var harborDepths = []int{0, 3, 6, 9}

const harborShadowPerDepth = 8


// harborBoxes is the wall, flush containers first and then the pulled out
// ones from the shallowest to the deepest, the order they are drawn in.
var harborBoxes = func() []harborCrate {
	tones := []string{"a", "b", "c", "b", "a", "c", "b"}
	var flush []harborCrate
	front := make([][]harborCrate, len(harborDepths))
	for col, h := range harborHeights {
		for k := 0; k < h; k++ {
			at := [2]int{col, k}
			ref := harborLabels[at]
			step := harborFront[at]
			if ref != "" {
				step = harborLabelDepth[ref]
			}
			c := harborCrate{
				X:     harborX(col),
				Y:     harborY(k),
				Ref:   ref,
				Tone:  tones[(col*5+k*3)%len(tones)],
				Ink:   ref == "your-app",
				Depth: harborDepths[step],
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

func harborPoints(xy ...int) string {
	s := ""
	for i := 0; i+1 < len(xy); i += 2 {
		s += harborN(xy[i]) + "," + harborN(xy[i+1]) + " "
	}
	return s
}

// harborShadow is the shadow a pulled out container throws on the wall:
// the square where it leaves the wall, swept down and to the left.
func harborShadow(x, y, depth int) string {
	w, l := harborBox, depth*harborShadowPerDepth
	return harborPoints(x, y, x+w, y, x+w, y+w, x+w-l, y+w+l, x-l, y+w+l, x-l, y+l)
}

// harborSideLeft and harborSideBottom are the two sides of a pulled out
// container that show, between the wall and its front face.
func harborSideLeft(x, y, d int) string {
	w := harborBox
	return harborPoints(x, y, x+d, y-d, x+d, y-d+w, x, y+w)
}

func harborSideBottom(x, y, d int) string {
	w := harborBox
	return harborPoints(x, y+w, x+d, y+w-d, x+d+w, y+w-d, x+w, y+w)
}

// harborRibs are the x positions of the corrugation ribs of a container.
func harborRibs(x int) []int {
	var ribs []int
	for r := x + 8; r < x+harborBox-6; r += 8 {
		ribs = append(ribs, r)
	}
	return ribs
}

func harborTone(tone string) string {
	if tone == "a" {
		return "fill-card"
	}
	return "fill-muted"
}

func harborN(n int) string { return strconv.Itoa(n) }
