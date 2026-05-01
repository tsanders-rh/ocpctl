# Addon Post-Config UI Visualization

## API Response Format

When fetching cluster details via `GET /api/v1/clusters/{id}`, the response now includes execution order metadata for visualizing addon dependencies:

```json
{
  "cluster": {
    "id": "c04ccb14-5fd6-4c28-9276-3ca34a8a6226",
    "name": "tsanders-cnv-pre100",
    "customPostConfig": {
      "scripts": [
        {
          "name": "verify-quay-credentials",
          "description": "Verify quay.io credentials are present in pull secret",
          "dependsOn": [],
          "content": "#!/bin/bash\n..."
        }
      ],
      "manifests": [
        {
          "name": "cnv-nightly-catalogsource",
          "description": "CatalogSource for CNV pre-release (nightly) builds",
          "dependsOn": ["verify-quay-credentials"],
          "content": "apiVersion: operators.coreos.com/v1alpha1\n..."
        }
      ],
      "operators": [
        {
          "name": "kubevirt-hyperconverged",
          "namespace": "openshift-cnv",
          "source": "cnv-nightly",
          "channel": "nightly",
          "dependsOn": ["verify-quay-credentials", "cnv-nightly-catalogsource"]
        }
      ]
    }
  },
  "postConfigExecutionOrder": [
    {
      "name": "verify-quay-credentials",
      "type": "script",
      "dependencies": [],
      "order": 1
    },
    {
      "name": "cnv-nightly-catalogsource",
      "type": "manifest",
      "dependencies": ["verify-quay-credentials"],
      "order": 2
    },
    {
      "name": "kubevirt-hyperconverged",
      "type": "operator",
      "dependencies": ["verify-quay-credentials", "cnv-nightly-catalogsource"],
      "order": 3
    }
  ]
}
```

## Configuration Status Endpoint

To show real-time installation status, fetch from `GET /api/v1/clusters/{id}/configurations`:

```json
{
  "cluster_id": "c04ccb14-5fd6-4c28-9276-3ca34a8a6226",
  "cluster_name": "tsanders-cnv-pre100",
  "configurations": [
    {
      "id": "cfg-123",
      "config_type": "script",
      "config_name": "verify-quay-credentials",
      "status": "completed",
      "created_at": "2026-05-01T00:00:00Z",
      "completed_at": "2026-05-01T00:00:10Z"
    },
    {
      "id": "cfg-124",
      "config_type": "manifest",
      "config_name": "cnv-nightly-catalogsource",
      "status": "completed",
      "created_at": "2026-05-01T00:00:11Z",
      "completed_at": "2026-05-01T00:00:15Z"
    },
    {
      "id": "cfg-125",
      "config_type": "operator",
      "config_name": "kubevirt-hyperconverged",
      "status": "installing",
      "created_at": "2026-05-01T00:00:16Z"
    }
  ],
  "total": 3
}
```

## UI Visualization Recommendations

### 1. Execution Order Table

Display tasks in execution order with:

| Order | Name | Type | Dependencies | Status |
|-------|------|------|--------------|--------|
| 1 | verify-quay-credentials | Script | - | ✅ Completed |
| 2 | cnv-nightly-catalogsource | Manifest | verify-quay-credentials | ✅ Completed |
| 3 | kubevirt-hyperconverged | Operator | verify-quay-credentials, cnv-nightly-catalogsource | ⏳ Installing |

### 2. Dependency Graph Visualization

Use a library like `react-flow`, `vis-network`, or `dagre` to show:

```
┌────────────────────────────┐
│ verify-quay-credentials    │
│ (Script)                   │
│ ✅ Completed               │
└────────────┬───────────────┘
             │
             ├──────────────────────────────┐
             │                              │
             ▼                              ▼
┌────────────────────────────┐  ┌─────────────────────────┐
│ cnv-nightly-catalogsource  │  │ kubevirt-hyperconverged │
│ (Manifest)                 │  │ (Operator)              │
│ ✅ Completed               │  │ ⏳ Installing           │
└────────────┬───────────────┘  └─────────────────────────┘
             │
             └──────────────────────────────┘
```

### 3. Status Badges

Color-code by status:
- 🟢 **Completed**: Green
- 🔵 **Installing**: Blue
- 🟡 **Pending**: Yellow
- 🔴 **Failed**: Red

### 4. Type Icons

Add icons by type:
- 📜 **Script**
- 📄 **Manifest**
- ⚙️ **Operator**
- 📦 **Helm Chart**

### 5. Collapsible Details

Allow clicking on each task to show:
- Full configuration (YAML/JSON)
- Error messages (if failed)
- Timestamps (created, completed)
- Logs (link to logs endpoint)

## Example React Component Structure

```tsx
interface TaskExecutionInfo {
  name: string;
  type: 'script' | 'manifest' | 'operator' | 'helmChart';
  dependencies: string[];
  order: number;
}

interface ConfigurationStatus {
  config_name: string;
  config_type: string;
  status: 'pending' | 'installing' | 'completed' | 'failed';
  error_message?: string;
}

function AddonExecutionView({ clusterId }: { clusterId: string }) {
  const { cluster, postConfigExecutionOrder } = useClusterDetails(clusterId);
  const { configurations } = useClusterConfigurations(clusterId);

  // Merge execution order with status
  const tasksWithStatus = postConfigExecutionOrder.map(task => ({
    ...task,
    status: configurations.find(c => c.config_name === task.name)?.status || 'pending'
  }));

  return (
    <div>
      <h2>Post-Deployment Configuration</h2>
      <ExecutionOrderTable tasks={tasksWithStatus} />
      <DependencyGraph tasks={tasksWithStatus} />
    </div>
  );
}
```

## Real-time Updates

Poll the configurations endpoint every 5-10 seconds while `post_deploy_status` is `"in_progress"`:

```tsx
useEffect(() => {
  if (cluster.post_deploy_status === 'in_progress') {
    const interval = setInterval(() => {
      refetchConfigurations();
    }, 5000); // Poll every 5 seconds

    return () => clearInterval(interval);
  }
}, [cluster.post_deploy_status]);
```

## Summary

With these changes:

✅ **UI receives execution order** via `postConfigExecutionOrder` field
✅ **Dependencies are visible** for each task
✅ **Installation status** is available via `/configurations` endpoint
✅ **Real-time updates** via polling configurations
✅ **All task types tracked** (scripts, manifests, operators, helm charts)
✅ **Logs streamed to database** for UI display

The UI can now show users exactly what's being installed, in what order, and with what dependencies!
