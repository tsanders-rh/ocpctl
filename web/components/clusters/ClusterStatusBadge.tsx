import { Badge } from "@/components/ui/badge";
import { ClusterStatus } from "@/types/api";
import { Moon, Sunrise, Loader2 } from "lucide-react";

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
    [ClusterStatus.HIBERNATING]: "warning",
    [ClusterStatus.HIBERNATED]: "secondary",
    [ClusterStatus.RESUMING]: "warning",
    [ClusterStatus.DESTROYING]: "warning",
    [ClusterStatus.DESTROYED]: "secondary",
    [ClusterStatus.FAILED]: "destructive",
  };

  // Add icons for hibernation states
  const getIcon = () => {
    switch (status) {
      case ClusterStatus.HIBERNATED:
        return <Moon className="h-3 w-3 mr-1" />;
      case ClusterStatus.HIBERNATING:
        return <Loader2 className="h-3 w-3 mr-1 animate-spin" />;
      case ClusterStatus.RESUMING:
        return <Sunrise className="h-3 w-3 mr-1" />;
      default:
        return null;
    }
  };

  return (
    <Badge variant={variants[status]} className="flex items-center w-fit">
      {getIcon()}
      {status}
    </Badge>
  );
}
