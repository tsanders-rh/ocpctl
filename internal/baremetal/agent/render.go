package agent

import (
	"bytes"
	"embed"
	"text/template"

	"github.com/tsanders-rh/ocpctl/internal/baremetal/host"
)

//go:embed templates/agent-config.yaml.tmpl
var templatesFS embed.FS

var agentConfigTmpl = template.Must(template.ParseFS(templatesFS, "templates/agent-config.yaml.tmpl"))

// masters returns the control-plane VMs (workers/spares excluded — they join via
// the MachineSet, not ABI rendezvous).
func masters(spec InstallSpec) []host.VM {
	out := make([]host.VM, 0, len(spec.Masters))
	for _, vm := range spec.Masters {
		if vm.Role == "master" && !vm.Spare {
			out = append(out, vm)
		}
	}
	return out
}

// renderAgentConfig renders agent-config.yaml with per-master static nmstate.
func renderAgentConfig(spec InstallSpec) (string, error) {
	data := struct {
		ClusterName  string
		RendezvousIP string
		NetGateway   string
		Iface        string
		Masters      []host.VM
	}{spec.ClusterName, spec.RendezvousIP, spec.NetGateway, defaultIface, masters(spec)}
	var buf bytes.Buffer
	if err := agentConfigTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
