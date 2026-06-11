"use client";

import { useState } from "react";
import {
  useWindowsSnapshots,
  useWindowsSnapshotCoverage,
  useCreateWindowsSnapshot,
  useDeleteWindowsSnapshot,
} from "@/lib/hooks/useWindowsSnapshots";
import type {
  WindowsSnapshot,
  WindowsSnapshotStatus,
  CreateWindowsSnapshotRequest,
} from "@/lib/api/endpoints/windows-snapshots";
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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  HardDrive,
  CheckCircle2,
  X,
  Trash2,
  AlertCircle,
  Loader2,
  Plus,
  Database,
  MapPin,
} from "lucide-react";
import { formatDistanceToNow } from "date-fns";

const AWS_REGIONS = [
  "us-east-1",
  "us-east-2",
  "us-west-1",
  "us-west-2",
  "eu-west-1",
  "eu-west-2",
  "eu-west-3",
  "eu-central-1",
  "eu-north-1",
  "ap-southeast-1",
  "ap-southeast-2",
  "ap-northeast-1",
  "ap-northeast-2",
  "ap-south-1",
  "ca-central-1",
  "sa-east-1",
];

export default function WindowsSnapshotsPage() {
  const [statusFilter, setStatusFilter] = useState<
    WindowsSnapshotStatus | "ALL"
  >("ALL");
  const [regionFilter, setRegionFilter] = useState<string | "ALL">("ALL");
  const [createDialogOpen, setCreateDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [selectedSnapshot, setSelectedSnapshot] =
    useState<WindowsSnapshot | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);

  // Form state for creating snapshots
  const [newSnapshot, setNewSnapshot] =
    useState<CreateWindowsSnapshotRequest>({
      region: "us-east-1",
      version: "1.0",
      s3_source_url: "",
      creation_method: "regenerate",
      source_snapshot_id: "",
      source_region: "",
    });

  // Fetch data
  const { data: coverage, isLoading: coverageLoading } =
    useWindowsSnapshotCoverage();
  const { data: snapshotsData, isLoading: snapshotsLoading } =
    useWindowsSnapshots({
      status: statusFilter === "ALL" ? undefined : statusFilter,
      region: regionFilter === "ALL" ? undefined : regionFilter,
    });

  // Mutations
  const createSnapshot = useCreateWindowsSnapshot();
  const deleteSnapshot = useDeleteWindowsSnapshot();

  const handleCreate = async () => {
    try {
      setErrorMessage(null);
      await createSnapshot.mutateAsync(newSnapshot);
      setCreateDialogOpen(false);
      setNewSnapshot({
        region: "us-east-1",
        version: "1.0",
        s3_source_url: "",
        creation_method: "regenerate",
        source_snapshot_id: "",
        source_region: "",
      });
    } catch (error) {
      console.error("Failed to create snapshot:", error);
      const errorMsg =
        error instanceof Error ? error.message : "Unknown error occurred";
      setErrorMessage(errorMsg);
    }
  };

  const handleDelete = async () => {
    if (!selectedSnapshot) return;

    try {
      setErrorMessage(null);
      await deleteSnapshot.mutateAsync(selectedSnapshot.id);
      setDeleteDialogOpen(false);
      setSelectedSnapshot(null);
    } catch (error) {
      console.error("Failed to delete snapshot:", error);
      const errorMsg =
        error instanceof Error ? error.message : "Unknown error occurred";
      setErrorMessage(errorMsg);
    }
  };

  const getStatusBadge = (status: WindowsSnapshotStatus) => {
    switch (status) {
      case "ready":
        return (
          <Badge
            variant="default"
            className="bg-green-600 flex items-center gap-1"
          >
            <CheckCircle2 className="h-3 w-3" />
            Ready
          </Badge>
        );
      case "creating":
        return (
          <Badge variant="secondary" className="flex items-center gap-1">
            <Loader2 className="h-3 w-3 animate-spin" />
            Creating
          </Badge>
        );
      case "validating":
        return (
          <Badge variant="secondary" className="flex items-center gap-1">
            <Loader2 className="h-3 w-3 animate-spin" />
            Validating
          </Badge>
        );
      case "failed":
        return (
          <Badge variant="destructive" className="flex items-center gap-1">
            <AlertCircle className="h-3 w-3" />
            Failed
          </Badge>
        );
      case "deleting":
        return (
          <Badge variant="outline" className="flex items-center gap-1">
            <Loader2 className="h-3 w-3 animate-spin" />
            Deleting
          </Badge>
        );
    }
  };

  const snapshots = snapshotsData?.snapshots || [];

  // Get available source regions for copying (regions with ready snapshots)
  const availableSourceRegions =
    coverage?.snapshots_by_region
      ? Object.entries(coverage.snapshots_by_region)
          .filter(([_, snapshot]) => snapshot.status === "ready")
          .map(([region]) => region)
      : [];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Windows Snapshots</h1>
          <p className="text-muted-foreground">
            Manage regional EBS snapshots for fast Windows VM deployment
          </p>
        </div>
        <Button onClick={() => setCreateDialogOpen(true)}>
          <Plus className="h-4 w-4 mr-2" />
          Create Snapshot
        </Button>
      </div>

      {/* Coverage Widget */}
      <Card>
        <CardHeader>
          <CardTitle>Regional Coverage</CardTitle>
        </CardHeader>
        <CardContent>
          {coverageLoading ? (
            <div className="text-center py-8 text-muted-foreground">
              Loading coverage data...
            </div>
          ) : coverage ? (
            <div className="space-y-4">
              <div className="flex items-center justify-between">
                <div>
                  <div className="text-3xl font-bold">
                    {coverage.covered_regions}/{coverage.total_regions}
                  </div>
                  <p className="text-sm text-muted-foreground">
                    Regions covered ({coverage.coverage_percent.toFixed(1)}%)
                  </p>
                </div>
                <Database className="h-12 w-12 text-muted-foreground" />
              </div>

              {/* Progress bar */}
              <div className="w-full bg-gray-200 rounded-full h-3">
                <div
                  className="bg-green-600 h-3 rounded-full transition-all"
                  style={{ width: `${coverage.coverage_percent}%` }}
                />
              </div>

              {/* Missing regions */}
              {coverage?.missing_regions && coverage.missing_regions.length > 0 && (
                <div>
                  <p className="text-sm font-medium mb-2">Missing regions:</p>
                  <div className="flex flex-wrap gap-2">
                    {coverage.missing_regions.map((region) => (
                      <Badge key={region} variant="outline">
                        <MapPin className="h-3 w-3 mr-1" />
                        {region}
                      </Badge>
                    ))}
                  </div>
                </div>
              )}

              {/* Outdated regions */}
              {coverage?.outdated_regions && coverage.outdated_regions.length > 0 && (
                <div>
                  <p className="text-sm font-medium mb-2">
                    Outdated regions (need update to v{coverage.latest_version}
                    ):
                  </p>
                  <div className="flex flex-wrap gap-2">
                    {coverage.outdated_regions.map((region) => (
                      <Badge
                        key={region}
                        variant="outline"
                        className="border-orange-500 text-orange-700"
                      >
                        <MapPin className="h-3 w-3 mr-1" />
                        {region}
                      </Badge>
                    ))}
                  </div>
                </div>
              )}
            </div>
          ) : (
            <div className="text-center py-8 text-muted-foreground">
              No coverage data available
            </div>
          )}
        </CardContent>
      </Card>

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
                onValueChange={(value) =>
                  setStatusFilter(value as WindowsSnapshotStatus | "ALL")
                }
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="ALL">All Statuses</SelectItem>
                  <SelectItem value="creating">Creating</SelectItem>
                  <SelectItem value="validating">Validating</SelectItem>
                  <SelectItem value="ready">Ready</SelectItem>
                  <SelectItem value="failed">Failed</SelectItem>
                  <SelectItem value="deleting">Deleting</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="flex-1">
              <Label>Region</Label>
              <Select
                value={regionFilter}
                onValueChange={(value) => setRegionFilter(value)}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="ALL">All Regions</SelectItem>
                  {AWS_REGIONS.map((region) => (
                    <SelectItem key={region} value={region}>
                      {region}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Snapshots Table */}
      <Card>
        <CardHeader>
          <CardTitle>Snapshots ({snapshotsData?.total || 0})</CardTitle>
        </CardHeader>
        <CardContent>
          {snapshotsLoading ? (
            <div className="text-center py-8 text-muted-foreground">
              Loading snapshots...
            </div>
          ) : snapshots.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              No snapshots found
            </div>
          ) : (
            <div className="space-y-4">
              {snapshots.map((snapshot) => (
                <div
                  key={snapshot.id}
                  className="flex items-center justify-between p-4 border rounded-lg hover:bg-muted/50"
                >
                  <div className="flex items-start gap-4 flex-1">
                    <HardDrive className="h-5 w-5 mt-0.5 text-muted-foreground" />
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 mb-1">
                        <span className="font-medium">{snapshot.region}</span>
                        {getStatusBadge(snapshot.status)}
                        <Badge variant="outline">v{snapshot.version}</Badge>
                      </div>
                      <div className="text-sm text-muted-foreground space-y-1">
                        <div>EBS Snapshot: {snapshot.ebs_snapshot_id}</div>
                        {snapshot.ssm_parameter_path && (
                          <div>SSM: {snapshot.ssm_parameter_path}</div>
                        )}
                        {snapshot.snapshot_size_gb && (
                          <div>Size: {snapshot.snapshot_size_gb} GB</div>
                        )}
                        <div>
                          Created:{" "}
                          {formatDistanceToNow(new Date(snapshot.created_at), {
                            addSuffix: true,
                          })}
                        </div>
                        {snapshot.validated_at && (
                          <div>
                            Validated:{" "}
                            {formatDistanceToNow(
                              new Date(snapshot.validated_at),
                              { addSuffix: true }
                            )}
                          </div>
                        )}
                        {snapshot.error_message && (
                          <div className="text-red-600">
                            Error: {snapshot.error_message}
                          </div>
                        )}
                      </div>
                    </div>
                  </div>

                  <div className="flex items-center gap-2">
                    {snapshot.status === "ready" && (
                      <Button
                        variant="destructive"
                        size="sm"
                        onClick={() => {
                          setSelectedSnapshot(snapshot);
                          setDeleteDialogOpen(true);
                        }}
                      >
                        <Trash2 className="h-4 w-4 mr-1" />
                        Delete
                      </Button>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Create Snapshot Dialog */}
      {createDialogOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          {/* Backdrop */}
          <div
            className="absolute inset-0 bg-black/50"
            onClick={() => setCreateDialogOpen(false)}
          />

          {/* Dialog Card */}
          <Card className="relative z-10 w-full max-w-md mx-4">
            <CardHeader>
              <div className="flex items-center justify-between">
                <CardTitle>Create Windows Snapshot</CardTitle>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setCreateDialogOpen(false)}
                  className="h-8 w-8 p-0"
                >
                  <X className="h-4 w-4" />
                </Button>
              </div>
              <p className="text-sm text-muted-foreground mt-2">
                Create a validated EBS snapshot for fast Windows VM deployment
                in a specific region
              </p>
            </CardHeader>

            <CardContent className="space-y-4">
              <div>
                <Label>Region *</Label>
                <Select
                  value={newSnapshot.region}
                  onValueChange={(value) =>
                    setNewSnapshot({ ...newSnapshot, region: value })
                  }
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {AWS_REGIONS.map((region) => (
                      <SelectItem key={region} value={region}>
                        {region}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>

              <div>
                <Label>Creation Method *</Label>
                <Select
                  value={newSnapshot.creation_method || "regenerate"}
                  onValueChange={(value) =>
                    setNewSnapshot({
                      ...newSnapshot,
                      creation_method: value as "regenerate" | "copy",
                      source_snapshot_id: value === "copy" ? newSnapshot.source_snapshot_id : "",
                      source_region: value === "copy" ? newSnapshot.source_region : "",
                    })
                  }
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="regenerate">
                      Regenerate from S3 (85 min, ~$18)
                    </SelectItem>
                    <SelectItem
                      value="copy"
                      disabled={availableSourceRegions.length === 0}
                    >
                      Copy from existing snapshot (60 min, ~$0.05)
                      {availableSourceRegions.length === 0 && " - No snapshots available"}
                    </SelectItem>
                  </SelectContent>
                </Select>
                {availableSourceRegions.length === 0 && (
                  <p className="text-xs text-muted-foreground mt-1">
                    Copy requires at least one ready snapshot in another region
                  </p>
                )}
              </div>

              {newSnapshot.creation_method === "copy" && availableSourceRegions.length > 0 && (
                <div>
                  <Label>Source Region *</Label>
                  <Select
                    value={newSnapshot.source_region || ""}
                    onValueChange={(value) => {
                      const sourceSnapshot = coverage?.snapshots_by_region[value];
                      setNewSnapshot({
                        ...newSnapshot,
                        source_region: value,
                        source_snapshot_id: sourceSnapshot?.ebs_snapshot_id || "",
                        version: sourceSnapshot?.version || newSnapshot.version,
                      });
                    }}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="Select source region" />
                    </SelectTrigger>
                    <SelectContent>
                      {availableSourceRegions.map((region) => {
                        const snapshot = coverage?.snapshots_by_region[region];
                        return (
                          <SelectItem key={region} value={region}>
                            {region} (v{snapshot?.version || "?"}, {snapshot?.snapshot_size_gb || "?"}GB)
                          </SelectItem>
                        );
                      })}
                    </SelectContent>
                  </Select>
                  <p className="text-xs text-muted-foreground mt-1">
                    Snapshot will be copied from this region
                  </p>
                </div>
              )}

              <div>
                <Label>Version *</Label>
                <Input
                  value={newSnapshot.version}
                  onChange={(e) =>
                    setNewSnapshot({ ...newSnapshot, version: e.target.value })
                  }
                  placeholder="1.0"
                />
              </div>

              <div>
                <Label>S3 Source URL (optional)</Label>
                <Input
                  value={newSnapshot.s3_source_url}
                  onChange={(e) =>
                    setNewSnapshot({
                      ...newSnapshot,
                      s3_source_url: e.target.value,
                    })
                  }
                  placeholder="s3://ocpctl-binaries/windows-images/windows-10-oadp.qcow2"
                />
                <p className="text-xs text-muted-foreground mt-1">
                  Leave blank to use default Windows image
                </p>
              </div>

              <div className="bg-blue-50 border border-blue-200 rounded-md p-4">
                <p className="text-sm text-blue-800">
                  <strong>Note:</strong>{" "}
                  {newSnapshot.creation_method === "copy" ? (
                    <>Copying a snapshot will:</>
                  ) : (
                    <>Regenerating a snapshot will:</>
                  )}
                </p>
                <ul className="text-sm text-blue-700 mt-2 ml-4 list-disc space-y-1">
                  {newSnapshot.creation_method === "copy" ? (
                    <>
                      <li>Copy EBS snapshot to target region (~60 minutes)</li>
                      <li>Add ocpctl tags to copied snapshot</li>
                      <li>Publish to SSM Parameter Store</li>
                      <li>Cost: ~$0.05 (snapshot copy transfer)</li>
                    </>
                  ) : (
                    <>
                      <li>Create a temporary OpenShift cluster (~$18 cost)</li>
                      <li>Install OpenShift Virtualization</li>
                      <li>Import Windows image from S3 (20 minutes)</li>
                      <li>Create EBS snapshot (65 minutes)</li>
                      <li>Validate by booting a test VM</li>
                      <li>Publish to SSM Parameter Store</li>
                      <li>Destroy temporary cluster</li>
                      <li>Total time: ~85 minutes</li>
                    </>
                  )}
                </ul>
              </div>

              {errorMessage && (
                <div className="bg-red-50 border border-red-200 rounded-md p-4">
                  <p className="text-sm text-red-800 font-medium">Error</p>
                  <p className="text-sm text-red-600 mt-1">{errorMessage}</p>
                </div>
              )}

              <div className="flex gap-2 justify-end pt-4">
                <Button
                  variant="outline"
                  onClick={() => {
                    setCreateDialogOpen(false);
                    setErrorMessage(null);
                  }}
                >
                  Cancel
                </Button>
                <Button
                  onClick={handleCreate}
                  disabled={
                    createSnapshot.isPending ||
                    !newSnapshot.region ||
                    !newSnapshot.version ||
                    (newSnapshot.creation_method === "copy" &&
                      (!newSnapshot.source_region || !newSnapshot.source_snapshot_id))
                  }
                >
                  {createSnapshot.isPending ? (
                    <>
                      <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                      Creating...
                    </>
                  ) : (
                    "Create Snapshot"
                  )}
                </Button>
              </div>
            </CardContent>
          </Card>
        </div>
      )}

      {/* Delete Confirmation Dialog */}
      {deleteDialogOpen && selectedSnapshot && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          {/* Backdrop */}
          <div
            className="absolute inset-0 bg-black/50"
            onClick={() => setDeleteDialogOpen(false)}
          />

          {/* Dialog Card */}
          <Card className="relative z-10 w-full max-w-md mx-4">
            <CardHeader>
              <div className="flex items-center justify-between">
                <CardTitle>Delete Snapshot</CardTitle>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => setDeleteDialogOpen(false)}
                  className="h-8 w-8 p-0"
                >
                  <X className="h-4 w-4" />
                </Button>
              </div>
            </CardHeader>

            <CardContent className="space-y-4">
              <div className="bg-red-50 border border-red-200 rounded-md p-4">
                <p className="text-sm text-red-800 font-medium">
                  Are you sure you want to delete this snapshot?
                </p>
                <p className="text-sm text-red-600 mt-2">
                  Region:{" "}
                  <span className="font-mono">{selectedSnapshot.region}</span>
                </p>
                <p className="text-sm text-red-600">
                  EBS Snapshot:{" "}
                  <span className="font-mono">
                    {selectedSnapshot.ebs_snapshot_id}
                  </span>
                </p>
                <p className="text-sm text-red-600 mt-2">
                  This will delete the EBS snapshot and SSM parameter. Future
                  Windows VM deployments in this region will fall back to slow
                  S3 import. This action cannot be undone.
                </p>
              </div>

              {errorMessage && (
                <div className="bg-red-50 border border-red-200 rounded-md p-4 mt-4">
                  <p className="text-sm text-red-800 font-medium">Error</p>
                  <p className="text-sm text-red-600 mt-1">{errorMessage}</p>
                </div>
              )}

              <div className="flex gap-2 justify-end pt-4">
                <Button
                  variant="outline"
                  onClick={() => {
                    setDeleteDialogOpen(false);
                    setErrorMessage(null);
                  }}
                >
                  Cancel
                </Button>
                <Button
                  variant="destructive"
                  onClick={handleDelete}
                  disabled={deleteSnapshot.isPending}
                >
                  {deleteSnapshot.isPending ? (
                    <>
                      <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                      Deleting...
                    </>
                  ) : (
                    "Delete Snapshot"
                  )}
                </Button>
              </div>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  );
}
