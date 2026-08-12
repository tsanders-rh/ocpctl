import { apiClient } from "../client";
import type {
  ClusterTemplate,
  ClusterTemplatesResponse,
} from "@/types/api";

export const clusterTemplatesApi = {
  list: async (): Promise<ClusterTemplatesResponse> => {
    return apiClient.get<ClusterTemplatesResponse>(`/cluster-templates`);
  },

  get: async (id: string): Promise<ClusterTemplate> => {
    return apiClient.get<ClusterTemplate>(`/cluster-templates/${id}`);
  },

  create: async (data: {
    name: string;
    config: Record<string, unknown>;
  }): Promise<ClusterTemplate> => {
    return apiClient.post<ClusterTemplate>(`/cluster-templates`, data);
  },

  update: async (
    id: string,
    data: { name: string; config: Record<string, unknown> }
  ): Promise<ClusterTemplate> => {
    return apiClient.patch<ClusterTemplate>(`/cluster-templates/${id}`, data);
  },

  delete: async (id: string): Promise<void> => {
    return apiClient.delete<void>(`/cluster-templates/${id}`);
  },
};
