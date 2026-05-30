"use client";

import { useState } from "react";
import { useParams, useRouter } from "next/navigation";
import {
  PackageCheck,
  ArrowLeft,
  Edit,
  Trash2,
  Copy,
  Upload,
  AlertCircle,
  CheckCircle,
  Clock,
} from "lucide-react";
import { useAddon, useDeleteAddon, usePublishAddon, useCloneAddon } from "@/lib/hooks/useAddons";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
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
import { toast } from "sonner";
import Link from "next/link";
import { DependencyGraph } from "@/components/addons/DependencyGraph";

export default function AddonDetailPage() {
  const params = useParams();
  const router = useRouter();
  const addonId = params.id as string;

  const { data: addon, isLoading, error } = useAddon(addonId);
  const deleteAddon = useDeleteAddon();
  const publishAddon = usePublishAddon();
  const cloneAddon = useCloneAddon();

  const [showDeleteDialog, setShowDeleteDialog] = useState(false);
  const [showPublishDialog, setShowPublishDialog] = useState(false);

  const formatCategoryName = (category: string) => {
    if (category === "cicd") return "CI/CD";
    return category.charAt(0).toUpperCase() + category.slice(1);
  };

  const handleDelete = async () => {
    try {
      await deleteAddon.mutateAsync(addonId);
      toast.success("Addon deleted successfully");
      router.push("/addons");
    } catch (error) {
      toast.error("Failed to delete addon");
      console.error(error);
    }
  };

  const handlePublish = async () => {
    try {
      await publishAddon.mutateAsync(addonId);
      toast.success("Addon published successfully");
      setShowPublishDialog(false);
    } catch (error) {
      toast.error("Failed to publish addon");
      console.error(error);
    }
  };

  const handleClone = async () => {
    try {
      const clonedAddon = await cloneAddon.mutateAsync(addonId);
      toast.success("Addon cloned successfully");
      router.push(`/addons/${clonedAddon.id}`);
    } catch (error) {
      toast.error("Failed to clone addon");
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

  const isUserAddon = addon.addonSource === "user";
  const canEdit = isUserAddon && !addon.isPublished;
  const canPublish = isUserAddon && !addon.isPublished;
  const canDelete = isUserAddon;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Button variant="ghost" onClick={() => router.back()}>
            <ArrowLeft className="h-4 w-4" />
          </Button>
          <div>
            <div className="flex items-center gap-2">
              <PackageCheck className="h-6 w-6" />
              <h1 className="text-3xl font-bold">{addon.name}</h1>
            </div>
            <p className="text-muted-foreground mt-1">{addon.addonId}</p>
          </div>
        </div>

        <div className="flex gap-2">
          {canEdit && (
            <Link href={`/addons/${addon.id}/edit`}>
              <Button variant="outline">
                <Edit className="h-4 w-4 mr-2" />
                Edit
              </Button>
            </Link>
          )}
          {canPublish && (
            <Button onClick={() => setShowPublishDialog(true)}>
              <Upload className="h-4 w-4 mr-2" />
              Publish
            </Button>
          )}
          <Button variant="outline" onClick={handleClone}>
            <Copy className="h-4 w-4 mr-2" />
            Clone
          </Button>
          {canDelete && (
            <Button
              variant="outline"
              onClick={() => setShowDeleteDialog(true)}
              className="text-destructive hover:text-destructive"
            >
              <Trash2 className="h-4 w-4 mr-2" />
              Delete
            </Button>
          )}
        </div>
      </div>

      {/* Status Badges */}
      <div className="flex gap-2">
        {addon.addonSource === "system" && (
          <Badge variant="secondary">System Addon</Badge>
        )}
        {addon.isPublished && addon.addonSource === "user" && (
          <Badge variant="default">
            <CheckCircle className="h-3 w-3 mr-1" />
            Published
          </Badge>
        )}
        {!addon.isPublished && addon.addonSource === "user" && (
          <Badge variant="outline">
            <Clock className="h-3 w-3 mr-1" />
            Draft
          </Badge>
        )}
        {addon.isImmutable && <Badge variant="secondary">Immutable</Badge>}
      </div>

      {/* Main Content */}
      <Tabs defaultValue="overview" className="space-y-4">
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="configuration">Configuration</TabsTrigger>
          <TabsTrigger value="dependencies">Dependencies</TabsTrigger>
          <TabsTrigger value="metadata">Metadata</TabsTrigger>
          <TabsTrigger value="history">Version History</TabsTrigger>
        </TabsList>

        {/* Overview Tab */}
        <TabsContent value="overview" className="space-y-4">
          <Card className="p-6">
            <h2 className="text-xl font-semibold mb-4">Description</h2>
            <p className="text-muted-foreground">{addon.description}</p>
          </Card>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <Card className="p-6">
              <h3 className="font-semibold mb-3">Details</h3>
              <dl className="space-y-2">
                <div className="flex justify-between">
                  <dt className="text-sm text-muted-foreground">Category</dt>
                  <dd className="text-sm font-medium">{formatCategoryName(addon.category)}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-sm text-muted-foreground">Version</dt>
                  <dd className="text-sm font-medium">{addon.displayName || addon.version}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-sm text-muted-foreground">Version Number</dt>
                  <dd className="text-sm font-medium">v{addon.versionNumber}</dd>
                </div>
                <div className="flex justify-between">
                  <dt className="text-sm text-muted-foreground">Enabled</dt>
                  <dd className="text-sm font-medium">
                    {addon.enabled ? (
                      <Badge variant="default" className="text-xs">Yes</Badge>
                    ) : (
                      <Badge variant="secondary" className="text-xs">No</Badge>
                    )}
                  </dd>
                </div>
              </dl>
            </Card>

            <Card className="p-6">
              <h3 className="font-semibold mb-3">Supported Platforms</h3>
              <div className="flex flex-wrap gap-2">
                {addon.supportedPlatforms.map((platform) => (
                  <Badge key={platform} variant="outline">
                    {platform}
                  </Badge>
                ))}
              </div>
            </Card>
          </div>

          {addon.metadata && (
            <Card className="p-6">
              <h3 className="font-semibold mb-3">Requirements & Notes</h3>
              <div className="space-y-4">
                {addon.metadata.requiresBareMetal && (
                  <div>
                    <Badge variant="secondary">Requires Bare Metal</Badge>
                  </div>
                )}
                {addon.metadata.requiredCapabilities && addon.metadata.requiredCapabilities.length > 0 && (
                  <div>
                    <h4 className="text-sm font-medium mb-2">Required Capabilities</h4>
                    <div className="flex flex-wrap gap-2">
                      {addon.metadata.requiredCapabilities.map((cap) => (
                        <Badge key={cap} variant="outline">{cap}</Badge>
                      ))}
                    </div>
                  </div>
                )}
                {addon.metadata.notes && addon.metadata.notes.length > 0 && (
                  <div>
                    <h4 className="text-sm font-medium mb-2">Notes</h4>
                    <ul className="list-disc list-inside space-y-1 text-sm text-muted-foreground">
                      {addon.metadata.notes.map((note, i) => (
                        <li key={i}>{note}</li>
                      ))}
                    </ul>
                  </div>
                )}
                {addon.metadata.warnings && addon.metadata.warnings.length > 0 && (
                  <div>
                    <h4 className="text-sm font-medium mb-2 text-yellow-600 dark:text-yellow-500">Warnings</h4>
                    <ul className="list-disc list-inside space-y-1 text-sm text-yellow-600 dark:text-yellow-500">
                      {addon.metadata.warnings.map((warning, i) => (
                        <li key={i}>{warning}</li>
                      ))}
                    </ul>
                  </div>
                )}
              </div>
            </Card>
          )}

          <Card className="p-6">
            <h3 className="font-semibold mb-3">Timestamps</h3>
            <dl className="space-y-2">
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Created</dt>
                <dd className="text-sm font-medium">
                  {new Date(addon.createdAt).toLocaleString()}
                </dd>
              </div>
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Updated</dt>
                <dd className="text-sm font-medium">
                  {new Date(addon.updatedAt).toLocaleString()}
                </dd>
              </div>
              {addon.publishedAt && (
                <div className="flex justify-between">
                  <dt className="text-sm text-muted-foreground">Published</dt>
                  <dd className="text-sm font-medium">
                    {new Date(addon.publishedAt).toLocaleString()}
                  </dd>
                </div>
              )}
            </dl>
          </Card>
        </TabsContent>

        {/* Configuration Tab */}
        <TabsContent value="configuration" className="space-y-4">
          <ConfigurationView config={addon.config} />
        </TabsContent>

        {/* Dependencies Tab */}
        <TabsContent value="dependencies" className="space-y-4">
          <DependencyGraph config={addon.config} />
        </TabsContent>

        {/* Metadata Tab */}
        <TabsContent value="metadata" className="space-y-4">
          <Card className="p-6">
            <pre className="text-sm overflow-auto">
              {JSON.stringify(addon.metadata || {}, null, 2)}
            </pre>
          </Card>
        </TabsContent>

        {/* Version History Tab */}
        <TabsContent value="history" className="space-y-4">
          <Card className="p-6">
            <h3 className="font-semibold mb-4">Version Information</h3>
            <dl className="space-y-2">
              <div className="flex justify-between">
                <dt className="text-sm text-muted-foreground">Version Number</dt>
                <dd className="text-sm font-medium">v{addon.versionNumber}</dd>
              </div>
              {addon.parentVersionId && (
                <div className="flex justify-between">
                  <dt className="text-sm text-muted-foreground">Parent Version</dt>
                  <dd className="text-sm font-medium">
                    <Link
                      href={`/addons/${addon.parentVersionId}`}
                      className="text-blue-600 hover:underline"
                    >
                      View Parent
                    </Link>
                  </dd>
                </div>
              )}
            </dl>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Delete Confirmation Dialog */}
      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete Addon</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to delete &quot;{addon.name}&quot;? This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {deleteAddon.isPending ? "Deleting..." : "Delete"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Publish Confirmation Dialog */}
      <AlertDialog open={showPublishDialog} onOpenChange={setShowPublishDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Publish Addon</AlertDialogTitle>
            <AlertDialogDescription>
              Publishing this addon will make it immutable. You won&apos;t be able to edit it after
              publishing. To make changes, you&apos;ll need to clone it and create a new version.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handlePublish}>
              {publishAddon.isPending ? "Publishing..." : "Publish"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

// Configuration Viewer Component
function ConfigurationView({ config }: { config: any }) {
  const hasOperators = config.operators && config.operators.length > 0;
  const hasScripts = config.scripts && config.scripts.length > 0;
  const hasManifests = config.manifests && config.manifests.length > 0;
  const hasHelmCharts = config.helm_charts && config.helm_charts.length > 0;

  if (!hasOperators && !hasScripts && !hasManifests && !hasHelmCharts) {
    return (
      <Card className="p-6">
        <p className="text-muted-foreground">No configuration defined for this addon.</p>
      </Card>
    );
  }

  return (
    <div className="space-y-4">
      {hasOperators && (
        <Card className="p-6">
          <h3 className="font-semibold mb-4">Operators ({config.operators.length})</h3>
          <div className="space-y-3">
            {config.operators.map((op: any, i: number) => (
              <div key={i} className="border-l-2 border-blue-500 pl-4">
                <div className="font-medium">{op.name}</div>
                <div className="text-sm text-muted-foreground">
                  Namespace: {op.namespace || "default"}
                </div>
                {op.source && (
                  <div className="text-sm text-muted-foreground">Source: {op.source}</div>
                )}
                {op.channel && (
                  <div className="text-sm text-muted-foreground">Channel: {op.channel}</div>
                )}
                {op.depends_on && op.depends_on.length > 0 && (
                  <div className="text-sm text-muted-foreground">
                    Depends on: {op.depends_on.join(", ")}
                  </div>
                )}
              </div>
            ))}
          </div>
        </Card>
      )}

      {hasScripts && (
        <Card className="p-6">
          <h3 className="font-semibold mb-4">Scripts ({config.scripts.length})</h3>
          <div className="space-y-4">
            {config.scripts.map((script: any, i: number) => (
              <div key={i} className="border-l-2 border-green-500 pl-4 space-y-2">
                <div className="font-medium">{script.name}</div>
                <div className="text-sm text-muted-foreground">Path: {script.path}</div>
                {script.timeout && (
                  <div className="text-sm text-muted-foreground">Timeout: {script.timeout}</div>
                )}
                {script.depends_on && script.depends_on.length > 0 && (
                  <div className="text-sm text-muted-foreground">
                    Depends on: {script.depends_on.join(", ")}
                  </div>
                )}
                {script.content && (
                  <div className="mt-2">
                    <div className="text-xs text-muted-foreground mb-1">Content:</div>
                    <pre className="text-xs bg-muted p-3 rounded overflow-auto max-h-96">
                      <code>{script.content}</code>
                    </pre>
                  </div>
                )}
              </div>
            ))}
          </div>
        </Card>
      )}

      {hasManifests && (
        <Card className="p-6">
          <h3 className="font-semibold mb-4">Manifests ({config.manifests.length})</h3>
          <div className="space-y-4">
            {config.manifests.map((manifest: any, i: number) => (
              <div key={i} className="border-l-2 border-purple-500 pl-4 space-y-2">
                <div className="font-medium">{manifest.name}</div>
                {manifest.path && (
                  <div className="text-sm text-muted-foreground">Path: {manifest.path}</div>
                )}
                {manifest.depends_on && manifest.depends_on.length > 0 && (
                  <div className="text-sm text-muted-foreground">
                    Depends on: {manifest.depends_on.join(", ")}
                  </div>
                )}
                {manifest.content && (
                  <div className="mt-2">
                    <div className="text-xs text-muted-foreground mb-1">Content:</div>
                    <pre className="text-xs bg-muted p-3 rounded overflow-auto max-h-96">
                      <code>{manifest.content}</code>
                    </pre>
                  </div>
                )}
              </div>
            ))}
          </div>
        </Card>
      )}

      {hasHelmCharts && (
        <Card className="p-6">
          <h3 className="font-semibold mb-4">Helm Charts ({config.helm_charts.length})</h3>
          <div className="space-y-3">
            {config.helm_charts.map((chart: any, i: number) => (
              <div key={i} className="border-l-2 border-orange-500 pl-4">
                <div className="font-medium">{chart.name}</div>
                <div className="text-sm text-muted-foreground">Repo: {chart.repo}</div>
                <div className="text-sm text-muted-foreground">Chart: {chart.chart}</div>
                {chart.version && (
                  <div className="text-sm text-muted-foreground">Version: {chart.version}</div>
                )}
                {chart.namespace && (
                  <div className="text-sm text-muted-foreground">Namespace: {chart.namespace}</div>
                )}
                {chart.depends_on && chart.depends_on.length > 0 && (
                  <div className="text-sm text-muted-foreground">
                    Depends on: {chart.depends_on.join(", ")}
                  </div>
                )}
              </div>
            ))}
          </div>
        </Card>
      )}
    </div>
  );
}
