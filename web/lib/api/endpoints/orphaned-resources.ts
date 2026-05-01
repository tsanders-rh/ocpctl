import { apiClient } from "../client";

export type OrphanedResourceType =
  // AWS Resources
  | "VPC"
  | "LoadBalancer"
  | "DNSRecord"
  | "EC2Instance"
  | "HostedZone"
  | "IAMRole"
  | "OIDCProvider"
  | "EBSVolume"
  | "ElasticIP"
  | "CloudWatchLogGroup"
  // GCP Resources
  | "GCPServiceAccount"
  | "GCPNetwork"
  | "GCPSubnetwork"
  | "GCPDisk"
  | "GCPInstance"
  | "GCPBucket"
  | "GCPIPAddress";

export type OrphanedResourceStatus = "ACTIVE" | "RESOLVED" | "IGNORED";

export interface OrphanedResource {
  id: string;
  resource_type: OrphanedResourceType;
  resource_id: string;
  resource_name: string;
  region: string;
  cluster_name: string;
  tags: Record<string, string>;
  first_detected_at: string;
  last_detected_at: string;
  detection_count: number;
  status: OrphanedResourceStatus;
  resolved_at?: string;
  resolved_by?: string;
  notes?: string;
  created_at: string;
  updated_at: string;
}

export interface OrphanedResourceStats {
  total_active: number;
  total_resolved: number;
  total_ignored: number;
  by_type: Record<string, number>;
  by_region: Record<string, number>;
  oldest_detected?: string;
}

export interface OrphanedResourceListResponse {
  resources: OrphanedResource[];
  total: number;
  limit: number;
  offset: number;
}

export interface OrphanedResourceFilters {
  status?: OrphanedResourceStatus;
  type?: OrphanedResourceType;
  region?: string;
  limit?: number;
  offset?: number;
}

export interface MarkResolvedRequest {
  notes: string;
}

export interface MarkIgnoredRequest {
  notes: string;
}

export const orphanedResourcesApi = {
  /**
   * List orphaned resources with optional filters
   */
  list: async (
    filters?: OrphanedResourceFilters
  ): Promise<OrphanedResourceListResponse> => {
    const params = new URLSearchParams();

    if (filters?.status) params.append("status", filters.status);
    if (filters?.type) params.append("type", filters.type);
    if (filters?.region) params.append("region", filters.region);
    if (filters?.limit) params.append("limit", filters.limit.toString());
    if (filters?.offset) params.append("offset", filters.offset.toString());

    const queryString = params.toString();
    const url = `/admin/orphaned-resources${queryString ? `?${queryString}` : ""}`;

    return apiClient.get<OrphanedResourceListResponse>(url);
  },

  /**
   * Get statistics for orphaned resources
   */
  getStats: async (): Promise<OrphanedResourceStats> => {
    return apiClient.get<OrphanedResourceStats>("/admin/orphaned-resources/stats");
  },

  /**
   * Mark an orphaned resource as resolved
   */
  markResolved: async (
    id: string,
    notes: string
  ): Promise<OrphanedResource> => {
    return apiClient.patch<OrphanedResource>(
      `/admin/orphaned-resources/${id}/resolve`,
      { notes }
    );
  },

  /**
   * Mark an orphaned resource as ignored
   */
  markIgnored: async (
    id: string,
    notes: string
  ): Promise<OrphanedResource> => {
    return apiClient.patch<OrphanedResource>(
      `/admin/orphaned-resources/${id}/ignore`,
      { notes }
    );
  },

  /**
   * Delete an orphaned resource (currently only supports HostedZone)
   * This will actually delete the AWS resource and mark it as resolved
   */
  deleteResource: async (id: string): Promise<OrphanedResource> => {
    return apiClient.delete<OrphanedResource>(
      `/admin/orphaned-resources/${id}`
    );
  },
};
