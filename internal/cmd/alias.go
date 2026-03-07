package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/homegrew/grew/internal/config"
)

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

func runAlias(args []string) error {
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
	fmt.Printf("Opening %s with %s...\n", path, editor)
	proc, err := os.StartProcess(editor, []string{editor, path}, &os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
	})
	if err != nil {
		return fmt.Errorf("open editor: %w", err)
	}
	_, err = proc.Wait()
	return err
}
