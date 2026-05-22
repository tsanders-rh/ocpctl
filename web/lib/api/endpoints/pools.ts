import { apiClient } from "../client";
import type {
  ClusterPool,
  PoolWithStats,
  CreatePoolRequest,
  UpdatePoolRequest,
  LeaseRequest,
  LeaseResponse,
  ListPoolsResponse,
  PoolStats,
} from "@/types/api";

export const poolsApi = {
  // Public: List enabled pools (all authenticated users)
  list: async (): Promise<ListPoolsResponse> => {
    return apiClient.get<ListPoolsResponse>("/pools?enabled_only=true");
  },

  // Admin: Pool Management
  listPools: async (): Promise<ListPoolsResponse> => {
    return apiClient.get<ListPoolsResponse>("/admin/pools");
  },

  getPool: async (name: string): Promise<PoolWithStats> => {
    return apiClient.get<PoolWithStats>(`/admin/pools/${encodeURIComponent(name)}`);
  },

  createPool: async (data: CreatePoolRequest): Promise<ClusterPool> => {
    return apiClient.post<ClusterPool>("/admin/pools", data);
  },

  updatePool: async (name: string, data: UpdatePoolRequest): Promise<ClusterPool> => {
    return apiClient.patch<ClusterPool>(`/admin/pools/${encodeURIComponent(name)}`, data);
  },

  deletePool: async (name: string): Promise<void> => {
    return apiClient.delete<void>(`/admin/pools/${encodeURIComponent(name)}`);
  },

  // CI/CD: Pool Lease/Release
  leaseCluster: async (poolName: string, data: LeaseRequest): Promise<LeaseResponse> => {
    return apiClient.post<LeaseResponse>(
      `/pools/${encodeURIComponent(poolName)}/lease`,
      data
    );
  },

  releaseCluster: async (clusterId: string): Promise<void> => {
    return apiClient.post<void>(`/pools/clusters/${clusterId}/release`, {});
  },

  getPoolStats: async (poolName: string): Promise<PoolStats> => {
    return apiClient.get<PoolStats>(`/pools/${encodeURIComponent(poolName)}/stats`);
  },
};
