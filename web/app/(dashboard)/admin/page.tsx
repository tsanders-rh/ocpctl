"use client";

import { useUsers } from "@/lib/hooks/useUsers";
import { useClusters } from "@/lib/hooks/useClusters";
import { useClusterStatistics } from "@/lib/hooks/useAdminStats";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Users, Layers, Activity, TrendingUp } from "lucide-react";
import { DonutChart, BarChart, Card as TremorCard, Title } from "@tremor/react";

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

  // Format data for donut chart
  const statusChartData = clusterStats?.clusters_by_status.map((item) => ({
    name: item.status,
    value: item.count,
  })) || [];

  // Format data for profile bar chart
  const profileChartData = clusterStats?.clusters_by_profile
    .sort((a, b) => b.count - a.count) // Sort by count descending
    .slice(0, 10) // Show top 10 profiles
    .map((item) => ({
      name: item.profile,
      "Cluster Count": item.count,
    })) || [];

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
      {!statsLoading && clusterStats && (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          {/* Cluster Status Donut Chart */}
          <TremorCard>
            <Title>Clusters by Status</Title>
            <DonutChart
              className="mt-6"
              data={statusChartData}
              category="value"
              index="name"
              valueFormatter={(value: number) => `${value} clusters`}
              colors={["emerald", "blue", "amber", "rose", "slate", "violet"]}
            />
          </TremorCard>

          {/* Cluster by Profile Bar Chart */}
          <TremorCard>
            <Title>Clusters by Profile</Title>
            <BarChart
              className="mt-6"
              data={profileChartData}
              index="name"
              categories={["Cluster Count"]}
              colors={["blue"]}
              valueFormatter={(value: number) => `${value} clusters`}
              yAxisWidth={48}
            />
          </TremorCard>
        </div>
      )}

      <Card>
        <CardHeader>
          <CardTitle>Quick Actions</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2">
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
