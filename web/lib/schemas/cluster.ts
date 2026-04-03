import { z } from "zod";
import { Platform, ClusterType } from "@/types/api";

// Custom Post-Config schemas
const customResourceSchema = z.object({
  api_version: z.string(),
  kind: z.string(),
  name: z.string(),
  namespace: z.string().optional(),
  spec: z.record(z.any()).optional(),
});

const customOperatorSchema = z.object({
  name: z.string(),
  namespace: z.string(),
  source: z.string().optional(),
  channel: z.string(),
  custom_resource: customResourceSchema.optional(),
  variables: z.record(z.string()).optional(),
  condition: z.string().optional(),
  dependsOn: z.array(z.string()).optional(),
});

const customScriptSchema = z.object({
  name: z.string(),
  content: z.string().optional(),
  url: z.string().optional(),
  description: z.string().optional(),
  timeout: z.string().optional(),
  env: z.record(z.string()).optional(),
  variables: z.record(z.string()).optional(),
  condition: z.string().optional(),
  dependsOn: z.array(z.string()).optional(),
});

const customManifestSchema = z.object({
  name: z.string(),
  content: z.string().optional(),
  url: z.string().optional(),
  description: z.string().optional(),
  variables: z.record(z.string()).optional(),
  condition: z.string().optional(),
  dependsOn: z.array(z.string()).optional(),
});

const customHelmChartSchema = z.object({
  name: z.string(),
  repo: z.string(),
  chart: z.string(),
  version: z.string().optional(),
  namespace: z.string(),
  values: z.record(z.any()).optional(),
  variables: z.record(z.string()).optional(),
  condition: z.string().optional(),
  dependsOn: z.array(z.string()).optional(),
});

const customPostConfigSchema = z.object({
  operators: z.array(customOperatorSchema).optional(),
  scripts: z.array(customScriptSchema).optional(),
  manifests: z.array(customManifestSchema).optional(),
  helmCharts: z.array(customHelmChartSchema).optional(),
}).optional();

export const createClusterSchema = z.object({
  name: z
    .string()
    .min(3, "Minimum 3 characters")
    .max(63, "Maximum 63 characters")
    .regex(/^[a-z0-9-]+$/, "Must be lowercase alphanumeric with hyphens"),
  platform: z.nativeEnum(Platform, {
    required_error: "Platform is required",
  }),
  cluster_type: z.nativeEnum(ClusterType, {
    required_error: "Cluster type is required",
  }),
  version: z.string().min(1, "Version is required"),
  profile: z.string().min(1, "Profile is required"),
  region: z.string().min(1, "Region is required"),
  base_domain: z.string().optional(),
  owner: z.string().email("Invalid email address"),
  team: z.string().min(2, "Team name required (min 2 characters)"),
  cost_center: z.string().min(2, "Cost center required (min 2 characters)"),
  ttl_hours: z.number().int().min(0, "TTL must be 0 or greater (0 = never expires)").max(720),
  ssh_public_key: z.string().optional(),
  extra_tags: z.record(z.string()).optional(),
  offhours_opt_in: z.boolean().default(false),
  skip_post_deployment: z.boolean().default(false),
  postConfigAddOns: z.array(z.object({
    id: z.string(),
    version: z.string(),
  })).optional(),
  customPostConfig: customPostConfigSchema,
  enable_efs_storage: z.boolean().default(false),
  override_work_hours: z.boolean().default(false),
  work_hours_enabled: z.boolean().optional(),
  work_hours_start: z.string().optional(),
  work_hours_end: z.string().optional(),
  work_days: z.array(z.string()).optional(),
}).refine(
  (data) => {
    // base_domain is required for OpenShift clusters
    if (data.cluster_type === ClusterType.OpenShift) {
      return !!data.base_domain;
    }
    return true;
  },
  {
    message: "Base domain is required for OpenShift clusters",
    path: ["base_domain"],
  }
);

export type CreateClusterFormData = z.infer<typeof createClusterSchema>;
