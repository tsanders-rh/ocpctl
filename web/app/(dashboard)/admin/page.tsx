"use client";

import { useUsers } from "@/lib/hooks/useUsers";
import { useClusters } from "@/lib/hooks/useClusters";
import { useClusterStatistics } from "@/lib/hooks/useAdminStats";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Users, Layers, Activity, TrendingUp, DollarSign } from "lucide-react";
import { DonutChart, BarList, Card as TremorCard, Title } from "@tremor/react";

export default function AdminDashboardPage() {
  const { data: usersData } = useUsers();
  const { data: clustersData } = useClusters({ page: 1, per_page: 1 });
  const { data: clusterStats, isLoading: statsLoading } = useClusterStatistics();

  const stats = [
    {
      title: "Total Users",
      value: usersData?.total || 0,
      icon: Users,
      description: "Registered users",
    },
    {
      title: "Total Clusters",
      value: clusterStats?.total_clusters || 0,
      icon: Layers,
      description: "All clusters (all time)",
    },
    {
      title: "Active Clusters",
      value: clusterStats?.active_clusters || 0,
      icon: TrendingUp,
      description: "Currently active",
    },
    {
      title: "System Status",
      value: "Healthy",
      icon: Activity,
      description: "All services operational",
    },
  ];

  // Color mapping for status (both legend and chart)
  const statusConfig = {
    READY: { chart: "emerald", legend: "bg-emerald-500" },
    CREATING: { chart: "cyan", legend: "bg-cyan-500" },
    DESTROYING: { chart: "amber", legend: "bg-amber-500" },
    HIBERNATED: { chart: "slate", legend: "bg-slate-500" },
    PROVISIONING: { chart: "blue", legend: "bg-blue-500" },
    FAILED: { chart: "red", legend: "bg-red-500" },
    UNKNOWN: { chart: "violet", legend: "bg-violet-500" },
  } as const;

  // Sort data in consistent order for color mapping
  const statusOrder = ["READY", "CREATING", "DESTROYING", "HIBERNATED", "PROVISIONING", "FAILED", "UNKNOWN"];
  const statusChartData = clusterStats?.clusters_by_status
    ?.map((item) => ({
      name: item.status,
      value: item.count,
    }))
    .sort((a, b) => {
      const indexA = statusOrder.indexOf(a.name);
      const indexB = statusOrder.indexOf(b.name);
      return (indexA === -1 ? 999 : indexA) - (indexB === -1 ? 999 : indexB);
    }) || [];

  // Build colors array matching the sorted data
  const statusChartColors = statusChartData.map((item) =>
    statusConfig[item.name as keyof typeof statusConfig]?.chart || "slate"
  );

  // Debug logging
  console.log("Status Chart Data:", statusChartData);
  console.log("Status Chart Colors:", statusChartColors);

  // Format data for profile bar list (sorted by count descending)
  const total = clusterStats?.active_clusters || 0;
  const profileListData = clusterStats?.clusters_by_profile
    ?.sort((a, b) => b.count - a.count)
    .slice(0, 10) // Show top 10 profiles
    .map((item) => ({
      name: item.profile,
      value: item.count,
    })) || [];

  // Platform color mapping
  const platformConfig = {
    aws: { chart: "blue", legend: "bg-blue-500" },
    gcp: { chart: "green", legend: "bg-green-500" },
    ibmcloud: { chart: "purple", legend: "bg-purple-500" },
    azure: { chart: "cyan", legend: "bg-cyan-500" },
  } as const;

  // Format data for platform donut chart
  const platformChartData = clusterStats?.clusters_by_platform
    ?.map((item) => ({
      name: item.platform.toUpperCase(),
      value: item.count,
    }))
    .sort((a, b) => b.value - a.value) || [];

  const platformChartColors = platformChartData.map((item) =>
    platformConfig[item.name.toLowerCase() as keyof typeof platformConfig]?.chart || "slate"
  );

  // Format cost data for displays
  const costByProfileData = clusterStats?.cost_by_profile
    ?.sort((a, b) => b.hourly_cost - a.hourly_cost)
    .slice(0, 10)
    .map((item) => ({
      name: item.profile,
      value: item.hourly_cost,
      // Store additional data for display
      count: item.cluster_count,
      daily: item.daily_cost,
      monthly: item.monthly_cost,
    })) || [];

  const costByUserData = clusterStats?.cost_by_user
    ?.sort((a, b) => b.hourly_cost - a.hourly_cost)
    .slice(0, 10)
    .map((item) => ({
      name: item.username,
      value: item.hourly_cost,
      count: item.cluster_count,
      daily: item.daily_cost,
      monthly: item.monthly_cost,
    })) || [];

  const formatCurrency = (value: number) => `$${value.toFixed(2)}`;

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Admin Dashboard</h1>
        <p className="text-muted-foreground">
          System overview and management
        </p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-4 gap-6">
        {stats.map((stat) => {
          const Icon = stat.icon;
          return (
            <Card key={stat.title}>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">
                  {stat.title}
                </CardTitle>
                <Icon className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">{stat.value}</div>
                <p className="text-xs text-muted-foreground">
                  {stat.description}
                </p>
              </CardContent>
            </Card>
          );
        })}
      </div>

      {/* Cluster Statistics Charts */}
      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        {/* Cluster Status Donut Chart */}
        <TremorCard>
          <Title>Active Clusters by Status</Title>
          {statsLoading ? (
            <div className="mt-6 h-80 flex items-center justify-center text-muted-foreground">
              Loading statistics...
            </div>
          ) : statusChartData.length > 0 ? (
            <>
              <div className="relative">
                <DonutChart
                  className="mt-6 h-44"
                  data={statusChartData}
                  category="value"
                  index="name"
                  valueFormatter={(value: number) => `${value}`}
                  colors={statusChartColors as any}
                  showAnimation={true}
                  showTooltip={false}
                  showLabel={false}
                  variant="donut"
                />
                <div className="absolute inset-0 flex flex-col items-center justify-center pointer-events-none">
                  <div className="text-3xl font-bold text-slate-900 dark:text-slate-100">
                    {clusterStats?.active_clusters || 0}
                  </div>
                  <div className="text-sm text-slate-600 dark:text-slate-400">
                    Active Clusters
                  </div>
                </div>
              </div>
              <div className="mt-8 space-y-3">
                {statusChartData.map((item) => {
                  const total = clusterStats?.active_clusters || 0;
                  const percentage = total > 0 ? Math.round((item.value / total) * 100) : 0;
                  return (
                    <div key={item.name} className="flex items-center justify-between text-sm">
                      <div className="flex items-center gap-2">
                        <span className={`h-3 w-3 rounded-full ${statusConfig[item.name as keyof typeof statusConfig]?.legend || 'bg-slate-500'}`} />
                        <span className="text-slate-600 dark:text-slate-400">{item.name}</span>
                      </div>
                      <span className="font-medium text-slate-900 dark:text-slate-100">
                        {item.value} ({percentage}%)
                      </span>
                    </div>
                  );
                })}
              </div>
            </>
          ) : (
            <div className="mt-6 h-80 flex items-center justify-center text-muted-foreground">
              No cluster data available
            </div>
          )}
        </TremorCard>

        {/* Cluster Platform Donut Chart */}
        <TremorCard>
          <Title>Active Clusters by Platform</Title>
          {statsLoading ? (
            <div className="mt-6 h-80 flex items-center justify-center text-muted-foreground">
              Loading statistics...
            </div>
          ) : platformChartData.length > 0 ? (
            <>
              <div className="relative">
                <DonutChart
                  className="mt-6 h-44"
                  data={platformChartData}
                  category="value"
                  index="name"
                  valueFormatter={(value: number) => `${value}`}
                  colors={platformChartColors as any}
                  showAnimation={true}
                  showTooltip={false}
                  showLabel={false}
                  variant="donut"
                />
                <div className="absolute inset-0 flex flex-col items-center justify-center pointer-events-none">
                  <div className="text-3xl font-bold text-slate-900 dark:text-slate-100">
                    {platformChartData.reduce((sum: number, item: { value: number }) => sum + item.value, 0)}
                  </div>
                  <div className="text-sm text-slate-600 dark:text-slate-400">
                    Platforms
                  </div>
                </div>
              </div>
              <div className="mt-8 space-y-3">
                {platformChartData.map((item: { name: string; value: number }) => {
                  const totalCount = platformChartData.reduce((sum: number, i: { value: number }) => sum + i.value, 0);
                  const percentage = totalCount > 0 ? Math.round((item.value / totalCount) * 100) : 0;
                  return (
                    <div key={item.name} className="flex items-center justify-between text-sm">
                      <div className="flex items-center gap-2">
                        <span className={`h-3 w-3 rounded-full ${platformConfig[item.name.toLowerCase() as keyof typeof platformConfig]?.legend || 'bg-slate-500'}`} />
                        <span className="text-slate-600 dark:text-slate-400">{item.name}</span>
                      </div>
                      <span className="font-medium text-slate-900 dark:text-slate-100">
                        {item.value} ({percentage}%)
                      </span>
                    </div>
                  );
                })}
              </div>
            </>
          ) : (
            <div className="mt-6 h-80 flex items-center justify-center text-muted-foreground">
              No platform data available
            </div>
          )}
        </TremorCard>

        {/* Cluster by Profile Bar List */}
        <TremorCard>
          <Title>Active Clusters by Profile</Title>
          {statsLoading ? (
            <div className="mt-6 flex items-center justify-center text-muted-foreground">
              Loading statistics...
            </div>
          ) : profileListData.length > 0 ? (
            <BarList
              data={profileListData}
              className="mt-6"
              color="blue"
              showAnimation={true}
              valueFormatter={(value: number) => {
                const percentage = total > 0 ? Math.round((value / total) * 100) : 0;
                return `${value} (${percentage}%)`;
              }}
            />
          ) : (
            <div className="mt-6 flex items-center justify-center text-muted-foreground">
              No cluster data available
            </div>
          )}
        </TremorCard>
      </div>

      {/* Resource & Cost Insights */}
      <div>
        <h2 className="text-2xl font-bold mb-4">Resource & Cost Insights</h2>

        {/* Cost Summary Cards */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-6 mb-6">
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Hourly Cost</CardTitle>
              <DollarSign className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">
                {formatCurrency(clusterStats?.total_hourly_cost || 0)}
              </div>
              <p className="text-xs text-muted-foreground">
                Current running rate
              </p>
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Daily Cost</CardTitle>
              <DollarSign className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">
                {formatCurrency(clusterStats?.total_daily_cost || 0)}
              </div>
              <p className="text-xs text-muted-foreground">
                Estimated per day
              </p>
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Monthly Cost</CardTitle>
              <DollarSign className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">
                {formatCurrency(clusterStats?.total_monthly_cost || 0)}
              </div>
              <p className="text-xs text-muted-foreground">
                Estimated per month (30 days)
              </p>
            </CardContent>
          </Card>
        </div>

        {/* Cost Breakdown Charts */}
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          {/* Cost by Profile */}
          <TremorCard>
            <Title>Cost by Profile</Title>
            {statsLoading ? (
              <div className="mt-6 flex items-center justify-center text-muted-foreground">
                Loading statistics...
              </div>
            ) : costByProfileData.length > 0 ? (
              <BarList
                data={costByProfileData}
                className="mt-6"
                color="emerald"
                showAnimation={true}
                valueFormatter={(value: number) => formatCurrency(value) + "/hr"}
              />
            ) : (
              <div className="mt-6 flex items-center justify-center text-muted-foreground">
                No cost data available
              </div>
            )}
          </TremorCard>

          {/* Cost by User */}
          <TremorCard>
            <Title>Cost by User</Title>
            {statsLoading ? (
              <div className="mt-6 flex items-center justify-center text-muted-foreground">
                Loading statistics...
              </div>
            ) : costByUserData.length > 0 ? (
              <BarList
                data={costByUserData}
                className="mt-6"
                color="amber"
                showAnimation={true}
                valueFormatter={(value: number) => formatCurrency(value) + "/hr"}
              />
            ) : (
              <div className="mt-6 flex items-center justify-center text-muted-foreground">
                No cost data available
              </div>
            )}
          </TremorCard>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Quick Actions</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2">
          <div className="text-sm text-muted-foreground">
            • View infrastructure: <a href="/admin/infrastructure" className="text-blue-600 hover:underline">Infrastructure Status</a>
          </div>
          <div className="text-sm text-muted-foreground">
            • Manage users: <a href="/admin/users" className="text-blue-600 hover:underline">User Management</a>
          </div>
          <div className="text-sm text-muted-foreground">
            • View all clusters: <a href="/clusters" className="text-blue-600 hover:underline">All Clusters</a>
          </div>
          <div className="text-sm text-muted-foreground">
            • Orphaned resources: <a href="/admin/orphaned-resources" className="text-blue-600 hover:underline">AWS Orphans</a>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
