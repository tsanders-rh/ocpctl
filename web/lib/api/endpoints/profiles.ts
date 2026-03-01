import { apiClient } from "../client";
import type { Profile, Platform } from "@/types/api";

export const profilesApi = {
  list: async (platform?: Platform): Promise<Profile[]> => {
    const query = platform ? `?platform=${platform}` : "";
    return apiClient.get<Profile[]>(`/profiles${query}`);
  },

  get: async (name: string): Promise<Profile> => {
    return apiClient.get<Profile>(`/profiles/${name}`);
  },
};
