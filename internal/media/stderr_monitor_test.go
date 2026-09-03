package media

import (
	"strings"
	"testing"
)

// Real stderr from an Arc A310 transcoding a 2160p 10-bit HEVC REMUX. FFmpeg
// reported these, dropped the affected frames, then exited 0 and produced a
// 65 MB file of the correct duration. Neither the exit status nor a duration
// check catches that, which is why the stream is scanned.
const arcA310DecodeFailure = `[vist#0:0/hevc @ 0x5ae0b8b50ec0] Error submitting packet to decoder: Cannot allocate memory
[hevc @ 0x5ae0b9fcfe00] get_buffer() failed
[hevc @ 0x5ae0b9fcfe00] thread_get_buffer() failed
[hevc @ 0x5ae0b9fcfe00] Error parsing NAL unit #2.
[vist#0:0/hevc @ 0x5ae0b8b50ec0] Decoding error: Cannot allocate memory
    Last message repeated 7 times
[out#0/matroska @ 0x5ae0b8ba9780] video:14910kB audio:30974kB subtitle:17852kB
frame= 1439 fps=136 q=-0.0 Lsize=   64323kB time=00:00:59.99 bitrate=8782.4kbits/s speed=5.67x
`

func TestStderrMonitorDetectsDecodeErrors(t *testing.T) {
	m := newStderrMonitor()
	if _, err := m.Write([]byte(arcA310DecodeFailure)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	findings := m.Findings()
	if len(findings) == 0 {
		t.Fatal("expected decode errors to be detected")
	}

	// Each distinct problem should be reported once, not once per occurrence.
	for _, want := range []string{
		"Error submitting packet to decoder",
		"get_buffer() failed",
		"Error parsing NAL unit",
		"Decoding error",
	} {
		found := false
		for _, f := range findings {
			if strings.Contains(f, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected a finding containing %q, got %v", want, findings)
		}
	}
}

// Real stderr tail from a 1080p VAAPI transcode on an Arc A310 that ran the
// full 1h43m film and then failed on the final frame. None of the decode-side
// patterns appear — the failure is entirely on the filter side. FFmpeg here
// exited non-zero, but the same message can accompany an exit 0 with the last
// frames simply missing, which is what this guards against.
const arcA310FilterFailure = `frame=155452 fps=544 q=-0.0 size= 2542080kB time=01:43:38.59 bitrate=3348.8kbits/s speed=21.8x
[vf#0:0 @ 0x62ffc12bef40] Error while filtering: Cannot allocate memory
Failed to inject frame into filter network: Cannot allocate memory
Error while filtering: Cannot allocate memory
[out#0/matroska @ 0x62ffc130e180] video:1392231kB audio:1145873kB subtitle:0kB
frame=155505 fps=543 q=-0.0 Lsize= 2543232kB time=01:43:40.65 bitrate=3349.2kbits/s speed=21.7x
`

func TestStderrMonitorDetectsFilterErrors(t *testing.T) {
	m := newStderrMonitor()
	if _, err := m.Write([]byte(arcA310FilterFailure)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	findings := m.Findings()
	if len(findings) == 0 {
		t.Fatal("filter-side frame loss was not detected — the job would exit 0 with frames missing")
	}
	for _, want := range []string{
		"Error while filtering",
		"Failed to inject frame into filter network",
	} {
		found := false
		for _, f := range findings {
			if strings.Contains(f, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected a finding containing %q, got %v", want, findings)
		}
	}
}

// A clean transcode must not be failed. Progress output is written to the same
// stream and contains no decode markers.
func TestStderrMonitorIgnoresCleanOutput(t *testing.T) {
	clean := `frame=  120 fps=0.0 q=-0.0 size=    3819kB time=00:00:04.91 bitrate=6363.7kbits/s speed=15.2x
[out#0/matroska @ 0x5825a5594780] video:3818kB audio:0kB subtitle:0kB muxing overhead: 0.043153%
Stream #0:0: Video: hevc (Main 10), vaapi(tv, progressive), 1920x1080
progress=continue
progress=end
`
	m := newStderrMonitor()
	if _, err := m.Write([]byte(clean)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := m.Findings(); len(got) > 0 {
		t.Errorf("clean output must produce no findings, got %v", got)
	}
}

// FFmpeg writes progress updates separated by \r, so line splitting cannot
// rely on \n alone.
func TestStderrMonitorSplitsOnCarriageReturn(t *testing.T) {
	m := newStderrMonitor()
	m.Write([]byte("frame=1 fps=0\rframe=2 fps=0\r[hevc @ 0x1] get_buffer() failed\r"))
	if len(m.Findings()) != 1 {
		t.Errorf("expected 1 finding from \\r-separated stream, got %v", m.Findings())
	}
}

// Input arrives in arbitrary chunks from the pipe, so a marker split across two
// writes must still be recognised.
func TestStderrMonitorHandlesSplitWrites(t *testing.T) {
	m := newStderrMonitor()
	m.Write([]byte("[hevc @ 0x1] get_buf"))
	m.Write([]byte("fer() failed\n"))
	if len(m.Findings()) != 1 {
		t.Errorf("expected marker spanning two writes to be detected, got %v", m.Findings())
	}
}

// The retained tail must stay bounded regardless of how much FFmpeg emits.
// A multi-hour transcode with "-progress pipe:2" produces tens of megabytes.
func TestStderrMonitorTailIsBounded(t *testing.T) {
	m := newStderrMonitor()
	chunk := []byte(strings.Repeat("frame=1 fps=30 time=00:00:01.00 bitrate=1000kbits/s\n", 200))
	for i := 0; i < 500; i++ {
		m.Write(chunk)
	}

	if got := len(m.tail); got > maxStderrTail {
		t.Errorf("tail grew to %d bytes, cap is %d", got, maxStderrTail)
	}
	if got := cap(m.tail); got > 4*maxStderrTail {
		t.Errorf("tail backing array grew to %d bytes, expected it to stay bounded", got)
	}
	if !strings.HasPrefix(m.Tail(), "...") {
		t.Error("a truncated tail should be marked with a leading ellipsis")
	}
}

// Findings are capped so a stream failing on every frame cannot accumulate
// unboundedly.
func TestStderrMonitorCapsFindings(t *testing.T) {
	m := newStderrMonitor()
	for i := 0; i < 1000; i++ {
		m.Write([]byte("[hevc @ 0x1] Decoding error: Cannot allocate memory\n"))
	}
	if got := len(m.Findings()); got != 1 {
		t.Errorf("repeated identical errors should collapse to 1 finding, got %d", got)
	}
}

func TestTranscodeIntegrityErrorMessage(t *testing.T) {
	err := &TranscodeIntegrityError{Findings: []string{"get_buffer() failed"}}
	msg := err.Error()
	if !strings.Contains(msg, "exited 0") || !strings.Contains(msg, "get_buffer() failed") {
		t.Errorf("error message should explain the zero exit and cite the finding, got %q", msg)
	}
}
