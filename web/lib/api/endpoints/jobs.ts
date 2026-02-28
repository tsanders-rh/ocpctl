import { apiClient } from "../client";
import type { Job, PaginatedResponse } from "@/types/api";

export interface JobFilters {
  cluster_id?: string;
  job_type?: string;
  status?: string;
  page?: number;
  per_page?: number;
}

export const jobsApi = {
  list: async (filters?: JobFilters): Promise<PaginatedResponse<Job>> => {
    const params = new URLSearchParams();

    if (filters) {
      Object.entries(filters).forEach(([key, value]) => {
        if (value !== undefined && value !== null && value !== "") {
          params.append(key, String(value));
        }
      });
    }

    const query = params.toString();
    return apiClient.get<PaginatedResponse<Job>>(
      `/jobs${query ? `?${query}` : ""}`
    );
  },

  get: async (id: string): Promise<Job> => {
    return apiClient.get<Job>(`/jobs/${id}`);
  },
};
