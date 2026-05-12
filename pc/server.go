package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Server handles TCP connections from the Android client
type Server struct {
	addr       string
	listener   net.Listener
	client     net.Conn
	clientMu   sync.RWMutex
	videoInfo  VideoInfo
	audioInfo  AudioInfo
	stop       chan struct{}
	wg         sync.WaitGroup
}

// NewServer creates a new server
func NewServer(addr string, video VideoInfo, audio AudioInfo) *Server {
	return &Server{
		addr:      addr,
		videoInfo: video,
		audioInfo: audio,
		stop:      make(chan struct{}),
	}
}

// Start begins listening for connections
func (s *Server) Start() error {
	// Setup ADB reverse port forwarding
	fmt.Println("Setting up ADB reverse port forwarding...")
	adbCmd := exec.Command("adb", "reverse", fmt.Sprintf("tcp:%s", s.port()), fmt.Sprintf("tcp:%s", s.port()))
	if out, err := adbCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "ADB reverse warning: %v: %s\n", err, string(out))
		fmt.Println("Make sure your tablet is connected via USB with ADB enabled.")
	} else {
		fmt.Println("ADB reverse tunnel established.")
	}

	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	s.listener = ln
	
	fmt.Printf("Server listening on %s\n", s.addr)
	
	s.wg.Add(1)
	go s.acceptLoop()
	
	return nil
}

func (s *Server) port() string {
	_, port, _ := net.SplitHostPort(s.addr)
	if port == "" {
		return "7777"
	}
	return port
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.stop:
				return
			default:
				fmt.Fprintf(os.Stderr, "accept error: %v\n", err)
				time.Sleep(time.Second)
				continue
			}
		}
		
		fmt.Printf("Client connected: %s\n", conn.RemoteAddr())
		
		s.clientMu.Lock()
		if s.client != nil {
			s.client.Close()
		}
		s.client = conn
		s.clientMu.Unlock()
		
		s.wg.Add(1)
		go s.handleClient(conn)
	}
}

func (s *Server) handleClient(conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		s.clientMu.Lock()
		if s.client == conn {
			s.client = nil
		}
		s.clientMu.Unlock()
		conn.Close()
		fmt.Println("Client disconnected.")
	}()
	
	// Send video info
	if err := WriteMessage(conn, MsgVideoInfo, EncodeVideoInfo(s.videoInfo)); err != nil {
		fmt.Fprintf(os.Stderr, "failed to send video info: %v\n", err)
		return
	}
	
	// Send audio info
	if err := WriteMessage(conn, MsgAudioInfo, EncodeAudioInfo(s.audioInfo)); err != nil {
		fmt.Fprintf(os.Stderr, "failed to send audio info: %v\n", err)
		return
	}
	
	// Read touch events from client
	for {
		select {
		case <-s.stop:
			return
		default:
		}
		
		msgType, payload, err := ReadMessage(conn)
		if err != nil {
			if err != io.EOF {
				fmt.Fprintf(os.Stderr, "read error: %v\n", err)
			}
			return
		}
		
		switch msgType {
		case MsgTouchEvent:
			// Touch input disabled - consume payload
			_ = payload
		case MsgHeartbeat:
			// Ignore or use for latency calculation
		default:
			fmt.Fprintf(os.Stderr, "unknown message type: 0x%02x\n", msgType)
		}
	}
}

// SendVideo sends a video frame to the connected client
func (s *Server) SendVideo(frame []byte) error {
	s.clientMu.RLock()
	client := s.client
	s.clientMu.RUnlock()

	if client == nil {
		return nil // no client connected
	}

	return WriteMessage(client, MsgVideoFrame, frame)
}

// SendAudio sends an audio packet to the connected client
func (s *Server) SendAudio(packet []byte) error {
	s.clientMu.RLock()
	client := s.client
	s.clientMu.RUnlock()
	
	if client == nil {
		return nil
	}
	
	return WriteMessage(client, MsgAudioPacket, packet)
}

// Stop shuts down the server
func (s *Server) Stop() {
	close(s.stop)
	if s.listener != nil {
		s.listener.Close()
	}
	s.clientMu.Lock()
	if s.client != nil {
		s.client.Close()
	}
	s.clientMu.Unlock()
	s.wg.Wait()
}

// HasClient returns true if a client is connected
func (s *Server) HasClient() bool {
	s.clientMu.RLock()
	defer s.clientMu.RUnlock()
	return s.client != nil
}
