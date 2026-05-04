package services

import (
	"github.com/homegrew/grew/internal/cmd"
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
	"github.com/spf13/cobra"
	"github.com/homegrew/grew/pkg/ui"
)

var Command = &cobra.Command{
	Use:   "services <subcommand> [arguments]",
	Short: "Manage background services",
	Long: `Manage background services for installed formulas. Services are
registered with the platform init system (launchd) so they persist across reboots.

Subcommands:
  list, ls              List all managed services and their status
  start <formula>       Write a service definition and start it
  stop <formula>        Stop the service and remove its definition
  restart <formula>     Stop then start the service
  run <formula>         Run the service command in the foreground
  info <formula>        Show service configuration and status

The service definition comes from the formula's "service" field.
The run command supports {prefix}, {opt}, and {cellar} placeholders
that are expanded to the grew directory paths.

On macOS, services are managed via launchctl (~/Library/LaunchAgents).

Examples:
  grew services list
  grew services start postgresql
  grew services stop redis
  grew services restart postgresql
  grew services run postgresql
  grew services info postgresql`,
	RunE: func(c *cobra.Command, args []string) error {
		slog.Debug("starting services command execution")
		if len(args) == 0 {
			return c.Help()
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
	},
}

func init() {
}

type servicesCtx struct {
	cmd.ReadContext
	mgr *service.Manager
}

func newServicesCtx() (*servicesCtx, error) {
	common, err := cmd.NewReadContext()
	if err != nil {
		return nil, err
	}

	mgr, err := service.DefaultManager(common.Paths.Cellar, common.Paths.Opt, common.Loader)
	if err != nil {
		return nil, err
	}

	return &servicesCtx{
		ReadContext: common,
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

	ui.FprintArrow(os.Stderr, "Starting %s service...", f.Name)
	if err := ctx.mgr.Start(f); err != nil {
		return err
	}
	ui.FprintArrow(os.Stderr, "%s service started", f.Name)
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

	ui.FprintArrow(os.Stderr, "Stopping %s service...", name)
	if err := ctx.mgr.Stop(name); err != nil {
		return err
	}
	ui.FprintArrow(os.Stderr, "%s service stopped", name)
	return nil
}

func servicesRestart(args []string) error {
	ctx, f, err := requireServiceFormula("restart", args)
	if err != nil {
		return err
	}

	ui.FprintArrow(os.Stderr, "Restarting %s service...", f.Name)
	if err := ctx.mgr.Restart(f); err != nil {
		return err
	}
	ui.FprintArrow(os.Stderr, "%s service restarted", f.Name)
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

	ui.FprintArrow(os.Stderr, "Running %s in foreground (%s)", f.Name, strings.Join(cmdArgs, " "))
	ui.FprintArrow(os.Stderr, "Press Ctrl-C to stop")

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
