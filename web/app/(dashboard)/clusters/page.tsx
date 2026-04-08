"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, useMemo } from "react";
import { useClusters } from "@/lib/hooks/useClusters";
import { useUsers } from "@/lib/hooks/useUsers";
import { useProfiles } from "@/lib/hooks/useProfiles";
import { useAuthStore } from "@/lib/stores/authStore";
import { Button } from "@/components/ui/button";
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
import { formatDate, formatTTL, getTTLWarningLevel } from "@/lib/utils/formatters";
import { Plus, Filter, X, Search, RefreshCw, Star, AlertCircle, Clock, Layers } from "lucide-react";
import { Platform, ClusterStatus, UserRole } from "@/types/api";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { EmptyState } from "@/components/ui/empty-state";

export default function ClustersPage() {
  const router = useRouter();
  const { user } = useAuthStore();
  const isAdmin = user?.role === UserRole.ADMIN;

  const [page, setPage] = useState(1);
  const [showFilters, setShowFilters] = useState(false);
  const [searchQuery, setSearchQuery] = useState("");
  const [favorites, setFavorites] = useState<Set<string>>(() => {
    if (typeof window !== 'undefined') {
      const stored = localStorage.getItem('favorite-clusters');
      return new Set(stored ? JSON.parse(stored) : []);
    }
    return new Set();
  });
  const [filters, setFilters] = useState<{
    platform?: Platform;
    status?: ClusterStatus;
    owner?: string;
    team?: string;
    profile?: string;
    version?: string;
  }>({});

  const { data, isLoading, error, refetch } = useClusters({
    page,
    per_page: 20,
    ...filters,
  });

  const { data: usersData } = useUsers();
  const { data: profilesData } = useProfiles();

  // Get all clusters (without pagination) for filter options
  const { data: allClustersData } = useClusters({
    per_page: 1000, // Get all clusters for filter dropdowns
  });

  // Extract unique teams and versions from all clusters
  const uniqueTeams = useMemo(() => {
    if (!allClustersData?.data) return [];
    const teams = new Set(
      allClustersData.data.map((c) => c.team).filter(Boolean)
    );
    return Array.from(teams).sort();
  }, [allClustersData]);

  const uniqueVersions = useMemo(() => {
    if (!allClustersData?.data) return [];
    const versions = new Set(
      allClustersData.data.map((c) => c.version).filter(Boolean)
    );
    return Array.from(versions).sort().reverse(); // Most recent first
  }, [allClustersData]);

  const clearFilters = () => {
    setFilters({});
    setSearchQuery("");
    setPage(1);
  };

  const toggleFavorite = (clusterId: string) => {
    const newFavorites = new Set(favorites);
    if (newFavorites.has(clusterId)) {
      newFavorites.delete(clusterId);
    } else {
      newFavorites.add(clusterId);
    }
    setFavorites(newFavorites);
    if (typeof window !== 'undefined') {
      localStorage.setItem('favorite-clusters', JSON.stringify(Array.from(newFavorites)));
    }
  };

  const hasActiveFilters = Object.keys(filters).some(
    (key) => filters[key as keyof typeof filters]
  ) || searchQuery.length > 0;

  const clusters = data?.data || [];

  // Filter clusters by search query - must be called before any early returns
  const filteredClusters = useMemo(() => {
    if (!searchQuery || clusters.length === 0) return clusters;
    const query = searchQuery.toLowerCase();
    return clusters.filter(c =>
      c.name.toLowerCase().includes(query) ||
      c.team.toLowerCase().includes(query) ||
      c.region.toLowerCase().includes(query) ||
      (c.owner && c.owner.toLowerCase().includes(query)) ||
      c.profile.toLowerCase().includes(query)
    );
  }, [clusters, searchQuery]);

  // Sort clusters: favorites first, then by created date - must be called before any early returns
  const sortedClusters = useMemo(() => {
    if (filteredClusters.length === 0) return [];
    return [...filteredClusters].sort((a, b) => {
      const aFav = favorites.has(a.id);
      const bFav = favorites.has(b.id);
      if (aFav && !bFav) return -1;
      if (!aFav && bFav) return 1;
      // If both are favorites or both are not, sort by created date (newest first)
      return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
    });
  }, [filteredClusters, favorites]);

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

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold flex items-center gap-3">
            {isAdmin ? "All Clusters" : "My Clusters"}
            {data.pagination && (
              <Badge variant="secondary" className="text-base font-normal">
                {data.pagination.total}
              </Badge>
            )}
          </h1>
          <p className="text-muted-foreground">
            {isAdmin
              ? "View and manage all clusters across users"
              : "Manage your OpenShift clusters"}
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => refetch()} disabled={isLoading}>
            <RefreshCw className={`mr-2 h-4 w-4 ${isLoading ? 'animate-spin' : ''}`} />
            Refresh
          </Button>
          <Link href="/clusters/new">
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              Create Cluster
            </Button>
          </Link>
        </div>
      </div>

      {/* Search and Filters */}
      <div className="space-y-4">
        {/* Search Bar */}
        <div className="relative max-w-md">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search clusters by name, team, region, owner, or profile..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="pl-10"
          />
          {searchQuery && (
            <Button
              variant="ghost"
              size="sm"
              className="absolute right-1 top-1/2 -translate-y-1/2 h-7 w-7 p-0"
              onClick={() => setSearchQuery("")}
            >
              <X className="h-4 w-4" />
            </Button>
          )}
        </div>

        {/* Filter Controls */}
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
                    value={filters.platform}
                    onValueChange={(value) =>
                      setFilters({ ...filters, platform: value as Platform })
                    }
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="All platforms" />
                    </SelectTrigger>
                    <SelectContent>
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
                    value={filters.status}
                    onValueChange={(value) =>
                      setFilters({ ...filters, status: value as ClusterStatus })
                    }
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="All statuses" />
                    </SelectTrigger>
                    <SelectContent>
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
                      value={filters.owner}
                      onValueChange={(value) =>
                        setFilters({ ...filters, owner: value })
                      }
                    >
                      <SelectTrigger>
                        <SelectValue placeholder="All owners" />
                      </SelectTrigger>
                      <SelectContent>
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
                  <Select
                    value={filters.team}
                    onValueChange={(value) =>
                      setFilters({ ...filters, team: value })
                    }
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="All teams" />
                    </SelectTrigger>
                    <SelectContent>
                      {uniqueTeams.map((team) => (
                        <SelectItem key={team} value={team}>
                          {team}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>

                <div className="space-y-2">
                  <Label>Profile</Label>
                  <Select
                    value={filters.profile}
                    onValueChange={(value) =>
                      setFilters({ ...filters, profile: value })
                    }
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="All profiles" />
                    </SelectTrigger>
                    <SelectContent>
                      {profilesData?.map((profile) => (
                        <SelectItem key={profile.name} value={profile.name}>
                          {profile.display_name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>

                <div className="space-y-2">
                  <Label>Version</Label>
                  <Select
                    value={filters.version}
                    onValueChange={(value) =>
                      setFilters({ ...filters, version: value })
                    }
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="All versions" />
                    </SelectTrigger>
                    <SelectContent>
                      {uniqueVersions.map((version) => (
                        <SelectItem key={version} value={version}>
                          {version}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
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
                <th className="p-4 text-left text-sm font-medium w-12"></th>
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
              {sortedClusters.length === 0 ? (
                <tr>
                  <td colSpan={isAdmin ? 11 : 10} className="p-0">
                    <EmptyState
                      icon={Layers}
                      title={hasActiveFilters ? "No clusters found" : "No clusters yet"}
                      description={
                        hasActiveFilters
                          ? "No clusters match your current filters or search query. Try adjusting your filters or clearing your search."
                          : "Get started by creating your first Kubernetes cluster. Choose from OpenShift, EKS, or IKS platforms."
                      }
                      action={
                        !hasActiveFilters
                          ? {
                              label: "Create your first cluster",
                              onClick: () => router.push("/clusters/new")
                            }
                          : undefined
                      }
                    />
                  </td>
                </tr>
              ) : (
                sortedClusters.map((cluster) => (
                  <tr
                    key={cluster.id}
                    className="border-b last:border-0 hover:bg-muted/50"
                  >
                    <td className="p-4">
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-8 w-8 p-0"
                        onClick={(e) => {
                          e.stopPropagation();
                          toggleFavorite(cluster.id);
                        }}
                      >
                        <Star
                          className={`h-4 w-4 ${favorites.has(cluster.id) ? 'fill-yellow-400 text-yellow-400' : 'text-muted-foreground'}`}
                        />
                      </Button>
                    </td>
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
                      <div className="flex items-center gap-2">
                        {getTTLWarningLevel(cluster.destroy_at) === 'critical' && (
                          <AlertCircle className="h-4 w-4 text-red-500" />
                        )}
                        {getTTLWarningLevel(cluster.destroy_at) === 'warning' && (
                          <Clock className="h-4 w-4 text-yellow-500" />
                        )}
                        <span className={
                          getTTLWarningLevel(cluster.destroy_at) === 'critical' ? 'text-red-600 font-semibold' :
                          getTTLWarningLevel(cluster.destroy_at) === 'warning' ? 'text-yellow-600 font-medium' :
                          ''
                        }>
                          {formatTTL(cluster.destroy_at)}
                        </span>
                      </div>
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
