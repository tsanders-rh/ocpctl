import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  orphanedResourcesApi,
  OrphanedResourceFilters,
} from "../api/endpoints/orphaned-resources";

/**
 * Hook to fetch orphaned resources list with filters
 */
export function useOrphanedResources(filters?: OrphanedResourceFilters) {
  return useQuery({
    queryKey: ["orphaned-resources", filters],
    queryFn: () => orphanedResourcesApi.list(filters),
  });
}

/**
 * Hook to fetch orphaned resources statistics
 */
export function useOrphanedResourcesStats() {
  return useQuery({
    queryKey: ["orphaned-resources", "stats"],
    queryFn: () => orphanedResourcesApi.getStats(),
    refetchInterval: 30000, // Refetch every 30 seconds for up-to-date stats
  });
}

/**
 * Hook to mark an orphaned resource as resolved
 */
export function useMarkResourceResolved() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ id, notes }: { id: string; notes: string }) =>
      orphanedResourcesApi.markResolved(id, notes),
    onSuccess: () => {
      // Invalidate both list and stats queries
      queryClient.invalidateQueries({
        queryKey: ["orphaned-resources"],
      });
    },
  });
}

/**
 * Hook to mark an orphaned resource as ignored
 */
export function useMarkResourceIgnored() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ id, notes }: { id: string; notes: string }) =>
      orphanedResourcesApi.markIgnored(id, notes),
    onSuccess: () => {
      // Invalidate both list and stats queries
      queryClient.invalidateQueries({
        queryKey: ["orphaned-resources"],
      });
    },
  });
}

/**
 * Hook to delete an orphaned resource (currently only supports HostedZone)
 */
export function useDeleteResource() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) => orphanedResourcesApi.deleteResource(id),
    onSuccess: () => {
      // Invalidate both list and stats queries
      queryClient.invalidateQueries({
        queryKey: ["orphaned-resources"],
      });
    },
  });
}
