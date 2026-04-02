package main

import (
	"fmt"
	"sync"
	"time"
)

const maxWindowSize = 50

type ToolEntry struct {
	ToolName   string    `json:"tool"`
	Timestamp  time.Time `json:"timestamp"`
	Success    bool      `json:"success"`
	TokensUsed int       `json:"tokens_used"`
}

type Anomaly struct {
	Detector string      `json:"detector"`
	Detail   string      `json:"detail"`
	Severity string      `json:"severity"`
	Window   []ToolEntry `json:"window,omitempty"`
}

type DetectorConfig struct {
	Threshold               int    `yaml:"threshold" json:"threshold"`
	Severity                string `yaml:"severity" json:"severity"`
	WindowMinutes           int    `yaml:"window_minutes,omitempty" json:"window_minutes,omitempty"`
	ExpectedDurationMinutes int    `yaml:"expected_duration_minutes,omitempty" json:"expected_duration_minutes,omitempty"`
	BudgetPct               int    `yaml:"budget_pct,omitempty" json:"budget_pct,omitempty"`
	TimePct                 int    `yaml:"time_pct,omitempty" json:"time_pct,omitempty"`
}

type TrajectoryConfig struct {
	Enabled         bool                      `yaml:"enabled" json:"enabled"`
	Detectors       map[string]DetectorConfig `yaml:"detectors" json:"detectors"`
	OnCritical      string                    `yaml:"on_critical" json:"on_critical"`
	CooldownMinutes int                       `yaml:"cooldown_minutes" json:"cooldown_minutes"`
}

type TrajectoryMonitor struct {
	mu        sync.Mutex
	window    []ToolEntry
	config    TrajectoryConfig
	cooldowns map[string]time.Time
}

func DefaultTrajectoryConfig() TrajectoryConfig {
	return TrajectoryConfig{
		Enabled: true,
		Detectors: map[string]DetectorConfig{
			"tool_repetition": {
				Threshold: 5,
				Severity:  "warning",
			},
			"tool_cycle": {
				Threshold: 4,
				Severity:  "warning",
			},
			"error_cascade": {
				Threshold:     5,
				WindowMinutes: 2,
				Severity:      "warning",
			},
		},
		OnCritical:      "alert",
		CooldownMinutes: 5,
	}
}

func NewTrajectoryMonitor(config TrajectoryConfig) *TrajectoryMonitor {
	return &TrajectoryMonitor{
		config:    config,
		cooldowns: make(map[string]time.Time),
	}
}

// TrajectoryState is the JSON-serializable snapshot of the trajectory monitor.
type TrajectoryState struct {
	Agent           string                       `json:"agent"`
	Enabled         bool                         `json:"enabled"`
	WindowSize      int                          `json:"window_size"`
	CurrentEntries  int                          `json:"current_entries"`
	ActiveAnomalies []Anomaly                    `json:"active_anomalies"`
	Detectors       map[string]DetectorStatus    `json:"detectors"`
}

// DetectorStatus reports the current status of a single detector.
type DetectorStatus struct {
	Status    string  `json:"status"`
	LastFired *string `json:"last_fired"`
}

// GetState returns a snapshot of the current trajectory monitor state.
func (tm *TrajectoryMonitor) GetState(agentName string) TrajectoryState {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	anomalies := tm.RunDetectorsLocked()

	detectors := make(map[string]DetectorStatus)
	for name := range tm.config.Detectors {
		status := "idle"
		var lastFired *string
		if coolUntil, ok := tm.cooldowns[name]; ok {
			if time.Now().Before(coolUntil) {
				status = "cooldown"
			}
			t := coolUntil.Add(-time.Duration(tm.config.CooldownMinutes) * time.Minute).Format(time.RFC3339)
			lastFired = &t
		}
		detectors[name] = DetectorStatus{Status: status, LastFired: lastFired}
	}

	return TrajectoryState{
		Agent:           agentName,
		Enabled:         tm.config.Enabled,
		WindowSize:      maxWindowSize,
		CurrentEntries:  len(tm.window),
		ActiveAnomalies: anomalies,
		Detectors:       detectors,
	}
}

// RunDetectorsLocked runs detectors without acquiring the lock (caller must hold it).
func (tm *TrajectoryMonitor) RunDetectorsLocked() []Anomaly {
	if len(tm.window) == 0 {
		return nil
	}

	window := tm.window
	detectorFuncs := map[string]func([]ToolEntry, DetectorConfig) *Anomaly{
		"tool_repetition": detectToolRepetition,
		"tool_cycle":      detectToolCycle,
		"error_cascade":   detectErrorCascade,
	}

	var anomalies []Anomaly
	for name, fn := range detectorFuncs {
		cfg, ok := tm.config.Detectors[name]
		if !ok {
			continue
		}
		anomaly := fn(window, cfg)
		if anomaly != nil {
			anomaly.Detector = name
			anomalies = append(anomalies, *anomaly)
		}
	}
	return anomalies
}

func (tm *TrajectoryMonitor) RecordToolCall(entry ToolEntry) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.window = append(tm.window, entry)
	if len(tm.window) > maxWindowSize {
		tm.window = tm.window[len(tm.window)-maxWindowSize:]
	}
}

func (tm *TrajectoryMonitor) RunDetectors() []Anomaly {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if len(tm.window) == 0 {
		return nil
	}

	now := time.Now()
	window := tm.window

	detectorFuncs := map[string]func([]ToolEntry, DetectorConfig) *Anomaly{
		"tool_repetition": detectToolRepetition,
		"tool_cycle":      detectToolCycle,
		"error_cascade":   detectErrorCascade,
	}

	var anomalies []Anomaly
	for name, fn := range detectorFuncs {
		cfg, ok := tm.config.Detectors[name]
		if !ok {
			continue
		}

		// Check cooldown
		if coolUntil, inCooldown := tm.cooldowns[name]; inCooldown {
			if now.Before(coolUntil) {
				continue
			}
			delete(tm.cooldowns, name)
		}

		anomaly := fn(window, cfg)
		if anomaly != nil {
			anomaly.Detector = name
			anomalies = append(anomalies, *anomaly)
			cooldown := time.Duration(tm.config.CooldownMinutes) * time.Minute
			tm.cooldowns[name] = now.Add(cooldown)
		}
	}

	if len(anomalies) == 0 {
		return nil
	}
	return anomalies
}

func detectToolRepetition(window []ToolEntry, cfg DetectorConfig) *Anomaly {
	if len(window) == 0 {
		return nil
	}

	last := window[len(window)-1].ToolName
	count := 0
	for i := len(window) - 1; i >= 0; i-- {
		if window[i].ToolName == last {
			count++
		} else {
			break
		}
	}

	if count >= cfg.Threshold {
		return &Anomaly{
			Detail:   fmt.Sprintf("tool %q called %d times consecutively (threshold %d)", last, count, cfg.Threshold),
			Severity: cfg.Severity,
		}
	}
	return nil
}

func detectToolCycle(window []ToolEntry, cfg DetectorConfig) *Anomaly {
	if len(window) < 2 {
		return nil
	}

	// Try pattern lengths 2 and 3, shortest match wins
	for patLen := 2; patLen <= 3; patLen++ {
		if len(window) < patLen*2 {
			continue
		}

		// Extract candidate pattern from window tail
		pattern := make([]string, patLen)
		for i := 0; i < patLen; i++ {
			pattern[i] = window[len(window)-patLen+i].ToolName
		}

		// Count consecutive repetitions scanning backward
		count := 1
		pos := len(window) - patLen
		for pos >= patLen {
			match := true
			for i := 0; i < patLen; i++ {
				if window[pos-patLen+i].ToolName != pattern[i] {
					match = false
					break
				}
			}
			if !match {
				break
			}
			count++
			pos -= patLen
		}

		if count >= cfg.Threshold {
			return &Anomaly{
				Detail:   fmt.Sprintf("tool cycle of length %d repeated %d times (threshold %d)", patLen, count, cfg.Threshold),
				Severity: cfg.Severity,
			}
		}
	}

	return nil
}

func detectErrorCascade(window []ToolEntry, cfg DetectorConfig) *Anomaly {
	if len(window) == 0 {
		return nil
	}

	windowDur := time.Duration(cfg.WindowMinutes) * time.Minute
	cutoff := window[len(window)-1].Timestamp.Add(-windowDur)

	errorCount := 0
	for i := len(window) - 1; i >= 0; i-- {
		if window[i].Timestamp.Before(cutoff) {
			break
		}
		if !window[i].Success {
			errorCount++
		}
	}

	if errorCount >= cfg.Threshold {
		return &Anomaly{
			Detail:   fmt.Sprintf("%d errors in %d minute window (threshold %d)", errorCount, cfg.WindowMinutes, cfg.Threshold),
			Severity: cfg.Severity,
		}
	}
	return nil
}
