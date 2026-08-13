import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { clusterTemplatesApi } from "../api";

export function useClusterTemplates() {
  return useQuery({
    queryKey: ["clusterTemplates"],
    queryFn: () => clusterTemplatesApi.list(),
    staleTime: 60 * 1000,
  });
}

export function useCreateClusterTemplate() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (data: { name: string; config: Record<string, unknown> }) =>
      clusterTemplatesApi.create(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["clusterTemplates"] });
    },
  });
}

export function useUpdateClusterTemplate() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({
      id,
      data,
    }: {
      id: string;
      data: { name: string; config: Record<string, unknown> };
    }) => clusterTemplatesApi.update(id, data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["clusterTemplates"] });
    },
  });
}

export function useDeleteClusterTemplate() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: (id: string) => clusterTemplatesApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["clusterTemplates"] });
    },
  });
}
