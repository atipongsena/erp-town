// Command sqlcgen runs `sqlc generate` for the package in the current directory using
// the pinned sqlc/sqlc Docker image (plan ruling R6). The directory is passed to Docker
// as an absolute host path, so it behaves the same from PowerShell and Git Bash.
//
// Usage (via go generate in go/internal/store): go run ../../tools/sqlcgen
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const image = "sqlc/sqlc:1.29.0"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sqlcgen:", err)
		os.Exit(1)
	}
}

func run() error {
	dir, err := filepath.Abs(".")
	if err != nil {
		return err
	}
	args := []string{"run", "--rm", "-v", dir + ":/src", "-w", "/src"}
	if runtime.GOOS != "windows" {
		// write generated files as the calling user, not root
		args = append(args, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()))
	}
	args = append(args, image, "generate")
	cmd := exec.Command("docker", args...)
	// Git Bash: never rewrite the /src paths for the docker client.
	cmd.Env = append(os.Environ(), "MSYS_NO_PATHCONV=1")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker %v: %w", args, err)
	}
	return nil
}
