// Comando nobred: daemon local do NobreAI Cockpit (API HTTP + stream).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/adapter"
	"github.com/nobretech/nobreai-cockpit/internal/adapter/claude"
	"github.com/nobretech/nobreai-cockpit/internal/api"
	"github.com/nobretech/nobreai-cockpit/internal/board"
	"github.com/nobretech/nobreai-cockpit/internal/demo"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/integration"
	"github.com/nobretech/nobreai-cockpit/internal/launch"
	"github.com/nobretech/nobreai-cockpit/internal/pty"
	"github.com/nobretech/nobreai-cockpit/internal/resources"
	"github.com/nobretech/nobreai-cockpit/internal/rtk"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

const defaultAddr = "127.0.0.1:7420"

func main() {
	dbPath := flag.String("db", "", "caminho do banco SQLite (padrão: "+store.DefaultPath()+")")
	seedDemo := flag.Bool("seed-demo", false, "carrega os dados de demonstração (sem --db usa "+store.DefaultDemoPath()+", nunca o banco real)")
	flag.Parse()
	path, err := resolveDBPath(*dbPath, *seedDemo)
	if err != nil {
		log.Fatalf("nobred: %v", err)
	}
	if err := run(path, *seedDemo); err != nil {
		log.Fatalf("nobred: %v", err)
	}
}

// resolveDBPath decide o banco a abrir. Com --seed-demo o banco real nunca é
// usado: sem --db vale o banco de demonstração, e --db igual ao caminho do
// banco real é recusado.
func resolveDBPath(dbFlag string, seedDemo bool) (string, error) {
	if dbFlag == "" {
		if seedDemo {
			return store.DefaultDemoPath(), nil
		}
		return store.DefaultPath(), nil
	}
	if seedDemo && sameFile(dbFlag, store.DefaultPath()) {
		return "", errors.New("--seed-demo não pode usar o banco real; omita --db ou use outro arquivo")
	}
	return dbFlag, nil
}

func sameFile(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	return errA == nil && errB == nil && aa == bb
}

// checkAddr recusa endereço de escuta que não seja loopback: o daemon nunca
// fica exposto na rede.
func checkAddr(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return fmt.Errorf("NOBRE_ADDR inválido: %q (use host:porta, ex.: %s)", addr, defaultAddr)
	}
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.To4() != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("NOBRE_ADDR precisa ser um endereço local (127.0.0.0/8 ou localhost), recebido %q", addr)
}

func run(dbPath string, seedDemo bool) error {
	addr := os.Getenv("NOBRE_ADDR")
	if addr == "" {
		addr = defaultAddr
	}
	if err := checkAddr(addr); err != nil {
		return err
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer st.Close()
	if seedDemo {
		if err := loadDemo(st); err != nil {
			return err
		}
	}

	hub := api.NewHub()
	git := gitinfo.Exec{}
	claudeAdapter := claude.New(st, adapter.Deps{Now: time.Now, Git: git, Publish: hub.Publish})
	paths := claude.DefaultPaths()
	// PATH das sessões e da busca do claude: o do shell de login do usuário,
	// consultado na hora (a unidade do systemd só tem o mínimo, de reserva),
	// sempre com o diretório do nobre (a sessão nova chama nobre pela skill).
	// Quando o PATH muda, o próprio daemon passa a usá-lo: o git, o terminal
	// do "Continuar" e o claude --version são procurados por ele.
	userPATH := &launch.UserPATH{Shell: launch.LoginShell(), Fallback: os.Getenv("PATH"), Logf: log.Printf,
		OnChange: func(p string) { os.Setenv("PATH", p) }}
	if paths.NobreBin != "" {
		userPATH.NobreDir = filepath.Dir(paths.NobreBin)
	}
	paths.SearchPATH = func() string { return userPATH.Get(time.Minute) }
	go userPATH.Get(0) // já na partida, sem atrasar o servidor
	claudeAdapter.SetPaths(paths)
	// RTK: comprime a saída dos comandos do Claude Code; o hook mora no mesmo
	// settings.json. Sem demo o Tool existe, mas o monitor não roda.
	home, _ := os.UserHomeDir()
	rtkTool := &rtk.Tool{SettingsPath: paths.SettingsPath, Home: home,
		SearchPATH: func() string { return userPATH.Get(time.Minute) }, Store: st, Now: time.Now}
	// As leituras de transcript em segundo plano terminam antes de st.Close
	// (defers rodam em ordem inversa).
	defer claudeAdapter.Wait()
	// Recursos dos worktrees: processos a cada 5 s (só em memória) e disco
	// medido em segundo plano, com cache de 10 min.
	monitor := resources.Monitor{Sampler: resources.NewSampler("/proc", time.Now), Meter: resources.NewDiskMeter(10*time.Minute, time.Now)}
	// Ordem no desligamento: o servidor HTTP para primeiro (Shutdown, no corpo
	// de run), depois os terminais morrem (Close), depois o Wait do adaptador
	// e por fim o banco.
	terms := pty.NewManager(pty.Options{})
	defer terms.Close()
	// Monitor da integração com o Claude Code; o daemon de demonstração não o roda.
	var integ api.IntegrationSource
	var mon *integration.Monitor
	if !seedDemo {
		mon = &integration.Monitor{
			Health: func() []adapter.Check {
				checks := claudeAdapter.Health()
				if checks == nil { // sem settings.json do Claude: nada a vigiar
					return nil
				}
				return append(checks, rtkTool.Check())
			},
			Files:       claudeAdapter.WatchedFiles,
			NobreBin:    claudeAdapter.NobreBinary,
			Store:       st,
			DaemonStart: time.Now(),
			Publish:     hub.Publish,
			Logf:        log.Printf,
		}
		integ = mon
	}
	// A demonstração nunca mexe no settings.json real: sem RTK, as rotas dão 503.
	apiRTK := rtkTool
	if seedDemo {
		apiRTK = nil
	}
	handler := api.New(st, api.Deps{
		Integration: integ,
		Now:         time.Now,
		ProcRoot:    "/proc",
		Git:         git,
		Launcher:    launch.Exec{},
		Hub:         hub,
		Adapters:    map[string]adapter.Adapter{"claude": claudeAdapter},
		UserPATH:    userPATH,
		Resources:   monitor,
		Terminals:   terms,
		RTK:         apiRTK,
	})
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	// Streams SSE abertos impediriam o Shutdown de terminar.
	srv.RegisterOnShutdown(hub.Close)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// A porta é ocupada antes de qualquer trabalho em segundo plano: um segundo
	// nobred (com o daemon real no ar) falha aqui, sem iniciar o backfill nem
	// disputar os cursores do daemon real.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	// Histórico: mede o contexto dos transcripts já existentes (para entre
	// sessões quando o ctx termina; o Wait do adaptador espera).
	claudeAdapter.StartBackfill(ctx)

	// Reconciliação a cada 30 s: encerra sessões de processos que morreram.
	recDone := make(chan struct{})
	go func() {
		defer close(recDone)
		board.RunReconciler(ctx, st, "/proc", 30*time.Second, time.Now,
			func() { hub.Publish("cards"); hub.Publish("sessions") }, log.Printf)
	}()
	// Em qualquer retorno (inclusive falha precoce do servidor) o reconciliador
	// para e termina antes de st.Close (defers rodam em ordem inversa).
	defer func() { stop(); <-recDone }()
	// Integração a cada 2 min: nota branches de cards que entraram na base.
	intDone := make(chan struct{})
	go func() {
		defer close(intDone)
		board.RunIntegrations(ctx, st, git, 2*time.Minute, time.Now,
			func() { hub.Publish("cards"); hub.Publish("memories") }, log.Printf)
	}()
	defer func() { stop(); <-intDone }()
	if mon != nil {
		monDone := make(chan struct{})
		go func() {
			defer close(monDone)
			mon.Run(ctx, 30*time.Second, 5*time.Minute)
		}()
		defer func() { stop(); <-monDone }()
	}
	go monitor.Sampler.Run(ctx, 5*time.Second)
	go monitor.Meter.Run(ctx)

	errc := make(chan error, 1)
	go func() {
		log.Printf("nobred ouvindo em http://%s (banco: %s)", addr, dbPath)
		errc <- srv.Serve(ln)
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Print("nobred encerrando…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// loadDemo carrega os dados de demonstração; num banco que já tem cards só
// registra o fato (e, se for o banco de demonstração, aponta as sessões vivas
// para este processo, para a reconciliação não as encerrar).
func loadDemo(st *store.Store) error {
	err := demo.Seed(st, time.Now(), os.Getpid())
	if !errors.Is(err, demo.ErrNotEmpty) {
		return err
	}
	set, err := st.GetSettings()
	if err != nil {
		return err
	}
	if !set.Demo {
		log.Print("nobred: o banco já tem cards e não é de demonstração; dados de demonstração não carregados")
		return nil
	}
	log.Print("nobred: banco de demonstração já carregado; mantendo os dados")
	return demo.RefreshLivePID(st, os.Getpid())
}
