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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { ExecutionPanel } from "@/components/clusters/ClusterForm/ExecutionPanel";
import { Platform } from "@/types/api";

export default function NewClusterPage() {
  const router = useRouter();
  const { user } = useAuthStore();
  const [selectedPlatform, setSelectedPlatform] = useState<Platform>(Platform.AWS);
  const { data: profiles } = useProfiles(selectedPlatform);
  const createCluster = useCreateCluster();

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
      offhours_opt_in: false,
    },
  });

  const watchedValues = watch();
  const selectedProfile = profiles?.find((p) => p.name === watchedValues.profile);

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
    try {
      const result = await createCluster.mutateAsync(data);
      router.push(`/clusters/${result.id}`);
    } catch (error) {
      console.error("Failed to create cluster:", error);
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
                    {profiles && profiles.length > 0 ? (
                      profiles.map((profile) => (
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
                  <Input
                    id="team"
                    placeholder="engineering"
                    {...register("team")}
                  />
                  {errors.team && (
                    <p className="text-sm text-red-600">{errors.team.message}</p>
                  )}
                </div>

                <div className="space-y-2">
                  <Label htmlFor="cost_center">Cost Center</Label>
                  <Input
                    id="cost_center"
                    placeholder="eng-001"
                    {...register("cost_center")}
                  />
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
                    min={1}
                    max={selectedProfile.lifecycle.max_ttl_hours}
                    {...register("ttl_hours", { valueAsNumber: true })}
                  />
                  <p className="text-sm text-muted-foreground">
                    Max: {selectedProfile.lifecycle.max_ttl_hours} hours
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
              <div className="rounded-lg border bg-card p-6 space-y-4">
                <h2 className="text-lg font-semibold">Advanced</h2>

                <div className="space-y-2">
                  <Label htmlFor="ssh_public_key">SSH Public Key (Optional)</Label>
                  <Textarea
                    id="ssh_public_key"
                    placeholder="ssh-rsa AAAA..."
                    rows={3}
                    {...register("ssh_public_key")}
                  />
                </div>

                <div className="flex items-center space-x-2">
                  <Checkbox
                    id="offhours_opt_in"
                    checked={watchedValues.offhours_opt_in}
                    onCheckedChange={(checked) =>
                      setValue("offhours_opt_in", checked as boolean)
                    }
                  />
                  <Label htmlFor="offhours_opt_in" className="cursor-pointer">
                    Enable off-hours scaling
                  </Label>
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

            {createCluster.isError && (
              <div className="text-sm text-red-600 bg-red-50 p-3 rounded-md">
                Failed to create cluster. Please try again.
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
