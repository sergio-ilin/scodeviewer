package main

import (
	"flag"
	"os"
	"path/filepath"

	"gioui.org/app"
	"scodeviewer/pkg/config"
	"scodeviewer/pkg/git"
	"scodeviewer/pkg/gui"
)

func main() {
	pathFlag := flag.String("path", "", "Path to git repository")
	flag.Parse()

	targetPath := *pathFlag
	gitSvc := git.NewService()

	if targetPath == "" {
		cwd, err := os.Getwd()
		if err == nil && gitSvc.IsGitRepo(cwd) {
			targetPath = cwd
		}
	} else {
		abs, err := filepath.Abs(targetPath)
		if err == nil && gitSvc.IsGitRepo(abs) {
			targetPath = abs
		}
	}

	if targetPath == "" {
		if cfgMgr, err := config.NewManager(); err == nil {
			cfg := cfgMgr.GetConfig()
			if cfg.ActivePath != "" && gitSvc.IsGitRepo(cfg.ActivePath) {
				targetPath = cfg.ActivePath
			}
		}
	}

	appInstance := gui.NewApp(targetPath)

	go func() {
		if err := appInstance.Run(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}()

	app.Main()
}
