package rhwa

import (
	"bytes"
	"embed"
	"text/template"
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

type opsData struct {
	OC        string
	NS        string
	Operators []Operator
}

type fenceNode struct {
	Host string
	UUID string
}

type fenceData struct {
	OC         string
	NS         string
	NetGateway string
	SushyPort  int
	SushyUser  string
	SushyPass  string
	FenceNodes []fenceNode
}

func renderOperators(spec Spec) (string, error) {
	return render("operators.sh.tmpl", opsData{
		OC:        ocCmd,
		NS:        spec.ns(),
		Operators: spec.Operators,
	})
}

func renderFencing(spec Spec) (string, error) {
	var nodes []fenceNode
	for _, n := range clusterNodes(spec.Nodes) {
		nodes = append(nodes, fenceNode{Host: n.Host, UUID: spec.UUIDs[n.Name]})
	}
	return render("fencing.sh.tmpl", fenceData{
		OC:         ocCmd,
		NS:         spec.ns(),
		NetGateway: spec.NetGateway,
		SushyPort:  spec.SushyPort,
		SushyUser:  spec.SushyUser,
		SushyPass:  spec.SushyPass,
		FenceNodes: nodes,
	})
}
