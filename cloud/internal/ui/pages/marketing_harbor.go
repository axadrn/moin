package pages

import "strconv"

// The harbor of the hero is a still wall of 20-foot shipping containers
// seen from the side, three tiers, every second tier shifted half a box.
// Every container is an app: the owner code and type code printed small at
// the top right, the app's name big at the bottom left. A few containers
// are pulled out of the wall and throw a short shadow. Units are SVG user
// units; the wall is anchored at the bottom and cut at the sides (slice).
const (
	harborW     = 2400
	harborH     = 300
	harborTopY  = -30 // room above the top tier for pulled out boxes
	harborLeftX = 118 // shifts the view so your-app sits in the middle
	harborColW  = 240 // a 20-foot box plus the gap
	harborBoxW  = 236
	harborBoxH  = 96
	harborTierH = 100
	harborPitch = 9 // the corrugation
)

// harborHeights is how many containers stand in each column.
var harborHeights = []int{1, 2, 2, 3, 2, 3, 3, 2, 3, 2}

// harborLabels are the apps printed on containers, by column and tier from
// the ground up, spread over the middle that every screen shows.
var harborLabels = map[[2]int]string{
	{1, 1}: "node",
	{2, 1}: "sqlite",
	{3, 1}: "postgres",
	{3, 2}: "python",
	{4, 1}: "redis",
	{8, 1}: "bun",
	{5, 2}: "your-app",
	{6, 1}: "rails",
	{9, 1}: "golang",
	{8, 2}: "deno",
	{0, 0}: "php",
	{5, 0}: "nextjs",
	{7, 0}: "django",
}

// harborCodes are the owner codes printed next to each app, like the four
// letters and the serial on a real container (the fourth letter is U).
var harborCodes = map[string]string{
	"node": "NODU 104421", "sqlite": "SQLU 207734", "postgres": "PGSU 310652", "python": "PYTU 418803",
	"redis": "RDSU 520167", "bun": "BUNU 623390", "your-app": "APPU 100042", "rails": "RLSU 731248",
	"golang": "GOLU 846501", "deno": "DNOU 952276",
	"nextjs": "NXJU 118250", "php": "PHPU 224519", "django": "DJGU 437712",
}

// harborFront are the containers pulled out of the wall, by column and
// tier, with how far they stand out: your-app the most, and two quiet
// ones, so the eye goes to your app.
var harborFront = map[[2]int]int{
	{2, 1}: 4, {5, 2}: 10, {7, 1}: 6,
}

// harborShadowPerDepth is how long a shadow falls per unit a container
// stands out. Light comes from the top right, the shadow falls down left.
const harborShadowPerDepth = 7

// harborCrate is one container in the wall.
type harborCrate struct {
	X, Y  int
	Ref   string // the app printed on it, empty for none
	Code  string // its owner code, with the app
	Tone  int    // 0 to 3, four grays
	Ink   bool   // your-app, in the foreground color
	Depth int    // how far it stands out of the wall, 0 when flush
}

// harborBoxes is the wall: flush containers first, then the pulled out ones
// from the shallowest to the deepest, the order they are drawn in.
var harborBoxes = func() []harborCrate {
	var flush, front []harborCrate
	for col, h := range harborHeights {
		for k := 0; k < h; k++ {
			at := [2]int{col, k}
			x := col * harborColW
			if k%2 == 1 {
				x += harborColW / 2
			}
			ref := harborLabels[at]
			c := harborCrate{
				X:     x,
				Y:     harborH - (k+1)*harborTierH + (harborTierH - harborBoxH),
				Ref:   ref,
				Code:  harborCodes[ref],
				Tone:  (col*3 + k*2 + col/3) % 4,
				Ink:   ref == "your-app",
				Depth: harborFront[at],
			}
			if c.Depth == 0 {
				flush = append(flush, c)
			} else {
				front = append(front, c)
			}
		}
	}
	for d := 1; d <= 10; d++ {
		for _, c := range front {
			if c.Depth == d {
				flush = append(flush, c)
			}
		}
	}
	return flush
}()

// harborTone is the fill of a container face: two grays of the theme, and
// the same two a step darker (light theme) or lighter (dark theme).
func harborTone(tone int) (base, over string) {
	switch tone {
	case 0:
		return "hub-harbor-card", ""
	case 1:
		return "hub-harbor-muted", ""
	case 2:
		return "hub-harbor-muted", "hub-harbor-tint-9"
	}
	return "hub-harbor-card", "hub-harbor-tint-5"
}

// harborRibs are the x positions of the corrugation ribs of a container.
func harborRibs(x int) []int {
	var ribs []int
	for r := x + harborPitch; r < x+harborBoxW-harborPitch*7/10; r += harborPitch {
		ribs = append(ribs, r)
	}
	return ribs
}

func harborPoints(xy ...int) string {
	s := ""
	for i := 0; i+1 < len(xy); i += 2 {
		s += harborN(xy[i]) + "," + harborN(xy[i+1]) + " "
	}
	return s
}

// harborShadow is the shadow a pulled out container throws on the wall:
// its front face swept down and to the left.
func harborShadow(x, y, d int) string {
	fx, fy, w, h, l := x+d, y-d, harborBoxW, harborBoxH, d*harborShadowPerDepth
	return harborPoints(fx, fy, fx+w, fy, fx+w, fy+h, fx+w-l, fy+h+l, fx-l, fy+h+l, fx-l, fy+l)
}

// harborSideLeft and harborSideBottom are the two sides of a pulled out
// container that show, between the wall and its front face.
func harborSideLeft(x, y, d int) string {
	return harborPoints(x, y, x+d, y-d, x+d, y-d+harborBoxH, x, y+harborBoxH)
}

func harborSideBottom(x, y, d int) string {
	w, h := harborBoxW, harborBoxH
	return harborPoints(x, y+h, x+d, y+h-d, x+d+w, y+h-d, x+w, y+h)
}

func harborN(n int) string { return strconv.Itoa(n) }
