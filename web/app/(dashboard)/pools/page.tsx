"use client";

import { usePools } from "@/lib/hooks/usePools";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Database, Users, Clock, Server } from "lucide-react";
import Link from "next/link";

export default function PoolsPage() {
  const { data, isLoading, error } = usePools();

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg">Loading pools...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg text-red-600">
          Error loading pools: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      </div>
    );
  }

  const pools = data?.pools || [];
  const enabledPools = pools.filter(p => p.enabled);

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Cluster Pools</h1>
        <p className="text-muted-foreground">
          Pre-provisioned clusters ready for immediate use
        </p>
      </div>

      {enabledPools.length === 0 ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-12">
            <Database className="h-12 w-12 text-muted-foreground mb-4" />
            <p className="text-lg font-medium">No pools available</p>
            <p className="text-sm text-muted-foreground">
              Contact your administrator to create cluster pools
            </p>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-3">
          {enabledPools.map((pool) => (
            <Card key={pool.id} className="hover:shadow-lg transition-shadow">
              <CardHeader>
                <div className="flex items-start justify-between">
                  <div>
                    <CardTitle className="text-xl">{pool.display_name}</CardTitle>
                    <CardDescription className="mt-1">
                      {pool.description || `${pool.profile} cluster pool`}
                    </CardDescription>
                  </div>
                  <Badge variant="default" className="ml-2">
                    {pool.profile}
                  </Badge>
                </div>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="grid grid-cols-2 gap-4 text-sm">
                  <div className="flex items-center gap-2">
                    <Server className="h-4 w-4 text-muted-foreground" />
                    <span className="text-muted-foreground">Target:</span>
                    <span className="font-medium">{pool.target_size}</span>
                  </div>
                  <div className="flex items-center gap-2">
                    <Clock className="h-4 w-4 text-muted-foreground" />
                    <span className="text-muted-foreground">Max Lease:</span>
                    <span className="font-medium">{pool.max_lease_duration_hours}h</span>
                  </div>
                </div>

                {pool.scheduled_mode && (
                  <div className="text-sm">
                    <Badge variant="outline">Work Hours Only</Badge>
                    <p className="text-muted-foreground mt-1">
                      {pool.schedule_start_hour}:00 - {pool.schedule_end_hour}:00 {pool.schedule_timezone}
                    </p>
                  </div>
                )}

                <div className="pt-2">
                  <Link href={`/pools/${pool.name}`}>
                    <Button className="w-full">
                      View Pool Details
                    </Button>
                  </Link>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
