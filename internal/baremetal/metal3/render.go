package metal3

import (
	"bytes"
	"embed"
	"text/template"

	"github.com/tsanders-rh/ocpctl/internal/baremetal/host"
)

//go:embed templates/*.sh.tmpl
var templatesFS embed.FS

var tmpls = template.Must(template.ParseFS(templatesFS, "templates/*.sh.tmpl"))

func render(name string, data any) (string, error) {
	var buf bytes.Buffer
	if err := tmpls.ExecuteTemplate(&buf, name, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

type bmhData struct {
	OC      string
	Host    string
	BMCUser string
	BMCPass string
	MAC     string
	BMC     string
}

func renderMasterBMH(spec Spec, vm host.VM) (string, error) {
	return render("bmh-master.sh.tmpl", bmhData{
		OC:      ocCmd,
		Host:    vm.Host,
		BMCUser: spec.BMCUser,
		BMCPass: spec.BMCPass,
		BMC:     bmcAddress(spec.NetGateway, spec.SushyPort, spec.UUIDs[vm.Name]),
	})
}

func renderEvict() (string, error) {
	return render("evict-daemons.sh.tmpl", struct{ OC string }{OC: ocCmd})
}

func renderWorkerBMH(spec Spec, vm host.VM) (string, error) {
	return render("bmh-worker.sh.tmpl", bmhData{
		OC:      ocCmd,
		Host:    vm.Host,
		BMCUser: spec.BMCUser,
		BMCPass: spec.BMCPass,
		MAC:     vm.MAC,
		BMC:     bmcAddress(spec.NetGateway, spec.SushyPort, spec.UUIDs[vm.Name]),
	})
}
