"use client";

import { useState, useEffect } from "react";
import { useParams, useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { poolsApi } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Checkbox } from "@/components/ui/checkbox";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { formatDate } from "@/lib/utils/formatters";
import { ArrowLeft, Save, AlertCircle, CheckCircle, Database, Power, Clock, RefreshCw } from "lucide-react";
import type { UpdatePoolRequest } from "@/types/api";

const DAYS_OF_WEEK = [
  { value: 1, label: "Monday" },
  { value: 2, label: "Tuesday" },
  { value: 3, label: "Wednesday" },
  { value: 4, label: "Thursday" },
  { value: 5, label: "Friday" },
  { value: 6, label: "Saturday" },
  { value: 0, label: "Sunday" },
];

export default function PoolDetailsPage() {
  const params = useParams();
  const router = useRouter();
  const queryClient = useQueryClient();
  const poolName = decodeURIComponent(params.name as string);

  const [updateSuccess, setUpdateSuccess] = useState("");
  const [updateError, setUpdateError] = useState("");
  const [scheduledMode, setScheduledMode] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(false);
  const [autoRelease, setAutoRelease] = useState(true);
  const [enabled, setEnabled] = useState(true);
  const [selectedDays, setSelectedDays] = useState<number[]>([1, 2, 3, 4, 5]);

  const { data: pool, isLoading } = useQuery({
    queryKey: ["pool", poolName],
    queryFn: () => poolsApi.getPool(poolName),
    refetchInterval: 30000, // Refresh every 30 seconds
  });

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isDirty },
  } = useForm<UpdatePoolRequest>();

  useEffect(() => {
    if (pool) {
      reset({
        display_name: pool.display_name,
        description: pool.description || "",
        target_size: pool.target_size,
        min_size: pool.min_size,
        max_size: pool.max_size,
        max_lease_duration_hours: pool.max_lease_duration_hours,
        max_cluster_age_days: pool.max_cluster_age_days,
        schedule_timezone: pool.schedule_timezone,
        schedule_start_hour: pool.schedule_start_hour,
        schedule_end_hour: pool.schedule_end_hour,
      });
      setScheduledMode(pool.scheduled_mode);
      setAutoRefresh(pool.auto_refresh_enabled);
      setAutoRelease(pool.auto_release_enabled);
      setEnabled(pool.enabled);
      setSelectedDays(pool.schedule_days_of_week || [1, 2, 3, 4, 5]);
    }
  }, [pool, reset]);

  const updateMutation = useMutation({
    mutationFn: (data: UpdatePoolRequest) => poolsApi.updatePool(poolName, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["pool", poolName] });
      queryClient.invalidateQueries({ queryKey: ["pools"] });
      setUpdateSuccess("Pool updated successfully!");
      setUpdateError("");
      setTimeout(() => setUpdateSuccess(""), 3000);
    },
    onError: (error: any) => {
      setUpdateError(error.message || "Failed to update pool");
      setUpdateSuccess("");
    },
  });

  const toggleDay = (day: number) => {
    if (selectedDays.includes(day)) {
      setSelectedDays(selectedDays.filter((d) => d !== day));
    } else {
      setSelectedDays([...selectedDays, day].sort());
    }
  };

  const onSubmit = async (data: UpdatePoolRequest) => {
    setUpdateSuccess("");
    setUpdateError("");

    const payload: UpdatePoolRequest = {
      ...data,
      auto_release_enabled: autoRelease,
      auto_refresh_enabled: autoRefresh,
      scheduled_mode: scheduledMode,
      schedule_days_of_week: scheduledMode ? selectedDays : undefined,
      enabled,
    };

    updateMutation.mutate(payload);
  };

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg">Loading pool...</div>
      </div>
    );
  }

  if (!pool) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-lg text-red-600">Pool not found</div>
      </div>
    );
  }

  const stats = pool.stats;

  return (
    <div className="space-y-6 max-w-5xl">
      <div className="flex items-center gap-4">
        <Button variant="ghost" size="sm" onClick={() => router.back()}>
          <ArrowLeft className="mr-2 h-4 w-4" />
          Back
        </Button>
        <div className="flex-1">
          <h1 className="text-3xl font-bold">{pool.display_name}</h1>
          <p className="text-muted-foreground">Pool ID: {pool.name}</p>
        </div>
        <Badge variant={pool.enabled ? "default" : "secondary"}>
          {pool.enabled ? "Enabled" : "Disabled"}
        </Badge>
      </div>

      {/* Statistics Cards */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardContent className="p-6">
            <div className="flex items-center gap-3">
              <div className="rounded-lg bg-blue-500/10 p-3">
                <Database className="h-5 w-5 text-blue-600" />
              </div>
              <div>
                <p className="text-sm text-muted-foreground">Total</p>
                <p className="text-2xl font-bold">{stats.total_clusters}</p>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardContent className="p-6">
            <div className="flex items-center gap-3">
              <div className="rounded-lg bg-green-500/10 p-3">
                <Power className="h-5 w-5 text-green-600" />
              </div>
              <div>
                <p className="text-sm text-muted-foreground">Ready</p>
                <p className="text-2xl font-bold">{stats.ready_clusters}</p>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardContent className="p-6">
            <div className="flex items-center gap-3">
              <div className="rounded-lg bg-orange-500/10 p-3">
                <Clock className="h-5 w-5 text-orange-600" />
              </div>
              <div>
                <p className="text-sm text-muted-foreground">Leased</p>
                <p className="text-2xl font-bold">{stats.leased_clusters}</p>
              </div>
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardContent className="p-6">
            <div className="flex items-center gap-3">
              <div className="rounded-lg bg-purple-500/10 p-3">
                <RefreshCw className="h-5 w-5 text-purple-600" />
              </div>
              <div>
                <p className="text-sm text-muted-foreground">Provisioning</p>
                <p className="text-2xl font-bold">{stats.provisioning_clusters}</p>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Pool Information */}
      <Card>
        <CardHeader>
          <CardTitle>Pool Information</CardTitle>
          <CardDescription>Created {formatDate(pool.created_at)}</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="grid grid-cols-2 gap-4 text-sm">
            <div>
              <span className="text-muted-foreground">Profile:</span>{" "}
              <Badge variant="outline">{pool.profile}</Badge>
            </div>
            <div>
              <span className="text-muted-foreground">Created By:</span> {pool.created_by || "System"}
            </div>
            <div>
              <span className="text-muted-foreground">Min Size:</span> {pool.min_size}
            </div>
            <div>
              <span className="text-muted-foreground">Target Size:</span> {pool.target_size}
            </div>
            <div>
              <span className="text-muted-foreground">Max Size:</span> {pool.max_size}
            </div>
            <div>
              <span className="text-muted-foreground">Max Lease Duration:</span> {pool.max_lease_duration_hours}h
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Edit Form */}
      <form onSubmit={handleSubmit(onSubmit)}>
        {/* Basic Settings */}
        <Card className="mb-6">
          <CardHeader>
            <CardTitle>Basic Settings</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="display_name">Display Name *</Label>
                <Input
                  id="display_name"
                  {...register("display_name", {
                    required: "Display name is required",
                  })}
                />
                {errors.display_name && <p className="text-sm text-red-600">{errors.display_name.message}</p>}
              </div>

              <div className="space-y-2">
                <Label>Pool Status</Label>
                <div className="flex items-center space-x-2 pt-2">
                  <Checkbox
                    id="enabled"
                    checked={enabled}
                    onCheckedChange={(checked) => setEnabled(!!checked)}
                  />
                  <label htmlFor="enabled" className="text-sm font-medium">
                    Pool Enabled
                  </label>
                </div>
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="description">Description</Label>
              <Textarea
                id="description"
                rows={3}
                {...register("description")}
              />
            </div>
          </CardContent>
        </Card>

        {/* Pool Sizing */}
        <Card className="mb-6">
          <CardHeader>
            <CardTitle>Pool Sizing</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-3 gap-4">
              <div className="space-y-2">
                <Label htmlFor="min_size">Min Size *</Label>
                <Input
                  id="min_size"
                  type="number"
                  min="0"
                  {...register("min_size", {
                    required: "Required",
                    valueAsNumber: true,
                    min: { value: 0, message: "Must be at least 0" },
                  })}
                />
                {errors.min_size && <p className="text-sm text-red-600">{errors.min_size.message}</p>}
              </div>

              <div className="space-y-2">
                <Label htmlFor="target_size">Target Size *</Label>
                <Input
                  id="target_size"
                  type="number"
                  min="1"
                  {...register("target_size", {
                    required: "Required",
                    valueAsNumber: true,
                    min: { value: 1, message: "Must be at least 1" },
                  })}
                />
                {errors.target_size && <p className="text-sm text-red-600">{errors.target_size.message}</p>}
              </div>

              <div className="space-y-2">
                <Label htmlFor="max_size">Max Size *</Label>
                <Input
                  id="max_size"
                  type="number"
                  min="1"
                  {...register("max_size", {
                    required: "Required",
                    valueAsNumber: true,
                    min: { value: 1, message: "Must be at least 1" },
                  })}
                />
                {errors.max_size && <p className="text-sm text-red-600">{errors.max_size.message}</p>}
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Lease Configuration */}
        <Card className="mb-6">
          <CardHeader>
            <CardTitle>Lease Configuration</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="max_lease_duration_hours">Max Lease Duration (hours) *</Label>
                <Input
                  id="max_lease_duration_hours"
                  type="number"
                  min="1"
                  max="168"
                  {...register("max_lease_duration_hours", {
                    valueAsNumber: true,
                    min: { value: 1, message: "Min 1 hour" },
                    max: { value: 168, message: "Max 168 hours" },
                  })}
                />
              </div>

              <div className="space-y-2">
                <Label>Auto-Release</Label>
                <div className="flex items-center space-x-2 pt-2">
                  <Checkbox
                    id="auto_release"
                    checked={autoRelease}
                    onCheckedChange={(checked) => setAutoRelease(!!checked)}
                  />
                  <label htmlFor="auto_release" className="text-sm font-medium">
                    Auto-release expired leases
                  </label>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Lifecycle */}
        <Card className="mb-6">
          <CardHeader>
            <CardTitle>Cluster Lifecycle</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center space-x-2">
              <Checkbox
                id="auto_refresh"
                checked={autoRefresh}
                onCheckedChange={(checked) => setAutoRefresh(!!checked)}
              />
              <label htmlFor="auto_refresh" className="text-sm font-medium">
                Enable automatic cluster refresh
              </label>
            </div>

            {autoRefresh && (
              <div className="space-y-2 ml-6">
                <Label htmlFor="max_cluster_age_days">Max Cluster Age (days) *</Label>
                <Input
                  id="max_cluster_age_days"
                  type="number"
                  min="1"
                  max="90"
                  {...register("max_cluster_age_days", {
                    valueAsNumber: true,
                    min: { value: 1, message: "Min 1 day" },
                    max: { value: 90, message: "Max 90 days" },
                  })}
                />
              </div>
            )}
          </CardContent>
        </Card>

        {/* Schedule */}
        <Card className="mb-6">
          <CardHeader>
            <CardTitle>Schedule Configuration</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center space-x-2">
              <Checkbox
                id="scheduled_mode"
                checked={scheduledMode}
                onCheckedChange={(checked) => setScheduledMode(!!checked)}
              />
              <label htmlFor="scheduled_mode" className="text-sm font-medium">
                Enable scheduled mode
              </label>
            </div>

            {scheduledMode && (
              <div className="space-y-4 ml-6">
                <div className="grid grid-cols-3 gap-4">
                  <div className="space-y-2">
                    <Label htmlFor="schedule_timezone">Timezone</Label>
                    <Input id="schedule_timezone" {...register("schedule_timezone")} />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="schedule_start_hour">Start Hour</Label>
                    <Input
                      id="schedule_start_hour"
                      type="number"
                      min="0"
                      max="23"
                      {...register("schedule_start_hour", { valueAsNumber: true })}
                    />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="schedule_end_hour">End Hour</Label>
                    <Input
                      id="schedule_end_hour"
                      type="number"
                      min="0"
                      max="23"
                      {...register("schedule_end_hour", { valueAsNumber: true })}
                    />
                  </div>
                </div>

                <div className="space-y-2">
                  <Label>Active Days</Label>
                  <div className="flex flex-wrap gap-2">
                    {DAYS_OF_WEEK.map((day) => (
                      <div key={day.value} className="flex items-center space-x-2">
                        <Checkbox
                          id={`day-${day.value}`}
                          checked={selectedDays.includes(day.value)}
                          onCheckedChange={() => toggleDay(day.value)}
                        />
                        <label htmlFor={`day-${day.value}`} className="text-sm font-medium">
                          {day.label}
                        </label>
                      </div>
                    ))}
                  </div>
                </div>
              </div>
            )}
          </CardContent>
        </Card>

        {/* Status Messages */}
        {updateSuccess && (
          <div className="bg-green-50 border border-green-200 rounded-md p-3 flex items-center gap-2 mb-6">
            <CheckCircle className="h-5 w-5 text-green-600" />
            <p className="text-sm text-green-800">{updateSuccess}</p>
          </div>
        )}

        {updateError && (
          <div className="bg-red-50 border border-red-200 rounded-md p-3 flex items-center gap-2 mb-6">
            <AlertCircle className="h-5 w-5 text-red-600" />
            <p className="text-sm text-red-800">{updateError}</p>
          </div>
        )}

        <div className="flex gap-4">
          <Button type="submit" disabled={!isDirty && updateMutation.isPending}>
            <Save className="mr-2 h-4 w-4" />
            {updateMutation.isPending ? "Saving..." : "Save Changes"}
          </Button>
        </div>
      </form>
    </div>
  );
}
