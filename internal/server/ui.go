package server

import (
	"embed"
	"net/http"
	"strings"
)

//go:embed static/ui.html
var uiFS embed.FS

func (a *App) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/ui" && r.URL.Path != "/ui/" {
		http.NotFound(w, r)
		return
	}

	content, err := uiFS.ReadFile("static/ui.html")
	if err != nil {
		http.Error(w, "ui unavailable", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(strings.ReplaceAll(string(content), "{{CONFIG_PATH}}", a.configPath)))
}
