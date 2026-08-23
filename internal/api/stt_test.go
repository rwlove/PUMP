package api

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"testing"
)

// fakeWyoming accepts one connection, reads the ASR exchange, and replies with
// a transcript. It records what the client sent so the test can assert the
// audio was framed and reassembled correctly.
type fakeWyoming struct {
	ln         net.Listener
	transcript string

	gotAudio    []byte
	gotStart    bool
	gotStop     bool
	sawLanguage string
}

func startFakeWyoming(t *testing.T, transcript string) *fakeWyoming {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakeWyoming{ln: ln, transcript: transcript}
	go f.serve()
	return f
}

func (f *fakeWyoming) addr() string { return f.ln.Addr().String() }

func (f *fakeWyoming) serve() {
	conn, err := f.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	for {
		typ, data, payload, err := readEventWithPayload(r)
		if err != nil {
			return
		}
		switch typ {
		case "transcribe":
			if l, ok := data["language"].(string); ok {
				f.sawLanguage = l
			}
		case "audio-start":
			f.gotStart = true
		case "audio-chunk":
			f.gotAudio = append(f.gotAudio, payload...)
		case "audio-stop":
			f.gotStop = true
			_ = writeWyomingEvent(w, "transcript",
				map[string]any{"text": f.transcript}, nil)
			return
		}
	}
}

// readEventWithPayload mirrors readWyomingEvent but also returns the payload,
// which the fake server needs to reassemble the audio.
func readEventWithPayload(r *bufio.Reader) (string, map[string]any, []byte, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return "", nil, nil, err
	}
	var header map[string]any
	if err := json.Unmarshal(line, &header); err != nil {
		return "", nil, nil, err
	}
	typ, _ := header["type"].(string)
	data, _ := header["data"].(map[string]any)
	if dl, ok := header["data_length"].(float64); ok {
		buf := make([]byte, int(dl))
		if _, err := io.ReadFull(r, buf); err != nil {
			return "", nil, nil, err
		}
		_ = json.Unmarshal(buf, &data)
	}
	var payload []byte
	if pl, ok := header["payload_length"].(float64); ok {
		payload = make([]byte, int(pl))
		if _, err := io.ReadFull(r, payload); err != nil {
			return "", nil, nil, err
		}
	}
	return typ, data, payload, nil
}

func TestTranscribePCM_RoundTrip(t *testing.T) {
	f := startFakeWyoming(t, "left shoulder tight")

	// Two chunks' worth so the chunking loop actually iterates.
	pcm := make([]byte, 8192+1234)
	for i := range pcm {
		pcm[i] = byte(i)
	}

	text, err := transcribePCM(f.addr(), pcm)
	if err != nil {
		t.Fatalf("transcribePCM: %v", err)
	}
	if text != "left shoulder tight" {
		t.Fatalf("transcript = %q, want %q", text, "left shoulder tight")
	}
	if !f.gotStart || !f.gotStop {
		t.Fatalf("missing audio-start/audio-stop framing: start=%v stop=%v", f.gotStart, f.gotStop)
	}
	if f.sawLanguage != "en" {
		t.Fatalf("language = %q, want en", f.sawLanguage)
	}
	if len(f.gotAudio) != len(pcm) {
		t.Fatalf("server reassembled %d audio bytes, want %d", len(f.gotAudio), len(pcm))
	}
	for i := range pcm {
		if f.gotAudio[i] != pcm[i] {
			t.Fatalf("audio corrupted at byte %d", i)
		}
	}
}

func TestTranscribePCM_DialError(t *testing.T) {
	// Nothing listening on this port → dial fails, surfaced as an error rather
	// than a hang or panic.
	if _, err := transcribePCM("127.0.0.1:1", []byte{0, 1, 2, 3}); err == nil {
		t.Fatal("expected a dial error, got nil")
	}
}
