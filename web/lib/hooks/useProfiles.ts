import { useQuery } from "@tanstack/react-query";
import { profilesApi } from "../api";
import type { Platform } from "@/types/api";

export function useProfiles(platform?: Platform, track?: string) {
  return useQuery({
    queryKey: ["profiles", platform, track],
    queryFn: () => profilesApi.list(platform, track),
    staleTime: 30 * 1000, // 30 seconds (allow quick updates to propagate)
  });
}

export function useProfile(name: string) {
  return useQuery({
    queryKey: ["profile", name],
    queryFn: () => profilesApi.get(name),
    enabled: !!name,
    staleTime: 30 * 1000, // 30 seconds
  });
}
