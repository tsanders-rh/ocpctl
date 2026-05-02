"use client";

import { useState } from "react";
import Link from "next/link";
import { useLongRunningClusters } from "@/lib/hooks/useAdminStats";
import { useHibernateCluster } from "@/lib/hooks/useClusters";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Clock,
  DollarSign,
  RefreshCw,
  Server,
  Pause,
  AlertCircle,
} from "lucide-react";
import { formatDistanceToNow } from "date-fns";
import type { LongRunningCluster } from "@/lib/api/endpoints/admin";

export default function LongRunningClustersPage() {
  const [minHours, setMinHours] = useState(24);
  const { data, isLoading, error, refetch } = useLongRunningClusters(minHours);
  const hibernateMutation = useHibernateCluster();

  const formatCurrency = (value: number) => `$${value.toFixed(2)}`;

  const formatDuration = (hours: number) => {
    if (hours < 24) return `${hours.toFixed(1)}h`;
    const days = Math.floor(hours / 24);
    const remainingHours = Math.floor(hours % 24);
    return `${days}d ${remainingHours}h`;
  };

  const handleHibernate = async (clusterId: string, clusterName: string) => {
    if (
      !confirm(
        `Are you sure you want to hibernate cluster "${clusterName}"? This will stop all cluster instances.`
      )
    ) {
      return;
    }

    try {
      await hibernateMutation.mutateAsync(clusterId);
      // Refetch the long-running clusters list after hibernation
      refetch();
    } catch (error) {
      console.error("Failed to hibernate cluster:", error);
      // Error handling is done by the mutation hook
    }
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
        <h1 className="text-3xl font-bold">Long-Running Clusters</h1>
        <Card>
          <CardContent className="pt-6">
            <div className="flex items-center gap-2 text-red-500">
              <AlertCircle className="h-5 w-5" />
              <span>
                Error:{" "}
                {error instanceof Error
                  ? error.message
                  : "Failed to fetch long-running clusters"}
              </span>
            </div>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (!data) {
    return null;
  }

  const minHoursOptions = [
    { value: 24, label: "24 hours" },
    { value: 48, label: "48 hours" },
    { value: 72, label: "3 days" },
    { value: 168, label: "1 week" },
  ];

  const avgRunningHours =
    data.clusters.length > 0
      ? data.clusters.reduce(
          (sum, c) => sum + c.running_duration_hours,
          0
        ) / data.clusters.length
      : 0;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h1 className="text-3xl font-bold">Long-Running Clusters</h1>
        <Button
          variant="outline"
          size="sm"
          onClick={() => refetch()}
          disabled={isLoading}
        >
          <RefreshCw className={`h-4 w-4 mr-2 ${isLoading ? "animate-spin" : ""}`} />
          Refresh
        </Button>
      </div>

      {/* Filter Controls */}
      <Card>
        <CardContent className="pt-6">
          <div className="flex items-center gap-4">
            <span className="text-sm font-medium">Minimum running time:</span>
            {minHoursOptions.map((option) => (
              <Button
                key={option.value}
                variant={minHours === option.value ? "default" : "outline"}
                size="sm"
                onClick={() => setMinHours(option.value)}
              >
                {option.label}
              </Button>
            ))}
          </div>
        </CardContent>
      </Card>

      {/* Summary Cards */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">
              Long-Running Clusters
            </CardTitle>
            <Server className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{data.total_count}</div>
            <p className="text-xs text-muted-foreground">
              Running {minHours}+ hours
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">
              Total Hourly Cost
            </CardTitle>
            <DollarSign className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {formatCurrency(data.total_hourly_cost)}
            </div>
            <p className="text-xs text-muted-foreground">
              {formatCurrency(data.total_daily_cost)}/day
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">
              Monthly Cost
            </CardTitle>
            <DollarSign className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {formatCurrency(data.total_monthly_cost)}
            </div>
            <p className="text-xs text-muted-foreground">
              Projected monthly expense
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">
              Avg. Running Time
            </CardTitle>
            <Clock className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">
              {formatDuration(avgRunningHours)}
            </div>
            <p className="text-xs text-muted-foreground">
              Average duration
            </p>
          </CardContent>
        </Card>
      </div>

      {/* Clusters Table */}
      <Card>
        <CardHeader>
          <CardTitle>Clusters</CardTitle>
        </CardHeader>
        <CardContent>
          {data.clusters.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              <Server className="h-12 w-12 mx-auto mb-4 opacity-50" />
              <p className="text-lg font-medium">No long-running clusters found</p>
              <p className="text-sm">
                All clusters have been hibernated within the last {minHours} hours
              </p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full">
                <thead>
                  <tr className="border-b text-left text-sm font-medium text-muted-foreground">
                    <th className="pb-3 pr-4">Cluster</th>
                    <th className="pb-3 pr-4">Owner</th>
                    <th className="pb-3 pr-4">Platform/Profile</th>
                    <th className="pb-3 pr-4">Region</th>
                    <th className="pb-3 pr-4">Running Time</th>
                    <th className="pb-3 pr-4">Hourly Cost</th>
                    <th className="pb-3 pr-4">Daily Cost</th>
                    <th className="pb-3 pr-4">Work Hours</th>
                    <th className="pb-3">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {data.clusters.map((cluster) => (
                    <tr
                      key={cluster.id}
                      className="border-b hover:bg-muted/50 transition-colors"
                    >
                      <td className="py-3 pr-4">
                        <Link
                          href={`/clusters/${cluster.id}`}
                          className="font-medium hover:underline text-blue-600"
                        >
                          {cluster.name}
                        </Link>
                      </td>
                      <td className="py-3 pr-4 text-sm text-muted-foreground">
                        {cluster.owner}
                      </td>
                      <td className="py-3 pr-4">
                        <div className="flex flex-col gap-1">
                          <Badge variant="outline" className="w-fit">
                            {cluster.platform}
                          </Badge>
                          <span className="text-xs text-muted-foreground">
                            {cluster.profile}
                          </span>
                        </div>
                      </td>
                      <td className="py-3 pr-4 text-sm">
                        {cluster.region}
                      </td>
                      <td className="py-3 pr-4">
                        <div className="flex flex-col">
                          <span className="font-medium">
                            {formatDuration(cluster.running_duration_hours)}
                          </span>
                          <span className="text-xs text-muted-foreground">
                            Created{" "}
                            {formatDistanceToNow(new Date(cluster.created_at), {
                              addSuffix: true,
                            })}
                          </span>
                        </div>
                      </td>
                      <td className="py-3 pr-4 font-medium">
                        {formatCurrency(cluster.hourly_cost)}
                      </td>
                      <td className="py-3 pr-4 font-medium">
                        {formatCurrency(cluster.daily_cost)}
                      </td>
                      <td className="py-3 pr-4">
                        <Badge
                          variant={
                            cluster.work_hours_enabled ? "default" : "secondary"
                          }
                        >
                          {cluster.work_hours_enabled ? "Enabled" : "Disabled"}
                        </Badge>
                      </td>
                      <td className="py-3">
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() =>
                            handleHibernate(cluster.id, cluster.name)
                          }
                          disabled={hibernateMutation.isPending}
                        >
                          <Pause className="h-4 w-4 mr-1" />
                          Hibernate
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
