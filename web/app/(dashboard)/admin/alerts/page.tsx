"use client";

import { useState } from "react";
import { useForm } from "react-hook-form";
import {
  useAllAlerts,
  useCreateAlert,
  useDeactivateAlert,
} from "@/lib/hooks/useAlerts";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { BroadcastAlertSeverity } from "@/types/api";
import { AlertTriangle, Info, ShieldAlert, Trash2 } from "lucide-react";
import { cn } from "@/lib/utils/cn";

interface AlertFormValues {
  title: string;
  body: string;
  severity: BroadcastAlertSeverity;
  expiresAt: string;
}

const severityMeta: Record<
  BroadcastAlertSeverity,
  { label: string; icon: typeof Info; className: string }
> = {
  [BroadcastAlertSeverity.INFO]: {
    label: "Info",
    icon: Info,
    className: "text-blue-600",
  },
  [BroadcastAlertSeverity.WARNING]: {
    label: "Warning",
    icon: AlertTriangle,
    className: "text-amber-600",
  },
  [BroadcastAlertSeverity.CRITICAL]: {
    label: "Critical",
    icon: ShieldAlert,
    className: "text-red-600",
  },
};

export default function AdminAlertsPage() {
  const { data } = useAllAlerts();
  const createAlert = useCreateAlert();
  const deactivateAlert = useDeactivateAlert();
  const [errorMessage, setErrorMessage] = useState("");

  const {
    register,
    handleSubmit,
    reset,
    setValue,
    watch,
    formState: { errors },
  } = useForm<AlertFormValues>({
    defaultValues: {
      severity: BroadcastAlertSeverity.INFO,
      title: "",
      body: "",
      expiresAt: "",
    },
  });

  const severity = watch("severity");

  const onSubmit = async (values: AlertFormValues) => {
    try {
      setErrorMessage("");
      await createAlert.mutateAsync({
        title: values.title,
        body: values.body,
        severity: values.severity,
        expiresAt: values.expiresAt
          ? new Date(values.expiresAt).toISOString()
          : null,
      });
      reset({
        severity: BroadcastAlertSeverity.INFO,
        title: "",
        body: "",
        expiresAt: "",
      });
    } catch (error) {
      setErrorMessage(
        error instanceof Error ? error.message : "Failed to create alert"
      );
    }
  };

  const alerts = data?.alerts ?? [];
  const active = alerts.filter((a) => a.active);
  const inactive = alerts.filter((a) => !a.active);

  return (
    <div className="space-y-6 max-w-3xl">
      <div>
        <h1 className="text-3xl font-bold">Broadcast Alerts</h1>
        <p className="text-muted-foreground">
          Send an alert to all users. Critical alerts block the console until
          acknowledged; info and warning alerts show as a dismissible banner.
          Each user must acknowledge an alert before it disappears for them.
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>New Alert</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="severity">Severity</Label>
              <Select
                value={severity}
                onValueChange={(v) =>
                  setValue("severity", v as BroadcastAlertSeverity)
                }
              >
                <SelectTrigger id="severity">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value={BroadcastAlertSeverity.INFO}>
                    Info (banner)
                  </SelectItem>
                  <SelectItem value={BroadcastAlertSeverity.WARNING}>
                    Warning (banner)
                  </SelectItem>
                  <SelectItem value={BroadcastAlertSeverity.CRITICAL}>
                    Critical (blocking modal)
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="title">Title</Label>
              <Input
                id="title"
                placeholder="Scheduled maintenance"
                {...register("title", {
                  required: "Title is required",
                  maxLength: { value: 200, message: "Max 200 characters" },
                })}
              />
              {errors.title && (
                <p className="text-sm text-red-600">{errors.title.message}</p>
              )}
            </div>

            <div className="space-y-2">
              <Label htmlFor="body">Message</Label>
              <Textarea
                id="body"
                rows={4}
                placeholder="ocpctl will be unavailable Fri 8-10pm EST for a database upgrade..."
                {...register("body", { required: "Message is required" })}
              />
              {errors.body && (
                <p className="text-sm text-red-600">{errors.body.message}</p>
              )}
            </div>

            <div className="space-y-2">
              <Label htmlFor="expiresAt">Expires (optional)</Label>
              <Input
                id="expiresAt"
                type="datetime-local"
                {...register("expiresAt")}
              />
              <p className="text-sm text-muted-foreground">
                After this time the alert stops showing automatically.
              </p>
            </div>

            {errorMessage && (
              <div className="text-sm text-red-600 bg-red-50 p-3 rounded-md">
                {errorMessage}
              </div>
            )}

            <Button type="submit" disabled={createAlert.isPending}>
              {createAlert.isPending ? "Publishing..." : "Publish Alert"}
            </Button>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Active Alerts</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {active.length === 0 && (
            <p className="text-sm text-muted-foreground">No active alerts.</p>
          )}
          {active.map((alert) => {
            const meta = severityMeta[alert.severity];
            const Icon = meta.icon;
            return (
              <div
                key={alert.id}
                className="flex items-start gap-3 rounded-lg border p-3"
              >
                <Icon
                  className={cn(
                    "mt-0.5 h-5 w-5 flex-shrink-0",
                    meta.className
                  )}
                />
                <div className="flex-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{alert.title}</span>
                    <Badge variant="secondary">{meta.label}</Badge>
                  </div>
                  <p className="text-sm text-muted-foreground whitespace-pre-wrap">
                    {alert.body}
                  </p>
                  <p className="text-xs text-muted-foreground mt-1">
                    {alert.ackCount}/{alert.totalUsers} acknowledged
                    {alert.expiresAt &&
                      ` · expires ${new Date(alert.expiresAt).toLocaleString()}`}
                  </p>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => deactivateAlert.mutate(alert.id)}
                  disabled={deactivateAlert.isPending}
                  title="Deactivate alert"
                  className="flex-shrink-0"
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              </div>
            );
          })}
        </CardContent>
      </Card>

      {inactive.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Past Alerts</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            {inactive.map((alert) => (
              <div
                key={alert.id}
                className="flex items-center justify-between text-sm text-muted-foreground"
              >
                <span>{alert.title}</span>
                <span>
                  {alert.ackCount}/{alert.totalUsers} acknowledged
                </span>
              </div>
            ))}
          </CardContent>
        </Card>
      )}
    </div>
  );
}
