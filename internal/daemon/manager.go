package daemon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/christiandoxa/whwmpc/internal/state"
	"github.com/christiandoxa/whwmpc/internal/sysrun"
)

const service = "whwmpc-monitor.service"
const timer = "whwmpc-monitor.timer"
const marker = "# Managed by whwmpc monitor v1\n"

var unitPath = regexp.MustCompile(`^/[A-Za-z0-9/._ -]+$`)

type Manager struct {
	Dir    string
	Binary string
	Runner sysrun.Runner
}

func Default() (Manager, error) {
	if runtime.GOOS != "linux" {
		return Manager{}, errors.New("daemon memerlukan systemd user di Linux")
	}
	if os.Geteuid() == 0 {
		return Manager{}, errors.New("daemon dipasang sebagai pengguna biasa")
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return Manager{}, err
	}
	binary, err := os.Executable()
	if err != nil {
		return Manager{}, err
	}
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		return Manager{}, err
	}
	return Manager{Dir: filepath.Join(config, "systemd", "user"), Binary: binary, Runner: sysrun.Native{}}, nil
}

func units(binary string, interval time.Duration) (string, string, error) {
	if interval < 5*time.Minute || interval > 24*time.Hour || interval%time.Second != 0 {
		return "", "", errors.New("interval harus 5 menit hingga 24 jam dalam detik bulat")
	}
	if !unitPath.MatchString(binary) {
		return "", "", errors.New("path executable tidak dapat dipasang pada unit")
	}
	s := marker + fmt.Sprintf(`[Unit]
Description=whwmpc local observation

[Service]
Type=oneshot
ExecStart="%s" daemon run
TimeoutStartSec=90s
CPUQuota=20%%
MemoryMax=192M
TasksMax=32
Nice=10
NoNewPrivileges=yes
StandardOutput=null
StandardError=journal
LogRateLimitIntervalSec=1min
LogRateLimitBurst=8
`, binary)
	t := marker + fmt.Sprintf(`[Unit]
Description=Schedule whwmpc local observation

[Timer]
OnActiveSec=%ds
OnUnitInactiveSec=%ds
AccuracySec=30s

[Install]
WantedBy=timers.target
`, int64(interval/time.Second), int64(interval/time.Second))
	return s, t, nil
}

func (m Manager) command(ctx context.Context, args ...string) (string, error) {
	r := m.Runner.Run(ctx, "/usr/bin/systemctl", append([]string{"--user", "--no-pager"}, args...)...)
	if r.Err != nil {
		return "", r.Err
	}
	if r.ExitCode != 0 {
		return "", fmt.Errorf("systemctl (%d): %s", r.ExitCode, strings.TrimSpace(string(r.Stderr)))
	}
	return strings.TrimSpace(string(r.Output)), nil
}

func (m Manager) installed() error {
	for _, name := range []string{service, timer} {
		file, err := state.OpenRegular(filepath.Join(m.Dir, name))
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		buf := make([]byte, len(marker))
		n, readErr := file.Read(buf)
		file.Close()
		if readErr != nil || n != len(marker) || string(buf) != marker {
			return fmt.Errorf("unit %s bukan milik whwmpc", name)
		}
	}
	return nil
}

func (m Manager) Manage(ctx context.Context, verb string, interval time.Duration) (string, error) {
	servicePath, timerPath := filepath.Join(m.Dir, service), filepath.Join(m.Dir, timer)
	switch verb {
	case "install":
		s, t, err := units(m.Binary, interval)
		if err != nil {
			return "", err
		}
		if _, err = m.command(ctx, "show", "--property=Version"); err != nil {
			return "", err
		}
		if err = os.MkdirAll(m.Dir, 0o700); err != nil {
			return "", err
		}
		if err = state.WriteNew(servicePath, []byte(s)); err != nil {
			return "", err
		}
		if err = state.WriteNew(timerPath, []byte(t)); err != nil {
			return "", errors.Join(err, os.Remove(servicePath))
		}
		if _, err = m.command(ctx, "daemon-reload"); err != nil {
			return "", fmt.Errorf("unit ditulis; muat ulang systemd: %w", err)
		}
		return "Unit terpasang. Jalankan daemon enable dan daemon start untuk menjadwalkan observasi.", nil
	case "enable", "start", "disable", "stop", "status", "uninstall":
		if err := m.installed(); err != nil {
			return "", err
		}
		if verb == "status" {
			return m.command(ctx, "show", "--property=ActiveState,UnitFileState,NextElapseUSecMonotonic,LastTriggerUSec", timer)
		}
		if verb == "stop" {
			return m.command(ctx, verb, timer, service)
		}
		if verb != "uninstall" {
			return m.command(ctx, verb, timer)
		}
		if _, err := m.command(ctx, "stop", timer, service); err != nil {
			return "", err
		}
		if _, err := m.command(ctx, "disable", timer); err != nil {
			return "", err
		}
		for _, path := range []string{timerPath, servicePath} {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return "", err
			}
		}
		return m.command(ctx, "daemon-reload")
	default:
		return "", errors.New("operasi daemon tidak dikenal")
	}
}
