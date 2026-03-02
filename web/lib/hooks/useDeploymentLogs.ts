import { useQuery } from "@tanstack/react-query";
import { clustersApi } from "../api/endpoints/clusters";
import type { DeploymentLogsResponse } from "@/types/api";

interface UseDeploymentLogsOptions {
  jobId?: string;
  afterSequence?: number;
  limit?: number;
  refreshInterval?: number | false;
}

export function useDeploymentLogs(
  clusterId: string,
  options?: UseDeploymentLogsOptions
) {
  return useQuery<DeploymentLogsResponse>({
    queryKey: ["deployment-logs", clusterId, options?.jobId, options?.afterSequence],
    queryFn: () =>
      clustersApi.getDeploymentLogs(clusterId, {
        job_id: options?.jobId,
        after_sequence: options?.afterSequence,
        limit: options?.limit,
      }),
    enabled: !!clusterId,
    refetchInterval: options?.refreshInterval ?? false,
    staleTime: 0, // Always fetch latest logs
  });
}
