package profile

import (
	"testing"
)

func TestValidateIBMCloudInstanceType(t *testing.T) {
	tests := []struct {
		name         string
		instanceType string
		wantErr      bool
	}{
		{
			name:         "valid balanced gen 2",
			instanceType: "bx2-4x16",
			wantErr:      false,
		},
		{
			name:         "valid compute optimized",
			instanceType: "cx2-8x16",
			wantErr:      false,
		},
		{
			name:         "valid memory optimized",
			instanceType: "mx2-16x128",
			wantErr:      false,
		},
		{
			name:         "valid balanced gen 3",
			instanceType: "bx3-8x40",
			wantErr:      false,
		},
		{
			name:         "invalid family",
			instanceType: "xx2-4x16",
			wantErr:      true,
		},
		{
			name:         "invalid format",
			instanceType: "invalid",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateIBMCloudInstanceType(tt.instanceType)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateIBMCloudInstanceType() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetIBMCloudInstanceTypeSpecs(t *testing.T) {
	tests := []struct {
		name         string
		instanceType string
		wantVCPU     int
		wantMemory   int
		wantErr      bool
	}{
		{
			name:         "bx2-4x16",
			instanceType: "bx2-4x16",
			wantVCPU:     4,
			wantMemory:   16,
			wantErr:      false,
		},
		{
			name:         "cx2-8x16",
			instanceType: "cx2-8x16",
			wantVCPU:     8,
			wantMemory:   16,
			wantErr:      false,
		},
		{
			name:         "mx2-32x256",
			instanceType: "mx2-32x256",
			wantVCPU:     32,
			wantMemory:   256,
			wantErr:      false,
		},
		{
			name:         "invalid format",
			instanceType: "invalid",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vcpu, memory, err := GetIBMCloudInstanceTypeSpecs(tt.instanceType)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetIBMCloudInstanceTypeSpecs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if vcpu != tt.wantVCPU {
					t.Errorf("GetIBMCloudInstanceTypeSpecs() vCPU = %v, want %v", vcpu, tt.wantVCPU)
				}
				if memory != tt.wantMemory {
					t.Errorf("GetIBMCloudInstanceTypeSpecs() memory = %v, want %v", memory, tt.wantMemory)
				}
			}
		})
	}
}

func TestGetIBMCloudRegionDisplayName(t *testing.T) {
	tests := []struct {
		name   string
		region string
		want   string
	}{
		{
			name:   "us-south",
			region: "us-south",
			want:   "Dallas (US South)",
		},
		{
			name:   "eu-de",
			region: "eu-de",
			want:   "Frankfurt (Europe Germany)",
		},
		{
			name:   "jp-tok",
			region: "jp-tok",
			want:   "Tokyo (Japan)",
		},
		{
			name:   "unknown region",
			region: "unknown",
			want:   "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetIBMCloudRegionDisplayName(tt.region)
			if got != tt.want {
				t.Errorf("GetIBMCloudRegionDisplayName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateIBMCloudConfig(t *testing.T) {
	tests := []struct {
		name    string
		data    *IBMCloudInstallConfigData
		wantErr bool
	}{
		{
			name: "valid config with new VPC",
			data: &IBMCloudInstallConfigData{
				InstallConfigData: InstallConfigData{
					Region:           "us-south",
					ControlPlaneType: "bx2-4x16",
					WorkerReplicas:   3,
					WorkerType:       "bx2-8x32",
				},
			},
			wantErr: false,
		},
		{
			name: "valid config with existing VPC",
			data: &IBMCloudInstallConfigData{
				InstallConfigData: InstallConfigData{
					Region:           "us-south",
					ControlPlaneType: "bx2-4x16",
					WorkerReplicas:   3,
					WorkerType:       "bx2-8x32",
				},
				VPCName:             "existing-vpc",
				ControlPlaneSubnets: []string{"subnet-1", "subnet-2"},
				ComputeSubnets:      []string{"subnet-3", "subnet-4"},
			},
			wantErr: false,
		},
		{
			name: "invalid region",
			data: &IBMCloudInstallConfigData{
				InstallConfigData: InstallConfigData{
					Region:           "invalid-region",
					ControlPlaneType: "bx2-4x16",
					WorkerReplicas:   3,
					WorkerType:       "bx2-8x32",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid control plane instance type",
			data: &IBMCloudInstallConfigData{
				InstallConfigData: InstallConfigData{
					Region:           "us-south",
					ControlPlaneType: "invalid-type",
					WorkerReplicas:   0,
				},
			},
			wantErr: true,
		},
		{
			name: "existing VPC without subnets",
			data: &IBMCloudInstallConfigData{
				InstallConfigData: InstallConfigData{
					Region:           "us-south",
					ControlPlaneType: "bx2-4x16",
				},
				VPCName: "existing-vpc",
				// Missing subnets
			},
			wantErr: true,
		},
		{
			name: "invalid publish strategy",
			data: &IBMCloudInstallConfigData{
				InstallConfigData: InstallConfigData{
					Region:           "us-south",
					ControlPlaneType: "bx2-4x16",
				},
				Publish: "InvalidStrategy",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIBMCloudConfig(tt.data)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIBMCloudConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
