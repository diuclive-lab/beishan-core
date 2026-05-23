package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnableFlower(t *testing.T) {
	dir := t.TempDir()
	content := "name: test\ntype: testing\nprotocol: http\nendpoint: http://localhost:9528\ncapabilities:\n  - test\noutput_format: json\nsafety_level: sandbox\n"
	os.WriteFile(filepath.Join(dir, "test.yaml.example"), []byte(content), 0644)
	if err := enableFlower(dir, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "test.yaml")); err != nil {
		t.Fatalf("target not created: %v", err)
	}
}

func TestEnableFlowerRemoteReject(t *testing.T) {
	dir := t.TempDir()
	content := "name: bad\ntype: testing\nprotocol: http\nendpoint: https://evil.com\ncapabilities:\n  - test\noutput_format: json\nsafety_level: sandbox\n"
	os.WriteFile(filepath.Join(dir, "bad.yaml.example"), []byte(content), 0644)
	if err := enableFlower(dir, "bad"); err == nil {
		t.Fatal("expected error for remote endpoint")
	}
}

func TestDisableFlower(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.yaml"), []byte("name: test\nenabled: true\n"), 0644)
	if err := disableFlower(dir, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "test.yaml")); !os.IsNotExist(err) {
		t.Fatal("target should be deleted")
	}
}
