"use client";

import { useState } from "react";
import {
  useOrphanedResources,
  useOrphanedResourcesStats,
  useMarkResourceResolved,
  useMarkResourceIgnored,
  useDeleteResource,
} from "@/lib/hooks/useOrphanedResources";
import type {
  OrphanedResource,
  OrphanedResourceStatus,
  OrphanedResourceType,
} from "@/lib/api/endpoints/orphaned-resources";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { Label } from "@/components/ui/label";
import {
  AlertTriangle,
  Database,
  Globe,
  HardDrive,
  CheckCircle2,
  EyeOff,
  ExternalLink,
  X,
  Trash2,
  Shield,
  Key,
  Disc,
  Network,
  FileText,
} from "lucide-react";
import { formatDistanceToNow } from "date-fns";

export default function OrphanedResourcesPage() {
  const [statusFilter, setStatusFilter] = useState<OrphanedResourceStatus | "ALL">("ACTIVE");
  const [typeFilter, setTypeFilter] = useState<OrphanedResourceType | "ALL">("ALL");
  const [selectedResource, setSelectedResource] = useState<OrphanedResource | null>(null);
  const [actionDialog, setActionDialog] = useState<"resolve" | "ignore" | "delete" | null>(null);
  const [notes, setNotes] = useState("");
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  // Fetch data
  const { data: stats, isLoading: statsLoading } = useOrphanedResourcesStats();
  const { data: resourcesData, isLoading: resourcesLoading } = useOrphanedResources({
    status: statusFilter === "ALL" ? undefined : statusFilter,
    type: typeFilter === "ALL" ? undefined : typeFilter,
    limit: 100,
  });

  // Mutations
  const markResolved = useMarkResourceResolved();
  const markIgnored = useMarkResourceIgnored();
  const deleteResource = useDeleteResource();

  const getDeleteTitle = (resourceType: OrphanedResourceType) => {
    const titles: Record<string, string> = {
      HostedZone: "Delete Hosted Zone",
      DNSRecord: "Delete DNS Record",
      EBSVolume: "Delete EBS Volume",
      ElasticIP: "Release Elastic IP",
      IAMRole: "Delete IAM Role",
      OIDCProvider: "Delete OIDC Provider",
      CloudWatchLogGroup: "Delete Log Group",
      LoadBalancer: "Delete Load Balancer",
      VPC: "Delete VPC",
    };
    return titles[resourceType] || "Delete Resource";
  };

  const getDeleteDescription = (resourceType: OrphanedResourceType) => {
    const descriptions: Record<string, string> = {
      HostedZone: "This will delete the hosted zone and all its records from AWS Route53. This action cannot be undone.",
      DNSRecord: "This will delete the DNS record from AWS Route53. This action cannot be undone.",
      EBSVolume: "This will delete the EBS volume from AWS. This action cannot be undone.",
      ElasticIP: "This will release the Elastic IP address. This action cannot be undone.",
      IAMRole: "This will delete the IAM role and detach all policies. This action cannot be undone.",
      OIDCProvider: "This will delete the OIDC provider. This action cannot be undone.",
      CloudWatchLogGroup: "This will delete the CloudWatch log group and all its logs. This action cannot be undone.",
      LoadBalancer: "This will delete the Application/Network Load Balancer from AWS. This action cannot be undone.",
      VPC: "This will delete the VPC and ALL dependent resources (subnets, route tables, security groups, NAT gateways, internet gateways, VPC endpoints, network interfaces, etc.). This action cannot be undone.",
    };
    return descriptions[resourceType] || "This will delete the resource from AWS. This action cannot be undone.";
  };

  const handleAction = async () => {
    if (!selectedResource || !actionDialog) return;

    try {
      setErrorMessage(null); // Clear any previous errors
      if (actionDialog === "resolve") {
        await markResolved.mutateAsync({ id: selectedResource.id, notes });
      } else if (actionDialog === "ignore") {
        await markIgnored.mutateAsync({ id: selectedResource.id, notes });
      } else if (actionDialog === "delete") {
        await deleteResource.mutateAsync(selectedResource.id);
      }
      setActionDialog(null);
      setSelectedResource(null);
      setNotes("");
    } catch (error) {
      console.error("Failed to update resource:", error);
      // Show error message to user
      const errorMsg = error instanceof Error ? error.message : "Unknown error occurred";
      setErrorMessage(errorMsg);
    }
  };

  const getResourceIcon = (type: OrphanedResourceType) => {
    switch (type) {
      case "VPC":
        return Globe;
      case "LoadBalancer":
        return Database;
      case "EC2Instance":
        return HardDrive;
      case "DNSRecord":
        return Globe;
      case "HostedZone":
        return Globe;
      case "IAMRole":
        return Shield;
      case "OIDCProvider":
        return Key;
      case "EBSVolume":
        return Disc;
      case "ElasticIP":
        return Network;
      case "CloudWatchLogGroup":
        return FileText;
      default:
        return AlertTriangle;
    }
  };

  const getStatusBadge = (status: OrphanedResourceStatus) => {
    switch (status) {
      case "ACTIVE":
        return (
          <Badge variant="destructive" className="flex items-center gap-1">
            <AlertTriangle className="h-3 w-3" />
            Active
          </Badge>
        );
      case "RESOLVED":
        return (
          <Badge variant="default" className="bg-green-600 flex items-center gap-1">
            <CheckCircle2 className="h-3 w-3" />
            Resolved
          </Badge>
        );
      case "IGNORED":
        return (
          <Badge variant="outline" className="flex items-center gap-1">
            <EyeOff className="h-3 w-3" />
            Ignored
          </Badge>
        );
    }
  };

  const getAWSConsoleLink = (resource: OrphanedResource) => {
    const region = resource.region === "global" ? "us-east-1" : resource.region;
    const baseUrl = `https://console.aws.amazon.com`;

    switch (resource.resource_type) {
      case "VPC":
        return `${baseUrl}/vpc/home?region=${region}#VpcDetails:VpcId=${resource.resource_id}`;
      case "LoadBalancer":
        return `${baseUrl}/ec2/home?region=${region}#LoadBalancers:`;
      case "EC2Instance":
        return `${baseUrl}/ec2/home?region=${region}#Instances:instanceId=${resource.resource_id}`;
      case "DNSRecord":
        return `${baseUrl}/route53/v2/hostedzones`;
      case "HostedZone":
        return `${baseUrl}/route53/v2/hostedzones`;
      case "IAMRole":
        return `${baseUrl}/iam/home#/roles`;
      case "OIDCProvider":
        return `${baseUrl}/iam/home#/identity_providers`;
      case "EBSVolume":
        return `${baseUrl}/ec2/home?region=${region}#Volumes:volumeId=${resource.resource_id}`;
      case "ElasticIP":
        return `${baseUrl}/ec2/home?region=${region}#Addresses:`;
      case "CloudWatchLogGroup":
        return `${baseUrl}/cloudwatch/home?region=${region}#logsV2:log-groups/log-group/${encodeURIComponent(resource.resource_id)}`;
      default:
        return `${baseUrl}`;
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Orphaned Cloud Resources</h1>
        <p className="text-muted-foreground">
          Cloud resources that exist but have no matching cluster in the database
        </p>
      </div>

      {/* Stats Cards */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Active Orphans</CardTitle>
            <AlertTriangle className="h-4 w-4 text-red-600" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-red-600">
              {statsLoading ? "..." : stats?.total_active || 0}
            </div>
            <p className="text-xs text-muted-foreground">Require attention</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">VPCs</CardTitle>
            <Globe className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {statsLoading ? "..." : stats?.by_type?.VPC || 0}
            </div>
            <p className="text-xs text-muted-foreground">Orphaned VPCs</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Load Balancers</CardTitle>
            <Database className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {statsLoading ? "..." : stats?.by_type?.LoadBalancer || 0}
            </div>
            <p className="text-xs text-muted-foreground">Orphaned LBs</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">EC2 Instances</CardTitle>
            <HardDrive className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {statsLoading ? "..." : stats?.by_type?.EC2Instance || 0}
            </div>
            <p className="text-xs text-muted-foreground">Orphaned instances</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">EBS Volumes</CardTitle>
            <Disc className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {statsLoading ? "..." : stats?.by_type?.EBSVolume || 0}
            </div>
            <p className="text-xs text-muted-foreground">Orphaned volumes</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Elastic IPs</CardTitle>
            <Network className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {statsLoading ? "..." : stats?.by_type?.ElasticIP || 0}
            </div>
            <p className="text-xs text-muted-foreground">Orphaned IPs</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">IAM Roles</CardTitle>
            <Shield className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {statsLoading ? "..." : stats?.by_type?.IAMRole || 0}
            </div>
            <p className="text-xs text-muted-foreground">Orphaned roles</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Log Groups</CardTitle>
            <FileText className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {statsLoading ? "..." : stats?.by_type?.CloudWatchLogGroup || 0}
            </div>
            <p className="text-xs text-muted-foreground">Orphaned logs</p>
          </CardContent>
        </Card>
      </div>

      {/* Filters */}
      <Card>
        <CardHeader>
          <CardTitle>Filters</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex gap-4">
            <div className="flex-1">
              <Label>Status</Label>
              <Select
                value={statusFilter}
                onValueChange={(value) => setStatusFilter(value as OrphanedResourceStatus | "ALL")}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="ALL">All Statuses</SelectItem>
                  <SelectItem value="ACTIVE">Active</SelectItem>
                  <SelectItem value="RESOLVED">Resolved</SelectItem>
                  <SelectItem value="IGNORED">Ignored</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="flex-1">
              <Label>Resource Type</Label>
              <Select
                value={typeFilter}
                onValueChange={(value) => setTypeFilter(value as OrphanedResourceType | "ALL")}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="ALL">All Types</SelectItem>
                  <SelectItem value="VPC">VPC</SelectItem>
                  <SelectItem value="LoadBalancer">Load Balancer</SelectItem>
                  <SelectItem value="EC2Instance">EC2 Instance</SelectItem>
                  <SelectItem value="DNSRecord">DNS Record</SelectItem>
                  <SelectItem value="HostedZone">Hosted Zone</SelectItem>
                  <SelectItem value="IAMRole">IAM Role</SelectItem>
                  <SelectItem value="OIDCProvider">OIDC Provider</SelectItem>
                  <SelectItem value="EBSVolume">EBS Volume</SelectItem>
                  <SelectItem value="ElasticIP">Elastic IP</SelectItem>
                  <SelectItem value="CloudWatchLogGroup">CloudWatch Log Group</SelectItem>
                </SelectContent>
              </Select>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Resources Table */}
      <Card>
        <CardHeader>
          <CardTitle>Orphaned Resources ({resourcesData?.total || 0})</CardTitle>
        </CardHeader>
        <CardContent>
          {resourcesLoading ? (
            <div className="text-center py-8 text-muted-foreground">Loading...</div>
          ) : !resourcesData?.resources || resourcesData.resources.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              No orphaned resources found
            </div>
          ) : (
            <div className="space-y-4">
              {resourcesData.resources.map((resource) => {
                const Icon = getResourceIcon(resource.resource_type);
                return (
                  <div
                    key={resource.id}
                    className="flex items-center justify-between p-4 border rounded-lg hover:bg-muted/50"
                  >
                    <div className="flex items-start gap-4 flex-1">
                      <Icon className="h-5 w-5 mt-0.5 text-muted-foreground" />
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2 mb-1">
                          <span className="font-medium">{resource.resource_name}</span>
                          {getStatusBadge(resource.status)}
                          <Badge variant="outline">{resource.resource_type}</Badge>
                        </div>
                        <div className="text-sm text-muted-foreground space-y-1">
                          <div>ID: {resource.resource_id}</div>
                          <div>Region: {resource.region}</div>
                          {resource.cluster_name && (
                            <div>Cluster: {resource.cluster_name}</div>
                          )}
                          <div>
                            First detected:{" "}
                            {formatDistanceToNow(new Date(resource.first_detected_at), {
                              addSuffix: true,
                            })}
                          </div>
                          <div>
                            Detected {resource.detection_count} time
                            {resource.detection_count !== 1 ? "s" : ""}
                          </div>
                          {resource.notes && (
                            <div className="italic">Notes: {resource.notes}</div>
                          )}
                        </div>
                      </div>
                    </div>

                    <div className="flex items-center gap-2">
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => window.open(getAWSConsoleLink(resource), "_blank")}
                      >
                        <ExternalLink className="h-4 w-4 mr-1" />
                        AWS Console
                      </Button>

                      {resource.status === "ACTIVE" && (
                        <>
                          {(resource.resource_type === "HostedZone" ||
                            resource.resource_type === "DNSRecord" ||
                            resource.resource_type === "EBSVolume" ||
                            resource.resource_type === "ElasticIP" ||
                            resource.resource_type === "IAMRole" ||
                            resource.resource_type === "OIDCProvider" ||
                            resource.resource_type === "CloudWatchLogGroup" ||
                            resource.resource_type === "LoadBalancer" ||
                            resource.resource_type === "VPC") && (
                            <Button
                              variant="destructive"
                              size="sm"
                              onClick={() => {
                                setSelectedResource(resource);
                                setActionDialog("delete");
                              }}
                            >
                              <Trash2 className="h-4 w-4 mr-1" />
                              Delete
                            </Button>
                          )}
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => {
                              setSelectedResource(resource);
                              setActionDialog("resolve");
                            }}
                          >
                            <CheckCircle2 className="h-4 w-4 mr-1" />
                            Resolve
                          </Button>
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => {
                              setSelectedResource(resource);
                              setActionDialog("ignore");
                            }}
                          >
                            <EyeOff className="h-4 w-4 mr-1" />
                            Ignore
                          </Button>
                        </>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Action Dialog */}
      {actionDialog && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          {/* Backdrop */}
          <div
            className="absolute inset-0 bg-black/50"
            onClick={() => setActionDialog(null)}
          />

          {/* Dialog Card */}
          <Card className="relative z-10 w-full max-w-md mx-4">
            <CardHeader>
              <div className="flex items-center justify-between">
                <CardTitle>
                  {actionDialog === "resolve"
                    ? "Mark as Resolved"
                    : actionDialog === "delete" && selectedResource
                    ? getDeleteTitle(selectedResource.resource_type)
                    : "Mark as Ignored"}
                </CardTitle>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setActionDialog(null)}
                  className="h-8 w-8 p-0"
                >
                  <X className="h-4 w-4" />
                </Button>
              </div>
              <p className="text-sm text-muted-foreground mt-2">
                {actionDialog === "resolve"
                  ? "Mark this resource as resolved (e.g., manually cleaned up in AWS Console)"
                  : actionDialog === "delete" && selectedResource
                  ? getDeleteDescription(selectedResource.resource_type)
                  : "Mark this resource as ignored (e.g., false positive or intentionally kept)"}
              </p>
            </CardHeader>

            <CardContent className="space-y-4">
              {actionDialog !== "delete" && (
                <div>
                  <Label>Notes (optional)</Label>
                  <Textarea
                    value={notes}
                    onChange={(e) => setNotes(e.target.value)}
                    placeholder="Add notes about why this resource was resolved/ignored..."
                    rows={3}
                  />
                </div>
              )}

              {actionDialog === "delete" && selectedResource && (
                <div className="bg-red-50 border border-red-200 rounded-md p-4">
                  <p className="text-sm text-red-800 font-medium">
                    Are you sure you want to delete this {selectedResource.resource_type}?
                  </p>
                  <p className="text-sm text-red-600 mt-2">
                    <span className="font-mono">{selectedResource.resource_name}</span>
                  </p>
                  <p className="text-sm text-red-600 mt-2">
                    {getDeleteDescription(selectedResource.resource_type)}
                  </p>
                </div>
              )}

              {errorMessage && (
                <div className="bg-red-50 border border-red-200 rounded-md p-4 mt-4">
                  <p className="text-sm text-red-800 font-medium">Error</p>
                  <p className="text-sm text-red-600 mt-1">{errorMessage}</p>
                </div>
              )}

              <div className="flex gap-2 justify-end pt-4">
                <Button variant="outline" onClick={() => {
                  setActionDialog(null);
                  setErrorMessage(null);
                }}>
                  Cancel
                </Button>
                <Button
                  onClick={handleAction}
                  disabled={markResolved.isPending || markIgnored.isPending || deleteResource.isPending}
                  variant={actionDialog === "delete" ? "destructive" : "default"}
                >
                  {actionDialog === "resolve"
                    ? "Mark Resolved"
                    : actionDialog === "delete" && selectedResource
                    ? getDeleteTitle(selectedResource.resource_type)
                    : "Mark Ignored"}
                </Button>
              </div>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  );
}
