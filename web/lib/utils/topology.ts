import {  type Cluster,
  type ClusterOutputs,
  type Profile,
  ClusterStatus,
  ClusterType,
  Platform,
} from "@/types/api";

// Topology element types
export type TopologyElementType =
  | "control-plane"
  | "worker"
  | "load-balancer"
  | "storage"
  | "access"
  | "managed-control-plane";

// Topology element definition
export interface TopologyElement {
  id: string;
  type: TopologyElementType;
  label: string;
  status?: ClusterStatus;
  metadata: {
    instanceType?: string;
    count?: number | string;
    minCount?: number;
    maxCount?: number;
    url?: string;
    detail?: string;
  };
  position: { x: number; y: number };
  size: { width: number; height: number };
}

// Layout configuration
// Connection line definition
export interface ConnectionLine {
  id: string;
  from: { x: number; y: number };
  to: { x: number; y: number };
  type: "data-flow" | "dependency";
}

export interface SectionBounds {
  label: string;
  y: number;
  height: number;
  color: string;
}

export interface TopologyLayout {
  elements: TopologyElement[];
  connections: ConnectionLine[];
  sections: SectionBounds[];
  platform: Platform;
  cluster_type?: ClusterType;
  region: string;
  networkMode?: string;
  hasStorage: boolean;
}

// Canvas constants
const CANVAS_WIDTH = 1200;
const CANVAS_HEIGHT = 800;
const MARGIN = 70;
const SECTION_SPACING = 65;
const NODE_SPACING = 25;

// Node dimensions
const NODE_DIMENSIONS = {
  "control-plane": { width: 110, height: 65 },
  "managed-control-plane": { width: 220, height: 75 },
  worker: { width: 110, height: 65 },
  "load-balancer": { width: 140, height: 60 },
  storage: { width: 110, height: 60 },
  access: { width: 450, height: 45 },
};

/**
 * Calculate the topology layout for a cluster
 */
export function getTopologyLayout(
  cluster: Cluster,
  profile: Profile | null,
  outputs?: ClusterOutputs
): TopologyLayout {
  const elements: TopologyElement[] = [];
  const sections: SectionBounds[] = [];
  let currentY = MARGIN;

  // Platform header (not a visual element, just metadata)
  const layout: TopologyLayout = {
    elements: [],
    connections: [],
    sections: [],
    platform: cluster.platform,
    cluster_type: profile?.platform === Platform.AWS && profile.compute.node_groups ? ClusterType.EKS :
                  profile?.platform === Platform.IBMCloud ? ClusterType.IKS :
                  ClusterType.OpenShift,
    region: cluster.region,
    hasStorage: !!(cluster as any).storage_config,
  };

  // Determine cluster type from profile
  const isEKS = layout.cluster_type === ClusterType.EKS;
  const isIKS = layout.cluster_type === ClusterType.IKS;
  const isOpenShift = layout.cluster_type === ClusterType.OpenShift;

  // 1. Control Plane Section
  const cpSectionStart = currentY;
  if (isEKS) {
    // EKS: Managed control plane
    elements.push({
      id: "managed-cp",
      type: "managed-control-plane",
      label: "EKS Control Plane",
      status: cluster.status,
      metadata: {
        detail: "AWS Managed",
      },
      position: { x: CANVAS_WIDTH / 2 - NODE_DIMENSIONS["managed-control-plane"].width / 2, y: currentY },
      size: NODE_DIMENSIONS["managed-control-plane"],
    });
    currentY += NODE_DIMENSIONS["managed-control-plane"].height + SECTION_SPACING;
  } else {
    // OpenShift/IKS: User-managed control plane
    const cpReplicas = profile?.compute.control_plane?.replicas || 3;
    const cpInstanceType = profile?.compute.control_plane?.instance_type || "unknown";

    // Calculate horizontal spacing for control plane nodes
    const totalCPWidth = cpReplicas * NODE_DIMENSIONS["control-plane"].width + (cpReplicas - 1) * NODE_SPACING;
    let cpX = (CANVAS_WIDTH - totalCPWidth) / 2;

    for (let i = 0; i < cpReplicas; i++) {
      elements.push({
        id: `cp-${i}`,
        type: "control-plane",
        label: `CP-${i + 1}`,
        status: cluster.status,
        metadata: {
          instanceType: cpInstanceType,
        },
        position: { x: cpX, y: currentY },
        size: NODE_DIMENSIONS["control-plane"],
      });
      cpX += NODE_DIMENSIONS["control-plane"].width + NODE_SPACING;
    }
    currentY += NODE_DIMENSIONS["control-plane"].height + SECTION_SPACING;
  }
  sections.push({
    label: "Control Plane",
    y: cpSectionStart,
    height: currentY - cpSectionStart - SECTION_SPACING,
    color: "rgb(100 116 139)", // slate-500
  });

  // 2. Worker Nodes / Node Groups
  const workerSectionStart = currentY;
  if (isEKS && profile?.compute.node_groups && profile.compute.node_groups.length > 0) {
    // EKS: Show each node group
    profile.compute.node_groups.forEach((ng, idx) => {
      elements.push({
        id: `ng-${idx}`,
        type: "worker",
        label: ng.name,
        status: cluster.status,
        metadata: {
          instanceType: ng.instance_type,
          count: `${ng.min_size}-${ng.max_size} (desired: ${ng.desired_capacity})`,
        },
        position: { x: CANVAS_WIDTH / 2 - NODE_DIMENSIONS.worker.width / 2, y: currentY },
        size: NODE_DIMENSIONS.worker,
      });
      currentY += NODE_DIMENSIONS.worker.height + NODE_SPACING;
    });
    currentY += SECTION_SPACING - NODE_SPACING;
  } else {
    // OpenShift/IKS: Show worker node pool
    const workerMin = profile?.compute.workers?.min_replicas || 0;
    const workerMax = profile?.compute.workers?.max_replicas || 0;
    const workerInstanceType = profile?.compute.workers?.instance_type || "unknown";

    if (workerMax > 0) {
      const workerCount = workerMin === workerMax
        ? `${workerMin} node${workerMin !== 1 ? 's' : ''}`
        : `${workerMin}-${workerMax} nodes`;
      const scalingLabel = workerMin !== workerMax ? " (autoscaling)" : "";

      elements.push({
        id: "workers",
        type: "worker",
        label: "Worker Nodes",
        status: cluster.status,
        metadata: {
          instanceType: workerInstanceType,
          count: `${workerCount}${scalingLabel}`,
        },
        position: { x: CANVAS_WIDTH / 2 - NODE_DIMENSIONS.worker.width / 2, y: currentY },
        size: NODE_DIMENSIONS.worker,
      });
      currentY += NODE_DIMENSIONS.worker.height + SECTION_SPACING;
    } else {
      // SNO cluster with no workers - still reserve space for the section to keep layout consistent
      // Use the same spacing as if there were workers: worker height + section spacing
      currentY += NODE_DIMENSIONS.worker.height + SECTION_SPACING;
    }
  }
  sections.push({
    label: isEKS ? "Node Groups" : "Worker Nodes",
    y: workerSectionStart,
    height: currentY - workerSectionStart - SECTION_SPACING,
    color: "rgb(100 116 139)", // slate-500
  });

  // 3. Load Balancers
  const lbSectionStart = currentY;
  if (isOpenShift) {
    // OpenShift: API + Ingress load balancers
    const lbSpacing = 60;
    const totalLBWidth = 2 * NODE_DIMENSIONS["load-balancer"].width + lbSpacing;
    let lbX = (CANVAS_WIDTH - totalLBWidth) / 2;

    elements.push({
      id: "api-lb",
      type: "load-balancer",
      label: "API\nLoad Balancer",
      metadata: {},
      position: { x: lbX, y: currentY },
      size: NODE_DIMENSIONS["load-balancer"],
    });

    lbX += NODE_DIMENSIONS["load-balancer"].width + lbSpacing;

    elements.push({
      id: "ingress-lb",
      type: "load-balancer",
      label: "Ingress\nLoad Balancer",
      metadata: {},
      position: { x: lbX, y: currentY },
      size: NODE_DIMENSIONS["load-balancer"],
    });

    currentY += NODE_DIMENSIONS["load-balancer"].height + SECTION_SPACING;
  } else {
    // EKS/IKS: Single load balancer
    elements.push({
      id: "lb",
      type: "load-balancer",
      label: "Load Balancer",
      metadata: {},
      position: { x: CANVAS_WIDTH / 2 - NODE_DIMENSIONS["load-balancer"].width / 2, y: currentY },
      size: NODE_DIMENSIONS["load-balancer"],
    });
    currentY += NODE_DIMENSIONS["load-balancer"].height + SECTION_SPACING;
  }
  sections.push({
    label: "Load Balancers",
    y: lbSectionStart,
    height: currentY - lbSectionStart - SECTION_SPACING,
    color: "rgb(100 116 139)", // slate-500
  });

  // 4. Storage (if configured)
  const storageSectionStart = currentY;
  if (layout.hasStorage || cluster.platform === Platform.AWS) {
    const storageTypes: TopologyElement[] = [];

    if (layout.hasStorage) {
      storageTypes.push({
        id: "efs",
        type: "storage",
        label: "EFS",
        metadata: { detail: "Shared Storage" },
        position: { x: 0, y: currentY },
        size: NODE_DIMENSIONS.storage,
      });
    }

    // S3 always exists for metadata/backups
    storageTypes.push({
      id: "s3",
      type: "storage",
      label: "S3",
      metadata: { detail: "Metadata/Backups" },
      position: { x: 0, y: currentY },
      size: NODE_DIMENSIONS.storage,
    });

    // Center storage elements
    const totalStorageWidth = storageTypes.length * NODE_DIMENSIONS.storage.width + (storageTypes.length - 1) * NODE_SPACING;
    let storageX = (CANVAS_WIDTH - totalStorageWidth) / 2;

    storageTypes.forEach((storage) => {
      storage.position.x = storageX;
      elements.push(storage);
      storageX += NODE_DIMENSIONS.storage.width + NODE_SPACING;
    });

    currentY += NODE_DIMENSIONS.storage.height + SECTION_SPACING;
  }
  if (layout.hasStorage || cluster.platform === Platform.AWS) {
    sections.push({
      label: "Storage",
      y: storageSectionStart,
      height: currentY - storageSectionStart - SECTION_SPACING,
      color: "rgb(100 116 139)", // slate-500
    });
  }

  // 5. External Access Endpoints
  const accessSectionStart = currentY;
  const accessElements: TopologyElement[] = [];

  if (outputs?.api_url) {
    accessElements.push({
      id: "api-access",
      type: "access",
      label: "API Endpoint",
      metadata: { url: outputs.api_url },
      position: { x: CANVAS_WIDTH / 2 - NODE_DIMENSIONS.access.width / 2, y: currentY },
      size: NODE_DIMENSIONS.access,
    });
    currentY += NODE_DIMENSIONS.access.height + NODE_SPACING;
  }

  if (outputs?.console_url && isOpenShift) {
    accessElements.push({
      id: "console-access",
      type: "access",
      label: "Console",
      metadata: { url: outputs.console_url },
      position: { x: CANVAS_WIDTH / 2 - NODE_DIMENSIONS.access.width / 2, y: currentY },
      size: NODE_DIMENSIONS.access,
    });
    currentY += NODE_DIMENSIONS.access.height + NODE_SPACING;
  }

  elements.push(...accessElements);

  if (accessElements.length > 0) {
    sections.push({
      label: "External Access",
      y: accessSectionStart,
      height: currentY - accessSectionStart,
      color: "rgb(100 116 139)", // slate-500
    });
  }

  // Generate connection lines
  const connections: ConnectionLine[] = [];

  // Find key elements for connections
  const controlPlanes = elements.filter(e => e.type === "control-plane" || e.type === "managed-control-plane");
  const workers = elements.filter(e => e.type === "worker");
  const loadBalancers = elements.filter(e => e.type === "load-balancer");
  const apiLB = elements.find(e => e.id === "api-lb");
  const ingressLB = elements.find(e => e.id === "ingress-lb");
  const storage = elements.filter(e => e.type === "storage");

  // API Load Balancer → Control Plane (external API traffic flows in)
  if (controlPlanes.length > 0 && apiLB) {
    // Use the leftmost CP node to connect from API LB (which is on the left)
    const leftCP = controlPlanes[0];
    connections.push({
      id: "api-lb-to-cp",
      from: {
        x: apiLB.position.x + apiLB.size.width / 2,
        y: apiLB.position.y + apiLB.size.height
      },
      to: {
        x: leftCP.position.x + leftCP.size.width / 2,
        y: leftCP.position.y + leftCP.size.height  // Bottom center of CP box
      },
      type: "data-flow",
    });
  }

  // Ingress Load Balancer → Control Plane (external app traffic flows in)
  if (controlPlanes.length > 0 && ingressLB) {
    // Use the rightmost CP node to connect from Ingress LB (which is on the right)
    const rightCP = controlPlanes[controlPlanes.length - 1];
    connections.push({
      id: "ingress-lb-to-cp",
      from: {
        x: ingressLB.position.x + ingressLB.size.width / 2,
        y: ingressLB.position.y + ingressLB.size.height
      },
      to: {
        x: rightCP.position.x + rightCP.size.width / 2,
        y: rightCP.position.y + rightCP.size.height  // Bottom center of CP box
      },
      type: "data-flow",
    });
  }

  // EKS/IKS: Control Plane/Workers → Single Load Balancer
  if ((controlPlanes.length > 0 || workers.length > 0) && loadBalancers.length === 1 && !apiLB) {
    const lb = loadBalancers[0];
    const source = controlPlanes.length > 0 ? controlPlanes[0] : workers[0];
    connections.push({
      id: "nodes-to-lb",
      from: {
        x: source.position.x + source.size.width / 2,
        y: source.position.y + source.size.height
      },
      to: {
        x: lb.position.x + lb.size.width / 2,
        y: lb.position.y
      },
      type: "data-flow",
    });
  }

  // Load Balancers → Storage
  if (loadBalancers.length > 0 && storage.length > 0) {
    const lb = loadBalancers[0];
    const s3 = storage.find(s => s.id === "s3") || storage[0];
    connections.push({
      id: "lb-to-storage",
      from: {
        x: lb.position.x + lb.size.width / 2,
        y: lb.position.y + lb.size.height
      },
      to: {
        x: s3.position.x + s3.size.width / 2,
        y: s3.position.y
      },
      type: "dependency",
    });
  }

  layout.elements = elements;
  layout.connections = connections;
  layout.sections = sections;
  return layout;
}

/**
 * Get border color based on cluster status
 */
export function getStatusColor(status?: ClusterStatus): string {
  switch (status) {
    case ClusterStatus.READY:
      return "stroke-emerald-500";
    case ClusterStatus.CREATING:
    case ClusterStatus.HIBERNATING:
    case ClusterStatus.RESUMING:
      return "stroke-yellow-500";
    case ClusterStatus.FAILED:
    case ClusterStatus.DESTROY_FAILED:
      return "stroke-red-500";
    case ClusterStatus.HIBERNATED:
      return "stroke-blue-500";
    case ClusterStatus.PENDING:
    case ClusterStatus.DESTROYING:
    case ClusterStatus.DESTROYED:
    default:
      return "stroke-gray-400";
  }
}

/**
 * Get fill color for status
 */
export function getStatusFill(status?: ClusterStatus): string {
  switch (status) {
    case ClusterStatus.READY:
      return "fill-emerald-50 dark:fill-emerald-950";
    case ClusterStatus.CREATING:
    case ClusterStatus.HIBERNATING:
    case ClusterStatus.RESUMING:
      return "fill-yellow-50 dark:fill-yellow-950";
    case ClusterStatus.FAILED:
    case ClusterStatus.DESTROY_FAILED:
      return "fill-red-50 dark:fill-red-950";
    case ClusterStatus.HIBERNATED:
      return "fill-blue-50 dark:fill-blue-950";
    case ClusterStatus.PENDING:
    case ClusterStatus.DESTROYING:
    case ClusterStatus.DESTROYED:
    default:
      return "fill-gray-50 dark:fill-gray-900";
  }
}

/**
 * Get canvas dimensions
 */
export function getCanvasDimensions() {
  return {
    width: CANVAS_WIDTH,
    height: CANVAS_HEIGHT,
  };
}
