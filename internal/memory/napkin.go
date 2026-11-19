package memory

import (
	"bufio"
	"io"
	"regexp"
	"strings"
)

// NapkinItem é um item lido de um .claude/napkin.md.
type NapkinItem struct {
	Category    Category
	Rule, Do    string
	Date        string // "AAAA-MM-DD" ou ""
	NeedsReview bool   // cortado ou sem ação
}

const (
	maxRule = 300
	maxDo   = 400
)

var (
	itemStart = regexp.MustCompile(`^\s{0,3}(?:\d+[.)]|[-*])\s+(\S.*)$`)
	dateHead  = regexp.MustCompile(`^\[(\d{4}-\d{2}-\d{2})\]\s*`)
	doPrefix  = regexp.MustCompile(`(?i)^do instead:\s*`)
)

// sectionCategory mapeia o título da seção; ignore marca seções a pular.
func sectionCategory(title string) (cat Category, ignore bool) {
	n := Normalize(title)
	switch {
	case strings.Contains(n, "curation") || strings.Contains(n, "curadoria"):
		return "", true
	case strings.Contains(n, "execu") || strings.Contains(n, "valid"):
		return CategoryExecucao, false
	case strings.Contains(n, "shell") || strings.Contains(n, "command") || strings.Contains(n, "comando"):
		return CategoryShell, false
	case strings.Contains(n, "diretiv") || strings.Contains(n, "directive"):
		return CategoryDiretiva, false
	}
	return CategoryDominio, false
}

type napkinDraft struct {
	cat       Category
	head, do  []string
	inDo, has bool
}

// ParseNapkin lê o formato do napkin e devolve os itens, tolerando variações
// (sem data, sem negrito, marcador "-", ação em várias linhas).
func ParseNapkin(r io.Reader) ([]NapkinItem, error) {
	var out []NapkinItem
	var cur *napkinDraft
	ignore := false
	cat := CategoryDominio

	flush := func() {
		if cur == nil {
			return
		}
		out = append(out, cur.build())
		cur = nil
	}

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "#") {
			flush()
			if strings.HasPrefix(trim, "##") {
				title := strings.TrimSpace(strings.TrimLeft(trim, "#"))
				cat, ignore = sectionCategory(title)
			}
			continue
		}
		if ignore || trim == "" {
			continue
		}
		if m := itemStart.FindStringSubmatch(line); m != nil && !doPrefix.MatchString(m[1]) {
			flush()
			cur = &napkinDraft{cat: cat, head: []string{m[1]}}
			continue
		}
		if cur == nil {
			continue
		}
		if doPrefix.MatchString(trim) {
			cur.inDo = true
			cur.do = append(cur.do, doPrefix.ReplaceAllString(trim, ""))
		} else if cur.inDo {
			cur.do = append(cur.do, trim)
		} else {
			cur.head = append(cur.head, trim)
		}
	}
	flush()
	return out, sc.Err()
}

func (d *napkinDraft) build() NapkinItem {
	head := strings.TrimSpace(strings.ReplaceAll(strings.Join(d.head, " "), "**", ""))
	it := NapkinItem{Category: d.cat}
	if m := dateHead.FindStringSubmatch(head); m != nil {
		it.Date = m[1]
		head = strings.TrimSpace(head[len(m[0]):])
	}
	var cutR, cutD bool
	it.Rule, cutR = CleanText(head, maxRule)
	it.Do, cutD = CleanText(strings.Join(d.do, " "), maxDo)
	it.NeedsReview = cutR || cutD || it.Do == ""
	return it
}
