package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/homegrew/grew/internal/cellar"
	"github.com/homegrew/grew/internal/config"
	"github.com/homegrew/grew/internal/formula"
	"github.com/homegrew/grew/internal/service"
	"github.com/homegrew/grew/internal/tap"
)

func runServices(args []string) error {
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
	paths  config.Paths
	mgr    *service.Manager
	loader *formula.Loader
	cel    *cellar.Cellar
}

func newServicesCtx() (*servicesCtx, error) {
	paths := config.Default()
	if err := paths.Init(); err != nil {
		return nil, err
	}

	tapMgr := &tap.Manager{TapsDir: paths.Taps}
	if err := tapMgr.InitCore(); err != nil {
		Debugf("init core tap: %v\n", err)
	}

	loader := newLoader(paths.Taps)
	cel := &cellar.Cellar{Path: paths.Cellar}

	mgr, err := service.DefaultManager(paths.Cellar, paths.Opt, loader)
	if err != nil {
		return nil, err
	}

	return &servicesCtx{
		paths:  paths,
		mgr:    mgr,
		loader: loader,
		cel:    cel,
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
	if len(args) != 1 {
		return fmt.Errorf("usage: grew services start <formula>")
	}

	ctx, err := newServicesCtx()
	if err != nil {
		return err
	}

	f, err := loadServiceFormula(ctx, args[0])
	if err != nil {
		return err
	}

	fmt.Printf("==> Starting %s service...\n", f.Name)
	if err := ctx.mgr.Start(f); err != nil {
		return err
	}
	fmt.Printf("==> %s service started\n", f.Name)
	return nil
}

func servicesStop(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: grew services stop <formula>")
	}

	ctx, err := newServicesCtx()
	if err != nil {
		return err
	}

	name := args[0]
	if !ctx.mgr.IsManaged(name) {
		return fmt.Errorf("service %q is not running", name)
	}

	fmt.Printf("==> Stopping %s service...\n", name)
	if err := ctx.mgr.Stop(name); err != nil {
		return err
	}
	fmt.Printf("==> %s service stopped\n", name)
	return nil
}

func servicesRestart(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: grew services restart <formula>")
	}

	ctx, err := newServicesCtx()
	if err != nil {
		return err
	}

	f, err := loadServiceFormula(ctx, args[0])
	if err != nil {
		return err
	}

	fmt.Printf("==> Restarting %s service...\n", f.Name)
	if err := ctx.mgr.Restart(f); err != nil {
		return err
	}
	fmt.Printf("==> %s service restarted\n", f.Name)
	return nil
}

func servicesRun(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: grew services run <formula>")
	}

	ctx, err := newServicesCtx()
	if err != nil {
		return err
	}

	f, err := loadServiceFormula(ctx, args[0])
	if err != nil {
		return err
	}

	cmdArgs := ctx.mgr.ResolveCommand(f)
	if len(cmdArgs) == 0 {
		return fmt.Errorf("formula %q service has no run command", f.Name)
	}

	fmt.Printf("==> Running %s in foreground (%s)\n", f.Name, strings.Join(cmdArgs, " "))
	fmt.Printf("==> Press Ctrl-C to stop\n")

	cmd := exec.Command(cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if f.Service.WorkingDir != "" {
		cmd.Dir = f.Service.WorkingDir
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
	if len(args) != 1 {
		return fmt.Errorf("usage: grew services info <formula>")
	}

	ctx, err := newServicesCtx()
	if err != nil {
		return err
	}

	name := args[0]
	f, err := ctx.loader.LoadByName(name)
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

// loadServiceFormula loads and validates a formula for service use.
func loadServiceFormula(ctx *servicesCtx, name string) (*formula.Formula, error) {
	if !ctx.cel.IsInstalled(name) {
		return nil, fmt.Errorf("formula %q is not installed", name)
	}
	f, err := ctx.loader.LoadByName(name)
	if err != nil {
		return nil, fmt.Errorf("formula not found: %s", name)
	}
	if f.Service == nil {
		return nil, fmt.Errorf("formula %q does not define a service", name)
	}
	return f, nil
}
