import { apiClient } from "../client";

export interface LinkStorageRequest {
  target_cluster_id: string;
}

export interface ClusterStorageLinkResponse {
  cluster_id: string;
  cluster_name: string;
  role: "source" | "target" | "shared";
  linked_at: string;
}

export interface StorageGroupResponse {
  id: string;
  name: string;
  efs_id?: string;
  efs_security_group_id?: string;
  s3_bucket?: string;
  region: string;
  status: "PROVISIONING" | "READY" | "FAILED" | "DELETING";
  linked_clusters: ClusterStorageLinkResponse[];
  created_at: string;
  updated_at: string;
}

export interface LinkStorageResponse {
  message: string;
  job_id: string;
  source_cluster: string;
  target_cluster: string;
  estimated_time: string;
}

export interface UnlinkStorageResponse {
  message: string;
  job_id: string;
  cluster: string;
  storage_group: string;
  estimated_time: string;
}

export const storageApi = {
  /**
   * Link a cluster to another cluster for shared storage
   */
  linkToCluster: async (
    clusterId: string,
    targetClusterId: string
  ): Promise<LinkStorageResponse> => {
    return apiClient.post<LinkStorageResponse>(
      `/clusters/${clusterId}/storage/link`,
      { target_cluster_id: targetClusterId }
    );
  },

  /**
   * Get all storage groups linked to a cluster
   */
  getStorage: async (clusterId: string): Promise<StorageGroupResponse[]> => {
    return apiClient.get<StorageGroupResponse[]>(
      `/clusters/${clusterId}/storage`
    );
  },

  /**
   * Unlink a cluster from a storage group
   */
  unlinkStorage: async (
    clusterId: string,
    storageGroupId: string
  ): Promise<UnlinkStorageResponse> => {
    return apiClient.delete<UnlinkStorageResponse>(
      `/clusters/${clusterId}/storage/link/${storageGroupId}`
    );
  },
};
