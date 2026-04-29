import { apiClient } from "../client";
import type {
  PostConfigAddonsResponse,
  PostConfigTemplatesResponse,
  PostConfigTemplate,
  ValidatePostConfigResponse,
  CustomPostConfig,
} from "@/types/api";

export const postConfigApi = {
  // Add-ons API
  addons: {
    list: async (params?: {
      category?: string;
      platform?: string;
      profile?: string;
      search?: string;
    }): Promise<PostConfigAddonsResponse> => {
      const queryParams = new URLSearchParams();
      if (params?.category) queryParams.set("category", params.category);
      if (params?.platform) queryParams.set("platform", params.platform);
      if (params?.profile) queryParams.set("profile", params.profile);
      if (params?.search) queryParams.set("search", params.search);

      const query = queryParams.toString() ? `?${queryParams.toString()}` : "";
      return apiClient.get<PostConfigAddonsResponse>(`/post-config/addons${query}`);
    },
  },

  // Templates API
  templates: {
    list: async (params?: {
      public?: boolean;
      tags?: string[];
    }): Promise<PostConfigTemplatesResponse> => {
      const queryParams = new URLSearchParams();
      if (params?.public !== undefined) queryParams.set("public", String(params.public));
      if (params?.tags && params.tags.length > 0) {
        queryParams.set("tags", params.tags.join(","));
      }

      const query = queryParams.toString() ? `?${queryParams.toString()}` : "";
      return apiClient.get<PostConfigTemplatesResponse>(`/templates${query}`);
    },

    get: async (id: string): Promise<PostConfigTemplate> => {
      return apiClient.get<PostConfigTemplate>(`/templates/${id}`);
    },

    create: async (data: {
      name: string;
      description: string;
      config: CustomPostConfig;
      isPublic: boolean;
      tags: string[];
    }): Promise<PostConfigTemplate> => {
      return apiClient.post<PostConfigTemplate>(`/templates`, data);
    },

    update: async (
      id: string,
      data: {
        name: string;
        description: string;
        config: CustomPostConfig;
        isPublic: boolean;
        tags: string[];
      }
    ): Promise<PostConfigTemplate> => {
      return apiClient.patch<PostConfigTemplate>(`/templates/${id}`, data);
    },

    delete: async (id: string): Promise<void> => {
      return apiClient.delete<void>(`/templates/${id}`);
    },
  },

  // Validation API
  validate: async (config: CustomPostConfig): Promise<ValidatePostConfigResponse> => {
    return apiClient.post<ValidatePostConfigResponse>(`/post-config/validate`, {
      config,
    });
  },
};
