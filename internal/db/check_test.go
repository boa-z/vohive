package db

import (
	"path/filepath"
	"testing"
)

func TestCheckSchema(t *testing.T) {
	oldDB := DB
	t.Cleanup(func() {
		if DB != nil && DB != oldDB {
			if sqlDB, err := DB.DB(); err == nil && sqlDB != nil {
				_ = sqlDB.Close()
			}
		}
		DB = oldDB
	})

	if err := Init(filepath.Join(t.TempDir(), "schema.db")); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	checks := []struct {
		name  string
		model any
	}{
		{"devices", &Device{}},
		{"sim_cards", &SIMCard{}},
		{"sim_subscriptions", &SIMSubscription{}},
		{"pending_phone_numbers", &PendingPhoneNumber{}},
		{"sms", &SMS{}},
		{"sms_contacts", &SMSContact{}},
	}

	for _, check := range checks {
		if !DB.Migrator().HasTable(check.model) {
			t.Errorf("Init() did not create %s table", check.name)
		}
	}
}
