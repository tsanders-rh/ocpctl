import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { clustersApi, type ClusterFilters } from "../api/endpoints/clusters";
import type { CreateClusterRequest, ExtendClusterRequest } from "@/types/api";

export function useClusters(filters?: ClusterFilters) {
  return useQuery({
    queryKey: ["clusters", filters],
    queryFn: () => clustersApi.list(filters),
    staleTime: 30 * 1000, // 30 seconds
  });
}

export function useCluster(id: string) {
  return useQuery({
    queryKey: ["cluster", id],
    queryFn: () => clustersApi.get(id),
    enabled: !!id,
    refetchInterval: (query) => {
      // Poll every 5 seconds if status is PENDING, CREATING, or DESTROYING
      const data = query.state.data;
      if (!data) return false;
      const activeStatuses = ["PENDING", "CREATING", "DESTROYING"];
      return activeStatuses.includes(data.status) ? 5000 : false;
    },
  });
}

export function useCreateCluster() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: CreateClusterRequest) => clustersApi.create(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["clusters"] });
    },
  });
}

export function useDeleteCluster() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) => clustersApi.delete(id),
    onSuccess: (_, id) => {
      queryClient.invalidateQueries({ queryKey: ["clusters"] });
      queryClient.invalidateQueries({ queryKey: ["cluster", id] });
    },
  });
}

export function useExtendCluster() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: ExtendClusterRequest }) =>
      clustersApi.extend(id, data),
    onSuccess: (_, { id }) => {
      queryClient.invalidateQueries({ queryKey: ["cluster", id] });
      queryClient.invalidateQueries({ queryKey: ["clusters"] });
    },
  });
}

export function useClusterOutputs(id: string) {
  return useQuery({
    queryKey: ["cluster", id, "outputs"],
    queryFn: () => clustersApi.getOutputs(id),
    enabled: !!id,
  });
}
