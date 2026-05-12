package main

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Hyprland manages the virtual output
type Hyprland struct {
	outputName string
	width      int
	height     int
	fps        int
}

// NewHyprland creates a new Hyprland manager
func NewHyprland(width, height, fps int) *Hyprland {
	return &Hyprland{
		outputName: "HEADLESS-1",
		width:      width,
		height:     height,
		fps:        fps,
	}
}

// Setup creates the headless output and positions it
func (h *Hyprland) Setup() error {
	// Create headless output
	cmd := exec.Command("hyprctl", "output", "create", "headless")
	if out, err := cmd.CombinedOutput(); err != nil {
		// If it already exists, that's okay
		if !strings.Contains(string(out), "exists") {
			return fmt.Errorf("failed to create headless output: %v: %s", err, string(out))
		}
	}

	// Wait a moment for the output to be ready
	// and find its actual name
	name, err := h.findHeadlessOutput()
	if err != nil {
		return err
	}
	h.outputName = name

	// Set resolution and position
	// Position it to the right of the primary monitor
	cmd = exec.Command("hyprctl", "keyword", "monitor", fmt.Sprintf("%s,%dx%d@%d,auto-right,1", h.outputName, h.width, h.height, h.fps))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to configure headless output: %v: %s", err, string(out))
	}

	return nil
}

// findHeadlessOutput finds the actual headless output name
func (h *Hyprland) findHeadlessOutput() (string, error) {
	cmd := exec.Command("hyprctl", "monitors")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Monitor ") && strings.Contains(line, "HEADLESS") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return strings.TrimSuffix(parts[1], ":"), nil
			}
		}
	}
	return "HEADLESS-1", nil // fallback
}

// GetOutputName returns the detected headless output name
func (h *Hyprland) GetOutputName() string {
	return h.outputName
}

// Cleanup removes the headless output
func (h *Hyprland) Cleanup() error {
	cmd := exec.Command("hyprctl", "output", "remove", h.outputName)
	if out, err := cmd.CombinedOutput(); err != nil {
		// Ignore "doesn't exist" errors
		if strings.Contains(string(out), "doesn't exist") {
			return nil
		}
		return fmt.Errorf("failed to remove headless output: %v: %s", err, string(out))
	}
	return nil
}

// GetMonitorID returns the numeric ID of the headless monitor for wf-recorder
func (h *Hyprland) GetMonitorID() (int, error) {
	cmd := exec.Command("hyprctl", "monitors", "-j")
	out, err := cmd.Output()
	if err != nil {
		return -1, err
	}
	// Simple parsing: find the monitor with our output name
	// The JSON output from hyprctl is an array of objects
	// We'll do a simple string search for "name":"HEADLESS-1" and extract "id":N
	text := string(out)
	idx := strings.Index(text, fmt.Sprintf(`"name":"%s"`, h.outputName))
	if idx == -1 {
		// Try without quotes spacing variations
		idx = strings.Index(text, fmt.Sprintf(`"name": "%s"`, h.outputName))
	}
	if idx == -1 {
		return 0, nil // fallback to 0
	}

	// Search backwards for "id":
	before := text[:idx]
	lastID := strings.LastIndex(before, `"id":`)
	if lastID == -1 {
		return 0, nil
	}
	idStr := strings.TrimSpace(before[lastID+5:])
	idStr = strings.Split(idStr, ",")[0]
	idStr = strings.Split(idStr, "}")[0]
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return 0, nil
	}
	return id, nil
}
