package config_test

import (
	"testing"

	"github.com/home-operations/newt-sidecar/internal/config"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.Config
		wantErr bool
	}{
		{
			name: "valid: gateway mode",
			cfg: config.Config{
				SiteID:         "site-1",
				GatewayName:    "my-gw",
				TargetHostname: "gw.internal",
			},
			wantErr: false,
		},
		{
			name: "valid: enable-service only",
			cfg: config.Config{
				SiteID:        "site-1",
				EnableService: true,
			},
			wantErr: false,
		},
		{
			name: "valid: auto-service only",
			cfg: config.Config{
				SiteID:      "site-1",
				AutoService: true,
			},
			wantErr: false,
		},
		{
			name: "valid: gateway + enable-service",
			cfg: config.Config{
				SiteID:         "site-1",
				GatewayName:    "my-gw",
				TargetHostname: "gw.internal",
				EnableService:  true,
			},
			wantErr: false,
		},
		{
			name:    "invalid: missing site-id",
			cfg:     config.Config{GatewayName: "my-gw", TargetHostname: "gw.internal"},
			wantErr: true,
		},
		{
			name:    "invalid: gateway-name set but no target-hostname",
			cfg:     config.Config{SiteID: "site-1", GatewayName: "my-gw"},
			wantErr: true,
		},
		{
			name:    "invalid: no mode selected",
			cfg:     config.Config{SiteID: "site-1"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
