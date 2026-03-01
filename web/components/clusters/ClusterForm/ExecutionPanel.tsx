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
              <li>
                • Control Plane: {profile.compute?.control_plane?.replicas || "?"} ×{" "}
                {profile.compute?.control_plane?.instance_type || "?"}
              </li>
              <li>
                • Workers: {profile.compute?.workers?.min_replicas || "?"}-
                {profile.compute?.workers?.max_replicas || "?"} ×{" "}
                {profile.compute?.workers?.instance_type || "?"}
                {profile.compute?.workers?.autoscaling && " (autoscaling)"}
              </li>
              <li>• Estimated cost: {formatCurrency(estimatedCost)}/hour</li>
              <li>• Time to live: {ttl} hours</li>
            </ul>
          </CardContent>
        </Card>
      )}

      {/* Generated Install Config Preview */}
      {profile && formValues.name && (
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
    region: ${formValues.region || "<region>"}
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
