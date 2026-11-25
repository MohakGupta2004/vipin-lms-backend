package email

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strconv"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// The radar mirrors the chart on the admin result page (components/diagnostic/radar-chart.tsx in the frontend):
// the same radius, rings and colours, in the same units. Labels are a little larger (26 instead of 22) so they
// still read when a phone shrinks the image to under 300px, so they also sit a little further out (222, not 214)
// and the view box (-370 -250 740 510 there) has more margin so no label touches the edge. Email clients do not render SVG, so it is drawn to a PNG
// at radarScale pixels per unit, about twice the size it is shown at so it stays sharp on retina screens.
const (
	radarMinX        = -395.0
	radarMinY        = -278.0
	radarViewWidth   = 790.0
	radarViewHeight  = 560.0
	radarRadius      = 178.0
	radarLabelRadius = 222.0
	radarLabelSize   = 26.0
	radarLineHeight  = 29.0
	radarMinDrawn    = 8.0 // zero scores are drawn 8% out so the shape still joins up
	radarScale       = 1.4
	radarLabelChars  = 14

	// radarDisplayWidth is the width the chart is shown at in an email: the card's 560px minus its padding.
	radarDisplayWidth = 496
	radarContentID    = "diagnostic-chart"
)

var (
	radarPixelWidth    = int(radarViewWidth * radarScale)
	radarPixelHeight   = int(radarViewHeight * radarScale)
	radarDisplayHeight = int(math.Round(radarDisplayWidth * radarViewHeight / radarViewWidth))

	radarLineColor  = color.NRGBA{0x10, 0x2a, 0x43, 0xff}
	radarLabelColor = color.NRGBA{0x33, 0x4e, 0x68, 0xff}
	radarTeal       = color.NRGBA{0x2f, 0x7f, 0x7f, 0xff} // hsl(180 46% 34%), the site's --teal
	radarRings      = []float64{0.25, 0.5, 0.75}

	radarFont = mustParseFont(gomedium.TTF)
)

var errRadarTooFewAxes = errors.New("a radar chart needs at least 3 axes")

func mustParseFont(ttf []byte) *opentype.Font {
	f, err := opentype.Parse(ttf)
	if err != nil {
		panic(err) // the font is embedded, so this only fails if the package itself is broken
	}
	return f
}

type radarPoint struct{ x, y float64 }

// radarPNG draws one axis per label with the matching 0-100 value, labelled with the label and its percent.
func radarPNG(labels []string, values []int) ([]byte, error) {
	if len(labels) < 3 || len(values) != len(labels) {
		return nil, errRadarTooFewAxes
	}
	c := &radarCanvas{img: image.NewRGBA(image.Rect(0, 0, radarPixelWidth, radarPixelHeight))}
	draw.Draw(c.img, c.img.Bounds(), image.White, image.Point{}, draw.Src) // white, so dark-mode clients do not hide the labels

	n := len(labels)
	axisEnds := make([]radarPoint, n)
	for i := range n {
		axisEnds[i] = radarAt(i, n, radarRadius)
	}

	// grid: inner rings and spokes faint, outer ring stronger
	var grid [][]radarPoint
	for _, ring := range radarRings {
		grid = append(grid, strokeClosed(scalePoints(axisEnds, ring), 1)...)
	}
	for _, end := range axisEnds {
		grid = append(grid, strokeSegment(radarPoint{}, end, 1))
	}
	c.fill(radarLineColor, 0.14, grid...)
	c.fill(radarLineColor, 0.45, strokeClosed(axisEnds, 1.5)...)

	// the score shape
	shape := make([]radarPoint, n)
	for i, v := range values {
		shape[i] = radarAt(i, n, max(float64(clampPercent(v)), radarMinDrawn)/100*radarRadius)
	}
	c.fill(radarTeal, 0.3, shape)
	outline := strokeClosed(shape, 3.5)
	for _, p := range shape {
		outline = append(outline, disc(p, 5))
	}
	c.fill(radarTeal, 1, outline...)

	// labels: the wrapped chapter name, then its percent
	face, err := opentype.NewFace(radarFont, &opentype.FaceOptions{Size: radarLabelSize * radarScale, DPI: 72})
	if err != nil {
		return nil, err
	}
	defer face.Close()
	// SVG's dominant-baseline="middle" puts the middle of the x-height on the line's y
	halfXHeight := float64(face.Metrics().XHeight) / 64 / 2
	for i, label := range labels {
		lines := wrapRadarLabel(label)
		at := radarAt(i, n, radarLabelRadius)
		top := at.y - float64(len(lines))*radarLineHeight/2 // len(lines)+1 lines in all, centred on the axis end
		for j, line := range append(lines, strconv.Itoa(clampPercent(values[i]))+"%") {
			col := radarLabelColor
			if j == len(lines) {
				col = radarLineColor
			}
			c.text(face, line, col, at.x, top+float64(j)*radarLineHeight, halfXHeight, radarAnchor(at))
		}
	}

	var out bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, c.img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

type radarCanvas struct {
	img *image.RGBA
}

func (c *radarCanvas) px(p radarPoint) (float32, float32) {
	return float32((p.x - radarMinX) * radarScale), float32((p.y - radarMinY) * radarScale)
}

// fill paints the union of the shapes (closed polygons in chart units) in col at the given opacity.
// Every shape is wound the same way, so where shapes overlap their coverage adds up instead of cancelling out.
func (c *radarCanvas) fill(col color.NRGBA, opacity float64, shapes ...[]radarPoint) {
	r := vector.NewRasterizer(c.img.Bounds().Dx(), c.img.Bounds().Dy())
	for _, shape := range shapes {
		if len(shape) < 3 {
			continue
		}
		if signedArea(shape) < 0 {
			shape = reversePoints(shape)
		}
		r.MoveTo(c.px(shape[0]))
		for _, p := range shape[1:] {
			r.LineTo(c.px(p))
		}
		r.ClosePath()
	}
	col.A = uint8(math.Round(opacity * 255))
	r.Draw(c.img, c.img.Bounds(), image.NewUniform(col), image.Point{})
}

// text draws one line with its x-height centred on y (chart units), anchored like SVG's text-anchor.
func (c *radarCanvas) text(face font.Face, s string, col color.NRGBA, x, y, halfXHeight float64, anchor string) {
	d := &font.Drawer{Dst: c.img, Src: image.NewUniform(col), Face: face}
	width := float64(d.MeasureString(s)) / 64
	px, py := c.px(radarPoint{x, y})
	left := float64(px)
	switch anchor {
	case "end":
		left -= width
	case "middle":
		left -= width / 2
	}
	d.Dot = fixed.Point26_6{X: fixed.Int26_6(math.Round(left * 64)), Y: fixed.Int26_6(math.Round((float64(py) + halfXHeight) * 64))}
	d.DrawString(s)
}

// radarAt is the point at radius on axis i of n, with axis 0 pointing straight up.
func radarAt(i, n int, radius float64) radarPoint {
	angle := -math.Pi/2 + 2*math.Pi*float64(i)/float64(n)
	return radarPoint{math.Cos(angle) * radius, math.Sin(angle) * radius}
}

func radarAnchor(p radarPoint) string {
	x := p.x / radarLabelRadius
	switch {
	case x > 0.3:
		return "start"
	case x < -0.3:
		return "end"
	default:
		return "middle"
	}
}

// wrapRadarLabel breaks a long axis label into short lines, like the frontend's wrapLabel.
func wrapRadarLabel(text string) []string {
	var lines []string
	line := ""
	for _, word := range strings.Fields(text) {
		switch {
		case line == "":
			line = word
		case len(line)+1+len(word) > radarLabelChars:
			lines = append(lines, line)
			line = word
		default:
			line += " " + word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func clampPercent(v int) int {
	return min(max(v, 0), 100)
}

func scalePoints(points []radarPoint, by float64) []radarPoint {
	out := make([]radarPoint, len(points))
	for i, p := range points {
		out[i] = radarPoint{p.x * by, p.y * by}
	}
	return out
}

// strokeClosed outlines a closed polygon with round joins, as shapes for fill.
func strokeClosed(points []radarPoint, width float64) [][]radarPoint {
	shapes := make([][]radarPoint, 0, 2*len(points))
	for i, p := range points {
		shapes = append(shapes, strokeSegment(p, points[(i+1)%len(points)], width), disc(p, width/2))
	}
	return shapes
}

// strokeSegment is the rectangle covering a line of the given width from a to b.
func strokeSegment(a, b radarPoint, width float64) []radarPoint {
	dx, dy := b.x-a.x, b.y-a.y
	length := math.Hypot(dx, dy)
	if length == 0 {
		return nil
	}
	nx, ny := -dy/length*width/2, dx/length*width/2
	return []radarPoint{{a.x + nx, a.y + ny}, {b.x + nx, b.y + ny}, {b.x - nx, b.y - ny}, {a.x - nx, a.y - ny}}
}

func disc(center radarPoint, radius float64) []radarPoint {
	const sides = 24
	points := make([]radarPoint, sides)
	for i := range sides {
		angle := 2 * math.Pi * float64(i) / sides
		points[i] = radarPoint{center.x + math.Cos(angle)*radius, center.y + math.Sin(angle)*radius}
	}
	return points
}

func signedArea(points []radarPoint) float64 {
	area := 0.0
	for i, p := range points {
		q := points[(i+1)%len(points)]
		area += p.x*q.y - q.x*p.y
	}
	return area / 2
}

func reversePoints(points []radarPoint) []radarPoint {
	out := make([]radarPoint, len(points))
	for i, p := range points {
		out[len(points)-1-i] = p
	}
	return out
}
