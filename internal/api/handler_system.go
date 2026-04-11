package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/tsanders-rh/ocpctl/internal/store"
)

// SystemHandler handles system/infrastructure API endpoints
type SystemHandler struct {
	store   *store.Store
	version string
}

// NewSystemHandler creates a new system handler
func NewSystemHandler(s *store.Store, version string) *SystemHandler {
	return &SystemHandler{
		store:   s,
		version: version,
	}
}

// WorkerInfo represents a worker instance
type WorkerInfo struct {
	InstanceID   string    `json:"instance_id"`
	PrivateIP    string    `json:"private_ip"`
	PublicIP     string    `json:"public_ip,omitempty"`
	LaunchTime   time.Time `json:"launch_time"`
	State        string    `json:"state"`
	HealthStatus string    `json:"health_status"`
	Type         string    `json:"type"` // "static" or "autoscale"
	Version      string    `json:"version,omitempty"`
}

// ASGInfo represents autoscaling group information
type ASGInfo struct {
	Name            string       `json:"name"`
	DesiredCapacity int          `json:"desired_capacity"`
	MinSize         int          `json:"min_size"`
	MaxSize         int          `json:"max_size"`
	Instances       []WorkerInfo `json:"instances"`
}

// InfrastructureInfo represents the overall infrastructure status
type InfrastructureInfo struct {
	APIServer struct {
		IP      string `json:"ip"`
		Version string `json:"version"`
		Status  string `json:"status"`
		Uptime  string `json:"uptime,omitempty"`
	} `json:"api_server"`
	Database struct {
		Host   string `json:"host"`
		Status string `json:"status"`
	} `json:"database"`
	StaticWorkers  []WorkerInfo `json:"static_workers"`
	AutoscaleGroup *ASGInfo     `json:"autoscale_group,omitempty"`
	Timestamp      time.Time    `json:"timestamp"`
}

// GetInfrastructure returns infrastructure status
//
//	@Summary		Get infrastructure status
//	@Description	Returns information about API server, workers, and autoscaling groups
//	@Tags			system
//	@Produce		json
//	@Success		200	{object}	InfrastructureInfo
//	@Failure		500	{object}	ErrorResponse
//	@Security		BearerAuth
//	@Router			/admin/system/infrastructure [get]
func (h *SystemHandler) GetInfrastructure(c echo.Context) error {
	ctx := c.Request().Context()

	info := InfrastructureInfo{
		Timestamp: time.Now(),
	}

	// Get API server info
	info.APIServer.IP = getAPIServerIP()
	info.APIServer.Version = h.version
	info.APIServer.Status = "healthy"

	// Get database info
	dbHost, dbStatus := h.getDatabaseInfo(ctx)
	info.Database.Host = dbHost
	info.Database.Status = dbStatus

	// Get static workers (running on API server)
	staticWorkers := h.getStaticWorkers(ctx)
	info.StaticWorkers = staticWorkers

	// Get autoscale workers from ASG
	asgInfo := h.getAutoscaleWorkers(ctx)
	if asgInfo != nil {
		info.AutoscaleGroup = asgInfo
	}

	return c.JSON(http.StatusOK, info)
}

// getAPIServerIP returns the API server IP (from EC2 metadata or fallback to hostname)
func getAPIServerIP() string {
	// Try to get from EC2 metadata
	cmd := exec.Command("ec2-metadata", "--local-ipv4")
	output, err := cmd.Output()
	if err == nil {
		parts := strings.Split(string(output), ":")
		if len(parts) == 2 {
			return strings.TrimSpace(parts[1])
		}
	}

	// Fallback: try hostname -I
	cmd = exec.Command("hostname", "-I")
	output, err = cmd.Output()
	if err == nil {
		ips := strings.Fields(string(output))
		if len(ips) > 0 {
			return ips[0] // Return first IP
		}
	}

	return "unknown" // Could not determine IP
}

// getDatabaseInfo checks database connectivity and returns host from DATABASE_URL
func (h *SystemHandler) getDatabaseInfo(ctx context.Context) (string, string) {
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if err := h.store.Ping(pingCtx); err != nil {
		return "unknown", "unhealthy"
	}

	// Parse host from DATABASE_URL environment variable
	dbHost := h.store.GetDatabaseHost()
	if dbHost == "" {
		dbHost = "unknown"
	}

	return dbHost, "healthy"
}

// getStaticWorkers returns info about static workers (running on API server)
func (h *SystemHandler) getStaticWorkers(ctx context.Context) []WorkerInfo {
	var workers []WorkerInfo

	// Check if worker service is running locally
	cmd := exec.Command("systemctl", "is-active", "ocpctl-worker")
	output, err := cmd.Output()
	isActive := err == nil && strings.TrimSpace(string(output)) == "active"

	if isActive {
		// Get version from worker health endpoint
		version := h.getWorkerVersion("localhost:8081")

		// For static workers, use systemd service start time if available
		// Otherwise use a sentinel value (Unix epoch) to indicate "unknown"
		launchTime := time.Unix(0, 0) // Will be handled specially in frontend

		workers = append(workers, WorkerInfo{
			InstanceID:   "static-worker",
			PrivateIP:    getAPIServerIP(),
			Type:         "static",
			State:        "running",
			HealthStatus: "healthy",
			Version:      version,
			LaunchTime:   launchTime,
		})
	}

	return workers
}

// getAutoscaleWorkers queries AWS for ASG information
func (h *SystemHandler) getAutoscaleWorkers(ctx context.Context) *ASGInfo {
	asgName := "ocpctl-worker-asg"

	// Get ASG details
	cmd := exec.Command("aws", "autoscaling", "describe-auto-scaling-groups",
		"--auto-scaling-group-names", asgName,
		"--query", "AutoScalingGroups[0]",
		"--output", "json")

	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var asgData struct {
		DesiredCapacity int `json:"DesiredCapacity"`
		MinSize         int `json:"MinSize"`
		MaxSize         int `json:"MaxSize"`
		Instances       []struct {
			InstanceID       string `json:"InstanceId"`
			HealthStatus     string `json:"HealthStatus"`
			LifecycleState   string `json:"LifecycleState"`
			AvailabilityZone string `json:"AvailabilityZone"`
		} `json:"Instances"`
	}

	if err := json.Unmarshal(output, &asgData); err != nil {
		return nil
	}

	asgInfo := &ASGInfo{
		Name:            asgName,
		DesiredCapacity: asgData.DesiredCapacity,
		MinSize:         asgData.MinSize,
		MaxSize:         asgData.MaxSize,
		Instances:       []WorkerInfo{},
	}

	// Get instance details for each instance
	for _, inst := range asgData.Instances {
		instanceInfo := h.getInstanceDetails(inst.InstanceID)
		if instanceInfo != nil {
			instanceInfo.HealthStatus = inst.HealthStatus
			instanceInfo.State = inst.LifecycleState
			instanceInfo.Type = "autoscale"
			asgInfo.Instances = append(asgInfo.Instances, *instanceInfo)
		}
	}

	return asgInfo
}

// getInstanceDetails gets EC2 instance details
func (h *SystemHandler) getInstanceDetails(instanceID string) *WorkerInfo {
	cmd := exec.Command("aws", "ec2", "describe-instances",
		"--instance-ids", instanceID,
		"--query", "Reservations[0].Instances[0].[PrivateIpAddress,PublicIpAddress,LaunchTime,State.Name]",
		"--output", "json")

	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var details []interface{}
	if err := json.Unmarshal(output, &details); err != nil || len(details) < 4 {
		return nil
	}

	privateIP, _ := details[0].(string)
	publicIP, _ := details[1].(string)
	launchTimeStr, _ := details[2].(string)
	state, _ := details[3].(string)

	launchTime, _ := time.Parse(time.RFC3339, launchTimeStr)

	// Try to get version from worker health endpoint
	version := ""
	if state == "running" && privateIP != "" {
		version = h.getWorkerVersion(privateIP + ":8081")
	}

	return &WorkerInfo{
		InstanceID: instanceID,
		PrivateIP:  privateIP,
		PublicIP:   publicIP,
		LaunchTime: launchTime,
		State:      state,
		Version:    version,
	}
}

// getWorkerVersion calls worker health endpoint to get version
func (h *SystemHandler) getWorkerVersion(endpoint string) string {
	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	resp, err := client.Get(fmt.Sprintf("http://%s/version", endpoint))
	if err != nil {
		return "unknown"
	}
	defer resp.Body.Close()

	var versionData struct {
		Version string `json:"version"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&versionData); err != nil {
		return "unknown"
	}

	return versionData.Version
}
