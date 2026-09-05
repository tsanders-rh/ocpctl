"use client";

import { useEffect, useState } from "react";
import {
  useAutoRemediation,
  useUpdateAutoRemediation,
} from "@/lib/hooks/useOrphanedResources";
import type { AutoRemediationMode } from "@/lib/api/endpoints/orphaned-resources";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { ShieldCheck, AlertTriangle, Save } from "lucide-react";
import { formatDistanceToNow } from "date-fns";

const MODE_LABEL: Record<string, string> = {
  off: "Off",
  dryrun: "Dry-run (audit only)",
  on: "On (delete)",
};

function modeBadge(mode: string) {
  switch (mode) {
    case "on":
      return <Badge variant="destructive">On (deleting)</Badge>;
    case "dryrun":
      return (
        <Badge variant="default" className="bg-amber-600">
          Dry-run
        </Badge>
      );
    default:
      return <Badge variant="outline">Off</Badge>;
  }
}

export function AutoRemediationCard() {
  const { data, isLoading } = useAutoRemediation();
  const update = useUpdateAutoRemediation();

  const [mode, setMode] = useState<AutoRemediationMode>("off");
  const [maxPerCycle, setMaxPerCycle] = useState<number>(20);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [confirmText, setConfirmText] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [savedAt, setSavedAt] = useState<number | null>(null);

  // Sync the form to the server value once it loads (only when settings exist).
  useEffect(() => {
    if (data?.settings) {
      setMode(data.settings.mode);
      setMaxPerCycle(data.settings.maxPerCycle);
    }
  }, [data?.settings]);

  const status = data?.status ?? null;

  const save = async () => {
    setError(null);
    try {
      await update.mutateAsync({ mode, maxPerCycle });
      setSavedAt(Date.now());
      setConfirmOpen(false);
      setConfirmText("");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to save settings");
    }
  };

  const handleSaveClick = () => {
    setError(null);
    if (maxPerCycle < 1 || maxPerCycle > 500) {
      setError("Max deletions per cycle must be between 1 and 500");
      return;
    }
    // Require a typed confirmation only when ARMING real deletion.
    const wasOn = data?.settings?.mode === "on";
    if (mode === "on" && !wasOn) {
      setConfirmOpen(true);
      return;
    }
    save();
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <ShieldCheck className="h-5 w-5" />
          Automated Remediation
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-6">
        <p className="text-sm text-muted-foreground">
          Controls whether the janitor automatically deletes orphaned resources.
          Only resources that carry a verified <span className="font-mono">ocpctl</span>{" "}
          ownership tag and pass every safety check (grace period, no live source
          cluster, empty VPC) are ever eligible. Changes take effect within one
          janitor cycle (~15&nbsp;min) &mdash; no restart required.
        </p>

        {!isLoading && !data?.configured && (
          <div className="rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800">
            Not yet configured in the console. The worker&rsquo;s environment
            default is currently in effect
            {status ? (
              <>
                {" "}
                (last cycle ran as <span className="font-medium">{MODE_LABEL[status.mode] ?? status.mode}</span>)
              </>
            ) : null}
            . Saving here makes the console the source of truth.
          </div>
        )}

        <div className="flex flex-col gap-4 sm:flex-row sm:items-end">
          <div className="flex-1">
            <Label>Mode</Label>
            <Select value={mode} onValueChange={(v) => setMode(v as AutoRemediationMode)}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="off">Off &mdash; detect only</SelectItem>
                <SelectItem value="dryrun">Dry-run &mdash; audit what would be deleted</SelectItem>
                <SelectItem value="on">On &mdash; actually delete</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="w-full sm:w-48">
            <Label>Max deletions / cycle</Label>
            <Input
              type="number"
              min={1}
              max={500}
              value={maxPerCycle}
              onChange={(e) => setMaxPerCycle(parseInt(e.target.value, 10) || 0)}
            />
          </div>

          <Button onClick={handleSaveClick} disabled={update.isPending}>
            <Save className="mr-1 h-4 w-4" />
            {update.isPending ? "Saving..." : "Save"}
          </Button>
        </div>

        {mode === "on" && (
          <div className="flex items-start gap-2 rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
            <span>
              In <span className="font-medium">On</span> mode the janitor will
              permanently delete gate-passing orphaned resources from the cloud
              provider, up to {maxPerCycle || 0} per cycle.
            </span>
          </div>
        )}

        {error && (
          <div className="rounded-md border border-red-200 bg-red-50 p-3 text-sm text-red-800">
            {error}
          </div>
        )}

        {savedAt && !error && !update.isPending && (
          <div className="text-sm text-green-700">Settings saved.</div>
        )}

        {/* Last-cycle status */}
        <div className="border-t pt-4">
          <div className="mb-3 flex items-center justify-between">
            <h4 className="text-sm font-medium">Last cycle</h4>
            {status ? (
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                {modeBadge(status.mode)}
                <span>
                  {formatDistanceToNow(new Date(status.lastRunAt), { addSuffix: true })}
                </span>
              </div>
            ) : null}
          </div>
          {!status ? (
            <p className="text-sm text-muted-foreground">
              No auto-remediation cycle has run yet.
            </p>
          ) : (
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-6">
              <StatCell label="Evaluated" value={status.evaluated} />
              <StatCell label="Would delete" value={status.wouldDelete} />
              <StatCell label="Deleted" value={status.deleted} highlight={status.deleted > 0} />
              <StatCell label="Failed" value={status.failed} danger={status.failed > 0} />
              <StatCell label="Skipped (unsafe)" value={status.skippedUnsafe} />
              <StatCell label="Skipped (unowned)" value={status.skippedUnowned} />
            </div>
          )}
          {status?.capped && (
            <p className="mt-3 text-xs text-amber-700">
              Hit the per-cycle cap &mdash; remaining resources were deferred to the
              next cycle.
            </p>
          )}
        </div>
      </CardContent>

      {/* Typed confirmation to arm real deletion */}
      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Arm automated deletion?</AlertDialogTitle>
            <AlertDialogDescription>
              This lets the janitor permanently delete gate-passing orphaned
              resources (up to {maxPerCycle || 0} per cycle) with no further
              confirmation. Type <span className="font-mono font-semibold">on</span>{" "}
              below to confirm.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <Input
            value={confirmText}
            onChange={(e) => setConfirmText(e.target.value)}
            placeholder="Type 'on' to confirm"
            autoFocus
          />
          <AlertDialogFooter>
            <AlertDialogCancel onClick={() => setConfirmText("")}>
              Cancel
            </AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault();
                save();
              }}
              disabled={confirmText.trim().toLowerCase() !== "on" || update.isPending}
              className="bg-red-600 hover:bg-red-700"
            >
              Arm deletion
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  );
}

function StatCell({
  label,
  value,
  highlight,
  danger,
}: {
  label: string;
  value: number;
  highlight?: boolean;
  danger?: boolean;
}) {
  return (
    <div className="rounded-md border p-2">
      <div
        className={`text-xl font-bold ${
          danger ? "text-red-600" : highlight ? "text-green-600" : ""
        }`}
      >
        {value}
      </div>
      <div className="text-xs text-muted-foreground">{label}</div>
    </div>
  );
}
