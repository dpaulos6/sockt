package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"sockt/internal/database"
	"sockt/internal/server"
	"sockt/internal/transport"
)

func getenv(key, fallback string) string {
	if x := os.Getenv(key); x != "" {
		return x
	}
	return fallback
}
func safeListen(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("use a literal loopback IP or localhost, not a public hostname")
	}
	if ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("Sockt v0.7 must listen on loopback behind an HTTPS reverse proxy; refusing %q", addr)
}
func main() {
	sub := "serve"
	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "serve", "migrate", "invite", "import-json":
			sub = args[0]
			args = args[1:]
		}
	}
	flags := flag.NewFlagSet(sub, flag.ExitOnError)
	dsn := flags.String("db", getenv("DATABASE_URL", ""), "PostgreSQL connection URL (prefer DATABASE_URL)")
	listen := flags.String("listen", getenv("SOCKT_LISTEN", "127.0.0.1:8080"), "HTTP listen address: loopback IP:port only")
	importFile := flags.String("file", "data/messages.json", "v0.5 JSON message history to import")
	_ = flags.Parse(args)
	if *dsn == "" {
		log.Fatal("DATABASE_URL is required (see .env.example)")
	}
	// Never log a database URL; it contains secrets.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	db, err := database.Open(ctx, *dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	switch sub {
	case "migrate":
		if err := db.Migrate(context.Background()); err != nil {
			log.Fatal("migration failed: ", err)
		}
		fmt.Println("Sockt PostgreSQL migrations applied.")
		return
	case "invite":
		if flags.NArg() != 1 {
			log.Fatal("usage: socktd invite [--db URL] <username>")
		}
		ctx, stop := context.WithTimeout(context.Background(), 8*time.Second)
		defer stop()
		token, err := db.Invite(ctx, flags.Arg(0))
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Invite for %s (shown once):\n%s\n\nGive it only to the intended person. They use it to register their own password.\n", flags.Arg(0), token)
		return
	case "import-json":
		ctx, stop := context.WithTimeout(context.Background(), 90*time.Second)
		defer stop()
		count, err := db.ImportJSON(ctx, *importFile)
		if err != nil {
			log.Fatal("history import failed: ", err)
		}
		fmt.Printf("Imported %d legacy messages. Keep a backup of the JSON file.\n", count)
		return
	case "serve":
		if err := db.CheckSchema(context.Background()); err != nil {
			log.Fatal(err)
		}
		if err := safeListen(*listen); err != nil {
			log.Fatal(err)
		}
		sockt := server.New(db, log.Default())
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				http.Error(w, "GET required", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("ok\n"))
		})
		mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
			transport.ServeWS(w, r, sockt.AcceptConnection)
		})
		httpSrv := &http.Server{
			Addr: *listen, Handler: mux,
			ReadHeaderTimeout: 5 * time.Second,
			IdleTimeout:       90 * time.Second,
			MaxHeaderBytes:    16 * 1024,
		}
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(stop)
		go func() {
			<-stop
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = httpSrv.Shutdown(ctx)
			sockt.Shutdown() // Close upgraded chat clients (HTTP Shutdown alone doesn't).
		}()
		log.Printf("socktd v0.7 WebSocket endpoint ws://%s/ws (PostgreSQL)", *listen)
		err := httpSrv.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}
}
