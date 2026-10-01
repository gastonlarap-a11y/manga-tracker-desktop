package main

import (
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options"
)

func TestWindowBackground(t *testing.T) {
	tests := []struct {
		name string
		goos string
		want options.RGBA
	}{
		{
			name: "macOS lets the window's material show",
			goos: "darwin",
			want: options.RGBA{R: 0, G: 0, B: 0, A: 0},
		},
		{
			// Windows reads any alpha but 0 as 255; 1 used to sit here and
			// meant opaque there and nearly invisible on macOS.
			name: "Windows is opaque, in the dashboard's dark colour",
			goos: "windows",
			want: options.RGBA{R: 17, G: 18, B: 24, A: 255},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := *windowBackground(tt.goos); got != tt.want {
				t.Fatalf("windowBackground(%q) = %+v, want %+v", tt.goos, got, tt.want)
			}
		})
	}
}
