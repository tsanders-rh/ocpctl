import { apiClient } from "../client";
import type {
  Cluster,
  CreateClusterRequest,
  ExtendClusterRequest,
  PaginatedResponse,
  ClusterOutputs,
  DeploymentLogsResponse,
} from "@/types/api";

export interface ClusterFilters {
  platform?: string;
  profile?: string;
  owner?: string;
  team?: string;
  cost_center?: string;
  status?: string;
  page?: number;
  per_page?: number;
}

export const clustersApi = {
  list: async (
    filters?: ClusterFilters
  ): Promise<PaginatedResponse<Cluster>> => {
    const params = new URLSearchParams();

    if (filters) {
      Object.entries(filters).forEach(([key, value]) => {
        if (value !== undefined && value !== null && value !== "") {
          params.append(key, String(value));
        }
      });
    }

    const query = params.toString();
    return apiClient.get<PaginatedResponse<Cluster>>(
      `/clusters${query ? `?${query}` : ""}`
    );
  },

  get: async (id: string): Promise<Cluster> => {
    return apiClient.get<Cluster>(`/clusters/${id}`);
  },

  create: async (data: CreateClusterRequest): Promise<Cluster> => {
    return apiClient.post<Cluster>("/clusters", data);
  },

  delete: async (id: string): Promise<Cluster> => {
    return apiClient.delete<Cluster>(`/clusters/${id}`);
  },

  extend: async (id: string, data: ExtendClusterRequest): Promise<Cluster> => {
    return apiClient.patch<Cluster>(`/clusters/${id}/extend`, data);
  },

  getOutputs: async (id: string): Promise<ClusterOutputs> => {
    return apiClient.get<ClusterOutputs>(`/clusters/${id}/outputs`);
  },

  getDeploymentLogs: async (
    id: string,
    params?: {
      job_id?: string;
      after_sequence?: number;
      limit?: number;
    }
  ): Promise<DeploymentLogsResponse> => {
    const queryParams = new URLSearchParams();

    if (params?.job_id) {
      queryParams.append("job_id", params.job_id);
    }
    if (params?.after_sequence !== undefined) {
      queryParams.append("after_sequence", String(params.after_sequence));
    }
    if (params?.limit) {
      queryParams.append("limit", String(params.limit));
    }

    const query = queryParams.toString();
    return apiClient.get<DeploymentLogsResponse>(
      `/clusters/${id}/logs${query ? `?${query}` : ""}`
    );
  },
};
