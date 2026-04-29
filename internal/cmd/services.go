package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/service"
)

func runServices(args []string) error {
	slog.Debug("starting services command execution")
	slog.Debug("starting services command execution")
	if len(args) == 0 {
		return servicesUsage()
	}

	sub := args[0]
	rest := args[1:]

	switch sub {
	case "list", "ls":
		return servicesList(rest)
	case "start":
		return servicesStart(rest)
	case "stop":
		return servicesStop(rest)
	case "restart":
		return servicesRestart(rest)
	case "run":
		return servicesRun(rest)
	case "info":
		return servicesInfo(rest)
	default:
		return fmt.Errorf("unknown services subcommand: %s\nRun 'grew help services' for usage", sub)
	}
}

func servicesUsage() error {
	fmt.Print(`Usage: grew services <subcommand> [arguments]

Subcommands:
  list, ls              List managed services
  start <formula>       Start a service (runs at login)
  stop <formula>        Stop and unregister a service
  restart <formula>     Restart a service
  run <formula>         Run the service command in the foreground
  info <formula>        Show service info and status

Examples:
  grew services list
  grew services start postgresql
  grew services stop postgresql
  grew services restart redis
  grew services run postgresql
`)
	return nil
}

type servicesCtx struct {
	readContext
	mgr *service.Manager
}

func newServicesCtx() (*servicesCtx, error) {
	common, err := newReadContext()
	if err != nil {
		return nil, err
	}

	mgr, err := service.DefaultManager(common.Paths.Cellar, common.Paths.Opt, common.Loader)
	if err != nil {
		return nil, err
	}

	return &servicesCtx{
		readContext: common,
		mgr:         mgr,
	}, nil
}

func servicesList(_ []string) error {
	ctx, err := newServicesCtx()
	if err != nil {
		return err
	}

	infos, err := ctx.mgr.List()
	if err != nil {
		return err
	}

	if len(infos) == 0 {
		fmt.Println("No managed services.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATUS\tPID\tFILE")
	for _, info := range infos {
		pid := "-"
		if info.PID > 0 {
			pid = fmt.Sprintf("%d", info.PID)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", info.Name, info.Status, pid, info.File)
	}
	w.Flush()
	return nil
}

func servicesStart(args []string) error {
	ctx, f, err := requireServiceFormula("start", args)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "==> Starting %s service...\n", f.Name)
	if err := ctx.mgr.Start(f); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "==> %s service started\n", f.Name)
	return nil
}

func servicesStop(args []string) error {
	ctx, name, err := requireServiceCtx("stop", args)
	if err != nil {
		return err
	}

	if !ctx.mgr.IsManaged(name) {
		return fmt.Errorf("service %q is not running", name)
	}

	fmt.Fprintf(os.Stderr, "==> Stopping %s service...\n", name)
	if err := ctx.mgr.Stop(name); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "==> %s service stopped\n", name)
	return nil
}

func servicesRestart(args []string) error {
	ctx, f, err := requireServiceFormula("restart", args)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "==> Restarting %s service...\n", f.Name)
	if err := ctx.mgr.Restart(f); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "==> %s service restarted\n", f.Name)
	return nil
}

func servicesRun(args []string) error {
	ctx, f, err := requireServiceFormula("run", args)
	if err != nil {
		return err
	}

	cmdArgs := ctx.mgr.ResolveCommand(f)
	if len(cmdArgs) == 0 {
		return fmt.Errorf("formula %q service has no run command", f.Name)
	}

	// Validate the resolved command binary exists and is not a flag.
	if strings.HasPrefix(cmdArgs[0], "-") {
		return fmt.Errorf("service command starts with dash: %q", cmdArgs[0])
	}
	cmdPath, err := exec.LookPath(cmdArgs[0])
	if err != nil {
		return fmt.Errorf("service command %q not found: %w", cmdArgs[0], err)
	}

	fmt.Fprintf(os.Stderr, "==> Running %s in foreground (%s)\n", f.Name, strings.Join(cmdArgs, " "))
	fmt.Fprintf(os.Stderr, "==> Press Ctrl-C to stop\n")

	cmd := exec.Command(cmdPath, cmdArgs[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if f.Service.WorkingDir != "" {
		wd := filepath.Clean(f.Service.WorkingDir)
		if !filepath.IsAbs(wd) {
			return fmt.Errorf("service working directory must be absolute: %q", f.Service.WorkingDir)
		}
		if wd != f.Service.WorkingDir {
			return fmt.Errorf("service working directory contains traversal elements: %q", f.Service.WorkingDir)
		}
		cmd.Dir = wd
	}

	// Forward signals so the child process gets a clean shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		if cmd.Process != nil {
			cmd.Process.Signal(sig)
		}
	}()

	return cmd.Run()
}

func servicesInfo(args []string) error {
	ctx, name, err := requireServiceCtx("info", args)
	if err != nil {
		return err
	}

	f, err := ctx.Loader.LoadByName(name)
	if err != nil {
		return fmt.Errorf("formula not found: %s", name)
	}

	if f.Service == nil {
		fmt.Printf("%s does not define a service.\n", name)
		return nil
	}

	fmt.Printf("Name:       %s\n", f.Name)
	fmt.Printf("Command:    %s\n", strings.Join(f.Service.Run, " "))
	if f.Service.RunType != "" {
		fmt.Printf("Run type:   %s\n", f.Service.RunType)
	}
	if f.Service.WorkingDir != "" {
		fmt.Printf("Working dir: %s\n", f.Service.WorkingDir)
	}
	if f.Service.LogPath != "" {
		fmt.Printf("Log:        %s\n", f.Service.LogPath)
	}
	if f.Service.ErrorLogPath != "" {
		fmt.Printf("Error log:  %s\n", f.Service.ErrorLogPath)
	}
	fmt.Printf("Keep alive: %v\n", f.Service.KeepAlive)

	if ctx.mgr.IsManaged(name) {
		infos, _ := ctx.mgr.List()
		for _, info := range infos {
			if info.Name == name {
				fmt.Printf("Status:     %s\n", info.Status)
				if info.PID > 0 {
					fmt.Printf("PID:        %d\n", info.PID)
				}
				fmt.Printf("File:       %s\n", info.File)
				break
			}
		}
	} else {
		fmt.Printf("Status:     not registered\n")
	}

	return nil
}

// requireServiceFormula validates args, creates the services context, and loads the formula.
func requireServiceFormula(sub string, args []string) (*servicesCtx, *formula.Formula, error) {
	if len(args) != 1 {
		return nil, nil, fmt.Errorf("usage: grew services %s <formula>", sub)
	}
	ctx, err := newServicesCtx()
	if err != nil {
		return nil, nil, err
	}
	f, err := loadServiceFormula(ctx, args[0])
	if err != nil {
		return nil, nil, err
	}
	return ctx, f, nil
}

// requireServiceCtx validates args and creates the services context.
func requireServiceCtx(sub string, args []string) (*servicesCtx, string, error) {
	if len(args) != 1 {
		return nil, "", fmt.Errorf("usage: grew services %s <formula>", sub)
	}
	ctx, err := newServicesCtx()
	if err != nil {
		return nil, "", err
	}
	return ctx, args[0], nil
}

// loadServiceFormula loads and validates a formula for service use.
func loadServiceFormula(ctx *servicesCtx, name string) (*formula.Formula, error) {
	if !ctx.Cellar.IsInstalled(name) {
		return nil, fmt.Errorf("formula %q is not installed", name)
	}
	f, err := ctx.Loader.LoadByName(name)
	if err != nil {
		return nil, fmt.Errorf("formula not found: %s", name)
	}
	if f.Service == nil {
		return nil, fmt.Errorf("formula %q does not define a service", name)
	}
	return f, nil
}
