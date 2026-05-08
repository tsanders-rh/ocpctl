"use client";

import { useState } from "react";
import { useProfiles } from "@/lib/hooks/useProfiles";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatCurrency } from "@/lib/utils/formatters";
import { Platform } from "@/types/api";
import Link from "next/link";

export default function ProfilesPage() {
  const [selectedPlatform, setSelectedPlatform] = useState<Platform | undefined>();
  const [selectedTrack, setSelectedTrack] = useState<string | undefined>();
  const { data: profiles, isLoading, error } = useProfiles(selectedPlatform, selectedTrack);

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg">Loading profiles...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg text-red-600">
          Error loading profiles: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      </div>
    );
  }

  if (!profiles) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg">No profiles available</div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Cluster Profiles</h1>
        <p className="text-muted-foreground">
          Pre-configured cluster sizes and configurations
        </p>
      </div>

      <div className="space-y-4">
        <div>
          <div className="text-sm font-medium mb-2">Platform</div>
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
              variant={selectedPlatform === Platform.GCP ? "default" : "outline"}
              size="sm"
              onClick={() => setSelectedPlatform(Platform.GCP)}
            >
              GCP
            </Button>
            <Button
              variant={selectedPlatform === Platform.IBMCloud ? "default" : "outline"}
              size="sm"
              onClick={() => setSelectedPlatform(Platform.IBMCloud)}
            >
              IBM Cloud
            </Button>
          </div>
        </div>

        <div>
          <div className="text-sm font-medium mb-2">Release Track</div>
          <div className="flex gap-2">
            <Button
              variant={selectedTrack === undefined ? "default" : "outline"}
              size="sm"
              onClick={() => setSelectedTrack(undefined)}
            >
              All Tracks
            </Button>
            <Button
              variant={selectedTrack === "ga" ? "default" : "outline"}
              size="sm"
              onClick={() => setSelectedTrack("ga")}
            >
              GA (Stable)
            </Button>
            <Button
              variant={selectedTrack === "prerelease" ? "default" : "outline"}
              size="sm"
              onClick={() => setSelectedTrack("prerelease")}
            >
              Pre-Release
            </Button>
            <Button
              variant={selectedTrack === "kube" ? "default" : "outline"}
              size="sm"
              onClick={() => setSelectedTrack("kube")}
            >
              Kubernetes
            </Button>
          </div>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {profiles.map((profile) => (
          <Link key={profile.name} href={`/profiles/${profile.name}`}>
            <Card className="hover:shadow-lg transition-shadow cursor-pointer h-full">
              <CardHeader>
              <div className="flex justify-between items-start gap-2">
                <CardTitle>{profile.display_name}</CardTitle>
                <div className="flex gap-1 flex-wrap justify-end">
                  <Badge>{profile.platform.toUpperCase()}</Badge>
                  {profile.track && (
                    <Badge
                      className={
                        profile.track === "ga"
                          ? "bg-green-600 hover:bg-green-700 text-white"
                          : profile.track === "prerelease"
                          ? "bg-yellow-500 hover:bg-yellow-600 text-black"
                          : profile.track === "kube"
                          ? "bg-blue-600 hover:bg-blue-700 text-white"
                          : ""
                      }
                    >
                      {profile.track === "ga" ? "GA" : profile.track === "prerelease" ? "Pre-Release" : "Kube"}
                    </Badge>
                  )}
                </div>
              </div>
              <CardDescription>{profile.description}</CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="space-y-2">
                {profile.compute.control_plane && (
                  <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground">Control Plane</span>
                    <span className="font-medium">
                      {profile.compute.control_plane.replicas} ×{" "}
                      {profile.compute.control_plane.instance_type}
                    </span>
                  </div>
                )}
                {profile.compute.workers && (
                  <>
                    <div className="flex justify-between text-sm">
                      <span className="text-muted-foreground">Workers</span>
                      <span className="font-medium">
                        {profile.compute.workers.replicas ?? "?"} ×{" "}
                        {profile.compute.workers.instance_type}
                      </span>
                    </div>
                    <div className="flex justify-between text-sm">
                      <span className="text-muted-foreground">Range</span>
                      <span className="font-medium">
                        {profile.compute.workers.min_replicas ?? "?"}-
                        {profile.compute.workers.max_replicas ?? "?"} workers
                      </span>
                    </div>
                  </>
                )}
                {profile.compute.node_groups && profile.compute.node_groups.length > 0 && (
                  <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground">Node Groups</span>
                    <span className="font-medium">
                      {profile.compute.node_groups.length} group{profile.compute.node_groups.length !== 1 ? 's' : ''}
                    </span>
                  </div>
                )}
              </div>

              <div className="border-t pt-4 space-y-2">
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Hourly Cost</span>
                  <span className="font-medium">
                    {formatCurrency(profile.cost_controls?.estimated_hourly_cost || 0)}/hr
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
                  {profile.compute.workers?.autoscaling && (
                    <Badge variant="secondary">Autoscaling</Badge>
                  )}
                </div>
              </div>

              {profile.post_deployment?.enabled && (
                <div className="border-t pt-4">
                  <div className="text-sm text-muted-foreground mb-2">
                    Post-Deployment
                  </div>
                  <div className="text-xs text-muted-foreground mb-2">
                    Automatically installed after cluster creation:
                  </div>
                  <div className="space-y-2">
                    {profile.post_deployment.operators?.map((operator) => (
                      <div key={operator.name} className="text-sm">
                        <div className="font-medium flex items-center gap-2">
                          <Badge variant="outline" className="text-xs">Operator</Badge>
                          {operator.name}
                        </div>
                        <div className="text-xs text-muted-foreground ml-16">
                          Namespace: {operator.namespace} • Channel: {operator.channel}
                        </div>
                      </div>
                    ))}
                    {profile.post_deployment.scripts?.map((script) => (
                      <div key={script.name} className="text-sm">
                        <div className="font-medium flex items-center gap-2">
                          <Badge variant="outline" className="text-xs">Script</Badge>
                          {script.name}
                        </div>
                        {script.description && (
                          <div className="text-xs text-muted-foreground ml-14">
                            {script.description}
                          </div>
                        )}
                      </div>
                    ))}
                    {profile.post_deployment.manifests?.map((manifest) => (
                      <div key={manifest.name} className="text-sm">
                        <div className="font-medium flex items-center gap-2">
                          <Badge variant="outline" className="text-xs">Manifest</Badge>
                          {manifest.name}
                        </div>
                      </div>
                    ))}
                    {profile.post_deployment.helm_charts?.map((chart) => (
                      <div key={chart.name} className="text-sm">
                        <div className="font-medium flex items-center gap-2">
                          <Badge variant="outline" className="text-xs">Helm</Badge>
                          {chart.name}
                        </div>
                        <div className="text-xs text-muted-foreground ml-12">
                          Chart: {chart.chart} • Namespace: {chart.namespace}
                        </div>
                      </div>
                    ))}
                  </div>
                  <div className="text-xs text-muted-foreground mt-2">
                    💡 Can be skipped during cluster creation
                  </div>
                </div>
              )}

              {/* Version Information - show OpenShift or Kubernetes versions based on profile */}
              <div className="border-t pt-4">
                <div className="text-sm text-muted-foreground mb-2">
                  {profile.openshift_versions ? "OpenShift Versions" : "Kubernetes Versions"}
                </div>
                <div className="text-sm">
                  {profile.openshift_versions?.default || profile.kubernetes_versions?.default || "N/A"} (default)
                </div>
                <div className="text-xs text-muted-foreground mt-1">
                  Available: {(profile.openshift_versions?.allowed || profile.kubernetes_versions?.allowed)?.join(", ") || "N/A"}
                </div>
              </div>

              {profile.credentials_mode && (
                <div className="border-t pt-4">
                  <div className="text-sm text-muted-foreground mb-2">
                    Credentials Mode
                  </div>
                  <div className="text-sm">
                    {profile.credentials_mode}
                  </div>
                </div>
              )}

              <div className="border-t pt-4">
                <div className="text-sm text-muted-foreground mb-2">
                  Regions
                </div>
                <div className="text-sm">
                  {profile.regions?.default || "N/A"} (default)
                </div>
                <div className="text-xs text-muted-foreground mt-1">
                  Available: {profile.regions?.allowed?.join(", ") || "N/A"}
                </div>
              </div>
            </CardContent>
            </Card>
          </Link>
        ))}
      </div>
    </div>
  );
}
