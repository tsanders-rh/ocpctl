import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import {
  addonsApi,
  type ListAddonsParams,
  type CreateAddonRequest,
  type UpdateAddonRequest,
} from "../api/endpoints/addons";

/**
 * Fetch all available addons (system + published user addons)
 */
export function useAddons(params?: ListAddonsParams) {
  return useQuery({
    queryKey: ["addons", params],
    queryFn: () => addonsApi.list(params),
    staleTime: 30 * 1000, // 30 seconds
    refetchOnWindowFocus: true,
  });
}

/**
 * Fetch current user's addons (drafts + published)
 */
export function useMyAddons() {
  return useQuery({
    queryKey: ["addons", "my"],
    queryFn: () => addonsApi.listMy(),
    staleTime: 10 * 1000, // 10 seconds
    refetchOnWindowFocus: true,
  });
}

/**
 * Fetch a specific addon by ID
 */
export function useAddon(id: string) {
  return useQuery({
    queryKey: ["addon", id],
    queryFn: () => addonsApi.get(id),
    enabled: !!id,
    staleTime: 30 * 1000,
  });
}

/**
 * Create a new addon
 */
export function useCreateAddon() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: CreateAddonRequest) => addonsApi.create(data),
    onSuccess: () => {
      // Invalidate both addon lists
      queryClient.invalidateQueries({ queryKey: ["addons"] });
    },
  });
}

/**
 * Update an existing addon
 */
export function useUpdateAddon() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: UpdateAddonRequest }) =>
      addonsApi.update(id, data),
    onSuccess: (updatedAddon) => {
      // Invalidate addon lists
      queryClient.invalidateQueries({ queryKey: ["addons"] });
      // Update the specific addon in cache
      queryClient.setQueryData(["addon", updatedAddon.id], updatedAddon);
    },
  });
}

/**
 * Delete an addon
 */
export function useDeleteAddon() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) => addonsApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["addons"] });
    },
  });
}

/**
 * Publish a draft addon (makes it immutable)
 */
export function usePublishAddon() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) => addonsApi.publish(id),
    onSuccess: (publishedAddon) => {
      // Invalidate addon lists
      queryClient.invalidateQueries({ queryKey: ["addons"] });
      // Update the specific addon in cache
      queryClient.setQueryData(["addon", publishedAddon.id], publishedAddon);
    },
  });
}

/**
 * Clone an addon to create a new version
 */
export function useCloneAddon() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) => addonsApi.clone(id),
    onSuccess: () => {
      // Invalidate addon lists to show the new clone
      queryClient.invalidateQueries({ queryKey: ["addons"] });
    },
  });
}
