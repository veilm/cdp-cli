package cli

import "testing"

func TestParseEmulationSettings(t *testing.T) {
	tests := []struct {
		device string
		mobile bool
		want   emulationSettings
	}{
		{"phone", false, emulationSettings{name: "phone", width: 390, height: 844, dpr: 2, mobile: true, touch: true}},
		{"tablet", false, emulationSettings{name: "tablet", width: 820, height: 1180, dpr: 2, mobile: true, touch: true}},
		{"390x844", false, emulationSettings{name: "390x844", width: 390, height: 844, dpr: 2}},
		{"1024X768", true, emulationSettings{name: "1024X768", width: 1024, height: 768, dpr: 2, mobile: true, touch: true}},
		{"refresh-reset", false, emulationSettings{name: "refresh-reset", reopen: true}},
	}
	for _, tt := range tests {
		t.Run(tt.device, func(t *testing.T) {
			got, err := parseEmulationSettings(tt.device, tt.mobile, 2)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseEmulationSettingsRejectsInvalidValues(t *testing.T) {
	for _, device := range []string{"watch", "390", "0x844", "390x0", "reset"} {
		if _, err := parseEmulationSettings(device, false, 1); err == nil {
			t.Errorf("expected %q to fail", device)
		}
	}
	if _, err := parseEmulationSettings("phone", false, 0); err == nil {
		t.Error("expected zero DPR to fail")
	}
}
