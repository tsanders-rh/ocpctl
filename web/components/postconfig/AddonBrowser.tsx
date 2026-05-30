"use client";

import { useState } from "react";
import { usePostConfigAddons } from "@/lib/hooks/usePostConfig";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import { Search, Package, AlertCircle } from "lucide-react";
import type { PostConfigAddon, AddonSelection } from "@/types/api";

interface AddonBrowserProps {
  platform?: string;
  profile?: string;
  selectedAddons: AddonSelection[];
  onSelectionChange: (selections: AddonSelection[]) => void;
}

export function AddonBrowser({
  platform,
  profile,
  selectedAddons,
  onSelectionChange,
}: AddonBrowserProps) {
  const [category, setCategory] = useState<string>("all");
  const [search, setSearch] = useState<string>("");

  const { data, isLoading } = usePostConfigAddons({
    category: category === "all" ? undefined : category,
    platform,
    profile,
    search: search || undefined,
  });

  const handleToggleAddon = (addonId: string, defaultVersion: string) => {
    const isSelected = selectedAddons.some((s) => s.id === addonId);

    if (isSelected) {
      // Deselect
      onSelectionChange(selectedAddons.filter((s) => s.id !== addonId));
    } else {
      // Select with default version
      // First, check if this addon conflicts with any currently selected addons
      const addon = addons.find((a: PostConfigAddon) => a.id === addonId);
      const conflictsWith = addon?.metadata?.conflictsWith || [];

      // Remove any conflicting addons from selection
      const updatedSelections = selectedAddons.filter((s) => !conflictsWith.includes(s.id));

      // Add the new addon
      onSelectionChange([...updatedSelections, { id: addonId, version: defaultVersion }]);
    }
  };

  // Helper function to check if an addon is disabled due to conflicts
  const isAddonDisabled = (addon: PostConfigAddon): boolean => {
    if (!addon.metadata?.conflictsWith) return false;

    // Check if any selected addon conflicts with this one
    return selectedAddons.some((s) => addon.metadata?.conflictsWith?.includes(s.id));
  };

  // Helper function to get the conflicting addon name
  const getConflictingAddon = (addon: PostConfigAddon): PostConfigAddon | undefined => {
    if (!addon.metadata?.conflictsWith) return undefined;

    const conflictingId = selectedAddons.find((s) => addon.metadata?.conflictsWith?.includes(s.id))?.id;
    return addons.find((a: PostConfigAddon) => a.id === conflictingId);
  };

  const handleVersionChange = (addonId: string, version: string) => {
    onSelectionChange(
      selectedAddons.map((s) =>
        s.id === addonId ? { ...s, version } : s
      )
    );
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-8">
        <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-primary"></div>
      </div>
    );
  }

  const categories = Object.keys(data?.categories || {});
  const addons = data?.addons || [];

  return (
    <div className="space-y-4">
      <div className="flex flex-col sm:flex-row gap-3">
        <div className="flex-1 relative">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            type="text"
            placeholder="Search add-ons..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="pl-9"
          />
        </div>

        <Select value={category} onValueChange={setCategory}>
          <SelectTrigger className="w-full sm:w-[200px]">
            <SelectValue placeholder="All Categories" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Categories</SelectItem>
            {categories.map((cat) => (
              <SelectItem key={cat} value={cat}>
                {cat.charAt(0).toUpperCase() + cat.slice(1)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {addons.length === 0 ? (
        <div className="text-center py-8 text-muted-foreground">
          <Package className="h-12 w-12 mx-auto mb-3 opacity-50" />
          <p>No add-ons found</p>
        </div>
      ) : (
        <div className="space-y-3 max-h-[400px] overflow-y-auto">
          {addons.map((addon: PostConfigAddon) => {
            const selection = selectedAddons.find((s) => s.id === addon.id);
            const isSelected = !!selection;
            const isDisabled = isAddonDisabled(addon);
            const conflictingAddon = getConflictingAddon(addon);

            return (
              <div
                key={addon.id}
                className={`border rounded-lg p-4 space-y-3 ${isDisabled ? 'opacity-60 bg-muted/30' : ''}`}
              >
                <div className="flex items-start space-x-3">
                  <TooltipProvider>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <div>
                          <Checkbox
                            id={`addon-${addon.id}`}
                            checked={isSelected}
                            disabled={isDisabled}
                            onCheckedChange={() => handleToggleAddon(addon.id, addon.versions.default)}
                            className="mt-1"
                          />
                        </div>
                      </TooltipTrigger>
                      {isDisabled && conflictingAddon && (
                        <TooltipContent>
                          <p className="flex items-center gap-2">
                            <AlertCircle className="h-4 w-4" />
                            Conflicts with {conflictingAddon.name}
                          </p>
                        </TooltipContent>
                      )}
                    </Tooltip>
                  </TooltipProvider>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <Label
                        htmlFor={`addon-${addon.id}`}
                        className={`font-medium text-base ${isDisabled ? 'cursor-not-allowed' : 'cursor-pointer'}`}
                      >
                        {addon.name}
                      </Label>
                      {addon.addonSource === "system" && (
                        <span className="inline-flex items-center rounded-md bg-blue-50 dark:bg-blue-950 px-2 py-1 text-xs font-medium text-blue-700 dark:text-blue-300 ring-1 ring-inset ring-blue-700/10">
                          System
                        </span>
                      )}
                      {addon.addonSource === "user" && addon.isPublished && (
                        <span className="inline-flex items-center rounded-md bg-green-50 dark:bg-green-950 px-2 py-1 text-xs font-medium text-green-700 dark:text-green-300 ring-1 ring-inset ring-green-700/10">
                          Published
                        </span>
                      )}
                      {addon.addonSource === "user" && !addon.isPublished && (
                        <span className="inline-flex items-center rounded-md bg-orange-50 dark:bg-orange-950 px-2 py-1 text-xs font-medium text-orange-700 dark:text-orange-300 ring-1 ring-inset ring-orange-700/10">
                          Draft
                        </span>
                      )}
                      {isDisabled && conflictingAddon && (
                        <span className="inline-flex items-center rounded-md bg-yellow-50 dark:bg-yellow-950 px-2 py-1 text-xs font-medium text-yellow-700 dark:text-yellow-300 ring-1 ring-inset ring-yellow-700/10">
                          <AlertCircle className="h-3 w-3 mr-1" />
                          Conflicts with {conflictingAddon.name}
                        </span>
                      )}
                    </div>
                    <p className="text-sm text-muted-foreground mt-1">
                      {addon.description}
                    </p>
                    <div className="flex gap-2 mt-2">
                      <span className="inline-flex items-center rounded-md bg-primary/10 px-2 py-1 text-xs font-medium text-primary ring-1 ring-inset ring-primary/20">
                        {addon.category}
                      </span>
                      {addon.supportedPlatforms.length > 0 && (
                        <span className="inline-flex items-center rounded-md bg-blue-50 dark:bg-blue-950 px-2 py-1 text-xs font-medium text-blue-700 dark:text-blue-300 ring-1 ring-inset ring-blue-700/10">
                          {addon.supportedPlatforms.join(", ")}
                        </span>
                      )}
                    </div>
                  </div>
                </div>

                {/* Version selector - shown when addon is selected */}
                {isSelected && selection && (
                  <div className="ml-9 pl-4 border-l-2 border-muted">
                    <Label className="text-sm font-medium">Version</Label>
                    <Select
                      value={selection.version}
                      onValueChange={(v) => handleVersionChange(addon.id, v)}
                    >
                      <SelectTrigger className="w-full mt-2">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {addon.versions.allowed.map((v) => (
                          <SelectItem key={v.channel} value={v.channel}>
                            {v.displayName}
                            {v.channel === addon.versions.default && (
                              <span className="ml-2 text-xs text-muted-foreground">
                                (recommended)
                              </span>
                            )}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                )}
              </div>
            );
          })}
        </div>
      )}

      {selectedAddons.length > 0 && (
        <div className="bg-muted/50 rounded-md p-3 text-sm">
          <p className="font-medium mb-1">
            {selectedAddons.length} add-on{selectedAddons.length !== 1 ? "s" : ""}{" "}
            selected
          </p>
          <p className="text-muted-foreground text-xs">
            Selected add-ons will be installed after cluster creation
          </p>
        </div>
      )}
    </div>
  );
}
