import { apiClient } from "../client";
import type {
  APIKey,
  CreateAPIKeyRequest,
  CreateAPIKeyResponse,
  UpdateAPIKeyRequest,
} from "@/types/api";

export const apiKeysApi = {
  list: async (): Promise<APIKey[]> => {
    return apiClient.get<APIKey[]>("/api-keys");
  },

  create: async (data: CreateAPIKeyRequest): Promise<CreateAPIKeyResponse> => {
    return apiClient.post<CreateAPIKeyResponse>("/api-keys", data);
  },

  update: async (id: string, data: UpdateAPIKeyRequest): Promise<APIKey> => {
    return apiClient.patch<APIKey>(`/api-keys/${id}`, data);
  },

  revoke: async (id: string): Promise<void> => {
    return apiClient.post<void>(`/api-keys/${id}/revoke`);
  },

  delete: async (id: string): Promise<void> => {
    return apiClient.delete<void>(`/api-keys/${id}`);
  },
};
