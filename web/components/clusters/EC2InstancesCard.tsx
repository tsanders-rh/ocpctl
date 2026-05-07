"use client";

import React from "react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { useClusterInstances } from "@/lib/hooks/useClusters";
import { type EC2Instance } from "@/types/api";
import { formatDistanceToNow } from "date-fns";

interface EC2InstancesCardProps {
  clusterId: string;
  platform?: string;
  clusterType?: string;
}

export function EC2InstancesCard({ clusterId, platform, clusterType }: EC2InstancesCardProps) {
  const { data: instances, isLoading, error } = useClusterInstances(clusterId, platform);

  // Only show for AWS and GCP clusters
  if (platform !== "aws" && platform !== "gcp") {
    return null;
  }

  // Determine title based on platform
  const title = platform === "aws" ? "EC2 Instances" : platform === "gcp" ? "GCP Instances" : "Instances";

  // For ROSA clusters, explain that worker nodes are managed by AWS/Red Hat
  const isROSA = clusterType === "rosa";
  const emptyMessage = isROSA
    ? "ROSA is a managed service - worker nodes are managed by AWS and Red Hat, and control plane nodes are not visible in your account."
    : platform === "aws"
    ? "No EC2 instances found for this cluster"
    : platform === "gcp"
    ? "No GCP instances found for this cluster"
    : "No instances found for this cluster";

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
          <CardDescription>Loading instance information...</CardDescription>
        </CardHeader>
      </Card>
    );
  }

  if (error) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
          <CardDescription className="text-destructive">
            Failed to load instances
          </CardDescription>
        </CardHeader>
      </Card>
    );
  }

  if (!instances || instances.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
          <CardDescription>{emptyMessage}</CardDescription>
        </CardHeader>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <CardDescription>
          {instances.length} instance{instances.length !== 1 ? 's' : ''} running
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="space-y-4">
          {instances.map((instance: EC2Instance) => (
            <div
              key={instance.instance_id}
              className="flex items-center justify-between border-b pb-4 last:border-b-0 last:pb-0"
            >
              <div className="flex flex-col space-y-1">
                <div className="flex items-center space-x-2">
                  <span className="font-mono text-sm font-medium">{instance.instance_id}</span>
                  <InstanceStateBadge state={instance.state} platform={platform} />
                </div>
                {instance.name && (
                  <span className="text-sm text-muted-foreground">{instance.name}</span>
                )}
                <div className="flex items-center space-x-4 text-xs text-muted-foreground">
                  <span className="font-mono">{instance.instance_type}</span>
                  {instance.private_ip_address && (
                    <span className="font-mono">Private: {instance.private_ip_address}</span>
                  )}
                  {instance.public_ip_address && (
                    <span className="font-mono">Public: {instance.public_ip_address}</span>
                  )}
                </div>
                {instance.launch_time && (
                  <span className="text-xs text-muted-foreground">
                    Launched {formatDistanceToNow(new Date(instance.launch_time), { addSuffix: true })}
                  </span>
                )}
              </div>
            </div>
          ))}
        </div>
      </CardContent>
    </Card>
  );
}

function InstanceStateBadge({ state, platform }: { state: string; platform?: string }) {
  // GCP uses different state names than AWS
  const gcpStateColors: Record<string, string> = {
    running: "bg-green-500",
    terminated: "bg-red-500",
    stopping: "bg-yellow-500",
    provisioning: "bg-blue-500",
    staging: "bg-blue-500",
    suspending: "bg-yellow-500",
    suspended: "bg-orange-500",
    repairing: "bg-purple-500",
  };

  const awsStateColors: Record<string, string> = {
    running: "bg-green-500",
    stopped: "bg-red-500",
    stopping: "bg-yellow-500",
    pending: "bg-blue-500",
    terminating: "bg-orange-500",
    terminated: "bg-gray-500",
  };

  const stateColors = platform === "gcp" ? gcpStateColors : awsStateColors;
  const color = stateColors[state.toLowerCase()] || "bg-gray-500";

  return (
    <Badge variant="outline" className={`${color} text-white border-0`}>
      {state.toUpperCase()}
    </Badge>
  );
}
