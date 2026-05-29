"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import { ArrowLeft, Plus, Trash2 } from "lucide-react";
import { useCreateAddon } from "@/lib/hooks/useAddons";
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
  addonId: z
    .string()
    .min(2, "Addon ID must be at least 2 characters")
    .regex(/^[a-z0-9-]+$/, "Addon ID must contain only lowercase letters, numbers, and hyphens"),
  name: z.string().min(2, "Name must be at least 2 characters"),
  description: z.string().min(10, "Description must be at least 10 characters"),
  category: z.enum(CATEGORIES as [string, ...string[]]),
  supportedPlatforms: z.array(z.string()).min(1, "Select at least one platform"),
  enabled: z.boolean(),
  version: z.string().min(1, "Version is required"),
  displayName: z.string().min(1, "Display name is required"),
});

type AddonFormData = z.infer<typeof addonSchema>;

export default function NewAddonPage() {
  const router = useRouter();
  const createAddon = useCreateAddon();

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
    formState: { errors },
  } = useForm<AddonFormData>({
    resolver: zodResolver(addonSchema),
    defaultValues: {
      enabled: true,
      supportedPlatforms: [],
    },
  });

  const selectedPlatforms = watch("supportedPlatforms") || [];

  const togglePlatform = (platform: string) => {
    const current = selectedPlatforms;
    const updated = current.includes(platform)
      ? current.filter((p) => p !== platform)
      : [...current, platform];
    setValue("supportedPlatforms", updated);
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

      const createdAddon = await createAddon.mutateAsync(payload);
      toast.success("Addon created successfully");
      router.push(`/addons/${createdAddon.id}`);
    } catch (error) {
      toast.error("Failed to create addon");
      console.error(error);
    }
  };

  return (
    <div className="space-y-6 max-w-4xl">
      <div className="flex items-center gap-4">
        <Button variant="ghost" onClick={() => router.back()}>
          <ArrowLeft className="h-4 w-4" />
        </Button>
        <div>
          <h1 className="text-3xl font-bold">Create Addon</h1>
          <p className="text-muted-foreground mt-1">
            Create a custom addon for post-deployment configuration
          </p>
        </div>
      </div>

      <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
        {/* Basic Information */}
        <Card className="p-6">
          <h2 className="text-xl font-semibold mb-4">Basic Information</h2>
          <div className="space-y-4">
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="addonId">
                  Addon ID <span className="text-red-500">*</span>
                </Label>
                <Input
                  id="addonId"
                  placeholder="my-custom-addon"
                  {...register("addonId")}
                />
                {errors.addonId && (
                  <p className="text-sm text-red-600">{errors.addonId.message}</p>
                )}
                <p className="text-xs text-muted-foreground">
                  Lowercase letters, numbers, and hyphens only
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
                <Select onValueChange={(value) => setValue("category", value as any)}>
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
                {errors.supportedPlatforms && (
                  <p className="text-sm text-red-600">
                    {errors.supportedPlatforms.message}
                  </p>
                )}
              </div>
            </div>

            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label htmlFor="version">
                  Version/Channel <span className="text-red-500">*</span>
                </Label>
                <Input
                  id="version"
                  placeholder="v1.0 or stable"
                  {...register("version")}
                />
                {errors.version && (
                  <p className="text-sm text-red-600">{errors.version.message}</p>
                )}
              </div>

              <div className="space-y-2">
                <Label htmlFor="displayName">
                  Display Name <span className="text-red-500">*</span>
                </Label>
                <Input
                  id="displayName"
                  placeholder="My Addon v1.0"
                  {...register("displayName")}
                />
                {errors.displayName && (
                  <p className="text-sm text-red-600">{errors.displayName.message}</p>
                )}
              </div>
            </div>

            <div className="flex items-center space-x-2">
              <Checkbox
                id="enabled"
                defaultChecked={true}
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
          <Button type="submit" disabled={createAddon.isPending}>
            {createAddon.isPending ? "Creating..." : "Create Addon"}
          </Button>
        </div>
      </form>
    </div>
  );
}
