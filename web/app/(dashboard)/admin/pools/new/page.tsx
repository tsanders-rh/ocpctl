"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { useMutation, useQuery } from "@tanstack/react-query";
import { poolsApi, profilesApi } from "@/lib/api";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Checkbox } from "@/components/ui/checkbox";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ArrowLeft, AlertCircle, CheckCircle, HelpCircle } from "lucide-react";
import type { CreatePoolRequest } from "@/types/api";

const DAYS_OF_WEEK = [
  { value: 1, label: "Monday" },
  { value: 2, label: "Tuesday" },
  { value: 3, label: "Wednesday" },
  { value: 4, label: "Thursday" },
  { value: 5, label: "Friday" },
  { value: 6, label: "Saturday" },
  { value: 0, label: "Sunday" },
];

export default function NewPoolPage() {
  const router = useRouter();
  const [successMessage, setSuccessMessage] = useState("");
  const [errorMessage, setErrorMessage] = useState("");
  const [selectedProfile, setSelectedProfile] = useState<string>("");
  const [scheduledMode, setScheduledMode] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(false);
  const [autoRelease, setAutoRelease] = useState(true);
  const [selectedDays, setSelectedDays] = useState<number[]>([1, 2, 3, 4, 5]); // Mon-Fri default

  // Cluster configuration overrides
  const [clusterVersion, setClusterVersion] = useState<string>("");
  const [clusterRegion, setClusterRegion] = useState<string>("");
  const [clusterBaseDomain, setClusterBaseDomain] = useState<string>("");
  const [clusterExtraTags, setClusterExtraTags] = useState<string>("");
  const [workHoursEnabled, setWorkHoursEnabled] = useState<boolean | undefined>(undefined);
  const [credentialsMode, setCredentialsMode] = useState<string>("");

  const { data: profilesData } = useQuery({
    queryKey: ["profiles"],
    queryFn: () => profilesApi.list(),
  });

  const {
    register,
    handleSubmit,
    formState: { errors, isSubmitting },
    setValue,
  } = useForm<CreatePoolRequest>({
    defaultValues: {
      target_size: 3,
      min_size: 1,
      max_size: 10,
      default_lease_duration_hours: 24,
      max_lease_duration_hours: 2,
      auto_release_enabled: true,
      max_cluster_age_days: 7,
      auto_refresh_enabled: false,
      scheduled_mode: false,
      schedule_start_hour: 8,
      schedule_end_hour: 18,
      enabled: true,
    },
  });

  const createMutation = useMutation({
    mutationFn: (data: CreatePoolRequest) => poolsApi.createPool(data),
    onSuccess: (pool) => {
      setSuccessMessage(`Pool "${pool.display_name}" created successfully!`);
      setErrorMessage("");
      setTimeout(() => {
        router.push(`/admin/pools/${encodeURIComponent(pool.name)}`);
      }, 1500);
    },
    onError: (error: any) => {
      setErrorMessage(error.message || "Failed to create pool");
      setSuccessMessage("");
    },
  });

  const toggleDay = (day: number) => {
    if (selectedDays.includes(day)) {
      setSelectedDays(selectedDays.filter((d) => d !== day));
    } else {
      setSelectedDays([...selectedDays, day].sort());
    }
  };

  const onSubmit = async (data: CreatePoolRequest) => {
    setSuccessMessage("");
    setErrorMessage("");

    // Build cluster_config from overrides
    const cluster_config: any = {};
    if (clusterVersion) cluster_config.version = clusterVersion;
    if (clusterRegion) cluster_config.region = clusterRegion;
    if (clusterBaseDomain) cluster_config.base_domain = clusterBaseDomain;
    if (clusterExtraTags) {
      try {
        cluster_config.extra_tags = JSON.parse(clusterExtraTags);
      } catch (e) {
        setErrorMessage("Invalid JSON in extra tags");
        return;
      }
    }
    if (workHoursEnabled !== undefined) cluster_config.work_hours_enabled = workHoursEnabled;
    if (credentialsMode) cluster_config.credentials_mode = credentialsMode;

    // Build the final payload
    const payload: CreatePoolRequest = {
      ...data,
      profile: selectedProfile,
      auto_release_enabled: autoRelease,
      auto_refresh_enabled: autoRefresh,
      scheduled_mode: scheduledMode,
      schedule_days_of_week: scheduledMode ? selectedDays : undefined,
      cluster_config: Object.keys(cluster_config).length > 0 ? cluster_config : undefined,
    };

    createMutation.mutate(payload);
  };

  const profiles = profilesData?.filter((p) => p.enabled) || [];
  const selectedProfileData = profiles.find((p) => p.name === selectedProfile);

  return (
    <div className="space-y-6 max-w-4xl">
      <div className="flex items-center gap-4">
        <Button variant="ghost" size="sm" onClick={() => router.back()}>
          <ArrowLeft className="mr-2 h-4 w-4" />
          Back
        </Button>
        <div>
          <h1 className="text-3xl font-bold">Create Cluster Pool</h1>
          <p className="text-muted-foreground">
            Configure a new pre-provisioned cluster pool for fast CI/CD integration
          </p>
        </div>
      </div>

      <form onSubmit={handleSubmit(onSubmit)}>
        {/* Basic Information */}
        <Card className="mb-6">
          <CardHeader>
            <CardTitle>Basic Information</CardTitle>
            <CardDescription>
              Configure the pool name, profile, and description
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="name">Pool ID *</Label>
                <Input
                  id="name"
                  placeholder="ci-sno-pool"
                  {...register("name", {
                    required: "Pool ID is required",
                    pattern: {
                      value: /^[a-z0-9-]+$/,
                      message: "Pool ID must be lowercase letters, numbers, and hyphens only",
                    },
                    minLength: { value: 2, message: "Pool ID must be at least 2 characters" },
                    maxLength: { value: 64, message: "Pool ID must not exceed 64 characters" },
                  })}
                />
                {errors.name && <p className="text-sm text-red-600">{errors.name.message}</p>}
                <p className="text-sm text-muted-foreground">
                  Unique identifier (e.g., "ci-sno-pool")
                </p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="display_name">Display Name *</Label>
                <Input
                  id="display_name"
                  placeholder="CI SNO Pool"
                  {...register("display_name", {
                    required: "Display name is required",
                  })}
                />
                {errors.display_name && <p className="text-sm text-red-600">{errors.display_name.message}</p>}
                <p className="text-sm text-muted-foreground">
                  Human-readable name for the pool
                </p>
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="profile">Profile *</Label>
              <Select value={selectedProfile} onValueChange={setSelectedProfile} required>
                <SelectTrigger>
                  <SelectValue placeholder="Select a cluster profile" />
                </SelectTrigger>
                <SelectContent>
                  {profiles.map((profile) => (
                    <SelectItem key={profile.name} value={profile.name}>
                      {profile.display_name} ({profile.platform})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-sm text-muted-foreground">
                All clusters in this pool will use this profile
              </p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="description">Description</Label>
              <Textarea
                id="description"
                placeholder="Pool for CI/CD integration testing..."
                rows={3}
                {...register("description")}
              />
            </div>
          </CardContent>
        </Card>

        {/* Cluster Configuration Overrides */}
        {selectedProfile && selectedProfileData && (
          <Card className="mb-6">
            <CardHeader>
              <CardTitle>Cluster Configuration Overrides (Optional)</CardTitle>
              <CardDescription>
                Override profile defaults for clusters in this pool. Leave blank to use profile defaults.
              </CardDescription>
            </CardHeader>
            <CardContent className="space-y-4">
              <div className="grid grid-cols-2 gap-4">
                {/* Version override */}
                {selectedProfileData.openshift_versions?.allowed && (
                  <div className="space-y-2">
                    <Label htmlFor="cluster_version">OpenShift Version</Label>
                    <Select value={clusterVersion} onValueChange={setClusterVersion}>
                      <SelectTrigger>
                        <SelectValue placeholder={`Use profile default (${selectedProfileData.openshift_versions.default})`} />
                      </SelectTrigger>
                      <SelectContent>
                        {selectedProfileData.openshift_versions.allowed.map((version: string) => (
                          <SelectItem key={version} value={version}>
                            {version}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <p className="text-sm text-muted-foreground">
                      Override version for all clusters in this pool
                    </p>
                  </div>
                )}

                {selectedProfileData.kubernetes_versions?.allowed && (
                  <div className="space-y-2">
                    <Label htmlFor="cluster_version">Kubernetes Version</Label>
                    <Select value={clusterVersion} onValueChange={setClusterVersion}>
                      <SelectTrigger>
                        <SelectValue placeholder={`Use profile default (${selectedProfileData.kubernetes_versions.default})`} />
                      </SelectTrigger>
                      <SelectContent>
                        {selectedProfileData.kubernetes_versions.allowed.map((version: string) => (
                          <SelectItem key={version} value={version}>
                            {version}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <p className="text-sm text-muted-foreground">
                      Override version for all clusters in this pool
                    </p>
                  </div>
                )}

                {/* Region override */}
                <div className="space-y-2">
                  <Label htmlFor="cluster_region">Region</Label>
                  <Select value={clusterRegion} onValueChange={setClusterRegion}>
                    <SelectTrigger>
                      <SelectValue placeholder={`Use profile default (${selectedProfileData.regions?.default || 'N/A'})`} />
                    </SelectTrigger>
                    <SelectContent>
                      {selectedProfileData.regions?.allowed?.map((region: string) => (
                        <SelectItem key={region} value={region}>
                          {region}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <p className="text-sm text-muted-foreground">
                    Override region for all clusters in this pool
                  </p>
                </div>
              </div>

              {/* Base domain override */}
              {selectedProfileData.base_domains?.allowed && (
                <div className="space-y-2">
                  <Label htmlFor="cluster_base_domain">Base Domain</Label>
                  <Select value={clusterBaseDomain} onValueChange={setClusterBaseDomain}>
                    <SelectTrigger>
                      <SelectValue placeholder={`Use profile default (${selectedProfileData.base_domains.default})`} />
                    </SelectTrigger>
                    <SelectContent>
                      {selectedProfileData.base_domains.allowed.map((domain: string) => (
                        <SelectItem key={domain} value={domain}>
                          {domain}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <p className="text-sm text-muted-foreground">
                    Override base domain for all clusters in this pool
                  </p>
                </div>
              )}

              {/* Extra tags */}
              <div className="space-y-2">
                <Label htmlFor="extra_tags">Extra Tags (JSON)</Label>
                <Textarea
                  id="extra_tags"
                  placeholder='{"team": "platform-eng", "environment": "ci"}'
                  rows={3}
                  value={clusterExtraTags}
                  onChange={(e) => setClusterExtraTags(e.target.value)}
                />
                <p className="text-sm text-muted-foreground">
                  Additional AWS tags as JSON (e.g., {`{"key": "value"}`})
                </p>
              </div>

              {/* Work hours and credentials */}
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label>Work Hours Hibernation</Label>
                  <div className="flex flex-col gap-2">
                    <div className="flex items-center space-x-2">
                      <Checkbox
                        id="work_hours_on"
                        checked={workHoursEnabled === true}
                        onCheckedChange={(checked) => setWorkHoursEnabled(checked ? true : undefined)}
                      />
                      <label htmlFor="work_hours_on" className="text-sm">
                        Enable work hours
                      </label>
                    </div>
                    <div className="flex items-center space-x-2">
                      <Checkbox
                        id="work_hours_off"
                        checked={workHoursEnabled === false}
                        onCheckedChange={(checked) => setWorkHoursEnabled(checked ? false : undefined)}
                      />
                      <label htmlFor="work_hours_off" className="text-sm">
                        Disable work hours
                      </label>
                    </div>
                  </div>
                  <p className="text-sm text-muted-foreground">
                    Override hibernation schedule
                  </p>
                </div>

                <div className="space-y-2">
                  <Label htmlFor="credentials_mode">Credentials Mode</Label>
                  <Select value={credentialsMode} onValueChange={setCredentialsMode}>
                    <SelectTrigger>
                      <SelectValue placeholder="Use profile default" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="Auto">Auto</SelectItem>
                      <SelectItem value="Manual">Manual</SelectItem>
                      <SelectItem value="Passthrough">Passthrough</SelectItem>
                      <SelectItem value="Mint">Mint</SelectItem>
                    </SelectContent>
                  </Select>
                  <p className="text-sm text-muted-foreground">
                    Cloud credentials handling mode
                  </p>
                </div>
              </div>
            </CardContent>
          </Card>
        )}

        {/* Pool Sizing */}
        <Card className="mb-6">
          <CardHeader>
            <CardTitle>Pool Sizing</CardTitle>
            <CardDescription>
              Configure the number of clusters to maintain in the pool
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-3 gap-4">
              <div className="space-y-2">
                <Label htmlFor="min_size">Minimum Size *</Label>
                <Input
                  id="min_size"
                  type="number"
                  min="0"
                  {...register("min_size", {
                    required: "Minimum size is required",
                    valueAsNumber: true,
                    min: { value: 0, message: "Minimum size must be at least 0" },
                  })}
                />
                {errors.min_size && <p className="text-sm text-red-600">{errors.min_size.message}</p>}
                <p className="text-sm text-muted-foreground">
                  Trigger replenishment below this
                </p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="target_size">Target Size *</Label>
                <Input
                  id="target_size"
                  type="number"
                  min="1"
                  {...register("target_size", {
                    required: "Target size is required",
                    valueAsNumber: true,
                    min: { value: 1, message: "Target size must be at least 1" },
                  })}
                />
                {errors.target_size && <p className="text-sm text-red-600">{errors.target_size.message}</p>}
                <p className="text-sm text-muted-foreground">
                  Desired number of clusters
                </p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="max_size">Maximum Size *</Label>
                <Input
                  id="max_size"
                  type="number"
                  min="1"
                  {...register("max_size", {
                    required: "Maximum size is required",
                    valueAsNumber: true,
                    min: { value: 1, message: "Maximum size must be at least 1" },
                  })}
                />
                {errors.max_size && <p className="text-sm text-red-600">{errors.max_size.message}</p>}
                <p className="text-sm text-muted-foreground">
                  Hard limit on pool size
                </p>
              </div>
            </div>
          </CardContent>
        </Card>

        {/* Lease Configuration */}
        <Card className="mb-6">
          <CardHeader>
            <CardTitle>Lease Configuration</CardTitle>
            <CardDescription>
              Configure cluster leasing behavior
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="default_lease_duration_hours">Default Lease Duration (hours) *</Label>
                <Input
                  id="default_lease_duration_hours"
                  type="number"
                  min="1"
                  max="168"
                  {...register("default_lease_duration_hours", {
                    valueAsNumber: true,
                    min: { value: 1, message: "Minimum lease duration is 1 hour" },
                    max: { value: 168, message: "Maximum lease duration is 168 hours (1 week)" },
                  })}
                />
                <p className="text-sm text-muted-foreground">
                  Default lease duration when leasing a cluster (1-168 hours)
                </p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="max_lease_duration_hours">Max Lease Duration (hours) *</Label>
                <Input
                  id="max_lease_duration_hours"
                  type="number"
                  min="1"
                  max="168"
                  {...register("max_lease_duration_hours", {
                    valueAsNumber: true,
                    min: { value: 1, message: "Minimum lease duration is 1 hour" },
                    max: { value: 168, message: "Maximum lease duration is 168 hours (1 week)" },
                  })}
                />
                <p className="text-sm text-muted-foreground">
                  Maximum time a cluster can be leased (1-168 hours)
                </p>
              </div>
            </div>

            <div className="space-y-2">
              <Label>Auto-Release</Label>
              <div className="flex items-center space-x-2">
                <Checkbox
                  id="auto_release"
                  checked={autoRelease}
                  onCheckedChange={(checked) => setAutoRelease(!!checked)}
                />
                <label
                  htmlFor="auto_release"
                  className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
                >
                  Automatically release expired leases
                </label>
              </div>
              <p className="text-sm text-muted-foreground">
                Release clusters when lease expires
              </p>
            </div>
          </CardContent>
        </Card>

        {/* Cluster Lifecycle */}
        <Card className="mb-6">
          <CardHeader>
            <CardTitle>Cluster Lifecycle</CardTitle>
            <CardDescription>
              Configure cluster aging and refresh policies
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center space-x-2">
              <Checkbox
                id="auto_refresh"
                checked={autoRefresh}
                onCheckedChange={(checked) => setAutoRefresh(!!checked)}
              />
              <label
                htmlFor="auto_refresh"
                className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
              >
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
                    min: { value: 1, message: "Minimum age is 1 day" },
                    max: { value: 90, message: "Maximum age is 90 days" },
                  })}
                />
                <p className="text-sm text-muted-foreground">
                  Replace clusters older than this (1-90 days)
                </p>
              </div>
            )}
          </CardContent>
        </Card>

        {/* Schedule Configuration */}
        <Card className="mb-6">
          <CardHeader>
            <CardTitle>Schedule Configuration</CardTitle>
            <CardDescription>
              Optionally limit pool operations to specific hours/days
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center space-x-2">
              <Checkbox
                id="scheduled_mode"
                checked={scheduledMode}
                onCheckedChange={(checked) => setScheduledMode(!!checked)}
              />
              <label
                htmlFor="scheduled_mode"
                className="text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
              >
                Enable scheduled mode
              </label>
            </div>

            {scheduledMode && (
              <div className="space-y-4 ml-6">
                <div className="grid grid-cols-3 gap-4">
                  <div className="space-y-2">
                    <Label htmlFor="schedule_timezone">Timezone</Label>
                    <Input
                      id="schedule_timezone"
                      placeholder="America/New_York"
                      defaultValue="America/New_York"
                      {...register("schedule_timezone")}
                    />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="schedule_start_hour">Start Hour</Label>
                    <Input
                      id="schedule_start_hour"
                      type="number"
                      min="0"
                      max="23"
                      {...register("schedule_start_hour", {
                        valueAsNumber: true,
                        min: { value: 0, message: "Hour must be 0-23" },
                        max: { value: 23, message: "Hour must be 0-23" },
                      })}
                    />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="schedule_end_hour">End Hour</Label>
                    <Input
                      id="schedule_end_hour"
                      type="number"
                      min="0"
                      max="23"
                      {...register("schedule_end_hour", {
                        valueAsNumber: true,
                        min: { value: 0, message: "Hour must be 0-23" },
                        max: { value: 23, message: "Hour must be 0-23" },
                      })}
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
                        <label
                          htmlFor={`day-${day.value}`}
                          className="text-sm font-medium leading-none"
                        >
                          {day.label}
                        </label>
                      </div>
                    ))}
                  </div>
                  <p className="text-sm text-muted-foreground">
                    Pool operations only run during these days/hours
                  </p>
                </div>
              </div>
            )}
          </CardContent>
        </Card>

        {/* Status Messages */}
        {successMessage && (
          <div className="bg-green-50 border border-green-200 rounded-md p-3 flex items-center gap-2 mb-6">
            <CheckCircle className="h-5 w-5 text-green-600" />
            <p className="text-sm text-green-800">{successMessage}</p>
          </div>
        )}

        {errorMessage && (
          <div className="bg-red-50 border border-red-200 rounded-md p-3 flex items-center gap-2 mb-6">
            <AlertCircle className="h-5 w-5 text-red-600" />
            <p className="text-sm text-red-800">{errorMessage}</p>
          </div>
        )}

        <div className="flex gap-4">
          <Button
            type="button"
            variant="outline"
            onClick={() => router.back()}
            disabled={isSubmitting}
          >
            Cancel
          </Button>
          <Button
            type="submit"
            disabled={isSubmitting || createMutation.isPending || !selectedProfile}
          >
            {isSubmitting || createMutation.isPending ? "Creating..." : "Create Pool"}
          </Button>
        </div>
      </form>
    </div>
  );
}
