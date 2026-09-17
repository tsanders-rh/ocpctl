package host

import (
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed templates/*.tmpl
var templatesFS embed.FS

var tmpls = template.Must(template.New("host").
	Funcs(template.FuncMap{"shq": shQuote}).
	ParseFS(templatesFS, "templates/*.tmpl"))

const (
	remoteNetXMLPath     = "/tmp/rhwa-net.xml"
	remoteHAProxyCfgPath = "/etc/haproxy/haproxy.cfg"
)

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func render(name string, data any) (string, error) {
	var buf strings.Builder
	if err := tmpls.ExecuteTemplate(&buf, name, data); err != nil {
		return "", fmt.Errorf("render %s: %w", name, err)
	}
	return buf.String(), nil
}

func renderPackages() (string, error)    { return render("packages.sh.tmpl", nil) }
func renderStoragePool() (string, error) { return render("storage-pool.sh.tmpl", nil) }
func renderHAProxyReload() (string, error) {
	return render("haproxy.sh.tmpl", nil)
}

func renderLibvirtNetXML(spec *HostSpec) (string, error) {
	// The ceph VM (ODF) is not an OpenShift node, so it never lives in Nodes;
	// append its reservation only for the network render (rhwa-lab pins it too).
	reservations := spec.Nodes
	if spec.CephReservation != nil {
		reservations = append(append([]VM(nil), spec.Nodes...), *spec.CephReservation)
	}
	return render("libvirt-net.xml.tmpl", struct {
		LibvirtNet string
		NetGateway string
		Net3       string
		Nodes      []VM
	}{spec.LibvirtNet, spec.NetGateway, net3(spec.NetCIDR), reservations})
}

func renderLibvirtNetDefine(spec *HostSpec) (string, error) {
	return render("libvirt-net.sh.tmpl", struct {
		LibvirtNet string
		NetXMLPath string
	}{spec.LibvirtNet, remoteNetXMLPath})
}

func renderHAProxyCfg(spec *HostSpec) (string, error) {
	return render("haproxy.cfg.tmpl", spec)
}

func renderSushy(spec *HostSpec) (string, error) {
	return render("sushy.sh.tmpl", spec)
}

func renderDomain(spec *HostSpec, vm VM) (string, error) {
	cdrom := "--disk device=cdrom,bus=sata"
	agentISO := ""
	if vm.Role == "master" {
		// Masters boot the agent ISO. It is built later (piece 3), so the file does
		// not exist at define time; AgentISO is pre-touched below so virt-install
		// accepts the cdrom source, and the real ISO overwrites it before boot.
		agentISO = fmt.Sprintf("/var/lib/libvirt/images/%s-agent.iso", spec.ClusterName)
		cdrom = fmt.Sprintf("--disk path=%s,device=cdrom", agentISO)
	}
	return render("domain.sh.tmpl", struct {
		Name       string
		RAMMB      int
		VCPU       int
		MAC        string
		CDROM      string
		AgentISO   string
		LibvirtNet string
		NodeDiskGB int
	}{vm.Name, vm.RAMGB * 1024, vm.VCPU, vm.MAC, cdrom, agentISO, spec.LibvirtNet, spec.NodeDiskGB})
}
