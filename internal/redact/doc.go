// Package redact holds the pixels of one screenshot while it is being
// redacted: the picture as it was opened, the picture as it is now, and what
// it takes to undo, reset and save the difference.
//
// It knows nothing about the user interface. The editor draws the working
// picture in tiles of [TileSize], and every tile carries a version that
// changes whenever a stroke touches it, so only what changed is uploaded
// again.
package redact

import (
	"image"
	"image/draw"
	"math"
)

// Tool is what a stroke does to the pixels under the brush.
type Tool uint8

const (
	// Black paints opaque black.
	Black Tool = iota
	// Erase restores the pixels the picture had when it was opened.
	Erase
	// Blur replaces the pixels with a heavily blurred copy of the picture as
	// it was opened.
	Blur
)

// TileSize is the edge length of the tiles the working picture is versioned
// and uploaded in.
const TileSize = 256

// maxUndoBytes bounds the tile snapshots kept for undo. The oldest strokes
// lose their undo first; their effect stays and can only be reset.
const maxUndoBytes = 512 << 20

// Point is a position in picture pixels.
type Point struct{ X, Y float64 }

// Document is one picture being redacted. It belongs to one goroutine; only
// the blurred copy is computed on another one.
type Document struct {
	path   string
	format string
	orig   *image.RGBA
	work   *image.RGBA
	blur   func() *image.RGBA

	tilesX, tilesY int
	tileVer        []uint32

	undo      []*undoRecord
	undoBytes int
	stroke    *undoRecord
	edits     int
}

// undoRecord holds the tiles one stroke changed, as they were before it.
type undoRecord struct {
	tiles map[int][]byte
	bytes int
}

// New returns a document for img, which was decoded from path in format
// ("png" or "jpeg"). The picture is copied; img is not retained.
func New(path, format string, img image.Image) *Document {
	b := img.Bounds()
	orig := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(orig, orig.Rect, img, b.Min, draw.Src)
	work := image.NewRGBA(orig.Rect)
	copy(work.Pix, orig.Pix)

	d := &Document{path: path, format: format, orig: orig, work: work}
	d.tilesX = (b.Dx() + TileSize - 1) / TileSize
	d.tilesY = (b.Dy() + TileSize - 1) / TileSize
	d.tileVer = make([]uint32, d.tilesX*d.tilesY)

	// The blurred copy is only needed once the blur brush is used, but
	// computing it takes a moment on a large screenshot. It starts now, in
	// the background, and the first blur stroke waits for it if it must.
	var blurred *image.RGBA
	done := make(chan struct{})
	go func() {
		blurred = boxBlur(orig, blurRadius(b.Dx(), b.Dy()))
		close(done)
	}()
	d.blur = func() *image.RGBA { <-done; return blurred }
	return d
}

// Path is the file the document was opened from.
func (d *Document) Path() string { return d.path }

// Format is the encoding the file had, "png" or "jpeg".
func (d *Document) Format() string { return d.format }

// Size returns the picture's dimensions in pixels.
func (d *Document) Size() (w, h int) { return d.work.Rect.Dx(), d.work.Rect.Dy() }

// Working is the picture as it is now. Its pixels are premultiplied RGBA and
// must not be changed by the caller.
func (d *Document) Working() *image.RGBA { return d.work }

// Tiles returns the number of tiles across and down.
func (d *Document) Tiles() (x, y int) { return d.tilesX, d.tilesY }

// TileRect returns the pixel rectangle of tile i, counted row by row.
func (d *Document) TileRect(i int) image.Rectangle {
	tx, ty := i%d.tilesX, i/d.tilesX
	r := image.Rect(tx*TileSize, ty*TileSize, (tx+1)*TileSize, (ty+1)*TileSize)
	return r.Intersect(d.work.Rect)
}

// TileVersion changes whenever the pixels of tile i change.
func (d *Document) TileVersion(i int) uint32 { return d.tileVer[i] }

// Modified reports whether the working picture differs from the opened one
// by at least one stroke that was not undone.
func (d *Document) Modified() bool { return d.edits > 0 }

// CanUndo reports whether there is a stroke to undo.
func (d *Document) CanUndo() bool { return len(d.undo) > 0 }

// BeginStroke starts a stroke: everything painted until [Document.EndStroke]
// is undone in one step.
func (d *Document) BeginStroke() {
	d.stroke = &undoRecord{tiles: map[int][]byte{}}
}

// EndStroke finishes the stroke begun by [Document.BeginStroke].
func (d *Document) EndStroke() {
	rec := d.stroke
	d.stroke = nil
	if rec == nil || len(rec.tiles) == 0 {
		return
	}
	d.edits++
	d.undo = append(d.undo, rec)
	d.undoBytes += rec.bytes
	for d.undoBytes > maxUndoBytes && len(d.undo) > 1 {
		d.undoBytes -= d.undo[0].bytes
		d.undo[0] = nil
		d.undo = d.undo[1:]
	}
}

// Paint applies t along the segment from a to b with a round brush of radius
// r, all in picture pixels. A single dab is a segment whose ends coincide.
func (d *Document) Paint(t Tool, a, b Point, r float64) {
	r = max(r, 0.5)
	area := image.Rect(
		int(math.Floor(min(a.X, b.X)-r)), int(math.Floor(min(a.Y, b.Y)-r)),
		int(math.Ceil(max(a.X, b.X)+r))+1, int(math.Ceil(max(a.Y, b.Y)+r))+1,
	).Intersect(d.work.Rect)
	if area.Empty() {
		return
	}
	var src *image.RGBA
	switch t {
	case Erase:
		src = d.orig
	case Blur:
		src = d.blur()
	}
	d.touch(area)

	abx, aby := b.X-a.X, b.Y-a.Y
	l2 := abx*abx + aby*aby
	r2 := r * r
	stride := d.work.Stride
	for y := area.Min.Y; y < area.Max.Y; y++ {
		py := float64(y) + 0.5
		row := d.work.Pix[y*stride : y*stride+stride]
		for x := area.Min.X; x < area.Max.X; x++ {
			px := float64(x) + 0.5
			// The distance of the pixel centre to the segment.
			f := 0.0
			if l2 > 0 {
				f = min(max(((px-a.X)*abx+(py-a.Y)*aby)/l2, 0), 1)
			}
			dx, dy := px-(a.X+f*abx), py-(a.Y+f*aby)
			if dx*dx+dy*dy > r2 {
				continue
			}
			o := x * 4
			if src == nil {
				row[o], row[o+1], row[o+2], row[o+3] = 0, 0, 0, 255
			} else {
				copy(row[o:o+4], src.Pix[y*stride+o:y*stride+o+4])
			}
		}
	}
}

// touch snapshots the tiles under area for the stroke in progress and bumps
// their versions.
func (d *Document) touch(area image.Rectangle) {
	for ty := area.Min.Y / TileSize; ty <= (area.Max.Y-1)/TileSize; ty++ {
		for tx := area.Min.X / TileSize; tx <= (area.Max.X-1)/TileSize; tx++ {
			i := ty*d.tilesX + tx
			if rec := d.stroke; rec != nil {
				if _, ok := rec.tiles[i]; !ok {
					pix := d.copyTile(i)
					rec.tiles[i] = pix
					rec.bytes += len(pix)
				}
			}
			d.tileVer[i]++
		}
	}
}

// copyTile returns the pixels of tile i, row after row without padding.
func (d *Document) copyTile(i int) []byte {
	r := d.TileRect(i)
	w := r.Dx() * 4
	out := make([]byte, 0, w*r.Dy())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		o := y*d.work.Stride + r.Min.X*4
		out = append(out, d.work.Pix[o:o+w]...)
	}
	return out
}

// Undo reverts the last stroke and reports whether there was one.
func (d *Document) Undo() bool {
	if len(d.undo) == 0 {
		return false
	}
	rec := d.undo[len(d.undo)-1]
	d.undo[len(d.undo)-1] = nil
	d.undo = d.undo[:len(d.undo)-1]
	d.undoBytes -= rec.bytes
	for i, pix := range rec.tiles {
		r := d.TileRect(i)
		w := r.Dx() * 4
		for y := r.Min.Y; y < r.Max.Y; y++ {
			o := y*d.work.Stride + r.Min.X*4
			copy(d.work.Pix[o:o+w], pix[(y-r.Min.Y)*w:])
		}
		d.tileVer[i]++
	}
	d.edits--
	return true
}

// Reset discards every change since the document was opened.
func (d *Document) Reset() {
	copy(d.work.Pix, d.orig.Pix)
	clear(d.undo)
	d.undo, d.undoBytes, d.stroke, d.edits = d.undo[:0], 0, nil, 0
	for i := range d.tileVer {
		d.tileVer[i]++
	}
}
