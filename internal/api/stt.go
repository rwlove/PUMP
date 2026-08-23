package api

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Speech-to-text for set-note dictation. The browser captures microphone
// audio, downsamples it to 16 kHz mono 16-bit PCM, and POSTs the raw samples
// here; this handler relays them to the self-hosted faster-whisper service
// over the Wyoming protocol and returns the transcript. Nothing leaves the
// network — the point of doing it here rather than in the browser's cloud
// Web Speech API.
//
// whisperAddr is the host:port of the Wyoming ASR service. Empty disables the
// endpoint (returns 503) so a deployment without whisper degrades cleanly
// rather than hanging on a dial that will never connect.
var whisperAddr string

const (
	// 16 kHz * 2 bytes * 1 channel * 60 s ≈ 1.9 MB. A set note is a short
	// phrase; this caps a runaway or malicious upload well above any real
	// dictation.
	maxSTTBytes = 2 * 1024 * 1024
	// Whisper audio format. Fixed here and mirrored by the client capture —
	// the Wyoming header must describe the samples exactly or transcription
	// silently garbles.
	sttRate     = 16000
	sttWidth    = 2
	sttChannels = 1
)

// postSTT accepts raw 16 kHz mono s16le PCM and returns {"text": …}.
func postSTT(c *gin.Context) {
	if whisperAddr == "" {
		c.JSON(http.StatusServiceUnavailable,
			gin.H{"error": "speech-to-text is not configured"})
		return
	}

	pcm, err := io.ReadAll(io.LimitReader(c.Request.Body, maxSTTBytes+1))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "could not read audio"})
		return
	}
	if len(pcm) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no audio"})
		return
	}
	if len(pcm) > maxSTTBytes {
		c.JSON(http.StatusRequestEntityTooLarge,
			gin.H{"error": "audio too long"})
		return
	}

	text, err := transcribePCM(whisperAddr, pcm)
	if err != nil {
		slog.Warn("stt: transcription failed",
			slog.String("addr", whisperAddr), slog.Any("err", err))
		c.JSON(http.StatusBadGateway,
			gin.H{"error": "transcription failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"text": text})
}

// transcribePCM speaks the minimal Wyoming ASR exchange against addr and
// returns the first transcript. The connection is one-shot: dial, stream the
// audio, read until the transcript, close.
func transcribePCM(addr string, pcm []byte) (string, error) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	// A whole run — send audio, wait for the model — must finish inside this.
	// large-v3-turbo on a short clip is well under this; the deadline is a
	// backstop against a wedged service holding the request open.
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	w := bufio.NewWriter(conn)
	r := bufio.NewReader(conn)

	audioFmt := map[string]any{"rate": sttRate, "width": sttWidth, "channels": sttChannels}

	if err := writeWyomingEvent(w, "transcribe", map[string]any{"language": "en"}, nil); err != nil {
		return "", err
	}
	if err := writeWyomingEvent(w, "audio-start", audioFmt, nil); err != nil {
		return "", err
	}
	// Chunk the PCM. Wyoming carries each chunk as a binary payload behind a
	// JSON header; the size is arbitrary, 8 KiB keeps headers cheap.
	const chunk = 8192
	for i := 0; i < len(pcm); i += chunk {
		end := i + chunk
		if end > len(pcm) {
			end = len(pcm)
		}
		if err := writeWyomingEvent(w, "audio-chunk", audioFmt, pcm[i:end]); err != nil {
			return "", err
		}
	}
	if err := writeWyomingEvent(w, "audio-stop", nil, nil); err != nil {
		return "", err
	}

	// Read events until the transcript arrives. faster-whisper emits exactly
	// one "transcript" after "audio-stop"; anything else (info, etc.) is
	// skipped.
	for {
		typ, data, err := readWyomingEvent(r)
		if err != nil {
			return "", err
		}
		if typ == "transcript" {
			text, _ := data["text"].(string)
			return text, nil
		}
	}
}

// writeWyomingEvent frames one event: a JSON header line, then the optional
// binary payload. data is embedded inline in the header (the reader accepts
// both inline data and the length-prefixed form; inline is simpler to emit).
func writeWyomingEvent(w *bufio.Writer, typ string, data map[string]any, payload []byte) error {
	header := map[string]any{"type": typ}
	if data != nil {
		header["data"] = data
	}
	if payload != nil {
		header["payload_length"] = len(payload)
	}
	b, err := json.Marshal(header)
	if err != nil {
		return err
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return err
	}
	if payload != nil {
		if _, err := w.Write(payload); err != nil {
			return err
		}
	}
	return w.Flush()
}

// readWyomingEvent reads one event. The header may carry data inline, or as a
// length-prefixed JSON blob (data_length) that overrides it; a payload_length
// binary blob may follow. We only need the type and data, but both length
// blocks must be consumed to stay framed for the next event.
func readWyomingEvent(r *bufio.Reader) (string, map[string]any, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return "", nil, err
	}
	var header map[string]any
	if err := json.Unmarshal(line, &header); err != nil {
		return "", nil, err
	}
	typ, _ := header["type"].(string)
	data, _ := header["data"].(map[string]any)

	if dl, ok := header["data_length"].(float64); ok {
		buf := make([]byte, int(dl))
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", nil, err
		}
		_ = json.Unmarshal(buf, &data)
	}
	if pl, ok := header["payload_length"].(float64); ok {
		if _, err := io.CopyN(io.Discard, r, int64(pl)); err != nil {
			return "", nil, err
		}
	}
	return typ, data, nil
}
