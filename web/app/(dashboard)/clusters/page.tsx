"use client";

import Link from "next/link";
import { useState } from "react";
import { useClusters } from "@/lib/hooks/useClusters";
import { Button } from "@/components/ui/button";
import { ClusterStatusBadge } from "@/components/clusters/ClusterStatusBadge";
import { formatDate, formatTTL } from "@/lib/utils/formatters";
import { Plus } from "lucide-react";

export default function ClustersPage() {
  const [page, setPage] = useState(1);
  const { data, isLoading, error } = useClusters({ page, per_page: 20 });

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg">Loading clusters...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg text-red-600">
          Error loading clusters: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      </div>
    );
  }

  if (!data) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg">No data available</div>
      </div>
    );
  }

  const clusters = data.data || [];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Clusters</h1>
          <p className="text-muted-foreground">
            Manage your OpenShift clusters
          </p>
        </div>
        <Link href="/clusters/new">
          <Button>
            <Plus className="mr-2 h-4 w-4" />
            Create Cluster
          </Button>
        </Link>
      </div>

      <div className="rounded-lg border bg-card">
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead>
              <tr className="border-b bg-muted/50">
                <th className="p-4 text-left text-sm font-medium">Name</th>
                <th className="p-4 text-left text-sm font-medium">Status</th>
                <th className="p-4 text-left text-sm font-medium">Platform</th>
                <th className="p-4 text-left text-sm font-medium">Region</th>
                <th className="p-4 text-left text-sm font-medium">Profile</th>
                <th className="p-4 text-left text-sm font-medium">TTL</th>
                <th className="p-4 text-left text-sm font-medium">Created</th>
                <th className="p-4 text-left text-sm font-medium">Actions</th>
              </tr>
            </thead>
            <tbody>
              {clusters.length === 0 ? (
                <tr>
                  <td colSpan={8} className="p-8 text-center text-muted-foreground">
                    No clusters found. Create your first cluster to get started.
                  </td>
                </tr>
              ) : (
                clusters.map((cluster) => (
                  <tr
                    key={cluster.id}
                    className="border-b last:border-0 hover:bg-muted/50"
                  >
                    <td className="p-4">
                      <Link
                        href={`/clusters/${cluster.id}`}
                        className="font-medium hover:underline"
                      >
                        {cluster.name}
                      </Link>
                    </td>
                    <td className="p-4">
                      <ClusterStatusBadge status={cluster.status} />
                    </td>
                    <td className="p-4 uppercase">{cluster.platform}</td>
                    <td className="p-4">{cluster.region}</td>
                    <td className="p-4 text-sm text-muted-foreground">
                      {cluster.profile}
                    </td>
                    <td className="p-4 text-sm">
                      {formatTTL(cluster.destroy_at)}
                    </td>
                    <td className="p-4 text-sm text-muted-foreground">
                      {formatDate(cluster.created_at)}
                    </td>
                    <td className="p-4">
                      <Link href={`/clusters/${cluster.id}`}>
                        <Button variant="outline" size="sm">
                          View
                        </Button>
                      </Link>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      {data.pagination && data.pagination.total_pages > 1 && (
        <div className="flex items-center justify-between">
          <div className="text-sm text-muted-foreground">
            Page {data.pagination.page} of {data.pagination.total_pages} (
            {data.pagination.total} total)
          </div>
          <div className="flex gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              disabled={page === 1}
            >
              Previous
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => setPage((p) => p + 1)}
              disabled={page === data.pagination.total_pages}
            >
              Next
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
