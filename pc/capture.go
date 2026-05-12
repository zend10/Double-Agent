package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Capture manages video and audio capture subprocesses
type Capture struct {
	width      int
	height     int
	fps        int
	outputName string
	videoCmd   *exec.Cmd
	audioCmd   *exec.Cmd
	videoPipe  string
	videoOut   chan TimestampedData
	audioOut   chan TimestampedData
	done       chan struct{}
	startTime  time.Time
}

// NewCapture creates a new capture manager
func NewCapture(width, height, fps int, outputName string) *Capture {
	return &Capture{
		width:      width,
		height:     height,
		fps:        fps,
		outputName: outputName,
		videoOut:   make(chan TimestampedData, 30),
		audioOut:   make(chan TimestampedData, 100),
		done:       make(chan struct{}),
		startTime:  time.Now(),
	}
}

func (c *Capture) now() int64 {
	return time.Since(c.startTime).Nanoseconds()
}

// Start begins video and audio capture
func (c *Capture) Start() error {
	if err := c.startVideo(); err != nil {
		return fmt.Errorf("video capture failed: %w", err)
	}

	// Audio is best-effort; don't fail if it doesn't work
	if err := c.startAudio(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: audio capture failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "continuing without audio...\n")
		go c.feedSilence()
	}

	return nil
}

func (c *Capture) startVideo() error {
	wfRecorder := "wf-recorder"

	// Create a named pipe for wf-recorder to write H.264 to
	c.videoPipe = "/tmp/double-agent-video-" + fmt.Sprintf("%d", os.Getpid()) + ".h264"
	os.Remove(c.videoPipe)
	if err := syscall.Mkfifo(c.videoPipe, 0600); err != nil {
		return fmt.Errorf("mkfifo failed: %w", err)
	}

	// wf-recorder captures and encodes directly to H.264 with VAAPI
	// -D disables damage tracking (capture full frame every time)
	args := []string{
		"-o", c.outputName,
		"-D", // disable damage tracking
		"-c", "h264_vaapi",
		"-d", "/dev/dri/renderD128",
		"-r", fmt.Sprintf("%d", c.fps),
		"--file", c.videoPipe,
	}

	c.videoCmd = exec.Command(wfRecorder, args...)
	c.videoCmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}

	if err := c.videoCmd.Start(); err != nil {
		os.Remove(c.videoPipe)
		return fmt.Errorf("start wf-recorder: %w", err)
	}

	// Wait a moment for wf-recorder to open the pipe for writing
	time.Sleep(500 * time.Millisecond)

	// Open pipe for reading
	pipe, err := os.OpenFile(c.videoPipe, os.O_RDONLY, 0)
	if err != nil {
		c.stopVideo()
		return fmt.Errorf("open video pipe: %w", err)
	}

	go c.readVideo(pipe)
	return nil
}

func (c *Capture) startAudio() error {
	monitorSource, err := c.findMonitorSource()
	if err != nil {
		return err
	}

	c.audioCmd = exec.Command("parec",
		"--rate=48000",
		"--channels=2",
		"--format=s16le",
		"--latency-msec=10",
		"--device="+monitorSource,
	)
	c.audioCmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}

	audioPipe, err := c.audioCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("parec stdout pipe: %w", err)
	}

	if err := c.audioCmd.Start(); err != nil {
		return fmt.Errorf("start parec: %w", err)
	}

	go c.readAudio(audioPipe)
	return nil
}

func (c *Capture) findMonitorSource() (string, error) {
	cmd := exec.Command("pactl", "info")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("pactl info failed: %w", err)
	}

	lines := string(out)
	var defaultSink string
	for _, line := range splitLines(lines) {
		if len(line) > 14 && line[:14] == "Default Sink: " {
			defaultSink = line[14:]
			break
		}
	}

	if defaultSink == "" {
		cmd = exec.Command("pactl", "list", "sources", "short")
		out, err = cmd.Output()
		if err != nil {
			return "", err
		}
		for _, line := range splitLines(string(out)) {
			fields := splitFields(line)
			if len(fields) >= 2 && contains(fields[1], ".monitor") {
				return fields[1], nil
			}
		}
		return "", fmt.Errorf("no monitor source found")
	}

	monitorName := defaultSink + ".monitor"

	cmd = exec.Command("pactl", "list", "sources", "short")
	out, err = cmd.Output()
	if err != nil {
		return "", err
	}
	for _, line := range splitLines(string(out)) {
		fields := splitFields(line)
		if len(fields) >= 2 && fields[1] == monitorName {
			return monitorName, nil
		}
	}

	for _, line := range splitLines(string(out)) {
		fields := splitFields(line)
		if len(fields) >= 2 && contains(fields[1], ".monitor") {
			return fields[1], nil
		}
	}

	return "", fmt.Errorf("monitor source not found for sink %s", defaultSink)
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func splitFields(s string) []string {
	var fields []string
	inField := false
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' || s[i] == '\t' {
			if inField {
				fields = append(fields, s[start:i])
				inField = false
			}
		} else {
			if !inField {
				start = i
				inField = true
			}
		}
	}
	if inField {
		fields = append(fields, s[start:])
	}
	return fields
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstr(s, substr) >= 0
}

func findSubstr(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func (c *Capture) readVideo(r io.Reader) {
	buf := make([]byte, 65536)
	pending := make([]byte, 0, 2*1024*1024)
	nalCount := 0

	for {
		select {
		case <-c.done:
			return
		default:
		}

		n, err := r.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)

			// Extract complete NAL units from pending buffer
			for {
				if len(pending) < 4 {
					break
				}

				// Find first start code
				startIdx := findNALStart(pending, 0)
				if startIdx < 0 {
					break
				}

				// Determine start code length (3 or 4 bytes)
				startCodeLen := 3
				if startIdx+3 < len(pending) && pending[startIdx+2] == 0 && pending[startIdx+3] == 1 {
					startCodeLen = 4
				}

				// Find next start code after current one
				nextStart := findNALStart(pending, startIdx+startCodeLen)
				if nextStart < 0 {
					break // need more data
				}

				// Extract NAL unit including start code
				nalLen := nextStart - startIdx
				nal := make([]byte, nalLen)
				copy(nal, pending[startIdx:nextStart])

				// Debug: print first 10 NAL types
				if nalCount < 10 {
					nalType := nal[startCodeLen] & 0x1F
					fmt.Fprintf(os.Stderr, "NAL #%d: type=%d size=%d\n", nalCount, nalType, nalLen)
					nalCount++
				}

				select {
				case c.videoOut <- TimestampedData{Timestamp: c.now(), Data: nal}:
				case <-c.done:
					return
				}

				pending = pending[nextStart:]
			}

			// Limit pending buffer size
			if len(pending) > 4*1024*1024 {
				// Keep last 2MB
				pending = append([]byte{}, pending[len(pending)-2*1024*1024:]...)
			}
		}

		if err != nil {
			if err != io.EOF {
				fmt.Fprintf(os.Stderr, "video read error: %v\n", err)
			}
			return
		}
	}
}

func findNALStart(data []byte, offset int) int {
	for i := offset; i < len(data)-3; i++ {
		if data[i] == 0 && data[i+1] == 0 {
			if data[i+2] == 1 {
				return i
			}
			if data[i+2] == 0 && i+3 < len(data) && data[i+3] == 1 {
				return i
			}
		}
	}
	return -1
}

func (c *Capture) readAudio(r io.Reader) {
	buf := make([]byte, 3840) // 20ms @ 48kHz stereo s16le

	for {
		select {
		case <-c.done:
			return
		default:
		}

		n, err := io.ReadFull(r, buf)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				if n > 0 {
					data := make([]byte, n)
					copy(data, buf[:n])
					select {
					case c.audioOut <- TimestampedData{Timestamp: c.now(), Data: data}:
					case <-c.done:
						return
					}
				}
				return
			}
			fmt.Fprintf(os.Stderr, "audio read error: %v\n", err)
			return
		}

		data := make([]byte, 3840)
		copy(data, buf)

		select {
		case c.audioOut <- TimestampedData{Timestamp: c.now(), Data: data}:
		case <-c.done:
			return
		}
	}
}

func (c *Capture) feedSilence() {
	silence := make([]byte, 3840)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			select {
			case c.audioOut <- TimestampedData{Timestamp: c.now(), Data: silence}:
			case <-c.done:
				return
			}
		}
	}
}

// Stop terminates all capture processes
func (c *Capture) Stop() {
	close(c.done)
	c.stopVideo()
	c.stopAudio()
}

func (c *Capture) stopVideo() {
	if c.videoCmd != nil && c.videoCmd.Process != nil {
		c.videoCmd.Process.Signal(syscall.SIGTERM)
		c.videoCmd.Wait()
	}
	if c.videoPipe != "" {
		os.Remove(c.videoPipe)
	}
}

func (c *Capture) stopAudio() {
	if c.audioCmd != nil && c.audioCmd.Process != nil {
		c.audioCmd.Process.Signal(syscall.SIGTERM)
		c.audioCmd.Wait()
	}
}

// VideoChan returns the video output channel
func (c *Capture) VideoChan() <-chan TimestampedData {
	return c.videoOut
}

// AudioChan returns the audio output channel
func (c *Capture) AudioChan() <-chan TimestampedData {
	return c.audioOut
}
