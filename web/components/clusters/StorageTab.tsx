"use client";

import { useState } from "react";
import { useClusterStorage, useUnlinkStorage } from "@/lib/hooks/useStorage";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Database, Link2, Trash2, HardDrive, FolderOpen } from "lucide-react";
import type { StorageGroupResponse } from "@/lib/api/endpoints/storage";
import { LinkStorageDialog } from "./LinkStorageDialog";

interface StorageTabProps {
  clusterId: string;
  platform: string;
}

export function StorageTab({ clusterId, platform }: StorageTabProps) {
  const [showLinkDialog, setShowLinkDialog] = useState(false);
  const { data: storageGroups, isLoading, error } = useClusterStorage(clusterId);
  const unlinkMutation = useUnlinkStorage();

  // Shared storage is AWS-only (uses EFS and S3)
  const isAWS = platform === "aws";
  const disabledReason = !isAWS
    ? `Shared storage is only available for AWS clusters (uses EFS and S3). This cluster is ${platform.toUpperCase()}.`
    : "";

  const handleUnlink = (storageGroupId: string) => {
    if (!confirm("Are you sure you want to unlink this storage? This will remove access to the shared storage for this cluster.")) {
      return;
    }

    unlinkMutation.mutate(
      { clusterId, storageGroupId },
      {
        onSuccess: () => {
          // Success notification could be added here
        },
        onError: (error) => {
          alert(`Failed to unlink storage: ${error.message}`);
        },
      }
    );
  };

  const getStatusBadge = (status: string) => {
    switch (status) {
      case "READY":
        return <Badge variant="default" className="bg-green-600">Ready</Badge>;
      case "PROVISIONING":
        return <Badge variant="default" className="bg-blue-600">Provisioning</Badge>;
      case "DELETING":
        return <Badge variant="default" className="bg-yellow-600">Deleting</Badge>;
      case "FAILED":
        return <Badge variant="destructive">Failed</Badge>;
      default:
        return <Badge variant="outline">{status}</Badge>;
    }
  };

  if (isLoading) {
    return (
      <div className="text-sm text-muted-foreground">
        Loading storage configuration...
      </div>
    );
  }

  if (error) {
    return (
      <div className="text-sm text-red-500">
        Error loading storage: {error.message}
      </div>
    );
  }

  return (
    <>
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <p className="text-sm text-muted-foreground">
            Manage shared storage for migration testing between clusters
          </p>
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <span>
                  <Button
                    onClick={() => setShowLinkDialog(true)}
                    size="sm"
                    className="gap-2"
                    disabled={!isAWS}
                  >
                    <Link2 className="h-4 w-4" />
                    Link to Cluster
                  </Button>
                </span>
              </TooltipTrigger>
              {!isAWS && (
                <TooltipContent>
                  <p className="max-w-xs">{disabledReason}</p>
                </TooltipContent>
              )}
            </Tooltip>
          </TooltipProvider>
        </div>

        {!storageGroups || storageGroups.length === 0 ? (
          <Card>
            <CardContent className="flex flex-col items-center justify-center py-10">
              <Database className="h-12 w-12 text-muted-foreground mb-4" />
              <h3 className="text-lg font-semibold mb-2">No Storage Configured</h3>
              <p className="text-sm text-muted-foreground text-center max-w-md mb-4">
                {isAWS
                  ? "Link this cluster to another cluster to create shared EFS and S3 storage for migration testing."
                  : `Shared storage is only available for AWS clusters. This ${platform.toUpperCase()} cluster cannot use shared storage.`}
              </p>
              <TooltipProvider>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span>
                      <Button
                        onClick={() => setShowLinkDialog(true)}
                        className="gap-2"
                        disabled={!isAWS}
                      >
                        <Link2 className="h-4 w-4" />
                        Link to Cluster
                      </Button>
                    </span>
                  </TooltipTrigger>
                  {!isAWS && (
                    <TooltipContent>
                      <p className="max-w-xs">{disabledReason}</p>
                    </TooltipContent>
                  )}
                </Tooltip>
              </TooltipProvider>
            </CardContent>
          </Card>
        ) : (
          <div className="space-y-4">
            {storageGroups.map((group: StorageGroupResponse) => (
              <Card key={group.id}>
                <CardHeader>
                  <div className="flex items-center justify-between">
                    <div>
                      <CardTitle className="text-base flex items-center gap-2">
                        <Database className="h-4 w-4" />
                        {group.name}
                      </CardTitle>
                      <p className="text-sm text-muted-foreground mt-1">
                        Region: {group.region}
                      </p>
                    </div>
                    <div className="flex items-center gap-2">
                      {getStatusBadge(group.status)}
                      {group.status === "READY" && (
                        <Button
                          onClick={() => handleUnlink(group.id)}
                          size="sm"
                          variant="destructive"
                          disabled={unlinkMutation.isPending}
                          className="gap-2"
                        >
                          <Trash2 className="h-4 w-4" />
                          Unlink
                        </Button>
                      )}
                    </div>
                  </div>
                </CardHeader>
                <CardContent>
                  <div className="space-y-3">
                    {/* Storage Resources */}
                    <div className="grid grid-cols-2 gap-4">
                      {group.efs_id && (
                        <div className="flex items-start gap-2">
                          <HardDrive className="h-4 w-4 text-muted-foreground mt-0.5" />
                          <div>
                            <p className="text-sm font-medium">EFS File System</p>
                            <p className="text-xs text-muted-foreground font-mono">
                              {group.efs_id}
                            </p>
                          </div>
                        </div>
                      )}
                      {group.s3_bucket && (
                        <div className="flex items-start gap-2">
                          <FolderOpen className="h-4 w-4 text-muted-foreground mt-0.5" />
                          <div>
                            <p className="text-sm font-medium">S3 Bucket</p>
                            <p className="text-xs text-muted-foreground font-mono">
                              {group.s3_bucket}
                            </p>
                          </div>
                        </div>
                      )}
                    </div>

                    {/* Linked Clusters */}
                    {group.linked_clusters.length > 0 && (
                      <div>
                        <p className="text-sm font-medium mb-2">Linked Clusters</p>
                        <div className="flex flex-wrap gap-2">
                          {group.linked_clusters.map((link) => (
                            <Badge key={link.cluster_id} variant="outline">
                              {link.cluster_name}
                              {link.role === "source" && " (Source)"}
                              {link.role === "target" && " (Target)"}
                            </Badge>
                          ))}
                        </div>
                      </div>
                    )}

                    {/* Status Messages */}
                    {group.status === "PROVISIONING" && (
                      <div className="text-sm text-muted-foreground bg-blue-50 dark:bg-blue-950 p-3 rounded-md">
                        Storage is being provisioned. This typically takes 5-10 minutes.
                      </div>
                    )}
                    {group.status === "DELETING" && (
                      <div className="text-sm text-muted-foreground bg-yellow-50 dark:bg-yellow-950 p-3 rounded-md">
                        Storage is being deleted. AWS resources are being cleaned up.
                      </div>
                    )}
                    {group.status === "FAILED" && (
                      <div className="text-sm text-red-600 bg-red-50 dark:bg-red-950 p-3 rounded-md">
                        Storage provisioning failed. Check deployment logs for details.
                      </div>
                    )}
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        )}
      </div>

      <LinkStorageDialog
        sourceClusterId={clusterId}
        isOpen={showLinkDialog}
        onClose={() => setShowLinkDialog(false)}
      />
    </>
  );
}
