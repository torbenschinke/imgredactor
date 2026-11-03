package main

import (
	"image"
	"math"
	"time"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/render"
	"github.com/worldiety/gift/ui/gesture"

	"github.com/torbenschinke/imgredactor/internal/redact"
)

var canvasType = gift.RegisterType("imgredactor.canvas")

// canvasInset is the room between the picture and the edge of the canvas, in
// logical pixels, so that a brush can start just outside the picture.
const canvasInset = 16

// canvasState is what the canvas keeps across builds: the textures of the
// tiles of the picture it shows and where the pointer is. It lives in the
// [Editor] because a view node is thrown away on every build.
type canvasState struct {
	doc      *redact.Document
	handles  []render.ImageHandle
	versions []uint32

	// invalidate lays the canvas out and repaints it, without a build. It
	// is how a stroke reaches the screen.
	invalidate func()

	pointer    geom.Point // in the canvas' own coordinates
	showCursor bool

	stroking bool
	prev     redact.Point // the last point of the stroke in progress
	last     redact.Point // the end of the last stroke, for shift-click lines
	hasLast  bool
}

func (st *canvasState) repaint() {
	if st.invalidate != nil {
		st.invalidate()
	}
}

// placement is where a picture of iw by ih pixels is drawn inside a canvas of
// w by h: its top left corner and its scale, in logical pixels per picture
// pixel. A picture is fitted into the canvas and never enlarged beyond one
// logical pixel per picture pixel.
func placement(w, h float32, iw, ih int) (origin geom.Point, scale float32) {
	aw, ah := max(w-2*canvasInset, 1), max(h-2*canvasInset, 1)
	scale = min(aw/float32(iw), ah/float32(ih), 1)
	return geom.Pt((w-float32(iw)*scale)/2, (h-float32(ih)*scale)/2), scale
}

// canvas is the picture being redacted with the brush gestures attached.
func canvas(e *Editor) gift.View {
	st := &e.view
	// toPicture maps a position the gestures report into picture pixels.
	toPicture := func(c gesture.Common, p geom.Point) redact.Point {
		w, h := e.doc.Size()
		origin, scale := placement(c.Bounds.Width(), c.Bounds.Height(), w, h)
		l := c.Local(p)
		return redact.Point{X: float64((l.X - origin.X) / scale), Y: float64((l.Y - origin.Y) / scale)}
	}
	radius := func(c gesture.Common) float64 {
		w, h := e.doc.Size()
		_, scale := placement(c.Bounds.Width(), c.Bounds.Height(), w, h)
		return float64(e.brush / 2 / scale)
	}

	drag := gesture.Drag().MinimumDistance(0).
		OnChanged(func(v gesture.DragValue) {
			if e.doc == nil || v.Fingers > 1 {
				return
			}
			p, r := toPicture(v.Common, v.Location), radius(v.Common)
			st.pointer, st.showCursor = v.Local(v.Location), true
			if v.Phase == gesture.Began {
				e.doc.BeginStroke()
				st.stroking = true
				from := toPicture(v.Common, v.StartLocation)
				if v.Mods&gift.ModShift != 0 && st.hasLast {
					from = st.last
				}
				e.doc.Paint(e.tool, from, from, r)
				st.prev = from
			}
			if !st.stroking {
				return
			}
			e.doc.Paint(e.tool, st.prev, p, r)
			st.prev = p
			st.repaint()
		}).
		OnEnded(func(gesture.DragValue) { e.endStroke() }).
		OnCancelled(func() { e.endStroke() })

	hover := gesture.Hover().OnChanged(func(v gesture.HoverValue) {
		st.pointer, st.showCursor = v.Local(v.Location), v.Inside
		st.repaint()
	})

	return gesture.On(canvasView{e: e}, drag, hover).Label("Bild").Flex(1)
}

type canvasView struct{ e *Editor }

func (canvasView) ViewType() gift.TypeID { return canvasType }

func (v canvasView) Build(*gift.BuildContext) gift.Element {
	n := &canvasNode{e: v.e}
	return gift.Element{Key: "canvas", Label: "Bild", Layouter: n, Painter: n, Flex: 1}
}

type canvasNode struct{ e *Editor }

// Layout takes all the room it is offered.
func (n *canvasNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	n.e.view.invalidate = ctx.Invalidator()
	w, h := c.Max.W, c.Max.H
	if math.IsInf(float64(w), 1) {
		w = 640
	}
	if math.IsInf(float64(h), 1) {
		h = 480
	}
	return c.Constrain(geom.Sz(w, h))
}

var opaque = render.RGBA(255, 255, 255, 255)

// Paint draws the picture tile by tile. A tile is uploaded again only when its
// version changed, so a stroke costs the few tiles under the brush and not
// the whole screenshot.
func (n *canvasNode) Paint(ctx *gift.PaintContext) {
	st, doc := &n.e.view, n.e.doc
	images := ctx.Images()
	if images == nil {
		return
	}
	if st.doc != doc {
		for _, h := range st.handles {
			images.Deallocate(h)
		}
		st.doc, st.handles, st.versions = doc, nil, nil
		if doc != nil {
			tx, ty := doc.Tiles()
			st.handles = make([]render.ImageHandle, tx*ty)
			st.versions = make([]uint32, tx*ty)
		}
	}
	if doc == nil {
		return
	}

	b := ctx.Bounds()
	w, h := doc.Size()
	origin, scale := placement(b.Width(), b.Height(), w, h)
	origin = geom.Pt(b.Min.X+origin.X, b.Min.Y+origin.Y)
	ctx.PushClip(ctx.DeviceBounds())
	defer ctx.PopClip()

	waiting := false
	for i := range st.handles {
		id, ok := images.Resolve(st.handles[i])
		if !ok || st.versions[i] != doc.TileVersion(i) {
			// The new texture first: when the frame's upload budget is
			// spent, the old one is drawn for another frame rather than
			// a hole.
			if nh, uploaded := images.Acquire(tilePixels(doc.Working(), doc.TileRect(i))); uploaded {
				images.Deallocate(st.handles[i])
				st.handles[i], st.versions[i] = nh, doc.TileVersion(i)
				id, ok = images.Resolve(nh)
			} else {
				waiting = true
			}
		}
		if !ok {
			continue
		}
		r := doc.TileRect(i)
		ctx.Add(render.Op{
			Kind: render.OpImage, Fit: render.ImageTile, Color: opaque, Image: id,
			Bounds: geom.Rc(
				origin.X+float32(r.Min.X)*scale, origin.Y+float32(r.Min.Y)*scale,
				origin.X+float32(r.Max.X)*scale, origin.Y+float32(r.Max.Y)*scale),
		})
	}
	if waiting {
		ctx.Animate(16 * time.Millisecond)
	}

	if st.showCursor {
		// The brush outline, dark and light so it shows on any picture.
		rad := n.e.brush / 2
		c := geom.Pt(b.Min.X+st.pointer.X, b.Min.Y+st.pointer.Y)
		ring := func(r, width float32, col render.Color) {
			ctx.Add(render.Op{Kind: render.OpStrokeRoundRect, Color: col, StrokeWidth: width,
				CornerRadius: r, Bounds: geom.Rc(c.X-r, c.Y-r, c.X+r, c.Y+r)})
		}
		ring(rad+1, 1.5, render.RGBA(0, 0, 0, 200))
		ring(max(rad-0.5, 1), 1.5, render.RGBA(255, 255, 255, 230))
	}
}

// tilePixels is the part r of img, without copying it.
func tilePixels(img *image.RGBA, r image.Rectangle) render.Pixels {
	o := img.PixOffset(r.Min.X, r.Min.Y)
	return render.Pixels{Pix: img.Pix[o:], W: r.Dx(), H: r.Dy(), Stride: img.Stride}
}
