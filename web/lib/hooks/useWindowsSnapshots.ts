import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  windowsSnapshotsApi,
  type WindowsSnapshotFilters,
  type CreateWindowsSnapshotRequest,
} from "../api/endpoints/windows-snapshots";

const QUERY_KEYS = {
  snapshots: (filters?: WindowsSnapshotFilters) =>
    ["windows-snapshots", filters] as const,
  snapshot: (id: string) => ["windows-snapshots", id] as const,
  coverage: (version?: string) => ["windows-snapshots", "coverage", version] as const,
};

/**
 * Hook to fetch list of Windows snapshots
 */
export function useWindowsSnapshots(filters?: WindowsSnapshotFilters) {
  return useQuery({
    queryKey: QUERY_KEYS.snapshots(filters),
    queryFn: () => windowsSnapshotsApi.list(filters),
    refetchInterval: (data) => {
      // Poll every 10 seconds if there are snapshots in progress
      const hasInProgress = data?.snapshots?.some(
        (s) => s.status === "creating" || s.status === "validating" || s.status === "deleting"
      );
      return hasInProgress ? 10000 : false; // 10 seconds when in progress, disabled otherwise
    },
  });
}

/**
 * Hook to fetch a single Windows snapshot
 */
export function useWindowsSnapshot(id: string) {
  return useQuery({
    queryKey: QUERY_KEYS.snapshot(id),
    queryFn: () => windowsSnapshotsApi.get(id),
    enabled: !!id,
  });
}

/**
 * Hook to fetch Windows snapshot coverage statistics
 */
export function useWindowsSnapshotCoverage(version?: string) {
  return useQuery({
    queryKey: QUERY_KEYS.coverage(version),
    queryFn: () => windowsSnapshotsApi.getCoverage(version),
    refetchInterval: (data) => {
      // Poll every 10 seconds if there are incomplete snapshots
      const hasInProgress = data?.snapshots_by_region
        ? Object.values(data.snapshots_by_region).some(
            (s) => s.status === "creating" || s.status === "validating" || s.status === "deleting"
          )
        : false;
      return hasInProgress ? 10000 : 30000; // 10 seconds when in progress, 30 seconds otherwise
    },
  });
}

/**
 * Hook to create a new Windows snapshot
 */
export function useCreateWindowsSnapshot() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (request: CreateWindowsSnapshotRequest) =>
      windowsSnapshotsApi.create(request),
    onSuccess: () => {
      // Invalidate snapshots list and coverage to refetch
      queryClient.invalidateQueries({ queryKey: ["windows-snapshots"] });
    },
  });
}

/**
 * Hook to delete a Windows snapshot
 */
export function useDeleteWindowsSnapshot() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) => windowsSnapshotsApi.delete(id),
    onSuccess: () => {
      // Invalidate snapshots list and coverage to refetch
      queryClient.invalidateQueries({ queryKey: ["windows-snapshots"] });
    },
  });
}
