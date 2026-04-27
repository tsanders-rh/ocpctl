import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { clustersApi, type ClusterFilters } from "../api/endpoints/clusters";
import type { CreateClusterRequest, ExtendClusterRequest } from "@/types/api";

export function useClusters(filters?: ClusterFilters) {
  return useQuery({
    queryKey: ["clusters", filters],
    queryFn: () => clustersApi.list(filters),
    staleTime: 30 * 1000, // 30 seconds
    refetchInterval: (query) => {
      // Poll every 10 seconds if any cluster is transitioning or post-deploying
      const data = query.state.data;
      if (!data?.data) return false;

      const transitioningStatuses = ["PENDING", "CREATING", "DESTROYING", "HIBERNATING", "RESUMING"];
      const hasTransitioningCluster = data.data.some(
        (cluster) => transitioningStatuses.includes(cluster.status)
      );
      const hasPostDeploying = data.data.some(
        (cluster) => cluster.post_deploy_status === "in_progress"
      );

      return (hasTransitioningCluster || hasPostDeploying) ? 10000 : false;
    },
  });
}

export function useCluster(id: string) {
  return useQuery({
    queryKey: ["cluster", id],
    queryFn: () => clustersApi.get(id),
    enabled: !!id,
    refetchInterval: (query) => {
      // Poll every 5 seconds if status is transitioning or post-deployment is active
      const data = query.state.data;
      if (!data) return false;
      const activeStatuses = ["PENDING", "CREATING", "DESTROYING", "HIBERNATING", "RESUMING"];
      const isTransitioning = activeStatuses.includes(data.status);
      const isPostDeploying = data.post_deploy_status === "in_progress";
      return (isTransitioning || isPostDeploying) ? 5000 : false;
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

export function useClusterOutputs(id: string, clusterStatus?: string, hasActivePostConfigureJob?: boolean) {
  return useQuery({
    queryKey: ["cluster", id, "outputs", clusterStatus],
    queryFn: () => clustersApi.getOutputs(id),
    enabled: !!id && (clusterStatus === "READY" || clusterStatus === "HIBERNATED"),
    refetchInterval: (query) => {
      // Poll every 3 seconds if cluster is READY but outputs aren't loaded yet
      // This handles the case where cluster just became READY and outputs are being written
      const data = query.state.data;
      if (clusterStatus === "READY" && !data) {
        return 3000;
      }
      // Keep polling every 5 seconds if POST_CONFIGURE job is active
      // This ensures dashboard URL/token appear automatically when job completes
      if (clusterStatus === "READY" && hasActivePostConfigureJob) {
        return 5000;
      }
      return false;
    },
  });
}

export function useHibernateCluster() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) => clustersApi.hibernate(id),
    onSuccess: (_, id) => {
      queryClient.invalidateQueries({ queryKey: ["cluster", id] });
      queryClient.invalidateQueries({ queryKey: ["clusters"] });
      queryClient.invalidateQueries({ queryKey: ["jobs"] });
    },
  });
}

export function useResumeCluster() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) => clustersApi.resume(id),
    onSuccess: (_, id) => {
      queryClient.invalidateQueries({ queryKey: ["cluster", id] });
      queryClient.invalidateQueries({ queryKey: ["clusters"] });
      queryClient.invalidateQueries({ queryKey: ["jobs"] });
    },
  });
}

export function useClusterConfigurations(id: string, clusterStatus?: string) {
  return useQuery({
    queryKey: ["cluster", id, "configurations"],
    queryFn: () => clustersApi.getConfigurations(id),
    enabled: !!id,
    refetchInterval: (query) => {
      // Poll every 5 seconds if:
      // 1. Cluster is READY (to detect newly created configurations)
      // 2. OR there are pending/installing configurations
      const data = query.state.data;

      // If cluster is READY, poll to detect new configurations
      if (clusterStatus === "READY") {
        return 5000;
      }

      // Otherwise, only poll if there are active configurations
      if (!data) return false;
      const hasActiveConfig = data.configurations.some(
        (config) => config.status === "pending" || config.status === "installing"
      );
      return hasActiveConfig ? 5000 : false;
    },
  });
}

export function useTriggerPostConfiguration() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) => clustersApi.triggerPostConfiguration(id),
    onSuccess: (_, id) => {
      queryClient.invalidateQueries({ queryKey: ["cluster", id, "configurations"] });
      queryClient.invalidateQueries({ queryKey: ["jobs"] });
    },
  });
}

export function useRetryConfiguration() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ clusterId, configId }: { clusterId: string; configId: string }) =>
      clustersApi.retryConfiguration(clusterId, configId),
    onSuccess: (_, { clusterId }) => {
      queryClient.invalidateQueries({ queryKey: ["cluster", clusterId, "configurations"] });
      queryClient.invalidateQueries({ queryKey: ["jobs"] });
    },
  });
}

export function useClusterInstances(id: string, platform?: string) {
  return useQuery({
    queryKey: ["cluster", id, "instances"],
    queryFn: () => clustersApi.getInstances(id),
    enabled: !!id && (platform === "aws" || platform === "gcp"), // Fetch for AWS and GCP clusters
    staleTime: 60 * 1000, // 60 seconds
  });
}

export function useClusterStorageClasses(id: string, clusterStatus?: string) {
  return useQuery({
    queryKey: ["cluster", id, "storage-classes"],
    queryFn: () => clustersApi.getStorageClasses(id),
    enabled: !!id && (clusterStatus === "READY" || clusterStatus === "HIBERNATED"),
    staleTime: 60 * 1000, // 60 seconds
  });
}
