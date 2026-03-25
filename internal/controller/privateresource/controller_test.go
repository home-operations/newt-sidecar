package privateresource

import (
	"testing"

	v1alpha1 "github.com/home-operations/newt-sidecar/api/v1alpha1"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		spec    v1alpha1.PrivateResourceSpec
		wantErr bool
	}{
		{
			name: "valid host mode with IP",
			spec: v1alpha1.PrivateResourceSpec{
				Name:        "my-host",
				Mode:        "host",
				Destination: "10.0.0.1",
			},
			wantErr: false,
		},
		{
			name: "valid host mode with hostname and alias",
			spec: v1alpha1.PrivateResourceSpec{
				Name:        "my-host",
				Mode:        "host",
				Destination: "myserver.local",
				Alias:       "myserver.vpn.example.com",
			},
			wantErr: false,
		},
		{
			name: "valid cidr mode",
			spec: v1alpha1.PrivateResourceSpec{
				Name:        "my-cidr",
				Mode:        "cidr",
				Destination: "10.42.0.0/16",
			},
			wantErr: false,
		},
		{
			name: "cidr mode with invalid CIDR",
			spec: v1alpha1.PrivateResourceSpec{
				Name:        "bad-cidr",
				Mode:        "cidr",
				Destination: "not-a-cidr",
			},
			wantErr: true,
		},
		{
			name: "cidr mode with IP instead of CIDR",
			spec: v1alpha1.PrivateResourceSpec{
				Name:        "ip-as-cidr",
				Mode:        "cidr",
				Destination: "10.0.0.1",
			},
			wantErr: true,
		},
		{
			name: "roles contain Admin",
			spec: v1alpha1.PrivateResourceSpec{
				Name:        "admin-role",
				Mode:        "host",
				Destination: "10.0.0.1",
				Roles:       []string{"User", "Admin"},
			},
			wantErr: true,
		},
		{
			name: "roles without Admin",
			spec: v1alpha1.PrivateResourceSpec{
				Name:        "user-role",
				Mode:        "host",
				Destination: "10.0.0.1",
				Roles:       []string{"User", "Editor"},
			},
			wantErr: false,
		},
		{
			name: "valid cidr /32",
			spec: v1alpha1.PrivateResourceSpec{
				Name:        "single-host-cidr",
				Mode:        "cidr",
				Destination: "10.0.0.1/32",
			},
			wantErr: false,
		},
		{
			name: "empty roles is valid",
			spec: v1alpha1.PrivateResourceSpec{
				Name:        "no-roles",
				Mode:        "host",
				Destination: "10.0.0.1",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validate(&tt.spec)
			if (err != nil) != tt.wantErr {
				t.Errorf("validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
