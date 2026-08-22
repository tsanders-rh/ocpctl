import { useQuery } from "@tanstack/react-query";
import { reportsApi } from "../api/endpoints/reports";

// useUsageReport fetches the platform-wide usage report for a date range.
// Enabled only once both dates are set.
export function useUsageReport(startDate: string, endDate: string) {
  return useQuery({
    queryKey: ["usage-report", startDate, endDate],
    queryFn: () => reportsApi.getUsageReport(startDate, endDate),
    enabled: Boolean(startDate) && Boolean(endDate),
    staleTime: 5 * 60 * 1000, // 5 minutes
  });
}
