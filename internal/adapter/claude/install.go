package claude

import (
	"errors"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
)

// Install é um esboço: a Task 10 (setup automático) instala hooks e skill.
func (a *Adapter) Install() (adapter.InstallReport, error) {
	return adapter.InstallReport{}, errors.New("instalação automática do Claude Code ainda não disponível")
}
