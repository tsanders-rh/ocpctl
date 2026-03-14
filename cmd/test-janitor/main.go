package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

func extractClusterNameFromIAMRole(roleName string) string {
	var prefix string
	if strings.Contains(roleName, "-openshift-") {
		parts := strings.SplitN(roleName, "-openshift-", 2)
		if len(parts) == 2 {
			prefix = parts[0]
		}
	} else if strings.HasSuffix(roleName, "-master-role") {
		prefix = strings.TrimSuffix(roleName, "-master-role")
	} else if strings.HasSuffix(roleName, "-worker-role") {
		prefix = strings.TrimSuffix(roleName, "-worker-role")
	}

	if prefix == "" {
		return ""
	}

	parts := strings.Split(prefix, "-")
	if len(parts) < 2 {
		return ""
	}

	lastSegment := parts[len(parts)-1]
	if len(lastSegment) == 5 {
		return strings.Join(parts[0:len(parts)-1], "-")
	}

	return ""
}

func main() {
	ctx := context.Background()

	// Connect to database
	dbHost := os.Getenv("DB_HOST")
	if dbHost == "" {
		dbHost = "database-1.cjwlrafymtxz.us-east-1.rds.amazonaws.com"
	}
	dbUser := os.Getenv("DB_USER")
	if dbUser == "" {
		dbUser = "ocpctl_user"
	}
	dbPassword := os.Getenv("DB_PASSWORD")
	if dbPassword == "" {
		dbPassword = "wK72nPqR4tLmS8vZ"
	}
	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		dbName = "ocpctl"
	}

	dsn := fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=require",
		dbUser, dbPassword, dbHost, dbName)

	st, err := store.NewStore(dsn)
	if err != nil {
		log.Fatal(err)
	}

	// Get all clusters from database
	clusters, err := st.Clusters.ListAll(ctx)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Total clusters in database: %d\n", len(clusters))

	// Build lookup map
	clustersByName := make(map[string]*types.Cluster)
	for _, cluster := range clusters {
		clustersByName[cluster.Name] = cluster
	}

	// Show first 10 cluster names
	fmt.Println("\nFirst 10 cluster names in database:")
	for i, cluster := range clusters {
		if i >= 10 {
			break
		}
		fmt.Printf("  %s (status=%s)\n", cluster.Name, cluster.Status)
	}

	// Load AWS config
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatal(err)
	}

	iamClient := iam.NewFromConfig(cfg)

	// Scan IAM roles with pagination
	paginator := iam.NewListRolesPaginator(iamClient, &iam.ListRolesInput{})

	totalRoles := 0
	openshiftRoles := 0
	orphanedRoles := 0
	clusterNamesFromRoles := make(map[string]int)

	fmt.Println("\nScanning IAM roles...")

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			log.Fatal(err)
		}

		for _, role := range page.Roles {
			totalRoles++
			roleName := aws.ToString(role.RoleName)

			if !strings.Contains(roleName, "-openshift-") &&
				!strings.HasSuffix(roleName, "-master-role") &&
				!strings.HasSuffix(roleName, "-worker-role") {
				continue
			}

			openshiftRoles++

			clusterName := extractClusterNameFromIAMRole(roleName)
			if clusterName == "" {
				continue
			}

			clusterNamesFromRoles[clusterName]++

			cluster, exists := clustersByName[clusterName]
			if !exists || cluster.Status == types.ClusterStatusDestroyed {
				orphanedRoles++
				if orphanedRoles <= 5 {
					fmt.Printf("  ORPHAN: %s -> cluster '%s' (exists=%v)\n", roleName, clusterName, exists)
				}
			}
		}
	}

	fmt.Printf("\nResults:\n")
	fmt.Printf("  Total IAM roles scanned: %d\n", totalRoles)
	fmt.Printf("  OpenShift-related roles: %d\n", openshiftRoles)
	fmt.Printf("  Orphaned roles detected: %d\n", orphanedRoles)
	fmt.Printf("  Unique cluster names from roles: %d\n", len(clusterNamesFromRoles))

	fmt.Println("\nTop 10 clusters by role count:")
	type kv struct {
		key   string
		value int
	}
	var sorted []kv
	for k, v := range clusterNamesFromRoles {
		sorted = append(sorted, kv{k, v})
	}

	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j].value > sorted[i].value {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	for i := 0; i < 10 && i < len(sorted); i++ {
		cluster, exists := clustersByName[sorted[i].key]
		status := "NOT_IN_DB"
		if exists {
			status = string(cluster.Status)
		}
		fmt.Printf("  %s: %d roles (status=%s)\n", sorted[i].key, sorted[i].value, status)
	}
}
