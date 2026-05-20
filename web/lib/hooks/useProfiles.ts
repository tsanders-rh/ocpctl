import { useQuery } from "@tanstack/react-query";
import { profilesApi } from "../api";
import type { Platform } from "@/types/api";

export function useProfiles(platform?: Platform, track?: string, options?: { refetchInterval?: number }) {
  return useQuery({
    queryKey: ["profiles", platform, track],
    queryFn: () => profilesApi.list(platform, track),
    staleTime: 30 * 1000, // 30 seconds (allow quick updates to propagate)
    refetchInterval: options?.refetchInterval, // Optional polling interval
    refetchOnWindowFocus: true, // Refetch when user returns to tab
  });
}

export function useProfile(name: string, options?: { refetchInterval?: number }) {
  return useQuery({
    queryKey: ["profile", name],
    queryFn: () => profilesApi.get(name),
    enabled: !!name,
    staleTime: 30 * 1000, // 30 seconds
    refetchInterval: options?.refetchInterval, // Optional polling interval
    refetchOnWindowFocus: true, // Refetch when user returns to tab
  });
}
