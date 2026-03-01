"use client";

import Link from "next/link";
import { useState } from "react";
import { useClusters } from "@/lib/hooks/useClusters";
import { useUsers } from "@/lib/hooks/useUsers";
import { useAuthStore } from "@/lib/stores/authStore";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Card, CardContent } from "@/components/ui/card";
import { ClusterStatusBadge } from "@/components/clusters/ClusterStatusBadge";
import { formatDate, formatTTL } from "@/lib/utils/formatters";
import { Plus, Filter, X } from "lucide-react";
import { Platform, ClusterStatus, UserRole } from "@/types/api";

export default function ClustersPage() {
  const { user } = useAuthStore();
  const isAdmin = user?.role === UserRole.ADMIN;

  const [page, setPage] = useState(1);
  const [showFilters, setShowFilters] = useState(false);
  const [filters, setFilters] = useState<{
    platform?: Platform;
    status?: ClusterStatus;
    owner?: string;
    team?: string;
    profile?: string;
    version?: string;
  }>({});

  const { data, isLoading, error } = useClusters({
    page,
    per_page: 20,
    ...filters,
  });

  const { data: usersData } = useUsers();

  const clearFilters = () => {
    setFilters({});
    setPage(1);
  };

  const hasActiveFilters = Object.keys(filters).some(
    (key) => filters[key as keyof typeof filters]
  );

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
          Error loading clusters:{" "}
          {error instanceof Error ? error.message : "Unknown error"}
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
          <h1 className="text-3xl font-bold">
            {isAdmin ? "All Clusters" : "My Clusters"}
          </h1>
          <p className="text-muted-foreground">
            {isAdmin
              ? "View and manage all clusters across users"
              : "Manage your OpenShift clusters"}
          </p>
        </div>
        <Link href="/clusters/new">
          <Button>
            <Plus className="mr-2 h-4 w-4" />
            Create Cluster
          </Button>
        </Link>
      </div>

      {/* Filters */}
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <Button
            variant={showFilters ? "default" : "outline"}
            size="sm"
            onClick={() => setShowFilters(!showFilters)}
          >
            <Filter className="mr-2 h-4 w-4" />
            Filters
            {hasActiveFilters && (
              <span className="ml-2 rounded-full bg-primary-foreground text-primary px-2 py-0.5 text-xs">
                {Object.keys(filters).filter((k) => filters[k as keyof typeof filters]).length}
              </span>
            )}
          </Button>
          {hasActiveFilters && (
            <Button variant="ghost" size="sm" onClick={clearFilters}>
              <X className="mr-2 h-4 w-4" />
              Clear Filters
            </Button>
          )}
        </div>

        {showFilters && (
          <Card>
            <CardContent className="pt-6">
              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 gap-4">
                <div className="space-y-2">
                  <Label>Platform</Label>
                  <Select
                    value={filters.platform || ""}
                    onValueChange={(value) =>
                      setFilters({ ...filters, platform: value as Platform })
                    }
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="All platforms" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="">All platforms</SelectItem>
                      <SelectItem value={Platform.AWS}>AWS</SelectItem>
                      <SelectItem value={Platform.IBMCloud}>
                        IBM Cloud
                      </SelectItem>
                    </SelectContent>
                  </Select>
                </div>

                <div className="space-y-2">
                  <Label>Status</Label>
                  <Select
                    value={filters.status || ""}
                    onValueChange={(value) =>
                      setFilters({ ...filters, status: value as ClusterStatus })
                    }
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="All statuses" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="">All statuses</SelectItem>
                      <SelectItem value={ClusterStatus.PENDING}>
                        Pending
                      </SelectItem>
                      <SelectItem value={ClusterStatus.CREATING}>
                        Creating
                      </SelectItem>
                      <SelectItem value={ClusterStatus.READY}>Ready</SelectItem>
                      <SelectItem value={ClusterStatus.DESTROYING}>
                        Destroying
                      </SelectItem>
                      <SelectItem value={ClusterStatus.DESTROYED}>
                        Destroyed
                      </SelectItem>
                      <SelectItem value={ClusterStatus.FAILED}>
                        Failed
                      </SelectItem>
                    </SelectContent>
                  </Select>
                </div>

                {isAdmin && (
                  <div className="space-y-2">
                    <Label>Owner</Label>
                    <Select
                      value={filters.owner || ""}
                      onValueChange={(value) =>
                        setFilters({ ...filters, owner: value })
                      }
                    >
                      <SelectTrigger>
                        <SelectValue placeholder="All owners" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="">All owners</SelectItem>
                        {usersData?.users.map((u) => (
                          <SelectItem key={u.id} value={u.email}>
                            {u.email}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                )}

                <div className="space-y-2">
                  <Label>Team</Label>
                  <Input
                    placeholder="Enter team name"
                    value={filters.team || ""}
                    onChange={(e) =>
                      setFilters({ ...filters, team: e.target.value })
                    }
                  />
                </div>

                <div className="space-y-2">
                  <Label>Profile</Label>
                  <Input
                    placeholder="Enter profile name"
                    value={filters.profile || ""}
                    onChange={(e) =>
                      setFilters({ ...filters, profile: e.target.value })
                    }
                  />
                </div>

                <div className="space-y-2">
                  <Label>Version</Label>
                  <Input
                    placeholder="e.g., 4.14.0"
                    value={filters.version || ""}
                    onChange={(e) =>
                      setFilters({ ...filters, version: e.target.value })
                    }
                  />
                </div>
              </div>
            </CardContent>
          </Card>
        )}
      </div>

      <div className="rounded-lg border bg-card">
        <div className="overflow-x-auto">
          <table className="w-full">
            <thead>
              <tr className="border-b bg-muted/50">
                <th className="p-4 text-left text-sm font-medium">Name</th>
                <th className="p-4 text-left text-sm font-medium">Status</th>
                <th className="p-4 text-left text-sm font-medium">Platform</th>
                {isAdmin && (
                  <th className="p-4 text-left text-sm font-medium">Owner</th>
                )}
                <th className="p-4 text-left text-sm font-medium">Team</th>
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
                  <td
                    colSpan={isAdmin ? 10 : 9}
                    className="p-8 text-center text-muted-foreground"
                  >
                    {hasActiveFilters
                      ? "No clusters match the selected filters."
                      : "No clusters found. Create your first cluster to get started."}
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
                    {isAdmin && (
                      <td className="p-4 text-sm">{cluster.owner}</td>
                    )}
                    <td className="p-4 text-sm">{cluster.team}</td>
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
