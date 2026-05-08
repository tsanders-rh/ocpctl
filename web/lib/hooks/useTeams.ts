import { useQuery } from "@tanstack/react-query";
import { adminApi } from "../api/endpoints/admin";

export function useTeams() {
  return useQuery({
    queryKey: ["teams"],
    queryFn: () => adminApi.listTeams(),
    staleTime: 5 * 60 * 1000, // 5 minutes (teams don't change often)
  });
}
