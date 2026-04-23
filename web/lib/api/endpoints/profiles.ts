import { apiClient } from "../client";
import type { Profile, Platform } from "@/types/api";

export const profilesApi = {
  list: async (platform?: Platform, track?: string): Promise<Profile[]> => {
    const params = new URLSearchParams();
    if (platform) params.append("platform", platform);
    if (track) params.append("track", track);
    const query = params.toString() ? `?${params.toString()}` : "";
    return apiClient.get<Profile[]>(`/profiles${query}`);
  },

  get: async (name: string): Promise<Profile> => {
    return apiClient.get<Profile>(`/profiles/${name}`);
  },
};
