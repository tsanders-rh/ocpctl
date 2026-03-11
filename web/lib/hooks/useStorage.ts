import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { storageApi, StorageGroupResponse } from "../api/endpoints/storage";

/**
 * Hook to fetch storage groups for a cluster
 * Polls every 5 seconds for PROVISIONING status
 */
export function useClusterStorage(clusterId: string | undefined) {
  return useQuery({
    queryKey: ["cluster", clusterId, "storage"],
    queryFn: () => storageApi.getStorage(clusterId!),
    enabled: !!clusterId,
    refetchInterval: (query) => {
      const data = query.state.data as StorageGroupResponse[] | undefined;
      // Poll every 5 seconds if any storage group is provisioning
      const hasProvisioning = data?.some(
        (sg) => sg.status === "PROVISIONING" || sg.status === "DELETING"
      );
      return hasProvisioning ? 5000 : false;
    },
  });
}

/**
 * Hook to link a cluster to another cluster for shared storage
 */
export function useLinkStorage() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({
      clusterId,
      targetClusterId,
    }: {
      clusterId: string;
      targetClusterId: string;
    }) => storageApi.linkToCluster(clusterId, targetClusterId),
    onSuccess: (_, { clusterId }) => {
      // Invalidate storage queries for the source cluster
      queryClient.invalidateQueries({
        queryKey: ["cluster", clusterId, "storage"],
      });
      // Also invalidate cluster detail in case storage_config changed
      queryClient.invalidateQueries({
        queryKey: ["cluster", clusterId],
      });
      // Invalidate jobs to show the new PROVISION_SHARED_STORAGE job
      queryClient.invalidateQueries({
        queryKey: ["jobs"],
      });
    },
  });
}

/**
 * Hook to unlink a cluster from a storage group
 */
export function useUnlinkStorage() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({
      clusterId,
      storageGroupId,
    }: {
      clusterId: string;
      storageGroupId: string;
    }) => storageApi.unlinkStorage(clusterId, storageGroupId),
    onSuccess: (_, { clusterId }) => {
      // Invalidate storage queries
      queryClient.invalidateQueries({
        queryKey: ["cluster", clusterId, "storage"],
      });
      // Also invalidate cluster detail
      queryClient.invalidateQueries({
        queryKey: ["cluster", clusterId],
      });
      // Invalidate jobs to show the new UNLINK_SHARED_STORAGE job
      queryClient.invalidateQueries({
        queryKey: ["jobs"],
      });
    },
  });
}
