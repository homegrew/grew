package quarantine

import (
	"strings"
	"testing"
)

func TestRunScript_Success(t *testing.T) {
	script := []byte(`
print("hello world")
`)
	out, err := RunScript(script)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if strings.TrimSpace(out) != "hello world" {
		t.Errorf("expected 'hello world', got %q", out)
	}
}

func TestRunScript_SyntaxError(t *testing.T) {
	script := []byte(`
this is not valid swift
`)
	_, err := RunScript(script)
	if err == nil {
		t.Fatal("expected error for invalid swift script, got nil")
	}
	if !strings.Contains(err.Error(), "swift script failed") {
		t.Errorf("expected error to mention swift script failed, got: %v", err)
	}
}

func TestRunScript_WithArgs(t *testing.T) {
	script := []byte(`
import Foundation
let args = Array(CommandLine.arguments.dropFirst())
print(args.joined(separator: ","))
`)
	out, err := RunScript(script, "arg1", "arg2", "arg3")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if strings.TrimSpace(out) != "arg1,arg2,arg3" {
		t.Errorf("expected 'arg1,arg2,arg3', got %q", out)
	}
}
