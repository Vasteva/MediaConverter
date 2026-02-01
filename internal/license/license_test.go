package license

import "testing"

func TestValidate(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want bool
	}{
		{
			name: "valid pro key",
			key:  "VASTIVA-PRO-USER1-2328",
			want: true,
		},
		{
			name: "invalid prefix",
			key:  "WRONG-PRO-USER1-f597",
			want: false,
		},
		{
			name: "invalid checksum",
			key:  "VASTIVA-PRO-USER1-0000",
			want: false,
		},
		{
			name: "wrong parts count",
			key:  "VASTIVA-PRO-USER1",
			want: false,
		},
		{
			name: "empty key",
			key:  "",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Validate(tt.key); got != tt.want {
				t.Errorf("Validate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetPlanName(t *testing.T) {
	if GetPlanName("VASTIVA-PRO-USER1-2328") != "Vastiva Pro" {
		t.Error("expected Vastiva Pro for valid key")
	}
	if GetPlanName("invalid") != "Standard" {
		t.Error("expected Standard for invalid key")
	}
}
