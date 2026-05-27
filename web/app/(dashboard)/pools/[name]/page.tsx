"use client";

import { useRouter } from "next/navigation";
import { usePool } from "@/lib/hooks/usePools";
import { useProfile } from "@/lib/hooks/useProfiles";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ArrowLeft, Server, Clock, Database, TrendingUp, Settings, Info } from "lucide-react";
import Link from "next/link";
import { useState, useEffect } from "react";
import { poolsApi } from "@/lib/api";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";

interface PoolDetailPageProps {
  params: { name: string };
}

export default function PoolDetailPage({ params }: PoolDetailPageProps) {
  const { name } = params;
  const router = useRouter();
  const { data: pool, isLoading, error } = usePool(name, { refetchInterval: 10000 }); // Refresh every 10s
  const { data: profile } = useProfile(pool?.profile || "", { refetchInterval: 30000 }); // Fetch profile for version info
  const [isLeasing, setIsLeasing] = useState(false);
  const [leasedClusters, setLeasedClusters] = useState<any[]>([]);
  const [loadingClusters, setLoadingClusters] = useState(false);

  // Fetch leased clusters
  useEffect(() => {
    if (!name) return;

    const fetchLeasedClusters = async () => {
      try {
        setLoadingClusters(true);
        const response = await poolsApi.getPoolClusters(name, "LEASED");
        setLeasedClusters(response.clusters);
      } catch (err) {
        console.error("Failed to fetch leased clusters:", err);
      } finally {
        setLoadingClusters(false);
      }
    };

    fetchLeasedClusters();
    const interval = setInterval(fetchLeasedClusters, 10000); // Refresh every 10s

    return () => clearInterval(interval);
  }, [name]);

  const handleLeaseCluster = async () => {
    try {
      setIsLeasing(true);
      const response = await poolsApi.leaseCluster(name, {
        metadata: {
          source: "web-ui",
          timestamp: new Date().toISOString(),
        },
      });

      toast.success("Cluster leased successfully!", {
        description: `Cluster ${response.cluster_name} is now available`,
      });

      // Navigate to the cluster detail page
      router.push(`/clusters/${response.cluster_id}`);
    } catch (err) {
      toast.error("Failed to lease cluster", {
        description: err instanceof Error ? err.message : "Unknown error",
      });
    } finally {
      setIsLeasing(false);
    }
  };

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg">Loading pool details...</div>
      </div>
    );
  }

  if (error || !pool) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg text-red-600">
          Error loading pool: {error instanceof Error ? error.message : 'Pool not found'}
        </div>
      </div>
    );
  }

  const stats = pool.stats;

  // Get cluster version from pool config or profile default
  const getClusterVersion = () => {
    // Check if pool has a version override in cluster_config
    if (pool.cluster_config?.version) {
      return pool.cluster_config.version;
    }
    // Otherwise use profile default
    if (profile) {
      if (profile.openshift_versions) {
        return profile.openshift_versions.default || 'Latest';
      } else if (profile.kubernetes_versions) {
        return profile.kubernetes_versions.default || 'Latest';
      }
    }
    return 'Default';
  };

  const clusterVersion = getClusterVersion();
  const isOpenShift = profile?.openshift_versions !== undefined;
  const platform = profile?.platform || 'aws';
  const region = pool.cluster_config?.region || profile?.regions?.default || 'us-east-1';

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Link href="/pools">
          <Button variant="outline" size="sm">
            <ArrowLeft className="h-4 w-4 mr-2" />
            Back to Pools
          </Button>
        </Link>
      </div>

      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-3xl font-bold">{pool.display_name}</h1>
          <p className="text-muted-foreground mt-1">{pool.description || "Pre-provisioned cluster pool"}</p>
        </div>
        <div className="flex gap-2">
          <Badge variant="outline" className="text-base px-4 py-1">
            {platform.toUpperCase()}
          </Badge>
          <Badge variant="outline" className="text-base px-4 py-1">
            {pool.profile}
          </Badge>
        </div>
      </div>

      {/* Pool Configuration */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Settings className="h-5 w-5" />
            <CardTitle>Pool Configuration</CardTitle>
          </div>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 md:grid-cols-3 gap-4 text-sm">
            <div>
              <p className="text-muted-foreground">
                {isOpenShift ? 'OpenShift Version' : 'Kubernetes Version'}
              </p>
              <p className="font-medium">{clusterVersion}</p>
            </div>
            <div>
              <p className="text-muted-foreground">Region</p>
              <p className="font-medium">{region}</p>
            </div>
            <div>
              <p className="text-muted-foreground">Max Lease Duration</p>
              <p className="font-medium">{pool.max_lease_duration_hours} hours</p>
            </div>
            <div>
              <p className="text-muted-foreground">Target Size</p>
              <p className="font-medium">{pool.target_size} clusters</p>
            </div>
            <div>
              <p className="text-muted-foreground">Pool Range</p>
              <p className="font-medium">{pool.min_size} - {pool.max_size} clusters</p>
            </div>
            {pool.auto_refresh_enabled && (
              <div>
                <p className="text-muted-foreground">Auto Refresh</p>
                <p className="font-medium">Every {pool.max_cluster_age_days} days</p>
              </div>
            )}
            {pool.scheduled_mode && (
              <>
                <div>
                  <p className="text-muted-foreground">Active Hours</p>
                  <p className="font-medium">
                    {pool.schedule_start_hour}:00 - {pool.schedule_end_hour}:00
                  </p>
                </div>
                <div>
                  <p className="text-muted-foreground">Timezone</p>
                  <p className="font-medium">{pool.schedule_timezone}</p>
                </div>
              </>
            )}
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-4">
        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium text-muted-foreground">Ready Clusters</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2">
              <Server className="h-5 w-5 text-green-600" />
              <span className="text-3xl font-bold">{stats.ready_clusters}</span>
            </div>
            <p className="text-xs text-muted-foreground mt-2">Available for lease</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium text-muted-foreground">Leased Clusters</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2">
              <Clock className="h-5 w-5 text-blue-600" />
              <span className="text-3xl font-bold">{stats.leased_clusters}</span>
            </div>
            <p className="text-xs text-muted-foreground mt-2">Currently in use</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium text-muted-foreground">Provisioning</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2">
              <TrendingUp className="h-5 w-5 text-orange-600" />
              <span className="text-3xl font-bold">{stats.provisioning_clusters}</span>
            </div>
            <p className="text-xs text-muted-foreground mt-2">Being created</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-3">
            <CardTitle className="text-sm font-medium text-muted-foreground">Total Clusters</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-2">
              <Database className="h-5 w-5 text-purple-600" />
              <span className="text-3xl font-bold">{stats.total_clusters}</span>
            </div>
            <p className="text-xs text-muted-foreground mt-2">In pool</p>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Lease a Cluster</CardTitle>
          <CardDescription>
            Get immediate access to a pre-provisioned cluster from this pool
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {stats.ready_clusters > 0 ? (
            <>
              <div className="flex items-center justify-between p-4 bg-green-50 border border-green-200 rounded-lg">
                <div className="flex items-center gap-3">
                  <div className="h-12 w-12 rounded-full bg-green-100 flex items-center justify-center">
                    <Server className="h-6 w-6 text-green-600" />
                  </div>
                  <div>
                    <p className="font-medium">{stats.ready_clusters} cluster{stats.ready_clusters !== 1 ? 's' : ''} available</p>
                    <p className="text-sm text-muted-foreground">
                      Clusters are ready for immediate use
                    </p>
                  </div>
                </div>
                <Button
                  onClick={handleLeaseCluster}
                  disabled={isLeasing}
                  size="lg"
                >
                  {isLeasing ? "Leasing..." : "Lease Cluster"}
                </Button>
              </div>

              <div className="text-sm text-muted-foreground">
                <p>When you lease a cluster:</p>
                <ul className="list-disc list-inside ml-2 mt-1 space-y-1">
                  <li>You'll get immediate access to a fully configured cluster</li>
                  <li>The cluster will be automatically released after the lease period</li>
                  <li>You can manually release it earlier when you're done</li>
                </ul>
              </div>
            </>
          ) : (
            <div className="flex items-center justify-center p-8 bg-gray-50 border border-gray-200 rounded-lg">
              <div className="text-center">
                <Database className="h-12 w-12 text-muted-foreground mx-auto mb-3" />
                <p className="font-medium">No clusters available</p>
                <p className="text-sm text-muted-foreground mt-1">
                  {stats.provisioning_clusters > 0
                    ? `${stats.provisioning_clusters} cluster${stats.provisioning_clusters !== 1 ? 's are' : ' is'} currently being provisioned`
                    : "Check back later or contact your administrator"
                  }
                </p>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Leased Clusters */}
      {leasedClusters.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Leased Clusters</CardTitle>
            <CardDescription>
              Clusters currently in use from this pool
            </CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-3">
              {leasedClusters.map((cluster) => (
                <div
                  key={cluster.id}
                  className="flex items-center justify-between p-4 border rounded-lg hover:bg-gray-50"
                >
                  <div className="flex-1">
                    <div className="flex items-center gap-3">
                      <Link
                        href={`/clusters/${cluster.id}`}
                        className="font-medium hover:underline"
                      >
                        {cluster.name}
                      </Link>
                      <Badge variant="outline" className="text-xs">
                        {cluster.status}
                      </Badge>
                    </div>
                    <div className="flex items-center gap-4 mt-2 text-sm text-muted-foreground">
                      <div className="flex items-center gap-1">
                        <Clock className="h-3 w-3" />
                        <span>
                          Leased {cluster.leased_at ? formatDistanceToNow(new Date(cluster.leased_at), { addSuffix: true }) : "unknown"}
                        </span>
                      </div>
                      {cluster.leased_by && (
                        <div>
                          By: <span className="font-medium">{cluster.leased_by}</span>
                        </div>
                      )}
                      {cluster.lease_expires_at && (
                        <div>
                          Expires: {formatDistanceToNow(new Date(cluster.lease_expires_at), { addSuffix: true })}
                        </div>
                      )}
                    </div>
                  </div>
                  <Link href={`/clusters/${cluster.id}`}>
                    <Button variant="outline" size="sm">
                      View Details
                    </Button>
                  </Link>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
