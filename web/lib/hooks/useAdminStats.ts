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

export function useInfrastructure() {
  return useQuery({
    queryKey: ["admin", "infrastructure"],
    queryFn: () => adminApi.getInfrastructure(),
    staleTime: 10 * 1000, // 10 seconds
    refetchInterval: 10 * 1000, // Refresh every 10 seconds
  });
}
