"use client";

import { useParams, useRouter } from "next/navigation";
import { useProfile } from "@/lib/hooks/useProfiles";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { formatCurrency } from "@/lib/utils/formatters";
import { ArrowLeft, ExternalLink } from "lucide-react";
import Link from "next/link";

export default function ProfileDetailPage() {
  const params = useParams();
  const router = useRouter();
  const profileName = params.name as string;
  const { data: profile, isLoading, error } = useProfile(profileName);

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg">Loading profile...</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg text-red-600">
          Error loading profile: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      </div>
    );
  }

  if (!profile) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg">Profile not found</div>
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => router.push("/profiles")}
        >
          <ArrowLeft className="h-4 w-4 mr-2" />
          Back to Profiles
        </Button>
      </div>

      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-3xl font-bold">{profile.display_name}</h1>
          <p className="text-muted-foreground mt-2">{profile.description}</p>
        </div>
        <div className="flex gap-2">
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

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Compute Configuration */}
        <Card>
          <CardHeader>
            <CardTitle>Compute Configuration</CardTitle>
            <CardDescription>Instance types and sizing</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {profile.compute.control_plane && (
              <div>
                <div className="text-sm font-medium mb-2">Control Plane</div>
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Replicas</span>
                  <span className="font-medium">{profile.compute.control_plane.replicas}</span>
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Instance Type</span>
                  <span className="font-medium">{profile.compute.control_plane.instance_type}</span>
                </div>
              </div>
            )}

            {profile.compute.workers && (
              <div className="border-t pt-4">
                <div className="text-sm font-medium mb-2">Worker Nodes</div>
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Replicas</span>
                  <span className="font-medium">{profile.compute.workers.replicas ?? "N/A"}</span>
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Instance Type</span>
                  <span className="font-medium">{profile.compute.workers.instance_type}</span>
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Min Replicas</span>
                  <span className="font-medium">{profile.compute.workers.min_replicas ?? "N/A"}</span>
                </div>
                <div className="flex justify-between text-sm">
                  <span className="text-muted-foreground">Max Replicas</span>
                  <span className="font-medium">{profile.compute.workers.max_replicas ?? "N/A"}</span>
                </div>
                {profile.compute.workers.autoscaling && (
                  <div className="mt-2">
                    <Badge variant="secondary">Autoscaling Enabled</Badge>
                  </div>
                )}
              </div>
            )}

            {profile.compute.node_groups && profile.compute.node_groups.length > 0 && (
              <div className="border-t pt-4">
                <div className="text-sm font-medium mb-2">Node Groups</div>
                {profile.compute.node_groups.map((group, idx) => (
                  <div key={idx} className="mb-3 p-3 bg-muted rounded-md">
                    <div className="text-sm font-medium mb-1">{group.name}</div>
                    <div className="text-xs space-y-1">
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Instance Type</span>
                        <span>{group.instance_type}</span>
                      </div>
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Min Size</span>
                        <span>{group.min_size}</span>
                      </div>
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Max Size</span>
                        <span>{group.max_size}</span>
                      </div>
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">Desired Capacity</span>
                        <span>{group.desired_capacity}</span>
                      </div>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </CardContent>
        </Card>

        {/* Cost & Lifecycle */}
        <Card>
          <CardHeader>
            <CardTitle>Cost & Lifecycle</CardTitle>
            <CardDescription>Pricing and TTL settings</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div>
              <div className="text-sm font-medium mb-2">Cost Estimate</div>
              <div className="flex justify-between text-sm">
                <span className="text-muted-foreground">Hourly Cost</span>
                <span className="font-medium text-lg">
                  {formatCurrency(profile.cost_controls?.estimated_hourly_cost || 0)}/hr
                </span>
              </div>
            </div>

            <div className="border-t pt-4">
              <div className="text-sm font-medium mb-2">Lifecycle</div>
              <div className="flex justify-between text-sm">
                <span className="text-muted-foreground">Default TTL</span>
                <span className="font-medium">{profile.lifecycle.default_ttl_hours} hours</span>
              </div>
              <div className="flex justify-between text-sm">
                <span className="text-muted-foreground">Maximum TTL</span>
                <span className="font-medium">{profile.lifecycle.max_ttl_hours} hours</span>
              </div>
              <div className="flex justify-between text-sm">
                <span className="text-muted-foreground">Allow Custom TTL</span>
                <span className="font-medium">{profile.lifecycle.allow_custom_ttl ? "Yes" : "No"}</span>
              </div>
            </div>

            {profile.cost_controls && (
              <div className="border-t pt-4">
                <div className="text-sm font-medium mb-2">Cost Controls</div>
                {profile.cost_controls.max_monthly_cost !== undefined && (
                  <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground">Max Monthly Cost</span>
                    <span className="font-medium">{formatCurrency(profile.cost_controls.max_monthly_cost)}/mo</span>
                  </div>
                )}
                {profile.cost_controls.budget_alert_threshold !== undefined && (
                  <div className="flex justify-between text-sm">
                    <span className="text-muted-foreground">Budget Alert Threshold</span>
                    <span className="font-medium">{profile.cost_controls.budget_alert_threshold}%</span>
                  </div>
                )}
              </div>
            )}
          </CardContent>
        </Card>

        {/* Features */}
        <Card>
          <CardHeader>
            <CardTitle>Features</CardTitle>
            <CardDescription>Cluster capabilities and options</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex flex-wrap gap-2">
              {profile.features.off_hours_scaling && (
                <Badge variant="secondary">Off-hours Scaling</Badge>
              )}
              {profile.features.fips_mode && (
                <Badge variant="secondary">FIPS Mode</Badge>
              )}
              {profile.features.private_cluster && (
                <Badge variant="secondary">Private Cluster</Badge>
              )}
              {profile.compute.workers?.autoscaling && (
                <Badge variant="secondary">Worker Autoscaling</Badge>
              )}
            </div>

            {profile.credentials_mode && (
              <div className="border-t pt-4">
                <div className="text-sm font-medium mb-2">Credentials Mode</div>
                <div className="text-sm">
                  <Badge variant="outline">{profile.credentials_mode}</Badge>
                </div>
              </div>
            )}
          </CardContent>
        </Card>

        {/* Versions & Regions */}
        <Card>
          <CardHeader>
            <CardTitle>Versions & Regions</CardTitle>
            <CardDescription>Available versions and deployment locations</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div>
              <div className="text-sm font-medium mb-2">
                {profile.openshift_versions ? "OpenShift Versions" : "Kubernetes Versions"}
              </div>
              <div className="text-sm">
                <span className="font-medium">
                  {profile.openshift_versions?.default || profile.kubernetes_versions?.default || "N/A"}
                </span>
                <span className="text-muted-foreground"> (default)</span>
              </div>
              <div className="text-xs text-muted-foreground mt-2">
                Available: {(profile.openshift_versions?.allowed || profile.kubernetes_versions?.allowed)?.join(", ") || "N/A"}
              </div>
            </div>

            <div className="border-t pt-4">
              <div className="text-sm font-medium mb-2">Regions</div>
              <div className="text-sm">
                <span className="font-medium">{profile.regions?.default || "N/A"}</span>
                <span className="text-muted-foreground"> (default)</span>
              </div>
              <div className="text-xs text-muted-foreground mt-2">
                Available: {profile.regions?.allowed?.join(", ") || "N/A"}
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Post-Deployment */}
        {profile.post_deployment?.enabled && (
          <Card className="lg:col-span-2">
            <CardHeader>
              <CardTitle>Post-Deployment Configuration</CardTitle>
              <CardDescription>
                Automatically installed after cluster creation
              </CardDescription>
            </CardHeader>
            <CardContent>
              <div className="space-y-4">
                {profile.post_deployment.operators && profile.post_deployment.operators.length > 0 && (
                  <div>
                    <div className="text-sm font-medium mb-2">Operators</div>
                    <div className="space-y-2">
                      {profile.post_deployment.operators.map((operator) => (
                        <div key={operator.name} className="p-3 bg-muted rounded-md">
                          <div className="font-medium flex items-center gap-2">
                            <Badge variant="outline" className="text-xs">Operator</Badge>
                            {operator.name}
                          </div>
                          <div className="text-sm text-muted-foreground mt-1">
                            Namespace: {operator.namespace} • Channel: {operator.channel}
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {profile.post_deployment.scripts && profile.post_deployment.scripts.length > 0 && (
                  <div>
                    <div className="text-sm font-medium mb-2">Scripts</div>
                    <div className="space-y-2">
                      {profile.post_deployment.scripts.map((script) => (
                        <div key={script.name} className="p-3 bg-muted rounded-md">
                          <div className="font-medium flex items-center gap-2">
                            <Badge variant="outline" className="text-xs">Script</Badge>
                            {script.name}
                          </div>
                          {script.description && (
                            <div className="text-sm text-muted-foreground mt-1">
                              {script.description}
                            </div>
                          )}
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {profile.post_deployment.manifests && profile.post_deployment.manifests.length > 0 && (
                  <div>
                    <div className="text-sm font-medium mb-2">Manifests</div>
                    <div className="space-y-2">
                      {profile.post_deployment.manifests.map((manifest) => (
                        <div key={manifest.name} className="p-3 bg-muted rounded-md">
                          <div className="font-medium flex items-center gap-2">
                            <Badge variant="outline" className="text-xs">Manifest</Badge>
                            {manifest.name}
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                {profile.post_deployment.helm_charts && profile.post_deployment.helm_charts.length > 0 && (
                  <div>
                    <div className="text-sm font-medium mb-2">Helm Charts</div>
                    <div className="space-y-2">
                      {profile.post_deployment.helm_charts.map((chart) => (
                        <div key={chart.name} className="p-3 bg-muted rounded-md">
                          <div className="font-medium flex items-center gap-2">
                            <Badge variant="outline" className="text-xs">Helm</Badge>
                            {chart.name}
                          </div>
                          <div className="text-sm text-muted-foreground mt-1">
                            Chart: {chart.chart} • Namespace: {chart.namespace}
                          </div>
                        </div>
                      ))}
                    </div>
                  </div>
                )}

                <div className="text-sm text-muted-foreground">
                  💡 Post-deployment can be skipped during cluster creation
                </div>
              </div>
            </CardContent>
          </Card>
        )}
      </div>

      {/* Create Cluster CTA */}
      <div className="flex justify-end">
        <Link href={`/clusters/new?profile=${profile.name}`}>
          <Button size="lg">
            Create Cluster with {profile.display_name}
            <ExternalLink className="ml-2 h-4 w-4" />
          </Button>
        </Link>
      </div>
    </div>
  );
}
