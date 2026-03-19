"use client";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Server, Database, Cpu, RefreshCw, Clock } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useInfrastructure } from "@/lib/hooks/useAdminStats";
import type { WorkerInfo, ASGInfo, InfrastructureInfo } from "@/lib/api/endpoints/admin";

export default function InfrastructurePage() {
  const { data, isLoading, error, refetch } = useInfrastructure();
  const lastUpdate = data ? new Date(data.timestamp) : new Date();

  const getStatusBadge = (status: string) => {
    const statusLower = status.toLowerCase();
    if (statusLower === "healthy" || statusLower === "running" || statusLower === "inservice") {
      return <Badge className="bg-green-500">{status}</Badge>;
    }
    if (statusLower === "degraded" || statusLower === "pending") {
      return <Badge className="bg-yellow-500">{status}</Badge>;
    }
    if (statusLower === "unhealthy" || statusLower === "failed") {
      return <Badge className="bg-red-500">{status}</Badge>;
    }
    return <Badge className="bg-gray-500">{status}</Badge>;
  };

  const formatTimestamp = (timestamp: string) => {
    if (!timestamp) return "-";

    const date = new Date(timestamp);
    const now = new Date();

    // Handle invalid dates or very old dates (before 1990 = likely sentinel/zero value)
    if (isNaN(date.getTime()) || date.getFullYear() < 1990) {
      return "-";
    }

    const diffMs = now.getTime() - date.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    const diffHours = Math.floor(diffMs / 3600000);
    const diffDays = Math.floor(diffMs / 86400000);

    if (diffMins < 1) return "just now";
    if (diffMins < 60) return `${diffMins}m ago`;
    if (diffHours < 24) return `${diffHours}h ago`;
    if (diffDays < 365) return `${diffDays}d ago`;

    // For very old dates, show the actual date
    return date.toLocaleDateString();
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-96">
        <RefreshCw className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    );
  }

  if (error) {
    return (
      <div className="space-y-6">
        <h1 className="text-3xl font-bold">Infrastructure</h1>
        <Card>
          <CardContent className="pt-6">
            <div className="text-red-500">Error: {error instanceof Error ? error.message : 'Failed to fetch infrastructure data'}</div>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (!data) {
    return null;
  }

  const allWorkers = [
    ...data.static_workers,
    ...(data.autoscale_group?.instances || []),
  ];

  return (
    <div className="space-y-6">
      <div className="flex justify-between items-center">
        <div>
          <h1 className="text-3xl font-bold">Infrastructure</h1>
          <p className="text-muted-foreground">
            System components and worker status
          </p>
        </div>
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2 text-sm text-muted-foreground">
            <Clock className="h-4 w-4" />
            Updated {formatTimestamp(lastUpdate.toISOString())}
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={() => refetch()}
          >
            <RefreshCw className="h-4 w-4 mr-2" />
            Refresh Now
          </Button>
        </div>
      </div>

      {/* System Components */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {/* API Server */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">API Server</CardTitle>
            <Server className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex justify-between items-center">
              <span className="text-sm text-muted-foreground">Status</span>
              {getStatusBadge(data.api_server.status)}
            </div>
            <div className="flex justify-between items-center">
              <span className="text-sm text-muted-foreground">IP Address</span>
              <span className="text-sm font-mono">{data.api_server.ip}</span>
            </div>
            <div className="flex justify-between items-center">
              <span className="text-sm text-muted-foreground">Version</span>
              <span className="text-sm font-mono">{data.api_server.version}</span>
            </div>
          </CardContent>
        </Card>

        {/* Database */}
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">Database</CardTitle>
            <Database className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="flex justify-between items-center">
              <span className="text-sm text-muted-foreground">Status</span>
              {getStatusBadge(data.database.status)}
            </div>
            <div className="flex justify-between items-center">
              <span className="text-sm text-muted-foreground">Host</span>
              <span className="text-sm font-mono">{data.database.host}</span>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Workers */}
      <Card>
        <CardHeader>
          <div className="flex justify-between items-center">
            <CardTitle>Workers</CardTitle>
            <div className="flex items-center gap-2">
              <Cpu className="h-4 w-4 text-muted-foreground" />
              <span className="text-sm text-muted-foreground">
                {allWorkers.length} total
                {data.autoscale_group && (
                  <span className="ml-2">
                    (ASG: {data.autoscale_group.desired_capacity}/{data.autoscale_group.max_size})
                  </span>
                )}
              </span>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <div className="relative overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b">
                  <th className="text-left p-3 font-medium">Instance ID</th>
                  <th className="text-left p-3 font-medium">Type</th>
                  <th className="text-left p-3 font-medium">IP Address</th>
                  <th className="text-left p-3 font-medium">Version</th>
                  <th className="text-left p-3 font-medium">State</th>
                  <th className="text-left p-3 font-medium">Health</th>
                  <th className="text-left p-3 font-medium">Launch Time</th>
                </tr>
              </thead>
              <tbody>
                {allWorkers.length === 0 ? (
                  <tr>
                    <td colSpan={7} className="p-6 text-center text-muted-foreground">
                      No workers found
                    </td>
                  </tr>
                ) : (
                  allWorkers.map((worker) => (
                    <tr key={worker.instance_id} className="border-b">
                      <td className="p-3 font-mono text-xs">{worker.instance_id}</td>
                      <td className="p-3">
                        <Badge variant={worker.type === "static" ? "default" : "secondary"}>
                          {worker.type}
                        </Badge>
                      </td>
                      <td className="p-3 font-mono text-xs">{worker.private_ip}</td>
                      <td className="p-3 font-mono text-xs">
                        {worker.version || <span className="text-muted-foreground">unknown</span>}
                      </td>
                      <td className="p-3">
                        {getStatusBadge(worker.state)}
                      </td>
                      <td className="p-3">
                        {getStatusBadge(worker.health_status)}
                      </td>
                      <td className="p-3 text-xs text-muted-foreground">
                        {worker.launch_time ? formatTimestamp(worker.launch_time) : "-"}
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>

      {/* Autoscale Group Details */}
      {data.autoscale_group && (
        <Card>
          <CardHeader>
            <CardTitle>Autoscale Group Details</CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
              <div>
                <div className="text-sm text-muted-foreground">Group Name</div>
                <div className="text-sm font-mono mt-1">{data.autoscale_group.name}</div>
              </div>
              <div>
                <div className="text-sm text-muted-foreground">Desired</div>
                <div className="text-lg font-bold mt-1">{data.autoscale_group.desired_capacity}</div>
              </div>
              <div>
                <div className="text-sm text-muted-foreground">Min Size</div>
                <div className="text-lg font-bold mt-1">{data.autoscale_group.min_size}</div>
              </div>
              <div>
                <div className="text-sm text-muted-foreground">Max Size</div>
                <div className="text-lg font-bold mt-1">{data.autoscale_group.max_size}</div>
              </div>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
