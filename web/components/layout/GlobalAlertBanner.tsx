"use client";

import { useActiveAlerts, useAcknowledgeAlert } from "@/lib/hooks/useAlerts";
import { BroadcastAlertSeverity, type BroadcastAlert } from "@/types/api";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { AlertTriangle, Info, ShieldAlert } from "lucide-react";
import { cn } from "@/lib/utils/cn";

// GlobalAlertBanner renders broadcast alerts the current user hasn't acknowledged.
// Severity drives presentation: critical => blocking modal, info/warning => banner.
export function GlobalAlertBanner() {
  const { data } = useActiveAlerts();
  const acknowledge = useAcknowledgeAlert();

  const alerts = data?.alerts ?? [];
  if (alerts.length === 0) return null;

  // Alerts arrive sorted most-severe first, so the first critical is the one to show.
  const critical = alerts.find(
    (a) => a.severity === BroadcastAlertSeverity.CRITICAL
  );
  const banners = alerts.filter(
    (a) => a.severity !== BroadcastAlertSeverity.CRITICAL
  );

  const handleAck = (id: string) => acknowledge.mutate(id);

  // A critical alert takes over the screen; the user must acknowledge to proceed.
  // Acknowledging reveals the next critical alert (if any), then falls through to
  // banners/nothing on the next render.
  if (critical) {
    return (
      <Dialog open>
        <DialogContent
          className="max-w-lg [&>button]:hidden"
          onEscapeKeyDown={(e) => e.preventDefault()}
          onPointerDownOutside={(e) => e.preventDefault()}
          onInteractOutside={(e) => e.preventDefault()}
        >
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-destructive">
              <ShieldAlert className="h-5 w-5" />
              {critical.title}
            </DialogTitle>
            <DialogDescription className="whitespace-pre-wrap pt-2 text-foreground">
              {critical.body}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              onClick={() => handleAck(critical.id)}
              disabled={acknowledge.isPending}
            >
              {acknowledge.isPending ? "Acknowledging..." : "Acknowledge"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    );
  }

  // Otherwise render dismissible info/warning banners stacked above page content.
  return (
    <div className="mb-4 space-y-2">
      {banners.map((alert) => (
        <BannerRow
          key={alert.id}
          alert={alert}
          onAck={handleAck}
          pending={acknowledge.isPending}
        />
      ))}
    </div>
  );
}

function BannerRow({
  alert,
  onAck,
  pending,
}: {
  alert: BroadcastAlert;
  onAck: (id: string) => void;
  pending: boolean;
}) {
  const isWarning = alert.severity === BroadcastAlertSeverity.WARNING;
  const Icon = isWarning ? AlertTriangle : Info;
  return (
    <div
      role="alert"
      className={cn(
        "flex items-start gap-3 rounded-lg border p-4",
        isWarning
          ? "border-amber-300 bg-amber-50 text-amber-900 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-100"
          : "border-blue-300 bg-blue-50 text-blue-900 dark:border-blue-800 dark:bg-blue-950 dark:text-blue-100"
      )}
    >
      <Icon className="mt-0.5 h-5 w-5 flex-shrink-0" />
      <div className="flex-1">
        <p className="font-medium">{alert.title}</p>
        <p className="whitespace-pre-wrap text-sm">{alert.body}</p>
      </div>
      <Button
        size="sm"
        variant="outline"
        onClick={() => onAck(alert.id)}
        disabled={pending}
        className="flex-shrink-0"
      >
        Acknowledge
      </Button>
    </div>
  );
}
