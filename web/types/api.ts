// API Type Definitions for ocpctl

export enum Platform {
  AWS = "aws",
  IBMCloud = "ibmcloud",
}

export enum ClusterStatus {
  PENDING = "PENDING",
  CREATING = "CREATING",
  READY = "READY",
  DESTROYING = "DESTROYING",
  DESTROYED = "DESTROYED",
  FAILED = "FAILED",
}

export enum UserRole {
  ADMIN = "ADMIN",
  USER = "USER",
  VIEWER = "VIEWER",
}

export enum JobType {
  CREATE = "CREATE",
  DESTROY = "DESTROY",
  SCALE_WORKERS = "SCALE_WORKERS",
  JANITOR_DESTROY = "JANITOR_DESTROY",
  ORPHAN_SWEEP = "ORPHAN_SWEEP",
}

export enum JobStatus {
  PENDING = "PENDING",
  RUNNING = "RUNNING",
  SUCCEEDED = "SUCCEEDED",
  FAILED = "FAILED",
  RETRYING = "RETRYING",
}

// User Types
export interface User {
  id: string;
  email: string;
  username: string;
  role: UserRole;
  active: boolean;
  created_at: string;
  updated_at: string;
}

export interface LoginRequest {
  email: string;
  password: string;
}

export interface LoginResponse {
  user: User;
  access_token: string;
  expires_in: number;
}

export interface ChangePasswordRequest {
  current_password: string;
  new_password: string;
}

export interface UpdateMeRequest {
  username?: string;
}

export interface CreateUserRequest {
  email: string;
  username: string;
  password: string;
  role: UserRole;
}

export interface UpdateUserRequest {
  username?: string;
  role?: UserRole;
  active?: boolean;
}

// Cluster Types
export interface CreateClusterRequest {
  name: string;
  platform: Platform;
  version: string;
  profile: string;
  region: string;
  base_domain: string;
  owner: string;
  team: string;
  cost_center: string;
  ttl_hours?: number;
  ssh_public_key?: string;
  extra_tags?: Record<string, string>;
  offhours_opt_in?: boolean;
  idempotency_key?: string;
}

export interface Cluster {
  id: string;
  name: string;
  platform: Platform;
  version: string;
  profile: string;
  region: string;
  base_domain: string;
  owner: string;
  owner_id: string;
  team: string;
  cost_center: string;
  status: ClusterStatus;
  requested_by: string;
  ttl_hours: number;
  destroy_at: string | null;
  created_at: string;
  updated_at: string;
  destroyed_at: string | null;
  request_tags: Record<string, string>;
  effective_tags: Record<string, string>;
  ssh_public_key?: string;
  offhours_opt_in: boolean;
}

export interface ExtendClusterRequest {
  ttl_hours: number;
}

export interface ClusterOutputs {
  id: string;
  cluster_id: string;
  api_url?: string;
  console_url?: string;
  kubeconfig_s3_uri?: string;
  kubeadmin_secret_ref?: string;
  metadata_s3_uri?: string;
  created_at: string;
  updated_at: string;
}

// Profile Types
export interface Profile {
  name: string;
  display_name: string;
  description: string;
  platform: Platform;
  enabled: boolean;
  openshift_versions: {
    allowed: string[];
    default: string;
  };
  regions: {
    allowed: string[];
    default: string;
  };
  base_domains: {
    allowed: string[];
    default: string;
  };
  compute: {
    control_plane: {
      replicas: number;
      instance_type: string;
      schedulable: boolean;
    };
    workers: {
      replicas: number;
      min_replicas: number;
      max_replicas: number;
      instance_type: string;
      autoscaling: boolean;
    };
  };
  lifecycle: {
    max_ttl_hours: number;
    default_ttl_hours: number;
    allow_custom_ttl: boolean;
    warn_before_destroy_hours: number;
  };
  cost_controls?: {
    estimated_hourly_cost: number;
    max_monthly_cost: number;
    budget_alert_threshold: number;
  };
  features: {
    off_hours_scaling: boolean;
    fips_mode: boolean;
    private_cluster: boolean;
  };
  tags: {
    required: Record<string, string[]>;
    defaults: Record<string, string>;
    allow_user_tags: boolean;
  };
  platform_config?: {
    aws?: Record<string, any>;
    ibmcloud?: Record<string, any>;
  };
}

// Job Types
export interface Job {
  id: string;
  cluster_id: string;
  job_type: JobType;
  status: JobStatus;
  attempt: number;
  max_attempts: number;
  error_code?: string;
  error_message?: string;
  started_at?: string;
  ended_at?: string;
  created_at: string;
  updated_at: string;
  metadata: Record<string, any>;
}

// Pagination
export interface PaginatedResponse<T> {
  data: T[];
  pagination: {
    page: number;
    per_page: number;
    total: number;
    total_pages: number;
  };
  filters?: Record<string, any>;
}

// API Error
export interface APIError {
  error: string;
  message: string;
  status_code: number;
}
