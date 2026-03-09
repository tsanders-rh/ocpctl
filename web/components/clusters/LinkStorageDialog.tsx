"use client";

import { useState, useEffect } from "react";
import { useCluster, useClusters } from "@/lib/hooks/useClusters";
import { useLinkStorage } from "@/lib/hooks/useStorage";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { X, Link2 } from "lucide-react";

interface LinkStorageDialogProps {
  sourceClusterId: string;
  isOpen: boolean;
  onClose: () => void;
}

export function LinkStorageDialog({
  sourceClusterId,
  isOpen,
  onClose,
}: LinkStorageDialogProps) {
  const [selectedClusterId, setSelectedClusterId] = useState<string>("");

  const { data: sourceCluster } = useCluster(sourceClusterId);
  const { data: clustersResponse } = useClusters();
  const linkMutation = useLinkStorage();

  // Reset selected cluster when dialog opens
  useEffect(() => {
    if (isOpen) {
      setSelectedClusterId("");
    }
  }, [isOpen]);

  if (!isOpen) return null;

  const handleSubmit = () => {
    if (!selectedClusterId) return;

    linkMutation.mutate(
      { clusterId: sourceClusterId, targetClusterId: selectedClusterId },
      {
        onSuccess: () => {
          onClose();
        },
        onError: (error) => {
          alert(`Failed to link storage: ${error.message}`);
        },
      }
    );
  };

  // Filter clusters: same region, READY status, not source cluster
  const eligibleClusters = clustersResponse?.data.filter((cluster) => {
    if (!sourceCluster) return false;
    return (
      cluster.id !== sourceClusterId &&
      cluster.region === sourceCluster.region &&
      cluster.status === "READY"
    );
  });

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      {/* Backdrop */}
      <div
        className="absolute inset-0 bg-black/50"
        onClick={onClose}
      />

      {/* Dialog Card */}
      <Card className="relative z-10 w-full max-w-md mx-4">
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle className="flex items-center gap-2">
              <Link2 className="h-5 w-5" />
              Link Storage to Cluster
            </CardTitle>
            <Button
              variant="ghost"
              size="sm"
              onClick={onClose}
              className="h-8 w-8 p-0"
            >
              <X className="h-4 w-4" />
            </Button>
          </div>
          <p className="text-sm text-muted-foreground mt-2">
            Create shared EFS and S3 storage between two clusters for migration testing.
            Both clusters must be in the same region.
          </p>
        </CardHeader>
        <CardContent className="space-y-4">
          {/* Source Cluster */}
          <div className="space-y-2">
            <Label>Source Cluster</Label>
            <div className="p-3 bg-muted rounded-md">
              <p className="font-medium">{sourceCluster?.name}</p>
              <p className="text-sm text-muted-foreground">
                Region: {sourceCluster?.region}
              </p>
            </div>
          </div>

          {/* Target Cluster Selector */}
          <div className="space-y-2">
            <Label htmlFor="target-cluster">Target Cluster</Label>
            {!eligibleClusters || eligibleClusters.length === 0 ? (
              <div className="p-3 bg-yellow-50 dark:bg-yellow-950 rounded-md text-sm">
                No eligible clusters found. Target cluster must be in region{" "}
                <strong>{sourceCluster?.region}</strong> and have READY status.
              </div>
            ) : (
              <Select
                value={selectedClusterId}
                onValueChange={setSelectedClusterId}
              >
                <SelectTrigger id="target-cluster">
                  <SelectValue placeholder="Select a cluster..." />
                </SelectTrigger>
                <SelectContent>
                  {eligibleClusters.map((cluster) => (
                    <SelectItem key={cluster.id} value={cluster.id}>
                      {cluster.name} ({cluster.region})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </div>

          {/* Action Buttons */}
          <div className="flex gap-2 justify-end pt-4">
            <Button variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button
              onClick={handleSubmit}
              disabled={!selectedClusterId || linkMutation.isPending}
              className="gap-2"
            >
              {linkMutation.isPending ? (
                <>Linking...</>
              ) : (
                <>
                  <Link2 className="h-4 w-4" />
                  Link Storage
                </>
              )}
            </Button>
          </div>

          {/* Info Message */}
          {selectedClusterId && (
            <div className="text-xs text-muted-foreground bg-blue-50 dark:bg-blue-950 p-3 rounded-md">
              This will create a new storage group with EFS and S3 resources.
              Both clusters will have access to the shared storage. Provisioning
              typically takes 5-10 minutes.
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
