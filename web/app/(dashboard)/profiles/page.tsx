"use client";

import { useState } from "react";
import { useProfiles } from "@/lib/hooks/useProfiles";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatCurrency } from "@/lib/utils/formatters";
import { Platform } from "@/types/api";

export default function ProfilesPage() {
  const [selectedPlatform, setSelectedPlatform] = useState<Platform | undefined>();
  const { data: profiles, isLoading } = useProfiles(selectedPlatform);

  if (isLoading) {
    return <div>Loading profiles...</div>;
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Cluster Profiles</h1>
        <p className="text-muted-foreground">
          Pre-configured cluster sizes and configurations
        </p>
      </div>

      <div className="flex gap-2">
        <Button
          variant={selectedPlatform === undefined ? "default" : "outline"}
          size="sm"
          onClick={() => setSelectedPlatform(undefined)}
        >
          All Platforms
        </Button>
        <Button
          variant={selectedPlatform === Platform.AWS ? "default" : "outline"}
          size="sm"
          onClick={() => setSelectedPlatform(Platform.AWS)}
        >
          AWS
        </Button>
        <Button
          variant={selectedPlatform === Platform.IBMCloud ? "default" : "outline"}
          size="sm"
          onClick={() => setSelectedPlatform(Platform.IBMCloud)}
        >
          IBM Cloud
        </Button>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {profiles?.map((profile) => (
          <Card key={profile.name} className="hover:shadow-lg transition-shadow">
            <CardHeader>
              <div className="flex justify-between items-start">
                <CardTitle>{profile.display_name}</CardTitle>
                <Badge>{profile.platform.toUpperCase()}</Badge>
              </div>
              <CardDescription>{profile.description}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="space-y-2">
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Control Plane</span>
                  <span className="font-medium">
                    {profile.compute.control_plane.replicas} ×{" "}
                    {profile.compute.control_plane.instance_type}
                  </span>
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Workers</span>
                  <span className="font-medium">
                    {profile.compute.workers.replicas} ×{" "}
                    {profile.compute.workers.instance_type}
                  </span>
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Range</span>
                  <span className="font-medium">
                    {profile.compute.workers.min_replicas}-
                    {profile.compute.workers.max_replicas} workers
                  </span>
                </div>
              </div>

              <div className="border-t pt-4 space-y-2">
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Hourly Cost</span>
                  <span className="font-medium">
                    {formatCurrency(profile.lifecycle.estimated_hourly_cost)}/hr
                  </span>
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Default TTL</span>
                  <span className="font-medium">
                    {profile.lifecycle.default_ttl_hours}h
                  </span>
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Max TTL</span>
                  <span className="font-medium">
                    {profile.lifecycle.max_ttl_hours}h
                  </span>
                </div>
              </div>

              <div className="border-t pt-4">
                <div className="text-sm text-muted-foreground mb-2">Features</div>
                <div className="flex flex-wrap gap-1">
                  {profile.features.off_hours_scaling && (
                    <Badge variant="secondary">Off-hours Scaling</Badge>
                  )}
                  {profile.features.fips_mode && (
                    <Badge variant="secondary">FIPS Mode</Badge>
                  )}
                  {profile.features.private_cluster && (
                    <Badge variant="secondary">Private</Badge>
                  )}
                  {profile.compute.workers.autoscaling && (
                    <Badge variant="secondary">Autoscaling</Badge>
                  )}
                </div>
              </div>

              <div className="border-t pt-4">
                <div className="text-sm text-muted-foreground mb-2">
                  OpenShift Versions
                </div>
                <div className="text-sm">
                  {profile.openshift_versions.default} (default)
                </div>
                <div className="text-xs text-muted-foreground mt-1">
                  Available: {profile.openshift_versions.allowed.join(", ")}
                </div>
              </div>

              <div className="border-t pt-4">
                <div className="text-sm text-muted-foreground mb-2">
                  Regions
                </div>
                <div className="text-sm">
                  {profile.regions.default} (default)
                </div>
                <div className="text-xs text-muted-foreground mt-1">
                  Available: {profile.regions.allowed.join(", ")}
                </div>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  );
}
