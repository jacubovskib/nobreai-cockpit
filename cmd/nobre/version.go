package main

import (
	"fmt"

	"github.com/nobretech/nobreai-cockpit/internal/version"
)

// version mostra a versão do nobre e a do nobred em execução.
func (c *cli) version() error {
	fmt.Fprintf(c.out, "nobre %s\n", version.Version)
	var out struct{ Version string }
	if err := c.client().Do(ctx(), "GET", "/api/health", nil, &out); err != nil {
		fmt.Fprintf(c.out, "nobred: não respondeu em %s\n", c.addr())
		return nil
	}
	fmt.Fprintf(c.out, "nobred %s\n", out.Version)
	if out.Version != version.Version {
		fmt.Fprintln(c.out, "As versões diferem: reinicie o serviço (make restart ou rode o install.sh de novo).")
	}
	return nil
}
