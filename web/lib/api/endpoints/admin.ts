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
}

export const adminApi = {
  getClusterStatistics: async (): Promise<ClusterStatistics> => {
    return apiClient.get<ClusterStatistics>("/admin/clusters/statistics");
  },
};
