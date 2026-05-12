package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

func main() {
	// Default configuration (matches tablet: 1920x1200 in landscape)
	width := 1920
	height := 1200
	fps := 60
	addr := ":7777"

	fmt.Println("Double Agent - PC Server")
	fmt.Printf("Resolution: %dx%d @ %d FPS\n", width, height, fps)
	fmt.Printf("Listening on: %s\n", addr)

	// Setup Hyprland headless output
	hyp := NewHyprland(width, height, fps)
	if err := hyp.Setup(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to setup Hyprland: %v\n", err)
		os.Exit(1)
	}
	defer hyp.Cleanup()

	outputName := hyp.GetOutputName()
	fmt.Printf("Created headless output: %s\n", outputName)

	// Create server
	videoInfo := VideoInfo{
		Width:  uint16(width),
		Height: uint16(height),
		FPS:    uint8(fps),
	}
	audioInfo := AudioInfo{
		SampleRate: 48000,
		Channels:   2,
		Bits:       16,
	}

	server := NewServer(addr, videoInfo, audioInfo)
	if err := server.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start server: %v\n", err)
		os.Exit(1)
	}
	defer server.Stop()

	// Handle shutdown signals concurrently
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutdown signal received...")
		cancel()
	}()

	// Create capture
	capture := NewCapture(width, height, fps, outputName)

	// Wait for client before starting capture (but allow interrupt)
	fmt.Println("Waiting for Android client to connect...")
	for !server.HasClient() {
		select {
		case <-ctx.Done():
			fmt.Println("Exiting before client connected.")
			return
		case <-time.After(100 * time.Millisecond):
		}
	}

	fmt.Println("Client connected. Starting capture...")
	if err := capture.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start capture: %v\n", err)
		os.Exit(1)
	}
	defer capture.Stop()

	fmt.Println("Capture started. Streaming...")

	// Forward video and audio
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for {
			select {
			case td := <-capture.VideoChan():
				if td.Data == nil {
					return
				}
				payload := PrependTimestamp(td.Data, td.Timestamp)
				if err := server.SendVideo(payload); err != nil {
					fmt.Fprintf(os.Stderr, "video send error: %v\n", err)
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		for {
			select {
			case td := <-capture.AudioChan():
				if td.Data == nil {
					return
				}
				payload := PrependTimestamp(td.Data, td.Timestamp)
				if err := server.SendAudio(payload); err != nil {
					fmt.Fprintf(os.Stderr, "audio send error: %v\n", err)
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Wait for shutdown
	<-ctx.Done()
	fmt.Println("Shutting down...")
	wg.Wait()
}
