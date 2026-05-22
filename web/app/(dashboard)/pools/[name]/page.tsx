"use client";

import { use } from "react";
import { useRouter } from "next/navigation";
import { usePoolStats } from "@/lib/hooks/usePools";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ArrowLeft, Server, Clock, Database, TrendingUp } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { poolsApi } from "@/lib/api";
import { toast } from "sonner";

interface PoolDetailPageProps {
  params: Promise<{ name: string }>;
}

export default function PoolDetailPage({ params }: PoolDetailPageProps) {
  const { name } = use(params);
  const router = useRouter();
  const { data, isLoading, error } = usePoolStats(name, { refetchInterval: 10000 }); // Refresh every 10s
  const [isLeasing, setIsLeasing] = useState(false);

  const handleLeaseCluster = async () => {
    try {
      setIsLeasing(true);
      const response = await poolsApi.leaseCluster(name, {
        leased_by: "Web UI User",
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

  if (error || !data) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg text-red-600">
          Error loading pool: {error instanceof Error ? error.message : 'Pool not found'}
        </div>
      </div>
    );
  }

  const stats = data;

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

      <div>
        <h1 className="text-3xl font-bold">{name}</h1>
        <p className="text-muted-foreground">Real-time pool statistics and cluster availability</p>
      </div>

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
    </div>
  );
}
