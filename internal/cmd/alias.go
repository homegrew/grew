package cmd

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/homegrew/grew/internal/config"
	"github.com/spf13/cobra"
)

// AliasCmd represents the alias command
var AliasCmd = &cobra.Command{
	Use:   "alias [subcommand]",
	Short: "Manage command aliases",
	Long: `Manage command aliases. Aliases let you create shortcuts for
frequently used commands.

Subcommands:
  list, ls             List all aliases (default)
  add <name> <command> Create or overwrite an alias
  rm <name>            Remove an alias
  show <name>          Show what an alias expands to
  edit                 Open the alias file in $EDITOR

Examples:
  grew alias add i install
  grew alias add ri reinstall
  grew alias ls
  grew alias rm i
  grew alias show i
  grew alias edit`,
	RunE: func(cmd *cobra.Command, args []string) error {
		slog.Debug("starting alias command execution")
		if len(args) == 0 {
			return aliasList()
		}

		switch args[0] {
		case "list", "ls":
			return aliasList()
		case "add":
			if len(args) < 3 {
				return fmt.Errorf("usage: grew alias add <name> <command>")
			}
			return aliasAdd(args[1], args[2])
		case "rm", "remove", "delete":
			if len(args) < 2 {
				return fmt.Errorf("usage: grew alias rm <name>")
			}
			return aliasRemove(args[1])
		case "show":
			if len(args) < 2 {
				return fmt.Errorf("usage: grew alias show <name>")
			}
			return aliasShow(args[1])
		case "edit":
			return aliasEdit()
		default:
			return fmt.Errorf("unknown alias subcommand: %s\nRun 'grew help alias' for usage", args[0])
		}
	},
}

func init() {
	rootCmd.AddCommand(AliasCmd)
}

// aliases maps alias names to command strings.
type aliases map[string]string

func aliasFile() string {
	return filepath.Join(config.Default().Root, "aliases.json")
}

func loadAliases() (aliases, error) {
	a := make(aliases)
	data, err := os.ReadFile(aliasFile())
	if err != nil {
		if os.IsNotExist(err) {
			return a, nil
		}
		return nil, fmt.Errorf("read aliases: %w", err)
	}
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("parse aliases: %w", err)
	}
	return a, nil
}

func saveAliases(a aliases) error {
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal aliases: %w", err)
	}
	return os.WriteFile(aliasFile(), data, 0644)
}

func aliasList() error {
	a, err := loadAliases()
	if err != nil {
		return err
	}
	if len(a) == 0 {
		fmt.Println("No aliases defined.")
		return nil
	}
	names := make([]string, 0, len(a))
	for name := range a {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Printf("%-20s %s\n", name, a[name])
	}
	return nil
}

func aliasAdd(name, command string) error {
	a, err := loadAliases()
	if err != nil {
		return err
	}
	if old, exists := a[name]; exists {
		fmt.Printf("Overwriting alias %q (was: %s)\n", name, old)
	}
	a[name] = command
	if err := saveAliases(a); err != nil {
		return err
	}
	fmt.Printf("Added alias: %s -> %s\n", name, command)
	return nil
}

func aliasRemove(name string) error {
	a, err := loadAliases()
	if err != nil {
		return err
	}
	if _, exists := a[name]; !exists {
		return fmt.Errorf("alias %q does not exist", name)
	}
	delete(a, name)
	if err := saveAliases(a); err != nil {
		return err
	}
	fmt.Printf("Removed alias: %s\n", name)
	return nil
}

func aliasShow(name string) error {
	a, err := loadAliases()
	if err != nil {
		return err
	}
	cmd, exists := a[name]
	if !exists {
		return fmt.Errorf("alias %q does not exist", name)
	}
	fmt.Printf("%s: %s\n", name, cmd)
	return nil
}

func aliasEdit() error {
	path := aliasFile()
	// Ensure file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := saveAliases(make(aliases)); err != nil {
			return err
		}
	}
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		fmt.Printf("Alias file: %s\n", path)
		return fmt.Errorf("no EDITOR or VISUAL set; edit %s manually", path)
	}

	// Validate the editor value: reject shell metacharacters and empty components.
	if strings.ContainsAny(editor, "|;&$`\\'\"\n\t") {
		return fmt.Errorf("EDITOR contains invalid characters: %q", editor)
	}

	// Resolve the editor binary via PATH to ensure it exists.
	editorPath, err := exec.LookPath(editor)
	if err != nil {
		return fmt.Errorf("editor %q not found in PATH: %w", editor, err)
	}

	fmt.Printf("Opening %s with %s...\n", path, editor)
	cmd := exec.Command(editorPath, "--", path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
