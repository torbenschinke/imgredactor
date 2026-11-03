package main

import (
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/asset"
	"github.com/worldiety/gift/ui"

	"github.com/torbenschinke/imgredactor/internal/redact"
)

// Brush sizes, as diameters in logical pixels on the screen.
const (
	minBrush     = 4
	maxBrush     = 200
	defaultBrush = 28
)

// Editor is the state of the application: the folder, the picture being
// redacted, the tool and the saves under way. It belongs to the UI goroutine.
//
// Changes the toolbar or the strip show end with [Editor.changed], which
// rebuilds the screen. Strokes do not; they only repaint the canvas.
type Editor struct {
	app  *gift.App
	pipe *asset.Pipeline

	dir   string
	files []string // absolute paths, sorted by name
	cur   int      // index into files, or -1

	doc     *redact.Document
	loading bool
	loadGen int
	loadErr string

	tool  redact.Tool
	brush float32

	picking bool
	reveal  bool // scroll the current thumbnail into view after the next layout
	status  string
	toast   string
	toastOn bool
	saves   int // completed saves, so the strip reloads its thumbnails

	thumbRev map[string]int     // bumped when a file was saved, to reload its thumbnail
	pending  map[string]pendingSave // pictures being written, by path
	saving   sync.WaitGroup

	view canvasState
}

// pendingSave is a picture on its way to its file.
type pendingSave struct {
	img    *image.RGBA
	format string
}

func newEditor(app *gift.App, pipe *asset.Pipeline) *Editor {
	return &Editor{
		app: app, pipe: pipe, cur: -1, brush: defaultBrush,
		thumbRev: map[string]int{}, pending: map[string]pendingSave{},
	}
}

// changed rebuilds the screen.
func (e *Editor) changed() { e.app.Invalidate() }

// notify shows a message in a toast.
func (e *Editor) notify(msg string) {
	e.toast, e.toastOn = msg, true
	e.changed()
}

// OpenFolder saves the picture being edited and opens the pictures in dir.
func (e *Editor) OpenFolder(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		e.notify("Ordner kann nicht geöffnet werden: " + err.Error())
		return
	}
	e.CloseFolder()
	e.dir = dir
	for _, de := range entries {
		if de.Type().IsRegular() && !strings.HasPrefix(de.Name(), ".") && redact.IsPicture(de.Name()) {
			e.files = append(e.files, filepath.Join(dir, de.Name()))
		}
	}
	slices.SortFunc(e.files, func(a, b string) int {
		return strings.Compare(strings.ToLower(filepath.Base(a)), strings.ToLower(filepath.Base(b)))
	})
	e.status = fmt.Sprintf("%d Bilder in %s", len(e.files), dir)
	if len(e.files) > 0 {
		e.Select(0)
	}
	e.changed()
}

// CloseFolder saves the picture being edited and closes the folder.
func (e *Editor) CloseFolder() {
	e.saveCurrent()
	e.dir, e.files, e.cur = "", nil, -1
	e.loading, e.loadErr = false, ""
	e.loadGen++
	e.status = ""
	e.changed()
}

// Select saves the picture being edited and opens the i-th one.
func (e *Editor) Select(i int) {
	if i < 0 || i >= len(e.files) || i == e.cur {
		return
	}
	e.saveCurrent()
	e.cur, e.reveal = i, true
	e.loading, e.loadErr = true, ""
	e.loadGen++
	e.view.hasLast = false
	gen, path := e.loadGen, e.files[i]
	// A picture still being written is taken from memory: the file may not
	// have been replaced yet.
	saving, isSaving := e.pending[path]
	go func() {
		var doc *redact.Document
		var err error
		if isSaving {
			doc = redact.New(path, saving.format, saving.img)
		} else {
			doc, err = redact.Load(path)
		}
		e.app.Post(func() {
			if gen != e.loadGen {
				return
			}
			e.doc, e.loading = doc, false
			if err != nil {
				e.loadErr = err.Error()
			}
			e.changed()
		})
	}()
	e.changed()
}

// Step selects the picture d places away from the current one.
func (e *Editor) Step(d int) {
	if len(e.files) == 0 {
		return
	}
	e.Select(min(max(e.cur+d, 0), len(e.files)-1))
}

// saveCurrent hands the picture being edited to a goroutine that writes it,
// if it was changed, and closes it.
func (e *Editor) saveCurrent() {
	doc := e.doc
	e.doc = nil
	e.view.stroking = false
	if doc == nil || !doc.Modified() {
		return
	}
	path, format, img := doc.Path(), doc.Format(), doc.Working()
	e.pending[path] = pendingSave{img, format}
	e.saving.Go(func() {
		err := redact.WriteFile(path, img, format)
		e.app.Post(func() {
			if e.pending[path].img == img {
				delete(e.pending, path)
			}
			if err != nil {
				e.notify("Speichern fehlgeschlagen: " + err.Error())
				return
			}
			e.status = "Gespeichert: " + filepath.Base(path)
			// The thumbnail shows the file, which just changed.
			e.thumbRev[path]++
			e.saves++
			id := asset.File(path).Metadata().ID
			e.pipe.Invalidate(id)
			ui.ForgetImage(id)
			e.changed()
		})
	})
}

// Shutdown saves the picture being edited and waits for every save.
func (e *Editor) Shutdown() {
	e.saveCurrent()
	e.saving.Wait()
}

// endStroke finishes the stroke in progress.
func (e *Editor) endStroke() {
	st := &e.view
	if !st.stroking || e.doc == nil {
		return
	}
	e.doc.EndStroke()
	st.stroking, st.last, st.hasLast = false, st.prev, true
	e.changed()
}

// Undo reverts the last stroke.
func (e *Editor) Undo() {
	if e.doc != nil && e.doc.Undo() {
		e.view.hasLast = false
		e.changed()
	}
}

// Reset discards every change to the picture being edited.
func (e *Editor) Reset() {
	if e.doc != nil && e.doc.Modified() {
		e.doc.Reset()
		e.view.hasLast = false
		e.changed()
	}
}

// SetTool selects the tool of the brush.
func (e *Editor) SetTool(t redact.Tool) {
	e.tool = t
	e.changed()
}

// SetBrush sets the brush diameter, clamped to its range.
func (e *Editor) SetBrush(d float32) {
	e.brush = min(max(float32(math.Round(float64(d))), minBrush), maxBrush)
	e.view.repaint()
	e.changed()
}

// Key handles the keyboard shortcuts and reports whether it used the event.
func (e *Editor) Key(ev gift.Event) bool {
	if e.picking {
		return false
	}
	cmd := ev.Mods&(gift.ModMeta|gift.ModControl) != 0
	switch ev.Kind {
	case gift.EventKeyDown:
		switch {
		case ev.Key == gift.KeyLeft || ev.Key == gift.KeyUp:
			e.Step(-1)
		case ev.Key == gift.KeyRight || ev.Key == gift.KeyDown:
			e.Step(1)
		case ev.Key == gift.KeyHome:
			e.Select(0)
		case ev.Key == gift.KeyEnd:
			e.Select(len(e.files) - 1)
		case ev.Key == gift.KeyZ && cmd && !ev.Repeat:
			e.Undo()
		default:
			return false
		}
		return true
	case gift.EventRune:
		if cmd {
			return false
		}
		switch ev.Rune {
		case '1', 's', 'S':
			e.SetTool(redact.Black)
		case '2', 'r', 'R':
			e.SetTool(redact.Erase)
		case '3', 'w', 'W':
			e.SetTool(redact.Blur)
		case ']', '+':
			e.SetBrush(e.brush * 1.25)
		case '[', '-':
			e.SetBrush(e.brush / 1.25)
		default:
			return false
		}
		return true
	}
	return false
}
