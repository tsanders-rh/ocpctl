import { apiClient } from "../client";

export interface PostConfigAddon {
  id: string;
  addon_id: string;
  name: string;
  description: string;
  category: string;
  supported_platforms: string[];
  enabled: boolean;
  version: string;
  display_name: string;
  is_default: boolean;
  addon_source: "system" | "user";
  created_by_user_id?: string;
  is_published: boolean;
  is_immutable: boolean;
  published_at?: string;
  parent_version_id?: string;
  version_number: number;
  created_at: string;
  updated_at: string;
  metadata?: {
    requires_bare_metal?: boolean;
    required_capabilities?: string[];
    notes?: string[];
    warnings?: string[];
  };
  config: {
    operators?: Array<{
      name: string;
      namespace: string;
      source?: string;
      channel?: string;
      depends_on?: string[];
    }>;
    scripts?: Array<{
      name: string;
      path: string;
      timeout?: string;
      depends_on?: string[];
    }>;
    manifests?: Array<{
      name: string;
      content?: string;
      path?: string;
      depends_on?: string[];
    }>;
    helm_charts?: Array<{
      name: string;
      repo: string;
      chart: string;
      version?: string;
      namespace?: string;
      values?: Record<string, any>;
      depends_on?: string[];
    }>;
  };
}

export interface ListAddonsParams {
  category?: string;
  platform?: string;
  search?: string;
}

export interface CreateAddonRequest {
  addon_id: string;
  name: string;
  description: string;
  category: string;
  supported_platforms: string[];
  enabled?: boolean;
  version: string;
  display_name: string;
  config: PostConfigAddon["config"];
  metadata?: PostConfigAddon["metadata"];
}

export interface UpdateAddonRequest {
  name?: string;
  description?: string;
  category?: string;
  enabled?: boolean;
  display_name?: string;
  config?: PostConfigAddon["config"];
  metadata?: PostConfigAddon["metadata"];
}

export interface CloneAddonRequest {
  // No additional fields needed - clones entire addon
}

export const addonsApi = {
  /**
   * List all addons (system and published user addons)
   */
  list: async (params?: ListAddonsParams): Promise<PostConfigAddon[]> => {
    const queryParams = new URLSearchParams();
    if (params?.category) queryParams.append("category", params.category);
    if (params?.platform) queryParams.append("platform", params.platform);
    if (params?.search) queryParams.append("search", params.search);

    const query = queryParams.toString();
    const endpoint = `/post-config/addons${query ? `?${query}` : ""}`;
    return apiClient.get<PostConfigAddon[]>(endpoint);
  },

  /**
   * List user's own addons (drafts and published)
   */
  listMy: async (): Promise<PostConfigAddon[]> => {
    return apiClient.get<PostConfigAddon[]>("/post-config/addons/my");
  },

  /**
   * Get addon by ID
   */
  get: async (id: string): Promise<PostConfigAddon> => {
    return apiClient.get<PostConfigAddon>(`/post-config/addons/${id}`);
  },

  /**
   * Create a new user addon
   */
  create: async (data: CreateAddonRequest): Promise<PostConfigAddon> => {
    return apiClient.post<PostConfigAddon>("/post-config/addons", data);
  },

  /**
   * Update an existing addon (only drafts can be updated)
   */
  update: async (
    id: string,
    data: UpdateAddonRequest
  ): Promise<PostConfigAddon> => {
    return apiClient.put<PostConfigAddon>(`/post-config/addons/${id}`, data);
  },

  /**
   * Delete an addon
   */
  delete: async (id: string): Promise<void> => {
    return apiClient.delete<void>(`/post-config/addons/${id}`);
  },

  /**
   * Publish a draft addon (makes it immutable)
   */
  publish: async (id: string): Promise<PostConfigAddon> => {
    return apiClient.post<PostConfigAddon>(
      `/post-config/addons/${id}/publish`,
      {}
    );
  },

  /**
   * Clone an addon to create a new version
   */
  clone: async (id: string): Promise<PostConfigAddon> => {
    return apiClient.post<PostConfigAddon>(
      `/post-config/addons/${id}/clone`,
      {}
    );
  },
};
