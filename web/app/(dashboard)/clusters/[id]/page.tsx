"use client";

import { useState } from "react";
import { useParams, useRouter } from "next/navigation";
import { useCluster, useDeleteCluster, useExtendCluster, useClusterOutputs, useHibernateCluster, useResumeCluster } from "@/lib/hooks/useClusters";
import { useJobs } from "@/lib/hooks/useJobs";
import { useAuthStore } from "@/lib/stores/authStore";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ClusterStatusBadge } from "@/components/clusters/ClusterStatusBadge";
import { DeploymentLogs } from "@/components/clusters/DeploymentLogs";
import { StorageTab } from "@/components/clusters/StorageTab";
import { formatDate, formatTTL, formatCurrency } from "@/lib/utils/formatters";
import { ArrowLeft, Trash2, Clock, ExternalLink, Download, Copy, Moon, Sunrise } from "lucide-react";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

// Helper to convert work_days bitmask to day names
function workDaysBitmaskToNames(mask: number): string[] {
  const dayNames = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];
  const days: string[] = [];
  for (let i = 0; i < 7; i++) {
    if (mask & (1 << i)) {
      days.push(dayNames[i]);
    }
  }
  return days;
}

// Helper to check if a given day is a work day
function isWorkDay(workDaysMask: number, dayOfWeek: number): boolean {
  return (workDaysMask & (1 << dayOfWeek)) !== 0;
}

// Helper to calculate next action (hibernate or resume time)
function calculateNextAction(
  clusterStatus: string,
  workHoursStart: string,
  workHoursEnd: string,
  workDays: number,
  timezone: string,
  lastWorkHoursCheck?: string | null
): { action: string; timeDescription: string } | null {
  try {
    // Get current time in user's timezone
    const now = new Date();

    // Check if there's a grace period (manual resume)
    if (lastWorkHoursCheck && clusterStatus === 'READY') {
      const gracePeriodEnd = new Date(lastWorkHoursCheck);
      if (gracePeriodEnd > now) {
        // In grace period - show when it will be hibernated
        const formatter = new Intl.DateTimeFormat('en-US', {
          timeZone: timezone,
          weekday: 'short',
          month: 'short',
          day: 'numeric',
          hour: 'numeric',
          minute: '2-digit',
          hour12: true
        });
        const formattedTime = formatter.format(gracePeriodEnd);
        return { action: 'active', timeDescription: `Manual resume - hibernates ${formattedTime}` };
      }
    }

    const formatter = new Intl.DateTimeFormat('en-US', {
      timeZone: timezone,
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
      weekday: 'long',
    });

    const parts = formatter.formatToParts(now);
    const currentDay = parts.find(p => p.type === 'weekday')?.value;
    const currentHour = parseInt(parts.find(p => p.type === 'hour')?.value || '0');
    const currentMinute = parseInt(parts.find(p => p.type === 'minute')?.value || '0');
    const currentMinutes = currentHour * 60 + currentMinute;

    const dayMap: { [key: string]: number } = {
      'Sunday': 0, 'Monday': 1, 'Tuesday': 2, 'Wednesday': 3,
      'Thursday': 4, 'Friday': 5, 'Saturday': 6
    };
    const currentDayOfWeek = dayMap[currentDay || 'Sunday'];

    // Parse work hours
    const [startHour, startMinute] = workHoursStart.split(':').map(Number);
    const [endHour, endMinute] = workHoursEnd.split(':').map(Number);
    const startMinutes = startHour * 60 + startMinute;
    const endMinutes = endHour * 60 + endMinute;

    // Check if currently within work hours
    const isCurrentlyWorkDay = isWorkDay(workDays, currentDayOfWeek);
    let isWithinWorkHours = false;

    if (startMinutes < endMinutes) {
      // Normal case: 09:00 - 17:00
      isWithinWorkHours = isCurrentlyWorkDay && currentMinutes >= startMinutes && currentMinutes < endMinutes;
    } else {
      // Wraparound case: 22:00 - 06:00
      isWithinWorkHours = isCurrentlyWorkDay && (currentMinutes >= startMinutes || currentMinutes < endMinutes);
    }

    if (clusterStatus === 'READY') {
      if (!isWithinWorkHours) {
        return { action: 'hibernate', timeDescription: 'Will hibernate on next janitor cycle (within 5 min)' };
      }

      // Calculate time until work hours end
      let minutesUntilEnd: number;
      if (startMinutes < endMinutes) {
        minutesUntilEnd = endMinutes - currentMinutes;
      } else {
        // Wraparound case
        if (currentMinutes >= startMinutes) {
          minutesUntilEnd = (24 * 60) - currentMinutes + endMinutes;
        } else {
          minutesUntilEnd = endMinutes - currentMinutes;
        }
      }

      const hours = Math.floor(minutesUntilEnd / 60);
      const minutes = minutesUntilEnd % 60;

      if (hours > 0) {
        return { action: 'active', timeDescription: `Hibernates in ${hours}h ${minutes}m` };
      } else {
        return { action: 'active', timeDescription: `Hibernates in ${minutes} minutes` };
      }
    }

    if (clusterStatus === 'HIBERNATED') {
      if (isWithinWorkHours) {
        return { action: 'resume', timeDescription: 'Will resume on next janitor cycle (within 5 min)' };
      }

      // Calculate time until work hours start
      let minutesUntilStart: number;

      if (startMinutes < endMinutes) {
        // Normal case
        if (currentMinutes < startMinutes) {
          minutesUntilStart = startMinutes - currentMinutes;
        } else {
          // After work hours today, resume tomorrow or next work day
          let daysToAdd = 1;
          let nextDay = (currentDayOfWeek + daysToAdd) % 7;
          while (!isWorkDay(workDays, nextDay) && daysToAdd < 7) {
            daysToAdd++;
            nextDay = (currentDayOfWeek + daysToAdd) % 7;
          }
          minutesUntilStart = ((24 * 60) - currentMinutes) + (daysToAdd - 1) * 24 * 60 + startMinutes;
        }
      } else {
        // Wraparound case: 22:00 - 06:00
        if (currentMinutes < endMinutes) {
          // Currently in the early morning portion (before 06:00)
          // Work hours started yesterday at 22:00, shouldn't be hibernated
          minutesUntilStart = 0;
        } else if (currentMinutes < startMinutes) {
          // Between end and start (e.g., 10:00, between 06:00 and 22:00)
          minutesUntilStart = startMinutes - currentMinutes;
        } else {
          // After start time today (e.g., 23:00, after 22:00)
          // Already in work hours
          minutesUntilStart = 0;
        }
      }

      if (minutesUntilStart === 0) {
        return { action: 'resume', timeDescription: 'Should be resumed (check janitor logs)' };
      }

      const hours = Math.floor(minutesUntilStart / 60);
      const minutes = minutesUntilStart % 60;

      if (hours >= 24) {
        const days = Math.floor(hours / 24);
        const remainingHours = hours % 24;
        return { action: 'hibernated', timeDescription: `Resumes in ${days}d ${remainingHours}h` };
      } else if (hours > 0) {
        return { action: 'hibernated', timeDescription: `Resumes in ${hours}h ${minutes}m` };
      } else {
        return { action: 'hibernated', timeDescription: `Resumes in ${minutes} minutes` };
      }
    }

    return null;
  } catch (error) {
    console.error('Error calculating next action:', error);
    return null;
  }
}

export default function ClusterDetailPage() {
  const params = useParams();
  const router = useRouter();
  const id = params.id as string;

  const { user } = useAuthStore();
  const { data: cluster, isLoading } = useCluster(id);
  const { data: jobsData } = useJobs({ cluster_id: id, per_page: 10 });
  const { data: outputs } = useClusterOutputs(id, cluster?.status);
  const deleteCluster = useDeleteCluster();
  const extendCluster = useExtendCluster();
  const hibernateCluster = useHibernateCluster();
  const resumeCluster = useResumeCluster();

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

      {/* Cluster Information Card */}
      {cluster.status === "READY" && outputs && (
        <Card>
          <CardHeader>
            <CardTitle>Cluster Information</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {outputs.api_url && (
              <div className="space-y-2">
                <Label>API URL</Label>
                <div className="flex items-center gap-2">
                  <Input
                    value={outputs.api_url}
                    readOnly
                    className="flex-1 font-mono text-sm"
                  />
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => navigator.clipboard.writeText(outputs.api_url!)}
                  >
                    <Copy className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            )}

            {outputs.console_url && (
              <div className="space-y-2">
                <Label>Console URL</Label>
                <div className="flex items-center gap-2">
                  <Input
                    value={outputs.console_url}
                    readOnly
                    className="flex-1 font-mono text-sm"
                  />
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => navigator.clipboard.writeText(outputs.console_url!)}
                  >
                    <Copy className="h-4 w-4" />
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => window.open(outputs.console_url, '_blank')}
                  >
                    <ExternalLink className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            )}

            {outputs.kubeconfig_s3_uri && (
              <div className="space-y-2">
                <Label>Kubeconfig</Label>
                <div className="flex items-center gap-2">
                  <Input
                    value={outputs.kubeconfig_s3_uri}
                    readOnly
                    className="flex-1 font-mono text-sm"
                  />
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => navigator.clipboard.writeText(outputs.kubeconfig_s3_uri!)}
                  >
                    <Copy className="h-4 w-4" />
                  </Button>
                  <Button
                    size="sm"
                    onClick={async () => {
                      try {
                        const token = useAuthStore.getState().accessToken;
                        if (!token) {
                          alert('You are not authenticated. Please log in again.');
                          return;
                        }

                        // Get pre-signed download URL from API
                        const response = await fetch(`/api/v1/clusters/${cluster.id}/kubeconfig/download-url`, {
                          headers: {
                            'Authorization': `Bearer ${token}`,
                          },
                        });

                        if (!response.ok) {
                          throw new Error('Failed to get download URL');
                        }

                        const data = await response.json();

                        // For local storage (IBM Cloud), need to fetch with auth and create blob
                        if (data.storage_type === 'local') {
                          const kubeconfigResponse = await fetch(data.download_url, {
                            headers: {
                              'Authorization': `Bearer ${token}`,
                            },
                          });

                          if (!kubeconfigResponse.ok) {
                            throw new Error('Failed to fetch kubeconfig');
                          }

                          // Create blob from response and download
                          const blob = await kubeconfigResponse.blob();
                          const url = window.URL.createObjectURL(blob);
                          const link = document.createElement('a');
                          link.href = url;
                          link.download = data.filename || `kubeconfig-${cluster.name}.yaml`;
                          document.body.appendChild(link);
                          link.click();
                          document.body.removeChild(link);
                          window.URL.revokeObjectURL(url);
                        } else {
                          // For S3, use presigned URL directly (no auth needed)
                          const link = document.createElement('a');
                          link.href = data.download_url;
                          link.download = data.filename || `kubeconfig-${cluster.name}.yaml`;
                          document.body.appendChild(link);
                          link.click();
                          document.body.removeChild(link);
                        }
                      } catch (error) {
                        console.error('Failed to download kubeconfig:', error);
                        alert('Failed to download kubeconfig. Please try again.');
                      }
                    }}
                  >
                    <Download className="h-4 w-4" />
                  </Button>
                </div>
                <p className="text-xs text-muted-foreground">
                  {outputs.kubeconfig_s3_uri?.startsWith('s3://')
                    ? `S3 URI - Use AWS CLI to download: aws s3 cp ${outputs.kubeconfig_s3_uri} ./kubeconfig`
                    : `Local storage - Use download button above or access via API`}
                </p>
              </div>
            )}

            {outputs.kubeadmin && (
              <div className="space-y-2">
                <Label>Kubeadmin Credentials</Label>
                <div className="space-y-2">
                  <div className="flex items-center gap-2">
                    <Input
                      value={`Username: ${outputs.kubeadmin.username}`}
                      readOnly
                      className="flex-1 font-mono text-sm"
                    />
                  </div>
                  <div className="flex items-center gap-2">
                    <Input
                      value={`Password: ${outputs.kubeadmin.password}`}
                      readOnly
                      className="flex-1 font-mono text-sm"
                    />
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => navigator.clipboard.writeText(outputs.kubeadmin!.password)}
                    >
                      <Copy className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {/* Storage Card */}
      {(cluster.status === "READY" || cluster.status === "HIBERNATED") && (
        <Card>
          <CardHeader>
            <CardTitle>Storage</CardTitle>
          </CardHeader>
          <CardContent>
            <StorageTab clusterId={cluster.id} platform={cluster.platform} />
          </CardContent>
        </Card>
      )}

      {/* Work Hours Schedule Card */}
      {user && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Clock className="h-5 w-5" />
              Work Hours Schedule
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {/* Work Hours Status */}
            <div className="space-y-3">
              <div className="flex items-center justify-between">
                <div>
                  <div className="text-sm font-medium text-muted-foreground">Status</div>
                  <div className="text-lg font-medium">
                    {cluster.work_hours_enabled === null || cluster.work_hours_enabled === undefined ? (
                      user.work_hours_enabled ? (
                        <span className="text-green-600">Using profile defaults (enabled)</span>
                      ) : (
                        <span className="text-muted-foreground">Using profile defaults (disabled)</span>
                      )
                    ) : cluster.work_hours_enabled ? (
                      <span className="text-green-600">Hibernation enabled (cluster override)</span>
                    ) : (
                      <span className="text-muted-foreground">Always active (cluster override)</span>
                    )}
                  </div>
                </div>
              </div>

              {/* Show schedule if work hours are enabled (either from cluster or user profile) */}
              {((cluster.work_hours_enabled === null || cluster.work_hours_enabled === undefined) && user.work_hours_enabled) ||
               cluster.work_hours_enabled ? (
                <>
                  {/* Active Hours */}
                  <div>
                    <div className="text-sm font-medium text-muted-foreground">Active Hours</div>
                    <div className="text-lg">
                      {cluster.work_hours_start && cluster.work_hours_end ? (
                        `${cluster.work_hours_start} - ${cluster.work_hours_end}`
                      ) : user.work_hours ? (
                        `${user.work_hours.start_time} - ${user.work_hours.end_time}`
                      ) : (
                        "Not configured"
                      )}
                    </div>
                  </div>

                  {/* Work Days */}
                  <div>
                    <div className="text-sm font-medium text-muted-foreground">Work Days</div>
                    <div className="text-lg">
                      {cluster.work_days !== null && cluster.work_days !== undefined ? (
                        workDaysBitmaskToNames(cluster.work_days).join(", ")
                      ) : user.work_hours?.work_days ? (
                        user.work_hours.work_days.join(", ")
                      ) : (
                        "Not configured"
                      )}
                    </div>
                  </div>

                  {/* Timezone */}
                  <div>
                    <div className="text-sm font-medium text-muted-foreground">Timezone</div>
                    <div className="text-lg">{user.timezone}</div>
                  </div>

                  {/* Next Action */}
                  {(() => {
                    const workStart = cluster.work_hours_start || user.work_hours?.start_time;
                    const workEnd = cluster.work_hours_end || user.work_hours?.end_time;
                    const workDaysMask = cluster.work_days ?? (user.work_hours?.work_days ?
                      user.work_hours.work_days.reduce((mask, day) => {
                        const dayMap: { [key: string]: number } = {
                          'Sunday': 0, 'Monday': 1, 'Tuesday': 2, 'Wednesday': 3,
                          'Thursday': 4, 'Friday': 5, 'Saturday': 6
                        };
                        return mask | (1 << dayMap[day]);
                      }, 0) : 0
                    );

                    if (workStart && workEnd && workDaysMask !== null && workDaysMask !== undefined) {
                      const nextAction = calculateNextAction(
                        cluster.status,
                        workStart,
                        workEnd,
                        workDaysMask,
                        user.timezone,
                        cluster.last_work_hours_check
                      );

                      if (nextAction) {
                        return (
                          <div>
                            <div className="text-sm font-medium text-muted-foreground">Next Action</div>
                            <div className="text-lg font-medium">
                              {nextAction.action === 'hibernate' && (
                                <span className="text-yellow-600">{nextAction.timeDescription}</span>
                              )}
                              {nextAction.action === 'resume' && (
                                <span className="text-green-600">{nextAction.timeDescription}</span>
                              )}
                              {nextAction.action === 'active' && (
                                <span className="text-blue-600">{nextAction.timeDescription}</span>
                              )}
                              {nextAction.action === 'hibernated' && (
                                <span className="text-muted-foreground">{nextAction.timeDescription}</span>
                              )}
                            </div>
                          </div>
                        );
                      }
                    }
                    return null;
                  })()}

                  {/* Platform Support Notice */}
                  {cluster.platform !== "aws" && (
                    <div className="bg-yellow-50 border border-yellow-200 rounded-md p-3">
                      <p className="text-sm text-yellow-800">
                        <strong>Note:</strong> Automatic hibernation is only supported for AWS clusters.
                        This cluster will not be automatically hibernated.
                      </p>
                    </div>
                  )}
                </>
              ) : null}

              {/* Manual Controls */}
              {cluster.platform === "aws" && ["READY", "HIBERNATED"].includes(cluster.status) && (
                <div className="pt-4 border-t">
                  <div className="text-sm font-medium text-muted-foreground mb-3">Manual Controls</div>
                  <div className="flex gap-2">
                    {cluster.status === "READY" && (
                      <Button
                        variant="outline"
                        onClick={async () => {
                          if (confirm(`Are you sure you want to hibernate cluster "${cluster.name}"? This will stop all EC2 instances.`)) {
                            try {
                              await hibernateCluster.mutateAsync(id);
                            } catch (error: any) {
                              const errorMessage = error?.response?.message || error?.message || "Failed to hibernate cluster";
                              alert(`Hibernate failed: ${errorMessage}`);
                              console.error("Hibernate error:", error);
                            }
                          }
                        }}
                        disabled={hibernateCluster.isPending}
                      >
                        <Moon className="mr-2 h-4 w-4" />
                        {hibernateCluster.isPending ? "Hibernating..." : "Hibernate Now"}
                      </Button>
                    )}
                    {cluster.status === "HIBERNATED" && (
                      <Button
                        variant="outline"
                        onClick={async () => {
                          try {
                            await resumeCluster.mutateAsync(id);
                          } catch (error: any) {
                            const errorMessage = error?.response?.message || error?.message || "Failed to resume cluster";
                            alert(`Resume failed: ${errorMessage}`);
                            console.error("Resume error:", error);
                          }
                        }}
                        disabled={resumeCluster.isPending}
                      >
                        <Sunrise className="mr-2 h-4 w-4" />
                        {resumeCluster.isPending ? "Resuming..." : "Resume Now"}
                      </Button>
                    )}
                  </div>
                  <p className="text-xs text-muted-foreground mt-2">
                    {cluster.status === "READY"
                      ? "Hibernate cluster to stop EC2 instances and save costs. Cluster can be resumed later."
                      : "Resume cluster to restart EC2 instances and restore access."}
                  </p>
                </div>
              )}
            </div>
          </CardContent>
        </Card>
      )}

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

      {/* Deployment Logs Card */}
      {(cluster.status === "CREATING" || cluster.status === "READY" || cluster.status === "FAILED" || cluster.status === "DESTROYING") && (
        <DeploymentLogs
          clusterId={cluster.id}
          clusterStatus={cluster.status}
        />
      )}

      {/* Tags Card */}
      <Card>
        <CardHeader>
          <CardTitle>Tags</CardTitle>
        </CardHeader>
        <CardContent>
          {cluster.effective_tags && Object.keys(cluster.effective_tags).length > 0 ? (
            <div className="grid grid-cols-2 gap-2">
              {Object.entries(cluster.effective_tags).map(([key, value]) => (
                <div key={key} className="flex items-center gap-2 text-sm">
                  <span className="font-medium">{key}:</span>
                  <span className="text-muted-foreground">{value}</span>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">No tags</p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
