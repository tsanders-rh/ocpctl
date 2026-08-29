import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { alertsApi } from "../api";
import type { CreateAlertRequest } from "../api/endpoints/alerts";

// useActiveAlerts polls for broadcast alerts the current user has not acknowledged.
export function useActiveAlerts() {
  return useQuery({
    queryKey: ["alerts", "active"],
    queryFn: () => alertsApi.listActive(),
    refetchInterval: 30000, // 30s
    staleTime: 15 * 1000,
  });
}

export function useAcknowledgeAlert() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => alertsApi.acknowledge(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["alerts"] });
    },
  });
}

// Admin hooks
export function useAllAlerts() {
  return useQuery({
    queryKey: ["alerts", "all"],
    queryFn: () => alertsApi.listAll(),
    staleTime: 15 * 1000,
  });
}

export function useCreateAlert() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateAlertRequest) => alertsApi.create(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["alerts"] });
    },
  });
}

export function useDeactivateAlert() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => alertsApi.deactivate(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["alerts"] });
    },
  });
}
