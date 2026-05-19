package server

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/YUYUEYUER/prompt-guard/internal/config"
)

const backupDirName = ".prompt-guard-backups"

type backupInfo struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	Reason    string `json:"reason,omitempty"`
}

func (a *App) previewConfig(content []byte) (configPreviewResponse, error) {
	current, err := os.ReadFile(a.configPath)
	if err != nil {
		return configPreviewResponse{}, fmt.Errorf("read current config: %w", err)
	}
	currentCfg, err := config.Load(a.configPath)
	if err != nil {
		return configPreviewResponse{}, fmt.Errorf("load current config: %w", err)
	}
	proposedCfg, err := a.parseConfigContent(content)
	if err != nil {
		return configPreviewResponse{}, err
	}
	return configPreviewResponse{
		ConfigPath:      a.configPath,
		CurrentSummary:  summarizeLoadedConfig(currentCfg),
		ProposedSummary: summarizeLoadedConfig(proposedCfg),
		Diff:            buildLineDiff(string(current), string(content)),
	}, nil
}

func (a *App) saveConfigLocked(content []byte, reason string) (*backupInfo, error) {
	current, err := os.ReadFile(a.configPath)
	if err != nil {
		return nil, fmt.Errorf("read current config: %w", err)
	}
	if _, err := a.parseConfigContent(content); err != nil {
		return nil, err
	}
	if contentEqual(current, content) {
		if err := a.reloadLocked(); err != nil {
			return nil, err
		}
		return nil, nil
	}

	backup, err := a.createBackupLocked(current, reason)
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(a.configPath, content, 0o600); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}
	if err := a.reloadLocked(); err != nil {
		_ = os.WriteFile(a.configPath, current, 0o600)
		_ = a.reloadLocked()
		return nil, err
	}
	return &backup, nil
}

func (a *App) parseConfigContent(content []byte) (*config.Config, error) {
	dir := filepath.Dir(a.configPath)
	tempFile, err := os.CreateTemp(dir, "prompt-guard-config-preview-*.yaml")
	if err != nil {
		return nil, fmt.Errorf("create temp config: %w", err)
	}
	tempPath := tempFile.Name()
	_ = tempFile.Close()
	defer os.Remove(tempPath)

	if err := os.WriteFile(tempPath, content, 0o600); err != nil {
		return nil, fmt.Errorf("write temp config: %w", err)
	}
	cfg, err := config.Load(tempPath)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func (a *App) createBackupLocked(content []byte, reason string) (backupInfo, error) {
	dir := a.backupDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return backupInfo{}, fmt.Errorf("create backup dir: %w", err)
	}

	id := backupID()
	reasonSlug := sanitizeBackupReason(reason)
	filename := id + ".yaml"
	if reasonSlug != "" {
		filename = id + "__" + reasonSlug + ".yaml"
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return backupInfo{}, fmt.Errorf("write backup: %w", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return backupInfo{}, fmt.Errorf("stat backup: %w", err)
	}
	return backupInfo{
		ID:        strings.TrimSuffix(filename, filepath.Ext(filename)),
		CreatedAt: info.ModTime().UTC().Format(time.RFC3339),
		Path:      path,
		SizeBytes: info.Size(),
		Reason:    reasonSlug,
	}, nil
}

func (a *App) readBackup(id string) ([]byte, error) {
	if strings.TrimSpace(id) == "" {
		return nil, errors.New("backup_id is required")
	}
	path := filepath.Join(a.backupDir(), id+".yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("backup %q not found", id)
		}
		return nil, fmt.Errorf("read backup: %w", err)
	}
	return content, nil
}

func (a *App) listBackups(limit int) ([]backupInfo, error) {
	dir := a.backupDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read backup dir: %w", err)
	}

	backups := make([]backupInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".yaml")
		reason := ""
		if idx := strings.Index(id, "__"); idx >= 0 && idx+2 < len(id) {
			reason = id[idx+2:]
		}
		backups = append(backups, backupInfo{
			ID:        id,
			CreatedAt: info.ModTime().UTC().Format(time.RFC3339),
			Path:      filepath.Join(dir, entry.Name()),
			SizeBytes: info.Size(),
			Reason:    reason,
		})
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt > backups[j].CreatedAt
	})
	if limit > 0 && len(backups) > limit {
		backups = backups[:limit]
	}
	return backups, nil
}

func (a *App) backupDir() string {
	return filepath.Join(filepath.Dir(a.configPath), backupDirName)
}

func backupID() string {
	return time.Now().UTC().Format("20060102T150405.000000000Z")
}

func sanitizeBackupReason(reason string) string {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range reason {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	result := strings.Trim(b.String(), "-")
	if len(result) > 32 {
		result = result[:32]
	}
	return result
}

func buildLineDiff(current string, proposed string) string {
	currentLines := splitLines(current)
	proposedLines := splitLines(proposed)
	ops := diffLines(currentLines, proposedLines)

	var out strings.Builder
	out.WriteString("--- current\n")
	out.WriteString("+++ proposed\n")
	for _, op := range ops {
		out.WriteString(op.prefix)
		out.WriteString(op.line)
		out.WriteByte('\n')
	}
	return out.String()
}

type diffOp struct {
	prefix string
	line   string
}

func splitLines(content string) []string {
	trimmed := strings.ReplaceAll(content, "\r\n", "\n")
	trimmed = strings.TrimRight(trimmed, "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func diffLines(a []string, b []string) []diffOp {
	rows := len(a) + 1
	cols := len(b) + 1
	lcs := make([][]int, rows)
	for i := range lcs {
		lcs[i] = make([]int, cols)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}

	var ops []diffOp
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{prefix: " ", line: a[i]})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{prefix: "-", line: a[i]})
			i++
		default:
			ops = append(ops, diffOp{prefix: "+", line: b[j]})
			j++
		}
	}
	for ; i < len(a); i++ {
		ops = append(ops, diffOp{prefix: "-", line: a[i]})
	}
	for ; j < len(b); j++ {
		ops = append(ops, diffOp{prefix: "+", line: b[j]})
	}

	return squashEqualRuns(ops)
}

func squashEqualRuns(ops []diffOp) []diffOp {
	if len(ops) == 0 {
		return nil
	}
	var out []diffOp
	equalBuffer := make([]diffOp, 0, 6)
	flushEquals := func(forceAll bool) {
		if len(equalBuffer) == 0 {
			return
		}
		switch {
		case forceAll || len(equalBuffer) <= 6:
			out = append(out, equalBuffer...)
		default:
			out = append(out, equalBuffer[:3]...)
			out = append(out, diffOp{prefix: "@", line: fmt.Sprintf("... %d unchanged lines ...", len(equalBuffer)-6)})
			out = append(out, equalBuffer[len(equalBuffer)-3:]...)
		}
		equalBuffer = equalBuffer[:0]
	}

	hasChangeAhead := func(start int) bool {
		for i := start; i < len(ops); i++ {
			if ops[i].prefix != " " {
				return true
			}
		}
		return false
	}

	for idx, op := range ops {
		if op.prefix == " " {
			equalBuffer = append(equalBuffer, op)
			if !hasChangeAhead(idx + 1) {
				flushEquals(false)
			}
			continue
		}
		flushEquals(false)
		out = append(out, op)
	}
	flushEquals(true)
	return out
}

func contentEqual(a []byte, b []byte) bool {
	return bytes.Equal(bytes.TrimSpace(a), bytes.TrimSpace(b))
}
