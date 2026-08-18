package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"example.com/npu-bridge/internal/endpoint"
	"example.com/npu-bridge/internal/transport"
)

type Report struct {
	OK                  bool                  `json:"ok"`
	HostOS              string                `json:"host_os"`
	WSL                 bool                  `json:"wsl"`
	WorkerPath          string                `json:"worker_path"`
	WorkerProcessMS     float64               `json:"worker_process_ms"`
	WindowsTargetProbe  transport.ProbeResult `json:"windows_target_probe"`
	NoFirewallPath      bool                  `json:"no_firewall_path"`
	NoAdministratorPath bool                  `json:"no_administrator_path"`
	Errors              []string              `json:"errors,omitempty"`
}

func Run(ctx context.Context, workerPath string, target endpoint.Endpoint, timeout time.Duration) Report {
	report := Report{
		HostOS:              runtime.GOOS,
		WSL:                 isWSL(),
		WorkerPath:          workerPath,
		NoFirewallPath:      true,
		NoAdministratorPath: true,
	}
	if runtime.GOOS != "linux" {
		report.Errors = append(report.Errors, "doctor is intended to run from the WSL/Linux side")
	}
	if !report.WSL {
		report.Errors = append(report.Errors, "WSL was not detected")
	}
	if _, err := os.Stat(workerPath); err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("Windows worker is unavailable: %v", err))
		return report
	}

	started := time.Now()
	cmd := exec.CommandContext(ctx, workerPath, "probe", "--target", target.String(), "--connect-timeout", timeout.String())
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	report.WorkerProcessMS = float64(time.Since(started).Microseconds()) / 1000
	if err != nil {
		message := fmt.Sprintf("Windows probe process failed: %v", err)
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			message += ": " + detail
		}
		report.Errors = append(report.Errors, message)
		return report
	}
	if err := json.Unmarshal(stdout.Bytes(), &report.WindowsTargetProbe); err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("decode Windows probe result: %v", err))
		return report
	}
	if !report.WindowsTargetProbe.OK {
		report.Errors = append(report.Errors, "Windows worker cannot connect to the requested loopback target: "+report.WindowsTargetProbe.Error)
		return report
	}
	report.OK = len(report.Errors) == 0
	return report
}

func WriteJSON(w io.Writer, report Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(report)
}

func isWSL() bool {
	data, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return false
	}
	value := strings.ToLower(string(data))
	return strings.Contains(value, "microsoft") || strings.Contains(value, "wsl")
}
