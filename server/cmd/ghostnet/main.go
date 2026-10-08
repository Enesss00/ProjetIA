// Command ghostnet is the GHOST NET game server and tooling CLI.
//
//	ghostnet serve   [-addr :8080] [-static ../web/dist] [-db ./data]
//	ghostnet verify  -seed N        run a seeded game headless, print summary
//	ghostnet replay  -seeds a,b,c   determinism check across seeds
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"ghostnet/internal/netgen"
	"ghostnet/internal/report"
	"ghostnet/internal/server"
	"ghostnet/internal/sim"
)

// build is overridable via -ldflags "-X main.build=...".
var build = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = cmdServe(os.Args[2:])
	case "verify":
		err = cmdVerify(os.Args[2:])
	case "replay":
		err = cmdReplay(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `ghostnet — GHOST NET server & tools

  ghostnet serve  [-addr :8080] [-static DIR] [-db DIR] [-origin host]
  ghostnet verify -seed N [-profile smash|stealth|apt]
  ghostnet replay -seeds 1,2,3 [-runs 2]
`)
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	// Default the listen address from $PORT when set (cloud hosts like Render,
	// Fly and Railway inject it), falling back to :8080 for local runs.
	defaultAddr := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		defaultAddr = ":" + p
	}
	addr := fs.String("addr", defaultAddr, "listen address (defaults to :$PORT when set)")
	static := fs.String("static", "", "static web client directory")
	db := fs.String("db", "", "directory for per-game SQLite files")
	origin := fs.String("origin", "", "comma-separated allowed WS origin hosts")
	_ = fs.Parse(args)

	if *db != "" {
		if err := os.MkdirAll(*db, 0o755); err != nil {
			return err
		}
	}
	var origins []string
	if *origin != "" {
		origins = strings.Split(*origin, ",")
	}
	var llm report.Reporter
	if a := report.NewAnthropicFromEnv(); a != nil {
		llm = a
		fmt.Println("  LLM report enrichment: enabled (ANTHROPIC_API_KEY set)")
	} else {
		fmt.Println("  LLM report enrichment: disabled (deterministic template report)")
	}
	hub := server.NewHub(server.Options{
		Build:       build,
		DBPath:      *db,
		StaticDir:   *static,
		OriginHosts: origins,
		Reporter:    report.Template{},
		LLM:         llm,
	})
	srv := &http.Server{
		Addr:              *addr,
		Handler:           hub.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		fmt.Printf("GHOST NET server (build %s) listening on %s\n", build, *addr)
		if *static != "" {
			fmt.Printf("  serving web client from %s\n", *static)
		}
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "listen:", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	fmt.Println("\nshutting down…")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return srv.Shutdown(shutCtx)
}

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	seed := fs.Uint64("seed", 1, "network seed")
	profile := fs.String("profile", "", "attacker profile (default: seed-derived)")
	_ = fs.Parse(args)

	net := netgen.Generate(*seed, netgen.Options{Profile: *profile})
	var events []sim.Event
	eng := sim.NewEngine(sim.Config{Net: net}, func(ev sim.Event) { events = append(events, ev) })
	for eng.Tick() {
	}
	st := eng.State()
	rep := report.Template{}.Generate(events, st)
	fmt.Printf("seed=%d profile=%s outcome=%s score=%d ticks=%s\n",
		*seed, net.Attacker, st.Outcome, st.Score(), sim.FmtTick(st.Tick))
	fmt.Println(strings.Repeat("-", 60))
	fmt.Println(rep)
	return nil
}

func cmdReplay(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	seeds := fs.String("seeds", "1,2,3,42,1337", "comma-separated seeds")
	runs := fs.Int("runs", 3, "runs per seed")
	_ = fs.Parse(args)

	ok := true
	for _, s := range strings.Split(*seeds, ",") {
		s = strings.TrimSpace(s)
		seed, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return fmt.Errorf("bad seed %q: %w", s, err)
		}
		var firstHash string
		var firstScore int
		for r := 0; r < *runs; r++ {
			net := netgen.Generate(seed, netgen.Options{})
			eng := sim.NewEngine(sim.Config{Net: net}, nil)
			for eng.Tick() {
			}
			h := eng.State().Hash()
			sc := eng.State().Score()
			if r == 0 {
				firstHash, firstScore = h, sc
				continue
			}
			if h != firstHash || sc != firstScore {
				ok = false
				fmt.Printf("DIVERGENCE seed=%d run=%d\n", seed, r)
			}
		}
		status := "OK"
		if !ok {
			status = "FAIL"
		}
		fmt.Printf("seed=%-7d runs=%d score=%-5d hash=%s… %s\n", seed, *runs, firstScore, firstHash[:12], status)
	}
	if !ok {
		return errors.New("determinism check failed")
	}
	fmt.Println("determinism: all seeds reproduce identically")
	return nil
}
