import { apiClient } from "../client";

export type WindowsSnapshotStatus =
  | "creating"
  | "validating"
  | "ready"
  | "failed"
  | "deleting";

export interface WindowsSnapshot {
  id: string;
  region: string;
  version: string;
  ebs_snapshot_id: string;
  status: WindowsSnapshotStatus;
  ssm_parameter_path?: string;
  s3_source_url?: string;
  created_at: string;
  updated_at: string;
  validated_at?: string;
  error_message?: string;
  job_id?: string;
  snapshot_size_gb?: number;
  validation_vm_booted: boolean;
}

export interface WindowsSnapshotCoverage {
  total_regions: number;
  covered_regions: number;
  coverage_percent: number;
  latest_version: string;
  missing_regions: string[];
  outdated_regions: string[];
  snapshots_by_region: Record<string, WindowsSnapshot>;
}

export interface WindowsSnapshotListResponse {
  snapshots: WindowsSnapshot[];
  total: number;
}

export interface WindowsSnapshotFilters {
  region?: string;
  status?: WindowsSnapshotStatus;
}

export interface CreateWindowsSnapshotRequest {
  region: string;
  version: string;
  s3_source_url?: string;
}

export interface CreateWindowsSnapshotResponse {
  snapshot_id: string;
  job_id: string;
  status: WindowsSnapshotStatus;
  message: string;
}

export const windowsSnapshotsApi = {
  /**
   * List Windows snapshots with optional filters
   */
  list: async (
    filters?: WindowsSnapshotFilters
  ): Promise<WindowsSnapshotListResponse> => {
    const params = new URLSearchParams();

    if (filters?.region) params.append("region", filters.region);
    if (filters?.status) params.append("status", filters.status);

    const queryString = params.toString();
    const url = `/admin/windows-snapshots${queryString ? `?${queryString}` : ""}`;

    return apiClient.get<WindowsSnapshotListResponse>(url);
  },

  /**
   * Get a single Windows snapshot by ID
   */
  get: async (id: string): Promise<WindowsSnapshot> => {
    return apiClient.get<WindowsSnapshot>(`/admin/windows-snapshots/${id}`);
  },

  /**
   * Get snapshot coverage statistics
   */
  getCoverage: async (version?: string): Promise<WindowsSnapshotCoverage> => {
    const params = new URLSearchParams();
    if (version) params.append("version", version);

    const queryString = params.toString();
    const url = `/admin/windows-snapshots/coverage${queryString ? `?${queryString}` : ""}`;

    return apiClient.get<WindowsSnapshotCoverage>(url);
  },

  /**
   * Create a new Windows snapshot
   */
  create: async (
    request: CreateWindowsSnapshotRequest
  ): Promise<CreateWindowsSnapshotResponse> => {
    return apiClient.post<CreateWindowsSnapshotResponse>(
      "/admin/windows-snapshots",
      request
    );
  },

  /**
   * Delete a Windows snapshot
   */
  delete: async (id: string): Promise<void> => {
    return apiClient.delete(`/admin/windows-snapshots/${id}`);
  },
};
