// Command imgredactor removes personal data from screenshots: open a folder,
// pick a picture in the strip at the bottom and paint over what must go with
// the black pen, the blur brush or the eraser that brings the original back.
//
//	go run . [folder]
//
// A changed picture is saved over its file as soon as another one is opened,
// the folder is closed or the window is closed. Until then every change lives
// in memory only and can be undone or reset.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/asset"
	"github.com/worldiety/gift/asset/turbojpeg"
	backend "github.com/worldiety/gift/backend/ebiten"
	"github.com/worldiety/gift/font/inter"
	"github.com/worldiety/gift/ui"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: imgredactor [folder]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if err := run(flag.Arg(0)); err != nil {
		fmt.Fprintln(os.Stderr, "imgredactor:", err)
		os.Exit(1)
	}
}

func run(dir string) error {
	ui.SetDefaultFont(ui.MustFont(ui.FontQuery{Family: inter.Family}))
	if turbojpeg.Register() {
		asset.RegisterDecoder(asset.MIMEJPEG, turbojpeg.Decoder{})
	}

	var ed *Editor
	app := gift.New(gift.Options{Root: func(ctx *gift.Context) gift.View { return screen(ctx, ed) }})
	// The thumbnails of the strip. There is deliberately no disk cache: it
	// would keep small copies of the unredacted screenshots around.
	pipe := asset.NewPipeline(asset.Config{Deliver: app.Post, Sizes: []int{128, 256, 512}})
	ui.SetImagePipeline(pipe)
	ed = newEditor(app, pipe)
	ui.SetTheme(app, ui.DarkTheme())

	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		ed.OpenFolder(abs)
	}

	err := backend.Run(app, backend.Config{
		Title: "imgredactor", Width: 1440, Height: 920,
		DrawOnDemand: true,
		// A screenshot is uploaded in tiles; let a whole one land in one
		// frame instead of the few thumbnails a kiosk budgets for.
		Textures: backend.TextureConfig{UploadBytesPerFrame: 128 << 20, UploadsPerFrame: 1024, MaxBytes: 1 << 30},
	})
	// The window is closed: the picture being edited is saved like on any
	// other change of picture, and the process waits for the file.
	ed.Shutdown()
	pipe.Close()
	app.DrainPosts()
	return err
}
