"use client";

import { useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { poolsApi } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { formatDate } from "@/lib/utils/formatters";
import { Plus, Database, Trash2, Power, PowerOff, Clock, RefreshCw } from "lucide-react";
import type { PoolWithStats } from "@/types/api";

export default function PoolsPage() {
  const router = useRouter();
  const queryClient = useQueryClient();
  const [deletingPool, setDeletingPool] = useState<string | null>(null);

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ["pools"],
    queryFn: () => poolsApi.listPools(),
    refetchInterval: 30000, // Refresh every 30 seconds
  });

  const deleteMutation = useMutation({
    mutationFn: (poolName: string) => poolsApi.deletePool(poolName),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["pools"] });
      setDeletingPool(null);
    },
    onError: (error: any) => {
      alert(`Failed to delete pool: ${error.message || 'Unknown error'}`);
      setDeletingPool(null);
    },
  });

  const handleDelete = async (pool: PoolWithStats) => {
    if (pool.stats.total_clusters > 0) {
      alert(
        `Cannot delete pool "${pool.display_name}" because it has ${pool.stats.total_clusters} cluster(s). Please remove all clusters first.`
      );
      return;
    }

    if (!confirm(`Are you sure you want to delete pool "${pool.display_name}"? This action cannot be undone.`)) {
      return;
    }

    setDeletingPool(pool.name);
    deleteMutation.mutate(pool.name);
  };

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg">Loading cluster pools...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg text-red-600">
          Error loading pools: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      </div>
    );
  }

  const pools = data?.pools || [];

  // Calculate totals (with defensive checks)
  const totalClusters = pools.reduce((sum, p) => sum + (p.stats?.total_clusters || 0), 0);
  const totalReady = pools.reduce((sum, p) => sum + (p.stats?.ready_clusters || 0), 0);
  const totalLeased = pools.reduce((sum, p) => sum + (p.stats?.leased_clusters || 0), 0);

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold flex items-center gap-3">
            Cluster Pools
            <Badge variant="secondary" className="text-base font-normal">
              {pools.length}
            </Badge>
          </h1>
          <p className="text-muted-foreground">
            Manage pre-provisioned cluster pools for fast CI/CD integration
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => refetch()}>
            <RefreshCw className="mr-2 h-4 w-4" />
            Refresh
          </Button>
          <Link href="/admin/pools/new">
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              Create Pool
            </Button>
          </Link>
        </div>
      </div>

      {/* Statistics Cards */}
      <div className="grid gap-4 md:grid-cols-3">
        <div className="rounded-lg border bg-card p-6">
          <div className="flex items-center gap-3">
            <div className="rounded-lg bg-blue-500/10 p-3">
              <Database className="h-6 w-6 text-blue-600" />
            </div>
            <div>
              <p className="text-sm text-muted-foreground">Total Clusters</p>
              <p className="text-2xl font-bold">{totalClusters}</p>
            </div>
          </div>
        </div>

        <div className="rounded-lg border bg-card p-6">
          <div className="flex items-center gap-3">
            <div className="rounded-lg bg-green-500/10 p-3">
              <Power className="h-6 w-6 text-green-600" />
            </div>
            <div>
              <p className="text-sm text-muted-foreground">Ready</p>
              <p className="text-2xl font-bold">{totalReady}</p>
            </div>
          </div>
        </div>

        <div className="rounded-lg border bg-card p-6">
          <div className="flex items-center gap-3">
            <div className="rounded-lg bg-orange-500/10 p-3">
              <Clock className="h-6 w-6 text-orange-600" />
            </div>
            <div>
              <p className="text-sm text-muted-foreground">Leased</p>
              <p className="text-2xl font-bold">{totalLeased}</p>
            </div>
          </div>
        </div>
      </div>

      {/* Pools Table */}
      <div className="rounded-lg border bg-card">
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead>
              <tr className="border-b bg-muted/50">
                <th className="p-4 text-left text-sm font-medium">Pool Name</th>
                <th className="p-4 text-left text-sm font-medium">Profile</th>
                <th className="p-4 text-left text-sm font-medium">Status</th>
                <th className="p-4 text-left text-sm font-medium">Clusters</th>
                <th className="p-4 text-left text-sm font-medium">Ready/Target</th>
                <th className="p-4 text-left text-sm font-medium">Auto Refresh</th>
                <th className="p-4 text-left text-sm font-medium">Created</th>
                <th className="p-4 text-right text-sm font-medium">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {pools.length === 0 ? (
                <tr>
                  <td colSpan={8} className="p-8 text-center text-muted-foreground">
                    No cluster pools found. Create your first pool to get started.
                  </td>
                </tr>
              ) : (
                pools.map((pool) => (
                  <tr key={pool.id} className="hover:bg-muted/30">
                    <td className="p-4">
                      <Link
                        href={`/admin/pools/${encodeURIComponent(pool.name)}`}
                        className="font-medium hover:underline"
                      >
                        {pool.display_name}
                      </Link>
                      <div className="text-xs text-muted-foreground">{pool.name}</div>
                      {pool.description && (
                        <div className="text-xs text-muted-foreground mt-1">{pool.description}</div>
                      )}
                    </td>
                    <td className="p-4">
                      <Badge variant="outline">{pool.profile}</Badge>
                    </td>
                    <td className="p-4">
                      {pool.enabled ? (
                        <Badge variant="default" className="gap-1">
                          <Power className="h-3 w-3" />
                          Enabled
                        </Badge>
                      ) : (
                        <Badge variant="secondary" className="gap-1">
                          <PowerOff className="h-3 w-3" />
                          Disabled
                        </Badge>
                      )}
                    </td>
                    <td className="p-4">
                      <div className="flex flex-col gap-1 text-sm">
                        <div>
                          <span className="text-green-600 font-medium">{pool.stats?.ready_clusters || 0}</span> ready
                        </div>
                        <div>
                          <span className="text-orange-600 font-medium">{pool.stats?.leased_clusters || 0}</span> leased
                        </div>
                        {(pool.stats?.provisioning_clusters || 0) > 0 && (
                          <div>
                            <span className="text-blue-600 font-medium">{pool.stats.provisioning_clusters}</span> provisioning
                          </div>
                        )}
                        {(pool.stats?.expired_clusters || 0) > 0 && (
                          <div>
                            <span className="text-red-600 font-medium">{pool.stats.expired_clusters}</span> expired
                          </div>
                        )}
                      </div>
                    </td>
                    <td className="p-4">
                      <div className="text-sm">
                        <div className="font-medium">
                          {pool.stats?.ready_clusters || 0} / {pool.target_size}
                        </div>
                        <div className="text-xs text-muted-foreground">
                          Min: {pool.min_size} | Max: {pool.max_size}
                        </div>
                      </div>
                    </td>
                    <td className="p-4">
                      {pool.auto_refresh_enabled ? (
                        <Badge variant="outline" className="gap-1">
                          <RefreshCw className="h-3 w-3" />
                          {pool.max_cluster_age_days}d
                        </Badge>
                      ) : (
                        <Badge variant="secondary">Disabled</Badge>
                      )}
                    </td>
                    <td className="p-4 text-sm text-muted-foreground">
                      {formatDate(pool.created_at)}
                      {pool.created_by && (
                        <div className="text-xs">by {pool.created_by}</div>
                      )}
                    </td>
                    <td className="p-4 text-right">
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleDelete(pool)}
                        disabled={deletingPool === pool.name}
                      >
                        <Trash2 className="h-4 w-4 text-red-600" />
                      </Button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}
