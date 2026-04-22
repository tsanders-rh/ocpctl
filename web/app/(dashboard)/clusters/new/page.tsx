"use client";

import { useEffect, useState, useMemo } from "react";
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
import { CustomPostConfigEditor } from "@/components/postconfig/CustomPostConfigEditor";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ExecutionPanel } from "@/components/clusters/ClusterForm/ExecutionPanel";
import { Platform, ClusterType, type ValidationError } from "@/types/api";
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
  const [selectedClusterType, setSelectedClusterType] = useState<ClusterType>(ClusterType.OpenShift);
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
      cluster_type: ClusterType.OpenShift,
      owner: user?.email || "",
      team: "Migration Feature Team",
      cost_center: "733",
      offhours_opt_in: false,
      skip_post_deployment: false,
      postConfigAddOns: [],
      customPostConfig: undefined,
      enable_efs_storage: false,
      preserve_on_failure: false,
      credentials_mode: "Manual",
      override_work_hours: false,
      work_hours_enabled: user?.work_hours_enabled || false,
      work_hours_start: user?.work_hours?.start_time || "09:00",
      work_hours_end: user?.work_hours?.end_time || "17:00",
      work_days: user?.work_hours?.work_days || ["Monday", "Tuesday", "Wednesday", "Thursday", "Friday"],
    },
  });

  const watchedValues = watch();

  // Filter profiles by platform AND cluster type, then sort alphabetically
  const sortedProfiles = useMemo(() => {
    const filteredProfiles = profiles?.filter((p) => {
      // For OpenShift clusters, show profiles that start with platform prefix (aws-, ibmcloud-)
      if (selectedClusterType === ClusterType.OpenShift) {
        return p.name.startsWith(`${selectedPlatform}-`);
      }
      // For EKS/IKS clusters, show profiles that start with cluster type prefix (eks-, iks-)
      if (selectedClusterType === ClusterType.EKS) {
        return p.name.startsWith('eks-');
      }
      if (selectedClusterType === ClusterType.IKS) {
        return p.name.startsWith('iks-');
      }
      return false;
    });

    return filteredProfiles?.slice().sort((a, b) =>
      a.display_name.localeCompare(b.display_name)
    );
  }, [profiles, selectedClusterType, selectedPlatform]);

  const selectedProfile = sortedProfiles?.find((p) => p.name === watchedValues.profile);

  // Helper to get field-specific validation error
  const getFieldError = (fieldName: string): string | undefined => {
    const error = apiValidationErrors.find((e) => e.field === fieldName);
    return error?.message;
  };

  // Set default profile based on cluster type when profiles load or cluster type changes
  useEffect(() => {
    if (sortedProfiles && sortedProfiles.length > 0) {
      // Determine default profile based on cluster type
      let defaultProfileName = "";

      if (selectedClusterType === ClusterType.OpenShift) {
        // Default to SNO for OpenShift
        defaultProfileName = "aws-sno-test";
      } else if (selectedClusterType === ClusterType.EKS) {
        // Default to minimal EKS profile
        defaultProfileName = "eks-minimal";
      } else if (selectedClusterType === ClusterType.IKS) {
        // Default to minimal IKS profile
        defaultProfileName = "iks-minimal";
      }

      const defaultProfile = sortedProfiles.find(p => p.name === defaultProfileName);
      if (defaultProfile) {
        setValue("profile", defaultProfile.name);
      } else if (sortedProfiles.length > 0) {
        // Fallback to first available profile if default not found
        setValue("profile", sortedProfiles[0].name);
      }
    }
  }, [sortedProfiles, selectedClusterType, setValue]);

  // Update form defaults when profile changes
  useEffect(() => {
    if (selectedProfile) {
      // Set version based on cluster type
      const defaultVersion = watchedValues.cluster_type === "openshift"
        ? selectedProfile.openshift_versions?.default
        : selectedProfile.kubernetes_versions?.default;
      if (defaultVersion) {
        setValue("version", defaultVersion);
      }

      setValue("region", selectedProfile.regions.default);

      // Only set base_domain for OpenShift clusters
      if (watchedValues.cluster_type === "openshift" && selectedProfile.base_domains?.default) {
        setValue("base_domain", selectedProfile.base_domains.default);
      }

      setValue("ttl_hours", selectedProfile.lifecycle.default_ttl_hours);
    }
  }, [selectedProfile, setValue, watchedValues.cluster_type]);

  const onSubmit = async (data: CreateClusterFormData) => {
    // Clear previous errors
    setApiValidationErrors([]);
    setGeneralError("");

    try {
      // Prepare the payload
      const payload: any = { ...data };

      // Remove override_work_hours from payload (it's only for UI)
      delete payload.override_work_hours;

      // Handle credentials_mode: empty string = auto-detect = don't send to API
      if (payload.credentials_mode === "") {
        delete payload.credentials_mode;
      }

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
          Request a new Kubernetes cluster (OpenShift, EKS, or IKS)
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
                    const newPlatform = value as Platform;
                    setValue("platform", newPlatform);
                    setSelectedPlatform(newPlatform);
                    setValue("profile", ""); // Reset profile when platform changes

                    // Reset cluster type if incompatible with new platform
                    const currentClusterType = watchedValues.cluster_type;
                    if (newPlatform === Platform.AWS && currentClusterType === ClusterType.IKS) {
                      setValue("cluster_type", ClusterType.OpenShift);
                      setSelectedClusterType(ClusterType.OpenShift);
                    } else if (newPlatform === Platform.IBMCloud && currentClusterType === ClusterType.EKS) {
                      setValue("cluster_type", ClusterType.OpenShift);
                      setSelectedClusterType(ClusterType.OpenShift);
                    }
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

              <div className="space-y-2">
                <Label htmlFor="cluster_type">Cluster Type</Label>
                <Select
                  value={watchedValues.cluster_type || ""}
                  onValueChange={(value) => {
                    const clusterType = value as ClusterType;
                    setValue("cluster_type", clusterType);
                    setSelectedClusterType(clusterType);
                    setValue("profile", ""); // Reset profile when cluster type changes

                    // Auto-select the correct platform for EKS/IKS
                    if (clusterType === ClusterType.EKS) {
                      setValue("platform", Platform.AWS);
                      setSelectedPlatform(Platform.AWS);
                    } else if (clusterType === ClusterType.IKS) {
                      setValue("platform", Platform.IBMCloud);
                      setSelectedPlatform(Platform.IBMCloud);
                    }
                  }}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="Select cluster type" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="openshift">OpenShift</SelectItem>
                    {selectedPlatform === Platform.AWS && (
                      <SelectItem value="eks">Amazon EKS</SelectItem>
                    )}
                    {selectedPlatform === Platform.IBMCloud && (
                      <SelectItem value="iks">IBM Cloud IKS</SelectItem>
                    )}
                  </SelectContent>
                </Select>
                <p className="text-sm text-muted-foreground">
                  {watchedValues.cluster_type === "openshift" && "Red Hat OpenShift Container Platform"}
                  {watchedValues.cluster_type === "eks" && "AWS Elastic Kubernetes Service (managed Kubernetes)"}
                  {watchedValues.cluster_type === "iks" && "IBM Cloud Kubernetes Service (managed Kubernetes)"}
                </p>
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
                  <>
                    <p className="text-sm text-muted-foreground">
                      {selectedProfile.description}
                    </p>
                    {(selectedProfile.cost_controls?.estimated_hourly_cost ?? 0) >= 4 && (
                      <div className="flex items-start gap-2 p-3 bg-amber-50 dark:bg-amber-950 border border-amber-200 dark:border-amber-800 rounded-md">
                        <AlertCircle className="h-4 w-4 text-amber-600 dark:text-amber-400 mt-0.5 flex-shrink-0" />
                        <div className="text-sm space-y-1">
                          <p className="font-medium text-amber-900 dark:text-amber-100">
                            High-Cost Profile Warning
                          </p>
                          <p className="text-amber-800 dark:text-amber-200">
                            {selectedProfile.cost_controls?.warning_message ||
                             `This profile costs $${selectedProfile.cost_controls?.estimated_hourly_cost}/hr (~$${Math.round((selectedProfile.cost_controls?.estimated_hourly_cost ?? 0) * 24 * 30)}/month). Consider enabling work hours hibernation to reduce costs by ~66%.`}
                          </p>
                        </div>
                      </div>
                    )}
                    {selectedProfile.deployment_metrics && selectedProfile.deployment_metrics.sample_count >= 5 && (
                      <div className="flex items-start gap-2 p-3 bg-blue-50 dark:bg-blue-950 border border-blue-200 dark:border-blue-800 rounded-md">
                        <Clock className="h-4 w-4 text-blue-600 dark:text-blue-400 mt-0.5 flex-shrink-0" />
                        <div className="text-sm space-y-1">
                          <p className="font-medium text-blue-900 dark:text-blue-100">
                            Estimated Deployment Time
                          </p>
                          <p className="text-blue-800 dark:text-blue-200">
                            Typical: ~{Math.round(selectedProfile.deployment_metrics.p50_duration_seconds ? selectedProfile.deployment_metrics.p50_duration_seconds / 60 : selectedProfile.deployment_metrics.avg_duration_seconds / 60)} minutes
                            {selectedProfile.deployment_metrics.p95_duration_seconds && (
                              <span> (up to {Math.round(selectedProfile.deployment_metrics.p95_duration_seconds / 60)} minutes)</span>
                            )}
                          </p>
                          <p className="text-xs text-blue-600 dark:text-blue-400">
                            Based on {selectedProfile.deployment_metrics.sample_count} recent deployments
                          </p>
                        </div>
                      </div>
                    )}
                  </>
                )}
              </div>

              {selectedProfile && (selectedProfile.openshift_versions?.allowed || selectedProfile.kubernetes_versions?.allowed) && (
                <div className="space-y-2">
                  <Label htmlFor="version">
                    {watchedValues.cluster_type === "openshift" ? "OpenShift Version" : "Kubernetes Version"}
                  </Label>
                  <Select
                    value={watchedValues.version || ""}
                    onValueChange={(value) => setValue("version", value)}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {(watchedValues.cluster_type === "openshift"
                        ? selectedProfile.openshift_versions?.allowed
                        : selectedProfile.kubernetes_versions?.allowed
                      )?.map((version) => (
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

                {watchedValues.cluster_type === "openshift" && selectedProfile.base_domains?.allowed && (
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
                        {selectedProfile.base_domains.allowed.map((domain) => (
                          <SelectItem key={domain} value={domain}>
                            {domain}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                )}

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

                {/* Cloud Credentials */}
                {watchedValues.cluster_type === "openshift" && watchedValues.platform === "aws" && (
                  <div className="space-y-4">
                    <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wide">
                      Cloud Credentials
                    </h3>
                    <div className="space-y-2">
                      <Label htmlFor="credentials_mode">Credentials Mode</Label>
                      <Select
                        value={watchedValues.credentials_mode || "Manual"}
                        onValueChange={(value) => setValue("credentials_mode", value as "Manual" | "Passthrough" | "Mint" | "")}
                      >
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="Manual">Manual (default)</SelectItem>
                          <SelectItem value="">Auto-detect (recommended for 4.22+)</SelectItem>
                          <SelectItem value="Passthrough">Passthrough</SelectItem>
                          <SelectItem value="Mint">Mint</SelectItem>
                        </SelectContent>
                      </Select>
                      <p className="text-sm text-muted-foreground">
                        {watchedValues.credentials_mode === "Manual" && (
                          "Manually manage cloud credentials. Default for 4.21 and earlier."
                        )}
                        {watchedValues.credentials_mode === "" && (
                          <>
                            Let the installer auto-detect credential mode. <span className="font-semibold text-amber-600 dark:text-amber-400">Required for OpenShift 4.22.0-ec.5 due to bootstrap bug.</span>
                          </>
                        )}
                        {watchedValues.credentials_mode === "Passthrough" && (
                          "Pass full cloud credentials to all components."
                        )}
                        {watchedValues.credentials_mode === "Mint" && (
                          "Let Cloud Credential Operator create limited credentials automatically."
                        )}
                      </p>
                      {watchedValues.version?.includes("4.22") && watchedValues.credentials_mode === "Manual" && (
                        <div className="flex items-start gap-2 p-3 bg-amber-50 dark:bg-amber-950 border border-amber-200 dark:border-amber-800 rounded-md">
                          <AlertCircle className="h-4 w-4 text-amber-600 dark:text-amber-400 mt-0.5 flex-shrink-0" />
                          <div className="text-sm space-y-1">
                            <p className="font-medium text-amber-900 dark:text-amber-100">
                              OpenShift 4.22 Compatibility Warning
                            </p>
                            <p className="text-amber-800 dark:text-amber-200">
                              Manual mode has a known issue with OpenShift 4.22.0-ec.5 that causes bootstrap to hang.
                              Consider using <span className="font-semibold">Auto-detect</span> instead.
                            </p>
                          </div>
                        </div>
                      )}
                    </div>
                  </div>
                )}

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
                      Add custom tags to apply to all deployed {watchedValues.platform === "aws" ? "AWS" : "IBM Cloud"} resources
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

                    {/* EFS Storage - AWS only */}
                    {watchedValues.platform === "aws" && (
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
                    )}

                    {selectedProfile?.post_deployment?.enabled && (
                      <div className="flex items-start space-x-2">
                        <Checkbox
                          id="skip_post_deployment"
                          checked={watchedValues.skip_post_deployment}
                          onCheckedChange={(checked) =>
                            setValue("skip_post_deployment", checked as boolean)
                          }
                          className="mt-1"
                        />
                        <div className="flex-1">
                          <Label htmlFor="skip_post_deployment" className="cursor-pointer">
                            Skip automatic post-deployment configuration
                          </Label>
                          <p className="text-sm text-muted-foreground">
                            Skip automatic operator/manifest installation configured for this profile
                            {selectedProfile.post_deployment.operators && selectedProfile.post_deployment.operators.length > 0 && (
                              <span className="block mt-1">
                                Will skip: {selectedProfile.post_deployment.operators.map(op => op.name).join(", ")}
                              </span>
                            )}
                          </p>
                          {getFieldError("skip_post_deployment") && (
                            <p className="text-sm text-red-600 mt-1">
                              {getFieldError("skip_post_deployment")}
                            </p>
                          )}
                        </div>
                      </div>
                    )}

                    <div className="flex items-start space-x-2">
                      <Checkbox
                        id="preserve_on_failure"
                        checked={watchedValues.preserve_on_failure}
                        onCheckedChange={(checked) =>
                          setValue("preserve_on_failure", checked as boolean)
                        }
                        className="mt-1"
                      />
                      <div className="flex-1">
                        <Label htmlFor="preserve_on_failure" className="cursor-pointer">
                          Preserve resources on failure (debugging)
                        </Label>
                        <p className="text-sm text-muted-foreground">
                          Keep cluster resources and work directories when installation fails for debugging purposes
                        </p>
                        {getFieldError("preserve_on_failure") && (
                          <p className="text-sm text-red-600 mt-1">
                            {getFieldError("preserve_on_failure")}
                          </p>
                        )}
                      </div>
                    </div>
                  </div>
                </div>

                {/* User-Defined Post-Configuration */}
                <div className="space-y-4">
                  <h3 className="text-sm font-semibold text-muted-foreground uppercase tracking-wide">
                    User-Defined Post-Configuration
                  </h3>
                  <div className="space-y-3">
                    <p className="text-sm text-muted-foreground">
                      Customize cluster post-deployment with add-ons, templates, or custom configurations.
                      Configure operators, scripts, manifests, and Helm charts to be installed after cluster creation.
                    </p>
                    <CustomPostConfigEditor
                      platform={watchedValues.cluster_type}
                      value={watchedValues.customPostConfig}
                      selectedAddons={watchedValues.postConfigAddOns || []}
                      onAddonsChange={(addonIds) => setValue("postConfigAddOns", addonIds)}
                      onConfigChange={(config) => setValue("customPostConfig", config)}
                    />
                  </div>
                </div>

                {/* Work Hours Override */}
                <div className="space-y-4">
                  <div className="space-y-3">
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
