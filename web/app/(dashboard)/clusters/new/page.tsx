"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { useProfiles } from "@/lib/hooks/useProfiles";
import { useCreateCluster } from "@/lib/hooks/useClusters";
import { useAuthStore } from "@/lib/stores/authStore";
import { createClusterSchema, type CreateClusterFormData } from "@/lib/schemas/cluster";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Checkbox } from "@/components/ui/checkbox";
import { Switch } from "@/components/ui/switch";
import { TagsInput } from "@/components/ui/tags-input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ExecutionPanel } from "@/components/clusters/ClusterForm/ExecutionPanel";
import { Platform, type ValidationError } from "@/types/api";
import { ApiError } from "@/lib/api/client";
import { AlertCircle, Clock } from "lucide-react";

const DAYS_OF_WEEK = [
  "Sunday",
  "Monday",
  "Tuesday",
  "Wednesday",
  "Thursday",
  "Friday",
  "Saturday",
];

export default function NewClusterPage() {
  const router = useRouter();
  const { user } = useAuthStore();
  const [selectedPlatform, setSelectedPlatform] = useState<Platform>(Platform.AWS);
  const { data: profiles } = useProfiles(selectedPlatform);
  const createCluster = useCreateCluster();
  const [apiValidationErrors, setApiValidationErrors] = useState<ValidationError[]>([]);
  const [generalError, setGeneralError] = useState<string>("");

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    formState: { errors },
  } = useForm<CreateClusterFormData>({
    resolver: zodResolver(createClusterSchema),
    defaultValues: {
      platform: Platform.AWS,
      owner: user?.email || "",
      team: "Migration Feature Team",
      cost_center: "733",
      offhours_opt_in: false,
      enable_efs_storage: false,
      override_work_hours: false,
      work_hours_enabled: user?.work_hours_enabled || false,
      work_hours_start: user?.work_hours?.start_time || "09:00",
      work_hours_end: user?.work_hours?.end_time || "17:00",
      work_days: user?.work_hours?.work_days || ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday"],
    },
  });

  const watchedValues = watch();

  // Sort profiles alphabetically by display_name for consistent ordering
  const sortedProfiles = profiles?.slice().sort((a, b) =>
    a.display_name.localeCompare(b.display_name)
  );

  const selectedProfile = sortedProfiles?.find((p) => p.name === watchedValues.profile);

  // Helper to get field-specific validation error
  const getFieldError = (fieldName: string): string | undefined => {
    const error = apiValidationErrors.find((e) => e.field === fieldName);
    return error?.message;
  };

  // Set default profile to "AWS Single Node OpenShift (SNO)" when profiles load
  useEffect(() => {
    if (sortedProfiles && sortedProfiles.length > 0 && !watchedValues.profile) {
      const defaultProfile = sortedProfiles.find(p => p.name === "aws-sno-test");
      if (defaultProfile) {
        setValue("profile", defaultProfile.name);
      }
    }
  }, [sortedProfiles, setValue]);

  // Update form defaults when profile changes
  useEffect(() => {
    if (selectedProfile) {
      setValue("version", selectedProfile.openshift_versions.default);
      setValue("region", selectedProfile.regions.default);
      setValue("base_domain", selectedProfile.base_domains.default);
      setValue("ttl_hours", selectedProfile.lifecycle.default_ttl_hours);
    }
  }, [selectedProfile, setValue]);

  const onSubmit = async (data: CreateClusterFormData) => {
    // Clear previous errors
    setApiValidationErrors([]);
    setGeneralError("");

    try {
      // Prepare the payload
      const payload: any = { ...data };

      // Remove override_work_hours from payload (it's only for UI)
      delete payload.override_work_hours;

      // Only include work hours if override is enabled
      if (!data.override_work_hours) {
        delete payload.work_hours_enabled;
        delete payload.work_hours_start;
        delete payload.work_hours_end;
        delete payload.work_days;
      } else if (data.work_hours_enabled) {
        // Convert work hours to API format
        payload.work_hours = {
          start_time: data.work_hours_start,
          end_time: data.work_hours_end,
          work_days: data.work_days,
        };
        delete payload.work_hours_start;
        delete payload.work_hours_end;
        delete payload.work_days;
      } else {
        // User enabled override but disabled work hours
        delete payload.work_hours_start;
        delete payload.work_hours_end;
        delete payload.work_days;
      }

      const result = await createCluster.mutateAsync(payload);
      router.push(`/clusters/${result.id}`);
    } catch (error) {
      console.error("Failed to create cluster:", error);

      // Check if this is an ApiError with validation details
      if (error instanceof ApiError && error.response?.details) {
        setApiValidationErrors(error.response.details);
      } else if (error instanceof ApiError && error.response?.message) {
        setGeneralError(error.response.message);
      } else if (error instanceof Error) {
        setGeneralError(error.message);
      } else {
        setGeneralError("Failed to create cluster. Please try again.");
      }
    }
  };

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Create Cluster</h1>
        <p className="text-muted-foreground">
          Request a new OpenShift cluster
        </p>
      </div>

      <form onSubmit={handleSubmit(onSubmit)}>
        <div className="grid grid-cols-2 gap-8">
          {/* Left Panel - Form */}
          <div className="space-y-6">
            {/* Basic Info Section */}
            <div className="rounded-lg border bg-card p-6 space-y-4">
              <h2 className="text-lg font-semibold">Basic Information</h2>

              <div className="space-y-2">
                <Label htmlFor="name">Cluster Name</Label>
                <Input
                  id="name"
                  placeholder="my-cluster"
                  {...register("name")}
                />
                {errors.name && (
                  <p className="text-sm text-red-600">{errors.name.message}</p>
                )}
              </div>

              <div className="space-y-2">
                <Label htmlFor="platform">Platform</Label>
                <Select
                  value={watchedValues.platform || ""}
                  onValueChange={(value) => {
                    setValue("platform", value as Platform);
                    setSelectedPlatform(value as Platform);
                    setValue("profile", ""); // Reset profile when platform changes
                  }}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="Select platform" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="aws">AWS</SelectItem>
                    <SelectItem value="ibmcloud">IBM Cloud</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            {/* Profile Section */}
            <div className="rounded-lg border bg-card p-6 space-y-4">
              <h2 className="text-lg font-semibold">Profile</h2>

              <div className="space-y-2">
                <Label htmlFor="profile">Size Profile</Label>
                <Select
                  value={watchedValues.profile || ""}
                  onValueChange={(value) => setValue("profile", value)}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="Select profile" />
                  </SelectTrigger>
                  <SelectContent>
                    {sortedProfiles && sortedProfiles.length > 0 ? (
                      sortedProfiles.map((profile) => (
                        <SelectItem key={profile.name} value={profile.name}>
                          {profile.display_name} (${profile.cost_controls?.estimated_hourly_cost || 0}/hr)
                        </SelectItem>
                      ))
                    ) : (
                      <div className="p-2 text-sm text-muted-foreground">No profiles available</div>
                    )}
                  </SelectContent>
                </Select>
                {selectedProfile && (
                  <p className="text-sm text-muted-foreground">
                    {selectedProfile.description}
                  </p>
                )}
              </div>

              {selectedProfile && selectedProfile.openshift_versions?.allowed && (
                <div className="space-y-2">
                  <Label htmlFor="version">OpenShift Version</Label>
                  <Select
                    value={watchedValues.version || ""}
                    onValueChange={(value) => setValue("version", value)}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {selectedProfile.openshift_versions.allowed.map((version) => (
                        <SelectItem key={version} value={version}>
                          {version}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              )}
            </div>

            {/* Configuration Section */}
            {selectedProfile && (
              <div className="rounded-lg border bg-card p-6 space-y-4">
                <h2 className="text-lg font-semibold">Configuration</h2>

                <div className="space-y-2">
                  <Label htmlFor="region">Region</Label>
                  <Select
                    value={watchedValues.region || ""}
                    onValueChange={(value) => setValue("region", value)}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {selectedProfile.regions?.allowed?.map((region) => (
                        <SelectItem key={region} value={region}>
                          {region}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  {getFieldError("region") && (
                    <p className="text-sm text-red-600">{getFieldError("region")}</p>
                  )}
                </div>

                <div className="space-y-2">
                  <Label htmlFor="base_domain">Base Domain</Label>
                  <Select
                    value={watchedValues.base_domain || ""}
                    onValueChange={(value) => setValue("base_domain", value)}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {selectedProfile.base_domains?.allowed?.map((domain) => (
                        <SelectItem key={domain} value={domain}>
                          {domain}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>

                <div className="space-y-2">
                  <Label htmlFor="owner">Owner Email</Label>
                  <Input
                    id="owner"
                    type="email"
                    {...register("owner")}
                    readOnly
                    className="bg-muted"
                  />
                </div>

                <div className="space-y-2">
                  <Label htmlFor="team">Team</Label>
                  <Select
                    value={watchedValues.team || ""}
                    onValueChange={(value) => setValue("team", value)}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="Select team" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="Migration Feature Team">Migration Feature Team</SelectItem>
                      <SelectItem value="Application Inventory Management">Application Inventory Management</SelectItem>
                      <SelectItem value="Insights Discovery">Insights Discovery</SelectItem>
                      <SelectItem value="Application Modification">Application Modification</SelectItem>
                      <SelectItem value="OADP Team">OADP Team</SelectItem>
                      <SelectItem value="Staff">Staff</SelectItem>
                    </SelectContent>
                  </Select>
                  {errors.team && (
                    <p className="text-sm text-red-600">{errors.team.message}</p>
                  )}
                </div>

                <div className="space-y-2">
                  <Label htmlFor="cost_center">Cost Center</Label>
                  <Select
                    value={watchedValues.cost_center || ""}
                    onValueChange={(value) => setValue("cost_center", value)}
                  >
                    <SelectTrigger>
                      <SelectValue placeholder="Select cost center" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="733">733</SelectItem>
                    </SelectContent>
                  </Select>
                  {errors.cost_center && (
                    <p className="text-sm text-red-600">
                      {errors.cost_center.message}
                    </p>
                  )}
                </div>

                <div className="space-y-2">
                  <Label htmlFor="ttl_hours">Lifetime (hours)</Label>
                  <Input
                    id="ttl_hours"
                    type="number"
                    min={0}
                    max={selectedProfile.lifecycle.max_ttl_hours}
                    {...register("ttl_hours", { valueAsNumber: true })}
                  />
                  <p className="text-sm text-muted-foreground">
                    Max: {selectedProfile.lifecycle.max_ttl_hours} hours (0 = never expires)
                  </p>
                  {errors.ttl_hours && (
                    <p className="text-sm text-red-600">
                      {errors.ttl_hours.message}
                    </p>
                  )}
                </div>
              </div>
            )}

            {/* Advanced Section */}
            {selectedProfile && (
              <div className="rounded-lg border bg-card p-6 space-y-6">
                <h2 className="text-lg font-semibold">Advanced</h2>

                {/* Access & Security */}
                <div className="space-y-4">
                  <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wide">
                    Access & Security
                  </h3>
                  <div className="space-y-2">
                    <Label htmlFor="ssh_public_key">SSH Public Key (Optional)</Label>
                    <Textarea
                      id="ssh_public_key"
                      placeholder="ssh-rsa AAAA..."
                      rows={3}
                      {...register("ssh_public_key")}
                    />
                    <p className="text-sm text-muted-foreground">
                      Add SSH public key for direct node access
                    </p>
                  </div>
                </div>

                {/* Resource Tagging */}
                <div className="space-y-4">
                  <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wide">
                    Resource Tagging
                  </h3>
                  <div className="space-y-2">
                    <Label>Custom Tags (Optional)</Label>
                    <TagsInput
                      value={watchedValues.extra_tags || {}}
                      onChange={(tags) => setValue("extra_tags", tags)}
                    />
                    <p className="text-sm text-muted-foreground">
                      Add custom tags to apply to all deployed AWS resources
                    </p>
                  </div>
                </div>

                {/* Cluster Features */}
                <div className="space-y-4">
                  <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wide">
                    Cluster Features
                  </h3>
                  <div className="space-y-3">
                    <div className="flex items-start space-x-2">
                      <Checkbox
                        id="offhours_opt_in"
                        checked={watchedValues.offhours_opt_in}
                        onCheckedChange={(checked) =>
                          setValue("offhours_opt_in", checked as boolean)
                        }
                        className="mt-1"
                      />
                      <div className="flex-1">
                        <Label htmlFor="offhours_opt_in" className="cursor-pointer">
                          Enable off-hours scaling
                        </Label>
                        <p className="text-sm text-muted-foreground">
                          Automatically scale down workers during non-business hours to reduce costs
                        </p>
                        {getFieldError("offhours_opt_in") && (
                          <p className="text-sm text-red-600 mt-1">
                            {getFieldError("offhours_opt_in")}
                          </p>
                        )}
                      </div>
                    </div>

                    <div className="flex items-start space-x-2">
                      <Checkbox
                        id="enable_efs_storage"
                        checked={watchedValues.enable_efs_storage}
                        onCheckedChange={(checked) =>
                          setValue("enable_efs_storage", checked as boolean)
                        }
                        className="mt-1"
                      />
                      <div className="flex-1">
                        <Label htmlFor="enable_efs_storage" className="cursor-pointer">
                          Enable EFS shared storage (RWX)
                        </Label>
                        <p className="text-sm text-muted-foreground">
                          Provisions EFS file system with CSI driver for ReadWriteMany (RWX) storage class
                        </p>
                        {getFieldError("enable_efs_storage") && (
                          <p className="text-sm text-red-600 mt-1">
                            {getFieldError("enable_efs_storage")}
                          </p>
                        )}
                      </div>
                    </div>

                    <div className="flex items-start space-x-2">
                      <Checkbox
                        id="override_work_hours"
                        checked={watchedValues.override_work_hours}
                        onCheckedChange={(checked) =>
                          setValue("override_work_hours", checked as boolean)
                        }
                        className="mt-1"
                      />
                      <div className="flex-1">
                        <Label htmlFor="override_work_hours" className="cursor-pointer">
                          Override my default work hours
                        </Label>
                        <p className="text-sm text-muted-foreground">
                          Configure custom work hours for automatic hibernation of this cluster
                        </p>

                        {watchedValues.override_work_hours && (
                          <div className="mt-4 space-y-4 pl-4 border-l-2 border-muted">
                            {/* Work Hours Toggle */}
                            <div className="flex items-center justify-between">
                              <div className="space-y-0.5">
                                <Label htmlFor="work_hours_enabled" className="flex items-center gap-2">
                                  <Clock className="h-4 w-4" />
                                  Enable Automatic Hibernation
                                </Label>
                                <p className="text-sm text-muted-foreground">
                                  Hibernate cluster outside of work hours
                                </p>
                              </div>
                              <Switch
                                id="work_hours_enabled"
                                checked={watchedValues.work_hours_enabled}
                                onCheckedChange={(checked) => setValue("work_hours_enabled", checked)}
                              />
                            </div>

                            {watchedValues.work_hours_enabled && (
                              <div className="space-y-4">
                                {/* Time Range */}
                                <div className="grid grid-cols-2 gap-4">
                                  <div className="space-y-2">
                                    <Label htmlFor="work_hours_start">Start Time</Label>
                                    <Input
                                      id="work_hours_start"
                                      type="time"
                                      value={watchedValues.work_hours_start}
                                      onChange={(e) => setValue("work_hours_start", e.target.value)}
                                    />
                                  </div>
                                  <div className="space-y-2">
                                    <Label htmlFor="work_hours_end">End Time</Label>
                                    <Input
                                      id="work_hours_end"
                                      type="time"
                                      value={watchedValues.work_hours_end}
                                      onChange={(e) => setValue("work_hours_end", e.target.value)}
                                    />
                                  </div>
                                </div>

                                {/* Days Selector */}
                                <div className="space-y-2">
                                  <Label>Work Days</Label>
                                  <div className="grid grid-cols-4 sm:grid-cols-7 gap-2">
                                    {DAYS_OF_WEEK.map((day) => (
                                      <div key={day} className="flex items-center space-x-2">
                                        <Checkbox
                                          id={`day-${day}`}
                                          checked={watchedValues.work_days?.includes(day)}
                                          onCheckedChange={(checked) => {
                                            const currentDays = watchedValues.work_days || [];
                                            const newDays = checked
                                              ? [...currentDays, day]
                                              : currentDays.filter((d) => d !== day);
                                            setValue("work_days", newDays);
                                          }}
                                        />
                                        <Label
                                          htmlFor={`day-${day}`}
                                          className="text-sm font-normal cursor-pointer"
                                        >
                                          {day.substring(0, 3)}
                                        </Label>
                                      </div>
                                    ))}
                                  </div>
                                  <p className="text-sm text-muted-foreground">
                                    Cluster will be active during these hours and days
                                  </p>
                                </div>

                                {/* Schedule Preview */}
                                <div className="bg-muted/50 rounded-md p-3 text-sm">
                                  <p className="font-medium mb-1">Schedule:</p>
                                  <p className="text-muted-foreground">
                                    Active from{" "}
                                    <span className="font-medium text-foreground">
                                      {watchedValues.work_hours_start}
                                    </span>{" "}
                                    to{" "}
                                    <span className="font-medium text-foreground">
                                      {watchedValues.work_hours_end}
                                    </span>{" "}
                                    on{" "}
                                    <span className="font-medium text-foreground">
                                      {watchedValues.work_days?.length === 7
                                        ? "all days"
                                        : watchedValues.work_days?.join(", ")}
                                    </span>
                                  </p>
                                </div>
                              </div>
                            )}
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            )}

            {/* Submit Button */}
            <div className="flex gap-4">
              <Button
                type="button"
                variant="outline"
                onClick={() => router.back()}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={createCluster.isPending || !selectedProfile}
              >
                {createCluster.isPending ? "Creating..." : "Create Cluster"}
              </Button>
            </div>

            {/* Error Display */}
            {(apiValidationErrors.length > 0 || generalError) && (
              <div className="bg-red-50 border border-red-200 rounded-md p-4 space-y-3">
                <div className="flex items-start gap-2">
                  <AlertCircle className="h-5 w-5 text-red-600 mt-0.5 flex-shrink-0" />
                  <div className="flex-1 space-y-2">
                    <p className="font-semibold text-red-900">
                      {apiValidationErrors.length > 0
                        ? "Failed to create cluster - please fix the following errors:"
                        : "Failed to create cluster"}
                    </p>
                    {apiValidationErrors.length > 0 && (
                      <ul className="list-disc list-inside space-y-1 text-sm text-red-800">
                        {apiValidationErrors.map((error, index) => (
                          <li key={index}>
                            <span className="font-medium">{error.field}:</span> {error.message}
                          </li>
                        ))}
                      </ul>
                    )}
                    {generalError && (
                      <p className="text-sm text-red-800">{generalError}</p>
                    )}
                  </div>
                </div>
              </div>
            )}
          </div>

          {/* Right Panel - Execution Details */}
          <ExecutionPanel formValues={watchedValues} profile={selectedProfile} />
        </div>
      </form>
    </div>
  );
}
