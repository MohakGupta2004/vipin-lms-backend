package email

import (
	"bytes"
	"image/png"
	"reflect"
	"strings"
	"testing"
)

func TestRadarPNG(t *testing.T) {
	labels := []string{"One", "Two", "Three", "Four", "Five"}
	out, err := radarPNG(labels, []int{100, 0, 50, -20, 140})
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != radarPixelWidth || b.Dy() != radarPixelHeight {
		t.Fatalf("size = %v", b)
	}
	// the top axis scores 100%, so its dot sits at the end of the axis, in solid teal
	tip := radarAt(0, len(labels), radarRadius)
	x, y := (tip.x-radarMinX)*radarScale, (tip.y-radarMinY)*radarScale
	r, g, b, _ := img.At(int(x), int(y)).RGBA()
	if got := [3]uint32{r >> 8, g >> 8, b >> 8}; got != [3]uint32{uint32(radarTeal.R), uint32(radarTeal.G), uint32(radarTeal.B)} {
		t.Errorf("dot colour = %v, want teal", got)
	}
	// a corner is white background
	if r, g, b, _ := img.At(0, 0).RGBA(); r>>8 != 0xff || g>>8 != 0xff || b>>8 != 0xff {
		t.Errorf("corner is not white")
	}
}

func TestRadarPNGNeedsThreeAxes(t *testing.T) {
	if _, err := radarPNG([]string{"a", "b"}, []int{1, 2}); err == nil {
		t.Error("want error for 2 axes")
	}
	if _, err := radarPNG([]string{"a", "b", "c"}, []int{1, 2}); err == nil {
		t.Error("want error for mismatched values")
	}
}

func TestWrapRadarLabel(t *testing.T) {
	got := wrapRadarLabel("Prospective client relationships")
	want := []string{"Prospective", "client", "relationships"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := wrapRadarLabel("Market and company analysis"); !reflect.DeepEqual(got, []string{"Market and", "company", "analysis"}) {
		t.Errorf("got %q", got)
	}
}

func TestRenderDiagnosticResultChart(t *testing.T) {
	elements := []DiagnosticElementLine{{Name: "A", Percent: 10}, {Name: "B", Percent: 50}, {Name: "C", Percent: 90}}
	msg, err := RenderDiagnosticResult("a@b.co", DiagnosticResultData{TestTitle: "T", Elements: elements})
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(msg.Attachments))
	}
	a := msg.Attachments[0]
	if a.ContentID != radarContentID || a.ContentType != "image/png" || len(a.Content) == 0 {
		t.Errorf("attachment = %q %q %d bytes", a.ContentID, a.ContentType, len(a.Content))
	}
	if !strings.Contains(msg.HTML, `src="cid:`+radarContentID+`"`) {
		t.Error("html does not show the inline chart")
	}

	msg, err = RenderDiagnosticResult("a@b.co", DiagnosticResultData{TestTitle: "T", Elements: elements[:2]})
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Attachments) != 0 || strings.Contains(msg.HTML, "cid:") {
		t.Error("a chart was added for fewer than 3 elements")
	}
}
