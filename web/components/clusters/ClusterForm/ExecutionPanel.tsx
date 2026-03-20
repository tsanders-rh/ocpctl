import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { formatCurrency } from "@/lib/utils/formatters";
import type { Profile, CreateClusterRequest } from "@/types/api";

interface ExecutionPanelProps {
  formValues: Partial<CreateClusterRequest>;
  profile: Profile | undefined;
}

export function ExecutionPanel({ formValues, profile }: ExecutionPanelProps) {
  const estimatedCost = profile?.cost_controls?.estimated_hourly_cost || 0;
  const ttl = formValues.ttl_hours || profile?.lifecycle.default_ttl_hours || 0;

  return (
    <div className="space-y-6 sticky top-6">
      {/* Badges */}
      {profile && (
        <div className="flex gap-2 flex-wrap">
          <Badge variant="success">
            Approved Profile: {profile.display_name}
          </Badge>
          <Badge variant="info">Policy Enforced</Badge>
        </div>
      )}

      {/* Configuration Summary */}
      {profile && (
        <Card>
          <CardHeader>
            <CardTitle>Configuration</CardTitle>
          </CardHeader>
          <CardContent>
            <ul className="space-y-1 text-sm">
              <li>
                • Platform: {formValues.platform?.toUpperCase() || "Not selected"}
              </li>
              <li>• Region: {formValues.region || "Not selected"}</li>

              {/* OpenShift clusters - show control plane and workers */}
              {profile.compute?.control_plane && (
                <li>
                  • Control Plane: {profile.compute.control_plane.replicas} ×{" "}
                  {profile.compute.control_plane.instance_type}
                </li>
              )}
              {profile.compute?.workers && (
                <li>
                  • Workers: {profile.compute.workers.min_replicas ?? "?"}-
                  {profile.compute.workers.max_replicas ?? "?"} ×{" "}
                  {profile.compute.workers.instance_type}
                  {profile.compute.workers.autoscaling && " (autoscaling)"}
                </li>
              )}

              {/* EKS/IKS clusters - show node groups */}
              {profile.compute?.node_groups && profile.compute.node_groups.length > 0 && (
                <>
                  {profile.compute.node_groups.map((ng, idx) => (
                    <li key={idx}>
                      • {ng.name}: {ng.min_size}-{ng.max_size} ×{" "}
                      {ng.instance_type}
                      {ng.min_size !== ng.max_size && " (autoscaling)"}
                    </li>
                  ))}
                </>
              )}

              <li>• Estimated cost: {formatCurrency(estimatedCost)}/hour</li>
              <li>• Time to live: {ttl} hours</li>
              {formValues.extra_tags && Object.keys(formValues.extra_tags).length > 0 && (
                <>
                  <li className="mt-2 font-medium">Custom Tags:</li>
                  {Object.entries(formValues.extra_tags).map(([key, value]) => (
                    <li key={key} className="ml-4">
                      • {key}: {value}
                    </li>
                  ))}
                </>
              )}
            </ul>
          </CardContent>
        </Card>
      )}

      {/* Generated Install Config Preview (OpenShift only) */}
      {profile && formValues.name && formValues.cluster_type === "openshift" && profile.compute.control_plane && profile.compute.workers && (
        <Card>
          <CardHeader>
            <CardTitle>Install Config Preview</CardTitle>
          </CardHeader>
          <CardContent>
            <pre className="bg-muted p-4 rounded-md overflow-x-auto text-xs">
              <code>{`apiVersion: v1
baseDomain: ${formValues.base_domain || "<base-domain>"}
metadata:
  name: ${formValues.name}
platform:
  ${formValues.platform}:
    region: ${formValues.region || "<region>"}${formValues.extra_tags && Object.keys(formValues.extra_tags).length > 0 ? `
    userTags:${Object.entries(formValues.extra_tags).map(([key, value]) => `
      ${key}: ${value}`).join('')}` : ''}
controlPlane:
  name: master
  replicas: ${profile.compute.control_plane.replicas}
  platform:
    ${formValues.platform}:
      type: ${profile.compute.control_plane.instance_type}
compute:
- name: worker
  replicas: ${profile.compute.workers.replicas}
  platform:
    ${formValues.platform}:
      type: ${profile.compute.workers.instance_type}`}</code>
            </pre>
          </CardContent>
        </Card>
      )}

      {/* Request Payload */}
      {formValues.name && (
        <Card>
          <CardHeader>
            <CardTitle>Request Payload</CardTitle>
          </CardHeader>
          <CardContent>
            <pre className="bg-muted p-4 rounded-md overflow-x-auto text-xs max-h-60">
              <code>{JSON.stringify(formValues, null, 2)}</code>
            </pre>
          </CardContent>
        </Card>
      )}

      {/* Expected Outputs */}
      {formValues.name && formValues.base_domain && (
        <Card>
          <CardHeader>
            <CardTitle>Expected Outputs</CardTitle>
          </CardHeader>
          <CardContent>
            <ul className="space-y-1 text-sm text-muted-foreground">
              <li>
                • API URL: https://api.{formValues.name}.
                {formValues.base_domain}:6443
              </li>
              <li>
                • Console: https://console-openshift-console.apps.
                {formValues.name}.{formValues.base_domain}
              </li>
              <li>• Kubeconfig: S3 artifact after provisioning</li>
              <li>• Admin credentials: Secrets Manager reference</li>
            </ul>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
