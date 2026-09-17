package host

import (
	"strings"
	"testing"
)

func testSpec() *HostSpec {
	return &HostSpec{
		ClusterName: "rhwa-lab",
		LibvirtNet:  "rhwa",
		NetCIDR:     "192.168.126.0/24",
		NetGateway:  "192.168.126.1",
		APIVIP:      "192.168.126.5",
		IngressVIP:  "192.168.126.6",
		SushyPort:   8000,
		SushyUser:   "admin",
		SushyPass:   "password",
		NodeDiskGB:  120,
		Nodes: []VM{
			{Name: "rhwa-lab-master-0", Host: "master-0", Role: "master", IP: "192.168.126.11", MAC: "52:54:00:6a:01:00", VCPU: 8, RAMGB: 20},
			{Name: "rhwa-lab-worker-0", Host: "worker-0", Role: "worker", IP: "192.168.126.21", MAC: "52:54:00:6a:02:00", VCPU: 4, RAMGB: 16},
			{Name: "rhwa-lab-worker-1", Host: "worker-1", Role: "worker", IP: "192.168.126.22", MAC: "52:54:00:6a:02:01", VCPU: 4, RAMGB: 16, Spare: true},
		},
	}
}

func TestRenderLibvirtNetXML(t *testing.T) {
	got, err := renderLibvirtNetXML(testSpec())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<name>rhwa</name>",
		"<range start='192.168.126.100' end='192.168.126.199'/>",
		"<host mac='52:54:00:6a:01:00' name='master-0' ip='192.168.126.11'/>",
		"<host mac='52:54:00:6a:02:01' name='worker-1' ip='192.168.126.22'/>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("net XML missing %q:\n%s", want, got)
		}
	}
	// No ceph reservation unless one is set (ODF disabled).
	if strings.Contains(got, "52:54:00:6a:03:00") {
		t.Fatalf("net XML has an unexpected ceph reservation:\n%s", got)
	}
}

func TestRenderLibvirtNetXMLCephReservation(t *testing.T) {
	spec := testSpec()
	spec.CephReservation = &VM{Name: "rhwa-lab-ceph-0", Host: "ceph-0", IP: "192.168.126.10", MAC: "52:54:00:6a:03:00"}
	got, err := renderLibvirtNetXML(spec)
	if err != nil {
		t.Fatal(err)
	}
	// The ceph VM is pinned alongside the nodes (rhwa-lab's CEPH_ENABLED block).
	want := "<host mac='52:54:00:6a:03:00' name='ceph-0' ip='192.168.126.10'/>"
	if !strings.Contains(got, want) {
		t.Fatalf("net XML missing ceph reservation %q:\n%s", want, got)
	}
	if !strings.Contains(got, "name='master-0'") {
		t.Fatalf("net XML dropped node reservations:\n%s", got)
	}
}

func TestRenderHAProxyCfg(t *testing.T) {
	got, err := renderHAProxyCfg(testSpec())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "server apivip 192.168.126.5:6443 check") {
		t.Fatalf("haproxy cfg missing api vip:\n%s", got)
	}
	if !strings.Contains(got, "server ingressvip 192.168.126.6:443 check") {
		t.Fatalf("haproxy cfg missing ingress https vip:\n%s", got)
	}
	if !strings.Contains(got, "server ingressvip 192.168.126.6:80 check") {
		t.Fatalf("haproxy cfg missing ingress http vip:\n%s", got)
	}
}

func TestRenderSushy(t *testing.T) {
	got, err := renderSushy(testSpec())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"SUSHY_EMULATOR_LISTEN_PORT = 8000",
		"/CN=192.168.126.1",
		"htpasswd -bcB /etc/rhwa-sushy/htpasswd 'admin' 'password'",
		"quay.io/metal3-io/sushy-tools:latest",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("sushy script missing %q:\n%s", want, got)
		}
	}
}

func TestRenderDomainMasterGetsAgentISO(t *testing.T) {
	spec := testSpec()
	got, err := renderDomain(spec, spec.Nodes[0]) // master
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "path=/var/lib/libvirt/images/rhwa-lab-agent.iso,device=cdrom") {
		t.Fatalf("master domain missing agent ISO cdrom:\n%s", got)
	}
	// The agent ISO is built later, so the master domain must pre-touch a
	// placeholder or virt-install rejects the non-existent cdrom source.
	if !strings.Contains(got, "] || : > /var/lib/libvirt/images/rhwa-lab-agent.iso") {
		t.Fatalf("master domain missing agent ISO placeholder touch:\n%s", got)
	}
	if !strings.Contains(got, "qemu-img create -f qcow2 /var/lib/libvirt/images/rhwa-lab-master-0.qcow2 120G") {
		t.Fatalf("master domain missing disk create:\n%s", got)
	}
	if !strings.Contains(got, "--memory 20480") {
		t.Fatalf("master domain RAM MB wrong (want 20480):\n%s", got)
	}
}

func TestRenderDomainWorkerGetsEmptyTray(t *testing.T) {
	spec := testSpec()
	got, err := renderDomain(spec, spec.Nodes[1]) // worker
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "--disk device=cdrom,bus=sata") {
		t.Fatalf("worker domain missing empty cdrom tray:\n%s", got)
	}
	if strings.Contains(got, "agent.iso") {
		t.Fatalf("worker domain should not weld agent ISO:\n%s", got)
	}
}

func TestRenderSushyEscapesSpecialChars(t *testing.T) {
	spec := testSpec()
	spec.SushyPass = "pa'ss"
	got, err := renderSushy(spec)
	if err != nil {
		t.Fatal(err)
	}
	want := `'pa'\''ss'`
	if !strings.Contains(got, want) {
		t.Fatalf("sushy script missing escaped password token %q:\n%s", want, got)
	}
	if strings.Contains(got, "'pa'ss'") {
		t.Fatalf("sushy script contains unescaped quote breaking shell quoting:\n%s", got)
	}
}

func TestRenderStaticScriptsParse(t *testing.T) {
	if _, err := renderPackages(); err != nil {
		t.Fatalf("renderPackages: %v", err)
	}
	if _, err := renderStoragePool(); err != nil {
		t.Fatalf("renderStoragePool: %v", err)
	}
	if _, err := renderHAProxyReload(); err != nil {
		t.Fatalf("renderHAProxyReload: %v", err)
	}
	got, err := renderLibvirtNetDefine(testSpec())
	if err != nil {
		t.Fatalf("renderLibvirtNetDefine: %v", err)
	}
	if !strings.Contains(got, "virsh net-info rhwa") {
		t.Fatalf("net define missing net name:\n%s", got)
	}
	if !strings.Contains(got, remoteNetXMLPath) {
		t.Fatalf("net define missing xml path %q:\n%s", remoteNetXMLPath, got)
	}
}
