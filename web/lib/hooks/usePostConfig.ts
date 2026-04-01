import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { postConfigApi } from "../api";
import type {
  PostConfigTemplate,
  CustomPostConfig,
} from "@/types/api";

// Add-ons hooks
export function usePostConfigAddons(params?: {
  category?: string;
  platform?: string;
  search?: string;
}) {
  return useQuery({
    queryKey: ["postConfigAddons", params],
    queryFn: () => postConfigApi.addons.list(params),
    staleTime: 5 * 60 * 1000, // 5 minutes (add-ons don't change often)
  });
}

// Templates hooks
export function usePostConfigTemplates(params?: {
  public?: boolean;
  tags?: string[];
}) {
  return useQuery({
    queryKey: ["postConfigTemplates", params],
    queryFn: () => postConfigApi.templates.list(params),
    staleTime: 1 * 60 * 1000, // 1 minute
  });
}

export function usePostConfigTemplate(id: string) {
  return useQuery({
    queryKey: ["postConfigTemplate", id],
    queryFn: () => postConfigApi.templates.get(id),
    enabled: !!id,
    staleTime: 1 * 60 * 1000, // 1 minute
  });
}

export function useCreatePostConfigTemplate() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: {
      name: string;
      description: string;
      config: CustomPostConfig;
      isPublic: boolean;
      tags: string[];
    }) => postConfigApi.templates.create(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["postConfigTemplates"] });
    },
  });
}

export function useUpdatePostConfigTemplate() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({
      id,
      data,
    }: {
      id: string;
      data: {
        name: string;
        description: string;
        config: CustomPostConfig;
        isPublic: boolean;
        tags: string[];
      };
    }) => postConfigApi.templates.update(id, data),
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: ["postConfigTemplates"] });
      queryClient.invalidateQueries({
        queryKey: ["postConfigTemplate", variables.id],
      });
    },
  });
}

export function useDeletePostConfigTemplate() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) => postConfigApi.templates.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["postConfigTemplates"] });
    },
  });
}

// Validation hook
export function useValidatePostConfig() {
  return useMutation({
    mutationFn: (config: CustomPostConfig) => postConfigApi.validate(config),
  });
}
