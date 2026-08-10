package alerts

import (
	"testing"

	"lighthouse/db"

	"github.com/moby/moby/api/types/events"
)

func TestAuditLogCallback(t *testing.T) {
	setupTestDBExtra(t)
	am := NewAlertManager(nil)
	am.Start()
	db.OnAuditLogged("action", "resource", "status", "details")
	am.Stop()
}

func TestReloadRulesRegexErrors(t *testing.T) {
	setupTestDBExtra(t)
	db.GormDB.Create(&db.AlertRule{
		Name:             "Bad Container",
		ContainerPattern: "[invalid",
		Enabled:          true,
	})
	db.GormDB.Create(&db.AlertRule{
		Name:             "Bad Log",
		ContainerPattern: ".*",
		LogPattern:       "[invalid",
		Enabled:          true,
	})

	am := NewAlertManager(nil)
	am.ReloadRules()
}

func TestProcessContainerEventFull(t *testing.T) {
	am := NewAlertManager(nil)
	am.rules = map[int64]*AlertRule{
		1: {
			EventTypes:       "start,die,health_status",
			ContainerPattern: ".*",
		},
	}

	// Test without name but with ID
	am.processContainerEvent(events.Message{
		Action: "die",
		Actor: events.Actor{
			Attributes: map[string]string{},
			ID:         "1234567890123456",
		},
	})

	// Test down state and recovery
	am.processContainerEvent(events.Message{
		Action: "die",
		Actor: events.Actor{
			Attributes: map[string]string{"name": "test"},
		},
	})
	am.processContainerEvent(events.Message{
		Action: "start",
		Actor: events.Actor{
			Attributes: map[string]string{"name": "test"},
		},
	})
}

func TestModelsNilRegex(t *testing.T) {
	rule := &AlertRule{}
	rule.matchesContainer("test")
	rule.matchesLog("test")
}
