import { useQuery } from "@tanstack/react-query";
import { poolsApi } from "../api";

export function usePools(options?: { refetchInterval?: number }) {
  return useQuery({
    queryKey: ["pools"],
    queryFn: () => poolsApi.list(),
    staleTime: 30 * 1000, // 30 seconds
    refetchInterval: options?.refetchInterval, // Optional polling interval
    refetchOnWindowFocus: true, // Refetch when user returns to tab
  });
}

export function usePoolStats(poolName: string, options?: { refetchInterval?: number }) {
  return useQuery({
    queryKey: ["pool-stats", poolName],
    queryFn: () => poolsApi.getPoolStats(poolName),
    enabled: !!poolName,
    staleTime: 10 * 1000, // 10 seconds (more frequent for real-time stats)
    refetchInterval: options?.refetchInterval, // Optional polling interval
    refetchOnWindowFocus: true, // Refetch when user returns to tab
  });
}
