# Agent Guide: Modifying Double Agent

This document helps AI agents and developers understand the codebase so they can adapt it to different hardware, compositors, or Android devices.

## Architecture Overview

```
PC (Hyprland/Wayland)
  ├─ Hyprland headless output  ← virtual monitor created via hyprctl
  ├─ wf-recorder               ← captures headless output, encodes to H.264 via VAAPI
  ├─ parec                     ← captures desktop audio from PipeWire monitor source
  ├─ Go TCP server (:7777)     ← sends video/audio frames to Android client
  └─ ADB reverse tcp:7777      ← tunnels PC port to Android localhost

Android Tablet
  ├─ TCP client                ← connects to localhost:7777 via ADB tunnel
  ├─ MediaCodec                ← hardware H.264 decode → SurfaceView
  ├─ AudioTrack                ← PCM playback
  └─ SurfaceView               ← fullscreen video display
```

## Data Flow

1. **PC captures screen**: `wf-recorder` captures the Hyprland headless output and encodes it to H.264 using `h264_vaapi` (AMD hardware encoder).
2. **PC captures audio**: `parec` records from the default PipeWire sink's monitor source ("what you hear").
3. **PC streams**: The Go server reads encoded H.264 NAL units from `wf-recorder` and PCM audio chunks from `parec`, wraps them in a simple binary protocol, and sends them over TCP.
4. **ADB tunnel**: `adb reverse tcp:7777 tcp:7777` makes the PC's port 7777 available as `localhost:7777` on the Android device.
5. **Android receives**: `NetworkClient` reads framed messages, demuxes video/audio, and forwards them to `VideoDecoder` and `AudioPlayer`.
6. **Android displays**: `MediaCodec` decodes H.264 to a `SurfaceView`. `AudioTrack` plays PCM audio.

## Project Layout

```
double-agent/
├── pc/                          # PC Server (Go)
│   ├── main.go                  # Entry point: setup Hyprland output, server, capture
│   ├── hyprland.go              # Creates/removes headless output via hyprctl
│   ├── capture.go               # Manages wf-recorder and parec subprocesses
│   ├── server.go                # TCP server, ADB reverse tunnel setup
│   ├── protocol.go              # Binary message framing (type + length + payload)
│   └── go.mod
│
├── android/app/src/main/java/com/doubleagent/
│   ├── MainActivity.kt          # Activity lifecycle, wires components
│   ├── NetworkClient.kt         # TCP client, demuxes protocol messages
│   ├── VideoDecoder.kt          # MediaCodec H.264 decoder + SurfaceView output
│   ├── AudioPlayer.kt           # AudioTrack PCM playback
│   ├── Protocol.kt              # Message serialization / deserialization
│   └── TouchHandler.kt          # (removed in current version)
│
└── README.md
```

## Key Files Explained

### `pc/main.go`

**Purpose**: Entry point. Coordinates all components.

**What to modify**:
- Resolution/FPS/port (top of `main()`):
  ```go
  width := 1920
  height := 1200
  fps := 60
  addr := ":7777"
  ```
- Hyprland output name: passed to `NewCapture()`

### `pc/hyprland.go`

**Purpose**: Creates a virtual monitor using `hyprctl output create headless`.

**What to modify**:
- If your compositor is **not Hyprland** (e.g., Sway, River, DWL):
  - Replace `hyprctl` commands with your compositor's equivalent for creating virtual outputs.
  - If your compositor has no headless output support, you may need to use `wlroots screencopy` directly or capture a region of an existing output.
- Output positioning: change `auto-right` to `auto-left`, `auto-up`, `auto-down`, or explicit coordinates.

### `pc/capture.go`

**Purpose**: Spawns `wf-recorder` (video) and `parec` (audio) and reads their stdout.

**What to modify**:

#### Video capture
Current pipeline: `wf-recorder -o HEADLESS-N -D -c h264_vaapi -d /dev/dri/renderD128 -r 60 --file <pipe>`

- **Different encoder** (e.g., NVIDIA NVENC, Intel QuickSync):
  ```go
  // NVIDIA:
  "-c", "h264_nvenc"
  "-d", "/dev/dri/card0"  // or omit

  // Intel:
  "-c", "h264_vaapi"
  "-d", "/dev/dri/renderD128" // Intel uses same VAAPI device usually
  ```
- **Software encoding fallback** (if no GPU encoder):
  ```go
  "-c", "libx264"
  // Remove "-d", "/dev/dri/renderD128"
  ```
- **Different capture tool**: If `wf-recorder` doesn't work with your compositor, replace the `exec.Command` block with `grim` + `ffmpeg` in a loop, or use `wl-screenrec`, or write a custom Wayland screencopy client.

#### Audio capture
Current: `parec --rate=48000 --channels=2 --format=s16le --latency-msec=10 --device=<monitor-source>`

- **Different audio backend**: If not using PipeWire/PulseAudio:
  - Replace `parec` with `arecord` (ALSA), `ffmpeg -f alsa`, etc.
  - Update `AudioInfo` in `main.go` to match sample rate/channels.
- **No audio**: Comment out `c.startAudio()` call and the audio forwarding goroutine in `main.go`.

#### NAL unit splitting
`readVideo()` splits the raw H.264 Annex-B stream into individual NAL units by looking for start codes (`00 00 01` or `00 00 00 01`). If you switch to a capture tool that outputs MP4, MPEG-TS, or another container format, you must add a demuxer or change the Android decoder to accept the new format.

### `pc/server.go`

**Purpose**: TCP listener on `:7777`. Sets up `adb reverse`. Sends video/audio. Reads touch events (ignored currently).

**What to modify**:
- Port: change `addr` in `main.go` (not here)
- ADB path: if `adb` is not in PATH, use absolute path in `exec.Command("adb", ...)`
- Remove touch handling if you don't need it (already disabled in current version)

### `pc/protocol.go`

**Purpose**: Defines the wire protocol.

**Format**:
```
[1 byte: message type][4 bytes: payload length (big endian)][N bytes: payload]
```

**Message types**:
| Type | Name | Direction | Payload |
|------|------|-----------|---------|
| `0x01` | VIDEO_INFO | PC → Android | `width(u16), height(u16), fps(u8)` |
| `0x02` | VIDEO_FRAME | PC → Android | `timestamp(u64) + h264_annexb_data` |
| `0x03` | AUDIO_INFO | PC → Android | `sample_rate(u32), channels(u8), bits(u8)` |
| `0x04` | AUDIO_PACKET | PC → Android | `timestamp(u64) + pcm_data` |
| `0x05` | TOUCH_EVENT | Android → PC | `x(u16), y(u16), action(u8), pointer_id(u8)` |
| `0x06` | HEARTBEAT | Android → PC | `timestamp(u64)` |

**Timestamps**: 64-bit big-endian nanoseconds relative to stream start. Prepended to VIDEO_FRAME and AUDIO_PACKET payloads.

### `android/NetworkClient.kt`

**Purpose**: Connects to `localhost:7777`. Reads framed messages. Dispatches to callbacks.

**What to modify**:
- Host/port: change `host = "localhost"`, `port = 7777` in `MainActivity.kt`
- Buffer sizes: `frameQueue` / `audioQueue` capacities affect latency vs. smoothness
- Reconnection logic: currently retries every 1 second

### `android/VideoDecoder.kt`

**Purpose**: Buffers NAL units until SPS/PPS are found, configures `MediaCodec`, then decodes frames.

**What to modify**:
- **Decoder doesn't initialize**: Some tablets need different `MediaFormat` keys. Try adding:
  ```kotlin
  format.setInteger(MediaFormat.KEY_LOW_LATENCY, 1)
  format.setInteger(MediaFormat.KEY_OPERATING_RATE, 60)
  ```
- **Codec capability issues**: Some devices only support Baseline profile. If your encoder outputs High profile, add `-profile:v baseline -level 3.1` to `wf-recorder` args.
- **Black screen after init**: The decoder may need a different color format. Check `adb logcat` for `MediaCodec` errors.

### `android/AudioPlayer.kt`

**Purpose**: `AudioTrack` in streaming mode with low-latency performance mode.

**What to modify**:
- Buffer size: `(sampleRate * 60 / 1000 * channels * (bits / 8))` = 60ms worth. Reduce for lower latency, increase for stability.
- Queue drop threshold: `queueMs > 200` drops half the backlog. Adjust based on your latency target.
- **Audio is late**: Increase the drop threshold or reduce the buffer size.
- **Audio is choppy**: Increase the buffer size or add queue prefetching.

### `android/Protocol.kt`

**Purpose**: Mirrors the Go protocol. Handles serialization/deserialization.

**What to modify**:
- If you change the protocol on the PC side, you **must** update this file to match.
- `extractTimestamp()` assumes the first 8 bytes are a big-endian `u64`. Keep this in sync with `PrependTimestamp()` in Go.

## Common Modification Scenarios

### Different compositor (not Hyprland)

If your compositor is Sway, River, or another wlroots-based compositor:
1. **Headless output**: Check if it supports `wlr-output-management` or `create headless`. Most wlroots compositors do.
2. **Capture**: `wf-recorder` should work on any wlroots compositor. Just change the output name.
3. **Non-wlroots** (e.g., GNOME/Mutter, KDE/KWin): You cannot use `wf-recorder`. Use `gnome-screenshot` in a loop, `ffmpeg -f kmsgrab`, or `ffmpeg -f x11grab` if running XWayland.

### Different GPU (NVIDIA / Intel)

**NVIDIA**:
```bash
# Check NVENC support
ffmpeg -encoders | grep nvenc
```
In `capture.go`, change wf-recorder args:
```go
"-c", "h264_nvenc",
// Remove "-d", "/dev/dri/renderD128"
```

**Intel**:
Usually works with the same VAAPI args but you may need `intel-media-driver` or `libva-intel-driver`.

### Different Android device (decoder issues)

If the tablet shows black screen:
1. Check `adb logcat | grep -i "mediacodec\|videodecoder"` for decoder errors.
2. Some devices need `MediaCodec.createByCodecName("OMX.google.h264.decoder")` (software fallback). Edit `VideoDecoder.kt`.
3. Some devices crash with timestamps. Remove `td.timestampUs` from `queueInputBuffer()` and pass `0` instead.

### No audio

1. Remove `parec` entirely:
   - Comment out `c.startAudio()` in `capture.go`
   - Remove the audio forwarding goroutine in `main.go`
2. Or change the audio device:
   - Edit `findMonitorSource()` in `capture.go` to return a specific PulseAudio/PipeWire source name.

### WiFi instead of USB

1. Remove the `adb reverse` call from `server.go`.
2. Change Android `NetworkClient` host from `"localhost"` to your PC's LAN IP.
3. Ensure the PC firewall allows port 7777.
4. Note: WiFi introduces more latency and jitter than USB.

## Build System

### PC
Pure Go. No Makefile needed.
```bash
cd pc
go build -o double-agent-pc .
```

### Android
Gradle project with Kotlin DSL.
```bash
cd android
gradle assembleDebug
# or
./gradlew assembleDebug  # if wrapper exists
```

Requires:
- `ANDROID_HOME` pointing to Android SDK
- `JAVA_HOME` pointing to JDK 17+
- `compileSdk = 34`, `minSdk = 28`

## Troubleshooting for Developers

### Server says "Waiting for Android client to connect..." forever
- Check `adb devices` shows the tablet
- Check `adb reverse --list` shows `tcp:7777 tcp:7777`
- Check firewall: `ss -tlnp | grep 7777`
- Try connecting from PC: `nc -v localhost 7777`

### wf-recorder says "Couldn't find requested output"
- Run `hyprctl monitors` to get the actual headless output name
- The name may be `HEADLESS-1`, `HEADLESS-2`, etc. `hyprland.go` handles this.

### MediaCodec throws `MediaCodec.CodecException`
- SPS/PPS may be malformed. Add logging in `tryInitFromBuffer()` to dump hex.
- Encoder may be using High profile. Force Baseline: add `-profile:v baseline` to capture args.

### Audio drifts out of sync
- The current approach uses a simple queue with overflow dropping. For better sync:
  1. Add a sync thread that compares audio playback position (via `AudioTrack.getTimestamp()`) to video PTS.
  2. Drop video frames that are too late, or sleep if video is too early.
  3. Or switch to a single playback clock (e.g., sync video to audio).

## License

MIT — modify freely, no support provided.
