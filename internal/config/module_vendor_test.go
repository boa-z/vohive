package config

import "testing"

func TestNormalizeModuleVendor(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ModuleVendorQuectel},
		{"auto", ModuleVendorQuectel},
		{" Quectel ", ModuleVendorQuectel},
		{"SIMCOM", ModuleVendorSIMCOM},
		{"sim-com", ModuleVendorSIMCOM},
		{"sim_com", ModuleVendorSIMCOM},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := NormalizeModuleVendor(tt.in); got != tt.want {
				t.Fatalf("NormalizeModuleVendor(%q)=%q want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidateModuleVendor(t *testing.T) {
	if err := ValidateModuleVendor("simcom"); err != nil {
		t.Fatalf("ValidateModuleVendor(simcom) returned error: %v", err)
	}
	if err := ValidateModuleVendor("unknown"); err == nil {
		t.Fatal("ValidateModuleVendor(unknown) returned nil error")
	}
}
