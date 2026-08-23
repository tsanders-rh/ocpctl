import { apiClient } from "../client";

// Mirrors pkg/types/report.go

export interface UsageCostSummary {
  total_cost: number;
  total_runtime_hours: number;
  clusters_active: number;
  prior_period_comparison?: {
    current_period: number;
    previous_period: number;
    percent_change: number;
    start_date: string;
    end_date: string;
  };
}

export interface ClusterUsage {
  name: string;
  owner: string;
  region: string;
  status: string;
  cluster_type: string;
  created_at: string;
  destroyed_at?: string;
  runtime_hours: number;
  estimated_cost: number;
}

export interface ProfileUsage {
  profile: string;
  cluster_count: number;
  runtime_hours: number;
  estimated_cost: number;
  clusters: ClusterUsage[];
}

export interface UserUsage {
  owner: string;
  cluster_count: number;
  runtime_hours: number;
  estimated_cost: number;
}

export interface VersionUsage {
  version: string;
  cluster_count: number;
  runtime_hours: number;
  estimated_cost: number;
}

export interface AddonUsage {
  addon: string;
  cluster_count: number;
  runtime_hours: number;
  estimated_cost: number;
}

export interface LifecycleStats {
  created: number;
  destroyed: number;
  hibernated: number;
  create_success: number;
  create_failure: number;
  create_success_rate: number;
  avg_lifetime_hours: number;
  by_platform: Record<string, number>;
  by_cluster_type: Record<string, number>;
  by_status: Record<string, number>;
}

export interface UsageReport {
  start_date: string;
  end_date: string;
  generated_at: string;
  cost: UsageCostSummary;
  profiles: ProfileUsage[];
  users: UserUsage[];
  versions: VersionUsage[];
  addons: AddonUsage[];
  lifecycle: LifecycleStats;
}

export const reportsApi = {
  getUsageReport: async (startDate: string, endDate: string): Promise<UsageReport> => {
    const params = new URLSearchParams({ start_date: startDate, end_date: endDate });
    return apiClient.get<UsageReport>(`/admin/reports/usage?${params.toString()}`);
  },
};
