package instance_service

import (
	"errors"
	"testing"
	"time"

	"github.com/EvolutionAPI/evolution-go/pkg/config"
	instance_model "github.com/EvolutionAPI/evolution-go/pkg/instance/model"
	logger_wrapper "github.com/EvolutionAPI/evolution-go/pkg/logger"
	whatsmeow_service "github.com/EvolutionAPI/evolution-go/pkg/whatsmeow/service"
)

type mockWhatsmeowService struct {
	touchCount   int
	lastInstance string
	touchErr     error
}

func (m *mockWhatsmeowService) StartClient(_ *whatsmeow_service.ClientData) {}
func (m *mockWhatsmeowService) ConnectOnStartup(_ string)                   {}
func (m *mockWhatsmeowService) StartInstance(_ string) error                { return nil }
func (m *mockWhatsmeowService) ReconnectClient(_ string) error              { return nil }
func (m *mockWhatsmeowService) ClearInstanceCache(_ string, _ string) error {
	return nil
}
func (m *mockWhatsmeowService) CallWebhook(_ *instance_model.Instance, _ string, _ []byte) {
}
func (m *mockWhatsmeowService) SendToGlobalQueues(_ string, _ []byte, _ string) {}
func (m *mockWhatsmeowService) ForceUpdateJid(_ string, _ string) error         { return nil }
func (m *mockWhatsmeowService) UpdateInstanceSettings(_ string) error           { return nil }
func (m *mockWhatsmeowService) UpdateInstanceAdvancedSettings(_ string) error   { return nil }
func (m *mockWhatsmeowService) TouchActivity(instanceId string) error {
	m.touchCount++
	m.lastInstance = instanceId
	return m.touchErr
}

func newTestLoggerManager(t *testing.T) *logger_wrapper.LoggerManager {
	t.Helper()
	cfg := &config.Config{
		LogDirectory:  t.TempDir(),
		LogMaxSize:    1,
		LogMaxBackups: 1,
		LogMaxAge:     1,
		LogCompress:   false,
	}
	return logger_wrapper.NewLoggerManager(cfg)
}

func TestInstanceServiceTouchActivityUpdatesState(t *testing.T) {
	mockSvc := &mockWhatsmeowService{}
	logger := newTestLoggerManager(t)
	t.Cleanup(func() {
		_ = logger.GetLogger("instance-1").Close()
	})
	svc := &instances{
		whatsmeowService: mockSvc,
		loggerWrapper:    logger,
	}

	instance := &instance_model.Instance{Id: "instance-1"}

	if err := svc.TouchActivity(instance); err != nil {
		t.Fatalf("TouchActivity returned error: %v", err)
	}

	if mockSvc.touchCount != 1 {
		t.Fatalf("expected touch to be called once, got %d", mockSvc.touchCount)
	}

	if mockSvc.lastInstance != instance.Id {
		t.Fatalf("expected touch for %s, got %s", instance.Id, mockSvc.lastInstance)
	}

	if instance.LastActivityAt == nil || time.Since(*instance.LastActivityAt) > time.Second {
		t.Fatalf("expected LastActivityAt to be set recently, got %v", instance.LastActivityAt)
	}
}

func TestInstanceServiceTouchActivityPropagatesError(t *testing.T) {
	mockSvc := &mockWhatsmeowService{touchErr: errors.New("fail")}
	logger := newTestLoggerManager(t)
	t.Cleanup(func() {
		_ = logger.GetLogger("instance-err").Close()
	})
	svc := &instances{
		whatsmeowService: mockSvc,
		loggerWrapper:    logger,
	}

	instance := &instance_model.Instance{Id: "instance-err"}

	err := svc.TouchActivity(instance)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	if instance.LastActivityAt != nil {
		t.Fatalf("expected last activity to remain nil on error")
	}
}
