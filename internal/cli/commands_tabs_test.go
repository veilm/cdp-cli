package cli

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestTabsSubcommandHelp(t *testing.T) {
	tests := []struct {
		name  string
		run   func([]string) error
		usage string
	}{
		{"list", cmdTabsList, "usage: cdp tabs list"},
		{"switch", cmdTabsSwitch, "usage: cdp tabs switch"},
		{"open", cmdTabsOpen, "usage: cdp tabs open"},
		{"close", cmdTabsClose, "usage: cdp tabs close"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := captureStdout(t, func() error {
				return tt.run([]string{"--help"})
			})
			if err != nil {
				t.Fatalf("help returned an error: %v", err)
			}
			if !strings.Contains(output, tt.usage) {
				t.Errorf("help output %q does not contain %q", output, tt.usage)
			}
			if !strings.Contains(output, "Options:") {
				t.Errorf("help output %q does not list options", output)
			}
		})
	}
}

func captureStdout(t *testing.T, run func() error) (string, error) {
	t.Helper()

	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = original
		reader.Close()
		writer.Close()
	})

	runErr := run()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = original
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(output), runErr
}
