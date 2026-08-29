import { apiClient } from "../client";
import type { BroadcastAlert, BroadcastAlertsResponse } from "@/types/api";

export interface CreateAlertRequest {
  title: string;
  body: string;
  severity: string;
  expiresAt?: string | null;
}

export const alertsApi = {
  // User-facing
  listActive: async (): Promise<BroadcastAlertsResponse> => {
    return apiClient.get<BroadcastAlertsResponse>(`/broadcast-alerts/active`);
  },

  acknowledge: async (id: string): Promise<void> => {
    return apiClient.post<void>(`/broadcast-alerts/${id}/ack`);
  },

  // Admin
  listAll: async (): Promise<BroadcastAlertsResponse> => {
    return apiClient.get<BroadcastAlertsResponse>(`/admin/broadcast-alerts`);
  },

  create: async (data: CreateAlertRequest): Promise<BroadcastAlert> => {
    return apiClient.post<BroadcastAlert>(`/admin/broadcast-alerts`, data);
  },

  deactivate: async (id: string): Promise<void> => {
    return apiClient.delete<void>(`/admin/broadcast-alerts/${id}`);
  },
};
