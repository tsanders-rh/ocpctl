import { apiClient } from "../client";

export interface ClusterStatistics {
  total_clusters: number;
  active_clusters: number;
  clusters_by_status: Array<{
    status: string;
    count: number;
  }>;
  clusters_by_profile: Array<{
    profile: string;
    count: number;
  }>;
  total_hourly_cost: number;
  total_daily_cost: number;
  total_monthly_cost: number;
  cost_by_profile: Array<{
    profile: string;
    cluster_count: number;
    hourly_cost: number;
    daily_cost: number;
    monthly_cost: number;
  }>;
  cost_by_user: Array<{
    user_id: string;
    username: string;
    cluster_count: number;
    hourly_cost: number;
    daily_cost: number;
    monthly_cost: number;
  }>;
}

export const adminApi = {
  getClusterStatistics: async (): Promise<ClusterStatistics> => {
    return apiClient.get<ClusterStatistics>("/admin/clusters/statistics");
  },
};
