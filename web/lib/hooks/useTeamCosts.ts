import { useQuery } from "@tanstack/react-query";
import { adminApi } from "../api/endpoints/admin";

export function useTeamCosts(teamName: string) {
  return useQuery({
    queryKey: ["team-costs", teamName],
    queryFn: () => adminApi.getTeamCosts(teamName),
    staleTime: 5 * 60 * 1000, // 5 minutes
    refetchInterval: 5 * 60 * 1000, // Refresh every 5 minutes
  });
}
