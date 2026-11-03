package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/asset"
	"github.com/worldiety/gift/font/inter"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/gifttest"
	"github.com/worldiety/gift/render"
	"github.com/worldiety/gift/ui"

	"github.com/torbenschinke/imgredactor/internal/redact"
)

// fakeImages stands in for the GPU: it hands out handles and counts uploads.
type fakeImages struct {
	next     render.ImageID
	live     map[render.ImageID]bool
	acquires int
}

func (f *fakeImages) Resolve(h render.ImageHandle) (render.ImageID, bool) { return h.ID, f.live[h.ID] }

func (f *fakeImages) Acquire(px render.Pixels) (render.ImageHandle, bool) {
	if px.IsEmpty() {
		panic("empty upload")
	}
	f.next++
	f.live[f.next] = true
	f.acquires++
	return render.ImageHandle{ID: f.next, Gen: 1}, true
}

func (f *fakeImages) Deallocate(h render.ImageHandle) { delete(f.live, h.ID) }

func writeShot(t *testing.T, path string, w, h int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{240, 240, 240, 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

type fixture struct {
	t   *testing.T
	h   *gifttest.Harness
	e   *Editor
	gpu *fakeImages
	dir string
}

func newFixture(t *testing.T) *fixture {
	dir := t.TempDir()
	writeShot(t, filepath.Join(dir, "01.png"), 800, 500)
	writeShot(t, filepath.Join(dir, "02.png"), 1200, 700)
	writeShot(t, filepath.Join(dir, "03.png"), 300, 200)

	var ed *Editor
	h := gifttest.New(t, gifttest.Options{
		Root: func(ctx *gift.Context) gift.View {
			if ed == nil { // the harness builds once before the editor exists
				return ui.Window()
			}
			return screen(ctx, ed)
		},
		Size: geom.Sz(1440, 920),
		Font: ui.MustFont(ui.FontQuery{Family: inter.Family}),
	})
	gpu := &fakeImages{live: map[render.ImageID]bool{}}
	h.App().SetImages(gpu)
	pipe := asset.NewPipeline(asset.Config{Deliver: h.App().Post})
	t.Cleanup(func() { pipe.Close(); h.App().DrainPosts() })
	ui.SetImagePipeline(pipe)
	ed = newEditor(h.App(), pipe)
	h.App().Invalidate()
	h.Settle()
	return &fixture{t: t, h: h, e: ed, gpu: gpu, dir: dir}
}

// waitDoc runs frames until the picture being opened has arrived.
func (f *fixture) waitDoc() {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for f.e.loading || f.e.doc == nil {
		if time.Now().After(deadline) {
			f.t.Fatal("the picture did not load")
		}
		time.Sleep(5 * time.Millisecond)
		f.h.Frame()
	}
	f.h.Settle()
}

// waitSaves waits for the files being written and lets their results in.
func (f *fixture) waitSaves() {
	f.e.saving.Wait()
	f.h.Frame()
	f.h.Settle()
}

// drag paints a horizontal stroke through the middle of the canvas.
func (f *fixture) drag(dx float32) {
	c := f.h.Find(gifttest.ByType("imgredactor.canvas")).Center()
	f.h.MoveTo(geom.Pt(c.X-dx, c.Y))
	f.h.PressAt(geom.Pt(c.X-dx, c.Y))
	for x := -dx; x <= dx; x += 10 {
		f.h.MoveTo(geom.Pt(c.X+x, c.Y))
		f.h.Frame()
	}
	f.h.ReleaseAt(geom.Pt(c.X+dx, c.Y))
	f.h.Settle()
}

func pixel(t *testing.T, path string, x, y int) color.RGBA {
	t.Helper()
	d, err := redact.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return d.Working().RGBAAt(x, y)
}

var black = color.RGBA{0, 0, 0, 255}

func TestEmptyStateOpensThePicker(t *testing.T) {
	f := newFixture(t)
	f.h.AssertExists(gifttest.ByText("Kein Ordner geöffnet"))
	f.h.Find(gifttest.ByText("Ordner öffnen …")).Click()
	f.h.Settle()
	if !f.e.picking {
		t.Fatal("the button does not open the folder picker")
	}
	f.h.AssertExists(gifttest.ByText("Ordner mit Screenshots wählen"))
}

func TestStrokeSaveOnSwitch(t *testing.T) {
	f := newFixture(t)
	f.e.OpenFolder(f.dir)
	f.waitDoc()
	f.h.AssertExists(gifttest.ByText("800 × 500"))
	f.h.AssertNoOverflow()
	if f.gpu.acquires == 0 {
		t.Fatal("the picture was never uploaded")
	}

	before := f.gpu.acquires
	f.drag(100)
	doc := f.e.doc
	if !doc.Modified() {
		t.Fatal("the stroke changed nothing")
	}
	if got := doc.Working().RGBAAt(400, 250); got != black {
		t.Fatalf("the middle of the stroke is %v, want black", got)
	}
	if got := doc.Working().RGBAAt(400, 50); got == black {
		t.Fatal("the stroke reached far beyond the brush")
	}
	// An 800 by 500 picture is eight tiles; a stroke through the middle
	// touches four of them, once per frame it moved in.
	if up := f.gpu.acquires - before; up == 0 || up > 4*30 {
		t.Fatalf("the stroke uploaded %d tiles", up)
	}
	f.h.AssertExists(gifttest.ByTextContains("Ungespeicherte Änderungen"))

	// The file is untouched until another picture is opened.
	if got := pixel(t, filepath.Join(f.dir, "01.png"), 400, 250); got == black {
		t.Fatal("the file was written before the picture was left")
	}
	f.h.Find(gifttest.ByText("02.png").And(gifttest.ByType("ui.Button"))).Click()
	f.waitDoc()
	f.waitSaves()
	if f.e.cur != 1 {
		t.Fatalf("the click opened picture %d", f.e.cur)
	}
	if got := pixel(t, filepath.Join(f.dir, "01.png"), 400, 250); got != black {
		t.Fatalf("the saved file has %v under the stroke, want black", got)
	}
	f.h.AssertExists(gifttest.ByText("Gespeichert: 01.png"))

	// An unchanged picture is not written.
	st, _ := os.Stat(filepath.Join(f.dir, "02.png"))
	f.h.Key(gift.KeyRight)
	f.waitDoc()
	f.waitSaves()
	st2, _ := os.Stat(filepath.Join(f.dir, "02.png"))
	if !st.ModTime().Equal(st2.ModTime()) || f.e.cur != 2 {
		t.Fatal("leaving an unchanged picture wrote it")
	}
}

func TestUndoResetAndTools(t *testing.T) {
	f := newFixture(t)
	f.e.OpenFolder(f.dir)
	f.waitDoc()

	f.h.TypeRune('3')
	f.h.Settle()
	if f.e.tool != redact.Blur {
		t.Fatal("3 does not select the blur brush")
	}
	// A segment is part of one control, so the click is aimed at its label.
	f.h.ClickAt(f.h.Find(gifttest.ByText("Schwärzen")).Center())
	f.h.Settle()
	if f.e.tool != redact.Black {
		t.Fatal("the segment does not select the black pen")
	}

	f.drag(80)
	f.h.Key(gift.KeyZ, gift.ModMeta)
	f.h.Settle()
	if f.e.doc.Modified() {
		t.Fatal("⌘Z does not undo the stroke")
	}

	f.drag(80)
	f.drag(40)
	if !f.e.doc.Modified() {
		t.Fatal("strokes after an undo change nothing")
	}
	f.h.Find(gifttest.ByText("Zurücksetzen").And(gifttest.ByType("ui.Button"))).Click()
	f.h.Settle()
	if f.e.doc.Modified() || f.e.doc.Working().RGBAAt(400, 250) == black {
		t.Fatal("reset does not discard the strokes")
	}

	// Closing the folder saves.
	f.drag(80)
	f.h.Find(gifttest.ByText("Ordner schließen").And(gifttest.ByType("ui.Button"))).Click()
	f.waitSaves()
	if got := pixel(t, filepath.Join(f.dir, "01.png"), 400, 250); got != black {
		t.Fatalf("closing the folder did not save: %v", got)
	}
	f.h.AssertExists(gifttest.ByText("Kein Ordner geöffnet"))
}

func TestBrushSize(t *testing.T) {
	f := newFixture(t)
	f.h.TypeRune(']')
	f.h.Settle()
	if f.e.brush <= defaultBrush {
		t.Fatal("] does not enlarge the brush")
	}
	for range 40 {
		f.h.TypeRune('[')
	}
	f.h.Settle()
	if f.e.brush != minBrush {
		t.Fatalf("the brush shrank to %v, want %v", f.e.brush, float32(minBrush))
	}
}
