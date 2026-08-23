package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanPublicIPv4RejectsInfrastructureAndAllowsTestAddresses(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "safe.txt"), []byte("127.0.0.1 192.0.2.1 1.1.1.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	violations, err := scanPublicIPv4(root)
	if err != nil || len(violations) != 0 {
		t.Fatalf("safe scan = %v, %v", violations, err)
	}
	if err := os.WriteFile(filepath.Join(root, "unsafe.txt"), []byte("9.9."+"9.9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	violations, err = scanPublicIPv4(root)
	if err != nil || len(violations) != 1 {
		t.Fatalf("unsafe scan = %v, %v", violations, err)
	}
}

func TestScanPublicIPv4SkipsInstalledNodeModules(t *testing.T) {
	root := t.TempDir()
	modules := filepath.Join(root, "node_modules", "fixture")
	if err := os.MkdirAll(modules, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modules, "package.txt"), []byte("9.9."+"9.9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	violations, err := scanPublicIPv4(root)
	if err != nil || len(violations) != 0 {
		t.Fatalf("node_modules scan = %v, %v", violations, err)
	}
}

func TestScanPublicIPv4SkipsLocalOperationsFacts(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "OPS.local.md"), []byte("operator host 9.9."+"9.9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	violations, err := scanPublicIPv4(root)
	if err != nil || len(violations) != 0 {
		t.Fatalf("local operations scan = %v, %v", violations, err)
	}
}
