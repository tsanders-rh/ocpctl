import { useQuery } from "@tanstack/react-query";
import { jobsApi, type JobFilters } from "../api/endpoints/jobs";

export function useJobs(filters?: JobFilters) {
  return useQuery({
    queryKey: ["jobs", filters],
    queryFn: () => jobsApi.list(filters),
    staleTime: 10 * 1000, // 10 seconds
  });
}

export function useJob(id: string) {
  return useQuery({
    queryKey: ["job", id],
    queryFn: () => jobsApi.get(id),
    enabled: !!id,
    refetchInterval: (query) => {
      // Poll every 5 seconds if status is PENDING or RUNNING
      const data = query.state.data;
      if (!data) return false;
      const activeStatuses = ["PENDING", "RUNNING", "RETRYING"];
      return activeStatuses.includes(data.status) ? 5000 : false;
    },
  });
}
