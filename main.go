package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"scodeviewer/pkg/api"
	"scodeviewer/pkg/config"
	"scodeviewer/pkg/git"
)

//go:embed web/*
var embeddedFiles embed.FS

func main() {
	portFlag := flag.Int("port", 52480, "Port to listen on")
	pathFlag := flag.String("path", "", "Initial repository path to open")
	openFlag := flag.Bool("open", true, "Automatically open browser/app window")
	appFlag := flag.Bool("app", true, "Open in standalone app window mode (Edge/Chrome)")
	flag.Parse()

	// 1. Initialize configuration manager
	cfgMgr, err := config.NewManager()
	if err != nil {
		log.Fatalf("Failed to initialize config manager: %v", err)
	}

	// 2. Initialize Git service
	gitSvc := git.NewService()

	// 3. Check target path: flag or current working directory
	targetPath := *pathFlag
	if targetPath == "" {
		cwd, err := os.Getwd()
		if err == nil && gitSvc.IsGitRepo(cwd) {
			targetPath = cwd
		}
	}

	if targetPath != "" {
		absPath, err := filepath.Abs(targetPath)
		if err == nil && gitSvc.IsGitRepo(absPath) {
			cfgMgr.AddProject(absPath, true)
			fmt.Printf("📂 Открыт репозиторий: %s\n", absPath)
		}
	}

	// 4. Setup static FS
	staticFS, err := fs.Sub(embeddedFiles, "web")
	if err != nil {
		log.Fatalf("Failed to sub embedded web files: %v", err)
	}

	// 5. Setup HTTP Server
	server := api.NewServer(cfgMgr, gitSvc, staticFS)
	handler := server.Routes()

	// Find available port
	listener, port, err := getListener(*portFlag)
	if err != nil {
		log.Fatalf("Failed to bind port: %v", err)
	}

	appURL := fmt.Sprintf("http://localhost:%d", port)
	fmt.Println("==================================================")
	fmt.Printf("⚡ sCodeViewer запущен на %s\n", appURL)
	fmt.Println("🚀 Скорость работы: нативная (Go binary, <15MB RAM)")
	fmt.Println("Для выхода нажмите Ctrl+C")
	fmt.Println("==================================================")

	// 6. Auto-open window if enabled
	if *openFlag {
		go func() {
			time.Sleep(200 * time.Millisecond)
			openApp(appURL, *appFlag)
		}()
	}

	// 7. Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println("\nОстановка сервера sCodeViewer...")
		_ = listener.Close()
		os.Exit(0)
	}()

	httpServer := &http.Server{
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}
}

func getListener(preferredPort int) (net.Listener, int, error) {
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferredPort))
	if err == nil {
		return l, preferredPort, nil
	}
	// Fallback to random free port
	l, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	return l, port, nil
}

func openApp(url string, appMode bool) {
	if runtime.GOOS == "windows" {
		if appMode {
			// Try Edge standalone app window
			edgeCmd := exec.Command("cmd", "/c", "start", "msedge", fmt.Sprintf("--app=%s", url))
			if err := edgeCmd.Start(); err == nil {
				return
			}
			// Try Chrome standalone app window
			chromeCmd := exec.Command("cmd", "/c", "start", "chrome", fmt.Sprintf("--app=%s", url))
			if err := chromeCmd.Start(); err == nil {
				return
			}
		}
		// Fallback to default browser
		_ = exec.Command("cmd", "/c", "start", url).Start()
		return
	}

	if runtime.GOOS == "darwin" {
		_ = exec.Command("open", url).Start()
		return
	}

	_ = exec.Command("xdg-open", url).Start()
}
