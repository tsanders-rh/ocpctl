"use client";

import { useUsers } from "@/lib/hooks/useUsers";
import { useClusters } from "@/lib/hooks/useClusters";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Users, Layers, Activity } from "lucide-react";

export default function AdminDashboardPage() {
  const { data: usersData } = useUsers();
  const { data: clustersData } = useClusters({ page: 1, per_page: 1 });

  const stats = [
    {
      title: "Total Users",
      value: usersData?.total || 0,
      icon: Users,
      description: "Registered users",
    },
    {
      title: "Total Clusters",
      value: clustersData?.pagination?.total || 0,
      icon: Layers,
      description: "All clusters (all users)",
    },
    {
      title: "System Status",
      value: "Healthy",
      icon: Activity,
      description: "All services operational",
    },
  ];

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Admin Dashboard</h1>
        <p className="text-muted-foreground">
          System overview and management
        </p>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
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
        </CardContent>
      </Card>
    </div>
  );
}
