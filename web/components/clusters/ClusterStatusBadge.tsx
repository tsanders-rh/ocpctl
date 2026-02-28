import { Badge } from "@/components/ui/badge";
import { ClusterStatus } from "@/types/api";

interface ClusterStatusBadgeProps {
  status: ClusterStatus;
}

export function ClusterStatusBadge({ status }: ClusterStatusBadgeProps) {
  const variants: Record<
    ClusterStatus,
    "default" | "success" | "warning" | "destructive" | "secondary"
  > = {
    [ClusterStatus.PENDING]: "warning",
    [ClusterStatus.CREATING]: "info" as any,
    [ClusterStatus.READY]: "success",
    [ClusterStatus.DESTROYING]: "warning",
    [ClusterStatus.DESTROYED]: "secondary",
    [ClusterStatus.FAILED]: "destructive",
  };

  return <Badge variant={variants[status]}>{status}</Badge>;
}
