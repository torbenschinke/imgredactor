package main

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/asset"
	"github.com/worldiety/gift/font/inter"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/icon/outline"
	"github.com/worldiety/gift/ui"
	"github.com/worldiety/gift/ui/filepicker"
	"github.com/worldiety/gift/ui/gesture"

	"github.com/torbenschinke/imgredactor/internal/redact"
)

// The size of a thumbnail in the strip.
const (
	thumbW = 148
	thumbH = 92
)

var (
	hair     = ui.Border{Width: 1, Color: ui.ColorSeparator}
	unsaved  = ui.RGB(255, 159, 10)
	bold     = ui.MustFont(ui.FontQuery{Family: inter.Family, Weight: ui.WeightBold})
	shortcut = "←/→ Bild wechseln · 1/2/3 Werkzeug · [ ] Pinselgröße · ⌘Z Rückgängig · ⇧+Klick gerade Linie"
)

// screen is the whole window: the toolbar, the picture and the strip, with
// the folder picker as a sheet over it.
func screen(_ *gift.Context, e *Editor) gift.View {
	body := []gift.View{toolbar(e), ui.Divider()}
	if e.dir == "" {
		body = append(body, emptyState(e))
	} else {
		body = append(body, workspace(e)...)
	}

	picker := filepicker.New(os.DirFS("/"),
		func(paths []string) {
			e.picking = false
			e.OpenFolder("/" + strings.TrimPrefix(path.Clean(paths[0]), "."))
		},
		func() { e.picking = false; e.changed() },
	).Mode(filepicker.Directory).
		Extensions(redact.Extensions...).
		Start(pickerStart(e)).
		Title("Ordner mit Screenshots wählen").
		RootLabel("Computer").
		Key("picker")

	content := ui.Sheet(ui.VStack(body...), picker).
		Height(0.82).
		Presented(e.picking).
		OnDismiss(func() { e.picking = false; e.changed() })

	return ui.Window(
		gesture.On(
			ui.Toast(content, e.toast).
				Icon(outline.ExclamationCircle).
				Edge(ui.ToastBottom).
				Presented(e.toastOn).
				OnDismiss(func() { e.toastOn = false; e.changed() }),
		).OnKey(e.Key).KeyFallback(true),
	)
}

// pickerStart is where the folder picker opens, as an fs.FS path below the
// root: the open folder, else the desktop, where macOS puts screenshots.
func pickerStart(e *Editor) string {
	dir := e.dir
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, "Desktop")
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			dir = home
		}
	}
	p := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(dir)), "/")
	if p == "" || !fs.ValidPath(p) {
		return "."
	}
	return p
}

// label is the face of a toolbar button: an icon and a word, dimmed when the
// button is disabled.
func label(sym ui.Symbol, text string, disabled bool) gift.View {
	fg := ui.ColorLabel
	if disabled {
		fg = ui.ColorSecondaryLabel
	}
	return ui.HStack(
		ui.Icon(sym).Size(16).Foreground(fg),
		ui.Text(text).FontSize(13).Foreground(fg),
	).Gap(6).Align(geom.Center)
}

func button(sym ui.Symbol, text string, disabled bool, action func()) ui.ButtonView {
	return ui.Button(label(sym, text, disabled), action).
		CornerRadius(8).
		PaddingInsets(geom.Insets{Top: 6, Right: 12, Bottom: 6, Left: 10}).
		MinHeight(34).
		Disabled(disabled).
		Label(text)
}

// toolbar is the row of commands and tools at the top.
func toolbar(e *Editor) gift.View {
	noDoc := e.doc == nil
	return ui.HStack(
		button(outline.FolderOpen, "Ordner öffnen", false, func() { e.picking = true; e.changed() }),
		button(outline.Close, "Ordner schließen", e.dir == "", e.CloseFolder),
		ui.Box().Frame(12, 1),
		ui.SegmentedControl(int(e.tool), []string{"Schwärzen", "Radierer", "Weichzeichnen"},
			func(i int) { e.SetTool(redact.Tool(i)) }).
			FontSize(13).
			MinWidth(330).
			Label("Werkzeug"),
		ui.Box().Frame(12, 1),
		ui.Text("Pinsel").FontSize(13).Foreground(ui.ColorSecondaryLabel),
		ui.Slider(float64(e.brush), func(v float64) { e.SetBrush(float32(v)) }).
			Range(minBrush, maxBrush).
			Step(1).
			Frame(200, geom.Unbounded()).
			Label("Pinselgröße"),
		ui.Text(fmt.Sprintf("%.0f px", e.brush)).FontSize(13).MinWidth(48),
		ui.Spacer(),
		button(outline.Undo, "Rückgängig", noDoc || !e.doc.CanUndo(), e.Undo),
		button(outline.Refresh, "Zurücksetzen", noDoc || !e.doc.Modified(), e.Reset),
	).Gap(8).Align(geom.Center).
		PaddingInsets(geom.Insets{Top: 8, Right: 12, Bottom: 8, Left: 12}).
		Background(ui.ColorSurface)
}

// emptyState is shown while no folder is open.
func emptyState(e *Editor) gift.View {
	return ui.ZStack(ui.VStack(
		ui.Icon(outline.FolderOpen).Size(56).Foreground(ui.ColorSecondaryLabel),
		ui.Text("Kein Ordner geöffnet").Font(bold).FontSize(22),
		ui.Text("Öffne einen Ordner mit Screenshots (PNG oder JPG), um personenbezogene Daten zu schwärzen, "+
			"wegzuradieren oder weichzuzeichnen. Geändert wird beim Wechsel des Bildes automatisch gespeichert.").
			Foreground(ui.ColorSecondaryLabel).Align(ui.AlignCenter).MaxWidth(520),
		ui.Button(ui.Text("Ordner öffnen …").Foreground(ui.ColorOnAccent), func() { e.picking = true; e.changed() }).
			Style(ui.ButtonStyle{Background: ui.ColorAccent, CornerRadius: 10}).
			PressedStyle(ui.ButtonStyle{Background: ui.Fade(ui.ColorAccent, 0.8), CornerRadius: 10}).
			PaddingInsets(geom.Insets{Top: 10, Right: 20, Bottom: 10, Left: 20}).
			Lift(true),
		ui.Text(shortcut).FontSize(12).Foreground(ui.ColorSecondaryLabel),
	).Gap(16).Align(geom.Center)).Align(geom.Center).Flex(1)
}

// workspace is the picture, the status line and the strip.
func workspace(e *Editor) []gift.View {
	var over gift.View
	switch {
	case len(e.files) == 0:
		over = ui.Text("Keine PNG- oder JPG-Dateien in diesem Ordner.").Foreground(ui.ColorSecondaryLabel)
	case e.loadErr != "":
		over = ui.Text(e.loadErr).Foreground(ui.ColorDanger)
	case e.loading:
		over = ui.ProgressRing(0).Indeterminate().Size(36)
	}
	stage := []gift.View{canvas(e)}
	if over != nil {
		stage = append(stage, over)
	}
	return []gift.View{
		ui.ZStack(stage...).Align(geom.Center).Background(ui.ColorBackground).Flex(1),
		statusLine(e),
		ui.Divider(),
		gift.Memo("strip", stripProps{e: e, dir: e.dir, files: len(e.files), cur: e.cur, saves: e.saves,
			modified: e.doc != nil && e.doc.Modified()}, filmstrip),
	}
}

// statusLine names the picture and says whether it has unsaved changes.
func statusLine(e *Editor) gift.View {
	kids := []gift.View{}
	if e.cur >= 0 {
		kids = append(kids, ui.Text(filepath.Base(e.files[e.cur])).Font(bold).FontSize(13).
			MaxLines(1).Truncation(ui.TruncateMiddle).MaxWidth(420))
	}
	if e.doc != nil {
		w, h := e.doc.Size()
		kids = append(kids, ui.Text(fmt.Sprintf("%d × %d", w, h)).FontSize(13).Foreground(ui.ColorSecondaryLabel))
		if e.doc.Modified() {
			kids = append(kids, ui.Text("● Ungespeicherte Änderungen – werden beim Bildwechsel gespeichert").
				FontSize(13).Foreground(unsaved))
		}
	}
	kids = append(kids, ui.Spacer(),
		ui.Text(e.status).FontSize(13).Foreground(ui.ColorSecondaryLabel).MaxLines(1).Truncation(ui.TruncateMiddle))
	if len(e.files) > 0 {
		kids = append(kids, ui.Text(fmt.Sprintf("%d / %d", e.cur+1, len(e.files))).FontSize(13).MinWidth(56).
			Align(ui.AlignTrailing))
	}
	return ui.HStack(kids...).Gap(12).Align(geom.Center).
		PaddingInsets(geom.Insets{Top: 6, Right: 14, Bottom: 6, Left: 14}).
		Background(ui.ColorSurface)
}

// stripProps are the inputs of the strip; it is not rebuilt while they stay
// the same, which is every stroke and every move of the slider.
type stripProps struct {
	e        *Editor
	dir      string
	files    int
	cur      int
	saves    int
	modified bool
}

// filmstrip is the row of thumbnails at the bottom. A click saves the picture
// being edited and opens the one clicked.
func filmstrip(_ *gift.Context, p stripProps) gift.View {
	e := p.e
	tiles := make([]gift.View, len(e.files))
	for i, file := range e.files {
		sel := i == e.cur
		thumb := []gift.View{
			ui.Image(asset.File(file)).Fit(ui.FitContain).Frame(thumbW, thumbH).
				Placeholder(ui.Fade(ui.ColorLabel, 0.06)).CornerRadius(4),
		}
		if sel && p.modified {
			thumb = append(thumb, ui.Box().Frame(12, 12).CornerRadius(6).Background(unsaved).
				Border(ui.Border{Width: 2, Color: ui.ColorSurface}))
		}
		face := ui.ButtonStyle{Background: ui.ColorControl, Border: hair, CornerRadius: 8}
		if sel {
			face.Border = ui.Border{Width: 3, Color: ui.ColorAccent}
		}
		name := filepath.Base(file)
		tile := ui.Button(ui.VStack(
			ui.ZStack(thumb...).Align(geom.TopTrailing),
			ui.Text(name).FontSize(11).MaxLines(1).Truncation(ui.TruncateMiddle).MaxWidth(thumbW).
				Foreground(ui.ColorSecondaryLabel),
		).Gap(4).Align(geom.Center), func() { e.Select(i) }).
			Style(face).
			HoverStyle(ui.ButtonStyle{Background: ui.ColorControlHover, Border: face.Border, CornerRadius: 8}).
			Padding(6).
			Label(name).
			// A saved file gets a new key, so its thumbnail is loaded again.
			Key(fmt.Sprintf("%s#%d", file, e.thumbRev[file]))
		if sel {
			tiles[i] = reveal{e: e, child: tile}
		} else {
			tiles[i] = tile
		}
	}
	return ui.HScroll(tiles...).Gap(8).Padding(8).Background(ui.ColorSurface).Key("strip-" + p.dir)
}

var revealType = gift.RegisterType("imgredactor.reveal")

// reveal is the thumbnail of the current picture: after the layout that
// follows a change of picture it scrolls the strip to show it.
type reveal struct {
	e     *Editor
	child gift.View
}

func (reveal) ViewType() gift.TypeID { return revealType }

func (r reveal) Build(*gift.BuildContext) gift.Element {
	return gift.Element{Layouter: revealLayout{r.e}, Children: []gift.View{r.child}}
}

type revealLayout struct{ e *Editor }

func (l revealLayout) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	s := ctx.Measure(0, c)
	ctx.Place(0, geom.Point{})
	if l.e.reveal {
		l.e.reveal = false
		node, app := ctx.Node(), l.e.app
		app.Post(func() { app.ScrollIntoView(node) })
	}
	return s
}
