package main

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Message types
const (
	MsgVideoInfo   byte = 0x01
	MsgVideoFrame  byte = 0x02
	MsgAudioInfo   byte = 0x03
	MsgAudioPacket byte = 0x04
	MsgTouchEvent  byte = 0x05
	MsgHeartbeat   byte = 0x06
)

// Touch actions
const (
	TouchDown byte = 0x00
	TouchMove byte = 0x01
	TouchUp   byte = 0x02
)

// VideoInfo payload
type VideoInfo struct {
	Width  uint16
	Height uint16
	FPS    uint8
}

// AudioInfo payload
type AudioInfo struct {
	SampleRate uint32
	Channels   uint8
	Bits       uint8
}

// TouchEvent payload
type TouchEvent struct {
	X         uint16
	Y         uint16
	Action    uint8
	PointerID uint8
}

// TimestampedData holds a nanosecond timestamp + raw data
type TimestampedData struct {
	Timestamp int64  // nanoseconds since stream start
	Data      []byte
}

// EncodeVideoInfo serializes video info
func EncodeVideoInfo(v VideoInfo) []byte {
	buf := make([]byte, 5)
	binary.BigEndian.PutUint16(buf[0:], v.Width)
	binary.BigEndian.PutUint16(buf[2:], v.Height)
	buf[4] = v.FPS
	return buf
}

// DecodeVideoInfo deserializes video info
func DecodeVideoInfo(data []byte) (VideoInfo, error) {
	if len(data) < 5 {
		return VideoInfo{}, fmt.Errorf("video info too short")
	}
	return VideoInfo{
		Width:  binary.BigEndian.Uint16(data[0:]),
		Height: binary.BigEndian.Uint16(data[2:]),
		FPS:    data[4],
	}, nil
}

// EncodeAudioInfo serializes audio info
func EncodeAudioInfo(a AudioInfo) []byte {
	buf := make([]byte, 6)
	binary.BigEndian.PutUint32(buf[0:], a.SampleRate)
	buf[4] = a.Channels
	buf[5] = a.Bits
	return buf
}

// DecodeAudioInfo deserializes audio info
func DecodeAudioInfo(data []byte) (AudioInfo, error) {
	if len(data) < 6 {
		return AudioInfo{}, fmt.Errorf("audio info too short")
	}
	return AudioInfo{
		SampleRate: binary.BigEndian.Uint32(data[0:]),
		Channels:   data[4],
		Bits:       data[5],
	}, nil
}

// EncodeTouchEvent serializes touch event
func EncodeTouchEvent(t TouchEvent) []byte {
	buf := make([]byte, 6)
	binary.BigEndian.PutUint16(buf[0:], t.X)
	binary.BigEndian.PutUint16(buf[2:], t.Y)
	buf[4] = t.Action
	buf[5] = t.PointerID
	return buf
}

// DecodeTouchEvent deserializes touch event
func DecodeTouchEvent(data []byte) (TouchEvent, error) {
	if len(data) < 6 {
		return TouchEvent{}, fmt.Errorf("touch event too short")
	}
	return TouchEvent{
		X:         binary.BigEndian.Uint16(data[0:]),
		Y:         binary.BigEndian.Uint16(data[2:]),
		Action:    data[4],
		PointerID: data[5],
	}, nil
}

// PrependTimestamp prepends an 8-byte big-endian nanosecond timestamp to data
func PrependTimestamp(data []byte, ts int64) []byte {
	buf := make([]byte, 8+len(data))
	binary.BigEndian.PutUint64(buf[0:], uint64(ts))
	copy(buf[8:], data)
	return buf
}

// ExtractTimestamp extracts an 8-byte timestamp from the beginning of data
func ExtractTimestamp(data []byte) (int64, []byte, error) {
	if len(data) < 8 {
		return 0, nil, fmt.Errorf("data too short for timestamp")
	}
	ts := int64(binary.BigEndian.Uint64(data[0:]))
	return ts, data[8:], nil
}

// WriteMessage writes a framed message to the writer
func WriteMessage(w io.Writer, msgType byte, payload []byte) error {
	frame := make([]byte, 5+len(payload))
	frame[0] = msgType
	binary.BigEndian.PutUint32(frame[1:], uint32(len(payload)))
	copy(frame[5:], payload)
	_, err := w.Write(frame)
	return err
}

// ReadMessage reads a framed message from the reader
func ReadMessage(r io.Reader) (byte, []byte, error) {
	header := make([]byte, 5)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}
	msgType := header[0]
	length := binary.BigEndian.Uint32(header[1:])
	if length > 16*1024*1024 { // max 16MB
		return 0, nil, fmt.Errorf("message too large: %d", length)
	}
	payload := make([]byte, length)
	if length > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return 0, nil, err
		}
	}
	return msgType, payload, nil
}
