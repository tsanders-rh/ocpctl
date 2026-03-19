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

export interface WorkerInfo {
  instance_id: string;
  private_ip: string;
  public_ip?: string;
  launch_time: string;
  state: string;
  health_status: string;
  type: string;
  version?: string;
}

export interface ASGInfo {
  name: string;
  desired_capacity: number;
  min_size: number;
  max_size: number;
  instances: WorkerInfo[];
}

export interface InfrastructureInfo {
  api_server: {
    ip: string;
    version: string;
    status: string;
    uptime?: string;
  };
  database: {
    host: string;
    status: string;
  };
  static_workers: WorkerInfo[];
  autoscale_group?: ASGInfo;
  timestamp: string;
}

export const adminApi = {
  getClusterStatistics: async (): Promise<ClusterStatistics> => {
    return apiClient.get<ClusterStatistics>("/admin/clusters/statistics");
  },
  getInfrastructure: async (): Promise<InfrastructureInfo> => {
    return apiClient.get<InfrastructureInfo>("/admin/system/infrastructure");
  },
};
