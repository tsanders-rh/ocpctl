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
}

export function EC2InstancesCard({ clusterId, platform }: EC2InstancesCardProps) {
  const { data: instances, isLoading, error } = useClusterInstances(clusterId, platform);

  if (platform !== "aws") {
    return null; // Only show for AWS clusters
  }

  if (isLoading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>EC2 Instances</CardTitle>
          <CardDescription>Loading instance information...</CardDescription>
        </CardHeader>
      </Card>
    );
  }

  if (error) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>EC2 Instances</CardTitle>
          <CardDescription className="text-destructive">
            Failed to load EC2 instances
          </CardDescription>
        </CardHeader>
      </Card>
    );
  }

  if (!instances || instances.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>EC2 Instances</CardTitle>
          <CardDescription>No EC2 instances found for this cluster</CardDescription>
        </CardHeader>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>EC2 Instances</CardTitle>
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
                  <InstanceStateBadge state={instance.state} />
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

function InstanceStateBadge({ state }: { state: string }) {
  const stateColors: Record<string, string> = {
    running: "bg-green-500",
    stopped: "bg-red-500",
    stopping: "bg-yellow-500",
    pending: "bg-blue-500",
    terminating: "bg-orange-500",
    terminated: "bg-gray-500",
  };

  const color = stateColors[state.toLowerCase()] || "bg-gray-500";

  return (
    <Badge variant="outline" className={`${color} text-white border-0`}>
      {state.toUpperCase()}
    </Badge>
  );
}
