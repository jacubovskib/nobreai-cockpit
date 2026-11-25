package domain

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// CardType é o tipo do card.
type CardType string

const (
	TypeEpico    CardType = "epico"
	TypeHistoria CardType = "historia"
	TypeTarefa   CardType = "tarefa"
	TypeBug      CardType = "bug"
)

// CardTypes lista os tipos na ordem de exibição.
var CardTypes = []CardType{TypeEpico, TypeHistoria, TypeTarefa, TypeBug}

// ValidType diz se t é um tipo conhecido.
func ValidType(t CardType) bool {
	for _, v := range CardTypes {
		if v == t {
			return true
		}
	}
	return false
}

// TypeLabel é o nome do tipo na interface.
func TypeLabel(t CardType) string {
	switch t {
	case TypeEpico:
		return "Épico"
	case TypeHistoria:
		return "História"
	case TypeTarefa:
		return "Tarefa"
	case TypeBug:
		return "Bug"
	}
	return string(t)
}

// Priority é a prioridade do card.
type Priority string

const (
	PrioMaisAlta  Priority = "mais_alta"
	PrioAlta      Priority = "alta"
	PrioMedia     Priority = "media"
	PrioBaixa     Priority = "baixa"
	PrioMaisBaixa Priority = "mais_baixa"
)

var priorities = []Priority{PrioMaisAlta, PrioAlta, PrioMedia, PrioBaixa, PrioMaisBaixa}

// ValidPriority diz se p é uma prioridade conhecida.
func ValidPriority(p Priority) bool {
	for _, v := range priorities {
		if v == p {
			return true
		}
	}
	return false
}

// PriorityLabel é o nome da prioridade na interface.
func PriorityLabel(p Priority) string {
	switch p {
	case PrioMaisAlta:
		return "Mais alta"
	case PrioAlta:
		return "Alta"
	case PrioMedia:
		return "Média"
	case PrioBaixa:
		return "Baixa"
	case PrioMaisBaixa:
		return "Mais baixa"
	}
	return string(p)
}

// maxLabelRunes é o tamanho máximo de uma label.
const maxLabelRunes = 50

// NormalizeLabels apara as pontas, descarta duplicadas (mantendo a ordem) e
// rejeita labels vazias, com espaço interno ou longas demais.
func NormalizeLabels(in []string) ([]string, error) {
	if in == nil {
		return nil, nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		l := strings.TrimSpace(raw)
		if l == "" {
			return nil, errors.New("label vazia")
		}
		if strings.IndexFunc(l, unicode.IsSpace) >= 0 {
			return nil, fmt.Errorf("label sem espaços: %q", l)
		}
		if strings.IndexFunc(l, func(r rune) bool { return unicode.IsControl(r) || r == ',' }) >= 0 {
			return nil, fmt.Errorf("label sem vírgula nem caractere de controle: %q", l)
		}
		if utf8.RuneCountInString(l) > maxLabelRunes {
			return nil, fmt.Errorf("label passa de %d caracteres: %q", maxLabelRunes, l)
		}
		if seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	return out, nil
}

// ValidPoints valida a estimativa; nil significa sem estimativa.
func ValidPoints(p *float64) error {
	if p != nil && *p < 0 {
		return errors.New("points não pode ser negativo")
	}
	return nil
}

// HierarchyCheck reúne os fatos para validar tipo, prioridade e pai de um card.
// OldType é o tipo antes da mudança; Parent é o card pai escolhido (nil = sem
// pai); ChildCount, quantas filhas o card tem; Live, se há sessão ao vivo; New,
// se o card está sendo criado.
type HierarchyCheck struct {
	Card       Card
	OldType    CardType
	Parent     *Card
	ChildCount int
	Live       bool
	New        bool
}

// CheckHierarchy valida as regras da hierarquia de cards (épico > filhas).
func CheckHierarchy(h HierarchyCheck) error {
	if !ValidType(h.Card.Type) {
		return errors.New("tipo inválido (use epico, historia, tarefa ou bug)")
	}
	if !ValidPriority(h.Card.Priority) {
		return errors.New("prioridade inválida (use mais_alta, alta, media, baixa ou mais_baixa)")
	}
	becomingEpic := h.Card.Type == TypeEpico && h.OldType != TypeEpico
	if h.Parent != nil {
		if h.Parent.ID == h.Card.ID {
			return errors.New("um card não pode ser pai de si mesmo")
		}
		if h.Parent.Type != TypeEpico {
			return errors.New("o pai precisa ser um épico")
		}
		if h.Card.Type == TypeEpico {
			if becomingEpic && !h.New {
				return errors.New("tire o card do épico antes")
			}
			return errors.New("um épico não pode ter pai")
		}
	}
	if h.OldType == TypeEpico && h.Card.Type != TypeEpico && h.ChildCount > 0 {
		return fmt.Errorf("remova as %d filhas do épico antes", h.ChildCount)
	}
	if becomingEpic && !h.New && h.Live {
		return errors.New("o card tem uma sessão ao vivo; encerre a sessão antes de transformá-lo em épico")
	}
	if h.OldType == TypeEpico && h.Card.Type != TypeEpico && !h.New && h.Live {
		return errors.New("o épico tem uma sessão ao vivo; encerre a sessão antes de mudar o tipo")
	}
	return nil
}

var errEpicFollowsChildren = errors.New("a coluna do épico segue as filhas")

func allIn(children []Column, col Column) bool {
	for _, c := range children {
		if c != col {
			return false
		}
	}
	return true
}

// EpicColumn calcula a coluna de um épico a partir das filhas. Um épico já
// Concluído só sai dessa coluna se alguma filha for reaberta.
func EpicColumn(cur Column, children []Column) Column {
	switch {
	case cur == Concluido && allIn(children, Concluido):
		return Concluido
	case len(children) == 0, allIn(children, Backlog):
		return Backlog
	case allIn(children, Concluido):
		return Revisao
	}
	return EmAndamento
}

// EpicManualMove valida a mudança manual de coluna de um épico: só dá para
// concluir com todas as filhas concluídas (ou sem filhas) e reabrir um épico
// concluído, que volta para onde as filhas mandam.
func EpicManualMove(cur, to Column, children []Column) (Column, error) {
	if cur == Concluido && to != Concluido {
		return EpicColumn(Backlog, children), nil
	}
	if to == Concluido && allIn(children, Concluido) {
		return Concluido, nil
	}
	return cur, errEpicFollowsChildren
}

// WaitingInput são os fatos para decidir se um card espera por você.
type WaitingInput struct {
	Column            Column
	OpenQuestions     int
	HandoffIncomplete bool
	Ready             bool
	PauseReason       string
	Signal            Signal
}

// WaitingOnYou diz se o card espera uma ação sua e o motivo, na ordem de
// precedência: pergunta aberta, revisão, handoff incompleto, limite de custo,
// pronto para retomar.
func WaitingOnYou(in WaitingInput) (bool, string) {
	switch {
	case in.OpenQuestions > 0 && in.Column != Concluido:
		return true, "pergunta aberta"
	case in.Column == Revisao:
		return true, "revisão"
	case in.Column == Pausado && in.HandoffIncomplete && !in.Ready:
		return true, "handoff incompleto"
	case in.PauseReason == PauseReasonLimit:
		return true, "limite de custo"
	case in.Signal == SignalProntoParaRetomar:
		return true, "pronto para retomar"
	}
	return false, ""
}
