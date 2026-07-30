package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"codeberg.org/nocfa/segments/internal/mapbridge"
	"codeberg.org/nocfa/segments/internal/store"
)

func runMap(s *store.Store, args []string) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		printMapHelp()
		return nil
	}
	action := args[0]
	if action != "export" && action != "sync" {
		return fmt.Errorf("unknown map subcommand %q; expected export or sync", action)
	}

	repo := "."
	projectHint := ""
	interval := 2 * time.Second
	intervalSet := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--repo":
			if i+1 >= len(args) {
				return fmt.Errorf("--repo requires a path")
			}
			repo = args[i+1]
			i++
		case "--project", "-p":
			if i+1 >= len(args) {
				return fmt.Errorf("%s requires a name or id", args[i])
			}
			projectHint = args[i+1]
			i++
		case "--interval":
			if i+1 >= len(args) {
				return fmt.Errorf("--interval requires a duration")
			}
			parsed, err := time.ParseDuration(args[i+1])
			if err != nil || parsed <= 0 {
				return fmt.Errorf("invalid --interval value %q", args[i+1])
			}
			interval = parsed
			intervalSet = true
			i++
		case "-h", "--help":
			printMapHelp()
			return nil
		default:
			return fmt.Errorf("unknown argument: %s", args[i])
		}
	}
	if action == "export" && intervalSet {
		return fmt.Errorf("--interval is only valid with sg map sync")
	}

	projects, err := s.ListProjects()
	if err != nil {
		return err
	}
	project := resolveProject(projects, projectHint)
	if project == nil {
		if projectHint != "" {
			return fmt.Errorf("no project matches %q", projectHint)
		}
		return fmt.Errorf("cannot auto-resolve project; pass --project <name|id>")
	}
	bridge, err := mapbridge.New(mapbridge.Config{Store: s, Project: *project, Repo: repo, Out: os.Stdout})
	if err != nil {
		return err
	}
	if action == "export" {
		return bridge.Export()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return bridge.Sync(ctx, interval)
}

func printMapHelp() {
	fmt.Println("usage: sg map export [--repo <path>] [--project <name|id>]")
	fmt.Println("       sg map sync   [--repo <path>] [--project <name|id>] [--interval <dur>]")
	fmt.Println()
	fmt.Println("  export       materialize the selected Segments project once")
	fmt.Println("  sync         export, then continuously sync files and Segments")
	fmt.Println("  --repo       repository directory (default: current directory)")
	fmt.Println("  --project    project name or UUID prefix (default: CWD auto-resolution)")
	fmt.Println("  --interval   Segments poll cadence for sync (default: 2s)")
}
