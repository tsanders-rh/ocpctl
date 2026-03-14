import { z } from "zod";
import { Platform } from "@/types/api";

export const createClusterSchema = z.object({
  name: z
    .string()
    .min(3, "Minimum 3 characters")
    .max(63, "Maximum 63 characters")
    .regex(/^[a-z0-9-]+$/, "Must be lowercase alphanumeric with hyphens"),
  platform: z.nativeEnum(Platform, {
    required_error: "Platform is required",
  }),
  version: z.string().min(1, "Version is required"),
  profile: z.string().min(1, "Profile is required"),
  region: z.string().min(1, "Region is required"),
  base_domain: z.string().min(1, "Base domain is required"),
  owner: z.string().email("Invalid email address"),
  team: z.string().min(2, "Team name required (min 2 characters)"),
  cost_center: z.string().min(2, "Cost center required (min 2 characters)"),
  ttl_hours: z.number().int().min(0, "TTL must be 0 or greater (0 = never expires)").max(720),
  ssh_public_key: z.string().optional(),
  extra_tags: z.record(z.string()).optional(),
  offhours_opt_in: z.boolean().default(false),
  enable_efs_storage: z.boolean().default(false),
  override_work_hours: z.boolean().default(false),
  work_hours_enabled: z.boolean().optional(),
  work_hours_start: z.string().optional(),
  work_hours_end: z.string().optional(),
  work_days: z.array(z.string()).optional(),
});

export type CreateClusterFormData = z.infer<typeof createClusterSchema>;
