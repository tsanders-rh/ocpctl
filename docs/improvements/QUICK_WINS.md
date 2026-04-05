# Quick Win UX Improvements

High-impact, low-effort improvements that can be implemented in < 1 day each.

## 1. Search Bar (2-3 hours)

**Impact:** High | **Effort:** Low

Add instant search to clusters list page:

```tsx
// Add to web/app/(dashboard)/clusters/page.tsx
const [searchQuery, setSearchQuery] = useState("");

// Filter clusters by search query
const filteredClusters = useMemo(() => {
  if (!searchQuery) return clusters;
  const query = searchQuery.toLowerCase();
  return clusters.filter(c =>
    c.name.toLowerCase().includes(query) ||
    c.team.toLowerCase().includes(query) ||
    c.region.toLowerCase().includes(query) ||
    c.owner.toLowerCase().includes(query)
  );
}, [clusters, searchQuery]);

// Add search input before filters
<Input
  placeholder="Search clusters..."
  value={searchQuery}
  onChange={(e) => setSearchQuery(e.target.value)}
  className="max-w-sm"
/>
```

## 2. Copy to Clipboard Buttons (1-2 hours)

**Impact:** Medium | **Effort:** Very Low

Add copy buttons for cluster details:

```tsx
// Add to web/app/(dashboard)/clusters/[id]/page.tsx
import { Copy, Check } from "lucide-react";
import { useState } from "react";

function CopyButton({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = async () => {
    await navigator.clipboard.writeText(text);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <Button variant="outline" size="sm" onClick={handleCopy}>
      {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
      {label}
    </Button>
  );
}

// Use in cluster details
<CopyButton text={cluster.id} label="Copy ID" />
<CopyButton text={cluster.api_endpoint} label="Copy API Endpoint" />
<CopyButton text={cluster.console_url} label="Copy Console URL" />
```

## 3. Cluster Count Badge (30 minutes)

**Impact:** Low | **Effort:** Very Low

Show total cluster count in page header:

```tsx
// web/app/(dashboard)/clusters/page.tsx
<h1 className="text-3xl font-bold">
  {isAdmin ? "All Clusters" : "My Clusters"}
  {data.pagination && (
    <Badge variant="secondary" className="ml-3">
      {data.pagination.total}
    </Badge>
  )}
</h1>
```

## 4. Status Badge Tooltips (1 hour)

**Impact:** Low | **Effort:** Very Low

Add explanatory tooltips to status badges:

```tsx
// web/components/clusters/ClusterStatusBadge.tsx
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";

const statusDescriptions = {
  PENDING: "Cluster creation queued, waiting for worker",
  CREATING: "Infrastructure being provisioned",
  READY: "Cluster is active and ready to use",
  DESTROYING: "Cluster resources being deleted",
  DESTROYED: "Cluster has been destroyed",
  FAILED: "Cluster creation or operation failed",
  HIBERNATED: "Cluster is stopped to save costs",
};

export function ClusterStatusBadge({ status }: { status: ClusterStatus }) {
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger>
          <Badge variant={getVariant(status)}>
            {status}
          </Badge>
        </TooltipTrigger>
        <TooltipContent>
          <p>{statusDescriptions[status]}</p>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
```

## 5. Manual Refresh Button (30 minutes)

**Impact:** Low | **Effort:** Very Low

Add manual refresh to clusters list:

```tsx
// web/app/(dashboard)/clusters/page.tsx
import { RefreshCw } from "lucide-react";

const { data, isLoading, refetch } = useClusters({ page, per_page: 20, ...filters });

// Add button next to "Create Cluster"
<Button variant="outline" onClick={() => refetch()} disabled={isLoading}>
  <RefreshCw className={`mr-2 h-4 w-4 ${isLoading ? 'animate-spin' : ''}`} />
  Refresh
</Button>
```

## 6. View in AWS Console Link (1 hour)

**Impact:** Medium | **Effort:** Low

Add quick link to AWS Console for clusters:

```tsx
// web/app/(dashboard)/clusters/[id]/page.tsx
import { ExternalLink } from "lucide-react";

function getAWSConsoleURL(cluster: Cluster): string | null {
  if (cluster.platform !== 'aws') return null;

  // OpenShift on AWS - link to EC2 instances filtered by infraID
  if (cluster.cluster_type === 'openshift') {
    const region = cluster.region;
    const infraID = cluster.metadata?.infraID;
    if (!infraID) return null;
    return `https://console.aws.amazon.com/ec2/home?region=${region}#Instances:tag:kubernetes.io/cluster/${infraID}=owned`;
  }

  // EKS - link to EKS console
  if (cluster.cluster_type === 'eks') {
    return `https://console.aws.amazon.com/eks/home?region=${cluster.region}#/clusters/${cluster.name}`;
  }

  return null;
}

// Add button to cluster details
{cluster.platform === 'aws' && getAWSConsoleURL(cluster) && (
  <a href={getAWSConsoleURL(cluster)} target="_blank" rel="noopener noreferrer">
    <Button variant="outline">
      <ExternalLink className="mr-2 h-4 w-4" />
      View in AWS Console
    </Button>
  </a>
)}
```

## 7. Download install-config.yaml (2 hours)

**Impact:** Medium | **Effort:** Low

Add button to download install-config used for cluster creation:

```tsx
// Backend: Add endpoint to internal/api/handlers_cluster.go
func (h *ClusterHandler) GetInstallConfig(c echo.Context) error {
    clusterID := c.Param("id")
    cluster, err := h.store.GetCluster(c.Request().Context(), clusterID)
    if err != nil {
        return echo.NewHTTPError(http.StatusNotFound, "Cluster not found")
    }

    // Get install-config from S3 or regenerate from profile
    installConfig, err := h.getInstallConfig(cluster)
    if err != nil {
        return echo.NewHTTPError(http.StatusInternalServerError, "Failed to get install-config")
    }

    c.Response().Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=install-config-%s.yaml", cluster.Name))
    return c.Blob(http.StatusOK, "application/x-yaml", []byte(installConfig))
}

// Frontend: Add download button
<Button
  variant="outline"
  onClick={() => {
    fetch(`/api/v1/clusters/${cluster.id}/install-config`)
      .then(res => res.blob())
      .then(blob => {
        const url = window.URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `install-config-${cluster.name}.yaml`;
        a.click();
      });
  }}
>
  <Download className="mr-2 h-4 w-4" />
  Download install-config.yaml
</Button>
```

## 8. Favorite/Pin Clusters (3 hours)

**Impact:** Medium | **Effort:** Low

Allow users to pin important clusters to top of list:

```tsx
// Store favorites in localStorage
const [favorites, setFavorites] = useState<Set<string>>(() => {
  const stored = localStorage.getItem('favorite-clusters');
  return new Set(stored ? JSON.parse(stored) : []);
});

const toggleFavorite = (clusterId: string) => {
  const newFavorites = new Set(favorites);
  if (newFavorites.has(clusterId)) {
    newFavorites.delete(clusterId);
  } else {
    newFavorites.add(clusterId);
  }
  setFavorites(newFavorites);
  localStorage.setItem('favorite-clusters', JSON.stringify(Array.from(newFavorites)));
};

// Sort clusters: favorites first
const sortedClusters = useMemo(() => {
  return [...clusters].sort((a, b) => {
    const aFav = favorites.has(a.id);
    const bFav = favorites.has(b.id);
    if (aFav && !bFav) return -1;
    if (!aFav && bFav) return 1;
    return 0;
  });
}, [clusters, favorites]);

// Add star icon in table
import { Star } from "lucide-react";

<td className="p-4">
  <Button
    variant="ghost"
    size="sm"
    onClick={() => toggleFavorite(cluster.id)}
  >
    <Star
      className={`h-4 w-4 ${favorites.has(cluster.id) ? 'fill-yellow-400 text-yellow-400' : ''}`}
    />
  </Button>
</td>
```

## 9. TTL Expiration Warnings (2 hours)

**Impact:** High | **Effort:** Low

Show visual warnings for clusters about to expire:

```tsx
// Add to formatTTL utility
export function getTTLWarningLevel(destroyAt: string): 'critical' | 'warning' | 'normal' {
  const now = new Date();
  const destroy = new Date(destroyAt);
  const hoursRemaining = (destroy.getTime() - now.getTime()) / (1000 * 60 * 60);

  if (hoursRemaining < 1) return 'critical';
  if (hoursRemaining < 6) return 'warning';
  return 'normal';
}

// Use in clusters table
<td className="p-4 text-sm">
  <div className="flex items-center gap-2">
    {getTTLWarningLevel(cluster.destroy_at) === 'critical' && (
      <AlertCircle className="h-4 w-4 text-red-500" />
    )}
    {getTTLWarningLevel(cluster.destroy_at) === 'warning' && (
      <Clock className="h-4 w-4 text-yellow-500" />
    )}
    <span className={
      getTTLWarningLevel(cluster.destroy_at) === 'critical' ? 'text-red-600 font-semibold' :
      getTTLWarningLevel(cluster.destroy_at) === 'warning' ? 'text-yellow-600 font-medium' :
      ''
    }>
      {formatTTL(cluster.destroy_at)}
    </span>
  </div>
</td>
```

## 10. Empty State Illustrations (2 hours)

**Impact:** Low | **Effort:** Low

Better empty states with illustrations and CTAs:

```tsx
// web/components/EmptyState.tsx
export function EmptyState({
  title,
  description,
  action
}: {
  title: string;
  description: string;
  action?: { label: string; onClick: () => void; }
}) {
  return (
    <div className="flex flex-col items-center justify-center p-12 text-center">
      <div className="rounded-full bg-muted p-6 mb-4">
        <Inbox className="h-12 w-12 text-muted-foreground" />
      </div>
      <h3 className="text-lg font-semibold mb-2">{title}</h3>
      <p className="text-muted-foreground mb-6 max-w-md">{description}</p>
      {action && (
        <Button onClick={action.onClick}>
          {action.label}
        </Button>
      )}
    </div>
  );
}

// Use in clusters page when no clusters
{clusters.length === 0 && !hasActiveFilters && (
  <EmptyState
    title="No clusters yet"
    description="Get started by creating your first Kubernetes cluster. Choose from OpenShift, EKS, or IKS."
    action={{
      label: "Create your first cluster",
      onClick: () => router.push('/clusters/new')
    }}
  />
)}
```

## Implementation Order

Implement in this order for maximum impact:

1. **Search Bar** (2-3 hours) - Highest impact
2. **Copy to Clipboard** (1-2 hours) - Very useful
3. **TTL Warnings** (2 hours) - Prevents accidental deletion
4. **Refresh Button** (30 min) - Easy win
5. **AWS Console Link** (1 hour) - Useful for debugging
6. **Favorite Clusters** (3 hours) - Good for power users
7. **Status Tooltips** (1 hour) - Helps new users
8. **Cluster Count Badge** (30 min) - Visual polish
9. **Empty States** (2 hours) - Better first impression
10. **Download install-config** (2 hours) - Useful for advanced users

**Total estimated time:** 15-17 hours (2 developer days)

## Testing Checklist

For each quick win:
- [ ] Test on desktop (Chrome, Firefox, Safari)
- [ ] Test on mobile (responsive design)
- [ ] Test with dark mode
- [ ] Test with empty/error states
- [ ] Test accessibility (keyboard navigation, screen readers)
- [ ] Update UI tests if applicable

## See Also

- [Web UI Improvements](WEB_UI_IMPROVEMENTS.md) - Full improvement roadmap
- [Feature Matrix](../reference/FEATURE_MATRIX.md) - Current features
