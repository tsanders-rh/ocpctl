import { useQuery } from "@tanstack/react-query";
import { profilesApi } from "../api";
import type { Platform } from "@/types/api";

export function useProfiles(platform?: Platform, track?: string) {
  return useQuery({
    queryKey: ["profiles", platform, track],
    queryFn: () => profilesApi.list(platform, track),
    staleTime: 5 * 60 * 1000, // 5 minutes (profiles don't change often)
  });
}

export function useProfile(name: string) {
  return useQuery({
    queryKey: ["profile", name],
    queryFn: () => profilesApi.get(name),
    enabled: !!name,
    staleTime: 5 * 60 * 1000, // 5 minutes
  });
}
