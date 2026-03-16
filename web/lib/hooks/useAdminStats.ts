import { useQuery } from "@tanstack/react-query";
import { adminApi } from "../api/endpoints/admin";

export function useClusterStatistics() {
  return useQuery({
    queryKey: ["admin", "cluster-statistics"],
    queryFn: () => adminApi.getClusterStatistics(),
    staleTime: 60 * 1000, // 1 minute
    refetchInterval: 60 * 1000, // Refresh every minute
  });
}
