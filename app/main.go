package main

import (
	"context"
	"embed"
	"fmt"
	"os"
	"time"

	"shogun2sync/internal/applog"
	"shogun2sync/internal/selfcheck"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Headless install/CI probe: prove the installed tree can sync before
	// anyone has to open a window. Exit 0 only when every fatal check passes.
	if wantsSelfCheck(os.Args[1:]) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		report := selfcheck.Run(ctx)
		fmt.Fprint(os.Stdout, selfcheck.Format(report))
		if !report.OK {
			os.Exit(1)
		}
		return
	}

	// Everything the app logs goes to a file as well as the terminal, so a
	// player who hits trouble has something concrete to send us. Without
	// this, a GUI app launched from a desktop icon drops its log output on
	// the floor.
	closeLog := applog.Init()
	defer closeLog()

	app := NewApp()

	err := wails.Run(&options.App{
		Title:     "Shogun 2 Save Sync",
		Width:     1024,
		Height:    768,
		MinWidth:  720,
		MinHeight: 560,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		OnStartup:        app.startup,
		// Two copies running at once could each try to move the save folder
		// and link it somewhere different. Focus the existing window instead.
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "com.github.jalsarraf0.shogun2sync",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				wruntime.WindowUnminimise(app.ctx)
				wruntime.WindowShow(app.ctx)
			},
		},
		Linux: &linux.Options{
			// Must match the .desktop file's basename, or the window gets a
			// generic icon and doesn't group with its launcher.
			ProgramName: "shogun2sync",
			// Wails only defaults this to Never while options.Linux is nil.
			// Supplying this struct at all silently opts into hardware
			// acceleration, which crashes the webview on Wayland with
			// "Error 71 (Protocol error)" — wails#2977. Must stay explicit.
			WebviewGpuPolicy: linux.WebviewGpuPolicyNever,
		},
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		applog.Printf("fatal: %v", err)
		closeLog()
		os.Exit(1)
	}
}

func wantsSelfCheck(args []string) bool {
	for _, a := range args {
		switch a {
		case "--self-check", "-self-check", "/self-check":
			return true
		}
	}
	return false
}
