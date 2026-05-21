'use client';

import { useEffect, useState } from 'react';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Activity, Database, Layers, Server, Users, TrendingUp } from 'lucide-react';
import { useAuthStore } from '@/lib/stores/authStore';

interface MetricsSnapshot {
  api: {
    requests_per_second: number;
    active_connections: number;
    error_rate: number;
    requests_by_status: Record<string, number>;
  };
  workers: {
    total: number;
    active: number;
    idle: number;
  };
  jobs: {
    queued_by_type: Record<string, number>;
    processing_total: number;
    total_queued: number;
  };
  clusters: {
    total: number;
    by_status: Record<string, number>;
    by_profile: Record<string, number>;
  };
  autoscale: {
    current_workers: number;
    desired_workers: number;
  };
  database: {
    open_connections: number;
    max_connections: number;
  };
}

export default function MonitoringPage() {
  const [metrics, setMetrics] = useState<MetricsSnapshot | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [lastUpdate, setLastUpdate] = useState<Date>(new Date());
  const { accessToken } = useAuthStore();

  const fetchMetrics = async () => {
    try {
      console.log('[Monitoring] fetchMetrics called, token present:', !!accessToken);

      if (!accessToken) {
        throw new Error('Not authenticated');
      }

      console.log('[Monitoring] Fetching from /api/v1/admin/metrics/current');
      const response = await fetch('/api/v1/admin/metrics/current', {
        headers: {
          'Authorization': `Bearer ${accessToken}`,
        },
      });

      console.log('[Monitoring] Response status:', response.status);

      if (!response.ok) {
        const errorData = await response.json().catch(() => ({ message: response.statusText }));
        throw new Error(`HTTP ${response.status}: ${errorData.message || 'Failed to fetch metrics'}`);
      }

      const data = await response.json();
      console.log('[Monitoring] Metrics received:', data);
      setMetrics(data);
      setLastUpdate(new Date());
      setError(null);
    } catch (err) {
      console.error('[Monitoring] Error fetching metrics:', err);
      setError(err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (!accessToken) {
      console.log('[Monitoring] No access token available');
      setLoading(false);
      setError('Authentication required');
      return;
    }

    console.log('[Monitoring] Starting metrics fetch with token');

    // Initial fetch
    fetchMetrics();

    // Auto-refresh every 15 seconds
    const interval = setInterval(fetchMetrics, 15000);

    return () => clearInterval(interval);
  }, [accessToken]);

  if (loading && !metrics) {
    return (
      <div className="container mx-auto p-6">
        <div className="flex items-center justify-center h-64">
          <div className="text-center">
            <Activity className="mx-auto h-12 w-12 animate-pulse text-muted-foreground" />
            <p className="mt-4 text-muted-foreground">Loading metrics...</p>
          </div>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="container mx-auto p-6">
        <Card className="border-destructive">
          <CardHeader>
            <CardTitle className="text-destructive">Error Loading Metrics</CardTitle>
          </CardHeader>
          <CardContent>
            <p>{error}</p>
          </CardContent>
        </Card>
      </div>
    );
  }

  if (!metrics) return null;

  return (
    <div className="container mx-auto p-6 space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">System Monitoring</h1>
          <p className="text-muted-foreground mt-1">
            Real-time metrics • Last updated: {lastUpdate.toLocaleTimeString()}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <div className="flex items-center gap-2 text-sm">
            <div className="h-2 w-2 rounded-full bg-green-500 animate-pulse" />
            <span className="text-muted-foreground">Live (15s refresh)</span>
          </div>
        </div>
      </div>

      {/* Top Stats Row */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        <StatsCard
          title="Total Clusters"
          value={metrics.clusters.total}
          icon={Layers}
          description={`${metrics.clusters.by_status['READY'] || 0} ready`}
          color="blue"
        />
        <StatsCard
          title="Job Queue"
          value={metrics.jobs.total_queued}
          icon={Activity}
          description={`${metrics.jobs.processing_total} processing`}
          color={metrics.jobs.total_queued > 10 ? 'yellow' : 'green'}
        />
        <StatsCard
          title="Active Workers"
          value={`${metrics.workers.active}/${metrics.workers.total}`}
          icon={Server}
          description={`${metrics.workers.idle} idle`}
          color="purple"
        />
        <StatsCard
          title="DB Connections"
          value={metrics.database.open_connections}
          icon={Database}
          description={`${metrics.database.max_connections} max`}
          color="gray"
        />
      </div>

      {/* Clusters Section */}
      <div className="grid gap-4 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Clusters by Status</CardTitle>
            <CardDescription>Current cluster inventory</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-3">
              {Object.entries(metrics.clusters.by_status).map(([status, count]) => (
                <div key={status} className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <div
                      className={`h-3 w-3 rounded-full ${getStatusColor(status)}`}
                    />
                    <span className="text-sm font-medium">{status}</span>
                  </div>
                  <span className="text-2xl font-bold">{count}</span>
                </div>
              ))}
              {Object.keys(metrics.clusters.by_status).length === 0 && (
                <p className="text-center text-muted-foreground py-4">No clusters</p>
              )}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Clusters by Profile</CardTitle>
            <CardDescription>Profile distribution</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="space-y-3">
              {Object.entries(metrics.clusters.by_profile)
                .sort(([, a], [, b]) => b - a)
                .map(([profile, count]) => (
                  <div key={profile} className="flex items-center justify-between">
                    <span className="text-sm font-medium">{profile}</span>
                    <span className="text-2xl font-bold">{count}</span>
                  </div>
                ))}
              {Object.keys(metrics.clusters.by_profile).length === 0 && (
                <p className="text-center text-muted-foreground py-4">No clusters</p>
              )}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Jobs Section */}
      <Card>
        <CardHeader>
          <CardTitle>Job Queue by Type</CardTitle>
          <CardDescription>Pending jobs waiting for execution</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-3">
            {Object.entries(metrics.jobs.queued_by_type).map(([type, count]) => (
              <div key={type} className="flex items-center justify-between">
                <span className="text-sm font-medium">{type}</span>
                <div className="flex items-center gap-4">
                  <div className="w-64 h-6 bg-muted rounded-full overflow-hidden">
                    <div
                      className="h-full bg-blue-500 transition-all duration-300"
                      style={{
                        width: `${Math.min(100, (count / Math.max(...Object.values(metrics.jobs.queued_by_type))) * 100)}%`,
                      }}
                    />
                  </div>
                  <span className="text-2xl font-bold w-12 text-right">{count}</span>
                </div>
              </div>
            ))}
            {Object.keys(metrics.jobs.queued_by_type).length === 0 && (
              <p className="text-center text-muted-foreground py-8">
                Queue is empty
              </p>
            )}
          </div>
        </CardContent>
      </Card>

      {/* Autoscaling Section */}
      {metrics.autoscale.current_workers !== metrics.autoscale.desired_workers && (
        <Card className="border-yellow-500">
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <TrendingUp className="h-5 w-5" />
              Autoscaling in Progress
            </CardTitle>
            <CardDescription>Worker count adjustment</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="flex items-center gap-8">
              <div className="text-center">
                <div className="text-4xl font-bold">{metrics.autoscale.current_workers}</div>
                <div className="text-sm text-muted-foreground mt-1">Current</div>
              </div>
              <div className="text-3xl text-muted-foreground">→</div>
              <div className="text-center">
                <div className="text-4xl font-bold text-blue-600">
                  {metrics.autoscale.desired_workers}
                </div>
                <div className="text-sm text-muted-foreground mt-1">Desired</div>
              </div>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
}

interface StatsCardProps {
  title: string;
  value: string | number;
  icon: React.ElementType;
  description: string;
  color?: 'blue' | 'green' | 'yellow' | 'purple' | 'gray';
}

function StatsCard({ title, value, icon: Icon, description, color = 'gray' }: StatsCardProps) {
  const colorClasses = {
    blue: 'bg-blue-100 text-blue-600',
    green: 'bg-green-100 text-green-600',
    yellow: 'bg-yellow-100 text-yellow-600',
    purple: 'bg-purple-100 text-purple-600',
    gray: 'bg-gray-100 text-gray-600',
  };

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
        <CardTitle className="text-sm font-medium">{title}</CardTitle>
        <div className={`p-2 rounded-lg ${colorClasses[color]}`}>
          <Icon className="h-4 w-4" />
        </div>
      </CardHeader>
      <CardContent>
        <div className="text-2xl font-bold">{value}</div>
        <p className="text-xs text-muted-foreground mt-1">{description}</p>
      </CardContent>
    </Card>
  );
}

function getStatusColor(status: string): string {
  const colors: Record<string, string> = {
    READY: 'bg-green-500',
    CREATING: 'bg-blue-500',
    PENDING: 'bg-yellow-500',
    FAILED: 'bg-red-500',
    DESTROYING: 'bg-orange-500',
    DESTROYED: 'bg-gray-400',
    HIBERNATED: 'bg-purple-500',
  };
  return colors[status] || 'bg-gray-500';
}
