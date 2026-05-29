"use client";

import { useState, useMemo } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import {
  PackageCheck,
  Plus,
  Search,
  Filter,
  RefreshCw,
  AlertCircle,
} from "lucide-react";
import { useAddons, useMyAddons } from "@/lib/hooks/useAddons";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { Card } from "@/components/ui/card";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";

const CATEGORIES = [
  { value: "backup", label: "Backup" },
  { value: "migration", label: "Migration" },
  { value: "virtualization", label: "Virtualization" },
  { value: "monitoring", label: "Monitoring" },
  { value: "security", label: "Security" },
  { value: "storage", label: "Storage" },
  { value: "networking", label: "Networking" },
  { value: "cicd", label: "CI/CD" },
];

const PLATFORMS = [
  { value: "openshift", label: "OpenShift" },
  { value: "eks", label: "EKS" },
  { value: "gke", label: "GKE" },
  { value: "iks", label: "IKS" },
];

export default function AddonsPage() {
  const router = useRouter();
  const [searchQuery, setSearchQuery] = useState("");
  const [categoryFilter, setCategoryFilter] = useState<string>("all");
  const [platformFilter, setPlatformFilter] = useState<string>("all");
  const [activeTab, setActiveTab] = useState("all");

  // Fetch addons
  const {
    data: allAddons,
    isLoading: isLoadingAll,
    error: errorAll,
    refetch: refetchAll,
  } = useAddons({
    category: categoryFilter !== "all" ? categoryFilter : undefined,
    platform: platformFilter !== "all" ? platformFilter : undefined,
    search: searchQuery || undefined,
  });

  const {
    data: myAddons,
    isLoading: isLoadingMy,
    error: errorMy,
    refetch: refetchMy,
  } = useMyAddons();

  // Filter addons by search query (client-side for real-time feedback)
  const filteredAllAddons = useMemo(() => {
    if (!allAddons || !Array.isArray(allAddons)) return [];
    if (!searchQuery) return allAddons;

    const query = searchQuery.toLowerCase();
    return allAddons.filter(
      (addon) =>
        addon.name.toLowerCase().includes(query) ||
        addon.addon_id.toLowerCase().includes(query) ||
        addon.description.toLowerCase().includes(query)
    );
  }, [allAddons, searchQuery]);

  const filteredMyAddons = useMemo(() => {
    if (!myAddons || !Array.isArray(myAddons)) return [];
    if (!searchQuery) return myAddons;

    const query = searchQuery.toLowerCase();
    return myAddons.filter(
      (addon) =>
        addon.name.toLowerCase().includes(query) ||
        addon.addon_id.toLowerCase().includes(query) ||
        addon.description.toLowerCase().includes(query)
    );
  }, [myAddons, searchQuery]);

  // Separate system and user addons
  const systemAddons = useMemo(
    () => filteredAllAddons.filter((a) => a.addon_source === "system"),
    [filteredAllAddons]
  );

  const userAddons = useMemo(
    () => filteredAllAddons.filter((a) => a.addon_source === "user"),
    [filteredAllAddons]
  );

  const handleRefresh = () => {
    refetchAll();
    refetchMy();
  };

  const isLoading = isLoadingAll || isLoadingMy;
  const error = errorAll || errorMy;

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold flex items-center gap-2">
            <PackageCheck className="h-8 w-8" />
            Addons
          </h1>
          <p className="text-muted-foreground mt-1">
            Browse and manage post-deployment addons for your clusters
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={handleRefresh}>
            <RefreshCw className="h-4 w-4 mr-2" />
            Refresh
          </Button>
          <Link href="/addons/new">
            <Button size="sm">
              <Plus className="h-4 w-4 mr-2" />
              Create Addon
            </Button>
          </Link>
        </div>
      </div>

      {/* Search and Filters */}
      <div className="flex gap-4">
        <div className="flex-1 relative">
          <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search addons by name, ID, or description..."
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            className="pl-9"
          />
        </div>
        <Select value={categoryFilter} onValueChange={setCategoryFilter}>
          <SelectTrigger className="w-[180px]">
            <Filter className="h-4 w-4 mr-2" />
            <SelectValue placeholder="Category" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Categories</SelectItem>
            {CATEGORIES.map((cat) => (
              <SelectItem key={cat.value} value={cat.value}>
                {cat.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Select value={platformFilter} onValueChange={setPlatformFilter}>
          <SelectTrigger className="w-[180px]">
            <Filter className="h-4 w-4 mr-2" />
            <SelectValue placeholder="Platform" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Platforms</SelectItem>
            {PLATFORMS.map((plat) => (
              <SelectItem key={plat.value} value={plat.value}>
                {plat.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {/* Error State */}
      {error && (
        <Card className="p-4 border-destructive">
          <div className="flex items-center gap-2 text-destructive">
            <AlertCircle className="h-4 w-4" />
            <p>
              Failed to load addons: {error instanceof Error ? error.message : "Unknown error"}
            </p>
          </div>
        </Card>
      )}

      {/* Tabs */}
      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="all">
            All Addons ({filteredAllAddons.length})
          </TabsTrigger>
          <TabsTrigger value="system">
            System ({systemAddons.length})
          </TabsTrigger>
          <TabsTrigger value="community">
            Community ({userAddons.length})
          </TabsTrigger>
          <TabsTrigger value="my">
            My Addons ({filteredMyAddons.length})
          </TabsTrigger>
        </TabsList>

        {/* All Addons Tab */}
        <TabsContent value="all" className="space-y-4">
          {isLoading ? (
            <div className="text-center py-12 text-muted-foreground">
              Loading addons...
            </div>
          ) : filteredAllAddons.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              No addons found matching your filters.
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {filteredAllAddons.map((addon) => (
                <AddonCard key={addon.id} addon={addon} />
              ))}
            </div>
          )}
        </TabsContent>

        {/* System Addons Tab */}
        <TabsContent value="system" className="space-y-4">
          {isLoading ? (
            <div className="text-center py-12 text-muted-foreground">
              Loading system addons...
            </div>
          ) : systemAddons.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              No system addons found.
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {systemAddons.map((addon) => (
                <AddonCard key={addon.id} addon={addon} />
              ))}
            </div>
          )}
        </TabsContent>

        {/* Community Addons Tab */}
        <TabsContent value="community" className="space-y-4">
          {isLoading ? (
            <div className="text-center py-12 text-muted-foreground">
              Loading community addons...
            </div>
          ) : userAddons.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              No community addons found.
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {userAddons.map((addon) => (
                <AddonCard key={addon.id} addon={addon} />
              ))}
            </div>
          )}
        </TabsContent>

        {/* My Addons Tab */}
        <TabsContent value="my" className="space-y-4">
          {isLoadingMy ? (
            <div className="text-center py-12 text-muted-foreground">
              Loading your addons...
            </div>
          ) : filteredMyAddons.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              <p className="mb-4">You haven&apos;t created any addons yet.</p>
              <Link href="/addons/new">
                <Button>
                  <Plus className="h-4 w-4 mr-2" />
                  Create Your First Addon
                </Button>
              </Link>
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {filteredMyAddons.map((addon) => (
                <AddonCard key={addon.id} addon={addon} showActions />
              ))}
            </div>
          )}
        </TabsContent>
      </Tabs>
    </div>
  );
}

// Addon Card Component
function AddonCard({
  addon,
  showActions = false,
}: {
  addon: any;
  showActions?: boolean;
}) {
  const router = useRouter();

  const getCategoryColor = (category: string) => {
    const colors: Record<string, string> = {
      backup: "bg-blue-100 text-blue-800 dark:bg-blue-900 dark:text-blue-300",
      migration: "bg-purple-100 text-purple-800 dark:bg-purple-900 dark:text-purple-300",
      virtualization: "bg-pink-100 text-pink-800 dark:bg-pink-900 dark:text-pink-300",
      monitoring: "bg-green-100 text-green-800 dark:bg-green-900 dark:text-green-300",
      security: "bg-red-100 text-red-800 dark:bg-red-900 dark:text-red-300",
      storage: "bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-300",
      networking: "bg-indigo-100 text-indigo-800 dark:bg-indigo-900 dark:text-indigo-300",
      cicd: "bg-orange-100 text-orange-800 dark:bg-orange-900 dark:text-orange-300",
    };
    return colors[category] || "bg-gray-100 text-gray-800";
  };

  return (
    <Card
      className="p-4 hover:bg-muted/50 cursor-pointer transition-colors"
      onClick={() => router.push(`/addons/${addon.id}`)}
    >
      <div className="space-y-3">
        {/* Header */}
        <div className="flex items-start justify-between">
          <div className="flex-1">
            <h3 className="font-semibold">{addon.name}</h3>
            <p className="text-sm text-muted-foreground">{addon.addon_id}</p>
          </div>
          {addon.addon_source === "system" && (
            <Badge variant="secondary" className="text-xs">
              System
            </Badge>
          )}
          {addon.is_published && addon.addon_source === "user" && (
            <Badge variant="default" className="text-xs">
              Published
            </Badge>
          )}
          {!addon.is_published && addon.addon_source === "user" && (
            <Badge variant="outline" className="text-xs">
              Draft
            </Badge>
          )}
        </div>

        {/* Description */}
        <p className="text-sm text-muted-foreground line-clamp-2">
          {addon.description}
        </p>

        {/* Metadata */}
        <div className="flex flex-wrap gap-2">
          <Badge className={getCategoryColor(addon.category)}>
            {addon.category}
          </Badge>
          {addon.supported_platforms.slice(0, 2).map((platform: string) => (
            <Badge key={platform} variant="outline" className="text-xs">
              {platform}
            </Badge>
          ))}
          {addon.supported_platforms.length > 2 && (
            <Badge variant="outline" className="text-xs">
              +{addon.supported_platforms.length - 2}
            </Badge>
          )}
        </div>

        {/* Version Info */}
        <div className="flex items-center justify-between text-xs text-muted-foreground">
          <span>{addon.display_name || addon.version}</span>
          {addon.version_number && (
            <span>v{addon.version_number}</span>
          )}
        </div>
      </div>
    </Card>
  );
}
