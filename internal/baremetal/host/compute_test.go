package host

import (
	"reflect"
	"testing"
)

func TestNet3(t *testing.T) {
	if got := net3("192.168.126.0/24"); got != "192.168.126" {
		t.Fatalf("net3 = %q, want 192.168.126", got)
	}
	if got := net3("10.0.5.0/24"); got != "10.0.5" {
		t.Fatalf("net3 = %q, want 10.0.5", got)
	}
}

func TestComputeNodes(t *testing.T) {
	got := ComputeNodes(Topology{
		ClusterName:       "rhwa-lab",
		NetCIDR:           "192.168.126.0/24",
		ControlPlaneCount: 3,
		WorkerCount:       3,
		SpareCount:        3,
		CPVCPU:            8, CPRAMGB: 20,
		WKVCPU: 4, WKRAMGB: 16,
	})
	want := []VM{
		{Name: "rhwa-lab-master-0", Host: "master-0", Role: "master", IP: "192.168.126.11", MAC: "52:54:00:6a:01:00", VCPU: 8, RAMGB: 20, Spare: false},
		{Name: "rhwa-lab-master-1", Host: "master-1", Role: "master", IP: "192.168.126.12", MAC: "52:54:00:6a:01:01", VCPU: 8, RAMGB: 20, Spare: false},
		{Name: "rhwa-lab-master-2", Host: "master-2", Role: "master", IP: "192.168.126.13", MAC: "52:54:00:6a:01:02", VCPU: 8, RAMGB: 20, Spare: false},
		{Name: "rhwa-lab-worker-0", Host: "worker-0", Role: "worker", IP: "192.168.126.21", MAC: "52:54:00:6a:02:00", VCPU: 4, RAMGB: 16, Spare: false},
		{Name: "rhwa-lab-worker-1", Host: "worker-1", Role: "worker", IP: "192.168.126.22", MAC: "52:54:00:6a:02:01", VCPU: 4, RAMGB: 16, Spare: false},
		{Name: "rhwa-lab-worker-2", Host: "worker-2", Role: "worker", IP: "192.168.126.23", MAC: "52:54:00:6a:02:02", VCPU: 4, RAMGB: 16, Spare: false},
		{Name: "rhwa-lab-worker-3", Host: "worker-3", Role: "worker", IP: "192.168.126.24", MAC: "52:54:00:6a:02:03", VCPU: 4, RAMGB: 16, Spare: true},
		{Name: "rhwa-lab-worker-4", Host: "worker-4", Role: "worker", IP: "192.168.126.25", MAC: "52:54:00:6a:02:04", VCPU: 4, RAMGB: 16, Spare: true},
		{Name: "rhwa-lab-worker-5", Host: "worker-5", Role: "worker", IP: "192.168.126.26", MAC: "52:54:00:6a:02:05", VCPU: 4, RAMGB: 16, Spare: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ComputeNodes mismatch:\n got=%#v\nwant=%#v", got, want)
	}
}
