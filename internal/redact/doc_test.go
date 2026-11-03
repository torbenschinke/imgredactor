package redact

import (
	"image"
	"image/color"
	"path/filepath"
	"testing"
)

// stripes is a w by h picture of alternating white and dark rows, something
// a blur visibly changes.
func stripes(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		c := color.RGBA{250, 250, 250, 255}
		if y%4 < 2 {
			c = color.RGBA{20, 40, 60, 255}
		}
		for x := range w {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func TestBlackCoversTheBrushOnly(t *testing.T) {
	d := New("x.png", "png", stripes(600, 300))
	d.BeginStroke()
	d.Paint(Black, Point{100, 100}, Point{400, 100}, 10)
	d.EndStroke()

	if got := d.Working().RGBAAt(250, 100); got != (color.RGBA{0, 0, 0, 255}) {
		t.Fatalf("pixel on the stroke is %v, want black", got)
	}
	if got := d.Working().RGBAAt(250, 120); got == (color.RGBA{0, 0, 0, 255}) {
		t.Fatal("pixel outside the brush is black")
	}
	if !d.Modified() || !d.CanUndo() {
		t.Fatal("a stroke does not mark the document modified")
	}
}

func TestStrokeBumpsOnlyTouchedTiles(t *testing.T) {
	d := New("x.png", "png", stripes(3*TileSize, 2*TileSize))
	d.BeginStroke()
	d.Paint(Black, Point{10, 10}, Point{10, 10}, 4)
	d.EndStroke()
	for i := range 6 {
		want := uint32(0)
		if i == 0 {
			want = 1
		}
		if got := d.TileVersion(i); got != want {
			t.Fatalf("tile %d has version %d, want %d", i, got, want)
		}
	}
}

func TestEraseRestoresTheOriginal(t *testing.T) {
	src := stripes(200, 200)
	d := New("x.png", "png", src)
	d.BeginStroke()
	d.Paint(Black, Point{50, 50}, Point{150, 150}, 20)
	d.EndStroke()
	d.BeginStroke()
	d.Paint(Erase, Point{50, 50}, Point{150, 150}, 30)
	d.EndStroke()
	if string(d.Working().Pix) != string(src.Pix) {
		t.Fatal("erasing over a black stroke does not restore the picture")
	}
}

func TestBlurChangesStripes(t *testing.T) {
	src := stripes(400, 400)
	d := New("x.png", "png", src)
	d.BeginStroke()
	d.Paint(Blur, Point{200, 200}, Point{200, 200}, 50)
	d.EndStroke()
	a, b := d.Working().RGBAAt(200, 200), d.Working().RGBAAt(200, 202)
	if diff := int(a.R) - int(b.R); diff > 10 || diff < -10 {
		t.Fatalf("blurred rows still differ: %v and %v", a, b)
	}
	if d.Working().RGBAAt(10, 10) != src.RGBAAt(10, 10) {
		t.Fatal("blur outside the brush changed the picture")
	}
}

func TestUndoAndReset(t *testing.T) {
	src := stripes(300, 300)
	d := New("x.png", "png", src)
	d.BeginStroke()
	d.Paint(Black, Point{10, 10}, Point{290, 290}, 15)
	d.EndStroke()
	after1 := string(d.Working().Pix)
	d.BeginStroke()
	d.Paint(Black, Point{290, 10}, Point{10, 290}, 15)
	d.EndStroke()

	if !d.Undo() || string(d.Working().Pix) != after1 {
		t.Fatal("undo does not restore the state after the first stroke")
	}
	if !d.Undo() || string(d.Working().Pix) != string(src.Pix) || d.Modified() {
		t.Fatal("undoing every stroke does not restore the original")
	}
	if d.Undo() {
		t.Fatal("undo with nothing left reported success")
	}

	d.BeginStroke()
	d.Paint(Blur, Point{150, 150}, Point{150, 150}, 40)
	d.EndStroke()
	d.Reset()
	if string(d.Working().Pix) != string(src.Pix) || d.Modified() || d.CanUndo() {
		t.Fatal("reset does not restore the original")
	}
}

func TestWriteFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	for _, format := range []string{"png", "jpeg"} {
		path := filepath.Join(dir, "shot."+format)
		if err := WriteFile(path, stripes(64, 48), format); err != nil {
			t.Fatal(err)
		}
		d, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		d.BeginStroke()
		d.Paint(Black, Point{0, 0}, Point{64, 48}, 6)
		d.EndStroke()
		if err := WriteFile(path, d.Working(), d.Format()); err != nil {
			t.Fatal(err)
		}
		d2, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		if w, h := d2.Size(); w != 64 || h != 48 || d2.Format() != format {
			t.Fatalf("%s: reloaded %dx%d %s", format, w, h, d2.Format())
		}
		if c := d2.Working().RGBAAt(32, 24); c.R > 16 || c.G > 16 || c.B > 16 {
			t.Fatalf("%s: the redaction did not survive saving: %v", format, c)
		}
	}
	if m, _ := filepath.Glob(filepath.Join(dir, ".*.tmp")); len(m) > 0 {
		t.Fatalf("temporary files left behind: %v", m)
	}
}
