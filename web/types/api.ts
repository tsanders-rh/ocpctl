// API Type Definitions for ocpctl

export enum Platform {
  AWS = "aws",
  IBMCloud = "ibmcloud",
}

export enum ClusterType {
  OpenShift = "openshift",
  EKS = "eks",
  IKS = "iks",
}

export enum ClusterStatus {
  PENDING = "PENDING",
  CREATING = "CREATING",
  READY = "READY",
  HIBERNATING = "HIBERNATING",
  HIBERNATED = "HIBERNATED",
  RESUMING = "RESUMING",
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
  CONFIGURE_EFS = "CONFIGURE_EFS",
  PROVISION_SHARED_STORAGE = "PROVISION_SHARED_STORAGE",
  UNLINK_SHARED_STORAGE = "UNLINK_SHARED_STORAGE",
  HIBERNATE = "HIBERNATE",
  RESUME = "RESUME",
  POST_CONFIGURE = "POST_CONFIGURE",
}

export enum JobStatus {
  PENDING = "PENDING",
  RUNNING = "RUNNING",
  SUCCEEDED = "SUCCEEDED",
  FAILED = "FAILED",
  RETRYING = "RETRYING",
}

// User Types
export interface WorkHoursSchedule {
  start_time: string; // "09:00" format
  end_time: string; // "17:00" format
  work_days: string[]; // ["Monday", "Tuesday", ...]
}

export interface User {
  id: string;
  email: string;
  username: string;
  role: UserRole;
  timezone: string;
  work_hours_enabled: boolean;
  work_hours?: WorkHoursSchedule;
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
  timezone?: string;
  work_hours_enabled?: boolean;
  work_hours?: WorkHoursSchedule;
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
  new_password?: string;
}

// Cluster Types
export interface CreateClusterRequest {
  name: string;
  platform: Platform;
  cluster_type?: ClusterType;
  version: string;
  profile: string;
  region: string;
  base_domain?: string;
  owner: string;
  team: string;
  cost_center: string;
  ttl_hours?: number;
  ssh_public_key?: string;
  extra_tags?: Record<string, string>;
  offhours_opt_in?: boolean;
  work_hours_enabled?: boolean;
  work_hours?: WorkHoursSchedule;
  skip_post_deployment?: boolean;
  postConfigAddOns?: string[];
  customPostConfig?: CustomPostConfig;
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
  work_hours_enabled?: boolean | null; // NULL = use user default
  work_hours_start?: string;
  work_hours_end?: string;
  work_days?: number; // Bitmask
  last_work_hours_check?: string;
}

export interface ExtendClusterRequest {
  ttl_hours: number;
}

export interface ClusterOutputs {
  id: string;
  cluster_id: string;
  api_url?: string;
  console_url?: string;
  dashboard_token?: string; // Kubernetes Dashboard bearer token
  kubeconfig?: string; // Full kubeconfig content
  kubeconfig_s3_uri?: string;
  kubeadmin?: {
    username: string;
    password: string;
  };
  kubeadmin_secret_ref?: string;
  metadata_s3_uri?: string;
  created_at: string;
  updated_at: string;
}

// Profile Types
export interface PostDeploymentConfig {
  enabled: boolean;
  timeout?: string;
  operators?: OperatorConfig[];
  scripts?: ScriptConfig[];
  manifests?: ManifestConfig[];
  helm_charts?: HelmChartConfig[];
}

export interface OperatorConfig {
  name: string;
  namespace: string;
  source: string;
  channel: string;
  custom_resource?: CustomResourceConfig;
}

export interface ScriptConfig {
  name: string;
  path: string;
  description?: string;
  env?: Record<string, string>;
}

export interface CustomResourceConfig {
  api_version: string;
  kind: string;
  name: string;
  namespace?: string;
  spec?: Record<string, any>;
}

export interface ManifestConfig {
  name: string;
  path: string;
}

export interface HelmChartConfig {
  name: string;
  repo: string;
  chart: string;
  version?: string;
  namespace: string;
  values?: Record<string, any>;
}

// Profile Deployment Metrics
export interface ProfileDeploymentMetrics {
  profile: string;
  avg_duration_seconds: number;
  min_duration_seconds: number;
  max_duration_seconds: number;
  p50_duration_seconds?: number;
  p95_duration_seconds?: number;
  sample_count: number;
  success_count: number;
  last_deployment_at?: string;
  created_at: string;
  updated_at: string;
}

export interface Profile {
  name: string;
  display_name: string;
  description: string;
  platform: Platform;
  enabled: boolean;
  openshift_versions?: {
    allowed: string[];
    default: string;
  };
  kubernetes_versions?: {
    allowed: string[];
    default: string;
  };
  regions: {
    allowed: string[];
    default: string;
  };
  base_domains?: {
    allowed: string[];
    default: string;
  };
  compute: {
    control_plane?: {
      replicas: number;
      instance_type: string;
      schedulable: boolean;
    };
    workers?: {
      replicas: number;
      min_replicas: number;
      max_replicas: number;
      instance_type: string;
      autoscaling: boolean;
    };
    node_groups?: {
      name: string;
      instance_type: string;
      desired_capacity: number;
      min_size: number;
      max_size: number;
      volume_size?: number;
      volume_type?: string;
    }[];
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
    warning_message?: string;
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
  post_deployment?: PostDeploymentConfig;
  deployment_metrics?: ProfileDeploymentMetrics;
}

// Cluster Configuration Types
export enum ConfigType {
  OPERATOR = "operator",
  MANIFEST = "manifest",
  HELM = "helm",
}

export enum ConfigStatus {
  PENDING = "pending",
  INSTALLING = "installing",
  COMPLETED = "completed",
  FAILED = "failed",
}

export interface ClusterConfiguration {
  id: string;
  cluster_id: string;
  config_type: ConfigType;
  config_name: string;
  status: ConfigStatus;
  error_message?: string;
  created_at: string;
  completed_at?: string;
  metadata?: Record<string, any>;
}

export interface ClusterConfigurationsResponse {
  cluster_id: string;
  cluster_name: string;
  configurations: ClusterConfiguration[];
  total: number;
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

// Deployment Log Types
export interface DeploymentLog {
  id: number;
  cluster_id: string;
  job_id: string;
  sequence: number;
  timestamp: string;
  log_level?: string;
  message: string;
  source: string;
}

export interface DeploymentLogStats {
  total_lines: number;
  error_count: number;
  warn_count: number;
  last_updated: string;
}

export interface DeploymentLogsResponse {
  logs: DeploymentLog[];
  meta: {
    cluster_id: string;
    job_id: string;
    after_id?: number;
    after_sequence: number;
    limit: number;
    count: number;
    stats: DeploymentLogStats;
  };
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
export interface ValidationError {
  field: string;
  message: string;
}

export interface APIError {
  error: string;
  message: string;
  status_code: number;
  details?: ValidationError[];
}

// Custom Post-Config Types
export interface CustomOperatorConfig {
  name: string;
  namespace: string;
  source: string;
  channel: string;
  custom_resource?: CustomResourceConfig;
  variables?: Record<string, string>;
  condition?: string;
  dependsOn?: string[];
}

export interface CustomScriptConfig {
  name: string;
  content?: string;
  url?: string;
  description?: string;
  timeout?: string;
  env?: Record<string, string>;
  variables?: Record<string, string>;
  condition?: string;
  dependsOn?: string[];
}

export interface CustomManifestConfig {
  name: string;
  content?: string;
  url?: string;
  description?: string;
  variables?: Record<string, string>;
  condition?: string;
  dependsOn?: string[];
}

export interface CustomHelmChartConfig {
  name: string;
  repo: string;
  chart: string;
  version?: string;
  namespace: string;
  values?: Record<string, any>;
  variables?: Record<string, string>;
  condition?: string;
  dependsOn?: string[];
}

export interface CustomPostConfig {
  operators?: CustomOperatorConfig[];
  scripts?: CustomScriptConfig[];
  manifests?: CustomManifestConfig[];
  helmCharts?: CustomHelmChartConfig[];
}

// Post-Config Add-on Types
export interface PostConfigAddon {
  id: string;
  addonId: string;
  name: string;
  description: string;
  category: string;
  config: CustomPostConfig;
  supportedPlatforms: string[];
  enabled: boolean;
  createdAt: string;
  updatedAt: string;
}

export interface PostConfigAddonsResponse {
  addons: PostConfigAddon[];
  categories: Record<string, PostConfigAddon[]>;
  total: number;
}

// Post-Config Template Types
export interface PostConfigTemplate {
  id: string;
  name: string;
  description: string;
  config: CustomPostConfig;
  ownerId: string;
  isPublic: boolean;
  tags: string[];
  createdAt: string;
  updatedAt: string;
}

export interface PostConfigTemplatesResponse {
  templates: PostConfigTemplate[];
}

// Post-Config Validation Types
export interface DAGInfo {
  executionOrder: string[];
  taskCount: number;
  dependencies: Record<string, string[]>;
}

export interface ValidatePostConfigResponse {
  valid: boolean;
  errors?: string[];
  dag?: DAGInfo;
}
