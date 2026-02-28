"use client";

import { useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { useCluster, useDeleteCluster, useExtendCluster } from "@/lib/hooks/useClusters";
import { useJobs } from "@/lib/hooks/useJobs";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ClusterStatusBadge } from "@/components/clusters/ClusterStatusBadge";
import { formatDate, formatTTL, formatCurrency } from "@/lib/utils/formatters";
import { ArrowLeft, Trash2, Clock } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export default function ClusterDetailPage() {
  const params = useParams();
  const router = useRouter();
  const id = params.id as string;

  const { data: cluster, isLoading } = useCluster(id);
  const { data: jobsData } = useJobs({ cluster_id: id, per_page: 10 });
  const deleteCluster = useDeleteCluster();
  const extendCluster = useExtendCluster();

  const [extendHours, setExtendHours] = useState<number>(24);

  if (isLoading) {
    return <div>Loading cluster...</div>;
  }

  if (!cluster) {
    return <div>Cluster not found</div>;
  }

  const handleDelete = async () => {
    if (confirm(`Are you sure you want to delete cluster "${cluster.name}"?`)) {
      await deleteCluster.mutateAsync(id);
      router.push("/clusters");
    }
  };

  const handleExtend = async () => {
    if (extendHours > 0) {
      await extendCluster.mutateAsync({
        id,
        data: { ttl_hours: extendHours },
      });
      setExtendHours(24);
    }
  };

  const jobs = jobsData?.data || [];

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center space-x-4">
          <Button variant="ghost" size="sm" onClick={() => router.back()}>
            <ArrowLeft className="mr-2 h-4 w-4" />
            Back
          </Button>
          <div>
            <h1 className="text-3xl font-bold">{cluster.name}</h1>
            <p className="text-muted-foreground">
              {cluster.platform.toUpperCase()} • {cluster.region}
            </p>
          </div>
        </div>
        <ClusterStatusBadge status={cluster.status} />
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
        {/* Overview Card */}
        <Card className="md:col-span-2">
          <CardHeader>
            <CardTitle>Overview</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <div>
                <div className="text-sm font-medium text-muted-foreground">
                  Platform
                </div>
                <div className="text-lg">{cluster.platform.toUpperCase()}</div>
              </div>
              <div>
                <div className="text-sm font-medium text-muted-foreground">
                  Version
                </div>
                <div className="text-lg">{cluster.version}</div>
              </div>
              <div>
                <div className="text-sm font-medium text-muted-foreground">
                  Profile
                </div>
                <div className="text-lg">{cluster.profile}</div>
              </div>
              <div>
                <div className="text-sm font-medium text-muted-foreground">
                  Region
                </div>
                <div className="text-lg">{cluster.region}</div>
              </div>
              <div>
                <div className="text-sm font-medium text-muted-foreground">
                  Base Domain
                </div>
                <div className="text-lg">{cluster.base_domain}</div>
              </div>
              <div>
                <div className="text-sm font-medium text-muted-foreground">
                  Owner
                </div>
                <div className="text-lg">{cluster.owner}</div>
              </div>
              <div>
                <div className="text-sm font-medium text-muted-foreground">
                  Team
                </div>
                <div className="text-lg">{cluster.team}</div>
              </div>
              <div>
                <div className="text-sm font-medium text-muted-foreground">
                  Cost Center
                </div>
                <div className="text-lg">{cluster.cost_center}</div>
              </div>
              <div>
                <div className="text-sm font-medium text-muted-foreground">
                  Created
                </div>
                <div className="text-lg">{formatDate(cluster.created_at)}</div>
              </div>
              <div>
                <div className="text-sm font-medium text-muted-foreground">
                  TTL Remaining
                </div>
                <div className="text-lg">{formatTTL(cluster.destroy_at)}</div>
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Actions Card */}
        <Card>
          <CardHeader>
            <CardTitle>Actions</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {cluster.status === "READY" && (
              <div className="space-y-2">
                <Label htmlFor="extend-hours">Extend TTL (hours)</Label>
                <div className="flex gap-2">
                  <Input
                    id="extend-hours"
                    type="number"
                    min={1}
                    value={extendHours}
                    onChange={(e) => setExtendHours(Number(e.target.value))}
                    className="flex-1"
                  />
                  <Button
                    onClick={handleExtend}
                    disabled={extendCluster.isPending}
                    size="sm"
                  >
                    <Clock className="mr-2 h-4 w-4" />
                    Extend
                  </Button>
                </div>
              </div>
            )}

            {!["DESTROYING", "DESTROYED"].includes(cluster.status) && (
              <Button
                variant="destructive"
                className="w-full"
                onClick={handleDelete}
                disabled={deleteCluster.isPending}
              >
                <Trash2 className="mr-2 h-4 w-4" />
                {deleteCluster.isPending ? "Deleting..." : "Delete Cluster"}
              </Button>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Jobs Card */}
      <Card>
        <CardHeader>
          <CardTitle>Jobs</CardTitle>
        </CardHeader>
        <CardContent>
          {jobs.length === 0 ? (
            <p className="text-sm text-muted-foreground">No jobs found</p>
          ) : (
            <div className="space-y-2">
              {jobs.map((job) => (
                <div
                  key={job.id}
                  className="flex items-center justify-between p-3 border rounded-md"
                >
                  <div>
                    <div className="font-medium">{job.job_type}</div>
                    <div className="text-sm text-muted-foreground">
                      {formatDate(job.created_at)}
                    </div>
                  </div>
                  <div className="flex items-center gap-4">
                    <div className="text-sm text-muted-foreground">
                      Attempt {job.attempt}/{job.max_attempts}
                    </div>
                    <ClusterStatusBadge status={job.status as any} />
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Tags Card */}
      <Card>
        <CardHeader>
          <CardTitle>Tags</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 gap-2">
            {Object.entries(cluster.effective_tags).map(([key, value]) => (
              <div key={key} className="flex items-center gap-2 text-sm">
                <span className="font-medium">{key}:</span>
                <span className="text-muted-foreground">{value}</span>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
