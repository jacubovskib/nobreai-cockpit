// Comando nobred: daemon local do NobreAI Cockpit (API HTTP + stream).
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nobretech/nobreai-cockpit/internal/api"
	"github.com/nobretech/nobreai-cockpit/internal/board"
	"github.com/nobretech/nobreai-cockpit/internal/demo"
	"github.com/nobretech/nobreai-cockpit/internal/gitinfo"
	"github.com/nobretech/nobreai-cockpit/internal/store"
)

const defaultAddr = "127.0.0.1:7420"

func main() {
	dbPath := flag.String("db", "", "caminho do banco SQLite (padrão: "+store.DefaultPath()+")")
	seedDemo := flag.Bool("seed-demo", false, "carrega os dados de demonstração (sem --db usa "+store.DefaultDemoPath()+", nunca o banco real)")
	flag.Parse()
	path := *dbPath
	if path == "" {
		path = store.DefaultPath()
		if *seedDemo {
			path = store.DefaultDemoPath()
		}
	}
	if err := run(path, *seedDemo); err != nil {
		log.Fatalf("nobred: %v", err)
	}
}

func run(dbPath string, seedDemo bool) error {
	addr := os.Getenv("NOBRE_ADDR")
	if addr == "" {
		addr = defaultAddr
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
	handler := api.New(st, api.Deps{
		Now:      time.Now,
		ProcRoot: "/proc",
		Git:      gitinfo.Exec{},
		Hub:      hub,
	})
	srv := &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	// Streams SSE abertos impediriam o Shutdown de terminar.
	srv.RegisterOnShutdown(hub.Close)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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

	errc := make(chan error, 1)
	go func() {
		log.Printf("nobred ouvindo em http://%s (banco: %s)", addr, dbPath)
		errc <- srv.ListenAndServe()
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
