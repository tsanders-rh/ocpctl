package host

import (
	"fmt"
	"strings"
)

func net3(cidr string) string {
	ip := cidr
	if i := strings.IndexByte(ip, '/'); i >= 0 {
		ip = ip[:i]
	}
	if i := strings.LastIndexByte(ip, '.'); i >= 0 {
		return ip[:i]
	}
	return ip
}

// ComputeNodes ports rhwa-lab's compute_nodes/compute_spares: masters .11+,
// workers/spares .21+, MAC 52:54:00:6a:<01|02>:<idx>. Spares continue the
// worker index sequence and are indistinguishable from installed workers
// except for the Spare flag.
func ComputeNodes(t Topology) []VM {
	n3 := net3(t.NetCIDR)
	vms := make([]VM, 0, t.ControlPlaneCount+t.WorkerCount+t.SpareCount)
	for i := 0; i < t.ControlPlaneCount; i++ {
		vms = append(vms, VM{
			Name:  fmt.Sprintf("%s-master-%d", t.ClusterName, i),
			Host:  fmt.Sprintf("master-%d", i),
			Role:  "master",
			IP:    fmt.Sprintf("%s.%d", n3, 11+i),
			MAC:   fmt.Sprintf("52:54:00:6a:01:%02x", i),
			VCPU:  t.CPVCPU,
			RAMGB: t.CPRAMGB,
		})
	}
	for i := 0; i < t.WorkerCount; i++ {
		vms = append(vms, VM{
			Name:  fmt.Sprintf("%s-worker-%d", t.ClusterName, i),
			Host:  fmt.Sprintf("worker-%d", i),
			Role:  "worker",
			IP:    fmt.Sprintf("%s.%d", n3, 21+i),
			MAC:   fmt.Sprintf("52:54:00:6a:02:%02x", i),
			VCPU:  t.WKVCPU,
			RAMGB: t.WKRAMGB,
		})
	}
	for i := 0; i < t.SpareCount; i++ {
		idx := t.WorkerCount + i
		vms = append(vms, VM{
			Name:  fmt.Sprintf("%s-worker-%d", t.ClusterName, idx),
			Host:  fmt.Sprintf("worker-%d", idx),
			Role:  "worker",
			IP:    fmt.Sprintf("%s.%d", n3, 21+idx),
			MAC:   fmt.Sprintf("52:54:00:6a:02:%02x", idx),
			VCPU:  t.WKVCPU,
			RAMGB: t.WKRAMGB,
			Spare: true,
		})
	}
	return vms
}
