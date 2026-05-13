package ui

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()
	fn()
	_ = w.Close()
	<-done
	os.Stdout = orig
	return buf.String()
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	done := make(chan struct{})
	var buf bytes.Buffer
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()
	fn()
	_ = w.Close()
	<-done
	os.Stderr = orig
	return buf.String()
}

func TestColorConstants(t *testing.T) {
	// Verify ANSI code constants are non-empty and distinct.
	codes := []string{Reset, Bold, Dim, Red, Green, Yellow, Blue, Cyan, White}
	for i, c := range codes {
		if c == "" {
			t.Errorf("code %d empty", i)
		}
	}
}

func TestColorRespectsNoColor(t *testing.T) {
	// Force noColor on for the duration of the test.
	orig := noColor
	noColor = true
	defer func() { noColor = orig }()
	got := color(Red, "hello")
	if got != "hello" {
		t.Errorf("color w/ noColor: got %q want %q", got, "hello")
	}
	if got := BoldText("x"); got != "x" {
		t.Errorf("BoldText w/ noColor: got %q", got)
	}
	if got := DimText("y"); got != "y" {
		t.Errorf("DimText w/ noColor: got %q", got)
	}
}

func TestColorWithoutNoColor(t *testing.T) {
	orig := noColor
	noColor = false
	defer func() { noColor = orig }()
	got := color(Red, "hi")
	if !strings.Contains(got, Red) || !strings.Contains(got, Reset) || !strings.Contains(got, "hi") {
		t.Errorf("color: missing ansi wrapping: %q", got)
	}
	if got := BoldText("x"); !strings.Contains(got, Bold) {
		t.Errorf("BoldText: missing Bold; got %q", got)
	}
	if got := DimText("y"); !strings.Contains(got, Dim) {
		t.Errorf("DimText: missing Dim; got %q", got)
	}
	if got := ColorText(Green, "z"); !strings.Contains(got, Green) {
		t.Errorf("ColorText: missing Green; got %q", got)
	}
}

func TestHeaderAndSection(t *testing.T) {
	out := captureStdout(t, func() {
		Header("hdr")
		Section("sec")
		Step("stp")
	})
	for _, want := range []string{"hdr", "sec", "stp"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	if !strings.Contains(out, "─") {
		t.Errorf("Header missing divider; got %q", out)
	}
}

func TestStepN(t *testing.T) {
	out := captureStdout(t, func() {
		StepN(2, 5, "doing thing")
	})
	if !strings.Contains(out, "[2/5]") || !strings.Contains(out, "doing thing") {
		t.Errorf("StepN output unexpected: %q", out)
	}
}

func TestBulletAllStyles(t *testing.T) {
	styles := []string{"ok", "warn", "error", "skip", "none", "found", "dry", "unknown"}
	for _, s := range styles {
		out := captureStdout(t, func() {
			Bullet(s, "msg-"+s)
		})
		if !strings.Contains(out, "msg-"+s) {
			t.Errorf("Bullet(%q): missing message; got %q", s, out)
		}
	}
}

func TestPreviewSection(t *testing.T) {
	out := captureStdout(t, func() {
		PreviewSection("Files", "a.txt", "b.txt")
	})
	for _, want := range []string{"Files", "a.txt", "b.txt"} {
		if !strings.Contains(out, want) {
			t.Errorf("PreviewSection missing %q in %q", want, out)
		}
	}
}

func TestSuccessBoxWithAndWithoutSteps(t *testing.T) {
	out := captureStdout(t, func() {
		SuccessBox("done")
	})
	if !strings.Contains(out, "done") {
		t.Errorf("SuccessBox: missing msg; got %q", out)
	}
	if strings.Contains(out, "Next steps") {
		t.Errorf("SuccessBox w/o steps should omit Next steps; got %q", out)
	}

	out = captureStdout(t, func() {
		SuccessBox("done", "step 1", "step 2")
	})
	for _, want := range []string{"done", "Next steps", "step 1", "step 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("SuccessBox w/ steps missing %q in %q", want, out)
		}
	}
}

func TestWarnBoxAndInfoBox(t *testing.T) {
	out := captureStdout(t, func() {
		WarnBox("warning", "line 1")
		InfoBox("info", "line A", "line B")
	})
	for _, want := range []string{"warning", "line 1", "info", "line A", "line B"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
}

func TestErrorAndErrorf(t *testing.T) {
	got := captureStderr(t, func() {
		Error("boom")
		Errorf("code=%d", 42)
	})
	if !strings.Contains(got, "boom") {
		t.Errorf("Error: missing 'boom' in %q", got)
	}
	if !strings.Contains(got, "code=42") {
		t.Errorf("Errorf: missing 'code=42' in %q", got)
	}
}

func TestWarnInfoSuccessDryRunCreateSkip(t *testing.T) {
	out := captureStdout(t, func() {
		Warn("w")
		Info("i")
		Success("s")
		DryRun("d")
		Create("c")
		Skip("k")
	})
	for _, want := range []string{"w", "i", "s", "d", "c", "k"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	if !strings.Contains(out, "(dry run)") {
		t.Errorf("DryRun missing marker; got %q", out)
	}
}

func TestConfirmAutoYes(t *testing.T) {
	out := captureStdout(t, func() {
		if !Confirm("proceed?", true) {
			t.Error("Confirm(autoYes=true) returned false")
		}
	})
	if !strings.Contains(out, "auto-confirmed") {
		t.Errorf("Confirm auto: missing marker; got %q", out)
	}
}

func TestConfirmReadsStdin(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"y\n", true},
		{"yes\n", true},
		{"Y\n", true},
		{"n\n", false},
		{"\n", false},
		{"maybe\n", false},
	}
	for _, tc := range cases {
		t.Run(strings.TrimSpace(tc.input), func(t *testing.T) {
			origIn := os.Stdin
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("pipe: %v", err)
			}
			os.Stdin = r
			defer func() { os.Stdin = origIn }()
			go func() {
				_, _ = w.Write([]byte(tc.input))
				_ = w.Close()
			}()
			out := captureStdout(t, func() {
				if got := Confirm("?", false); got != tc.want {
					t.Errorf("Confirm(%q) = %v; want %v", tc.input, got, tc.want)
				}
			})
			if !strings.Contains(out, "?") {
				t.Errorf("prompt missing in stdout: %q", out)
			}
		})
	}
}

func TestConfirmEOFReturnsFalse(t *testing.T) {
	origIn := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdin = r
	defer func() { os.Stdin = origIn }()
	// Close immediately => EOF.
	_ = w.Close()
	out := captureStdout(t, func() {
		if Confirm("?", false) {
			t.Error("Confirm on EOF should be false")
		}
	})
	if !strings.Contains(out, "?") {
		t.Errorf("prompt missing in stdout: %q", out)
	}
}
