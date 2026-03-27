import { useQuery } from "@tanstack/react-query";
import { clustersApi } from "../api/endpoints/clusters";
import type { DeploymentLogsResponse } from "@/types/api";

interface UseDeploymentLogsOptions {
  jobId?: string;
  afterId?: number;
  afterSequence?: number; // Deprecated - kept for backwards compatibility
  limit?: number;
  refreshInterval?: number | false;
}

export function useDeploymentLogs(
  clusterId: string,
  options?: UseDeploymentLogsOptions
) {
  return useQuery<DeploymentLogsResponse>({
    queryKey: ["deployment-logs", clusterId, options?.jobId, options?.afterId],
    queryFn: () =>
      clustersApi.getDeploymentLogs(clusterId, {
        job_id: options?.jobId,
        after_id: options?.afterId,
        after_sequence: options?.afterSequence,
        limit: options?.limit,
      }),
    enabled: !!clusterId,
    refetchInterval: options?.refreshInterval ?? false,
    staleTime: 0, // Always fetch latest logs
  });
}
