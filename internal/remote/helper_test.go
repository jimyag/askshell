package remote

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEmbeddedHelpersAreELF(t *testing.T) {
	for name, platform := range map[string]remotePlatform{
		"amd64": {os: "linux", arch: "x86_64"},
		"arm64": {os: "linux", arch: "aarch64"},
	} {
		binary, ok := helperBinary(platform)
		if !ok {
			t.Fatalf("no helper for %v", platform)
		}
		if len(binary) < 4 || string(binary[:4]) != "\x7fELF" {
			t.Fatalf("helper %s is not an ELF binary (%d bytes)", name, len(binary))
		}
	}
}

func TestHelperBinaryRejectsUnknownPlatforms(t *testing.T) {
	for _, platform := range []remotePlatform{
		{os: "darwin", arch: "arm64"},
		{os: "linux", arch: "riscv64"},
		{os: "freebsd", arch: "x86_64"},
	} {
		if _, ok := helperBinary(platform); ok {
			t.Fatalf("unexpected helper for %v", platform)
		}
	}
}

func TestDeployStreamsHelperThroughCat(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "received")
	stub := filepath.Join(dir, "ssh")
	script := "#!/bin/sh\nexec cat > " + out + "\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("cat"); err != nil {
		t.Skip("cat unavailable")
	}

	helper := make([]byte, 2*1024*1024)
	for index := range helper {
		helper[index] = byte(index % 251)
	}
	if err := deploy(stub, []string{"target"}, "/tmp/askshell-test", "/tmp/askshell-test/askshell-remote", helper); err != nil {
		t.Fatal(err)
	}
	received, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(received) != len(helper) {
		t.Fatalf("streamed %d bytes, want %d", len(received), len(helper))
	}
	for index := range received {
		if received[index] != helper[index] {
			t.Fatalf("byte %d corrupted: got %d want %d", index, received[index], helper[index])
		}
	}
}

func TestProbeParsesUnameOutput(t *testing.T) {
	platform, err := probe(fakeSSH(t, "#!/bin/sh\necho linux x86_64\n"), []string{"target"})
	if err != nil {
		t.Fatal(err)
	}
	if platform != (remotePlatform{os: "linux", arch: "x86_64"}) {
		t.Fatalf("platform = %+v", platform)
	}
}

func TestProbeRejectsGarbage(t *testing.T) {
	_, err := probe(fakeSSH(t, "#!/bin/sh\necho onlyone\n"), []string{"target"})
	if err == nil {
		t.Fatal("expected error for single-field uname output")
	}
}

func fakeSSH(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ssh")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestControlArgsStripTtyFlags(t *testing.T) {
	got := controlArgs([]string{
		"-tt", "-o", "StrictHostKeyChecking=no", "-T", "-p", "2222", "user@host",
	})
	want := []string{"-o", "StrictHostKeyChecking=no", "-p", "2222", "user@host"}
	if len(got) != len(want) {
		t.Fatalf("controlArgs = %v", got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("controlArgs = %v", got)
		}
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("don't"); got != `'don'\''t'` {
		t.Fatalf("shellQuote = %s", got)
	}
}
