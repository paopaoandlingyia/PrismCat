package config

import (
	"testing"
	"time"
)

func TestValidateArchiveKeyPrefix(t *testing.T) {
	valid := []string{
		"backups/prismcat",
		"backups/prismcat/${yyyy}",
		"backups/${yyyy}/${MM}-${dd}",
		"backups/${yyyy}/${yyyy}-${MM}-${dd}",
	}
	for _, prefix := range valid {
		if err := ValidateArchiveKeyPrefix(prefix); err != nil {
			t.Errorf("%q: %v", prefix, err)
		}
	}
	invalid := []string{"", "/backups", "backups/", "backups//x", "backups/../x", "backups/${month}", "backups/bad}"}
	for _, prefix := range invalid {
		if err := ValidateArchiveKeyPrefix(prefix); err == nil {
			t.Errorf("%q unexpectedly valid", prefix)
		}
	}
}

func TestValidateArchiveConfigDoesNotSilentlyNormalizeInvalidValues(t *testing.T) {
	base := ArchiveConfig{
		KeyPrefix: "backups/prismcat", ScheduleTime: "02:00", Timezone: "Asia/Shanghai",
		ZstdLevel: 10, LocalRetentionHours: 24, ImportRetentionHours: 24,
	}
	tests := []ArchiveConfig{
		func() ArchiveConfig { v := base; v.KeyPrefix = ""; return v }(),
		func() ArchiveConfig { v := base; v.ScheduleTime = "25:00"; return v }(),
		func() ArchiveConfig { v := base; v.Timezone = "Not/A-Timezone"; return v }(),
		func() ArchiveConfig { v := base; v.ZstdLevel = 20; return v }(),
		func() ArchiveConfig { v := base; v.LocalRetentionHours = 0; return v }(),
	}
	for _, cfg := range tests {
		if err := ValidateArchiveConfig(cfg); err == nil {
			t.Errorf("invalid archive config unexpectedly accepted: %#v", cfg)
		}
	}
}

func TestResolveArchiveKeyPrefix(t *testing.T) {
	day := time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC)
	got := ResolveArchiveKeyPrefix("backups/${yyyy}/${MM}-${dd}", day)
	if got != "backups/2026/08-17" {
		t.Fatalf("resolved = %q", got)
	}
}
