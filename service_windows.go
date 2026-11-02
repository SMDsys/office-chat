//go:build windows

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "OfficeChat"

func isWindowsService() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

type chatService struct {
	cfg Config
	log *log.Logger
	rt  *runtime
}

func (s *chatService) Execute(_ []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	rt, err := startServer(s.cfg, s.log)
	if err != nil {
		s.log.Printf("служба: %v", err)
		return false, 1
	}
	s.rt = rt
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for c := range r {
		switch c.Cmd {
		case svc.Interrogate:
			status <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			status <- svc.Status{State: svc.StopPending}
			rt.Stop()
			return false, 0
		}
	}
	rt.Stop()
	return false, 0
}

func runAsService(cfg Config, logger *log.Logger) error {
	return svc.Run(serviceName, &chatService{cfg: cfg, log: logger})
}

func installService(exeDir string) error {
	exe := filepath.Join(exeDir, "OfficeChat.exe")
	if _, err := os.Stat(exe); err != nil {
		exe, err = os.Executable()
		if err != nil {
			return err
		}
	}
	abs, err := filepath.Abs(exe)
	if err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("нет доступа к службам (запустите от имени администратора): %w", err)
	}
	defer m.Disconnect()

	cfg := mgr.Config{
		DisplayName: "Офис-чат",
		StartType:   mgr.StartAutomatic,
	}

	s, err := m.OpenService(serviceName)
	if err == nil {
		// Служба уже установлена: останавливаем, обновляем путь, запускаем.
		if st, qerr := s.Query(); qerr == nil && st.State != svc.Stopped {
			_, _ = s.Control(svc.Stop)
			deadline := time.Now().Add(15 * time.Second)
			for time.Now().Before(deadline) {
				st, qerr = s.Query()
				if qerr != nil || st.State == svc.Stopped {
					break
				}
				time.Sleep(300 * time.Millisecond)
			}
		}
		cfg.BinaryPathName = fmt.Sprintf("\"%s\" -service", abs)
		if err := s.UpdateConfig(cfg); err != nil {
			s.Close()
			return fmt.Errorf("обновление службы: %w", err)
		}
	} else {
		cfg.BinaryPathName = fmt.Sprintf("\"%s\" -service", abs)
		s, err = m.CreateService(serviceName, abs, cfg, "-service")
		if err != nil {
			return fmt.Errorf("создание службы: %w", err)
		}
	}
	defer s.Close()

	_ = s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
	}, 86400)

	if st, qerr := s.Query(); qerr == nil && st.State == svc.Running {
		_, _ = s.Control(svc.Stop)
		time.Sleep(2 * time.Second)
	}
	if err := s.Start(); err != nil {
		return fmt.Errorf("запуск службы: %w", err)
	}
	return nil
}

func uninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("нет доступа к службам (запустите от имени администратора): %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return errors.New("служба не установлена")
	}
	defer s.Close()
	_, _ = s.Control(svc.Stop)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		st, qerr := s.Query()
		if qerr != nil || st.State == svc.Stopped {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if err := s.Delete(); err != nil {
		return err
	}
	return nil
}
