//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/viettechno/vietgara-backend/internal/printagent/agent"
)

// errAdministrator asks for an elevated prompt.
var errAdministrator = errors.New("hãy mở Command Prompt bằng \"Run as administrator\" rồi chạy lại")

const (
	serviceSupported = true
	serviceName      = "VietGaraAgent"
	serviceDisplay   = "VietGara Print Agent"
)

func isService() bool {
	ok, err := svc.IsWindowsService()

	return err == nil && ok
}

// statePath is %VIETGARA_AGENT_STATE%, or %ProgramData%\VietGara\agent:
// the service runs as LocalSystem, and the directory is readable by the
// system and the administrators only.
func statePath() string {
	if p := os.Getenv("VIETGARA_AGENT_STATE"); p != "" {
		return p
	}

	base := os.Getenv("ProgramData")
	if base == "" {
		base = `C:\ProgramData`
	}

	return filepath.Join(base, "VietGara", "agent", "state.json")
}

// prepareStateDir creates the state's directory with an access list of the
// system and the administrators only, replacing the inherited one (any user
// may read %ProgramData%).
func prepareStateDir(store *agent.Store) error {
	if err := store.EnsureDir(); err != nil {
		return err
	}

	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if err != nil {
		return fmt.Errorf("access list: %w", err)
	}

	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("access list: %w", err)
	}

	err = windows.SetNamedSecurityInfo(filepath.Dir(store.Path), windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return errAdministrator
	}

	if err != nil {
		return fmt.Errorf("protect %s: %w", filepath.Dir(store.Path), err)
	}

	return nil
}

func installService(out io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find the agent: %w", err)
	}

	m, err := mgr.Connect()
	if err != nil {
		return errAdministrator
	}
	defer m.Disconnect() //nolint:errcheck // nothing to do

	s, err := m.OpenService(serviceName)
	if err == nil {
		// Installed already (a new pairing, or a new version extracted
		// elsewhere): restart it from this executable with the new key.
		defer func() { _ = s.Close() }()

		_, _ = s.Control(svc.Stop)
		waitStopped(s)

		cfg, err := s.Config()
		if err != nil {
			return fmt.Errorf("read the service: %w", err)
		}

		cfg.BinaryPathName = exe
		if err := s.UpdateConfig(cfg); err != nil {
			return fmt.Errorf("update the service: %w", err)
		}
	} else {
		s, err = m.CreateService(serviceName, exe, mgr.Config{
			DisplayName: serviceDisplay, StartType: mgr.StartAutomatic,
			Description: "Prints the documents of VietGara on this PC's printers.",
		})
		if err != nil {
			return fmt.Errorf("install the service: %w", err)
		}
		defer func() { _ = s.Close() }()

		_ = s.SetRecoveryActions([]mgr.RecoveryAction{
			{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
			{Type: mgr.ServiceRestart, Delay: time.Minute},
			{Type: mgr.ServiceRestart, Delay: 5 * time.Minute},
		}, uint32((24 * time.Hour).Seconds()))
	}

	if err := s.Start(); err != nil {
		return fmt.Errorf("start the service: %w", err)
	}

	say(out, "Đã cài và chạy dịch vụ \"%s\": tác nhân tự chạy khi bật máy.", serviceDisplay)

	return nil
}

func uninstallService(out io.Writer) error {
	m, err := mgr.Connect()
	if err != nil {
		return errAdministrator
	}
	defer m.Disconnect() //nolint:errcheck // nothing to do

	s, err := m.OpenService(serviceName)
	if err != nil {
		say(out, "Dịch vụ chưa được cài.")

		return nil
	}
	defer func() { _ = s.Close() }()

	_, _ = s.Control(svc.Stop)
	waitStopped(s)

	if err := s.Delete(); err != nil {
		return fmt.Errorf("remove the service: %w", err)
	}

	say(out, "Đã gỡ dịch vụ. Xoá tác nhân trên VietGara để thu hồi khoá của nó.")

	return nil
}

// waitStopped waits until the service stops, up to three minutes: it
// finishes the print in progress first (up to two).
func waitStopped(s *mgr.Service) {
	for range 360 {
		st, err := s.Query()
		if err != nil || st.State == svc.Stopped {
			return
		}

		time.Sleep(500 * time.Millisecond)
	}
}

func serviceStatus() string {
	m, err := mgr.Connect()
	if err != nil {
		return "không rõ (cần quyền quản trị)"
	}
	defer m.Disconnect() //nolint:errcheck // nothing to do

	s, err := m.OpenService(serviceName)
	if err != nil {
		return "chưa cài"
	}
	defer func() { _ = s.Close() }()

	st, err := s.Query()
	if err != nil {
		return "không rõ"
	}

	if st.State == svc.Running {
		return "đang chạy"
	}

	return "đã dừng"
}

// runService runs the agent as the Windows service, logging to agent.log.
func runService() error {
	f, err := logFile()
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	return svc.Run(serviceName, &handler{log: slog.New(slog.NewTextHandler(f, nil))}) //nolint:wrapcheck // the service's end
}

type handler struct{ log *slog.Logger }

// Execute serves the service until it is stopped.
func (h *handler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- newAgent(h.log).Run(ctx) }()

	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case err := <-done:
			cancel()

			if err != nil {
				h.log.Error("the agent stopped", "error", err)

				return false, 1
			}

			return false, 0
		case r := <-requests:
			switch r.Cmd { //nolint:exhaustive // the service accepts these only
			case svc.Interrogate:
				status <- r.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}

				cancel()

				if err := <-done; err != nil {
					h.log.Error("the agent stopped", "error", err)
				}

				return false, 0
			}
		}
	}
}

// logFile opens the service's log next to the state, starting it over when
// it grew past 5 MB.
func logFile() (*os.File, error) {
	path := filepath.Join(filepath.Dir(statePath()), "agent.log")

	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if info, err := os.Stat(path); err == nil && info.Size() > 5<<20 {
		flags |= os.O_TRUNC
	}

	f, err := os.OpenFile(path, flags, 0o600) //nolint:gosec // the agent's own log, next to its state
	if err != nil {
		return nil, fmt.Errorf("open the log %s: %w", path, err)
	}

	return f, nil
}
