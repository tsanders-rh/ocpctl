package odf

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"
)

//go:embed templates/*.tmpl
var templatesFS embed.FS

var tmpls = template.Must(template.ParseFS(templatesFS, "templates/*.tmpl"))

const pgNum = 64 // PGs for a small single-pool lab; the autoscaler tunes from here

// renderData is the union of fields the odf templates reference.
type renderData struct {
	// identity / network
	NodeName, Host, BaseDomain string
	SSHPubKey, MAC, IP         string
	NetGateway, LibvirtNet     string
	// ceph VM build
	ImagesDir, SeedDir string
	CloudImageURL      string
	OSVariant          string
	RAMMB, VCPU        int
	RootDiskGB         int
	OSDDevs            []string
	OSDDiskGB          int
	// ceph bootstrap / export
	Release, Image string
	RBDPool        string
	PoolReplica    int
	OSDCount       int
	PGNum          int
	ExporterURL    string
	// oc-applied manifests
	Namespace string
	Channel   string
	OC        string
}

// osdDevs returns the guest device names for the OSD data disks: root is vda, so
// the extra disks start at vdb (vdb, vdc, ...). count <= 9 is plenty for a lab.
func osdDevs(count int) []string {
	letters := "bcdefghij"
	devs := make([]string, 0, count)
	for i := 0; i < count && i < len(letters); i++ {
		devs = append(devs, "vd"+string(letters[i]))
	}
	return devs
}

func newRenderData(spec Spec, channel string) renderData {
	return renderData{
		NodeName:      spec.NodeName,
		Host:          spec.Host,
		BaseDomain:    spec.BaseDomain,
		SSHPubKey:     spec.SSHPubKey,
		MAC:           spec.MAC,
		IP:            spec.IP,
		NetGateway:    spec.NetGateway,
		LibvirtNet:    spec.LibvirtNet,
		ImagesDir:     remoteImages,
		SeedDir:       "/tmp/" + spec.NodeName + "-seed",
		CloudImageURL: orDefault(spec.CloudImageURL, defaultCloudImage),
		OSVariant:     orDefault(spec.OSVariant, defaultOSVariant),
		RAMMB:         spec.RAMGB * 1024,
		VCPU:          spec.VCPU,
		RootDiskGB:    spec.RootDiskGB,
		OSDDevs:       osdDevs(spec.OSDCount),
		OSDDiskGB:     spec.OSDDiskGB,
		Release:       spec.release(),
		Image:         spec.image(),
		RBDPool:       spec.rbdPool(),
		PoolReplica:   spec.PoolReplica,
		OSDCount:      spec.OSDCount,
		PGNum:         pgNum,
		ExporterURL:   spec.exporterURL(),
		Namespace:     spec.ns(),
		Channel:       channel,
		OC:            ocCmd,
	}
}

func orDefault(v, def string) string {
	if v != "" {
		return v
	}
	return def
}

func render(name string, data renderData) (string, error) {
	var buf bytes.Buffer
	if err := tmpls.ExecuteTemplate(&buf, name, data); err != nil {
		return "", fmt.Errorf("render %s: %w", name, err)
	}
	return buf.String(), nil
}

func renderUserData(spec Spec) (string, error) {
	return render("ceph-user-data.yaml.tmpl", newRenderData(spec, ""))
}
func renderMetaData(spec Spec) (string, error) {
	return render("ceph-meta-data.tmpl", newRenderData(spec, ""))
}
func renderNetworkConfig(spec Spec) (string, error) {
	return render("ceph-network-config.yaml.tmpl", newRenderData(spec, ""))
}
func renderDefine(spec Spec) (string, error) {
	return render("ceph-define.sh.tmpl", newRenderData(spec, ""))
}
func renderBootstrap(spec Spec) (string, error) {
	return render("ceph-bootstrap.sh.tmpl", newRenderData(spec, ""))
}
func renderExport(spec Spec) (string, error) {
	return render("ceph-export.sh.tmpl", newRenderData(spec, ""))
}
func renderOperator(spec Spec, channel string) (string, error) {
	return render("odf-operator.yaml.tmpl", newRenderData(spec, channel))
}
func renderStorageCluster(spec Spec) (string, error) {
	return render("storagecluster.yaml.tmpl", newRenderData(spec, ""))
}
