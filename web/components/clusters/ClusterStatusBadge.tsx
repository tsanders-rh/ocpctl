import { Badge } from "@/components/ui/badge";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { ClusterStatus } from "@/types/api";
import { Moon, Sunrise, Loader2 } from "lucide-react";

interface ClusterStatusBadgeProps {
  status: ClusterStatus;
}

const statusDescriptions: Record<ClusterStatus, string> = {
  [ClusterStatus.PENDING]: "Cluster creation queued, waiting for worker to pick up the job",
  [ClusterStatus.CREATING]: "Infrastructure being provisioned by cloud provider",
  [ClusterStatus.READY]: "Cluster is active and ready to use",
  [ClusterStatus.HIBERNATING]: "Cluster is being stopped to save costs",
  [ClusterStatus.HIBERNATED]: "Cluster is stopped (outside work hours or manually hibernated)",
  [ClusterStatus.RESUMING]: "Cluster is starting up from hibernation",
  [ClusterStatus.DESTROYING]: "Cluster resources being deleted from cloud provider",
  [ClusterStatus.DESTROYED]: "Cluster has been permanently destroyed",
  [ClusterStatus.FAILED]: "Cluster creation or operation failed - check logs for details",
};

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
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>
          <Badge variant={variants[status]} className="flex items-center w-fit cursor-help">
            {getIcon()}
            {status}
          </Badge>
        </TooltipTrigger>
        <TooltipContent>
          <p className="max-w-xs">{statusDescriptions[status]}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
