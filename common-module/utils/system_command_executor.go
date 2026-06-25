package utils

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func CLS() {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("clear") //Linux example, its tested
	case "darwin":
		cmd = exec.Command("sh", "-c", "clear && printf '\\e[3J'")
	case "windows":
		if os.Getenv("PROMPT") != "" {
			cmd = exec.Command("cmd", "/c", "cls") //Windows example, its tested
		} else {
			cmd = exec.Command("pwsh", "-Command", "Clear-Host")
		}
	default:
		fmt.Println("CLS for ", runtime.GOOS, " not implemented")
		return
	}
	cmd.Stdout = os.Stdout
	cmd.Run()
}

func Shellout(command string) (string, string, int, error) {
	var cmd *exec.Cmd
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	switch runtime.GOOS {
	case "linux":
		cmd = exec.Command("bash", "-c", command)
	case "windows":
		if os.Getenv("PROMPT") != "" {
			cmd = exec.Command("cmd", "/c", command)
		} else {
			cmd = exec.Command("pwsh", "-Command", command)
		}
	default:
		return "", "", 130, fmt.Errorf("%s not implemented", runtime.GOOS)
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := cmd.ProcessState.ExitCode()
	stdoutString := strings.TrimPrefix(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	stderrString := strings.TrimPrefix(strings.TrimSuffix(stderr.String(), "\n"), "\n")
	return stdoutString, stderrString, exitCode, err
}
