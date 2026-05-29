"use client";

import { useState, useEffect } from "react";
import { useParams, useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { ArrowLeft, Plus, Trash2, AlertCircle } from "lucide-react";
import { useAddon, useUpdateAddon } from "@/lib/hooks/useAddons";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Card } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { toast } from "sonner";

const CATEGORIES = [
  "backup",
  "migration",
  "virtualization",
  "monitoring",
  "security",
  "storage",
  "networking",
  "cicd",
];

const PLATFORMS = ["openshift", "eks", "gke", "iks"];

// Form schema
const addonSchema = z.object({
  name: z.string().min(2, "Name must be at least 2 characters"),
  description: z.string().min(10, "Description must be at least 10 characters"),
  category: z.enum(CATEGORIES as [string, ...string[]]),
  supported_platforms: z.array(z.string()).min(1, "Select at least one platform"),
  enabled: z.boolean(),
  display_name: z.string().min(1, "Display name is required"),
});

type AddonFormData = z.infer<typeof addonSchema>;

export default function EditAddonPage() {
  const params = useParams();
  const router = useRouter();
  const addonId = params.id as string;

  const { data: addon, isLoading, error } = useAddon(addonId);
  const updateAddon = useUpdateAddon();

  const [operators, setOperators] = useState<Array<{
    name: string;
    namespace: string;
    source?: string;
    channel?: string;
  }>>([]);

  const [scripts, setScripts] = useState<Array<{
    name: string;
    path: string;
    timeout?: string;
  }>>([]);

  const [manifests, setManifests] = useState<Array<{
    name: string;
    content?: string;
    path?: string;
  }>>([]);

  const {
    register,
    handleSubmit,
    watch,
    setValue,
    reset,
    formState: { errors },
  } = useForm<AddonFormData>({
    resolver: zodResolver(addonSchema),
  });

  const selectedPlatforms = watch("supported_platforms") || [];

  // Load addon data into form
  useEffect(() => {
    if (addon) {
      reset({
        name: addon.name,
        description: addon.description,
        category: addon.category as any,
        supported_platforms: addon.supportedPlatforms,
        enabled: addon.enabled,
        display_name: addon.displayName,
      });

      // Load configuration
      if (addon.config.operators) {
        setOperators(addon.config.operators);
      }
      if (addon.config.scripts) {
        setScripts(addon.config.scripts);
      }
      if (addon.config.manifests) {
        setManifests(addon.config.manifests);
      }
    }
  }, [addon, reset]);

  const togglePlatform = (platform: string) => {
    const current = selectedPlatforms;
    const updated = current.includes(platform)
      ? current.filter((p) => p !== platform)
      : [...current, platform];
    setValue("supported_platforms", updated);
  };

  const onSubmit = async (data: AddonFormData) => {
    try {
      const payload = {
        ...data,
        config: {
          operators: operators.length > 0 ? operators : undefined,
          scripts: scripts.length > 0 ? scripts : undefined,
          manifests: manifests.length > 0 ? manifests : undefined,
        },
      };

      await updateAddon.mutateAsync({ id: addonId, data: payload });
      toast.success("Addon updated successfully");
      router.push(`/addons/${addonId}`);
    } catch (error) {
      toast.error("Failed to update addon");
      console.error(error);
    }
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center min-h-[400px]">
        <div className="text-muted-foreground">Loading addon...</div>
      </div>
    );
  }

  if (error || !addon) {
    return (
      <div className="space-y-6">
        <Button variant="ghost" onClick={() => router.back()}>
          <ArrowLeft className="h-4 w-4 mr-2" />
          Back
        </Button>
        <Card className="p-8 border-destructive">
          <div className="flex items-center gap-2 text-destructive">
            <AlertCircle className="h-5 w-5" />
            <p>Failed to load addon: {error instanceof Error ? error.message : "Unknown error"}</p>
          </div>
        </Card>
      </div>
    );
  }

  // Check if addon can be edited
  if (addon.isPublished || addon.addonSource === "system") {
    return (
      <div className="space-y-6">
        <Button variant="ghost" onClick={() => router.back()}>
          <ArrowLeft className="h-4 w-4 mr-2" />
          Back
        </Button>
        <Card className="p-8 border-yellow-500">
          <div className="flex items-center gap-2 text-yellow-600 dark:text-yellow-500">
            <AlertCircle className="h-5 w-5" />
            <div>
              <p className="font-semibold">This addon cannot be edited</p>
              <p className="text-sm mt-1">
                {addon.isPublished
                  ? "Published addons are immutable. Clone this addon to create a new editable version."
                  : "System addons cannot be edited through the UI."}
              </p>
            </div>
          </div>
          <div className="mt-4">
            <Button onClick={() => router.push(`/addons/${addonId}`)}>
              View Addon Details
            </Button>
          </div>
        </Card>
      </div>
    );
  }

  return (
    <div className="space-y-6 max-w-4xl">
      <div className="flex items-center gap-4">
        <Button variant="ghost" onClick={() => router.back()}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <div>
          <h1 className="text-3xl font-bold">Edit Addon</h1>
          <p className="text-muted-foreground mt-1">{addon.addonId}</p>
        </div>
      </div>

      <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
        {/* Basic Information */}
        <Card className="p-6">
          <h2 className="text-xl font-semibold mb-4">Basic Information</h2>
          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="addon_id">Addon ID</Label>
                <Input
                  id="addon_id"
                  value={addon.addonId}
                  disabled
                  className="bg-muted"
                />
                <p className="text-xs text-muted-foreground">
                  Addon ID cannot be changed
                </p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="name">
                  Name <span className="text-red-500">*</span>
                </Label>
                <Input
                  id="name"
                  placeholder="My Custom Addon"
                  {...register("name")}
                />
                {errors.name && (
                  <p className="text-sm text-red-600">{errors.name.message}</p>
                )}
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="description">
                Description <span className="text-red-500">*</span>
              </Label>
              <Textarea
                id="description"
                placeholder="Describe what this addon does..."
                rows={3}
                {...register("description")}
              />
              {errors.description && (
                <p className="text-sm text-red-600">{errors.description.message}</p>
              )}
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="category">
                  Category <span className="text-red-500">*</span>
                </Label>
                <Select
                  value={watch("category")}
                  onValueChange={(value) => setValue("category", value as any)}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="Select category" />
                  </SelectTrigger>
                  <SelectContent>
                    {CATEGORIES.map((cat) => (
                      <SelectItem key={cat} value={cat}>
                        {cat.charAt(0).toUpperCase() + cat.slice(1)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                {errors.category && (
                  <p className="text-sm text-red-600">{errors.category.message}</p>
                )}
              </div>

              <div className="space-y-2">
                <Label>
                  Supported Platforms <span className="text-red-500">*</span>
                </Label>
                <div className="flex gap-4">
                  {PLATFORMS.map((platform) => (
                    <div key={platform} className="flex items-center space-x-2">
                      <Checkbox
                        id={platform}
                        checked={selectedPlatforms.includes(platform)}
                        onCheckedChange={() => togglePlatform(platform)}
                      />
                      <Label
                        htmlFor={platform}
                        className="text-sm font-normal cursor-pointer"
                      >
                        {platform.toUpperCase()}
                      </Label>
                    </div>
                  ))}
                </div>
                {errors.supported_platforms && (
                  <p className="text-sm text-red-600">
                    {errors.supported_platforms.message}
                  </p>
                )}
              </div>
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="version">Version/Channel</Label>
                <Input
                  id="version"
                  value={addon.version}
                  disabled
                  className="bg-muted"
                />
                <p className="text-xs text-muted-foreground">
                  Version cannot be changed
                </p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="display_name">
                  Display Name <span className="text-red-500">*</span>
                </Label>
                <Input
                  id="display_name"
                  placeholder="My Addon v1.0"
                  {...register("display_name")}
                />
                {errors.display_name && (
                  <p className="text-sm text-red-600">{errors.display_name.message}</p>
                )}
              </div>
            </div>

            <div className="flex items-center space-x-2">
              <Checkbox
                id="enabled"
                checked={watch("enabled")}
                onCheckedChange={(checked) => setValue("enabled", checked as boolean)}
              />
              <Label htmlFor="enabled" className="text-sm font-normal cursor-pointer">
                Enable this addon (users can select it for their clusters)
              </Label>
            </div>
          </div>
        </Card>

        {/* Operators Configuration */}
        <Card className="p-6">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-xl font-semibold">Operators</h2>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() =>
                setOperators([...operators, { name: "", namespace: "" }])
              }
            >
              <Plus className="h-4 w-4 mr-2" />
              Add Operator
            </Button>
          </div>

          {operators.length === 0 ? (
            <p className="text-muted-foreground text-sm">
              No operators configured. Click &quot;Add Operator&quot; to add one.
            </p>
          ) : (
            <div className="space-y-4">
              {operators.map((op, index) => (
                <div key={index} className="border rounded-lg p-4 space-y-3">
                  <div className="flex justify-between items-start">
                    <h3 className="font-medium">Operator {index + 1}</h3>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() =>
                        setOperators(operators.filter((_, i) => i !== index))
                      }
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                  <div className="grid grid-cols-2 gap-3">
                    <div>
                      <Label htmlFor={`op-name-${index}`}>Name *</Label>
                      <Input
                        id={`op-name-${index}`}
                        value={op.name}
                        onChange={(e) => {
                          const updated = [...operators];
                          updated[index].name = e.target.value;
                          setOperators(updated);
                        }}
                        placeholder="operator-name"
                      />
                    </div>
                    <div>
                      <Label htmlFor={`op-namespace-${index}`}>Namespace *</Label>
                      <Input
                        id={`op-namespace-${index}`}
                        value={op.namespace}
                        onChange={(e) => {
                          const updated = [...operators];
                          updated[index].namespace = e.target.value;
                          setOperators(updated);
                        }}
                        placeholder="openshift-operators"
                      />
                    </div>
                    <div>
                      <Label htmlFor={`op-source-${index}`}>Source</Label>
                      <Input
                        id={`op-source-${index}`}
                        value={op.source || ""}
                        onChange={(e) => {
                          const updated = [...operators];
                          updated[index].source = e.target.value;
                          setOperators(updated);
                        }}
                        placeholder="redhat-operators"
                      />
                    </div>
                    <div>
                      <Label htmlFor={`op-channel-${index}`}>Channel</Label>
                      <Input
                        id={`op-channel-${index}`}
                        value={op.channel || ""}
                        onChange={(e) => {
                          const updated = [...operators];
                          updated[index].channel = e.target.value;
                          setOperators(updated);
                        }}
                        placeholder="stable"
                      />
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </Card>

        {/* Scripts Configuration */}
        <Card className="p-6">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-xl font-semibold">Scripts</h2>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setScripts([...scripts, { name: "", path: "" }])}
            >
              <Plus className="h-4 w-4 mr-2" />
              Add Script
            </Button>
          </div>

          {scripts.length === 0 ? (
            <p className="text-muted-foreground text-sm">
              No scripts configured. Click &quot;Add Script&quot; to add one.
            </p>
          ) : (
            <div className="space-y-4">
              {scripts.map((script, index) => (
                <div key={index} className="border rounded-lg p-4 space-y-3">
                  <div className="flex justify-between items-start">
                    <h3 className="font-medium">Script {index + 1}</h3>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() =>
                        setScripts(scripts.filter((_, i) => i !== index))
                      }
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                  <div className="grid grid-cols-3 gap-3">
                    <div>
                      <Label htmlFor={`script-name-${index}`}>Name *</Label>
                      <Input
                        id={`script-name-${index}`}
                        value={script.name}
                        onChange={(e) => {
                          const updated = [...scripts];
                          updated[index].name = e.target.value;
                          setScripts(updated);
                        }}
                        placeholder="setup-script"
                      />
                    </div>
                    <div>
                      <Label htmlFor={`script-path-${index}`}>Path *</Label>
                      <Input
                        id={`script-path-${index}`}
                        value={script.path}
                        onChange={(e) => {
                          const updated = [...scripts];
                          updated[index].path = e.target.value;
                          setScripts(updated);
                        }}
                        placeholder="/scripts/setup.sh"
                      />
                    </div>
                    <div>
                      <Label htmlFor={`script-timeout-${index}`}>Timeout</Label>
                      <Input
                        id={`script-timeout-${index}`}
                        value={script.timeout || ""}
                        onChange={(e) => {
                          const updated = [...scripts];
                          updated[index].timeout = e.target.value;
                          setScripts(updated);
                        }}
                        placeholder="5m"
                      />
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </Card>

        {/* Manifests Configuration */}
        <Card className="p-6">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-xl font-semibold">Manifests</h2>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setManifests([...manifests, { name: "" }])}
            >
              <Plus className="h-4 w-4 mr-2" />
              Add Manifest
            </Button>
          </div>

          {manifests.length === 0 ? (
            <p className="text-muted-foreground text-sm">
              No manifests configured. Click &quot;Add Manifest&quot; to add one.
            </p>
          ) : (
            <div className="space-y-4">
              {manifests.map((manifest, index) => (
                <div key={index} className="border rounded-lg p-4 space-y-3">
                  <div className="flex justify-between items-start">
                    <h3 className="font-medium">Manifest {index + 1}</h3>
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={() =>
                        setManifests(manifests.filter((_, i) => i !== index))
                      }
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                  <div className="space-y-3">
                    <div>
                      <Label htmlFor={`manifest-name-${index}`}>Name *</Label>
                      <Input
                        id={`manifest-name-${index}`}
                        value={manifest.name}
                        onChange={(e) => {
                          const updated = [...manifests];
                          updated[index].name = e.target.value;
                          setManifests(updated);
                        }}
                        placeholder="custom-manifest"
                      />
                    </div>
                    <div>
                      <Label htmlFor={`manifest-path-${index}`}>Path</Label>
                      <Input
                        id={`manifest-path-${index}`}
                        value={manifest.path || ""}
                        onChange={(e) => {
                          const updated = [...manifests];
                          updated[index].path = e.target.value;
                          setManifests(updated);
                        }}
                        placeholder="/manifests/custom.yaml"
                      />
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </Card>

        {/* Form Actions */}
        <div className="flex justify-end gap-4">
          <Button
            type="button"
            variant="outline"
            onClick={() => router.back()}
          >
            Cancel
          </Button>
          <Button type="submit" disabled={updateAddon.isPending}>
            {updateAddon.isPending ? "Saving..." : "Save Changes"}
          </Button>
        </div>
      </form>
    </div>
  );
}
