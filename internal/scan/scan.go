// Package scan faz o levantamento mecânico e só de leitura do histórico de um
// repositório: branches com trabalho fora da base e planos com tarefas abertas.
package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
)

const (
	window       = 30 * 24 * time.Hour
	recentWindow = 14 * 24 * time.Hour
	maxBranches  = 20
	maxPlanDepth = 4
)

// Branch é um branch local com trabalho fora da base.
type Branch struct {
	Name       string    `json:"name"`
	Subject    string    `json:"subject"`
	Worktree   string    `json:"worktree"`
	LastCommit time.Time `json:"last_commit"`
	Ahead      int       `json:"ahead"`
	Recent     bool      `json:"recent"` // último commit nos últimos 14 dias
}

// Plan é um arquivo de plano em docs/ com tarefas abertas.
type Plan struct {
	Path     string    `json:"path"`
	Open     int       `json:"open"`
	Done     int       `json:"done"`
	Modified time.Time `json:"modified"`
}

// Result é o levantamento de um repositório.
type Result struct {
	Repo     string   `json:"repo"`
	Base     string   `json:"base"`
	Branches []Branch `json:"branches"`
	Plans    []Plan   `json:"plans"`
}

// Run levanta repo em relação a base. Todo git é só de leitura.
func Run(repo, base string, now time.Time) (Result, error) {
	res := Result{Repo: repo, Base: base, Branches: []Branch{}, Plans: []Plan{}}
	if base != "" && !strings.HasPrefix(base, "-") {
		bs, err := branches(repo, base, now)
		if err != nil {
			return res, err
		}
		res.Branches = bs
	}
	res.Plans = plans(repo, now)
	return res, nil
}

func branches(repo, base string, now time.Time) ([]Branch, error) {
	out, err := gitinfo.ReadOnly(repo, "for-each-ref", "refs/heads",
		"--format=%(refname:short)%09%(committerdate:unix)%09%(subject)")
	if err != nil {
		return nil, err
	}
	wts := worktrees(repo)
	short := strings.TrimPrefix(base, "origin/")
	since := now.Add(-window)
	bs := []Branch{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\t", 3)
		if len(f) < 3 {
			continue
		}
		name := f[0]
		if name == base || name == short || strings.HasPrefix(name, "backup/") {
			continue
		}
		unix, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil {
			continue
		}
		last := time.Unix(unix, 0)
		if last.Before(since) {
			continue
		}
		cnt, err := gitinfo.ReadOnly(repo, "rev-list", "--count", base+"..refs/heads/"+name)
		if err != nil {
			continue
		}
		ahead, _ := strconv.Atoi(strings.TrimSpace(cnt))
		if ahead == 0 {
			continue
		}
		bs = append(bs, Branch{Name: name, Subject: f[2], Worktree: wts[name], LastCommit: last,
			Ahead: ahead, Recent: !last.Before(now.Add(-recentWindow))})
	}
	sort.SliceStable(bs, func(i, j int) bool { return bs[i].LastCommit.After(bs[j].LastCommit) })
	if len(bs) > maxBranches {
		bs = bs[:maxBranches]
	}
	return bs, nil
}

// worktrees mapeia branch -> caminho do worktree em que está.
func worktrees(repo string) map[string]string {
	m := map[string]string{}
	out, err := gitinfo.ReadOnly(repo, "worktree", "list", "--porcelain")
	if err != nil {
		return m
	}
	var path string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "branch refs/heads/"):
			m[strings.TrimPrefix(line, "branch refs/heads/")] = path
		}
	}
	return m
}

func plans(repo string, now time.Time) []Plan {
	docs := filepath.Join(repo, "docs")
	since := now.Add(-window)
	ps := []Plan{}
	_ = filepath.WalkDir(docs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(docs, p)
		if d.IsDir() {
			if rel != "." && strings.Count(rel, string(filepath.Separator)) >= maxPlanDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(strings.ToLower(d.Name()), ".md") ||
			!strings.Contains(strings.ToLower(d.Name()), "plan") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(since) {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		var open, done int
		for _, l := range strings.Split(string(b), "\n") {
			l = strings.TrimSpace(l)
			switch {
			case strings.HasPrefix(l, "- [ ]"):
				open++
			case strings.HasPrefix(l, "- [x]"), strings.HasPrefix(l, "- [X]"):
				done++
			}
		}
		if open == 0 {
			return nil
		}
		r, _ := filepath.Rel(repo, p)
		ps = append(ps, Plan{Path: filepath.ToSlash(r), Open: open, Done: done, Modified: info.ModTime()})
		return nil
	})
	sort.SliceStable(ps, func(i, j int) bool { return ps[i].Modified.After(ps[j].Modified) })
	return ps
}
